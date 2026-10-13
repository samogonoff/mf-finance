"""Разбор вкладок ФОТ-дашборда (fot_p9..fot_p13) языковой моделью.

Блок «Разбор ИИ» на ФОТ-дашборде работает через тот же эндпоинт
`POST /insights/block`, что и в себестоимости: диспетчеризация в
routes.analyze_block — строго по префиксу `fot_`, до validate_charts
(вкладки серии с фронта не присылают). Право остаётся `cost:insights`.

Отличие от app/insights.py: **данные вкладки бэкенд собирает сам**. Фронт
шлёт только block («fot_p9»..«fot_p13») и context с теми же срезами, что
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

P12/P13 пока отдают degraded «вкладка в разработке»: их сборщики ещё не
слиты; при появлении фактов добавляется одна функция в _TAB_FACTS.
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


# P12/P13 facts не пишем: их сборщики ещё не слиты, вкладки отдают degraded.
_TAB_FACTS = {
    "p9": _facts_p9,
    "p10": _facts_p10,
    "p11": _facts_p11,
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
    if tab in ("p12", "p13"):
        # Бэкенд вкладок ещё не слит: вместе с ним добавится факты-функция
        # в _TAB_FACTS, а до тех пор — внятный degraded вместо разбора.
        started = time.monotonic()
        result: dict[str, Any] = {
            "block": block, "facts": [], "observations": [],
            "model": insights._model(), "degraded": True,
            "error": f"вкладка {tab.upper()} в разработке — разбор появится "
                     f"вместе с её данными",
            "elapsed_ms": int((time.monotonic() - started) * 1000),
        }
        return {"cached": False, **result}

    data = await _fot_dashboard(tab, _params_from_context(context))
    if tab not in _TAB_FACTS:
        # Сюда попадаем, только если живой fot.dashboard отдал неизвестную
        # вкладку без ValueError (например, новую вкладку без фактов).
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
