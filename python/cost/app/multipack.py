"""Мультипаки: себестоимость пака = Σ калькуляций-одиночек + упаковка.

Мультипак — модель-артикул, который физически содержит 3/5/7 пар, а продаётся
как ОДНА единица (группа «Носки&Колготки»). Его слот в источнике есть, но пустой:
на 02.09.2026 модель 430A-3839 / артикул B3-263430A «НОСКИ ДЕТСКИЕ (3 пары)»
несёт две строки типа «шт» с нулевой суммой. Наполняем этот слот сами.

ЧТО ЗДЕСЬ ЕСТЬ, А ЧЕГО НЕТ
=========================
Здесь — состав пака (какие одиночки, по сколько штук) и ГЕНЕРАЦИЯ строк расчёта.
Сохранения строк здесь нет: собранные строки уходят в редактор расчёта и
сохраняются штатным create_version / save_version_draft. Оттуда бесплатно
приходит всё остальное — цена, согласование ПЭО, запись в DWH, блокировки и
переживание refresh кэша (_reapply_active_versions_to_cache переприменяет
pending/approved версии после каждого реимпорта из MSSQL).

СВЁРТКА ПО СТАТЬЯМ, А НЕ ПЛОСКОЕ КОПИРОВАНИЕ
============================================
Одиночка носков — от 20 до 145 строк детализации. Копировать их в пак значило бы
получить 300+ нечитаемых строк на пак из трёх пар. Вместо этого на каждую
одиночку генерируется по одной строке НА СТАТЬЮ (основные материалы,
вспомогательные, пошив, раскрой, вязание, декоры), и вкладывается она в
существующую арифметику раздела как обычная материальная строка:

    Норма = количество штук в паке, цена материала = себестоимость статьи за штуку

_recalc_cost_buckets (app/db.py) разложит произведение по нужным статьям сама, и
структура себестоимости пака остаётся честной: видно, сколько в паке вязания,
сколько материалов, сколько упаковки.

КОЭФФИЦИЕНТ ИСТОЧНИКА У СГЕНЕРИРОВАННЫХ СТРОК — ВСЕГДА NULL
===========================================================
cost_factor_rub/usd (миграция 0031) поглощает расхождение «Норма × цена ≠
стоимость» в источнике: у четверти строк там коэффициент далёк от единицы
(25-й процентиль по носкам — 0,000241). Для наших строк произведение ТОЧНО равно
стоимости, поэтому коэффициент None — то же, что у операционных строк после
_derive_norm_price. Если бы он утёк из строки-шаблона, введённая себестоимость
умножилась бы на 0,0002 и статья молча обнулилась.

КАК ОТЛИЧИТЬ СТРОКУ СОСТАВА ОТ УПАКОВКИ
=======================================
По «свойство3» = MARKER. Упаковку калькулятор добавляет руками обычной кнопкой
«+ Добавить строку», и пересборка не должна её терять: пересобираются только
строки с маркером, остальные переносятся как есть.

ВЫБОР ЗАДАНИЯ У ОДИНОЧКИ
========================
У одиночки бывает несколько заданий производства с РАЗНОЙ себестоимостью (модель
430A-2848: 1,0067 / 0,9568 / 0,9566 / 0,9298 руб). Берём задание с максимальной
рублёвой себестоимостью — то же правило, что уже действует при простановке цены
(get_max_calc_cost, решение заказчика 26.08.2026: цена, оправданная для самого
дорогого задания, оправдана и для остальных). Сколько заданий было, интерфейс
показывает — молчаливого выбора здесь нет.
"""

from __future__ import annotations

import json
import logging
from typing import Any

from app.db import CACHE_COLUMNS, acquire
from app.logship import log

# Статья себестоимости → тип строки в дропдауне редактора расчёта.
# Порядок задаёт порядок строк внутри одной одиночки.
BUCKET_SPECS: tuple[tuple[str, str], ...] = (
    ("Основные материалы", "Материал основной"),
    ("Вспомогательные материалы", "Материал вспомогательный"),
    ("Пошив", "Пошив"),
    ("Раскрой", "Раскрой"),
    ("Вязание", "Вязание"),
    ("Декоры", "Декор"),
)

# У декоров нормы и цены материала в источнике нет вообще: их стоимость задаётся
# суммой в «Декоры, руб./USD.» напрямую, и _recalc_cost_buckets произведение для
# них НЕ считает (_PLAN_DECOR_TYPES в app/db.py). Поэтому строка декора в паке
# формируется иначе: норма и цена пустые, сумма посчитана здесь.
DECOR_BUCKET = "Декоры"

# Маркер строки, сгенерированной из состава пака. Лежит в «свойство3» — поле
# видно в редакторе, и по нему пересборка отличает свои строки от упаковки.
MARKER = "мультипак"

_ALL_BUCKETS = tuple(b for b, _ in BUCKET_SPECS)


def _n(v) -> float:
    try:
        return float(v)
    except (TypeError, ValueError):
        return 0.0


def _key(v) -> str:
    return (v or "").strip()


def is_component_row(row: dict) -> bool:
    """Строка сгенерирована из состава пака (а не добавлена руками)."""
    return _key(row.get("свойство3")) == MARKER


# ---------------------------------------------------------------------------
# Чтение себестоимости одиночек
# ---------------------------------------------------------------------------

def _bucket_sum_expr(alias: str, cur: str) -> str:
    """Σ всех статей себестоимости строки в одной валюте («руб.» / «USD.»)."""
    return " + ".join(f'COALESCE({alias}."{b}, {cur}", 0)' for b in _ALL_BUCKETS)


# Суммы по каждой статье отдельно, с алиасом «статья|валюта».
_BUCKET_SUMS_SQL = ",\n".join(
    f'           SUM(COALESCE(c."{b}, {cur}", 0)) AS "{b}|{cur}"'
    for b in _ALL_BUCKETS
    for cur in ("руб.", "USD.")
)

# Итог по уже посчитанным алиасам статей (для внешнего SELECT).
_TOTAL_RUB_EXPR = " + ".join(f'COALESCE("{b}|руб.", 0)' for b in _ALL_BUCKETS)
_TOTAL_USD_EXPR = " + ".join(f'COALESCE("{b}|USD.", 0)' for b in _ALL_BUCKETS)


async def _fetch_unit_costs(
    conn, keys: list[tuple[str, str, str, str]]
) -> dict[tuple[str, str, str, str], dict]:
    """Себестоимость одиночек по статьям. Ключ запроса — (модель, артикул,
    признак, план), где признак и план МОГУТ быть пустыми — «любой».

    Читаем из cost_data_all — то есть С УЧЁТОМ версий калькуляций и применённых
    наборов цен по плану: активная версия уже наложена на кэш, отдельно её
    подмешивать не нужно.

    ПОЧЕМУ ПЛАН — «ЛЮБОЙ», А НЕ ПЛАН ПАКА
    =====================================
    Проверено на живых данных 02.09.2026: у пака 430A-3839/B3-263430A
    PLAN_ID = 0, а у его одиночек (430A-2848/B2-124430A) — 6762, 6982, 7275,
    7773, по одному заданию в каждом. То есть план пака и план одиночки в общем
    случае РАЗНЫЕ, и «брать одиночку из плана пака» не нашло бы ничего. Поэтому
    пустой план в составе означает «любой», а конкретный — фиксацию, если
    калькулятор хочет считать именно по этому плану.

    КАКУЮ КАЛЬКУЛЯЦИЮ ВЫБИРАЕМ, ЕСЛИ ИХ НЕСКОЛЬКО
    =============================================
    Сначала самый свежий план (max «дата расчета» внутри плана) — устаревшая
    себестоимость хуже дорогой. Внутри плана — задание с максимальной рублёвой
    себестоимостью: то же правило, что при простановке цены (get_max_calc_cost,
    решение заказчика 26.08.2026). Рублёвые и долларовые суммы берём из ОДНОГО
    задания, иначе пара «руб/USD» перестала бы соответствовать курсу.
    """
    if not keys:
        return {}
    uniq = list({(_key(m), _key(a), _key(cs), _key(p)) for m, a, cs, p in keys})
    rows = await conn.fetch(
        f"""
        WITH req AS (
            SELECT * FROM unnest($1::text[], $2::text[], $3::text[], $4::text[])
                 AS r(m, a, cs, pid)
        ), per_task AS (
            SELECT r.m, r.a, r.cs, r.pid,
                   trim(COALESCE(c."Признак калькуляции", '')) AS act_cs,
                   trim(COALESCE(c."PLAN_ID", ''))             AS act_pid,
                   trim(COALESCE(c."Номер задания производства", '')) AS task,
                   max(c."дата расчета")            AS calc_date,
                   max(c."Курс на дату расчета")    AS fx_rate,
                   max(c."Наименование модели")     AS model_name,
                   max(c."Level 01")                AS level01,
                   max(c."Ставка НДС")              AS vat_rate,
                   count(*)                         AS src_rows,
{_BUCKET_SUMS_SQL}
              FROM req r
              JOIN cost_data_all c
                ON trim(c."Модель") = r.m
               AND trim(c."Артикул") = r.a
               AND (r.cs = '' OR trim(COALESCE(c."Признак калькуляции", '')) = r.cs)
               AND (r.pid = '' OR trim(COALESCE(c."PLAN_ID", '')) = r.pid)
             GROUP BY 1, 2, 3, 4, 5, 6, 7
        ), per_plan AS (
            -- Свежесть плана: планы сравниваем по последней дате расчёта внутри
            -- плана, а не задания между собой. Заодно считаем, сколько заданий
            -- в плане и сколько планов у одиночки — оконная функция поверх
            -- GROUP BY считается уже по свёрнутым строкам, то есть по планам.
            SELECT m, a, cs, pid, act_cs, act_pid,
                   max(calc_date)                             AS plan_date,
                   count(*)                                   AS tasks_total,
                   count(*) OVER (PARTITION BY m, a, cs, pid)  AS plans_total
              FROM per_task
             GROUP BY 1, 2, 3, 4, 5, 6
        ), ranked AS (
            SELECT t.*,
                   ({_TOTAL_RUB_EXPR}) AS total_rub,
                   ({_TOTAL_USD_EXPR}) AS total_usd,
                   p.plan_date, p.tasks_total, p.plans_total
              FROM per_task t
              JOIN per_plan p
                ON p.m = t.m AND p.a = t.a AND p.cs = t.cs AND p.pid = t.pid
               AND p.act_cs = t.act_cs AND p.act_pid = t.act_pid
        )
        SELECT DISTINCT ON (m, a, cs, pid) *
          FROM ranked
         ORDER BY m, a, cs, pid, plan_date DESC, total_rub DESC, task
        """,
        [k[0] for k in uniq], [k[1] for k in uniq],
        [k[2] for k in uniq], [k[3] for k in uniq],
    )
    out: dict[tuple[str, str, str, str], dict] = {}
    for r in rows:
        buckets = {
            b: {"rub": _n(r[f"{b}|руб."]), "usd": _n(r[f"{b}|USD."])}
            for b in _ALL_BUCKETS
        }
        out[(r["m"], r["a"], r["cs"], r["pid"])] = {
            # Что запрос НАШЁЛ (в отличие от того, что просили): интерфейс
            # показывает это как «взято из …», иначе выбор был бы молчаливым.
            "actual_calc_sign": r["act_cs"],
            "actual_plan_id": r["act_pid"],
            "task": r["task"],
            "tasks_total": int(r["tasks_total"] or 1),
            "plans_total": int(r["plans_total"] or 1),
            "calc_date": r["calc_date"],
            "fx_rate": _n(r["fx_rate"]),
            "model_name": r["model_name"],
            "level01": r["level01"],
            "vat_rate": _n(r["vat_rate"]),
            "src_rows": int(r["src_rows"] or 0),
            "buckets": buckets,
            "total_rub": _n(r["total_rub"]),
            "total_usd": _n(r["total_usd"]),
        }
    return out


async def _pack_template_row(
    conn, model: str, articul: str, calc_sign: str, plan_id: str, task_number: str
) -> dict | None:
    """Строка-шаблон самого пака из кэша: откуда наследуются атрибуты изделия.

    Берём строку актуальной («дата расчета» = max) калькуляции пака. Из неё
    сгенерированные строки получают модель, артикул, признак, план, задание,
    даты, уровни, цены, НДС и курс — то есть всё, что описывает изделие, а не
    материал. Без шаблона строки состава не прошли бы фильтры раздела.
    """
    task = _key(task_number)
    task_filter = "" if task == "" else ' AND trim(COALESCE("Номер задания производства", \'\')) = $5'
    params: list[Any] = [_key(model), _key(articul), _key(calc_sign), _key(plan_id)]
    if task != "":
        params.append(task)
    col_list = ", ".join(f'"{c}"' for c in CACHE_COLUMNS)
    row = await conn.fetchrow(
        f"""SELECT {col_list} FROM cost_data_all
             WHERE trim("Модель") = $1 AND trim("Артикул") = $2
               AND trim(COALESCE("Признак калькуляции", '')) = $3
               AND trim(COALESCE("PLAN_ID", '')) = $4{task_filter}
             ORDER BY "дата расчета" DESC, id
             LIMIT 1""",
        *params,
    )
    return dict(row) if row is not None else None


# ---------------------------------------------------------------------------
# Состав пака
# ---------------------------------------------------------------------------


async def _load_pack(conn, model: str, articul: str) -> dict | None:
    pack = await conn.fetchrow(
        """SELECT id, model, articul, pack_size, note, created_by, created_at,
                  updated_by, updated_at
             FROM cost_multipack
            WHERE model = $1 AND articul = $2""",
        _key(model), _key(articul),
    )
    if pack is None:
        return None
    items = await conn.fetch(
        """SELECT id, src_model, src_articul, src_calc_sign, src_plan_id, qty, position
             FROM cost_multipack_item
            WHERE multipack_id = $1
            ORDER BY position, id""",
        pack["id"],
    )
    return {**dict(pack), "items": [dict(i) for i in items]}


async def _last_build(
    conn, pack_id: int, calc_sign: str, plan_id: str, task_number: str
) -> dict | None:
    row = await conn.fetchrow(
        """SELECT id, version_id, items, items_rub, items_usd, packaging_rub,
                  packaging_usd, total_rub, total_usd, built_by, built_at
             FROM cost_multipack_build
            WHERE multipack_id = $1 AND calc_sign = $2 AND plan_id = $3
              AND task_number = $4
            ORDER BY built_at DESC
            LIMIT 1""",
        pack_id, _key(calc_sign), _key(plan_id), _key(task_number),
    )
    if row is None:
        return None
    out = dict(row)
    if isinstance(out.get("items"), str):
        out["items"] = json.loads(out["items"])
    # NUMERIC приезжает Decimal'ом, и в JSON уходит хвост вида
    # 2.944625999999999965694996717502363 — в интерфейсе это выглядит поломкой.
    for money in ("items_rub", "items_usd", "packaging_rub", "packaging_usd",
                  "total_rub", "total_usd"):
        if out.get(money) is not None:
            out[money] = round(float(out[money]), 6)
    return out


async def attach_version(
    *, model: str, articul: str, calc_sign: str = "", plan_id: str = "",
    task_number: str = "", version_id: int,
) -> None:
    """Привязать созданную версию к последней сборке пака.

    Зачем: журнал сборок отвечает на вопрос «из чего сложилась ЭТА версия
    калькуляции». Без ссылки на версию журнал показывает только «когда
    собирали», и связать сборку с согласованными цифрами нельзя.

    Вызывается после create_version и молчит, если пак не заведён или сборки
    ещё не было: сохранение обычной калькуляции не должно падать из-за этого.
    """
    async with acquire() as conn:
        pack_id = await conn.fetchval(
            "SELECT id FROM cost_multipack WHERE model = $1 AND articul = $2",
            _key(model), _key(articul),
        )
        if pack_id is None:
            return
        await conn.execute(
            """UPDATE cost_multipack_build SET version_id = $2
                WHERE id = (
                      SELECT id FROM cost_multipack_build
                       WHERE multipack_id = $1 AND calc_sign = $3
                         AND plan_id = $4 AND task_number = $5
                       ORDER BY built_at DESC LIMIT 1
                )""",
            pack_id, version_id, _key(calc_sign), _key(plan_id), _key(task_number),
        )


def _resolve_key(item: dict, calc_sign: str, plan_id: str) -> tuple[str, str, str, str]:
    """Ключ поиска калькуляции одиночки.

    Признак: пустой в составе → берём этап ПАКА. Этапы у пака и его одиночек
    должны совпадать (ПКПСС собирается из ПКПСС), и так один состав работает на
    всех этапах без правки.

    План: пустой в составе → остаётся ПУСТЫМ, то есть «любой». План пака и план
    одиночки в данных различаются (у пака 0, у одиночек 6762/7275/…), поэтому
    подставлять сюда план пака нельзя — не нашлось бы ничего. Выбор конкретной
    калькуляции при пустом плане описан в _fetch_unit_costs.
    """
    return (
        _key(item.get("src_model")),
        _key(item.get("src_articul")),
        _key(item.get("src_calc_sign")) or _key(calc_sign),
        _key(item.get("src_plan_id")),
    )


async def _resolve_items(
    conn, items: list[dict], calc_sign: str, plan_id: str
) -> list[dict]:
    """Состав + актуальная себестоимость каждой одиночки по статьям."""
    keys = [_resolve_key(it, calc_sign, plan_id) for it in items]
    costs = await _fetch_unit_costs(conn, keys)
    resolved: list[dict] = []
    for it, key in zip(items, keys):
        qty = _n(it.get("qty"))
        cost = costs.get(key)
        entry: dict[str, Any] = {
            "id": it.get("id"),
            "src_model": key[0],
            "src_articul": key[1],
            "src_calc_sign": key[2],
            "src_plan_id": key[3],
            # Пустое поле в составе = «следовать контексту». Интерфейсу нужно
            # знать, зафиксирован ли источник, чтобы показать это пользователем.
            "pinned_calc_sign": bool(_key(it.get("src_calc_sign"))),
            "pinned_plan_id": bool(_key(it.get("src_plan_id"))),
            "qty": qty,
            "position": it.get("position", 0),
            "found": cost is not None,
        }
        if cost is None:
            entry.update({
                "buckets": {b: {"rub": 0.0, "usd": 0.0} for b in _ALL_BUCKETS},
                "unit_rub": 0.0, "unit_usd": 0.0,
                "sum_rub": 0.0, "sum_usd": 0.0,
            })
        else:
            entry.update({
                "model_name": cost["model_name"],
                "level01": cost["level01"],
                "calc_date": cost["calc_date"],
                "fx_rate": cost["fx_rate"],
                "actual_calc_sign": cost["actual_calc_sign"],
                "actual_plan_id": cost["actual_plan_id"],
                "task": cost["task"],
                "tasks_total": cost["tasks_total"],
                "plans_total": cost["plans_total"],
                "src_rows": cost["src_rows"],
                "buckets": cost["buckets"],
                "unit_rub": cost["total_rub"],
                "unit_usd": cost["total_usd"],
                "sum_rub": round(cost["total_rub"] * qty, 6),
                "sum_usd": round(cost["total_usd"] * qty, 6),
            })
        resolved.append(entry)
    return resolved


def _stale_diff(last_build: dict | None, resolved: list[dict]) -> list[dict]:
    """Позиции, у которых себестоимость одиночки изменилась после сборки.

    Автопересбора намеренно нет: он молча менял бы уже согласованные цифры.
    Здесь только факт расхождения — пересобирает человек кнопкой.
    """
    if not last_build:
        return []
    snap = {
        (s.get("src_model"), s.get("src_articul"), s.get("src_calc_sign"), s.get("src_plan_id")): s
        for s in (last_build.get("items") or [])
    }
    diffs = []
    for r in resolved:
        key = (r["src_model"], r["src_articul"], r["src_calc_sign"], r["src_plan_id"])
        old = snap.get(key)
        if old is None:
            continue
        was = _n(old.get("unit_rub"))
        now = _n(r.get("unit_rub"))
        # Копейка на штуку — уже расхождение: пак умножает её на количество.
        if abs(was - now) > 0.005:
            diffs.append({
                "src_model": r["src_model"],
                "src_articul": r["src_articul"],
                "was_unit_rub": was,
                "now_unit_rub": now,
            })
    return diffs


async def get_state(
    model: str, articul: str, calc_sign: str = "", plan_id: str = "",
    task_number: str = "",
) -> dict:
    """Состояние мультипака для редактора расчёта.

    Отдаётся и когда пака нет (is_pack=false) — интерфейс по этому ответу решает,
    показывать блок состава или предложить «сделать мультипаком».
    """
    async with acquire() as conn:
        pack = await _load_pack(conn, model, articul)
        if pack is None:
            return {
                "is_pack": False, "model": _key(model), "articul": _key(articul),
                "items": [], "warnings": [],
            }
        resolved = await _resolve_items(conn, pack["items"], calc_sign, plan_id)
        last = await _last_build(conn, pack["id"], calc_sign, plan_id, task_number)
        template = await _pack_template_row(
            conn, model, articul, calc_sign, plan_id, task_number
        )
    return _state_payload(pack, resolved, last, template, calc_sign)


def _state_payload(
    pack: dict, resolved: list[dict], last: dict | None, template: dict | None,
    calc_sign: str,
) -> dict:
    items_rub = round(sum(r["sum_rub"] for r in resolved), 6)
    items_usd = round(sum(r["sum_usd"] for r in resolved), 6)
    qty_total = sum(_n(r["qty"]) for r in resolved)
    warnings: list[str] = []

    missing = [f'{r["src_model"]} / {r["src_articul"]}' for r in resolved if not r["found"]]
    if missing:
        warnings.append(
            "Не найдены калькуляции одиночек: " + ", ".join(missing)
            + ". Их стоимость в пак не попала."
        )
    if template is None:
        warnings.append(
            "Калькуляция самого пака не найдена в данных — собрать строки не получится."
        )
    if pack.get("pack_size") is not None and _n(pack["pack_size"]) != qty_total:
        warnings.append(
            f'Состав даёт {qty_total:g} шт, а в паке по паспорту {_n(pack["pack_size"]):g} шт.'
        )
    other_sign = sorted({
        r.get("actual_calc_sign") or "" for r in resolved
        if r["found"] and _key(calc_sign)
        and (r.get("actual_calc_sign") or "") != _key(calc_sign)
    })
    if other_sign:
        warnings.append(
            "Одиночки взяты с другого этапа калькуляции: " + ", ".join(other_sign)
            + f" (пак — {_key(calc_sign) or '—'})."
        )
    # План у пака и одиночек в данных различается, поэтому пустой план в составе
    # значит «любой». Но если планов у одиночки несколько, выбор сделан за
    # человека — надо сказать, какой именно взят.
    multi_plan = [
        f'{r["src_model"]} / {r["src_articul"]} → план {r.get("actual_plan_id") or "—"}'
        for r in resolved
        if r["found"] and not r["pinned_plan_id"] and int(r.get("plans_total") or 1) > 1
    ]
    if multi_plan:
        warnings.append(
            "У одиночек несколько планов, взят самый свежий: " + "; ".join(multi_plan)
            + ". Нужен другой — зафиксируйте план в составе."
        )
    # Курс сравниваем с курсом пака: при расхождении рублёвая и долларовая
    # себестоимости пака перестают соответствовать друг другу по курсу. Суммы
    # при этом НЕ пересчитываем — обе цифры родные, из одиночек.
    if template is not None:
        pack_fx = _n(template.get("Курс на дату расчета"))
        odd_fx = sorted({
            round(_n(r.get("fx_rate")), 4) for r in resolved
            if r["found"] and pack_fx and abs(_n(r.get("fx_rate")) - pack_fx) > 0.0001
        })
        if odd_fx:
            warnings.append(
                f"Курс у одиночек ({', '.join(str(x) for x in odd_fx)}) отличается от курса пака "
                f"({pack_fx:g}): USD-себестоимость пака не равна рублёвой, делённой на курс пака."
            )

    return {
        "is_pack": True,
        "pack_id": pack["id"],
        "model": pack["model"],
        "articul": pack["articul"],
        "pack_size": _n(pack["pack_size"]) if pack.get("pack_size") is not None else None,
        "note": pack.get("note"),
        "created_by": pack.get("created_by"),
        "updated_by": pack.get("updated_by"),
        "updated_at": pack.get("updated_at"),
        "items": resolved,
        "qty_total": qty_total,
        "items_rub": items_rub,
        "items_usd": items_usd,
        "last_build": last,
        "stale": _stale_diff(last, resolved),
        "warnings": warnings,
    }


async def save_pack(
    *, model: str, articul: str, items: list[dict], pack_size=None,
    note: str = "", username: str,
) -> dict:
    """Создать или переписать состав пака.

    Состав переписывается целиком: сверять построчный diff незачем — состав
    короткий, а UNIQUE по (пак, одиночка) не даёт продублировать позицию.
    """
    model, articul = _key(model), _key(articul)
    if not model or not articul:
        raise ValueError("не указаны модель и артикул мультипака")

    cleaned: list[dict] = []
    seen: set[tuple[str, str, str, str]] = set()
    for pos, it in enumerate(items or []):
        src_model = _key(it.get("src_model"))
        src_articul = _key(it.get("src_articul"))
        if not src_model or not src_articul:
            raise ValueError("у позиции состава не заданы модель и артикул одиночки")
        if src_model == model and src_articul == articul:
            raise ValueError("мультипак нельзя включить в собственный состав")
        qty = _n(it.get("qty"))
        if qty <= 0:
            raise ValueError(f"количество у {src_model} / {src_articul} должно быть больше нуля")
        key = (src_model, src_articul, _key(it.get("src_calc_sign")), _key(it.get("src_plan_id")))
        if key in seen:
            raise ValueError(
                f"{src_model} / {src_articul} указана в составе дважды — задайте количество"
            )
        seen.add(key)
        cleaned.append({
            "src_model": src_model,
            "src_articul": src_articul,
            "src_calc_sign": key[2],
            "src_plan_id": key[3],
            "qty": qty,
            "position": pos,
        })

    async with acquire() as conn:
        async with conn.transaction():
            # Мультипак в мультипаке запрещён (решение заказчика 02.09.2026):
            # иначе себестоимость двоилась бы и в паке, и в его паке.
            if cleaned:
                nested = await conn.fetch(
                    """SELECT model, articul FROM cost_multipack
                        WHERE (model, articul) IN (
                              SELECT * FROM unnest($1::text[], $2::text[])
                        )""",
                    [c["src_model"] for c in cleaned],
                    [c["src_articul"] for c in cleaned],
                )
                if nested:
                    listed = ", ".join(f'{r["model"]} / {r["articul"]}' for r in nested)
                    raise ValueError(
                        f"в состав нельзя добавить другой мультипак: {listed}"
                    )

            pack_id = await conn.fetchval(
                """INSERT INTO cost_multipack (model, articul, pack_size, note, created_by)
                   VALUES ($1, $2, $3, $4, $5)
                   ON CONFLICT (model, articul) DO UPDATE
                      SET pack_size = EXCLUDED.pack_size,
                          note = EXCLUDED.note,
                          updated_by = EXCLUDED.created_by,
                          updated_at = now()
                   RETURNING id""",
                model, articul,
                None if pack_size in (None, "") else _n(pack_size),
                note or None, username,
            )
            await conn.execute(
                "DELETE FROM cost_multipack_item WHERE multipack_id = $1", pack_id
            )
            for c in cleaned:
                await conn.execute(
                    """INSERT INTO cost_multipack_item
                       (multipack_id, src_model, src_articul, src_calc_sign,
                        src_plan_id, qty, position)
                       VALUES ($1, $2, $3, $4, $5, $6, $7)""",
                    pack_id, c["src_model"], c["src_articul"],
                    c["src_calc_sign"], c["src_plan_id"], c["qty"], c["position"],
                )
    log(logging.INFO, "мультипак: состав сохранён",
        model=model, articul=articul, items=len(cleaned), user=username)
    return {"pack_id": pack_id, "items": len(cleaned)}


async def delete_pack(model: str, articul: str, username: str) -> dict:
    """Распустить пак: убрать состав и журнал сборок.

    Строки расчёта при этом остаются в версии калькуляции — распустить состав и
    молча обнулить уже согласованную себестоимость нельзя. Строки убираются
    следующим сохранением версии из редактора.
    """
    async with acquire() as conn:
        deleted = await conn.fetchval(
            "DELETE FROM cost_multipack WHERE model = $1 AND articul = $2 RETURNING id",
            _key(model), _key(articul),
        )
    if deleted is None:
        raise ValueError("мультипак не найден")
    log(logging.INFO, "мультипак: состав удалён",
        model=_key(model), articul=_key(articul), user=username)
    return {"deleted": True}


# ---------------------------------------------------------------------------
# Сборка строк расчёта
# ---------------------------------------------------------------------------


def _component_row(template: dict, item: dict, bucket: str, row_type: str) -> dict | None:
    """Одна строка расчёта: статья bucket одной одиночки, умноженная на qty."""
    per_unit = item["buckets"].get(bucket) or {"rub": 0.0, "usd": 0.0}
    unit_rub, unit_usd = _n(per_unit.get("rub")), _n(per_unit.get("usd"))
    if unit_rub == 0 and unit_usd == 0:
        return None  # статьи у этой одиночки нет — пустую строку не плодим
    qty = _n(item["qty"])
    row = dict(template)
    row["Материал/операция/декор(призн)"] = row_type
    row["Наименование"] = f'{item["src_model"]} / {item["src_articul"]} · {bucket.lower()}'
    row["артикул материала"] = item["src_articul"]
    row["свойство1"] = item["src_model"]
    # Из какой именно калькуляции одиночки взята стоимость: этап и план. Не
    # украшение — без этого в согласованной версии не видно, что сложилось.
    row["свойство2"] = "/".join(filter(None, (
        item.get("actual_calc_sign") or item.get("src_calc_sign") or "",
        item.get("actual_plan_id") or "",
    )))
    row["свойство3"] = MARKER
    # Коэффициент источника — только None, см. модульный docstring.
    row["cost_factor_rub"] = None
    row["cost_factor_usd"] = None
    row["Декоры, наименование"] = None
    for b in _ALL_BUCKETS:
        row[f"{b}, руб."] = 0
        row[f"{b}, USD."] = 0
    row["Пошив, минуты"] = None
    row["Раскрой, минуты"] = None

    sum_rub = round(unit_rub * qty, 6)
    sum_usd = round(unit_usd * qty, 6)
    if bucket == DECOR_BUCKET:
        # Декор: норма и цена не участвуют, стоимость задаётся суммой.
        row["Норма"] = None
        row["цена материала, руб."] = None
        row["цена материала, USD."] = None
    else:
        row["Норма"] = qty
        row["цена материала, руб."] = unit_rub
        row["цена материала, USD."] = unit_usd
    row[f"{bucket}, руб."] = sum_rub
    row[f"{bucket}, USD."] = sum_usd
    row["change_type"] = "added"
    row["row_comment"] = None
    return row


def build_rows(template: dict, resolved: list[dict], keep_rows: list[dict]) -> dict:
    """Строки расчёта пака: свёртки состава + перенесённые строки упаковки.

    keep_rows — строки из редактора, которые надо сохранить: всё, что не несёт
    маркера состава. Строки самого источника (две пустышки типа «шт» с нулевой
    суммой у пака) в keep не попадают — их отбрасываем, иначе в детализации
    останутся пустые декоры.
    """
    kept: list[dict] = []
    for r in keep_rows or []:
        if is_component_row(r):
            continue
        # Пустышки источника: ни нормы, ни цены, ни суммы.
        has_money = any(
            _n(r.get(f"{b}, руб.")) or _n(r.get(f"{b}, USD.")) for b in _ALL_BUCKETS
        )
        has_input = _n(r.get("Норма")) or _n(r.get("цена материала, руб."))
        if not has_money and not has_input and not _key(r.get("Наименование")):
            continue
        kept.append(r)

    generated: list[dict] = []
    for item in resolved:
        if not item.get("found"):
            continue
        for bucket, row_type in BUCKET_SPECS:
            row = _component_row(template, item, bucket, row_type)
            if row is not None:
                generated.append(row)

    rows = generated + kept
    items_rub = round(sum(
        sum(_n(r.get(f"{b}, руб.")) for b in _ALL_BUCKETS) for r in generated
    ), 6)
    items_usd = round(sum(
        sum(_n(r.get(f"{b}, USD.")) for b in _ALL_BUCKETS) for r in generated
    ), 6)
    pack_rub = round(sum(
        sum(_n(r.get(f"{b}, руб.")) for b in _ALL_BUCKETS) for r in kept
    ), 6)
    pack_usd = round(sum(
        sum(_n(r.get(f"{b}, USD.")) for b in _ALL_BUCKETS) for r in kept
    ), 6)
    total_rub = round(items_rub + pack_rub, 6)
    total_usd = round(items_usd + pack_usd, 6)

    # «Себестоимость, руб./USD.» в источнике лежит на КАЖДОЙ строке калькуляции и
    # равна итогу по ней (проверено: max по ключу = Σ статей, расхождение в
    # шестом знаке). Витрина cost_calc_mv берёт именно max этой колонки, поэтому
    # итог проставляем во все строки — иначе дашборд показал бы себестоимость
    # пака нулевой, как она лежит в источнике.
    for i, r in enumerate(rows):
        r["Себестоимость, руб."] = total_rub
        r["Себестоимость, USD."] = total_usd
        r["sort_order"] = i

    return {
        "rows": rows,
        "items_rub": items_rub, "items_usd": items_usd,
        "packaging_rub": pack_rub, "packaging_usd": pack_usd,
        "total_rub": total_rub, "total_usd": total_usd,
        "generated": len(generated), "kept": len(kept),
    }


async def build(
    *, model: str, articul: str, calc_sign: str = "", plan_id: str = "",
    task_number: str = "", rows: list[dict] | None = None, username: str,
) -> dict:
    """Собрать/пересобрать строки пака и записать сборку в журнал.

    Версию НЕ создаёт и в кэш не пишет: строки возвращаются в редактор, там их
    видит человек и сохраняет обычным путём (черновик или отправка на
    утверждение). Иначе сборка обходила бы согласование ПЭО.
    """
    async with acquire() as conn:
        pack = await _load_pack(conn, model, articul)
        if pack is None:
            raise ValueError("мультипак не заведён — сначала задайте состав")
        if not pack["items"]:
            raise ValueError("состав мультипака пуст")
        resolved = await _resolve_items(conn, pack["items"], calc_sign, plan_id)
        template = await _pack_template_row(
            conn, model, articul, calc_sign, plan_id, task_number
        )
        if template is None:
            raise ValueError(
                "калькуляция мультипака не найдена в данных: собирать не во что"
            )
        built = build_rows(template, resolved, rows or [])
        last = await _last_build(conn, pack["id"], calc_sign, plan_id, task_number)
        snapshot = [
            {
                "src_model": r["src_model"], "src_articul": r["src_articul"],
                "src_calc_sign": r["src_calc_sign"], "src_plan_id": r["src_plan_id"],
                "actual_calc_sign": r.get("actual_calc_sign"),
                "actual_plan_id": r.get("actual_plan_id"),
                "qty": r["qty"], "unit_rub": r["unit_rub"], "unit_usd": r["unit_usd"],
                "sum_rub": r["sum_rub"], "sum_usd": r["sum_usd"],
                "task": r.get("task"), "tasks_total": r.get("tasks_total"),
                "calc_date": r["calc_date"].isoformat() if r.get("calc_date") else None,
                "fx_rate": r.get("fx_rate"),
                "buckets": r["buckets"],
                "found": r["found"],
            }
            for r in resolved
        ]
        await conn.execute(
            """INSERT INTO cost_multipack_build
               (multipack_id, calc_sign, plan_id, task_number, items,
                items_rub, items_usd, packaging_rub, packaging_usd,
                total_rub, total_usd, built_by)
               VALUES ($1, $2, $3, $4, $5::jsonb, $6, $7, $8, $9, $10, $11, $12)""",
            pack["id"], _key(calc_sign), _key(plan_id), _key(task_number),
            json.dumps(snapshot, ensure_ascii=False),
            built["items_rub"], built["items_usd"],
            built["packaging_rub"], built["packaging_usd"],
            built["total_rub"], built["total_usd"], username,
        )
    log(logging.INFO, "мультипак: сборка",
        model=_key(model), articul=_key(articul), calc_sign=_key(calc_sign),
        plan_id=_key(plan_id), rows=len(built["rows"]),
        total_rub=built["total_rub"], user=username)
    state = _state_payload(pack, resolved, last, template, calc_sign)
    return {**built, "state": state}


# ---------------------------------------------------------------------------
# Поиск одиночек и журнал
# ---------------------------------------------------------------------------


async def search_candidates(
    *, query: str = "", calc_sign: str = "", plan_id: str = "",
    level01: str = "", brand_manager: str = "",
    exclude_model: str = "", exclude_articul: str = "", limit: int = 50,
) -> dict:
    """Калькуляции-одиночки для добавления в состав.

    Текстовый поиск — по модели, артикулу и наименованию модели. Фильтры: этап
    (интерфейс по умолчанию подставляет этап пака — ПКПСС собирается из ПКПСС),
    группа Level 01 (по умолчанию — группа пака: одиночки носков ищут среди
    носков, а не среди 90 тысяч калькуляций раздела) и бренд-менеджер. План не
    фильтруем, а показываем: у пака и одиночек он различается, и подходящую
    калькуляцию выбирает человек. Паки из списка исключены — пак в пак нельзя.

    Отдаёт на одну строку больше лимита и флаг truncated: «показаны первые 50»
    должно быть видно, иначе усечённый список читается как полный.
    """
    q = _key(query)
    limit = max(1, min(int(limit or 50), 200))
    like = f"%{q.lower()}%"
    async with acquire() as conn:
        rows = await conn.fetch(
            f"""
            WITH src AS (
                SELECT trim(c."Модель")                                AS model,
                       trim(c."Артикул")                               AS articul,
                       trim(COALESCE(c."Признак калькуляции", ''))     AS calc_sign,
                       trim(COALESCE(c."PLAN_ID", ''))                 AS plan_id,
                       trim(COALESCE(c."Номер задания производства", '')) AS task,
                       max(c."Наименование модели")                    AS model_name,
                       max(c."Level 01")                               AS level01,
                       max(c."Бренд-менеджер")                         AS brand_manager,
                       max(c."дата расчета")                           AS calc_date,
                       SUM({_bucket_sum_expr("c", "руб.")})            AS total_rub
                  FROM cost_data_all c
                 WHERE ($1 = '' OR trim(COALESCE(c."Признак калькуляции", '')) = $1)
                   AND ($2 = '' OR lower(trim(c."Модель")) LIKE $3
                        OR lower(trim(c."Артикул")) LIKE $3
                        OR lower(COALESCE(c."Наименование модели", '')) LIKE $3)
                   AND ($6 = '' OR trim(COALESCE(c."Level 01", '')) = $6)
                   AND ($7 = '' OR trim(COALESCE(c."Бренд-менеджер", '')) = $7)
                 GROUP BY 1, 2, 3, 4, 5
            ), best AS (
                SELECT DISTINCT ON (model, articul, calc_sign, plan_id)
                       model, articul, calc_sign, plan_id, task, model_name,
                       level01, brand_manager, calc_date, total_rub,
                       count(*) OVER (PARTITION BY model, articul, calc_sign, plan_id) AS tasks_total
                  FROM src
                 ORDER BY model, articul, calc_sign, plan_id, total_rub DESC, task
            )
            SELECT b.* FROM best b
             WHERE NOT (b.model = $4 AND b.articul = $5)
               AND NOT EXISTS (
                   SELECT 1 FROM cost_multipack mp
                    WHERE mp.model = b.model AND mp.articul = b.articul
               )
             ORDER BY b.model, b.articul, b.plan_id
             LIMIT {limit + 1}
            """,
            _key(calc_sign), q, like, _key(exclude_model), _key(exclude_articul),
            _key(level01), _key(brand_manager),
        )
    data = [
        {
            "model": r["model"], "articul": r["articul"],
            "calc_sign": r["calc_sign"], "plan_id": r["plan_id"],
            "model_name": r["model_name"], "level01": r["level01"],
            "brand_manager": r["brand_manager"],
            "calc_date": r["calc_date"], "task": r["task"],
            "tasks_total": int(r["tasks_total"] or 1),
            "unit_rub": _n(r["total_rub"]),
        }
        for r in rows[:limit]
    ]
    return {"data": data, "truncated": len(rows) > limit, "limit": limit}


async def list_packs(limit: int = 500) -> list[dict]:
    """Журнал мультипаков: состав, последняя сборка, кто и когда."""
    limit = max(1, min(int(limit or 500), 2000))
    async with acquire() as conn:
        rows = await conn.fetch(
            f"""SELECT p.id, p.model, p.articul, p.pack_size, p.note,
                       p.created_by, p.created_at, p.updated_by, p.updated_at,
                       (SELECT count(*) FROM cost_multipack_item i
                         WHERE i.multipack_id = p.id)                AS items,
                       (SELECT COALESCE(sum(i.qty), 0) FROM cost_multipack_item i
                         WHERE i.multipack_id = p.id)                AS qty_total,
                       b.built_at, b.built_by, b.calc_sign, b.plan_id,
                       b.total_rub, b.total_usd
                  FROM cost_multipack p
                  LEFT JOIN LATERAL (
                       SELECT built_at, built_by, calc_sign, plan_id,
                              total_rub, total_usd
                         FROM cost_multipack_build
                        WHERE multipack_id = p.id
                        ORDER BY built_at DESC LIMIT 1
                  ) b ON TRUE
                 ORDER BY p.updated_at DESC, p.id DESC
                 LIMIT {limit}"""
        )
    out = []
    for r in rows:
        row = dict(r)
        for money in ("pack_size", "qty_total", "total_rub", "total_usd"):
            if row.get(money) is not None:
                row[money] = _n(row[money])
        out.append(row)
    return out


async def packs_using(model: str, articul: str) -> list[dict]:
    """В какие паки входит эта одиночка.

    Нужно при правке её калькуляции: себестоимость паков от этого не изменится
    сама (пересборка ручная), и человек должен знать, что паки поедут.
    """
    async with acquire() as conn:
        rows = await conn.fetch(
            """SELECT p.model, p.articul, i.qty
                 FROM cost_multipack_item i
                 JOIN cost_multipack p ON p.id = i.multipack_id
                WHERE i.src_model = $1 AND i.src_articul = $2
                ORDER BY p.model, p.articul""",
            _key(model), _key(articul),
        )
    # qty — NUMERIC, то есть Decimal: в JSON он уходит строкой, и на фронте
    # арифметика по нему молча даёт NaN. Приводим здесь.
    return [{**dict(r), "qty": _n(r["qty"])} for r in rows]


async def pack_flags(keys: list[tuple[str, str]]) -> set[tuple[str, str]]:
    """Какие из переданных (модель, артикул) — мультипаки.

    Главная таблица помечает их значком, а дашборды исключают: себестоимость
    пака складывается из одиночек, которые в агрегатах уже посчитаны.
    """
    if not keys:
        return set()
    uniq = list({(_key(m), _key(a)) for m, a in keys})
    async with acquire() as conn:
        rows = await conn.fetch(
            """SELECT model, articul FROM cost_multipack
                WHERE (model, articul) IN (
                      SELECT * FROM unnest($1::text[], $2::text[])
                )""",
            [k[0] for k in uniq], [k[1] for k in uniq],
        )
    return {(r["model"], r["articul"]) for r in rows}
