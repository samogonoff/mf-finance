"""Выгрузка ФОТ-дашборда презентацией PowerPoint (эндпоинт POST /fot/export/pptx).

Слайды деки собираются кодом из payload'ов вкладок ФОТ-дашборда — тех же, что
рендерит фронт: данные тянет fot_insights._fot_dashboard (то же ветвление
mock/live, что у GET /fot/dashboard). LLM внутри НЕ вызывается (прокси ~60 с
недопустим в выгрузке): «Наблюдения ИИ» приезжают готовым списком строк в теле
запроса, а вся арифметика уже посчитана сборщиками вкладок.

Оформление — реплика деки CFO_ALL_AGG_RS «Бюджет ФОТ 2026»: 16:9, винная
палитра, заголовки слайдов светлым винным, шапки таблиц винные с белым
текстом, зебра нечётных строк, числа справа в ru-формате; цвет текста
Δ-ячеек берётся из '<колонка>_color' payload'а (светофор #1C8A5A/#9E6B0F/
#C0392B, порт меры app/fot.py).

Вкладки строятся по общему контракту cards/tables (см. докстринг
fot_insights): пустая вкладка (P12/P13 сейчас отдают meta.status=
'not_implemented') даёт пропущенные слайды и примечание на титуле. Когда
сборщики вкладок оживут, дека подхватит их без правок этого модуля.
"""

from __future__ import annotations

import io
import logging
from datetime import date

try:
    from pptx import Presentation
    from pptx.chart.data import CategoryChartData
    from pptx.dml.color import RGBColor
    from pptx.enum.chart import XL_CHART_TYPE, XL_LABEL_POSITION
    from pptx.enum.shapes import MSO_SHAPE
    from pptx.enum.text import MSO_ANCHOR, PP_ALIGN
    from pptx.util import Inches, Pt
except ImportError as exc:  # python-pptx — опциональная фича (только выгрузка
    # деки): без неё работает весь раздел, кроме /fot/export/pptx. Стабы нужны
    # модульным константам-цветам и дефолтам аргументов (align=PP_ALIGN.RIGHT
    # вычисляется при import); остальные имена в рантайме зовёт только выгрузка
    # — см. гард в export_pptx.
    from types import SimpleNamespace

    Presentation = None
    RGBColor = lambda *args, **kwargs: None
    PP_ALIGN = SimpleNamespace(LEFT=None, CENTER=None, RIGHT=None, JUSTIFY=None)
    PPTX_IMPORT_ERROR = str(exc)
else:
    PPTX_IMPORT_ERROR = None

from app import fot_insights
from app.logship import log

# ─── Палитра деки «Бюджет ФОТ 2026» (реплика CFO_ALL_AGG_RS) ─────────────────

ACCENT = RGBColor(0x6B, 0x33, 0x47)       # акцент: шапки таблиц, числа карточек
ACCENT_SOFT = RGBColor(0x9B, 0x5A, 0x70)  # светлый винный — заголовки слайдов
CARD_BG = RGBColor(0xF8, 0xED, 0xF0)      # заливка карточек
ZEBRA = RGBColor(0xE6, 0xD2, 0xD9)        # зебра нечётных строк таблиц
BORDER = RGBColor(0xE0, 0xCB, 0xD3)       # бордеры, линии, сетка диаграммы
TEXT = RGBColor(0x3F, 0x3F, 0x46)         # основной текст
MUTED = RGBColor(0x6B, 0x72, 0x80)        # вторичный текст
WHITE = RGBColor(0xFF, 0xFF, 0xFF)
DELTA_GREEN = RGBColor(0x1C, 0x8A, 0x5A)  # светофор: рост в пределах порога
DELTA_AMBER = RGBColor(0x9E, 0x6B, 0x0F)  # рост выше порога без роста доли
DELTA_RED = RGBColor(0xC0, 0x39, 0x2B)    # зона контроля

FONT = "Arial"  # уточнить при ревью — в деке Cambria

# Геометрия слайда 16:9 (дюймы)
SLIDE_W, SLIDE_H = 13.333, 7.5
MARGIN = 0.32
CONTENT_W = SLIDE_W - 2 * MARGIN

# Шрифты таблиц: широкие (P10/P11-матрица, 12 колонок) — мелкий кегль
_WIDE_FS, _NARROW_FS = 9, 10

_MODE_LABELS = {"full": "Полная версия", "short": "Краткая версия"}

# Заголовки слайдов вкладок и названия вкладок для примечания на титуле
_TAB_TITLES = {
    "p11": "Направления и подразделения",
    "p12": "Численность план и факт",
    "p13": "HR-бюджет и отделы",
}
_TAB_NAMES = {"p12": "Численность", "p13": "HR-бюджет"}

# ─── Строка фильтров для титула ──────────────────────────────────────────────

# Порядок показа срезов: сначала скаляры панели, потом списочные срезы
# (повторяют параметры GET /fot/dashboard).
_FILTER_LABELS = (
    ("currency", "Валюта"), ("scenario_plan", "План"), ("intercompany", "ВГО"),
    ("year", "Год"), ("month", "Месяц"), ("country", "Страна"),
    ("legal_entity", "Юрлицо"), ("direction", "Направление"),
    ("cfo_group", "Группа ЦФО"), ("cfo_subgroup", "Подгруппа ЦФО"),
    ("cfo", "ЦФО"), ("matrix_path", "Матрица"), ("channel_path", "Каналы"),
)
# Значения кодов — человеческие подписи (те же справочники, что у app/fot.py).
_COUNTRY_LABELS = {"BY": "Беларусь", "RU": "Россия", "KZ": "Казахстан", "UZ": "Узбекистан"}
_SCENARIO_LABELS = {"бюджет": "Бюджет", "тактика": "Тактика", "НовСтрат": "Новая стратегия"}
_IC_LABELS = {"0": "без ВГО", "1": "ВГО", "all": "все"}


def _filter_value(key: str, raw: str) -> str:
    if key == "country":
        return _COUNTRY_LABELS.get(raw.upper(), raw)
    if key == "scenario_plan":
        return _SCENARIO_LABELS.get(raw, raw)
    if key == "intercompany":
        return _IC_LABELS.get(raw, raw)
    return raw


def _filters_line(filters: dict) -> str | None:
    """Срезы запроса → одна строка для титула; пусто — фильтры не применены."""
    parts: list[str] = []
    for key, label in _FILTER_LABELS:
        value = (filters or {}).get(key)
        if value in (None, "", []):
            continue
        raw_items = value if isinstance(value, (list, tuple)) else [value]
        items = [_filter_value(key, str(v).strip()) for v in raw_items if str(v).strip()]
        if items:
            parts.append(f"{label}: {', '.join(items)}")
    return "Срезы: " + " · ".join(parts) if parts else None


# ─── Форматирование чисел (ru: пробел-разряды, запятая-десятичная) ───────────

def _ru_num(value: float, dec: int = 0) -> str:
    return f"{value:,.{dec}f}".replace(",", " ").replace(".", ",")


def _is_num(value) -> bool:
    return isinstance(value, (int, float)) and not isinstance(value, bool)


def _fmt_cell(value, unit) -> str:
    """Ячейка таблицы: % — два знака, деньги/ставки — без копеек, пусто — тире."""
    if value is None:
        return "—"
    if _is_num(value):
        u = str(unit or "")
        if "%" in u:
            return _ru_num(float(value), 2) + " %"
        if "ставок" in u:
            return _ru_num(float(value), 2)
        return _ru_num(float(value), 0)
    return str(value)


def _fmt_card(card: dict) -> tuple[str, str]:
    """Карточка → (число, единица): млн и проценты с двумя знаками, тыс — целые."""
    unit = str(card.get("unit") or "")
    value = card.get("value")
    if value is None:
        return "—", unit
    if not _is_num(value):
        return str(value), unit
    if "%" in unit or unit.startswith("млн") or "ставок" in unit:
        return _ru_num(float(value), 2), unit
    return _ru_num(float(value), 0), unit


def _delta_color(raw) -> RGBColor | None:
    """Hex из '<колонка>_color' → цвет текста ячейки; кривой/чужой hex — None."""
    hexv = str(raw or "").strip().lstrip("#")
    if len(hexv) != 6:
        return None
    try:
        return RGBColor.from_string(hexv.upper())
    except ValueError:
        return None


def _tab_alive(payload) -> bool:
    """Вкладка с данными: не заглушка и в payload есть карточки или таблицы."""
    if not isinstance(payload, dict):
        return False
    if (payload.get("meta") or {}).get("status") == "not_implemented":
        return False
    return bool(payload.get("cards") or payload.get("tables"))


# ─── Примитивы слайда ────────────────────────────────────────────────────────

def _new_slide(prs: Presentation):
    return prs.slides.add_slide(prs.slide_layouts[6])  # макет 6 — пустой


def _put_note(slide, text: str, color, left: float, top: float, width: float,
              *, size: float = 11, bold: bool = False, italic: bool = False,
              height: float = 0.35) -> None:
    """Одноабзацный текстовый блок (заголовки, подписи, сноски)."""
    box = slide.shapes.add_textbox(Inches(left), Inches(top), Inches(width), Inches(height))
    tf = box.text_frame
    tf.word_wrap = True
    p = tf.paragraphs[0]
    run = p.add_run()
    run.text = text
    f = run.font
    f.name, f.size, f.bold, f.italic = FONT, Pt(size), bold, italic
    f.color.rgb = color


def _put_header(slide, title: str, subtitle: str | None = None,
                warning: str | None = None) -> float:
    """Заголовок слайда светлым винным + линейка + подзаголовок; → верх контента."""
    _put_note(slide, title, ACCENT_SOFT, MARGIN, 0.2, CONTENT_W, size=23, bold=True)
    rule = slide.shapes.add_shape(
        MSO_SHAPE.RECTANGLE, Inches(MARGIN + 0.02), Inches(0.82),
        Inches(CONTENT_W - 0.04), Inches(0.018))
    rule.fill.solid()
    rule.fill.fore_color.rgb = BORDER
    rule.line.fill.background()
    rule.shadow.inherit = False
    y = 0.92
    if subtitle:
        _put_note(slide, subtitle, MUTED, MARGIN + 0.02, y, CONTENT_W - 0.04, size=10.5)
        y += 0.26
    if warning:
        _put_note(slide, "Внимание: " + str(warning), DELTA_AMBER,
                  MARGIN + 0.02, y, CONTENT_W - 0.04, size=10)
        y += 0.24
    return y + 0.06


def _put_cards(slide, cards, left: float, top: float, width: float, *,
               height: float = 1.05, per_row: int = 5, gap: float = 0.12) -> None:
    """Карточки payload'а: скруглённые плашки «подпись / число+единица»."""
    items = [c for c in cards or [] if isinstance(c, dict)]
    card_w = (width - (per_row - 1) * gap) / per_row
    for idx, card in enumerate(items[: per_row * 2]):
        row, col = divmod(idx, per_row)
        x, y = left + col * (card_w + gap), top + row * (height + gap)
        shape = slide.shapes.add_shape(
            MSO_SHAPE.ROUNDED_RECTANGLE, Inches(x), Inches(y),
            Inches(card_w), Inches(height))
        shape.adjustments[0] = 0.10
        shape.fill.solid()
        shape.fill.fore_color.rgb = CARD_BG
        shape.line.color.rgb = BORDER
        shape.line.width = Pt(0.75)
        shape.shadow.inherit = False
        tf = shape.text_frame
        tf.word_wrap = True
        tf.vertical_anchor = MSO_ANCHOR.MIDDLE
        tf.margin_left = tf.margin_right = Inches(0.08)
        tf.margin_top = tf.margin_bottom = Inches(0.03)
        value, unit = _fmt_card(card)
        p1 = tf.paragraphs[0]
        r = p1.add_run()
        r.text = str(card.get("label") or "")
        r.font.name, r.font.size = FONT, Pt(9)
        r.font.color.rgb = MUTED
        p2 = tf.add_paragraph()
        r = p2.add_run()
        r.text = value
        r.font.name, r.font.size, r.font.bold = FONT, Pt(17), True
        r.font.color.rgb = ACCENT
        if unit:
            r2 = p2.add_run()
            r2.text = " " + unit
            r2.font.name, r2.font.size = FONT, Pt(9)
            r2.font.color.rgb = MUTED


def _set_cell(cell, text: str, size: float, *, bold: bool = False, color=TEXT,
              align=PP_ALIGN.RIGHT, fill=WHITE) -> None:
    cell.margin_left = cell.margin_right = Inches(0.04)
    cell.margin_top = cell.margin_bottom = Inches(0.01)
    cell.vertical_anchor = MSO_ANCHOR.MIDDLE
    cell.fill.solid()
    cell.fill.fore_color.rgb = fill
    tf = cell.text_frame
    tf.word_wrap = True
    p = tf.paragraphs[0]
    p.alignment = align
    run = p.add_run()
    run.text = text
    f = run.font
    f.name, f.size, f.bold = FONT, Pt(size), bold
    f.color.rgb = color


def _put_table(slide, table, left: float, top: float, width: float, *,
               max_rows: int | None = None, font_size: float | None = None,
               caption: str | None = None) -> float:
    """Таблица payload'а → нативная python-pptx таблица; → нижняя граница (дюймы).

    Шапка винная с белым текстом, зебра нечётных строк, числа справа, строка
    «Итого» — полужирная на светлом фоне. Цвет текста Δ-ячеек — по hex из
    '<колонка>_color'. Строки сверх max_rows не рисуются — вместо них сноска.
    """
    y = top
    if caption:
        _put_note(slide, caption, TEXT, left, y, width, size=11, bold=True, height=0.24)
        y += 0.26
    columns = [c for c in (table or {}).get("columns") or [] if isinstance(c, dict)]
    rows = [r for r in (table or {}).get("rows") or [] if isinstance(r, dict)]
    if not columns or not rows:
        _put_note(slide, "Нет данных.", MUTED, left, y, width, size=10, italic=True)
        return y + 0.28
    shown = rows[:max_rows] if max_rows else rows
    n_cols, n_rows = len(columns), len(shown) + 1
    wide = n_cols > 8
    fs = font_size or (_WIDE_FS if wide else _NARROW_FS)
    header_h = 0.48 if wide else 0.34
    data_h = 0.205 if wide else 0.26

    # Ширины колонок по весу содержимого (подпись первой колонки — шире)
    fmt = [[_fmt_cell(r.get(str(c.get("key"))), c.get("unit")) for c in columns]
           for r in shown]
    weights = []
    for i, c in enumerate(columns):
        lens = [len(str(c.get("label") or ""))] + [len(fmt[j][i]) for j in range(len(shown))]
        weights.append(min(max(lens) + (3 if i == 0 else 2), 22 if i == 0 else 13))
    col_w = [max(width * w / sum(weights), 0.52 if i else 1.15) for i, w in enumerate(weights)]
    col_w = [w * width / sum(col_w) for w in col_w]  # перенормировка после клампов

    frame = slide.shapes.add_table(
        n_rows, n_cols, Inches(left), Inches(y), Inches(width),
        Inches(header_h + len(shown) * data_h))
    tbl = frame.table
    tbl.first_row = False   # стилевые акценты дефолтного шаблона выключаем —
    tbl.horz_banding = False  # раскраска идёт из палитры деки, не из темы
    for i, w in enumerate(col_w):
        tbl.columns[i].width = Inches(w)
    tbl.rows[0].height = Inches(header_h)
    for j in range(1, n_rows):
        tbl.rows[j].height = Inches(data_h)

    for i, c in enumerate(columns):
        _set_cell(tbl.cell(0, i), str(c.get("label") or ""), fs, bold=True,
                  color=WHITE, align=PP_ALIGN.CENTER, fill=ACCENT)
    label_key = str(columns[0].get("key") or "")
    for j, r in enumerate(shown):
        itogo = str(r.get(label_key) or "").strip() == "Итого"
        base_fill = CARD_BG if itogo else (ZEBRA if j % 2 == 0 else WHITE)
        for i, c in enumerate(columns):
            key = str(c.get("key") or "")
            color = _delta_color(r.get(key + "_color")) or TEXT
            _set_cell(tbl.cell(j + 1, i), fmt[j][i], fs,
                      bold=itogo and i == 0, color=color,
                      align=PP_ALIGN.LEFT if i == 0 else PP_ALIGN.RIGHT,
                      fill=base_fill)
    y += header_h + len(shown) * data_h
    if max_rows and len(rows) > len(shown):
        _put_note(slide, f"Показаны первые {len(shown)} из {len(rows)} строк — "
                         f"полный разрез на дашборде", MUTED, left, y + 0.02, width,
                  size=8.5, italic=True, height=0.2)
        y += 0.22
    return y


def _put_bar_chart(slide, table, value_key: str, caption: str,
                   left: float, top: float, width: float, height: float) -> None:
    """Столбчатая диаграмма по одной колонке таблицы (без «Итого»); нет чисел — нет диаграммы."""
    rows = [r for r in (table or {}).get("rows") or [] if isinstance(r, dict)]
    points = [(str(r.get("label") or ""), r.get(value_key)) for r in rows
              if str(r.get("label") or "") not in ("", "Итого")]
    points = [(lbl, float(v)) for lbl, v in points if _is_num(v)]
    if not points:
        return
    _put_note(slide, caption, MUTED, left, top, width, size=10)
    chart_data = CategoryChartData()
    chart_data.categories = [lbl for lbl, _ in points]
    chart_data.add_series(caption, [v for _, v in points])
    frame = slide.shapes.add_chart(
        XL_CHART_TYPE.COLUMN_CLUSTERED, Inches(left), Inches(top + 0.24),
        Inches(width), Inches(max(height - 0.24, 1.2)), chart_data)
    chart = frame.chart
    chart.has_title = False
    chart.has_legend = False
    chart.font.name, chart.font.size = FONT, Pt(9)
    chart.font.color.rgb = TEXT
    plot = chart.plots[0]
    plot.gap_width = 70
    plot.has_data_labels = True
    labels = plot.data_labels
    labels.number_format, labels.number_format_is_linked = "#,##0", False
    labels.position = XL_LABEL_POSITION.OUTSIDE_END
    labels.font.name, labels.font.size = FONT, Pt(8.5)
    labels.font.color.rgb = TEXT
    series = plot.series[0]
    series.format.fill.solid()
    series.format.fill.fore_color.rgb = ACCENT
    value_axis = chart.value_axis
    value_axis.tick_labels.number_format = "#,##0"
    value_axis.tick_labels.number_format_is_linked = False
    value_axis.tick_labels.font.size = Pt(8)
    value_axis.has_major_gridlines = True
    value_axis.major_gridlines.format.line.color.rgb = BORDER
    value_axis.format.line.color.rgb = BORDER
    cat_axis = chart.category_axis
    cat_axis.tick_labels.font.size = Pt(9)
    cat_axis.format.line.color.rgb = BORDER


def _put_traffic_legend(slide, left: float, top: float, width: float) -> None:
    """Расшифровка светофора Δ-колонок (порт правила app/fot.py)."""
    _put_note(slide, "Светофор колонок Δ (рост к 2025):", TEXT, left, top, width,
              size=10, bold=True)
    items = (
        (DELTA_GREEN, "зелёный — рост в пределах +10 %"),
        (DELTA_AMBER, "янтарный — рост выше +10 % без роста доли ФОТ в выручке"),
        (DELTA_RED, "красный — рост выше +10 % при растущей доле ФОТ (зона контроля)"),
    )
    y = top + 0.3
    for color, text in items:
        _put_note(slide, "— " + text, color, left + 0.12, y, width - 0.12, size=10,
                  height=0.3)
        y += 0.34


# ─── Слайды деки ─────────────────────────────────────────────────────────────

def _slide_title(prs: Presentation, *, mode_label: str, filters_line: str | None,
                 missing_note: str | None) -> None:
    """Титул: название, версия выгрузки, дата, строка срезов, примечание."""
    slide = _new_slide(prs)
    band = slide.shapes.add_shape(MSO_SHAPE.RECTANGLE, Inches(0), Inches(0),
                                  Inches(SLIDE_W), Inches(0.14))
    band.fill.solid()
    band.fill.fore_color.rgb = ACCENT
    band.line.fill.background()
    band.shadow.inherit = False
    _put_note(slide, "Бюджет ФОТ 2026", ACCENT, 0.9, 2.05, 11.5, size=40, bold=True)
    _put_note(slide, mode_label, ACCENT_SOFT, 0.9, 3.1, 11.5, size=15, bold=True)
    _put_note(slide, "Сформировано " + date.today().strftime("%d.%m.%Y"),
              MUTED, 0.9, 3.56, 11.5, size=12)
    _put_note(slide, filters_line or "Все данные дашборда — срезы не применены",
              TEXT, 0.9, 4.3, 11.5, size=12, height=0.8)
    if missing_note:
        _put_note(slide, missing_note, MUTED, 0.9, 5.2, 11.5, size=12, italic=True)


def _slide_p10(prs: Presentation, data: dict) -> None:
    """«ФОТ свод по группе»: карточки P10 + таблица по странам + диаграмма."""
    slide = _new_slide(prs)
    meta = data.get("meta") or {}
    cur = str(meta.get("currency") or "BYN")
    y = _put_header(
        slide, "ФОТ свод по группе",
        subtitle=f"Полугодия фиксированные: 1ПГ — январь–июнь, 2ПГ — июль–декабрь. "
                 f"Таблица — суммы в тыс. {cur}, карточки — млн {cur}.",
        warning=meta.get("warning"))
    _put_cards(slide, data.get("cards"), MARGIN, y, CONTENT_W)
    y += 1.22
    table = fot_insights._table(data, "by_country")
    label = str((table or {}).get("label") or "ФОТ по странам")
    y = _put_table(slide, table, MARGIN, y, CONTENT_W, max_rows=8, caption=label)
    chart_h = max(SLIDE_H - 0.25 - (y + 0.2), 1.4)
    _put_bar_chart(slide, table, "exp26", f"ФОТ Ожидание 2026 по странам, тыс {cur}",
                   MARGIN, y + 0.2, 6.4, chart_h)
    _put_traffic_legend(slide, MARGIN + 6.8, y + 0.2, CONTENT_W - 6.8)


def _slide_p11(prs: Presentation, data: dict) -> None:
    """«Направления и подразделения»: три таблицы P11 на одном слайде."""
    slide = _new_slide(prs)
    meta = data.get("meta") or {}
    cur = str(meta.get("currency") or "BYN")
    y = _put_header(
        slide, "Направления и подразделения",
        subtitle=f"Суммы в тыс. {cur}; Δ — к аналогичному периоду 2025. "
                 f"Срезы панели сужают все таблицы.",
        warning=meta.get("warning"))
    _put_cards(slide, data.get("cards"), MARGIN, y, CONTENT_W)
    y += 1.2
    dept = fot_insights._table(data, "by_department")
    label = str((dept or {}).get("label") or "По подразделениям")
    y = _put_table(slide, dept, MARGIN, y, CONTENT_W, max_rows=7, caption=label)
    # Каналы сбыта и отделы — короткие таблицы (6 колонок) в два столбца
    half = (CONTENT_W - 0.3) / 2
    bottom = y + 0.12
    for key, left in (("by_channel", MARGIN), ("by_direction", MARGIN + half + 0.3)):
        tbl = fot_insights._table(data, key)
        _put_table(slide, tbl, left, bottom, half, max_rows=6, font_size=9,
                   caption=str((tbl or {}).get("label") or key))


def _slide_generic(prs: Presentation, data: dict, title: str) -> None:
    """Слайд «ожившей» вкладки (P12/P13): карточки и таблицы по общему контракту.

    Состав заранее неизвестен (сборщики вкладок пишет другой поток работ) —
    рендер идёт по контракту cards/tables без правок: что придёт в payload,
    то и ляжет на слайд; не влезшее по высоте остаётся на дашборде.
    """
    slide = _new_slide(prs)
    meta = data.get("meta") or {}
    cur = str(meta.get("currency") or "").strip()
    y = _put_header(slide, title,
                    subtitle=f"Валюта витрины: {cur}." if cur else None,
                    warning=meta.get("warning"))
    cards = data.get("cards") or []
    if cards:
        _put_cards(slide, cards, MARGIN, y, CONTENT_W)
        y += 1.25
    for table in data.get("tables") or []:
        if not isinstance(table, dict):
            continue
        if y > 6.5:
            _put_note(slide, "Остальные таблицы вкладки — на дашборде "
                             "(место на слайде закончилось).",
                      MUTED, MARGIN, y, CONTENT_W, size=9, italic=True, height=0.2)
            break
        label = str(table.get("label") or "")
        y = _put_table(slide, table, MARGIN, y, CONTENT_W, max_rows=12,
                       caption=label or None)


def _slide_observations(prs: Presentation, observations: list[str]) -> None:
    """Финальный слайд «Наблюдения ИИ»: готовые тексты; пусто — пометка."""
    slide = _new_slide(prs)
    _put_header(slide, "Наблюдения ИИ",
                subtitle="Готовые наблюдения «Разбора ИИ» дашборда — числа "
                         "посчитаны кодом по данным вкладок.")
    lines = [str(o).strip() for o in observations or [] if str(o).strip()]
    if not lines:
        _put_note(slide, "Наблюдения не сформированы — запустите «Разбор ИИ» на "
                         "дашборде и передайте их в выгрузку.",
                  MUTED, MARGIN, 1.5, CONTENT_W, size=12, italic=True)
        return
    box = slide.shapes.add_textbox(Inches(MARGIN), Inches(1.35),
                                   Inches(CONTENT_W), Inches(SLIDE_H - 1.7))
    tf = box.text_frame
    tf.word_wrap = True
    for i, line in enumerate(lines):
        p = tf.paragraphs[0] if i == 0 else tf.add_paragraph()
        p.space_after = Pt(10)
        run = p.add_run()
        run.text = f"{i + 1}. {line}"
        f = run.font
        f.name, f.size = FONT, Pt(12.5)
        f.color.rgb = TEXT


# ─── Сборка деки ─────────────────────────────────────────────────────────────

def build_deck(mode: str, payloads: dict, filters: dict,
               observations: list[str]) -> bytes:
    """Payload'ы вкладок → pptx-файл (bytes). Режимы full/short — см. докстринг модуля.

    Вкладки без данных пропускаются (кодифицированный рендер): в full на
    титуле остаётся примечание «Численность/HR-бюджет: вкладки в разработке»,
    в short слайд численности заменяется таблицами P11.
    """
    prs = Presentation()
    prs.slide_width = Inches(SLIDE_W)
    prs.slide_height = Inches(SLIDE_H)

    alive = {tab: _tab_alive(payloads.get(tab))
             for tab in ("p10", "p11", "p12", "p13")}
    missing = [_TAB_NAMES[tab] for tab in ("p12", "p13") if not alive[tab]] \
        if mode == "full" else ([] if alive["p12"] else [_TAB_NAMES["p12"]])
    missing_note = None
    if missing:
        joined = "/".join(missing)
        missing_note = (f"{joined}: вкладки в разработке" if len(missing) > 1
                        else f"{joined}: вкладка в разработке")

    _slide_title(prs, mode_label=_MODE_LABELS.get(mode, mode),
                 filters_line=_filters_line(filters or {}),
                 missing_note=missing_note)
    if alive["p10"]:
        _slide_p10(prs, payloads["p10"])
    if mode == "full":
        if alive["p11"]:
            _slide_p11(prs, payloads["p11"])
        if alive["p12"]:
            _slide_generic(prs, payloads["p12"], _TAB_TITLES["p12"])
        if alive["p13"]:
            _slide_generic(prs, payloads["p13"], _TAB_TITLES["p13"])
    else:
        # Краткая версия: численность P12, если жива; иначе — направления P11.
        if alive["p12"]:
            _slide_generic(prs, payloads["p12"], _TAB_TITLES["p12"])
        elif alive["p11"]:
            _slide_p11(prs, payloads["p11"])
    _slide_observations(prs, observations)

    buf = io.BytesIO()
    prs.save(buf)
    blob = buf.getvalue()
    log(logging.INFO, "fot_pptx: презентация собрана", mode=mode,
        slides=len(prs.slides), bytes=len(blob),
        tabs_alive=[t for t, ok in alive.items() if ok])
    return blob


async def _collect_tabs(mode: str, filters: dict) -> dict:
    """Сбор payload'ов вкладок тем же хелпером, что у «Разбора ИИ» и дашборда.

    Вкладка, не собравшаяся (ошибка сборщика), даёт None — дека строится из
    остального. В short P11 тянется только как замена умершей P12.
    """
    params = fot_insights._params_from_context(filters or {})
    payloads: dict = {}
    tabs = ("p10", "p11", "p12", "p13") if mode == "full" else ("p10", "p12")
    for tab in tabs:
        try:
            payloads[tab] = await fot_insights._fot_dashboard(tab, params)
        except Exception as exc:
            log(logging.WARNING, "fot_pptx: вкладка не собрана — слайд пропущен",
                tab=tab, error=str(exc)[:200])
            payloads[tab] = None
    if mode == "short" and not _tab_alive(payloads.get("p12")):
        try:
            payloads["p11"] = await fot_insights._fot_dashboard("p11", params)
        except Exception as exc:
            log(logging.WARNING, "fot_pptx: вкладка p11 не собрана (замена p12)",
                error=str(exc)[:200])
            payloads["p11"] = None
    return payloads


async def export_pptx(mode: str, filters: dict, observations: list) -> bytes:
    """Точка входа эндпоинта: сбор вкладок + дека; LLM-вызовов здесь нет."""
    if Presentation is None:
        raise RuntimeError(
            "python-pptx не установлен в образе python-cost (см. requirements.txt): "
            "пересоберите образ. Причина: %s" % PPTX_IMPORT_ERROR)
    payloads = await _collect_tabs(mode, filters or {})
    return build_deck(mode, payloads, filters or {}, list(observations or []))
