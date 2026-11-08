"""Биндеры вкладки b3 «Подразделения по странам» → слайды деки 28 и 30.

Вкладка показывает ОДНО подразделение в двух видах:

  * свод (лист 28, прототип b3_summary) — KPI-лента подразделения с Δ%,
    панель ПЧ/ФЧ и карточки стран присутствия (по клику на карточку веб
    открывает детализацию);
  * статьи страны (лист 30, прототип b3_country) — та же лента по одной
    стране, линия-разделитель и карточки статей ФОТ («Выплаты работникам»,
    «Прочие расходы», «Вспомогательные расходы», «Амортизация», «Основные
    расходы», «Вложения»).

Отличие от веба, зафиксированное осознанно: на экране селектор `b3_dept`
выбирает ОДНО подразделение, а выгрузка перебирает все — либо все сразу (фильтр
«Направление» пуст → `dept_options` сборщика), либо одно, если срез панели
«Направление» сужен (см. `collect.b3_depts`). Причина — дека печатает по
слайду на срез, а не интерактивный экран: пользователь, заказавший «все листы»,
ждёт полный набор страниц деки.

Данные — те же сборщики, что у дашборда (`_dashboard_b3`): `cards` (KPI-лента),
`headcount` (ПЧ/ФЧ), `cards_by_country` (карточки стран), `articles` (статьи
страны). Подписи (формулировки деки, разбивка на раны) — из манифеста, цвета —
из `sign_colors`/`dept_colors`, hex'ов в коде нет.

Методика (передаётся в `meta.methodology` сборщика, здесь только для чтения):
срез «Направление» панели на сам b3-дашборд НЕ действует, суммы — все
расходные статьи бюджета направления, Δ% считается только от положительной
базы, «Тактика 2 ПГ» незакрытого полугодия — гибрид «факт до C + тактика
после».
"""

from __future__ import annotations

import logging
import re

from app.fot_deck_export import format as fmt
from app.fot_deck_export import shapes
from app.fot_deck_export.errors import EmptySlice
from app.logship import log

#: Периоды статьи карточки листа 30: (значение, база прошлого года, круг Δ%).
ARTICLE_BLOCKS = (("v1", "b1", "d1"), ("v2", "b2", "d2"), ("v3", "b3", "d3"))

#: Сколько карточек в сетке деки и сколько бывает живьём. Дека печатает
#: 4 карточки стран и 5 статей, живая витрина даёт до 5 и до 6 соответственно
#: (у «Генеральной администрации» пять стран; у части направлений ненулевые
#: «Вложения»). Разницу разворачивает клон блока последнего места сетки.
SUMMARY_CARDS = 4
SUMMARY_LIMIT = 5
ARTICLE_CARDS = 5
ARTICLE_LIMIT = 6

def _year_plan(payload: dict) -> int | None:
    """Год среза: из meta.periods, иначе из параметров запроса."""
    periods = (payload.get("meta") or {}).get("periods") or {}
    if periods.get("year_plan"):
        return int(periods["year_plan"])
    params = (payload.get("meta") or {}).get("params") or {}
    if params.get("year"):
        return int(params["year"])
    return None


def _currency(payload: dict) -> str:
    return str((payload.get("meta") or {}).get("currency") or "BYN")


def _dept(payload: dict, variant) -> str:
    """Подразделение слайда: из варианта реестра, иначе из meta."""
    if isinstance(variant, dict) and variant.get("dept"):
        return str(variant["dept"])
    return str((payload.get("meta") or {}).get("sel_dept") or "")


def _sign_color(token: str | None, sign: dict) -> str | None:
    """Токен стиля format.py → цвет деки (None — цвет не трогаем).

    Ноль на b3 нейтрален: круг «0,0» деки серый (решение 7 спецификации), а
    неположительная база даёт «н/д» тем же приглушённым серым.
    """
    if token in (None, fmt.TOKEN_NEUTRAL, fmt.TOKEN_ZERO):
        return None
    if token == fmt.TOKEN_NA:
        return sign.get("na_text")
    if token == fmt.TOKEN_UP:
        return sign.get("delta_up")
    if token == fmt.TOKEN_DOWN:
        return sign.get("delta_down")
    raise RuntimeError("fot_deck: неизвестный токен стиля %r" % (token,))


def _label_key(name: str) -> str:
    """Имя подписи в манифесте: ``card7_exp`` → ``cardN_exp``."""
    return re.sub(r"^card\d+_", "cardN_", name)


def _put_label(ctx, slide, name: str, *, year, prev=None, country=None,
               dept=None, cur=None, values=(), para_idx: int = 0) -> list[str] | None:
    """Пишет подпись шейпа по шаблону деки; нет шейпа/шаблона — None."""
    sh = shapes.find_shape(slide, name)
    if sh is None:
        return None
    key = name if ctx.has_label(name) else _label_key(name)
    if not ctx.has_label(key):
        return None
    if prev is None and year:
        prev = int(year) - 1
    paras = ctx.label(key, year=year, prev=prev, country=country, dept=dept,
                      cur=cur, values=values)
    if para_idx >= len(paras):
        raise RuntimeError("fot_deck: подпись %r короче, чем ожидает биндер "
                           "(абзацев %d, нужен %d)"
                           % (key, len(paras), para_idx + 1))
    runs = list(paras[para_idx])
    # у пятой карточки статьи знак отклонения вынесен ОТДЕЛЬНЫМ раном («+»,
    # «0,8», «%»), а биндер пишет значение целиком со знаком — без снятия
    # рана-знака строка напечаталась бы «+−3» (то же правило, что у p14)
    if len(runs) > 1 and runs[0].strip() in ("+", fmt.MINUS, "-"):
        runs[0] = ""
    shapes.set_text(sh, list(enumerate(runs)))
    return runs


def _money(value, digits: int = 0) -> str:
    """Денежное значение блока 3: целые тыс. с разрядами, без плюса."""
    text, _t = fmt.money(value, group=True, digits=digits, plus=False)
    return text


def _pct_nd(value) -> tuple[str, str | None]:
    """Δ% с правилом «н/д»: нет значения или база не положительна — «н/д».

    Считает сборщик (`_b3_assemble`), сюда приходит готовое число или None —
    биндер правило не повторяет, а только печатает.
    """
    if value is None:
        return fmt.NA, fmt.TOKEN_NA
    return fmt.signed(value, digits=1, group=False)


# ─── Шапка листов 28/30 ─────────────────────────────────────────────────────

def _fill_title(ctx, slide, *, country, dept, cur, year, suffix="") -> None:
    """Подзаголовок листа: бренд деки + подпись среза, собранная шаблоном."""
    title = shapes.require_shape(slide, "title")
    lead = ctx.const(ctx.proto_name, "title_lead")
    if lead and len(title.text_frame.paragraphs[0].runs) >= 2:
        shapes.set_run(title, 0, lead)
    if not ctx.has_label("title"):
        raise RuntimeError("fot_deck: у прототипа %r нет подписи title — "
                           "пересоберите актив" % ctx.proto_name)
    paras = ctx.label("title", country=country, dept=dept, cur=cur, year=year)
    subtitle = "".join(paras[0]) + suffix
    if len(title.text_frame.paragraphs[0].runs) >= 2:
        shapes.set_run(title, 1, subtitle)
    else:
        shapes.set_run(title, 0, subtitle)


def _fill_badge(slide, text: str) -> None:
    """Бейдж листа: на своде — подразделение, на статьях — код страны."""
    badge = shapes.find_shape(slide, "badge")
    if badge is None:
        return
    shapes.set_run(badge, 0, str(text or ""))


def _badge_short(dept: str) -> str:
    """Короткая подпись бейджа свода: длинное название режется многоточием.

    У деки в бейдже «HR» — сокращение подразделения; живые названия длиннее
    («Генеральная администрация»), и в круг диаметром 52 px они не влезают.
    Правило простое: до 9 знаков — как есть, иначе 8 знаков и «…» (то же, что
    делает веб в `b3BadgeShort`).
    """
    text = str(dept or "")
    return text if len(text) <= 9 else text[:8] + "…"


def _fill_unit_label(ctx, slide, cur: str) -> None:
    """Подпись единицы («тыс. BYN») — константа деки с прорезью валюты."""
    sh = shapes.find_shape(slide, "unit_label")
    if sh is None:
        return
    consts = ctx._engine.manifest["prototypes"][ctx.proto_name].get("consts") or {}  # noqa: SLF001
    text = consts.get("unit_label")
    if text:
        shapes.set_run(sh, 0, str(text).replace("{cur}", cur))


# ─── KPI-лента и панель ПЧ/ФЧ ───────────────────────────────────────────────

def _fill_kpi(ctx, slide, cards: dict, headcount: dict, *, year, cur) -> None:
    """Лента листов 28/30: пять блоков и подписи их отклонений.

    Состав ленты у деки постоянный: факт 1 ПГ прошлого года, факт 1 ПГ
    текущего (+Δ), факт 2 ПГ прошлого, тактика 2 ПГ текущего (+Δ), ожидание
    года (+Δ). Метки с годами собираются шаблоном, значения печатаются
    целыми тыс., Δ% — со знаком и цветом по правилу знака.
    """
    values = {"kpi1": cards.get("f25_h1"), "kpi2": cards.get("f26_h1"),
              "kpi3": cards.get("f25_h2"), "kpi4": cards.get("t26_h2"),
              "kpi5": cards.get("exp")}
    for name, value in values.items():
        _put_label(ctx, slide, name + "_label", year=year, cur=cur)
        sh = shapes.find_shape(slide, name + "_value")
        if sh is not None:
            shapes.set_run(sh, 0, _money(value))
    devs = {"kpi2": cards.get("d1"), "kpi4": cards.get("d2"),
            "kpi5": cards.get("dyear")}
    for name, value in devs.items():
        pct_text, token = _pct_nd(value)
        sh = shapes.find_shape(slide, name + "_dev")
        if sh is None:
            continue
        # знак «%» стоит в шаблоне деки сразу за прорезью, поэтому значение
        # передаётся числом (как во всех подписях выгрузки)
        if _put_label(ctx, slide, name + "_dev", year=year,
                      values=(pct_text.rstrip("%"),)) is not None:
            color = _sign_color(token, ctx.sign)
            if color:
                for idx in range(len(sh.text_frame.paragraphs[0].runs)):
                    shapes.paint_run(sh, color, run_idx=idx)
        else:
            shapes.set_run(sh, 0, pct_text)
    _fill_headcount(ctx, slide, headcount, year=year)


def _fill_headcount(ctx, slide, hc: dict, *, year) -> None:
    """Панель ПЧ/ФЧ: пять колонок «метка / значение» + разделители.

    Значения — с одним знаком и БЕЗ разрядов (дека так печатает численность:
    «39,5», «1,0»), отклонения — со знаком и цветом по правилу знака.
    """
    cells = (("hc1", hc.get("plan_approved"), False),
             ("hc2", hc.get("fact_prev"), False),
             ("hc3", hc.get("fact_cur"), False),
             ("hc4", hc.get("d_plan"), True),
             ("hc5", hc.get("d_year"), True))
    for name, value, signed in cells:
        _put_label(ctx, slide, name + "_label", year=year)
        sh = shapes.find_shape(slide, name + "_value")
        if sh is None:
            continue
        if signed:
            text, token = _pct_nd(value)
        else:
            text, token = fmt.money(value, group=False, digits=1, plus=False)
        shapes.set_run(sh, 0, text)
        color = _sign_color(token, ctx.sign)
        if color:
            shapes.paint_run(sh, color, run_idx=0)


# ─── Лист 28: свод подразделения ────────────────────────────────────────────

def bind_summary(ctx, payload: dict, variant=None) -> None:
    """Лист 28: «Бюджет ФОТ <подразделения> — свод по группе»."""
    meta = payload.get("meta") or {}
    dept = _dept(payload, variant)
    cards_by_country = payload.get("cards_by_country") or []
    if not dept or not cards_by_country:
        raise EmptySlice("b3: у подразделения %r нет стран — лист 28 не "
                         "строится" % (dept,))
    year = _year_plan(payload)
    cur = _currency(payload)
    slide = ctx.slide(0)
    _fill_title(ctx, slide, country=None, dept=dept, cur=cur, year=year)
    _fill_badge(slide, _badge_short(dept))
    _fill_unit_label(ctx, slide, cur)
    _fill_kpi(ctx, slide, payload.get("cards") or {},
              payload.get("headcount") or {}, year=year, cur=cur)
    _put_label(ctx, slide, "cards_head", year=year)
    _fill_country_cards(ctx, slide, cards_by_country, year=year, cur=cur)
    log(logging.INFO, "fot_deck: лист 28 собран", tab=ctx.tab, dept=dept,
        countries=len(cards_by_country))


def _fill_country_cards(ctx, slide, cards: list[dict], *, year, cur) -> None:
    """Карточки стран: заполняет места сетки, лишние — клонирует/удаляет.

    Дека печатает четыре карточки (BY/RU/KZ/UZ), живьём у направления бывает
    пять стран (Генеральная администрация, ФЭО: +TR). Тогда блок последнего
    места клонируется на свободное место сетки — вёрстка карточки остаётся
    декиной, а не собирается кодом. Стран меньше четырёх — лишние места
    удаляются целиком (пустая рамка на слайде выглядела бы как сбой).
    """
    _ensure_cards(ctx, slide, len(cards), SUMMARY_LIMIT)
    for idx, card in enumerate(cards, 1):
        prefix = "card%d_" % idx
        sh = shapes.find_shape(slide, prefix + "title")
        if sh is not None:
            # код страны и название — ДВА рана одного шейпа у деки, причём
            # отбивку («BY  Беларусь») держит ПЕРВЫЙ ран: она снята с деки в
            # consts прототипа, иначе на слайде вышло бы «BYБеларусь»
            sep = ctx.const(ctx.proto_name, "card_title_sep")
            shapes.set_run(sh, 0, str(card.get("cc") or "") + sep)
            if len(sh.text_frame.paragraphs[0].runs) > 1:
                shapes.set_run(sh, 1, str(card.get("label") or ""))
        _put_label(ctx, slide, prefix + "exp", year=year, cur=cur,
                   values=(_money(card.get("exp")),))
        for role, key in (("d1", "d1"), ("d2", "d2"), ("dyear", "dyear")):
            pct_text, token = _pct_nd(card.get(key))
            sh = shapes.find_shape(slide, prefix + role)
            if sh is None:
                continue
            if _put_label(ctx, slide, prefix + role, year=year,
                          values=(pct_text.rstrip("%"),)) is not None:
                color = _sign_color(token, ctx.sign)
                if color:
                    # красим ТОЛЬКО ран значения: у деки подпись периода
                    # («1 ПГ 26/25:  ») серая, а знак — цветной; покраска
                    # всех ранов делала подпись красной и давала отличие
                    # ~250 px на строку при реставрации
                    runs = sh.text_frame.paragraphs[0].runs
                    shapes.paint_run(sh, color, run_idx=len(runs) - 1)


# ─── Лист 30: статьи страны ─────────────────────────────────────────────────

def bind_country(ctx, payload: dict, variant=None) -> None:
    """Лист 30: «<Страна> — <подразделение>: статьи ФОТ (тыс <валюта>)»."""
    meta = payload.get("meta") or {}
    dept = _dept(payload, variant)
    country = (variant or {}).get("country") if isinstance(variant, dict) else None
    country = str(country or meta.get("sel_country") or "")
    articles = payload.get("articles") or []
    if not dept or not country or not articles:
        raise EmptySlice("b3: нет статей страны %r подразделения %r — лист 30 "
                         "не строится" % (country, dept))
    year = _year_plan(payload)
    cur = _currency(payload)
    label = _country_label(payload, country)
    slide = ctx.slide(0)
    _fill_title(ctx, slide, country=label, dept=dept, cur=cur, year=year)
    _fill_badge(slide, country)
    _fill_kpi(ctx, slide, payload.get("cards") or {},
              payload.get("headcount") or {}, year=year, cur=cur)
    _fill_article_cards(ctx, slide, articles, year=year, cur=cur)
    log(logging.INFO, "fot_deck: лист 30 собран", tab=ctx.tab, dept=dept,
        country=country, articles=len(articles))


def _country_label(payload: dict, cc: str) -> str:
    """Название страны листа («Беларусь»): из карточек среза, иначе код."""
    for card in payload.get("cards_by_country") or []:
        if str(card.get("cc") or "") == cc:
            return str(card.get("label") or cc)
    return cc


def _fill_article_cards(ctx, slide, articles: list[dict], *, year, cur) -> None:
    """Карточки статей: заголовок, три блока периодов, три круга Δ% и подписи.

    Статей у деки пять, живьём бывает шесть (у HR «Вложений» нет, у
    «Генеральной администрации» есть) — шестая разворачивается клоном блока.
    Пустых статей сборщик не отдаёт вовсе, поэтому меньше пяти — тоже норма:
    лишние места удаляются.
    """
    _ensure_cards(ctx, slide, len(articles), ARTICLE_LIMIT)
    for idx, art in enumerate(articles, 1):
        prefix = "card%d_" % idx
        sh = shapes.find_shape(slide, prefix + "title")
        if sh is not None:
            shapes.set_run(sh, 0, str(art.get("label") or ""))
        for para, (value_key, base_key, pct_key) in enumerate(ARTICLE_BLOCKS, 1):
            _put_label(ctx, slide, "%sp%d_label" % (prefix, para), year=year)
            _put_label(ctx, slide, "%sp%d_value" % (prefix, para), year=year,
                       cur=cur, values=(_money(art.get(value_key)),))
            _put_label(ctx, slide, "%sp%d_base" % (prefix, para), year=year,
                       values=(_money(art.get(base_key)),))
            pct_text, token = _pct_nd(art.get(pct_key))
            _put_circle(ctx, slide, prefix, para, pct_text, token, year=year)


# ─── Общие помощники карточек ───────────────────────────────────────────────

def _put_circle(ctx, slide, prefix: str, num: int, text: str,
                token: str | None, *, year) -> None:
    """Круг Δ%: значение, обводка по знаку и подпись периода под ним."""
    sh = shapes.find_shape(slide, "%spp%d_text" % (prefix, num))
    if sh is not None:
        # у круга статьи процент стоит ВНУТРИ («+14,0%»), у круга «п.п.»
        # листов 7/8 единица «п.п.» вынесена вторым абзацем — здесь один
        # абзац, поэтому знак % печатает биндер
        shapes.set_run(sh, 0, text if text == fmt.NA else text + "%")
        color = _sign_color(token, ctx.sign)
        if color:
            shapes.paint_run(sh, color, run_idx=0)
    circle = shapes.find_shape(slide, "%spp%d_circle" % (prefix, num))
    if circle is not None:
        # ноль и «н/д» деки — приглушённый серый: обводка круга в деке серая
        color = _sign_color(token, ctx.sign) or ctx.sign.get("na_text")
        shapes.paint_outline(circle, color)
    _put_label(ctx, slide, "%spp%d_cap" % (prefix, num), year=year)


def _ensure_cards(ctx, slide, need: int, capacity: int) -> None:
    """Приводит сетку карточек слайда к числу данных: клонирует или удаляет.

    Сетки деки фиксированы (4 карточки стран, 5 карточек статей), а данных
    бывает и больше, и меньше. Клонируется блок ПОСЛЕДНЕГО места: он получает
    имя следующего места и сдвигается на шаг сетки, снятый с деки и лежащий в
    манифесте (`card_grid`) — держать шаг в коде значило бы разъехаться с
    декой при правке её геометрии. Лишние места удаляются целиком, вместе с
    подложкой: пустые рамки на слайде выглядели бы как сбой.
    """
    grid = ctx.card_grid()
    if need > capacity:
        raise RuntimeError(
            "fot_deck: у прототипа %r мест %d, а данных %d — сетка не "
            "размечена под такой объём" % (ctx.proto_name, capacity, need))
    have = ctx.card_count()
    while have < need:
        src_prefix = "card%d_" % have
        dst_prefix = "card%d_" % (have + 1)
        protos = [sh for sh in slide.shapes if sh.name.startswith(src_prefix)]
        if not protos:
            raise RuntimeError("fot_deck: у прототипа %r нет блока %r для "
                               "клонирования" % (ctx.proto_name, src_prefix))
        dx, dy = _slot_delta(grid, have, have + 1)
        shapes.clone_shapes(slide, protos, src_prefix, dst_prefix,
                            dy=dy, dx=dx)
        have += 1
    for idx in range(need + 1, capacity + 1):
        _drop_card(slide, idx)


def _slot_delta(grid: dict, src_idx: int, dst_idx: int) -> tuple[int, int]:
    """Сдвиг между местами сетки: (dx, dy) в EMU.

    У сеток блока 3 место считается по рядам: у свода четыре карточки в ряд,
    у статей — 3 + 2, и шаги по X/Y свои. Поэтому координаты места считаются
    из манифеста: `per_row` карточек в ряду, шаг `dx` между соседними местами
    и `dy` между рядами.
    """
    per_row = int(grid.get("per_row") or 4)
    dx = int(grid.get("dx") or 0)
    dy = int(grid.get("dy") or 0)
    srow, scol = divmod(int(src_idx) - 1, per_row)
    drow, dcol = divmod(int(dst_idx) - 1, per_row)
    return (dcol - scol) * dx, (drow - srow) * dy


def _drop_card(slide, idx: int) -> None:
    """Удаляет все шейпы карточки N по префиксу имени (подложку — тоже)."""
    prefix = "card%d_" % idx
    for sh in list(slide.shapes):
        if sh.name.startswith(prefix):
            shapes.delete_shape(sh)


# ─── Варианты ───────────────────────────────────────────────────────────────

# Вариантов у листов блока 3 нет: страницу задаёт ПАРА «подразделение ×
# страна», и перебирает её движок выгрузки (engine.export_deck) — он
# асинхронный и собирает данные тем же сборщиком, что экран. Биндер получает
# готовый payload одного среза и берёт подразделение/страну из его `meta`
# (см. `_dept` и `bind_country`).


__all__ = ["ARTICLE_BLOCKS", "ARTICLE_CARDS", "ARTICLE_LIMIT",
           "SUMMARY_CARDS", "SUMMARY_LIMIT", "bind_country", "bind_summary"]