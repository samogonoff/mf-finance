"""Биндер вкладки b1 «ГК: итоги по группе» → слайды деки 1–3.

Дека показывает один и тот же блок тремя листами: ГОД (1), 1 ПГ (2), 2 ПГ (3) —
это «варианты переключателя» отчёта, и выгрузка печатает все три, как дека.
Слайд года берёт данные корня payload'а, полугодия — свой элемент `slides[]`
(каждый со своими карточками, таблицей и карточками стран: сборщик считает их
для СВОЕГО периода, поэтому фильтр «полугодие» не смешивает числа).

Раскладка повторяет деку, а не веб-сетку:
  * KPI-лента — ШЕСТЬ блоков (пятый несёт подпись «п.п. к предпериоду»);
  * панель численности справа — пять колонок «метка / значение»;
  * сетка карточек стран — 4 штуки; карточка страны без данных НЕ печатается
    (её шейпы удаляются), как у p17 пропускается страна без строк;
  * таблица «страна × периоды» — своя у каждого листа (у года 9 колонок, у
    полугодий 8 и 11), поэтому шапка пишется из манифеста, а не из кода.

Подписи (включая годы) — из манифеста (`labels`/`header_labels`): биндер не
держит формулировок деки и не считает, какой год куда печатать.
"""

from __future__ import annotations

import logging
import re

from app.fot_deck_export import format as fmt
from app.fot_deck_export import shapes
from app.fot_deck_export.errors import EmptySlice
from app.logship import log

#: Ключи ленты `slides[i].cards` → номер KPI-блока деки (порядок слайда).
#: Пятый блок деки несёт ещё и подпись «п.п. к предпериоду» (kpi5_sub).
#: У ЛИСТА ГОДА ключи другие (`fot_fact_2025`, `fot_share_2025`, …): сборщик
#: отдаёт корень payload'а, а не элемент `slides[]`, поэтому карта отдельная.
KPI_KEYS = ("fot_value_prev", "fot_share_prev", "fot_value", "fot_dev",
            "fot_share_cur", "revenue_dev")

#: Ключи KPI-ленты ЛИСТА ГОДА (корень payload'а b1) в порядке блоков деки.
KPI_KEYS_YEAR = ("fot_fact_2025", "fot_share_2025", "fot_expect_2026",
                 "fot_dev_year", "fot_share_2026", "revenue_dev_year")

#: Пятый блок: главное значение + подпись отклонения доли в выручке.
KPI_SUB_INDEX = 5

#: Колонки таблицы «страна × периоды» по листам деки: у года 9 колонок, у 1 ПГ
#: 8, у 2 ПГ 11. Порядок — деки, ключи — поля строки payload'а.
TABLE_KEYS = {
    "b1_year": ("label", "f25_h1", "f25_h2", "f25", "b26_h1", "f26_h1",
                "b26_h2", "t26_h2", "exp26"),
    "b1_h1": ("label", "f25_h1", "b26_h1", "f26_h1", "dev_abs_fb", "dev_pct_fb",
              "dev_abs_ff25", "dev_pct_ff25"),
    "b1_h2": ("label", "f25_h2", "f26_h1", "b26_h2", "t26_h2", "dev_abs_tf25",
              "dev_pct_tf25", "dev_abs_tb", "dev_pct_tb", "dev_abs_tf1h",
              "dev_pct_tf1h"),
}

#: Колонки, которые дека печатает ПРОЦЕНТОМ (остальные — деньги листа).
PCT_KEYS = frozenset({"dev_pct_fb", "dev_pct_ff25", "dev_pct_tf25",
                      "dev_pct_tb", "dev_pct_tf1h"})

#: Колонки АБСОЛЮТНОГО отклонения: печатаются СО ЗНАКОМ («+3,7», «−0,4»),
#: хотя это и не проценты. Декa отличает их от сумм («65,6») тем же плюсом,
#: каким помечает динамику в карточках, поэтому «+» здесь обязателен.
ABS_DEV_KEYS = frozenset({"dev_abs_fb", "dev_abs_ff25", "dev_abs_tf25",
                          "dev_abs_tb", "dev_abs_tf1h"})

#: Ключи строки, которых нет в колонках деки (служебные поля сборщика).
ROW_META_KEYS = frozenset({"total", "drill_value"}) | frozenset(
    f"{k}_color" for k in TABLE_KEYS["b1_h2"])

#: Пометка продолжения таблицы (как у p17).
CONT_SUFFIX = " (продолжение)"

#: Число карточек стран на листах 1–3 (сетка фиксирована декой).
CARD_COUNT = 4


def _half_key(slide_payload: dict) -> str:
    """Период слайда полугодия: «h1»/«h2» из самого payload'а."""
    return str(slide_payload.get("period") or "").strip().lower()


def _year_plan(payload: dict) -> int | None:
    """Год отчётного периода: из meta.periods, иначе из срезов запроса."""
    periods = (payload.get("meta") or {}).get("periods") or {}
    if periods.get("year_plan"):
        return int(periods["year_plan"])
    params = (payload.get("meta") or {}).get("params") or {}
    if params.get("year"):
        return int(params["year"])
    return None


def _sign_color(token: str | None, sign: dict) -> str | None:
    """Токен стиля format.py → цвет деки (None — цвет не трогаем).

    Ноль на листах 1–3 не светофор (`TOKEN_ZERO`): круг «0,00» деки серый.
    Токен приходит из `fmt.pp` и `fmt.signed`, и молча упасть на нём значило
    бы терять выгрузку из-за нормального нулевого отклонения.
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


def _unit_text(unit) -> str:
    """Подпись единицы KPI-блока: «млн BYN» → «  млн BYN» (два пробела деки).

    Дека отделяет единицу от числа ДВУМЯ пробелами в одном ране (ран 2 блока).
    Пробелы — её типографика, поэтому берутся здесь, а не в шаблоне подписи:
    шаблон описывает роль рана, а не отбивку.
    """
    text = str(unit or "").strip()
    return ("  " + text) if text else ""


def _card_rows(payload: dict, cc: str) -> bool:
    """Есть ли у страны данные в этом срезе (иначе карточка не печатается)."""
    for card in payload.get("cards_by_country") or []:
        if str(card.get("cc") or "").upper() == cc:
            return card.get("value") is not None
    return False


def _drop_card(slide, idx: int) -> None:
    """Удаляет ВСЕ шейпы карточки страны (bg, тексты, круги, подписи)."""
    prefix = "card%d_" % idx
    for sh in list(slide.shapes):
        if sh.name.startswith(prefix):
            shapes.delete_shape(sh)


def bind_year(ctx, payload: dict, variant=None) -> None:
    """Лист 1 деки: «Бюджет ФОТ — свод по странам группы» (прототип b1_year)."""
    _bind_slide(ctx, payload, slide_payload=None, variant=variant)


def bind_h1(ctx, payload: dict, variant=None) -> None:
    """Лист 2 деки: итоги 1 полугодия (прототип b1_h1)."""
    _bind_slide(ctx, payload, slide_payload=_half(payload, "h1"), variant=variant)


def bind_h2(ctx, payload: dict, variant=None) -> None:
    """Лист 3 деки: итоги 2 полугодия (прототип b1_h2)."""
    _bind_slide(ctx, payload, slide_payload=_half(payload, "h2"), variant=variant)


def _half(payload: dict, key: str) -> dict:
    """Элемент `slides[]` своего полугодия; нет — EmptySlice (не пустой слайд)."""
    for item in payload.get("slides") or []:
        if _half_key(item) == key:
            return item
    raise EmptySlice("b1: в данных нет блока %r — выгрузка полугодия невозможна"
                     % key)


def _bind_slide(ctx, payload: dict, slide_payload: dict | None,
                variant=None) -> None:
    """Общая часть трёх листов: шапка, KPI, панель ПЧ, карточки, таблица.

    `slide_payload` — данные полугодия; None означает лист ГОДА: у него корень
    payload'а и своя таблица (`tables[0]`), а не вложенный блок.
    """
    slide = ctx.slide(0)
    year = _year_plan(payload)
    prev = (year - 1) if year else None
    proto = ctx.proto_name

    if slide_payload is None:
        cards = payload.get("cards") or []
        country_cards = payload.get("cards_by_country") or []
        table = (payload.get("tables") or [{}])[0] or {}
        headcount = payload.get("headcount") or {}
    else:
        cards = slide_payload.get("cards") or []
        country_cards = slide_payload.get("cards_by_country") or []
        table = slide_payload.get("table") or {}
        headcount = slide_payload.get("headcount") or payload.get("headcount") or {}

    # ── шапка: подзаголовок и бейдж (год периода — в самом шаблоне деки)
    _fill_title(ctx, slide, year, prev, slide_payload)
    _fill_badge(ctx, slide, year, slide_payload)

    _fill_kpi(ctx, slide, cards, year, prev, _half_no(slide_payload))
    _fill_headcount(ctx, slide, headcount, year, prev,
                    _half_no(slide_payload))
    _fill_cards(ctx, slide, country_cards, year, prev)
    _fill_table(ctx, slide, table, year, prev, _half_no(slide_payload))
    _fill_cards_head(ctx, slide, year, prev, slide_payload)
    _fill_unit_label(ctx, slide)


def _fill_title(ctx, slide, year, prev, slide_payload) -> None:
    """Заголовок листа: бренд (константа деки) и подпись периода.

    Бренд «ГК «МАРК ФОРМЭЛЬ»  » лежит в `consts` — он один на всю деку и не
    зависит от среза. Поэтому пишем ОБА рана: первый из константы, второй из
    шаблона подписи. Иначе в выгрузке остался бы токен бренда.
    """
    title = shapes.require_shape(slide, "title")
    consts = ctx._engine.manifest["prototypes"][ctx.proto_name].get("consts") or {}  # noqa: SLF001
    lead = consts.get("title_lead")
    if lead and len(title.text_frame.paragraphs[0].runs) >= 2:
        shapes.set_run(title, 0, lead)
    subtitle = _subtitle(ctx, year, prev, slide_payload)
    if subtitle:
        shapes.set_run(title, 1, subtitle)


def _fill_badge(ctx, slide, year, slide_payload) -> None:
    """Бейдж листа: первый абзац — год (данные), второй — номер ПГ.

    У года бейдж однострочный, у полугодий — двухстрочный («1 ПГ» / «2026»);
    состав абзацев задаёт шаблон, поэтому пишем столько, сколько он вернул.
    """
    badge = shapes.require_shape(slide, "badge")
    paras = _badge(ctx, year, slide_payload)
    if not paras:
        return
    shapes.set_text(badge, list(enumerate(paras[0])))
    tf = badge.text_frame
    if len(paras) > 1 and len(tf.paragraphs) > 1:
        shapes.set_text(badge, list(enumerate(paras[1])), para_idx=1)
    elif len(tf.paragraphs) > 1 and tf.paragraphs[1].runs:
        # второй абзац бейджа у листа года пуст в деке — добиваем его пустой
        # строкой, чтобы плейсхолдер актива не уехал на слайд
        shapes.set_text(badge, [(0, "")], para_idx=1)


def _subtitle(ctx, year, prev, slide_payload) -> str:
    """Второй ран заголовка: подпись периода из шаблона деки.

    У листа года подпись статична («свод по странам группы»), у полугодий —
    «детализация N полугодия по странам», где N — номер полугодия. Форму
    задаёт манифест (`labels.title`), значения — здесь: подставлять в
    формулировку деки свои слова нельзя, иначе «1:1» разъедется на первом же
    изменении подписи.
    """
    if ctx.has_label("title"):
        paras = ctx.label("title", year=year, prev=prev,
                          values=(_half_no(slide_payload),))
        # у заголовка два рана одного абзаца: бренд и подпись
        marks = list(paras[0])
        if len(marks) >= 2:
            return marks[1]
        return "".join(marks)
    # у года шаблон может не сняться (в подписи нет ни года, ни чисел) —
    # тогда подпись остаётся декиной и в активе
    return ""


def _half_no(slide_payload) -> str:
    """Номер полугодия для подписи: «1»/«2» (у года — пусто)."""
    if not slide_payload:
        return ""
    key = _half_key(slide_payload)
    return key[1:] if key.startswith("h") else ""


def _badge(ctx, year, slide_payload) -> list[list[str]]:
    """Бейдж листа абзацами: у года — год, у полугодий — «N ПГ» и год.

    У листов 1–3 бейдж однострочный, у полугодий — ДВА абзаца («1 ПГ» / «2026»),
    поэтому пишем по шаблону абзацами, а не одним раном.
    """
    if ctx.has_label("badge"):
        keys = _slot_keys(ctx, "badge")
        values = (_half_no(slide_payload),) if "v1" in keys else ()
        return ctx.label("badge", year=year, prev=None, values=values)
    # шаблона нет — год пишется прямо (единственный случай: лист 1)
    return [[str(year) if year else ""]]


def _fill_kpi(ctx, slide, cards: list[dict], year, prev,
              half_no: str = "") -> None:
    """KPI-лента: шесть блоков; у пятого — главное значение и «п.п. к предпериоду».

    Подписи блоков берутся из шаблонов деки (у года и полугодий они разные:
    «ФАКТ, ГОД 2025» / «ФАКТ, 1 ПГ 2025»), а не из `label` payload'а:
    дашборд пишет их строчными и без периода деки.
    """
    by_key = {str(c.get("key")): c for c in cards if c.get("key")}
    cfg = ctx.fmt
    keys = KPI_KEYS if half_no else KPI_KEYS_YEAR
    for num, key in enumerate(keys, 1):
        card = by_key.get(key) or {}
        _put_kpi_label(ctx, slide, num, year, prev, half_no)
        text, _token = _kpi_value(key, card.get("value"), cfg)
        value = shapes.require_shape(slide, "kpi%d_value" % num)
        shapes.set_run(value, 0, text)
        shapes.set_run(value, 1, _unit_text(card.get("unit")))

    # подпись под пятым блоком: «−3,7 п.п. к факту 2025»
    sub = shapes.find_shape(slide, "kpi5_sub")
    card = by_key.get(keys[KPI_SUB_INDEX - 1]) or {}
    if sub is not None and card.get("sub") is not None:
        text, _token = fmt.pp(card.get("sub"), int(cfg.get("pp_digits") or 2),
                              int(cfg.get("pp_zero_digits") or 2))
        values = [text]
        # у полугодий в подписи стоит ещё номер полугодия («к 1 ПГ 2025»)
        if "v2" in _slot_keys(ctx, "kpi5_sub"):
            values.append(_half_no_of(card))
        para = ctx.label("kpi5_sub", year=year, prev=prev, values=tuple(values))
        shapes.set_text(sub, list(enumerate(para[0])))
    elif sub is not None:
        shapes.delete_shape(sub)


def _slot_keys(ctx, name: str) -> set[str]:
    """Имена прорезей подписи (для необязательных значений вроде номера ПГ)."""
    meta = ctx._engine.manifest["prototypes"][ctx.proto_name]  # noqa: SLF001
    slots = (meta.get("labels") or {}).get(name) or []
    keys: set[str] = set()
    for slot in slots:
        for run in slot["runs"]:
            keys.update(fmt.slot_names(run["text"]))
    return keys


def _half_no_of(card: dict) -> str:
    """Номер полугодия из подписи карточки («п.п. к 1 ПГ 2025» → «1»)."""
    text = str(card.get("sub_label") or "")
    for part in text.split():
        if part.isdigit():
            return part
    return "2"


def _put_kpi_label(ctx, slide, num: int, year, prev, half_no: str) -> None:
    """Подпись KPI-блока по шаблону деки (у года и полугодий свои).

    Прорезь ``{v1}`` в подписях KPI — это номер полугодия («ФАКТ, {v1}ПГ 2025»);
    у года такой прорези нет вовсе, поэтому значение подставляется только когда
    шаблон её содержит.
    """
    name = "kpi%d_label" % num
    sh = shapes.require_shape(slide, name)
    if not ctx.has_label(name):
        # подпись без года и чисел в шаблоны не попадает — её текст статичен и
        # уже лежит в активе, трогать не нужно
        return
    keys = _slot_keys(ctx, name)
    values = (half_no,) if "v1" in keys else ()
    paras = ctx.label(name, year=year, prev=prev, values=values)
    shapes.set_text(sh, list(enumerate(paras[0])))


def _kpi_value(key: str, value, cfg: dict) -> tuple[str, str | None]:
    """Значение KPI-блока: суммы — млн без знака, динамика — со знаком.

    Сумма ФОТ («87,0 млн») плюса не несёт — плюс это признак ОТКЛОНЕНИЯ, а не
    значения; дека рисует «+» только у динамики. Знаки после запятой: у сумм —
    из типографики листа, у процентов динамики — один.
    """
    digits = int(cfg.get("cell_digits") or 1)
    if key in ("fot_dev", "revenue_dev", "fot_dev_year", "revenue_dev_year"):
        # динамика — ОТКЛОНЕНИЕ: плюс обязателен (у года ключи другие, но
        # смысл тот же — иначе на листе года пропадал «+»)
        return fmt.signed(value, digits=1)
    # суммы ФОТ и доли — БЕЗ плюса: плюс в деке означает отклонение
    return fmt.money(value, group=bool(cfg.get("cell_group")), digits=digits,
                     plus=False)


def _fill_headcount(ctx, slide, hc: dict, year, prev,
                    half_no: str = "") -> None:
    """Панель численности: пять колонок «метка / значение» + разделители.

    Метки — из шаблонов деки («ПЧ 2026», «ФЧ 1ПГ25», «Δ план»): они содержат
    год и номер полугодия, поэтому собираются подстановкой, а не пишутся
    кодом. Значения — целые/десятые без разрядов (дека так и печатает).
    """
    plan = hc.get("plan_approved")
    fact_prev = hc.get("fact_2025")
    fact_cur = hc.get("fact_2026")
    var_plan = hc.get("var_plan")
    var_prev = hc.get("var_2025")
    values = [
        ("hc1", plan, False),
        ("hc2", fact_prev, False),
        ("hc3", fact_cur, False),
        ("hc4", var_plan, True),
        ("hc5", var_prev, True),
    ]
    for name, value, signed in values:
        label = shapes.require_shape(slide, name + "_label")
        if ctx.has_label(name + "_label"):
            keys = _slot_keys(ctx, name + "_label")
            # номер полугодия в метках панели — у 2-й и 3-й колонки («ФЧ 1ПГ25»)
            pv = (half_no,) if "v1" in keys else ()
            para = ctx.label(name + "_label", year=year, prev=prev, values=pv)
            shapes.set_text(label, list(enumerate(para[0])))
        text, _token = (fmt.signed(value, digits=1) if signed
                        else fmt.money(value, group=True, digits=1, plus=False))
        shapes.set_run(shapes.require_shape(slide, name + "_value"), 0, text)


def _fill_unit_label(ctx, slide) -> None:
    """Подпись единицы листа («млн BYN») — константа деки из манифеста.

    Она не зависит от данных (её меняет только срез валюты, и тогда меняется
    весь лист), поэтому лежит в `consts` и в актив пишется как текст. Токен
    всё равно надо снять: в активе шейп обезличен.
    """
    sh = shapes.find_shape(slide, "unit_label")
    if sh is None:
        return
    consts = ctx._engine.manifest["prototypes"][ctx.proto_name].get("consts") or {}  # noqa: SLF001
    if "unit_label" in consts:
        shapes.set_run(sh, 0, consts["unit_label"])


def _fill_cards_head(ctx, slide, year, prev, slide_payload) -> None:
    """Подзаголовок блока карточек стран: формулировка деки с годами среза."""
    head = shapes.find_shape(slide, "cards_head")
    if head is None:
        return
    if not ctx.has_label("cards_head"):
        raise RuntimeError("fot_deck: у прототипа %r нет подписи cards_head — "
                           "пересоберите актив" % ctx.proto_name)
    # у полугодий в подписи стоит НОМЕР полугодия — он и есть {v1}
    values = (_half_no(slide_payload),) if slide_payload is not None else ()
    para = ctx.label("cards_head", year=year, prev=prev, values=values)
    shapes.set_text(head, list(enumerate(para[0])))


def _fill_cards(ctx, slide, cards: list[dict], year, prev) -> None:
    """Сетка карточек стран: 4 ячейки, у каждой — код, значение, доли, ПЧ и круг.

    Карточка без данных удаляется целиком (все её шейпы по префиксу имени), а
    остальные НЕ сдвигаются: дека размещает страны по местам, и перенос
    карточки на чужое место сломал бы вёрстку 1:1. Пустое место на листе —
    законный вид выгрузки, когда страна отфильтрована.

    У ЛИСТА ГОДА набор полей другой: сборщик (ветка `_p11_top`) отдаёт
    `exp26` (ожидание года) вместо `value` (ФОТ полугодия) и `dev_year`
    вместо `dev`. Раньше проверка «есть ли значение» смотрела только на
    `value`, и на листе года удалялись ВСЕ четыре карточки — лист выгрузки
    оставался без блока стран при живых данных.
    """
    by_cc = {str(c.get("cc") or "").upper(): c for c in cards if c.get("cc")}
    cfg = ctx.fmt
    for idx in range(1, CARD_COUNT + 1):
        code = _deck_card_code(ctx, idx)
        card = by_cc.get(code) if code else None
        if card is None or _card_value(card) is None:
            _drop_card(slide, idx)
            continue
        prefix = "card%d_" % idx
        shapes.set_run(shapes.require_shape(slide, prefix + "cc_code"), 0, code)
        _fill_card_body(ctx, slide, prefix, card, year, prev, cfg)


def _card_value(card: dict):
    """Значение карточки страны: `value` (полугодие) либо `exp26` (год).

    Одно поле из двух — по листу: у полугодий сборщик считает ФОТ периода
    (`value`), у года — ожидание года (`exp26`). Остальные поля карточки
    тоже названы по-разному, см. `_CARD_FIELDS`.
    """
    for key in ("value", "exp26"):
        if card.get(key) is not None:
            return card[key]
    return None


#: Имена полей карточки страны по листам: сборщик года (`_p11_top`) и
#: полугодий (`_b1_half_slide`) называют одни и те же величины по-разному.
#: Ключ — имя роли в биндере, значение — (поле года, поле полугодия).
_CARD_FIELDS = {
    "value": ("exp26", "value"),
    "dev": ("dev_year", "dev"),
    "dev_abs": ("dev_abs", "dev_abs"),
    "share_cur": ("rev_share_2026", "share_cur"),
    "share_prev": ("rev_share_2025", "share_prev"),
}


def _card_field(card: dict, role: str):
    """Поле карточки по роли: у года и полугодий разные имена (см. `_CARD_FIELDS`).

    Различие существенное: у года `rev_share_2026` — доля ФОТ в выручке
    ОЖИДАНИЯ года, у полугодия `share_cur` — доля факта полугодия. Поэтому
    подставлять одно вместо другого нельзя, и роль называется нейтрально.
    """
    year_key, half_key = _CARD_FIELDS.get(role, (role, role))
    for key in (year_key, half_key):
        if card.get(key) is not None:
            return card[key]
    return None


def _deck_card_code(ctx, idx: int) -> str:
    """Код страны, который дека ставит в карточку N (`card_slots` манифеста).

    Коды карточек в деке фиксированы (BY/RU/KZ/UZ по местам) — это геометрия
    листа, поэтому карта «место → страна» снята с деки в манифест при сборке
    актива, а не зашита здесь. Пустая строка — у прототипа карт стран нет.
    """
    meta = ctx._engine.manifest["prototypes"][ctx.proto_name]  # noqa: SLF001
    slots = meta.get("card_slots") or []
    if idx > len(slots):
        return ""
    return str(slots[idx - 1]).strip().upper()


def _label_key(name: str) -> str:
    """Имя подписи в манифесте: ``card3_cc_dev`` → ``cardN_cc_dev``.

    Подписи ячеек сетки свёрнуты по номеру места: формулировка у всех ячеек
    одна, номер места — геометрия, а не текст.
    """
    return re.sub(r"^card\d+_", "cardN_", name)


def _label_name(ctx, name: str) -> str:
    """Ключ подписи: точное имя ячейки, иначе свёрнутое по месту.

    Свёртывание возможно не всегда — подпись, которая у ячеек РАЗНАЯ
    («Динамика, п.п. (2026/2025): » одинакова, а значение своей ячейки нет),
    остаётся под конкретным номером места. Точное имя проверяется первым.
    """
    if ctx.has_label(name):
        return name
    return _label_key(name)


def _fill_card_body(ctx, slide, prefix: str, card: dict, year, prev,
                    cfg: dict) -> None:
    """Тело карточки страны: значение, динамика, доли, ПЧ/ФЧ, круг «п.п.».

    Набор ролей у года и полугодий РАЗНЫЙ (у года три доли и дельта до
    разделителя, у полугодий две доли и дельта под ним), поэтому каждая роль
    пишется только если шейп есть в этом прототипе.
    """
    digits = int(cfg.get("cell_digits") or 1)
    group = bool(cfg.get("cell_group"))

    def put(role: str, text: str) -> None:
        sh = shapes.find_shape(slide, prefix + role)
        if sh is not None:
            shapes.set_run(sh, 0, text)

    def put_label(role: str, values=()) -> bool:
        name = prefix + role
        sh = shapes.find_shape(slide, name)
        key = _label_name(ctx, name)
        if sh is None or not ctx.has_label(key):
            return False
        para = ctx.label(key, year=year, prev=prev, values=values)
        shapes.set_text(sh, list(enumerate(para[0])))
        return True

    # СУММА без плюса: «+» в деке — признак отклонения, а не значения
    value_text, _t = fmt.money(_card_field(card, "value"), group=group,
                               digits=digits, plus=False)
    put("cc_value", value_text + " млн")

    # подпись «Динамика, п.п. (2026/2025): » — формулировка деки с годами
    # среза; без неё на слайде оставался бы плейсхолдер актива
    put_label("cc_dyn_label")

    # динамика: у года «+8,2% к 2025 (+11,1 млн)», у полугодий
    # «к Факту 1ПГ25: +10,3% (+6,7 млн)» — оба из шаблона деки
    dev_pct, _t = fmt.signed(_card_field(card, "dev"), digits=1, group=False)
    # абсолютное отклонение — СО ЗНАКОМ («+11,1 млн», «−0,6 млн»): это
    # отклонение, а не сумма, плюс здесь обязателен
    dev_abs, _t = fmt.money(_card_field(card, "dev_abs"), group=group,
                            digits=digits)
    put_label("cc_dev", (dev_pct, dev_abs))

    # доли в выручке: у года их три (плюс доля в ФОТ группы), у полугодий — две
    _fill_share(ctx, slide, prefix, "cc_share1",
                _card_field(card, "share_cur"), year, prev)
    _fill_share(ctx, slide, prefix, "cc_share2",
                _card_field(card, "share_prev"), year, prev)
    _fill_share(ctx, slide, prefix, "cc_share3", card.get("fot_share"), year, prev)

    # ПЧ/ФЧ карточки страны: «ПЧ: 4 537,7   ФЧ 1ПГ 2026: 4 383,8» + «Откл. (Ф-П): −153,9»
    hc = shapes.find_shape(slide, prefix + "cc_hc")
    if hc is not None and ctx.has_label(_label_name(ctx, prefix + "cc_hc")):
        # ПЧ/ФЧ — СУММЫ (без плюса), отклонение — со знаком
        # пара ПЧ/ФЧ печатается ОБЫЧНЫМ пробелом в разрядах («4 537,7»), а не
        # неразрывным, как панель численности того же листа
        plan, _t = fmt.money(card.get("plan_approved"), group=True, digits=1,
                             plus=False, nbsp=False)
        fact, _t = fmt.money(card.get("fact_h1_2026"), group=True, digits=1,
                             plus=False, nbsp=False)
        var, _t = fmt.money(card.get("hc_var"), group=True, digits=1)
        paras = ctx.label(_label_name(ctx, prefix + "cc_hc"), year=year, prev=prev,
                          values=(plan, fact, var))
        shapes.set_text(hc, list(enumerate(paras[0])))
        if len(paras) > 1:
            # отклонение — ВТОРЫМ АБЗАЦЕМ, как в деке: склеивать абзацы
            # нельзя, у второго своя `spcBef` (отбивка 5 пт), и без неё
            # строка поднималась на 4 px — на картинке это видно, а по
            # тексту расхождения нет
            shapes.set_text_raw(hc, list(enumerate(paras[1])), para_idx=1)
            _paint_hc_var(ctx, hc)

    # круг «п.п.»: значение и цвет обводки по знаку отклонения доли
    _fill_pp_circle(ctx, slide, prefix, card, cfg)


def _paint_hc_var(ctx, shape) -> None:
    """Красит отклонение ПЧ/ФЧ по знаку (дека: снижение зелёное, рост красный).

    Второй абзац карточки страны — «Откл. (Ф-П): −153,9»: сама подпись серая,
    число — цветом знака. Токен берётся из текста значения, поэтому цвет
    остаётся верным и для «н/д».
    """
    tf = shape.text_frame
    if len(tf.paragraphs) < 2:
        return
    runs = tf.paragraphs[1].runs
    if len(runs) < 2:
        return
    _text, token = fmt.signed(_as_number(runs[-1].text), digits=1, group=True)
    color = _sign_color(token, ctx.sign)
    if color:
        shapes.paint_run(shape, color, para_idx=1, run_idx=len(runs) - 1)


def _as_number(text):
    """Текст рана → число (для повторного определения знака при покраске)."""
    raw = str(text or "").replace(fmt.NBSP, "").replace(fmt.MINUS, "-")
    raw = raw.replace(",", ".").strip()
    try:
        return float(raw)
    except ValueError:
        return None


def _fill_share(ctx, slide, prefix: str, role: str, value, year, prev) -> None:
    """Строка доли: подпись-шаблон деки + процент (число знаков листа).

    Знак «%» в шаблоне деки уже стоит после прорези («… (2026): {v1}%»),
    поэтому значение пишется ЧИСЛОМ: дописывать процент кодом значило бы
    держать в биндере пунктуацию подписи.
    """
    sh = shapes.find_shape(slide, prefix + role)
    if sh is None:
        return
    key = _label_name(ctx, prefix + role)
    if not ctx.has_label(key):
        return
    text, _t = fmt.money(value, group=False, digits=1, plus=False)
    para = ctx.label(key, year=year, prev=prev, values=(text,))
    shapes.set_text(sh, list(enumerate(para[0])))


def _fill_pp_circle(ctx, slide, prefix: str, card: dict, cfg: dict) -> None:
    """Круг «п.п.»: значение из шаблона деки, обводка — по знаку отклонения.

    Цвет обводки кодирует знак: рост доли красный, снижение зелёное, ноль и
    «н/д» — серый деки. Число берётся из `share_delta_pp`; ноль печатается
    полным числом знаков («0,00»), прочие — со срезанным хвостом («−0,1»).
    """
    text_sh = shapes.find_shape(slide, prefix + "pp_text")
    circle = shapes.find_shape(slide, prefix + "pp_circle")
    if text_sh is None:
        return
    value = card.get("share_delta_pp")
    text, token = fmt.pp(value, int(cfg.get("pp_digits") or 2),
                         int(cfg.get("pp_zero_digits") or 2))
    shapes.set_run(text_sh, 0, text)
    if circle is not None:
        color = (_sign_color(token, ctx.sign) if token != fmt.TOKEN_ZERO
                 else ctx.sign.get("na_text"))
        if color:
            shapes.paint_outline(circle, color)


def _fill_table(ctx, slide, table: dict, year=None, prev=None,
                half_no: str = "") -> None:
    """Таблица «страна × периоды»: шапка из манифеста, строки — данные среза."""
    name = ctx.table_names()[0] if ctx.table_names() else "tbl_countries"
    frame = shapes.find_shape(slide, name)
    if frame is None:
        return
    rows = table.get("rows") or []
    if not rows:
        # таблица без строк — не «пустой блок», а отсутствие данных: у листов
        # 1–3 она единственная, и печатать пустую сетку нельзя
        raise EmptySlice("%s: таблица стран пуста — данных среза нет"
                         % ctx.proto_name)
    keys = TABLE_KEYS[ctx.proto_name]
    spec = [{"style": "header",
             "cells": _header_cells(ctx, name, frames=len(keys), year=year,
                                    prev=prev, half_no=half_no)}]
    body_idx = 0
    for row in rows:
        cells = [_row_cell(ctx, key, row) for key in keys]
        if row.get("total"):
            # полоса итога ТЁМНАЯ: цвета те же по смыслу, но в СВЕТЛОЙ паре
            # деки (`delta_*_light`) — иначе на 6B3347 отклонения не читались
            # бы. `fill_table` выбирает пару по флагу `light` только для
            # токенов up/down, а `_row_sign` отдаёт готовый HEX, поэтому
            # оттенок берётся здесь
            spec.append({"style": "total", "cells": cells,
                         "signs": [_row_sign(ctx, key, row, light=True)
                                   for key in keys]})
            continue
        spec.append({"style": "body_tinted" if body_idx % 2 == 0 else "body_plain",
                     "cells": cells,
                     "signs": [_row_sign(ctx, key, row) for key in keys]})
        body_idx += 1
    shapes.fill_table(frame, spec, ctx.table_protos(frame, name), ctx.sign,
                      proto_row_count=ctx.deck_rows(name))
    log(logging.DEBUG, "fot_deck: таблица листа b1", tab=ctx.tab,
        proto=ctx.proto_name, rows=len(rows), keys=len(keys))


def _header_cells(ctx, table: str, frames: int, *, year=None, prev=None,
                  half_no: str = "") -> list[list[str]]:
    """Шапка таблицы по шаблонам деки; число колонок сверяется с картой ключей."""
    # подписи колонок содержат прорези «{vN}ПГ»: номер полугодия один на всю
    # шапку, поэтому в values он идёт первым (нумерация прорезей в шапке
    # сквозная, но нумерует их шаблон, а не биндер)
    labels = ctx.header_labels(table, year=year, prev=prev,
                               values=(half_no,))
    if len(labels) != frames:
        raise RuntimeError(
            "fot_deck: у прототипа %r шапка %d колонок, а карта листа %d — "
            "актив и биндер разошлись" % (ctx.proto_name, len(labels), frames))
    return labels


def _row_cell(ctx, key: str, row: dict) -> list[str]:
    """Ячейка строки: подпись, деньги листа (млн) или процент отклонения."""
    cfg = ctx.fmt
    if key == "label":
        return [str(row.get("label") or "")]
    value = row.get(key)
    digits = int(cfg.get("cell_digits") or 1)
    group = bool(cfg.get("cell_group"))
    if key in PCT_KEYS:
        # знак «%» дека печатает ПРЯМО В ЯЧЕЙКЕ («+5,4%»), а шапка называет
        # колонку («Откл. %») — в дампе слайдов 2/3 процент стоит у значения
        text, _t = fmt.signed(value, digits=1, group=False)
        return [text + "%"]
    if key in ABS_DEV_KEYS:
        # абсолютное отклонение — со знаком, но без процента («+3,7»)
        text, _t = fmt.money(value, group=group, digits=digits)
        return [text]
    # СУММЫ в таблице печатаются без плюса (плюс — признак отклонения)
    text, _t = fmt.money(value, group=group, digits=digits, plus=False)
    return [text]


def _row_sign(ctx, key: str, row: dict, light: bool = False) -> str | None:
    """УФ ячейки: отклонения красятся по знаку, значения — базовым цветом.

    Декa красит ОБЕ колонки каждой пары отклонений — и абсолютную, и
    процентную: у строки «+4,1 / +5,9%» обе ячейки красные, хотя знак у них
    один и тот же. Поэтому цвет считается по любому ключу отклонения, а не
    только по процентному.

    `light=True` — строка полосы ИТОГА: у деки там светлые оттенки
    (`delta_up_light` / `delta_down_light`), иначе цифры теряются на тёмной
    заливке 6B3347.
    """
    if key == "label":
        return None
    cfg = ctx.fmt
    value = row.get(key)
    if key in PCT_KEYS:
        _text, token = fmt.signed(value, digits=1, group=False)
    elif key in ABS_DEV_KEYS:
        # знак абс. отклонения берём у `signed`, а не у `money`: у сумм
        # токен всегда None (их не красят), и ячейка осталась бы нейтральной.
        # Ровный ноль `signed` отдаёт токеном `zero`, а `_sign_color` его
        # глушит — в деке «0,0» в колонке отклонения тоже базового цвета
        _text, token = fmt.signed(value, group=bool(cfg.get("cell_group")),
                                  digits=int(cfg.get("cell_digits") or 1))
    else:
        return None
    if light and token in (fmt.TOKEN_UP, fmt.TOKEN_DOWN):
        # на полосе итога дека печатает ОДИН светлый оттенок для любого знака
        # (снято с дек: у слайдов 2/3 все отклонения итога — F0D7DE, хотя
        # среди них есть и рост, и снижение). Поэтому пара delta_*_light
        # здесь не используется, а берётся общий тон полосы
        return ctx.sign.get("total_row_text")
    return _sign_color(token, ctx.sign)


__all__ = ["CARD_COUNT", "CONT_SUFFIX", "KPI_KEYS", "bind_h1", "bind_h2",
           "bind_year"]
