"""
Ценообразование группы компаний (постановка заказчика 23.09.2026).

Товар внутри группы проходит цепочку компаний: каждая продаёт следующей со
своей корректировкой к цене (+ наценка / − скидка), последняя — внешнему
рынку. Заказчику нужен финрез такой цепочки по ассортименту главной таблицы:
итог группы, доход каждого звена и проваливание Level 01 → … → Level 05 →
модель → артикул.

Что решил заказчик 23.09.2026 и почему модуль устроен так:

  · компании группы — редактируемый справочник (`cost_group_company`), не
    константы: состав группы меняется;
  · цепочка (`cost_group_chain`) привязана к АССОРТИМЕНТУ — хранит набор
    фильтров главной таблицы (`scope`, тот же белый список, что у
    POST /aggregated). Пересечение ассортимента разных цепочек допускается,
    его только подсвечивают (`chain_overlaps`);
  · база цены — отпускная «как в главной таблице»: `Отпускная цена по уровню,
    руб` из `cost_data_all` с наложением утверждённых цен в том же порядке
    источников, что в get_aggregated (pending → DWH → локальный аудит);
  · режим расчёта — на цепочке: `cascade` (% звена к цене предыдущего звена)
    или `base` (все % к базовой отпускной);
  · без расходов звеньев и без НДС — только цены. Себестоимость производителя
    (Σ шести статей, как `_sum_components` в routes.py, колонку
    «Себестоимость, руб.» не используем) нужна для дохода первого звена и
    итога группы;
  · всё в BYN; валюта отгрузки звена — дополнительный показ сумм звена по
    курсу НБ РБ (`purchase.rates_for_date`), она не обязана совпадать с
    валютой страны компании;
  · у каждого звена хранится эффективный % к базовой отпускной (`eff_pct`)
    независимо от режима — для будущей выгрузки в региональные 1С
    (витрина `cost_group_pricing_export`);
  · право `cost:group_pricing` — только у Full Admin (миграция 0059).

Расчёт — чистые функции над «базовыми строками» (единица = строка главной
таблицы, ключ калькуляции с датой и заданием). Группировка проваливания и
суммы — в Python: строк до десятков тысяч, это миллисекунды, а SQL на каждый
клик по матрице ходил бы в кэш заново. Карта утверждённых цен кэшируется в
модуле на 60 с, курсы — на 10 мин по дате: проваливание и список цепочек не
должны ходить в OLAP на каждый клик.

Pydantic-схем в разделе нет — функции принимают `dict` и сами валидируют,
ошибки — HTTPException с русским `detail` (400 валидация, 404 нет объекта,
409 дубль имени / компания в цепочках, 502 недоступен внешний источник).
"""
from __future__ import annotations

import asyncio
import json
import logging
import math
import os
import time
from datetime import date, datetime, timedelta, timezone
from typing import Any

import asyncpg
from fastapi import HTTPException

from app import purchase
from app.db import acquire, fetch_olap_changes
from app.logship import log

# Режим расчёта цепочки → подпись для интерфейса (meta.mode_label).
MODES: dict[str, str] = {
    "cascade": "каскад: % от цены предыдущего звена",
    "base": "от базовой: все % от базовой отпускной",
}
# Вес строки в агрегатах → подпись (base.weight_label). «выпуск шт» есть в
# основном у ФКСС; строки без объёма при weight=volume из денег выпадают и
# считаются отдельно (rows_without_volume).
WEIGHTS: dict[str, str] = {
    "unit": "по калькуляциям (1 строка = 1 ед.)",
    "volume": "по выпуску, шт («выпуск шт» главной таблицы)",
}
STATUSES = ("active", "archived")

# Иерархия проваливания финреза. Ключ — поле базовой строки, подпись — как в
# главной таблице. Путь (`path`) — значения по этим уровням сверху вниз.
LEVELS: list[tuple[str, str]] = [
    ("level01", "Level 01"),
    ("level02", "Level 02"),
    ("level03", "Level 03"),
    ("level04", "Level 04"),
    ("level05", "Level 05"),
    ("model", "Модель"),
    ("articul", "Артикул"),
]
ROW_LIMIT = 300

# Белый список фильтров ассортимента (scope) → колонка cost_data_all. Ключи и
# значения — текстом, как принимает POST /aggregated (MULTI_FILTER_COLUMNS в
# routes.py): фронт переиспользует /filter-options без перекодировки. Всё, чего
# здесь нет, при сохранении молча отбрасывается.
SCOPE_LIST_KEYS: dict[str, str] = {
    "level01": "Level 01",
    "level02": "Level 02",
    "level03": "Level 03",
    "level04": "Level 04",
    "level05": "Level 05",
    "brand_manager": "Бренд-менеджер",
    "country": "Страна пр-ва",
    "season": "Сезон",
    "calc_sign": "Признак калькуляции",
    "model": "Модель",
    "articul": "Артикул",
    "plan_id": "PLAN_ID",
}
SCOPE_DATE_KEYS = ("date_from", "date_to")
# Предел значений на один ключ scope. Модели и артикулы фронт принимает
# textarea-ей, а в SQL список уходит как `= ANY($n)`: на десятках тысяч
# значений запрос к cost_data_all и хранение JSONB перестают быть дешёвыми,
# а пользы от такого «ассортимента» нет — это уже не фильтр. Длина одного
# значения ограничена тем же соображением (в кэше ключи короче 100 символов).
MAX_SCOPE_VALUES = 5000
MAX_SCOPE_VALUE_LEN = 200

OVERRIDE_TTL = 60.0     # с; карта утверждённых цен (pending → DWH → аудит)
RATES_TTL = 600.0       # с; курсы НБ РБ на дату
RATES_CACHE_MAX = 64    # дат в кэше курсов: дата приходит из запроса, без предела кэш растёт бесконечно
OVERLAPS_TTL = 60.0     # с; пересечения ассортимента активных цепочек
BASE_ROWS_TTL = 60.0    # с; базовые строки по scope — проваливание и summary не должны повторять GROUP BY
BASE_ROWS_MAX = 16      # scope-ов в кэше базовых строк (до десятков тысяч dict на запись)
# Таймаут похода в OLAP за утверждёнными ценами. Карта строится под
# asyncio.Lock, то есть пока поток ждёт DWH, висят ВСЕ запросы финреза; а у
# pyodbc LoginTimeout 30 с — при упавшем VPN это полминуты тишины. Через
# OLAP_TIMEOUT_S секунд берём локальный аудит, как при любой другой ошибке.
OLAP_TIMEOUT_S = 10.0
# Допустимая дата курса: курсы НБ РБ в DWH начинаются не раньше 2000 года, а
# на будущее дальше года их нет и не будет — всё остальное опечатка.
RATE_DATE_MIN = date(2000, 1, 1)
RATE_DATE_AHEAD_DAYS = 366

# Фиксированные курсы mock-режима — те же, что отдаёт /purchase/rates при
# COST_MOCK=1, чтобы предпросмотр в моке совпадал с тем, что видит фронт.
MOCK_RATES: dict[str, float] = {
    "BYN": 1, "USD": 3.0687, "EUR": 3.5507, "RUB": 0.035546,
    "KZT": 0.0067319, "UZS": 0.00025744, "CNY": 0.45871,
}

Key4 = tuple[str, str, str, str]


# ── Мелкие помощники ─────────────────────────────────────────────────────────

def _s(v: Any) -> str:
    return str(v if v is not None else "").strip()


def _f(v: Any) -> float | None:
    """Число из asyncpg (Decimal/int/str) или None. Знак не проверяет."""
    if v is None or v == "":
        return None
    try:
        x = float(v)
    except (TypeError, ValueError):
        return None
    return x if math.isfinite(x) else None


def _iso(v: Any) -> Any:
    if isinstance(v, (datetime, date)):
        return v.isoformat()
    return v


def _parse_date(v: Any) -> date | None:
    if isinstance(v, datetime):
        return v.date()
    if isinstance(v, date):
        return v
    s = _s(v)[:10]
    try:
        return date.fromisoformat(s)
    except ValueError:
        return None


def _as_bool(v: Any, default: bool) -> bool:
    if v is None:
        return default
    if isinstance(v, bool):
        return v
    if isinstance(v, (int, float)):
        return bool(v)
    return _s(v).lower() not in ("", "0", "false", "no", "off")


def _pct(part: float, whole: float) -> float | None:
    return part / whole * 100.0 if whole else None


def _is_mock() -> bool:
    return os.environ.get("COST_MOCK", "").strip() == "1"


def _now_iso() -> str:
    return datetime.now(timezone.utc).isoformat()


# id компаний и цепочек — BIGSERIAL. Путь `/group/chains/{chain_id}` FastAPI
# принимает любым int, и 20-значный id уходил в asyncpg как `$1` bigint:
# «value out of int64 range» → необработанное исключение → 500. Объекта с таким
# id быть не может, поэтому ответ — тот же 404, что и на любой несуществующий.
_ID_MAX = 2**63 - 1


def _id_ok(obj_id: int) -> bool:
    return 1 <= obj_id <= _ID_MAX


# ── Scope: нормализация и SQL ────────────────────────────────────────────────

def normalize_scope(raw: Any) -> dict[str, Any]:
    """Привести scope к хранимому виду по белому списку §3 контракта.

    Неизвестные ключи отбрасываются, значения — `str.strip()`, пустые и
    дубликаты убираются, «all» (так /aggregated обозначает «без фильтра»)
    равносильно отсутствию. Даты проверяются и хранятся ISO-строкой.
    `latest_only` пишется всегда, чтобы сохранённый scope читался без знания
    умолчаний кода.
    """
    if raw is None:
        raw = {}
    if not isinstance(raw, dict):
        raise HTTPException(400, "scope должен быть объектом с фильтрами ассортимента")
    out: dict[str, Any] = {}
    for key in SCOPE_LIST_KEYS:
        vals = raw.get(key)
        if vals is None or vals == "":
            continue
        if isinstance(vals, str):
            vals = [vals]
        if not isinstance(vals, (list, tuple, set)):
            raise HTTPException(400, f"scope.{key}: ожидается список значений")
        clean: list[str] = []
        seen: set[str] = set()  # дубликаты снимаем через set: textarea артикулов бывает на тысячи строк
        for v in vals:
            s = _s(v)
            if not s or s == "all" or s in seen:
                continue
            if len(s) > MAX_SCOPE_VALUE_LEN:
                raise HTTPException(400, f"scope.{key}: значение длиннее {MAX_SCOPE_VALUE_LEN} символов")
            seen.add(s)
            clean.append(s)
            if len(clean) > MAX_SCOPE_VALUES:
                raise HTTPException(400, f"слишком много значений в scope.{key} (макс. {MAX_SCOPE_VALUES})")
        if clean:
            out[key] = clean
    dates: dict[str, date] = {}
    for key in SCOPE_DATE_KEYS:
        v = raw.get(key)
        if v in (None, ""):
            continue
        d = _parse_date(v)
        if d is None:
            raise HTTPException(400, f"scope.{key}: дата в формате YYYY-MM-DD")
        dates[key] = d
    if "date_from" in dates and "date_to" in dates and dates["date_from"] > dates["date_to"]:
        raise HTTPException(400, "scope: date_from позже date_to")
    for key, d in dates.items():
        out[key] = d.isoformat()
    out["latest_only"] = _as_bool(raw.get("latest_only"), default=True)
    return out


def scope_where(scope: dict[str, Any], params: list[Any]) -> str:
    """WHERE по scope для cost_data_all; значения — параметрами asyncpg.

    Условия те же, что в get_aggregated: `TRIM("Level 01") = ANY($n)` и даты по
    «дата расчета» включительно (в кэше все даты — полночь, так что `<=` с
    датой и есть «включительно», как в /load-data).
    """
    parts: list[str] = []
    for key, col in SCOPE_LIST_KEYS.items():
        vals = scope.get(key) or []
        if vals:
            params.append([str(v) for v in vals])
            parts.append(f'TRIM("{col}") = ANY(${len(params)}::text[])')
    if scope.get("date_from"):
        params.append(date.fromisoformat(scope["date_from"]))
        parts.append(f'"дата расчета" >= ${len(params)}')
    if scope.get("date_to"):
        params.append(date.fromisoformat(scope["date_to"]))
        parts.append(f'"дата расчета" <= ${len(params)}')
    return " AND ".join(parts) if parts else "TRUE"


# ── Базовые строки — «как в главной таблице» (§4.1) ──────────────────────────

# Единица расчёта — строка главной таблицы: ключ калькуляции (модель, артикул,
# признак, план, дата расчёта, задание). Цена — AVG по строкам материалов (у
# всех строк ключа она одна), себестоимость — Σ шести статей как
# `_sum_components`, объём — MAX «выпуск шт». Атрибуты уровней — MAX(TRIM()):
# внутри ключа они одинаковы, TRIM — чтобы путь проваливания сравнивался с
# тем же значением, что и фильтр scope. Признак — через COALESCE(…, ''), как в
# карте цен (universe / pending / аудит): иначе строка с пустым признаком имела
# бы ключ (m, a, None, pi) и не находила свою утверждённую цену под (m, a, '', pi).
_BASE_CTE = """
SELECT TRIM("Модель") AS m, TRIM("Артикул") AS a, TRIM(COALESCE("Признак калькуляции", '')) AS cs,
       TRIM(COALESCE("PLAN_ID", '')) AS pi, "дата расчета" AS d,
       TRIM(COALESCE("Номер задания производства", '')) AS task,
       MAX(TRIM("Level 01")) AS level01, MAX(TRIM("Level 02")) AS level02,
       MAX(TRIM("Level 03")) AS level03, MAX(TRIM("Level 04")) AS level04,
       MAX(TRIM("Level 05")) AS level05,
       MAX(TRIM("Наименование модели")) AS model_name, MAX(TRIM("Бренд-менеджер")) AS bm,
       AVG("Отпускная цена по уровню, руб") AS base_src,
       COALESCE(SUM("Пошив, руб."), 0) + COALESCE(SUM("Раскрой, руб."), 0)
     + COALESCE(SUM("Декоры, руб."), 0) + COALESCE(SUM("Вязание, руб."), 0)
     + COALESCE(SUM("Основные материалы, руб."), 0)
     + COALESCE(SUM("Вспомогательные материалы, руб."), 0) AS cost,
       MAX("выпуск шт") AS volume
  FROM cost_data_all
 WHERE {where}
 GROUP BY 1, 2, 3, 4, 5, 6
"""

_BASE_COLS = "m, a, cs, pi, d, task, level01, level02, level03, level04, level05, model_name, bm, base_src, cost, volume"

# Кэш базовых строк по scope. Клик по матрице (path) и плитки редактора
# (summary) — тот же scope, а GROUP BY по cost_data_all на нём стоит сотни
# миллисекунд; строки же меняются только с обновлением кэша источника, редко.
# Ключ — канонический JSON нормализованного scope. Наружу отдаются КОПИИ
# словарей: apply_prices проставляет строкам base/price_source на месте, и
# второй проход по общим объектам наложил бы цены поверх уже наложенных.
_base_rows_cache: dict[str, tuple[float, list[dict[str, Any]]]] = {}


async def base_rows(scope: dict[str, Any]) -> list[dict[str, Any]]:
    """Строки главной таблицы по scope — один запрос к cost_data_all (не к cost_calc_mv).

    `latest_only` оставляет на ключ цены (модель, артикул, признак, план) только
    строки последней «дата расчета» — оконной функцией во внешнем SELECT.
    Результат живёт BASE_ROWS_TTL секунд (см. `_base_rows_cache`).
    """
    cache_key = json.dumps(scope, sort_keys=True, ensure_ascii=False, default=str)
    hit = _base_rows_cache.get(cache_key)
    if hit and time.monotonic() - hit[0] < BASE_ROWS_TTL:
        return [dict(r) for r in hit[1]]
    params: list[Any] = []
    where = scope_where(scope, params)
    cte = _BASE_CTE.format(where=where)
    if scope.get("latest_only", True):
        sql = (
            f"WITH b AS ({cte}), r AS (SELECT b.*, MAX(d) OVER (PARTITION BY m, a, cs, pi) AS dmax FROM b) "
            f"SELECT {_BASE_COLS} FROM r WHERE d IS NOT DISTINCT FROM dmax"
        )
    else:
        sql = f"WITH b AS ({cte}) SELECT {_BASE_COLS} FROM b"
    async with acquire() as conn:
        recs = await conn.fetch(sql, *params)
    rows: list[dict[str, Any]] = []
    for r in recs:
        rows.append({
            "m": r["m"], "a": r["a"], "cs": r["cs"], "pi": r["pi"],
            "d": _iso(r["d"]), "task": r["task"],
            "level01": r["level01"], "level02": r["level02"], "level03": r["level03"],
            "level04": r["level04"], "level05": r["level05"],
            # дублируем под ключами LEVELS, чтобы проваливание и путь читали
            # строку одинаково на всех уровнях
            "model": r["m"], "articul": r["a"],
            "model_name": r["model_name"], "bm": r["bm"],
            "base_src": _f(r["base_src"]),
            "cost": _f(r["cost"]) or 0.0,
            "volume": _f(r["volume"]),
        })
    now = time.monotonic()
    for k in [k for k, v in _base_rows_cache.items() if now - v[0] >= BASE_ROWS_TTL]:
        del _base_rows_cache[k]
    while len(_base_rows_cache) >= BASE_ROWS_MAX:
        del _base_rows_cache[min(_base_rows_cache, key=lambda k: _base_rows_cache[k][0])]
    _base_rows_cache[cache_key] = (now, rows)
    return [dict(r) for r in rows]


# ── Наложение утверждённых цен (§4.2) ────────────────────────────────────────

_override_lock = asyncio.Lock()
_override_cache: dict[str, Any] = {"at": 0.0, "map": None, "refreshed_at": None}


async def price_overrides() -> tuple[dict[Key4, tuple[float | None, str]], str | None]:
    """Карта утверждённых цен на ВСЕ ключи кэша: (m, a, cs, pi) → (опт, источник).

    Порядок источников — как в get_aggregated: 1) `cost_price_pending`
    (несогласованные правки, таблица маленькая — читается целиком),
    2) DWH `CostHistory_Changes` через `fetch_olap_changes`, 3) при недоступности
    OLAP — локальный аудит `cost_price_changes_audit`. Запись более приоритетного
    источника «занимает» ключ даже без опта — так же ведёт себя главная таблица,
    иначе цены здесь и там расходились бы.

    Ключ кэша — «все»: карта строится на весь набор ключей cost_data_all
    (~30 тыс. на 23.09.2026, 0.8 с), чтобы проваливание, предпросмотр и список
    цепочек не ходили в OLAP на каждый клик. Живёт OVERRIDE_TTL секунд; под
    замком, чтобы параллельные запросы не строили её наперегонки.
    """
    now = time.monotonic()
    if _override_cache["map"] is not None and now - _override_cache["at"] < OVERRIDE_TTL:
        return _override_cache["map"], _override_cache["refreshed_at"]
    async with _override_lock:
        now = time.monotonic()
        if _override_cache["map"] is not None and now - _override_cache["at"] < OVERRIDE_TTL:
            return _override_cache["map"], _override_cache["refreshed_at"]

        overrides: dict[Key4, tuple[float | None, str]] = {}
        claimed: set[Key4] = set()
        async with acquire() as conn:
            pend = await conn.fetch(
                """SELECT DISTINCT ON (m, a, cs, pi) m, a, cs, pi, w FROM (
                       SELECT TRIM("Модель") AS m, TRIM("Артикул") AS a,
                              TRIM(COALESCE("Признак калькуляции", '')) AS cs,
                              TRIM(COALESCE("PLAN_ID", '')) AS pi,
                              "Отпускная цена по уровню, руб" AS w, created_at
                         FROM cost_price_pending) p
                   ORDER BY m, a, cs, pi, created_at DESC"""
            )
            for r in pend:
                k: Key4 = (r["m"], r["a"], r["cs"], r["pi"])
                claimed.add(k)
                w = _f(r["w"])
                if w is not None:
                    overrides[k] = (w, "pending")
            universe = await conn.fetch(
                """SELECT DISTINCT TRIM("Модель") AS m, TRIM("Артикул") AS a,
                          TRIM(COALESCE("Признак калькуляции", '')) AS cs,
                          TRIM(COALESCE("PLAN_ID", '')) AS pi
                     FROM cost_data_all"""
            )
        keys: list[Key4] = [(r["m"], r["a"], r["cs"], r["pi"]) for r in universe]

        olap_ok = False
        if not _is_mock():
            try:
                loop = asyncio.get_running_loop()
                recs = await asyncio.wait_for(
                    loop.run_in_executor(None, fetch_olap_changes, keys), timeout=OLAP_TIMEOUT_S)
                for rec in recs:
                    k = (_s(rec.get("Модель")), _s(rec.get("Артикул")),
                         _s(rec.get("calc_sign")), _s(rec.get("plan_id")))
                    if k in claimed:
                        continue
                    claimed.add(k)
                    w = _f(rec.get("wholesale_rub"))
                    if w is not None:
                        overrides[k] = (w, "dwh")
                olap_ok = True
            except asyncio.TimeoutError:
                log(logging.WARNING,
                    "ценообразование группы: OLAP не ответил, утверждённые цены из локального аудита",
                    timeout_s=OLAP_TIMEOUT_S, keys=len(keys))
            except Exception as exc:  # noqa: BLE001 — OLAP за VPN
                log(logging.WARNING,
                    "ценообразование группы: OLAP недоступен, утверждённые цены из локального аудита",
                    error=str(exc), keys=len(keys))
        if not olap_ok:
            async with acquire() as conn:
                arows = await conn.fetch(
                    """SELECT DISTINCT ON (m, a, cs, pi) m, a, cs, pi, w FROM (
                           SELECT TRIM(model) AS m, TRIM(articul) AS a,
                                  TRIM(COALESCE(calc_sign, '')) AS cs, TRIM(COALESCE(plan_id, '')) AS pi,
                                  wholesale_rub AS w, changed_at
                             FROM cost_price_changes_audit) t
                       ORDER BY m, a, cs, pi, changed_at DESC"""
                )
                for r in arows:
                    k = (r["m"], r["a"], r["cs"], r["pi"])
                    if k in claimed:
                        continue
                    claimed.add(k)
                    w = _f(r["w"])
                    if w is not None:
                        overrides[k] = (w, "audit")

        _override_cache.update({"at": time.monotonic(), "map": overrides, "refreshed_at": _now_iso()})
        return overrides, _override_cache["refreshed_at"]


def apply_prices(rows: list[dict[str, Any]], overrides: dict[Key4, tuple[float | None, str]]) -> None:
    """Проставить строкам `base` и `price_source` (pending | dwh | audit | calc | none)."""
    for row in rows:
        k: Key4 = (row["m"], row["a"], row["cs"], row["pi"])
        o = overrides.get(k)
        if o is not None:
            price, source = o
        else:
            price, source = row["base_src"], "calc"
        if price is None or price <= 0:
            price, source = None, "none"
        row["base"] = price
        row["price_source"] = source


# ── Цепочка: чистая арифметика (§4.4) ────────────────────────────────────────

def compute_chain(base: float, cost: float, links: list[dict[str, Any]], mode: str) -> list[dict[str, Any]]:
    """Цены и доход по звеньям для одной строки. Без БД и без округления.

    cascade: out_1 = base·(1 + a_1/100), out_k = out_{k−1}·(1 + a_k/100);
    base:    out_k = base·(1 + a_k/100).
    in_1 = cost (первое звено покупает у производителя по себестоимости),
    in_k = out_{k−1}; income_k = out_k − in_k. eff_pct_k = (out_k / base − 1)·100 —
    не зависит от строки, это то, что хранится в звене для 1С.
    """
    result: list[dict[str, Any]] = []
    inp = cost
    prev = base
    for i, link in enumerate(links, start=1):
        a = float(link.get("adjust_pct") or 0.0)
        ref = prev if mode == "cascade" else base
        out = ref * (1.0 + a / 100.0)
        result.append({
            "seq": int(link.get("seq") or i),
            "in": inp,
            "out": out,
            "income": out - inp,
            "eff_pct": (out / base - 1.0) * 100.0 if base else None,
        })
        inp = out
        prev = out
    return result


def eff_pcts(links: list[dict[str, Any]], mode: str) -> list[float]:
    """Эффективный % каждого звена к базовой — считается от условной базы 100.

    Округление до 6 знаков — точность колонки eff_pct NUMERIC(14,6): предпросмотр
    (simulate) и сохранённая цепочка должны показывать одно и то же число, а не
    −10.0 против −9.999999999999998.
    """
    return [round(float(c["eff_pct"]), 6) for c in compute_chain(100.0, 0.0, links, mode)]


def row_weight(row: dict[str, Any], weight: str) -> float | None:
    """Вес строки в агрегатах: 1 или «выпуск шт»; None — строка в деньги не входит."""
    if weight != "volume":
        return 1.0
    v = row.get("volume")
    return v if v is not None and v > 0 else None


# ── Агрегаты (§4.5) и матрица проваливания (§4.7) ────────────────────────────

def aggregate(rows: list[dict[str, Any]], links: list[dict[str, Any]], mode: str, weight: str
              ) -> tuple[dict[str, Any], list[dict[str, Any]], dict[str, Any]]:
    """Суммы по всем строкам с ценой и весом. Проценты — из Σ/Σ, не средние по строкам."""
    n = len(links)
    w_sum = cost_sum = base_sum = 0.0
    link_in = [0.0] * n
    link_out = [0.0] * n
    link_inc = [0.0] * n
    sources = {"pending": 0, "dwh": 0, "audit": 0, "calc": 0, "none": 0}
    rows_priced = rows_without_volume = 0
    for row in rows:
        sources[row["price_source"]] = sources.get(row["price_source"], 0) + 1
        base = row["base"]
        if base is None:
            continue
        rows_priced += 1
        w = row_weight(row, weight)
        if w is None:
            rows_without_volume += 1
            continue
        cost = row["cost"]
        chain = compute_chain(base, cost, links, mode)
        w_sum += w
        cost_sum += w * cost
        base_sum += w * base
        for i, c in enumerate(chain):
            link_in[i] += w * c["in"]
            link_out[i] += w * c["out"]
            link_inc[i] += w * c["income"]

    final_sum = link_out[-1] if n else 0.0
    total_income = final_sum - cost_sum
    link_aggs: list[dict[str, Any]] = []
    for i in range(n):
        link_aggs.append({
            "in_sum": link_in[i],
            "out_sum": link_out[i],
            "income_sum": link_inc[i],
            "income_pct_in": _pct(link_inc[i], link_in[i]),
            "income_pct_out": _pct(link_inc[i], link_out[i]),
            "income_share": _pct(link_inc[i], total_income),
        })
    base_info = {
        "rows": len(rows),
        "rows_priced": rows_priced,
        "rows_without_price": sources["none"],
        "rows_without_volume": rows_without_volume,
        "w_sum": w_sum,
        "cost_sum": cost_sum,
        "base_sum": base_sum,
        "weight": weight,
        "weight_label": WEIGHTS.get(weight, weight),
        "price_sources": sources,
    }
    total = {
        "cost_sum": cost_sum,
        "final_sum": final_sum,
        "income_sum": total_income,
        "margin_pct": _pct(total_income, final_sum),
        "markup_pct": _pct(total_income, cost_sum),
    }
    return base_info, link_aggs, total


def build_matrix(rows: list[dict[str, Any]], links: list[dict[str, Any]], mode: str, weight: str,
                 path: list[str]) -> tuple[list[dict[str, Any]], dict[str, Any]]:
    """Матрица уровня `LEVELS[len(path)]` после наложения пути.

    Значение пути сравнивается с `coalesce(attr, '')` — пустая строка означает
    «уровень не заполнен» (на 23.09.2026 у 31 721 калькуляции нет Level 01), и в
    такую группу тоже можно провалиться. `rows` группы — все её калькуляции,
    деньги — только строки с ценой и весом. Сортировка по доходу группы,
    не больше ROW_LIMIT строк (флаг `truncated`); группы без денег (ни одной
    строки с ценой и весом) — в конец, иначе при обрезке убыточные группы
    вылетали бы раньше пустых.
    """
    path = [_s(p) for p in path][: len(LEVELS) - 1]
    sel = rows
    for (key, _label), value in zip(LEVELS, path):
        sel = [r for r in sel if _s(r.get(key)) == value]
    dim, dim_label = LEVELS[len(path)]
    groups: dict[str, dict[str, Any]] = {}
    for r in sel:
        label = _s(r.get(dim))
        g = groups.get(label)
        if g is None:
            g = groups[label] = {
                "label": label, "name": None, "rows": 0, "w_sum": 0.0,
                "cost": 0.0, "base": 0.0, "final": 0.0, "income": 0.0,
                "links": [{"seq": int(l.get("seq") or i), "out": 0.0, "income": 0.0}
                          for i, l in enumerate(links, start=1)],
            }
        g["rows"] += 1
        if dim in ("model", "articul") and not g["name"] and r.get("model_name"):
            g["name"] = r["model_name"]
        base = r["base"]
        if base is None:
            continue
        w = row_weight(r, weight)
        if w is None:
            continue
        cost = r["cost"]
        chain = compute_chain(base, cost, links, mode)
        g["w_sum"] += w
        g["cost"] += w * cost
        g["base"] += w * base
        if chain:
            g["final"] += w * chain[-1]["out"]
            g["income"] += w * (chain[-1]["out"] - cost)
        for i, c in enumerate(chain):
            g["links"][i]["out"] += w * c["out"]
            g["links"][i]["income"] += w * c["income"]
    matrix = sorted(groups.values(), key=lambda g: (g["w_sum"] > 0, g["income"]), reverse=True)
    truncated = len(matrix) > ROW_LIMIT
    meta = {
        "dim": dim,
        "label": dim_label,
        "path": [{"level": k, "label": lbl, "value": v} for (k, lbl), v in zip(LEVELS, path)],
        "can_drill": len(path) + 1 < len(LEVELS),
        "levels": [{"key": k, "label": lbl} for k, lbl in LEVELS],
        "truncated": truncated,
        "row_limit": ROW_LIMIT,
    }
    return matrix[:ROW_LIMIT], meta


# ── Курсы НБ РБ (§4.6) ───────────────────────────────────────────────────────

_rates_cache: dict[str, tuple[float, dict[str, Any]]] = {}


def _check_rate_date(d: date) -> date:
    """Дата курса из запроса или тела цепочки — в разумных пределах, иначе 400."""
    if d < RATE_DATE_MIN or d > date.today() + timedelta(days=RATE_DATE_AHEAD_DAYS):
        raise HTTPException(400, "rate_date вне допустимого диапазона")
    return d


async def rates_for_links(links: list[dict[str, Any]], rate_date: date | None) -> dict[str, Any] | None:
    """Курсы для валют звеньев на дату. Все звенья в BYN → None, курсы не запрашиваются.

    Один вызов `purchase.rates_for_date` на дату, ответ кэшируется RATES_TTL.
    Валюта без курса на дату — 400: считать в ней нечем, и молча показать BYN
    было бы хуже, чем сказать об этом.
    """
    currencies = sorted({_s(l.get("currency")) or "BYN" for l in links})
    if all(c == "BYN" for c in currencies):
        return None
    d = rate_date or date.today()
    key = d.isoformat()
    cached = _rates_cache.get(key)
    if cached and time.monotonic() - cached[0] < RATES_TTL:
        data = cached[1]
    else:
        if _is_mock():
            data = {"date": key, "source": "mock", "rates": dict(MOCK_RATES), "as_of": {}}
        else:
            try:
                data = await purchase.rates_for_date(d)
            except Exception as exc:  # noqa: BLE001 — DWH за VPN
                raise HTTPException(502, f"курсы НБ РБ недоступны: {exc}")
        now = time.monotonic()
        for k in [k for k, v in _rates_cache.items() if now - v[0] >= RATES_TTL]:
            del _rates_cache[k]
        while len(_rates_cache) >= RATES_CACHE_MAX:
            del _rates_cache[min(_rates_cache, key=lambda k: _rates_cache[k][0])]
        _rates_cache[key] = (now, data)
    rates: dict[str, float] = {"BYN": 1.0}
    as_of: dict[str, str] = {}
    src_rates = data.get("rates") or {}
    src_as_of = data.get("as_of") or {}
    for c in currencies:
        if c == "BYN":
            continue
        r = _f(src_rates.get(c))
        if not r or r <= 0:
            raise HTTPException(400, f"нет курса {c} на {key}")
        rates[c] = r
        if src_as_of.get(c):
            as_of[c] = src_as_of[c]
    return {"date": key, "rates": rates, "as_of": as_of}


# ── Компании ─────────────────────────────────────────────────────────────────

_COMPANY_SQL = """
SELECT c.id, c.name, c.country, c.currency, c.comment, c.is_active, c.sort_order,
       c.created_by, c.created_at, c.updated_by, c.updated_at,
       (SELECT count(DISTINCT l.chain_id) FROM cost_group_chain_link l WHERE l.company_id = c.id) AS used_in_chains
  FROM cost_group_company c
"""


def _company_item(r: Any) -> dict[str, Any]:
    return {
        "id": r["id"],
        "name": r["name"],
        "country": r["country"],
        "currency": r["currency"],
        "comment": r["comment"],
        "is_active": bool(r["is_active"]),
        "sort_order": int(r["sort_order"] or 0),
        "used_in_chains": int(r["used_in_chains"] or 0),
        "created_by": r["created_by"],
        "created_at": _iso(r["created_at"]),
        "updated_by": r["updated_by"],
        "updated_at": _iso(r["updated_at"]),
    }


def _validate_company(data: Any, *, partial: bool) -> dict[str, Any]:
    """Поля компании из тела запроса. partial=True — только присланные (PUT)."""
    if not isinstance(data, dict):
        raise HTTPException(400, "Тело запроса должно быть объектом")
    out: dict[str, Any] = {}
    if "name" in data or not partial:
        name = _s(data.get("name"))
        if not name:
            raise HTTPException(400, "Название компании обязательно")
        if len(name) > 200:
            raise HTTPException(400, "Название компании длиннее 200 символов")
        out["name"] = name
    if "country" in data or not partial:
        country = _s(data.get("country")) or ("BY" if not partial else "")
        if not country:
            raise HTTPException(400, "Код страны не может быть пустым")
        out["country"] = country[:16]
    if "currency" in data or not partial:
        currency = (_s(data.get("currency")) or ("BYN" if not partial else "")).upper()
        if currency not in purchase.CURRENCIES:
            raise HTTPException(400, f"Валюта {currency or '(пусто)'}: допустимы {', '.join(purchase.CURRENCIES)}")
        out["currency"] = currency
    if "comment" in data:
        out["comment"] = _s(data.get("comment"))[:1000] or None
    elif not partial:
        out["comment"] = None
    if "is_active" in data or not partial:
        out["is_active"] = _as_bool(data.get("is_active"), default=True)
    if "sort_order" in data or not partial:
        try:
            sort_order = int(data.get("sort_order") or 0)
        except (TypeError, ValueError, OverflowError):
            raise HTTPException(400, "Порядок (sort_order) должен быть целым числом")
        if not (-2**31 <= sort_order <= 2**31 - 1):  # колонка INTEGER
            raise HTTPException(400, "Порядок (sort_order) вне допустимого диапазона")
        out["sort_order"] = sort_order
    if partial and not out:
        raise HTTPException(400, "Нет полей для изменения")
    return out


async def list_companies() -> list[dict[str, Any]]:
    """Все компании, включая неактивные: неактивные остаются в цепочках и должны быть видны."""
    async with acquire() as conn:
        rows = await conn.fetch(_COMPANY_SQL + " ORDER BY c.sort_order, c.name")
    return [_company_item(r) for r in rows]


async def get_company(company_id: int) -> dict[str, Any] | None:
    if not _id_ok(company_id):
        return None
    async with acquire() as conn:
        r = await conn.fetchrow(_COMPANY_SQL + " WHERE c.id = $1", company_id)
    return _company_item(r) if r else None


async def create_company(data: Any, *, set_by: str) -> dict[str, Any]:
    f = _validate_company(data, partial=False)
    try:
        async with acquire() as conn:
            new_id = await conn.fetchval(
                """INSERT INTO cost_group_company
                       (name, country, currency, comment, is_active, sort_order, created_by, updated_by)
                   VALUES ($1, $2, $3, $4, $5, $6, $7, $7)
                   RETURNING id""",
                f["name"], f["country"], f["currency"], f["comment"], f["is_active"], f["sort_order"], set_by,
            )
    except asyncpg.UniqueViolationError:
        raise HTTPException(409, f"Компания «{f['name']}» уже есть в справочнике")
    except (asyncpg.DataError, asyncpg.CheckViolationError) as exc:
        raise HTTPException(400, f"Компания не сохранена: значение не подходит под схему ({exc})")
    log(logging.INFO, "ценообразование группы: компания создана",
        company_id=new_id, name=f["name"], country=f["country"], currency=f["currency"], user=set_by)
    return await get_company(new_id)  # type: ignore[return-value]


async def update_company(company_id: int, data: Any, *, set_by: str) -> dict[str, Any]:
    f = _validate_company(data, partial=True)
    if not _id_ok(company_id):  # см. _ID_MAX: иначе DataError asyncpg
        raise HTTPException(404, "Компания не найдена")
    sets = [f"{col} = ${i}" for i, col in enumerate(f, start=1)]
    params: list[Any] = list(f.values())
    params.append(set_by)
    sets.append(f"updated_by = ${len(params)}")
    sets.append("updated_at = now()")
    params.append(company_id)
    try:
        async with acquire() as conn:
            found = await conn.fetchval(
                f"UPDATE cost_group_company SET {', '.join(sets)} WHERE id = ${len(params)} RETURNING id",
                *params,
            )
    except asyncpg.UniqueViolationError:
        raise HTTPException(409, f"Компания «{f.get('name')}» уже есть в справочнике")
    except (asyncpg.DataError, asyncpg.CheckViolationError) as exc:
        raise HTTPException(400, f"Компания не сохранена: значение не подходит под схему ({exc})")
    if not found:
        raise HTTPException(404, "Компания не найдена")
    log(logging.INFO, "ценообразование группы: компания изменена",
        company_id=company_id, fields=sorted(f), user=set_by)
    return await get_company(company_id)  # type: ignore[return-value]


async def delete_company(company_id: int, *, set_by: str) -> None:
    """Удалить компанию. Участвует в цепочках — 409: звенья ссылаются на неё
    (FK ON DELETE RESTRICT), а выкидывать её из чужих цепочек молча нельзя —
    пользователь либо правит цепочки, либо снимает флаг «активна».

    Проверка и DELETE — в одной транзакции, а FK-нарушение всё равно ловится:
    между SELECT и DELETE другой пользователь мог сохранить цепочку с этой
    компанией, и тогда ответ должен быть тем же 409, а не 500."""
    if not _id_ok(company_id):  # см. _ID_MAX
        raise HTTPException(404, "Компания не найдена")
    async with acquire() as conn:
        async with conn.transaction():
            name = await conn.fetchval("SELECT name FROM cost_group_company WHERE id = $1", company_id)
            if name is None:
                raise HTTPException(404, "Компания не найдена")
            chains = await conn.fetch(
                """SELECT DISTINCT c.name FROM cost_group_chain c
                     JOIN cost_group_chain_link l ON l.chain_id = c.id
                    WHERE l.company_id = $1 ORDER BY c.name""",
                company_id,
            )
            if chains:
                names = ", ".join(f"«{r['name']}»" for r in chains)
                raise HTTPException(
                    409, f"Компания участвует в цепочках: {names}. Удалите её из цепочек или снимите флаг «активна».")
            try:
                await conn.execute("DELETE FROM cost_group_company WHERE id = $1", company_id)
            except asyncpg.ForeignKeyViolationError:
                raise HTTPException(
                    409, "Компания участвует в цепочках. Удалите её из цепочек или снимите флаг «активна».")
    log(logging.INFO, "ценообразование группы: компания удалена", company_id=company_id, name=name, user=set_by)


# ── Цепочки ──────────────────────────────────────────────────────────────────

_CHAIN_SQL = """
SELECT id, name, mode, weight, scope, rate_date, status, comment,
       created_by, created_at, updated_by, updated_at
  FROM cost_group_chain
"""
_LINKS_SQL = """
SELECT l.id, l.chain_id, l.seq, l.company_id, k.name AS company_name,
       l.adjust_pct, l.currency, l.eff_pct
  FROM cost_group_chain_link l
  JOIN cost_group_company k ON k.id = l.company_id
 WHERE l.chain_id = ANY($1::bigint[])
 ORDER BY l.chain_id, l.seq
"""


def _link_item(r: Any) -> dict[str, Any]:
    return {
        "id": r["id"],
        "seq": int(r["seq"]),
        "company_id": r["company_id"],
        "company_name": r["company_name"],
        "adjust_pct": _f(r["adjust_pct"]) or 0.0,
        "currency": r["currency"],
        "eff_pct": _f(r["eff_pct"]) or 0.0,
    }


def _chain_item(r: Any, links: list[dict[str, Any]]) -> dict[str, Any]:
    scope = r["scope"]
    if isinstance(scope, str):  # asyncpg отдаёт jsonb строкой — как в app/roles.py
        scope = json.loads(scope)
    return {
        "id": r["id"],
        "name": r["name"],
        "mode": r["mode"],
        "weight": r["weight"],
        "scope": scope or {},
        "rate_date": _iso(r["rate_date"]),
        "status": r["status"],
        "comment": r["comment"],
        "created_by": r["created_by"],
        "created_at": _iso(r["created_at"]),
        "updated_by": r["updated_by"],
        "updated_at": _iso(r["updated_at"]),
        "links": links,
    }


async def _fetch_chains(conn: Any, where: str, *params: Any) -> list[dict[str, Any]]:
    rows = await conn.fetch(_CHAIN_SQL + where, *params)
    if not rows:
        return []
    links_by_chain: dict[int, list[dict[str, Any]]] = {r["id"]: [] for r in rows}
    for l in await conn.fetch(_LINKS_SQL, list(links_by_chain)):
        links_by_chain[l["chain_id"]].append(_link_item(l))
    return [_chain_item(r, links_by_chain[r["id"]]) for r in rows]


async def list_chains() -> list[dict[str, Any]]:
    """Все цепочки со звеньями: активные первыми, затем по имени."""
    async with acquire() as conn:
        return await _fetch_chains(conn, " ORDER BY (status <> 'active'), name")


async def get_chain(chain_id: int) -> dict[str, Any] | None:
    if not _id_ok(chain_id):  # см. _ID_MAX: require_chain ответит 404
        return None
    async with acquire() as conn:
        items = await _fetch_chains(conn, " WHERE id = $1", chain_id)
    return items[0] if items else None


async def require_chain(chain_id: int) -> dict[str, Any]:
    chain = await get_chain(chain_id)
    if chain is None:
        raise HTTPException(404, "Цепочка не найдена")
    return chain


def validate_chain(data: Any, *, require_name: bool = True) -> dict[str, Any]:
    """Тело POST/PUT цепочки (и `chain` в /group/simulate) → проверенные поля.

    `seq` звена — его порядок в массиве (1..n), присланный seq игнорируется:
    порядок задаёт пользователь перетаскиванием, а не числом.
    """
    if not isinstance(data, dict):
        raise HTTPException(400, "Тело запроса должно быть объектом цепочки")
    name = _s(data.get("name"))
    if require_name and not name:
        raise HTTPException(400, "Название цепочки обязательно")
    if len(name) > 200:
        raise HTTPException(400, "Название цепочки длиннее 200 символов")
    mode = _s(data.get("mode")) or "cascade"
    if mode not in MODES:
        raise HTTPException(400, f"Режим расчёта (mode): допустимо {' или '.join(MODES)}")
    weight = _s(data.get("weight")) or "unit"
    if weight not in WEIGHTS:
        raise HTTPException(400, f"Вес (weight): допустимо {' или '.join(WEIGHTS)}")
    status = _s(data.get("status")) or "active"
    if status not in STATUSES:
        raise HTTPException(400, f"Статус: допустимо {' или '.join(STATUSES)}")
    rate_date: date | None = None
    if data.get("rate_date") not in (None, ""):
        rate_date = _parse_date(data.get("rate_date"))
        if rate_date is None:
            raise HTTPException(400, "rate_date: дата в формате YYYY-MM-DD")
        _check_rate_date(rate_date)
    scope = normalize_scope(data.get("scope"))
    raw_links = data.get("links")
    if not isinstance(raw_links, list) or not raw_links:
        raise HTTPException(400, "У цепочки должно быть хотя бы одно звено")
    links: list[dict[str, Any]] = []
    for i, raw in enumerate(raw_links, start=1):
        if not isinstance(raw, dict):
            raise HTTPException(400, f"Звено {i}: ожидается объект")
        try:
            company_id = int(raw.get("company_id"))
        except (TypeError, ValueError, OverflowError):
            raise HTTPException(400, f"Звено {i}: не выбрана компания")
        if not (1 <= company_id < 2**63):  # BIGINT; 1e30 из JSON дало бы DataError asyncpg → 500
            raise HTTPException(400, f"Звено {i}: не выбрана компания")
        adjust = _f(raw.get("adjust_pct") if raw.get("adjust_pct") not in (None, "") else 0)
        # Сравниваем то, что реально лягет в NUMERIC(9,4): −99.99995 в колонке
        # станет −100.0000 и упадёт на CHECK, а 99999.99996 — на переполнении.
        adjust = None if adjust is None else round(adjust, 4)
        if adjust is None or adjust <= -100:
            raise HTTPException(400, f"Звено {i}: корректировка должна быть числом больше −100 (скидка не может съесть всю цену)")
        if adjust >= 100000:
            raise HTTPException(400, f"Звено {i}: корректировка должна быть меньше 100 000 %")
        currency = (_s(raw.get("currency")) or "BYN").upper()
        if currency not in purchase.CURRENCIES:
            raise HTTPException(400, f"Звено {i}: валюта {currency} неизвестна, допустимы {', '.join(purchase.CURRENCIES)}")
        links.append({"seq": i, "company_id": company_id, "adjust_pct": adjust, "currency": currency})
    # eff_pct хранится в NUMERIC(14,6): восемь знаков до запятой. Каскад из
    # звеньев по +1000 % набирает их за пару шагов — лучше сказать об этом
    # словами, чем словить переполнение колонки при сохранении.
    if any(not abs(e) < 1e8 for e in eff_pcts(links, mode)):
        raise HTTPException(400, "Слишком большая суммарная наценка: эффективный % к базовой не помещается в 8 разрядов")
    return {
        "name": name, "mode": mode, "weight": weight, "status": status,
        "scope": scope, "rate_date": rate_date,
        "comment": _s(data.get("comment"))[:2000] or None,
        "links": links,
    }


async def _company_names(conn: Any, ids: list[int]) -> dict[int, str]:
    rows = await conn.fetch("SELECT id, name FROM cost_group_company WHERE id = ANY($1::bigint[])", ids)
    names = {r["id"]: r["name"] for r in rows}
    missing = sorted({i for i in ids if i not in names})
    if missing:
        raise HTTPException(400, f"Компании с id {', '.join(map(str, missing))} нет в справочнике")
    return names


async def save_chain(data: Any, *, set_by: str, chain_id: int | None = None) -> dict[str, Any]:
    """Создать (chain_id=None) или переписать цепочку. Звенья заменяются целиком.

    eff_pct звеньев считает сервер при сохранении — так 1С читает готовое число
    из витрины и не знает про режимы.
    """
    f = validate_chain(data)
    if chain_id is not None and not _id_ok(chain_id):  # см. _ID_MAX
        raise HTTPException(404, "Цепочка не найдена")
    effs = eff_pcts(f["links"], f["mode"])
    scope_json = json.dumps(f["scope"], ensure_ascii=False)
    try:
        async with acquire() as conn:
            await _company_names(conn, [l["company_id"] for l in f["links"]])
            async with conn.transaction():
                if chain_id is None:
                    chain_id = await conn.fetchval(
                        """INSERT INTO cost_group_chain
                               (name, mode, weight, scope, rate_date, status, comment, created_by, updated_by)
                           VALUES ($1, $2, $3, $4::jsonb, $5, $6, $7, $8, $8)
                           RETURNING id""",
                        f["name"], f["mode"], f["weight"], scope_json, f["rate_date"], f["status"], f["comment"], set_by,
                    )
                    action = "создана"
                else:
                    found = await conn.fetchval(
                        """UPDATE cost_group_chain
                              SET name = $1, mode = $2, weight = $3, scope = $4::jsonb, rate_date = $5,
                                  status = $6, comment = $7, updated_by = $8, updated_at = now()
                            WHERE id = $9
                            RETURNING id""",
                        f["name"], f["mode"], f["weight"], scope_json, f["rate_date"], f["status"], f["comment"],
                        set_by, chain_id,
                    )
                    if not found:
                        raise HTTPException(404, "Цепочка не найдена")
                    await conn.execute("DELETE FROM cost_group_chain_link WHERE chain_id = $1", chain_id)
                    action = "изменена"
                await conn.executemany(
                    """INSERT INTO cost_group_chain_link (chain_id, seq, company_id, adjust_pct, currency, eff_pct)
                       VALUES ($1, $2, $3, $4, $5, $6)""",
                    [(chain_id, l["seq"], l["company_id"], l["adjust_pct"], l["currency"], e)
                     for l, e in zip(f["links"], effs)],
                )
    except asyncpg.UniqueViolationError:
        raise HTTPException(409, f"Цепочка «{f['name']}» уже есть")
    except asyncpg.ForeignKeyViolationError:
        raise HTTPException(400, "Одна из компаний звеньев удалена из справочника")
    except (asyncpg.DataError, asyncpg.CheckViolationError) as exc:
        # validate_chain держит числа в границах колонок; это страховка на
        # случай расхождения кода и схемы — 400 с текстом вместо 500.
        raise HTTPException(400, f"Цепочка не сохранена: значение не подходит под схему ({exc})")
    _overlap_cache["items"] = None
    log(logging.INFO, f"ценообразование группы: цепочка {action}",
        chain_id=chain_id, name=f["name"], mode=f["mode"], weight=f["weight"], status=f["status"],
        links=[(l["company_id"], l["adjust_pct"], l["currency"]) for l in f["links"]],
        scope=f["scope"], user=set_by)
    return await require_chain(chain_id)  # type: ignore[arg-type]


async def delete_chain(chain_id: int, *, set_by: str) -> None:
    if not _id_ok(chain_id):  # см. _ID_MAX
        raise HTTPException(404, "Цепочка не найдена")
    async with acquire() as conn:
        name = await conn.fetchval("DELETE FROM cost_group_chain WHERE id = $1 RETURNING name", chain_id)
    if name is None:
        raise HTTPException(404, "Цепочка не найдена")
    _overlap_cache["items"] = None
    log(logging.INFO, "ценообразование группы: цепочка удалена", chain_id=chain_id, name=name, user=set_by)


# ── Результат расчёта (§5.1) ─────────────────────────────────────────────────

async def chain_result(chain: dict[str, Any], *, path: list[str] | None = None,
                       rate_date: str | None = None, summary: bool = False) -> dict[str, Any]:
    """Финрез цепочки по её scope: базовые строки → цены → агрегаты → матрица.

    `rate_date` запроса перекрывает дату курса цепочки на этот вызов (поле даты
    в шапке финреза). `summary=True` — без матрицы (плитки в редакторе).
    Если ни одной строки с ценой — суммы 0 и звенья с нулями, без ошибки.
    """
    rd: date | None = None
    if rate_date not in (None, ""):
        rd = _parse_date(rate_date)
        if rd is None:
            raise HTTPException(400, "rate_date: дата в формате YYYY-MM-DD")
        _check_rate_date(rd)
    elif chain.get("rate_date"):
        rd = _parse_date(chain["rate_date"])

    links = chain["links"]
    mode = chain["mode"]
    weight = chain["weight"]

    rows = await base_rows(chain.get("scope") or {})
    overrides, refreshed_at = await price_overrides()
    apply_prices(rows, overrides)
    rate = await rates_for_links(links, rd)

    base_info, link_aggs, total = aggregate(rows, links, mode, weight)
    rates = rate["rates"] if rate else {}
    for agg, link in zip(link_aggs, links):
        cur = _s(link.get("currency")) or "BYN"
        r = float(rates.get(cur, 1.0)) if cur != "BYN" else 1.0
        agg.update({
            "seq": link["seq"],
            "company_id": link["company_id"],
            "company_name": link.get("company_name"),
            "adjust_pct": link["adjust_pct"],
            "eff_pct": link["eff_pct"],
            "currency": cur,
            "rate": r,
            "in_sum_cur": agg["in_sum"] / r,
            "out_sum_cur": agg["out_sum"] / r,
            "income_sum_cur": agg["income_sum"] / r,
        })

    meta: dict[str, Any] = {
        "mode_label": MODES.get(mode, mode),
        "cache_refreshed_at": refreshed_at,
    }
    result: dict[str, Any] = {
        "chain": chain, "rate": rate, "base": base_info, "links": link_aggs, "total": total, "meta": meta,
    }
    matrix, mmeta = build_matrix(rows, links, mode, weight, list(path or []))
    meta.update(mmeta)
    if not summary:
        result["matrix"] = matrix
    return result


async def simulate(chain_data: Any, *, path: list[str] | None = None, summary: bool = False) -> dict[str, Any]:
    """Предпросмотр из редактора: тот же расчёт по несохранённой цепочке."""
    f = validate_chain(chain_data, require_name=False)
    async with acquire() as conn:
        names = await _company_names(conn, [l["company_id"] for l in f["links"]])
    effs = eff_pcts(f["links"], f["mode"])
    item = {
        "id": None, "name": f["name"], "mode": f["mode"], "weight": f["weight"], "scope": f["scope"],
        "rate_date": _iso(f["rate_date"]), "status": f["status"], "comment": f["comment"],
        "created_by": None, "created_at": None, "updated_by": None, "updated_at": None,
        "links": [
            {"id": None, "seq": l["seq"], "company_id": l["company_id"], "company_name": names[l["company_id"]],
             "adjust_pct": l["adjust_pct"], "currency": l["currency"], "eff_pct": e}
            for l, e in zip(f["links"], effs)
        ],
    }
    return await chain_result(item, path=path, summary=summary)


# ── Размер ассортимента и пересечения цепочек ────────────────────────────────

async def scope_count(raw_scope: Any) -> dict[str, int]:
    """Сколько калькуляций, пар модель+артикул и моделей попадает в scope — один
    COUNT-запрос без наложения цен (кнопка «Проверить ассортимент»)."""
    scope = normalize_scope(raw_scope)
    params: list[Any] = []
    where = scope_where(scope, params)
    latest = "d IS NOT DISTINCT FROM dmax" if scope.get("latest_only", True) else "TRUE"
    sql = f"""
        WITH b AS (
            SELECT TRIM("Модель") AS m, TRIM("Артикул") AS a, TRIM("Признак калькуляции") AS cs,
                   TRIM(COALESCE("PLAN_ID", '')) AS pi, "дата расчета" AS d,
                   TRIM(COALESCE("Номер задания производства", '')) AS task
              FROM cost_data_all WHERE {where}
             GROUP BY 1, 2, 3, 4, 5, 6
        ), r AS (SELECT b.*, MAX(d) OVER (PARTITION BY m, a, cs, pi) AS dmax FROM b)
        SELECT count(*) FILTER (WHERE {latest}) AS rows,
               count(DISTINCT (m, a)) AS pairs,
               count(DISTINCT m) AS models
          FROM r"""
    async with acquire() as conn:
        r = await conn.fetchrow(sql, *params)
    return {"rows": int(r["rows"] or 0), "pairs": int(r["pairs"] or 0), "models": int(r["models"] or 0)}


_overlap_cache: dict[str, Any] = {"at": 0.0, "items": None}


async def chain_overlaps() -> list[dict[str, Any]]:
    """Попарные пересечения множеств (модель, артикул) активных цепочек.

    Заказчик разрешил пересечения (23.09.2026) — их только подсвечивают, поэтому
    здесь ничего не запрещается, только считается. На цепочку один
    SELECT DISTINCT по её scope, пересечения — в Python. Каждая пара отдаётся в
    обе стороны, чтобы карточка цепочки находила свои пересечения по chain_id.
    Кэш OVERLAPS_TTL, сбрасывается при сохранении/удалении цепочки.
    """
    now = time.monotonic()
    if _overlap_cache["items"] is not None and now - _overlap_cache["at"] < OVERLAPS_TTL:
        return _overlap_cache["items"]
    chains = [c for c in await list_chains() if c["status"] == "active"]
    pairs: dict[int, set[tuple[str, str]]] = {}
    if len(chains) > 1:
        async with acquire() as conn:
            for c in chains:
                params: list[Any] = []
                where = scope_where(c["scope"], params)
                rows = await conn.fetch(
                    f'SELECT DISTINCT TRIM("Модель") AS m, TRIM("Артикул") AS a FROM cost_data_all WHERE {where}',
                    *params,
                )
                pairs[c["id"]] = {(r["m"], r["a"]) for r in rows}
    items: list[dict[str, Any]] = []
    for i, c1 in enumerate(chains):
        for c2 in chains[i + 1:]:
            shared = len(pairs.get(c1["id"], set()) & pairs.get(c2["id"], set()))
            if not shared:
                continue
            items.append({"chain_id": c1["id"], "other_chain_id": c2["id"],
                          "other_chain_name": c2["name"], "shared_pairs": shared})
            items.append({"chain_id": c2["id"], "other_chain_id": c1["id"],
                          "other_chain_name": c1["name"], "shared_pairs": shared})
    _overlap_cache.update({"at": time.monotonic(), "items": items})
    return items
