"""Разбор вкладок ФОТ-дашборда (fot_p9..fot_p14) языковой моделью.

Блок «Разбор ИИ» на ФОТ-дашборде работает через тот же эндпоинт
`POST /insights/block`, что и в себестоимости: диспетчеризация в
routes.analyze_block — строго по префиксу `fot_`, до validate_charts
(вкладки серии с фронта не присылают). Право остаётся `cost:insights`.

Отличие от app/insights.py: **данные вкладки бэкенд собирает сам**. Фронт
шлёт только block («fot_p9»..«fot_p14») и context с теми же срезами, что
уходит в `GET /fot/dashboard`; отсюда вызывается тот же сборщик
(`app.fot.dashboard` в live, `app.mocks.fot_dashboard` в моке — ветвление
продублировано здесь, fot.py не знает про ИИ). Арифметику считает код:
`compute_*_facts` детерминированно выжимает из payload вкладки
(cards/tables/charts/meta) готовые числа с точкой сравнения и единицами,
модель только интерпретирует — те же гарантии, что у себестоимостного
разбора (см. докстринг app/insights.py).

Кэш — та же таблица `cost_insight_runs`, но хеш свой: кроме блока, контекста
и модели в него входит sha256-дайджест собранных данных. Без него после
обновления витрин (`POST /fot/refresh`) пользователь получал бы протухшие
наблюдения по свежим цифрам.
"""

from __future__ import annotations

import hashlib
import json
import logging
import re
import time
from datetime import date
from typing import Any

from app import insights
from app.logship import log

# ─── Цвета светофора Δ-колонок (порт меры «_ФОТ Δ цвет» из app/fot.py) ───────
# Дублируем значения, а не импортируем приватные имена: app/fot.py сейчас
# переписывается параллельным агентом, зависеть от его внутренностей нельзя.
_DELTA_RED, _DELTA_AMBER, _DELTA_GREEN = "#C0392B", "#9E6B0F", "#1C8A5A"

# ─── Промпт раздела ──────────────────────────────────────────────────────────

FOT_GLOSSARY = """Термины ФОТ-дашборда «ФОТ и численность» (Markformelle):
- ФОТ — фонд оплаты труда: статьи «Заработная плата» в контурах P&L и
  «Производство (BS)». Лимит ФОТ — плановый потолок ФОТ по выбранному
  план-сценарию.
- План-сценарии: «бюджет», «тактика», «НовСтрат». Бюджетные колонки вкладок
  P10–P13 — ВСЕГДА сценарий «бюджет», независимо от среза «План» панели.
- Полугодия фиксированные: 1ПГ — январь–июнь, 2ПГ — июль–декабрь; свод
  P10–P13 от срезов дат и «Плана» панели НЕ зависит.
- Ожидание 2026 = Факт 1ПГ26 + Тактика 2ПГ26 (план на 2ПГ уже в «Ожидании»,
  это не факт).
- ПЧ — плановая численность (ставки), ФЧ — фактическая; укомплектованность =
  ФЧ/ПЧ, ориентиры: 95 % и выше — норма, ниже 85 % — тревога.
- ВГО — внутригрупповые обороты; срез «ВГО» оставляет обороты между юрлицами
  группы, «без ВГО» — убирает.
- Светофор Δ-колонок: красный — рост выше +10 % при растущей доле ФОТ в
  выручке (зона контроля), янтарный — рост выше +10 % без роста доли,
  зелёный — остальное.
- Суммы в валюте витрины без пересчёта. «млн» и «тыс» — разные единицы:
  217,26 млн = 217 260 тыс."""

SYSTEM_PROMPT_FOT = """Ты аналитик ФОТ и численности на швейном предприятии.
Тебе дают факты, ПОСЧИТАННЫЕ кодом по данным одной вкладки ФОТ-дашборда, и ты
пишешь короткие наблюдения для финансиста.

Жёсткие правила:
1. Пиши ТОЛЬКО по-русски.
2. Используй ТОЛЬКО числа из раздела «Посчитанные факты». Не считай сам, не
   складывай, не выводи новых величин.
3. НЕ объясняй причины. В данных нет информации о причинах: запрещено писать
   «из-за», «потому что», «вследствие», предполагать найм, увольнения,
   пересмотр окладов, курс.
4. Не давай указаний и рекомендаций («нужно», «следует», «рекомендую»).
5. Проценты не складывай; разницу процентов называй в процентных пунктах
   (пп). «+0,4 пп» и «+0,4 %» — разные утверждения, не путай.
6. НЕЗАКРЫТЫЙ ПЕРИОД. Если в фактах есть пометка «период почти наверняка НЕ
   ЗАКРЫТ», по этой точке НЕЛЬЗЯ делать вывод о падении, ухудшении или
   обвале: там просто меньше дней. Первым наблюдением скажи, что последний
   период неполный и в сравнение не годится, а остальные наблюдения строй по
   закрытым периодам.
7. От 3 до 5 наблюдений, каждое до 350 символов. Наблюдение — это не
   констатация числа, а число с точкой сравнения: соседний период, соседняя
   страна или группа ЦФО, лимит, итог. «ФОТ Беларуси 98 млн» — плохо;
   «ФОТ Беларуси 98 млн, 45 % Ожидания группы, Δ +14 % к 2025» — хорошо.
8. Больше цени наблюдения, СВЯЗЫВАЮЩИЕ разные таблицы и карточки вкладки
   (страны против итога, группы ЦФО против каналов сбыта, ФОТ против
   численности), чем пересказ одной строки.
9. Если фактов мало или они не показывают ничего содержательного — так и
   скажи одним наблюдением. Не придумывай значимость.

Отвечай СТРОГО одним JSON-объектом без markdown:
{"observations":[{"text":"...","kind":"neutral|attention|risk"}]}
kind: neutral — констатация, attention — стоит посмотреть, risk — тревожный
признак (ФОТ выше лимита, красный светофор, сильный перекос стран/групп)."""


def _build_user_prompt(block_title: str, context: dict, facts: list[str],
                       data: dict) -> str:
    """Промпт пользователя: глоссарий, методология вкладки, срезы, факты."""
    ctx_lines = [f"- {k}: {v}" for k, v in (context or {}).items()
                 if v not in (None, "", [])]
    methodology = str((data.get("meta") or {}).get("methodology") or "").strip()
    return "\n\n".join(filter(None, [
        FOT_GLOSSARY,
        f"Раздел дашборда: {block_title}",
        f"Методология вкладки: {methodology}" if methodology else "",
        "Срезы пользователя:\n" + "\n".join(ctx_lines) if ctx_lines else "",
        "Посчитанные факты (единственный источник чисел):\n"
        + "\n".join(f"- {f}" for f in facts),
    ]))


# ─── Данные вкладки ──────────────────────────────────────────────────────────

# Списочные срезы панели (повторяют параметры GET /fot/dashboard) и скалярные.
_LIST_PARAM_KEYS = ("direction", "cfo_group", "cfo_subgroup", "cfo",
                    "legal_entity", "country", "matrix_path", "channel_path")
_SCALAR_PARAM_KEYS = ("scenario_plan", "intercompany", "year", "month",
                      "currency")


def _params_from_context(context: dict) -> dict:
    """Срезы из context запроса → параметры сборщика вкладки.

    Фронт кладёт в context те же ключи, что шлёт query-параметрами в
    /fot/dashboard; списочные — списками, скалярные — строками.
    """
    params: dict = {}
    for key in _LIST_PARAM_KEYS:
        value = context.get(key)
        if isinstance(value, (list, tuple)):
            items = [str(v).strip() for v in value if str(v).strip()]
            if items:
                params[key] = items
        elif isinstance(value, str) and value.strip():
            params[key] = [value.strip()]
    for key in _SCALAR_PARAM_KEYS:
        value = context.get(key)
        if value not in (None, ""):
            params[key] = value
    return params


async def _fot_dashboard(tab: str, params: dict) -> dict:
    """Payload вкладки — тот же сборщик, что у GET /fot/dashboard.

    Ветвление mock/live продублировано из routes.fot_dashboard: fot.py про
    ИИ-разбор не знает. Чужой tab — ValueError (наверху → 400).
    """
    from app import fot, mocks
    if fot.fot_mock_enabled():
        return mocks.fot_dashboard(tab, params)
    return await fot.dashboard(tab, params)


# ─── Хелперы по payload вкладки ──────────────────────────────────────────────

def _fmt(x: float) -> str:
    """Число для фактов — та же логика, что в app.insights._fmt."""
    return insights._fmt(x)


def _card(cards: list[dict], key: str) -> dict | None:
    return next((c for c in cards or [] if c.get("key") == key), None)


def _card_value(cards: list[dict], key: str) -> float | None:
    card = _card(cards, key)
    value = (card or {}).get("value")
    if isinstance(value, bool) or value is None:
        return None
    return float(value) if isinstance(value, (int, float)) else None


def _unit(cards: list[dict], key: str) -> str:
    return str((_card(cards, key) or {}).get("unit") or "")


def _table(data: dict, key: str) -> dict | None:
    return next((t for t in data.get("tables") or [] if t.get("key") == key),
                None)


def _rows(data: dict, key: str) -> list[dict]:
    table = _table(data, key)
    return [r for r in (table or {}).get("rows") or [] if isinstance(r, dict)]


def _num(v: Any) -> float | None:
    if isinstance(v, bool) or v is None or not isinstance(v, (int, float)):
        return None
    return float(v)


def _closed_note(data: dict) -> str | None:
    """Пометка незакрытого периода для P9 (месячная вкладка со срезом дат).

    month_date позже закрытого периода витрин — факт месяца в источнике ещё
    дольётся. Формулировка — та же, на которую опирается правило 6 промпта.
    P10/P11 фиксированных полугодий срезов дат не имеют — им пометка не нужна.
    """
    meta = data.get("meta") or {}
    raw_md = str((meta.get("period") or {}).get("month_date") or "")[:10]
    raw_closed = str(meta.get("closed_period") or "")[:10]
    if not raw_md or not raw_closed:
        return None
    try:
        md, closed = date.fromisoformat(raw_md), date.fromisoformat(raw_closed)
    except ValueError:
        return None
    if md <= closed:
        return None
    return (f"ВНИМАНИЕ: месяц среза {md.isoformat()} позже закрытого периода "
            f"{closed.isoformat()} — период почти наверняка НЕ ЗАКРЫТ и "
            f"несопоставим с полными периодами")


_COLOR_WORDS = {
    _DELTA_RED: "красный",
    _DELTA_AMBER: "янтарный",
    _DELTA_GREEN: "зелёный",
}


def _color_word(color: Any) -> str:
    """Светофор из payload словом (для фактов); неизвестный цвет — пусто."""
    return _COLOR_WORDS.get(str(color or ""), "")


def _staff_zone(pct: float | None, color: Any = None) -> str:
    """Укомплектованность словами: пороги 95/85 %, цвет — из payload."""
    if pct is None:
        return ""
    if pct >= 95:
        word = "норма (порог 95 % соблюдён)"
    elif pct >= 85:
        word = "ниже ориентира 95 %, но не ниже тревожного 85 %"
    else:
        word = "ниже тревожного порога 85 %"
    swatch = _color_word(color)
    return f"{word}" + (f", светофор {swatch}" if swatch else "")


# ─── Факты P9 «ФОТ и численность» ────────────────────────────────────────────

def _facts_p9(data: dict) -> list[str]:
    """Карточки месяца, месячная динамика, матрица по группам ЦФО."""
    facts: list[str] = []
    cards = data.get("cards") or []
    meta = data.get("meta") or {}
    cur = str(meta.get("currency") or "BYN")
    scenario = str(meta.get("scenario_plan_label") or meta.get("scenario_plan") or "")

    fot_fact = _card_value(cards, "fot_fact")
    fot_limit = _card_value(cards, "fot_limit")
    share = _card_value(cards, "fot_share")
    headcount = _card_value(cards, "headcount")
    per_head = _card_value(cards, "fot_per_head")

    if fot_fact is not None:
        facts.append(f"Карточки месяца: ФОТ факт = {_fmt(fot_fact)} {cur}")
    if fot_limit is not None:
        facts.append(f"Карточки месяца: ФОТ лимит ({scenario}) = "
                     f"{_fmt(fot_limit)} {cur}")
    if fot_fact is not None and fot_limit is not None:
        delta = fot_fact - fot_limit
        pct = delta / fot_limit * 100 if fot_limit else None
        over = "выше" if delta > 0 else "ниже"
        facts.append(f"Карточки месяца: ФОТ факт {over} лимита на "
                     f"{_fmt(abs(delta))} {cur}"
                     + (f" ({_fmt(abs(pct))}% лимита)" if pct is not None else ""))
    if share is not None:
        facts.append(f"Карточки месяца: ФОТ = {_fmt(share)}% выручки месяца")
    if headcount is not None:
        facts.append(f"Карточки месяца: численность = {_fmt(headcount)} ставок")
    if per_head is not None:
        facts.append(f"Карточки месяца: ФОТ на сотрудника = {_fmt(per_head)} {cur}")

    # Месячная серия графика: минимум/максимум/последний ФОТ и ход численности.
    # Пары строим по ИНДЕКСАМ исходной серии: в live месяцы без факта приходят
    # None, и простое сжатие до непустых значений сместило бы подписи месяцев.
    chart = next((c for c in data.get("charts") or []
                  if isinstance(c, dict) and c.get("key") == "monthly"), None)
    if chart:
        labels = [str(x) for x in (chart.get("labels") or [])]
        for s in chart.get("series") or []:
            if not isinstance(s, dict):
                continue
            raw = s.get("values") or []
            pairs = [(labels[i] if i < len(labels) else "", v)
                     for i, v in enumerate(raw) if _num(v) is not None]
            if not pairs:
                continue
            hi_lbl, hi = max(pairs, key=lambda p: p[1])
            lo_lbl, lo = min(pairs, key=lambda p: p[1])
            first_lbl, first = pairs[0]
            last_lbl, last = pairs[-1]
            name = str(s.get("label") or s.get("key") or "серия")
            unit = str(s.get("unit") or "")
            facts.append(
                f"График «{chart.get('label')}», {name}: максимум {_fmt(hi)} {unit}"
                + (f" ({hi_lbl})" if hi_lbl else "")
                + f", минимум {_fmt(lo)} {unit}"
                + (f" ({lo_lbl})" if lo_lbl else "")
                + f", последний {_fmt(last)} {unit}"
                + (f" ({last_lbl})" if last_lbl else ""))
            if len(pairs) >= 2 and first:
                delta = last - first
                facts.append(
                    f"График «{chart.get('label')}», {name}: изменение от первого "
                    f"({_fmt(first)}"
                    + (f", {first_lbl}" if first_lbl else "")
                    + f") к последнему месяцу {_fmt(delta)} {unit}"
                    + (f" ({_fmt(delta / abs(first) * 100)}%)" if first else ""))

    # Матрица по группам ЦФО: лидер по ФОТ и группы, где доля ФОТ выше лимитной.
    rows = _rows(data, "by_department")
    level_label = str(((data.get("tables") or [{}])[0].get("meta") or {})
                      .get("level_label") or "Группа ЦФО")
    with_fot = [(str(r.get("label") or "?"), _num(r.get("fot_fact"))) for r in rows]
    with_fot = [(lbl, v) for lbl, v in with_fot if v is not None]
    if with_fot:
        top_lbl, top = max(with_fot, key=lambda p: p[1])
        facts.append(f"Таблица «{level_label}»: наибольший ФОТ факт — "
                     f"{top_lbl}: {_fmt(top)} {cur}")
        total = sum(v for _, v in with_fot)
        if total:
            facts.append(f"Таблица «{level_label}»: {top_lbl} — "
                         f"{_fmt(top / total * 100)}% суммарного ФОТ по строкам")
    over_limit = []
    for r in rows:
        share_v, limit_v = _num(r.get("fot_share_pct")), _num(r.get("limit_share_pct"))
        if share_v is not None and limit_v is not None and share_v > limit_v:
            over_limit.append(f"{r.get('label')}: {_fmt(share_v)}% против "
                              f"лимита {_fmt(limit_v)}%")
    if over_limit:
        facts.append("Таблица «%s»: ФОТ, %% от выручки ВЫШЕ лимитной доли — %s"
                     % (level_label, "; ".join(over_limit[:5])))
    else:
        facts.append(f"Таблица «{level_label}»: нет строк, где ФОТ, % от выручки "
                     f"выше лимитной доли")

    note = _closed_note(data)
    if note:
        facts.append(note)
    return facts


# ─── Факты P10 «ФОТ свод по группе» ──────────────────────────────────────────

def _facts_p10(data: dict) -> list[str]:
    """Сводные карточки группы и таблица по странам (фикс. полугодия)."""
    facts: list[str] = []
    cards = data.get("cards") or []
    cur = str((data.get("meta") or {}).get("currency") or "BYN")

    exp26 = _card_value(cards, "fot_expect_2026")
    sh26 = _card_value(cards, "fot_share_2026")
    f25 = _card_value(cards, "fot_fact_2025")
    sh25 = _card_value(cards, "fot_share_2025")
    dev = _card_value(cards, "fot_dev_year")

    if exp26 is not None:
        facts.append(f"Свод по группе: ФОТ Ожидание 2026 = {_fmt(exp26)} млн {cur}")
    if f25 is not None:
        facts.append(f"Свод по группе: ФОТ Факт 2025 = {_fmt(f25)} млн {cur}")
    if exp26 is not None and f25 is not None and f25:
        delta, pct = exp26 - f25, (exp26 - f25) / f25 * 100
        facts.append(f"Свод по группе: Ожидание 2026 против Факта 2025 — "
                     f"{_fmt(delta)} млн {cur} ({_fmt(pct)}%)")
    if sh26 is not None and sh25 is not None:
        facts.append(f"Свод по группе: доля ФОТ в выручке — {_fmt(sh26)}% "
                     f"(Ожидание 2026) против {_fmt(sh25)}% (Факт 2025), "
                     f"разница {_fmt(sh26 - sh25)} пп")
    if dev is not None:
        facts.append(f"Свод по группе: Δ ФОТ год к 2025 = {_fmt(dev)}%")

    rows = _rows(data, "by_country")
    countries = [r for r in rows if r.get("label") and r.get("label") != "Итого"]
    itogo = next((r for r in rows if r.get("label") == "Итого"), None)

    dev_rows = [(str(r.get("label") or "?"), _num(r.get("dev_year")))
                for r in countries]
    dev_rows = [(lbl, v) for lbl, v in dev_rows if v is not None]
    if dev_rows:
        hi_lbl, hi = max(dev_rows, key=lambda p: p[1])
        lo_lbl, lo = min(dev_rows, key=lambda p: p[1])
        facts.append(f"Страны: лидер Δ год к 2025 — {hi_lbl} {_fmt(hi)}%, "
                     f"аутсайдер — {lo_lbl} {_fmt(lo)}%")
    reds = [str(r.get("label")) for r in countries
            if r.get("dev_year_color") == _DELTA_RED]
    if reds:
        facts.append(f"Страны с КРАСНЫМ светофором Δ (рост >10 % и растущая "
                     f"доля ФОТ — зона контроля): {', '.join(reds)}")
    ambers = [str(r.get("label")) for r in countries
              if r.get("dev_year_color") == _DELTA_AMBER]
    if ambers:
        facts.append(f"Страны с янтарным светофором Δ (рост >10 % без роста "
                     f"доли): {', '.join(ambers)}")
    greens = [str(r.get("label")) for r in countries
              if r.get("dev_year_color") == _DELTA_GREEN]
    if greens and (reds or ambers):
        facts.append(f"В зелёной зоне Δ (рост в пределах +10 %): "
                     f"{', '.join(greens)}")

    # Доля страны в Ожидании группы (выручку по странам таблица не отдаёт).
    exps = [(str(r.get("label") or "?"), _num(r.get("exp26"))) for r in countries]
    exps = [(lbl, v) for lbl, v in exps if v is not None]
    total_exp = sum(v for _, v in exps)
    if exps and total_exp:
        shares = [(lbl, v, v / total_exp * 100) for lbl, v in exps]
        lo_lbl, lo_v, lo_s = min(shares, key=lambda p: p[2])
        facts.append(f"Страны: минимальная доля в ФОТ Ожидание 2026 группы — "
                     f"{lo_lbl}: {_fmt(lo_v)} тыс {cur} ({_fmt(lo_s)}% группы)")

    if itogo is not None:
        it_dev = _num(itogo.get("dev_year"))
        if it_dev is not None:
            facts.append(f"Строка «Итого» по странам таблицы: Δ год к 2025 = "
                         f"{_fmt(it_dev)}%")

    warning = str((data.get("meta") or {}).get("warning") or "").strip()
    if warning:
        facts.append(f"ПРЕДУПРЕЖДЕНИЕ вкладки: {warning}")
    return facts


# ─── Факты P11 «ФОТ направления и подразделения» ─────────────────────────────

def _facts_p11(data: dict) -> list[str]:
    """Три разреза: группы/подразделения, каналы сбыта, отделы."""
    facts: list[str] = []
    cards = data.get("cards") or []
    cur = str((data.get("meta") or {}).get("currency") or "BYN")

    exp26 = _card_value(cards, "fot_expect_2026")
    f26_h1 = _card_value(cards, "fot_fact_1h2026")
    dev = _card_value(cards, "fot_dev_year")
    sh26 = _card_value(cards, "fot_share_2026")
    if exp26 is not None:
        facts.append(f"Свод: ФОТ Ожидание 2026 = {_fmt(exp26)} млн {cur}")
    if f26_h1 is not None:
        facts.append(f"Свод: ФОТ Факт 1ПГ26 = {_fmt(f26_h1)} тыс {cur}")
    if dev is not None:
        facts.append(f"Свод: Δ ФОТ год к 2025 = {_fmt(dev)}%")
    if sh26 is not None:
        facts.append(f"Свод: доля ФОТ в выручке (Ожидание 2026) = {_fmt(sh26)}%")

    def _hi_lo(rows: list[dict], level: str) -> None:
        """Максимальный рост и худшая динамика Δ год к 2025.

        Худшая строка зовётся «падением», только когда Δ отрицательная:
        когда растут все, минимум — «минимальный рост», а не падение.
        """
        items = [(str(r.get("label") or "?"), _num(r.get("dev_year")))
                 for r in rows]
        items = [(lbl, v) for lbl, v in items if v is not None]
        if not items:
            return
        hi_lbl, hi = max(items, key=lambda p: p[1])
        lo_lbl, lo = min(items, key=lambda p: p[1])
        lo_word = "падение" if lo < 0 else "минимальный рост"
        facts.append(f"{level}: максимальный рост Δ год к 2025 — {hi_lbl} "
                     f"{_fmt(hi)}%"
                     + (f"; {lo_word} — {lo_lbl} {_fmt(lo)}%"
                        if hi_lbl != lo_lbl else ""))

    def _top_by(rows: list[dict], level: str) -> None:
        """Топ строки по ФОТ Ожидание 2026 с долей в сумме строк."""
        items = [(str(r.get("label") or "?"), _num(r.get("exp26")))
                 for r in rows]
        items = [(lbl, v) for lbl, v in items if v is not None]
        if not items:
            return
        total = sum(v for _, v in items)
        top_lbl, top = max(items, key=lambda p: p[1])
        facts.append(f"{level}: наибольший ФОТ Ожидание 2026 — {top_lbl} "
                     f"{_fmt(top)} тыс {cur}"
                     + (f", {_fmt(top / total * 100)}% суммы строк"
                        if total else ""))

    dept = _rows(data, "by_department")
    _hi_lo(dept, "Группы ЦФО (таблица «По подразделениям»)")
    _top_by(dept, "Группы ЦФО (таблица «По подразделениям»)")
    _top_by(_rows(data, "by_channel"),
            "Каналы сбыта (таблица «Продажи: ФОТ по каналам сбыта»)")

    # Отделы: суммарная динамика разреза (сумма строк Ожидание против Факта).
    dirs = _rows(data, "by_direction")
    _hi_lo(dirs, "Направления (таблица «Отделы: ФОТ по направлениям»)")
    s_f25 = sum(v for v in (_num(r.get("f25")) for r in dirs) if v is not None)
    s_exp = sum(v for v in (_num(r.get("exp26")) for r in dirs) if v is not None)
    if s_f25 and s_exp:
        facts.append(f"Отделы, суммарно по строкам разреза: Факт 2025 = "
                     f"{_fmt(s_f25)} тыс {cur}, Ожидание 2026 = "
                     f"{_fmt(s_exp)} тыс {cur}, изменение "
                     f"{_fmt(s_exp - s_f25)} тыс ({_fmt((s_exp - s_f25) / s_f25 * 100)}%)")

    warning = str((data.get("meta") or {}).get("warning") or "").strip()
    if warning:
        facts.append(f"ПРЕДУПРЕЖДЕНИЕ вкладки: {warning}")
    return facts


# ─── Факты P12 «Численность план и факт» ─────────────────────────────────────

def _facts_p12(data: dict) -> list[str]:
    """Карточки ПЧ/ФЧ/укомплектованность, страны и группы ЦФО (фикс. полугодия).

    Укомплектованность = ФЧ 1ПГ26 / ПЧ утв.; пороги 95/85 %, светофор строки
    берём из payload (staff_pct_color), зону называем словами порогов.
    """
    facts: list[str] = []
    cards = data.get("cards") or []

    pa = _card_value(cards, "plan_approved")
    pt = _card_value(cards, "plan_tactic")
    q25 = _card_value(cards, "fact_2025")
    q26 = _card_value(cards, "fact_2026")
    staff = _card_value(cards, "staffing")

    if pa is not None:
        facts.append(f"Карточки: ПЧ утв. 2026 (стратегия) = {_fmt(pa)} ставок")
    if pt is not None:
        facts.append(f"Карточки: ПЧ тактика 2026 = {_fmt(pt)} ставок"
                     + (f", на {_fmt(pt - pa)} ставок больше утверждённой"
                        if pa is not None else ""))
    if q25 is not None:
        facts.append(f"Карточки: ФЧ на 30.06.2025 = {_fmt(q25)} ставок")
    if q26 is not None:
        facts.append(f"Карточки: ФЧ на 30.06.2026 = {_fmt(q26)} ставок")
    if q25 is not None and q26 is not None and q25:
        delta = q26 - q25
        facts.append(f"Карточки: численность за год — {_fmt(delta)} ставок "
                     f"({_fmt(delta / q25 * 100)}%)")
    if staff is not None:
        facts.append(f"Карточки: укомплектованность (ФЧ 1ПГ26 / ПЧ утв.) = "
                     f"{_fmt(staff)}% — {_staff_zone(staff)}")

    rows = _rows(data, "by_country")
    countries = [r for r in rows if r.get("label") and r.get("label") != "Итого"]
    itogo = next((r for r in rows if r.get("label") == "Итого"), None)

    staff_rows = [(str(r.get("label") or "?"), _num(r.get("staff_pct")),
                   r.get("staff_pct_color")) for r in countries]
    staff_rows = [(lbl, v, c) for lbl, v, c in staff_rows if v is not None]
    if staff_rows:
        hi = max(staff_rows, key=lambda p: p[1])
        lo = min(staff_rows, key=lambda p: p[1])
        facts.append(f"Страны: лучшая укомплектованность — {hi[0]} "
                     f"{_fmt(hi[1])}% ({_staff_zone(hi[1], hi[2])}); худшая — "
                     f"{lo[0]} {_fmt(lo[1])}% ({_staff_zone(lo[1], lo[2])})")
        alarms = [f"{lbl} {_fmt(v)}%" for lbl, v, c in staff_rows if v < 85]
        if alarms:
            facts.append(f"Страны НИЖЕ тревожного порога 85 % (красный "
                         f"светофор): {', '.join(alarms)}")
        else:
            facts.append("Стран ниже тревожного порога 85 % нет")
    dev_rows = [(str(r.get("label") or "?"), _num(r.get("dev_2625")))
                for r in countries]
    dev_rows = [(lbl, v) for lbl, v in dev_rows if v is not None]
    if dev_rows:
        hi_lbl, hi = max(dev_rows, key=lambda p: p[1])
        lo_lbl, lo = min(dev_rows, key=lambda p: p[1])
        facts.append(f"Страны: Δ численности 1ПГ26 к 1ПГ25 — максимум {hi_lbl} "
                     f"{_fmt(hi)}%, минимум {lo_lbl} {_fmt(lo)}%")
    if itogo is not None:
        it_staff = _num(itogo.get("staff_pct"))
        if it_staff is not None:
            facts.append(f"Строка «Итого» по странам таблицы: укомплектованность "
                         f"{_fmt(it_staff)}% — "
                         f"{_staff_zone(it_staff, itogo.get('staff_pct_color'))}")

    groups = _rows(data, "by_department")
    g_rows = [(str(r.get("label") or "?"), _num(r.get("q26")),
               _num(r.get("staff_pct"))) for r in groups
              if r.get("label") and r.get("label") != "Итого"]
    g_rows = [(lbl, q, s) for lbl, q, s in g_rows if q is not None]
    if g_rows:
        total_q = sum(q for _, q, _ in g_rows)
        top_lbl, top_q, top_staff = max(g_rows, key=lambda p: p[1])
        facts.append(f"Подразделения: наибольшая ФЧ 1ПГ26 — {top_lbl} "
                     f"{_fmt(top_q)} ставок"
                     + (f", укомплектованность {_fmt(top_staff)}%"
                        if top_staff is not None else "")
                     + (f", {_fmt(top_q / total_q * 100)}% суммы строк"
                        if total_q else ""))

    warning = str((data.get("meta") or {}).get("warning") or "").strip()
    if warning:
        facts.append(f"ПРЕДУПРЕЖДЕНИЕ вкладки: {warning}")
    return facts


# ─── Факты P13 «HR-бюджет и отделы» ──────────────────────────────────────────

def _facts_p13(data: dict) -> list[str]:
    """HR-карточки и HR-таблица (статья × страна HR-ЦФО), блок «Отделы»."""
    facts: list[str] = []
    cards = data.get("cards") or []
    cur = str((data.get("meta") or {}).get("currency") or "BYN")

    hr_fact = _card_value(cards, "hr_fact_1h26")
    hr_budget = _card_value(cards, "hr_budget_2h26")
    hr_tactic = _card_value(cards, "hr_tactic_2h26")
    hr_expect = _card_value(cards, "hr_expect_2026")
    if hr_fact is not None:
        facts.append(f"HR-карточки: ФОТ HR-ЦФО (6 подразделений), все статьи, "
                     f"Факт 1ПГ26 = {_fmt(hr_fact)} тыс {cur}")
    if hr_budget is not None:
        facts.append(f"HR-карточки: Бюджет 2ПГ26 = {_fmt(hr_budget)} тыс {cur}")
    if hr_tactic is not None:
        facts.append(f"HR-карточки: Тактика 2ПГ26 = {_fmt(hr_tactic)} тыс {cur}"
                     + (f", на {_fmt(hr_tactic - hr_budget)} тыс больше бюджета"
                        if hr_budget is not None else ""))
    if hr_expect is not None:
        facts.append(f"HR-карточки: Ожидание 2026 = {_fmt(hr_expect)} тыс {cur}"
                     + (f", на {_fmt(hr_expect - hr_fact)} тыс больше Факта "
                        f"1ПГ26 (Ожидание = Факт 1ПГ26 + Тактика 2ПГ26)"
                        if hr_fact is not None else ""))

    # HR-таблица: итоги по статьям («Итого — статья») и итог всей таблицы.
    hr_rows = _rows(data, "hr_budget")
    articles = [r for r in hr_rows
                if str(r.get("label") or "").startswith("Итого — ")]
    grand = next((r for r in hr_rows if str(r.get("label") or "") == "Итого"),
                 None)
    art_rows = [(str(r.get("label") or "?")[len("Итого — "):],
                 _num(r.get("exp26")), _num(r.get("dev_year")),
                 r.get("dev_year_color")) for r in articles]
    art_rows = [(lbl, v, d, c) for lbl, v, d, c in art_rows if v is not None]
    if art_rows:
        top_lbl, top_v, top_d, top_c = max(art_rows, key=lambda p: p[1])
        total_art = sum(v for _, v, _, _ in art_rows)
        facts.append(f"HR-таблица: статья-лидер по Ожиданию 2026 — {top_lbl} "
                     f"{_fmt(top_v)} тыс {cur}"
                     + (f", {_fmt(top_v / total_art * 100)}% суммы статей"
                        if total_art else ""))
        # Контрольная детализация лидера: факт 1ПГ26 и динамика со светофором.
        top_row = next((r for r in articles
                        if str(r.get("label") or "") == f"Итого — {top_lbl}"), None)
        if top_row is not None:
            f26 = _num(top_row.get("f26_h1"))
            if f26 is not None:
                facts.append(f"HR-таблица, «{top_lbl}», итого: Факт 1ПГ26 = "
                             f"{_fmt(f26)} тыс {cur}"
                             + (f", Δ год к 2025 {_fmt(top_d)}%"
                                f" (светофор {_color_word(top_c) or 'нет'})"
                                if top_d is not None else ""))
        ambers = [lbl for lbl, _, d, c in art_rows
                  if c == _DELTA_AMBER and lbl != top_lbl]
        greens = [lbl for lbl, _, d, c in art_rows if c == _DELTA_GREEN]
        if ambers:
            facts.append(f"HR-таблица: статьи с янтарным светофором Δ "
                         f"(рост >10 %): {', '.join(ambers)}")
        if greens:
            facts.append(f"HR-таблица: статьи в зелёной зоне Δ "
                         f"(рост в пределах +10 %): {', '.join(greens)}")

    # Страны HR: строки «статья — страна (коды)» — сумма Ожиданий по стране.
    hr_country: dict = {}
    for r in hr_rows:
        label = str(r.get("label") or "")
        if label.startswith("Итого") or " — " not in label:
            continue
        parts = label.split(" — ")
        if len(parts) < 2:
            continue
        ctry = parts[1].split(" (")[0].strip()
        value = _num(r.get("exp26"))
        if value is not None:
            hr_country[ctry] = hr_country.get(ctry, 0.0) + value
    if hr_country:
        top_ctry, top_sum = max(hr_country.items(), key=lambda p: p[1])
        facts.append(f"HR-таблица по странам: наибольший ФОТ HR (Ожидание "
                     f"2026) — {top_ctry}: {_fmt(top_sum)} тыс {cur} "
                     f"({_fmt(top_sum / sum(hr_country.values()) * 100)}% HR-ЦФО)")
    # Крупнейшая строка «статья × страна»: в ней сидят контрольные числа
    # PBI (например, ЗП Беларуси 224 — факт 592,29 тыс).
    line_rows = [(str(r.get("label") or "?"), _num(r.get("exp26")),
                  _num(r.get("f26_h1"))) for r in hr_rows
                 if not str(r.get("label") or "").startswith("Итого")]
    line_rows = [(lbl, v, f) for lbl, v, f in line_rows if v is not None]
    if line_rows:
        lbl, v, f = max(line_rows, key=lambda p: p[1])
        facts.append(f"HR-таблица, крупнейшая строка — {lbl}: Ожидание 2026 "
                     f"{_fmt(v)} тыс {cur}"
                     + (f", Факт 1ПГ26 {_fmt(f)} тыс" if f is not None else ""))

    # Блок «Отделы»: карточки ФОТ/ПЧ/ФЧ и таблица по странам.
    dept_fot = _card_value(cards, "dept_fot_1h26")
    plan = _card_value(cards, "dept_headcount_plan")
    fact = _card_value(cards, "dept_headcount_fact")
    dev = _card_value(cards, "dept_dev_year")
    if dept_fot is not None:
        facts.append(f"Отделы: ФОТ 1ПГ26 суммарно = {_fmt(dept_fot)} тыс {cur}"
                     + (f", Δ год к 2025 {_fmt(dev)}%" if dev is not None else ""))
    if plan is not None and fact is not None:
        facts.append(f"Отделы: ПЧ (тактика) = {_fmt(plan)} ставок, ФЧ "
                     f"(30.06.2026) = {_fmt(fact)} ставок"
                     + (f", укомплектованность {_fmt(fact / plan * 100)}%"
                        if plan else ""))
    dept_rows = _rows(data, "by_country")
    countries = [r for r in dept_rows
                 if r.get("label") and r.get("label") != "Итого"]
    exps = [(str(r.get("label") or "?"), _num(r.get("exp26")),
             _num(r.get("f26_h1"))) for r in countries]
    exps = [(lbl, v, f) for lbl, v, f in exps if v is not None]
    if exps:
        total = sum(v for _, v, _ in exps)
        top_lbl, top_v, top_f = max(exps, key=lambda p: p[1])
        facts.append(f"Отделы, страны: наибольший ФОТ (Ожидание 2026) — "
                     f"{top_lbl} {_fmt(top_v)} тыс {cur}"
                     + (f" ({_fmt(top_v / total * 100)}% суммы стран)"
                        if total else "")
                     + (f"; Факт 1ПГ26 {_fmt(top_f)} тыс" if top_f is not None else ""))

    warning = str((data.get("meta") or {}).get("warning") or "").strip()
    if warning:
        facts.append(f"ПРЕДУПРЕЖДЕНИЕ вкладки: {warning}")
    return facts


# ─── Факты P14 «ФОТ по направлениям бизнеса» (лист 4 деки) ────────────────────

def _facts_p14(data: dict) -> list[str]:
    """Лист направлений: таблица-слайд, доли выручки, бейджи п.п., ПЧ/ФЧ."""
    facts: list[str] = []
    cards = data.get("cards") or []
    cur = str((data.get("meta") or {}).get("currency") or "BYN")
    slide = (data.get("meta") or {}).get("slide") or {}
    where = str(slide.get("label") or "срез")

    f25 = _card_value(cards, "fot_fact_2025")
    exp26 = _card_value(cards, "fot_expect_2026")
    dev = _card_value(cards, "fot_dev_year")
    if f25 is not None:
        facts.append(f"Свод ({where}): ФОТ Факт 2025 = {_fmt(f25)} млн {cur}")
    if exp26 is not None:
        facts.append(f"Свод ({where}): ФОТ Ожидание 2026 = {_fmt(exp26)} млн {cur}")
    if dev is not None:
        facts.append(f"Свод ({where}): Δ ФОТ год к 2025 = {_fmt(dev)}%")
    hc = data.get("headcount") or {}
    if hc.get("plan_approved") is not None and hc.get("fact_2026") is not None:
        facts.append(f"Численность ({where}): ПЧ 2026 = {_fmt(_num(hc['plan_approved']))} "
                     f"ставок, ФЧ 1ПГ26 = {_fmt(_num(hc['fact_2026']))}"
                     + (f", укомплектованность {_fmt(_num(hc['staffing']))}%"
                        if hc.get("staffing") is not None else ""))

    dirs = [d for d in (data.get("directions") or [])
            if isinstance(d, dict) and str(d.get("label") or "") != "(без направления)"]
    exps = [(str(d.get("label") or "?"), _num(d.get("exp26"))) for d in dirs]
    exps = [(lbl, v) for lbl, v in exps if v is not None]
    if exps:
        total = sum(v for _, v in exps)
        top_lbl, top_v = max(exps, key=lambda p: p[1])
        if total:
            facts.append(f"Направления: наибольшее Ожидание 2026 — {top_lbl} "
                         f"{_fmt(top_v)} тыс {cur} ({_fmt(top_v / total * 100)}% среза)")

    devs = [(str(d.get("label") or "?"), _num(d.get("dev_year_pct")),
             _num(d.get("dev_year_abs"))) for d in dirs]
    devs = [t for t in devs if t[1] is not None]
    if devs:
        hi_lbl, hi, hi_abs = max(devs, key=lambda p: p[1])
        lo_lbl, lo, _lo_abs = min(devs, key=lambda p: p[1])
        facts.append(f"Направления, Δ год к 2025: максимальный рост — {hi_lbl} "
                     f"{_fmt(hi)}% ({_fmt(hi_abs)} тыс)")
        if hi_lbl != lo_lbl:
            lo_word = "падение" if lo < 0 else "минимальный рост"
            facts.append(f"Направления, Δ год к 2025: {lo_word} — {lo_lbl} {_fmt(lo)}%")

    rising = [str(d.get("label")) for d in dirs
              if (_num(d.get("pp_year")) or 0) > 0]
    if rising:
        facts.append(f"Направления с РАСТУЩЕЙ долей ФОТ в выручке (Ожидание 2026 к "
                     f"Факту 2025, пп): {', '.join(rising)}")
    sh = [(str(d.get("label") or "?"), _num(d.get("share_h1_25")),
           _num(d.get("share_exp"))) for d in dirs]
    sh = [t for t in sh if t[1] is not None and t[2] is not None]
    if sh:
        top_lbl, top_s25, top_s26 = max(sh, key=lambda p: max(p[1], p[2]))
        facts.append(f"Самая высокая доля ФОТ в выручке направления — {top_lbl}: "
                     f"{_fmt(max(top_s25, top_s26))}% (Факт 1ПГ25 {_fmt(top_s25)}%, "
                     f"Ожидание 2026 {_fmt(top_s26)}%)")
    staff = [(str(d.get("label") or "?"), _num(d.get("hc_staffing")),
              _num(d.get("hc_dev"))) for d in dirs]
    staff = [t for t in staff if t[1] is not None]
    if staff:
        lo_lbl, lo_s, lo_dev = min(staff, key=lambda p: p[1])
        facts.append(f"Численность направлений: минимальная укомплектованность — "
                     f"{lo_lbl} {_fmt(lo_s)}%"
                     + (f" (Откл. ФЧ к 1ПГ25 {_fmt(lo_dev)} ставок)"
                        if lo_dev is not None else ""))

    warning = str((data.get("meta") or {}).get("warning") or "").strip()
    if warning:
        facts.append(f"ПРЕДУПРЕЖДЕНИЕ вкладки: {warning}")
    return facts


_TAB_FACTS = {
    "p9": _facts_p9,
    "p10": _facts_p10,
    "p11": _facts_p11,
    "p12": _facts_p12,
    "p13": _facts_p13,
    "p14": _facts_p14,
}


# ─── Пул чисел и кэш ─────────────────────────────────────────────────────────

def _numbers_pool(data: dict, facts: list[str]) -> list[float]:
    """Числа, которые модель имеет право называть: из фактов и payload."""
    pool = [float(m) for f in facts
            for m in re.findall(r"-?\d+(?:\.\d+)?", f)]

    def walk(value: Any) -> None:
        if isinstance(value, bool) or value is None:
            return
        if isinstance(value, (int, float)):
            pool.append(float(value))
        elif isinstance(value, dict):
            for item in value.values():
                walk(item)
        elif isinstance(value, list):
            for item in value:
                walk(item)

    for key in ("cards", "tables", "charts"):
        walk((data.get(key) or []))
    return pool


def _request_hash(block: str, block_title: str, context: dict,
                  data: dict) -> str:
    """Ключ кэша fot_*: контекст + модель + ДАЙДЖЕСТ собранных данных.

    Данные собирает сервер, поэтому в хеш идёт не charts с фронта, а след
    самого payload: после обновления витрин digest меняется и кэш честно
    пересчитывается, а не отдаёт наблюдения по прошлым цифрам.
    """
    blob = json.dumps(data, ensure_ascii=False, sort_keys=True,
                      separators=(",", ":"), default=str)
    digest = hashlib.sha256(blob.encode("utf-8")).hexdigest()
    payload = {"kind": "fot", "block": block, "title": block_title,
               "context": context, "digest": digest,
               "model": insights._model()}
    blob = json.dumps(payload, ensure_ascii=False, sort_keys=True,
                      separators=(",", ":"), default=str)
    return hashlib.sha256(blob.encode("utf-8")).hexdigest()


# ─── Основной сценарий ───────────────────────────────────────────────────────

async def _analyze_fot(block: str, tab: str, block_title: str,
                       data: dict, context: dict) -> dict:
    """Разбор собранной вкладки. Контракт ответа — как у insights.analyze."""
    from app import llm_settings

    started = time.monotonic()
    # Кэш настроек живёт секунды: правка в админке применяется без рестарта.
    await llm_settings.refresh()
    facts = _TAB_FACTS[tab](data)

    result: dict[str, Any] = {
        "block": block,
        "facts": facts,
        "observations": [],
        "model": insights._model(),
        "degraded": False,
        "error": None,
    }

    def finish() -> dict:
        result["elapsed_ms"] = int((time.monotonic() - started) * 1000)
        return result

    if not facts:
        result["degraded"] = True
        result["error"] = "по вкладке нет числовых данных для разбора"
        return finish()

    if not insights.llm_enabled():
        result["degraded"] = True
        result["error"] = "разбор не настроен (COST_LLM_*)"
        return finish()

    try:
        observations, usage = await insights.call_llm(
            SYSTEM_PROMPT_FOT, _build_user_prompt(block_title, context,
                                                  facts, data))
    except insights.InsightError as exc:
        # Модель отвалилась — отдаём посчитанные факты, вкладка остаётся полезной.
        result["degraded"] = True
        result["error"] = str(exc)
        log(logging.WARNING, "fot_insights: разбор не удался", block=block,
            error=str(exc)[:200], model=insights._model())
        return finish()

    pool = _numbers_pool(data, facts)
    for obs in observations:
        bad = insights._verify_numbers(obs["text"], pool)
        obs["unverified_numbers"] = bad
        obs["numbers_ok"] = not bad

    result["observations"] = observations
    result["usage"] = usage
    return finish()


async def analyze_block(payload: dict, email: Any) -> dict:
    """Ветка fot_* для POST /insights/block. ValueError → наверху 400.

    Форма ответа и права — те же, что у себестоимостного разбора; отличие
    только в источнике данных (сервер сам собирает вкладку) и в хеше кэша.
    """
    block = str(payload.get("block") or "").strip()
    block_title = str(payload.get("block_title") or "").strip()
    context = payload.get("context") or {}
    if not isinstance(context, dict):
        raise ValueError("context должен быть объектом")

    tab = block[len("fot_"):].strip().lower()

    data = await _fot_dashboard(tab, _params_from_context(context))
    if tab not in _TAB_FACTS:
        # Чужая вкладка: fot.dashboard/mocks.fot_dashboard уже кинули ValueError;
        # эта страховка — на случай, если бэкенд отдаст вкладку без фактов.
        raise ValueError(f"fot: неизвестная вкладка {tab!r}")

    # В mock-режиме пользователя в заголовке может не быть: права отключены,
    # а журнал не должен падать на NOT NULL.
    who = email if isinstance(email, str) and email else "mock"

    req_hash = _request_hash(block, block_title, context, data)
    if not payload.get("force"):
        cached = await insights.cached_run(req_hash)
        if cached is not None:
            log(logging.INFO, "fot_insights: отдан кэш", block=block, user=who,
                cached_at=cached.get("cached_at"))
            return {"block": block, **cached}

    result = await _analyze_fot(block, tab, block_title, data, context)
    await insights.record_run(req_hash, block, block_title, who, context, result)

    usage = result.get("usage") or {}
    log(logging.INFO, "fot_insights: разбор вкладки", block=block, user=who,
        model=result.get("model"), degraded=result.get("degraded"),
        elapsed_ms=result.get("elapsed_ms"),
        prompt_tokens=usage.get("prompt_tokens"),
        completion_tokens=usage.get("completion_tokens"),
        cost=usage.get("cost"))

    return {"cached": False, **result}
