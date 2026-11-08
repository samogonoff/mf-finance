"""Сборка актива «дека-шаблон P17» из деки заказчика (задача 663620).

Дека «Марк_Формэль_Бюджет_ФОТ_2026_v16.pptx» обрезается до слайдов-прототипов
23–27 (вкладка p17 «Численность ГК»): прототипы получают семантические имена
шейпов, тексты заменяются плейсхолдерами «{{token}}», таблицы сжимаются до
строк-прототипов (шапка / tinted / plain / итог). Рядом кладутся два файла:

  app/assets/fot_deck/fot_deck_template.pptx  — сам актив (в репо, обезличенный);
  app/assets/fot_deck/manifest.json           — карта прототипов, ёмкости таблиц,
                                                цвета УФ, снятые с прототипов.

Актив — не «перерисованная дека», а её же XML: выгрузка (app/fot_deck_export)
клонирует прототипы и подставляет живые данные, поэтому вёрстка, шрифты,
заливки и геометрия остаются 1:1 с декой заказчика.

Флаг --dump-originals <path> дополнительно сохраняет JSON с ИСХОДНЫМИ текстами
(до обезличивания) — карту для реставрационного теста: актив + оригиналы →
pptx, равный слайдам 23–27 деки. Путь по умолчанию — /tmp/fot_deck_originals.json.
Класть этот файл внутрь репозитория запрещено: это выгрузка живых данных деки
заказчика (см. `_assert_outside_repo`), а не обезличенный актив.

Манифест получает `asset_sha256` — отпечаток самого файла актива: движок при
загрузке сверяет его и при несовпадении отказывается собирать выгрузку
(«актив не соответствует manifest — пересоберите»), вместо тихой сборки по
чужой вёрстке.

Запуск (на хосте нет python-pptx — гоняем в контейнере):

    docker cp deck.pptx swarm-python-cost-1:/tmp/deck_src.pptx
    docker exec -e PYTHONPATH=/var/www/cost swarm-python-cost-1 \
        python /var/www/cost/scripts/build_fot_deck_template.py --src /tmp/deck_src.pptx
"""

from __future__ import annotations

import argparse
import hashlib
import json
import os
import re
import sys
from datetime import datetime, timezone
from pathlib import Path

from lxml import etree
from pptx import Presentation
from pptx.oxml.ns import qn

#: Корень репозитория: сюда нельзя класть дампы с живыми данными деки (A3).
REPO_ROOT = Path(__file__).resolve().parents[3]

#: Разделитель разрядов деки — неразрывный пробел (U+00A0); по его наличию в
#: числовых текстах сборка определяет, группирует ли числа этот прототип.
NBSP = " "

# ─── Слайд-прототипы деки (1-based номера в исходнике) ───────────────────────

#: (номер слайда в исходной деке, имя прототипа в активе, ожидаемая подпись
#:  бейджа). Порядок = порядок слайдов в активе и в выгрузке: сначала листы
#:  блока 1 (1–3), затем отделы страны (7–8), затем численность ГК (23–27) —
#:  ровно как в деке заказчика.
PROTOS = (
    # блок 1 «ГК: итоги по группе» — год / 1ПГ / 2ПГ (слайды 1–3 деки)
    (1, "b1_year", ("2026",)),
    (2, "b1_h1", ("1 ПГ", "2026")),
    (3, "b1_h2", ("2 ПГ", "2026")),
    # ФОТ по направлениям бизнеса (слайды 4–6 деки): свод направлений,
    # производство по группам ЦФО, продажи по каналам. Прототипы BY-структуры:
    # для других стран меняются только заголовок и бейдж.
    (4, "nb_summary", ("BY",)),
    (5, "nb_prod", ("BY", "ПРОИЗВ.")),
    (6, "nb_sales", ("BY", "ПРОД.")),
    # отделы страны: свод (7) и анализ по подразделениям (8)
    (7, "dept_summary", ("BY", "ОТДЕЛЫ")),
    (8, "dept_cards", ("BY", "ОТДЕЛЫ")),
    # численность ГК (23–27)
    (23, "hc_summary", ("чел.",)),
    (24, "hc_country_by", ("BY",)),
    (25, "hc_country_ru", ("RU",)),
    (26, "hc_country_kz", ("KZ",)),
    (27, "hc_country_uz", ("UZ",)),
)

#: Слайды блока 1 (b1): раскладка «год → 1ПГ → 2ПГ». Меню KPI-ленты, панель
#: численности и карточки стран у них общие по структуре, но с РАЗНЫМ числом
#: ранов шапки бейджа (у года один ран «2026», у полугодий два — «1 ПГ»/«2026»).
B1_PROTOS = ("b1_year", "b1_h1", "b1_h2")

#: Слайды отделов (p15/p16): у них ДРУГОЙ набор цветов карточек, чем у b1/p17 —
#: подложка карточки F8F5F6 и разделитель D9C3CB, поэтому цвета снимаются с
#: этих прототипов отдельно и кладутся в манифест как `dept_colors`.
DEPT_PROTOS = ("dept_summary", "dept_cards")

#: Слайды направлений бизнеса (p14, листы 4–6 деки). Раскладка у них своя:
#: карточка направления несёт ТРИ круга Δ доли (а не два, как у отделов), у
#: листа 6 карточка уже (160 против 226), а данные рисуются ещё и диаграммами
#: (донат + мини-бары), которых нет ни у одного прототипа партии A.
NB_PROTOS = ("nb_summary", "nb_prod", "nb_sales")

#: Ожидаемые тексты KPI-лейблов (по ним же сверяется версия деки).
KPI_LABELS = (
    "УТВЕРЖД. ЧИСЛ., СТРАТ. 2026",
    "ФАКТ ЧИСЛ., 1 ПГ 2025",
    "ФАКТ ЧИСЛ., 1 ПГ 2026",
    "ОТКЛОНЕНИЕ К 2025",
    "ОТКЛОНЕНИЕ К УТВЕРЖД.",
)

#: Селектор KPI: числовой суффикс имени шейпа деки → имя в активе. Именно
#: суффикс имени («Text 3» → 3), а не cNvPr/@id: PowerPoint раздаёт @id
#: по-своему (на слайде 24 это 205…218), а имя с суффиксом копируется вместе
#: со слайдом и совпадает во всех пяти прототипах.
KPI_IDS = (("3", "kpi1"), ("6", "kpi2"), ("9", "kpi3"), ("12", "kpi4"), ("15", "kpi5"))

KIND_TEXT = "text"          # шейп с непустым текстом
KIND_ROUNDRECT = "roundrect"  # автофигура roundRect без текста (подложка)
KIND_ELLIPSE = "ellipse"    # круг без текста (круги Δ п.п. листов 1–3/7–8)
KIND_LINE = "line"          # линия-разделитель без текста
KIND_CHART = "chart"        # диаграмма (graphicFrame с c:chart)
#: Заливка первой ячейки строки → стиль строки (в деке ровно эти пять).
FILL_STYLES = {
    "9B5A70": "header",
    "F8EDF0": "body_tinted",
    "FFFFFF": "body_plain",
    "6B3347": "total",
    # строка-ГРУППА таблицы листа 6 («Отделы*», «ОПТ РБ»): своя заливка и
    # жирный текст. В вебе такой строке соответствует kind="direction".
    "E8D2D9": "group",
}

#: Таблицы прототипов в порядке документа.
TABLE_NAMES = {
    "hc_summary": ("tbl_countries",),
    "hc_country_kz": ("tbl_dept_a",),
    # листы 1–3: одна таблица «страна × периоды» на лист
    "b1_year": ("tbl_countries",),
    "b1_h1": ("tbl_countries",),
    "b1_h2": ("tbl_countries",),
    # листы 4–6: «направление/подразделение/канал × периоды», итог — ПЕРВОЙ
    # строкой (total_top), у листа 6 ещё и строки-группы (group)
    "nb_summary": ("tbl_directions",),
    "nb_prod": ("tbl_units",),
    "nb_sales": ("tbl_channels",),
    # лист 7: таблица «отдел × периоды», итог — ПЕРВОЙ строкой (total_top)
    "dept_summary": ("tbl_depts",),
    # лист 8 — сетка карточек, таблиц нет вовсе
    "dept_cards": (),
}
DEFAULT_TABLE_NAMES = ("tbl_dept_a", "tbl_dept_b")


# ─── Низкоуровневые хелперы по XML ───────────────────────────────────────────

def _cnvpr(shape_el) -> etree._Element | None:
    """p:cNvPr шейпа любого вида (sp / graphicFrame / pic)."""
    for path in ("p:nvSpPr", "p:nvGraphicFramePr", "p:nvPicPr", "p:nvGrpSpPr"):
        holder = shape_el.find(qn(path))
        if holder is not None:
            return holder.find(qn("p:cNvPr"))
    return None


def _shape_kind(shape) -> str:
    """Тип шейпа для селектора: text / roundrect / other.

    Текст читается ИЗ XML, а не через shape.text_frame: обращение к этому
    свойству заставляет python-pptx СОЗДАТЬ пустой <p:txBody> у фигуры, у
    которой его нет (линии-разделители деки), и актив мутирует — такой шейп
    начинает рендериться как текстовый (проверено на «Shape 5» слайда 24).
    """
    el = shape._element
    prst = el.find(".//" + qn("a:prstGeom"))
    txBody = el.find(qn("p:txBody"))
    has_text = False
    if txBody is not None:
        for t in txBody.iter(qn("a:t")):
            if (t.text or "").strip():
                has_text = True
                break
    if has_text:
        return KIND_TEXT
    # диаграмма живёт не в p:sp, а в p:graphicFrame и своего текста не имеет;
    # проверяем ДО геометрии — у graphicFrame prstGeom нет вовсе
    if el.find(".//" + qn("c:chart")) is not None:
        return KIND_CHART
    if prst is not None and prst.get("prst") == "roundRect":
        return KIND_ROUNDRECT
    if prst is not None and prst.get("prst") == "ellipse":
        return KIND_ELLIPSE
    if prst is not None and prst.get("prst") == "line":
        return KIND_LINE
    if txBody is not None:
        return KIND_TEXT
    return "other"


def _name_suffix(name: str) -> str | None:
    """Числовой суффикс имени шейпа деки: «Text 16» → «16»."""
    parts = str(name or "").split()
    if len(parts) >= 2 and parts[-1].isdigit():
        return parts[-1]
    return None


def _cell_fill(tc) -> str | None:
    """HEX заливки ячейки таблицы (a:tcPr/a:solidFill/a:srgbClr)."""
    tcPr = tc.find(qn("a:tcPr"))
    if tcPr is None:
        return None
    clr = tcPr.find(qn("a:solidFill") + "/" + qn("a:srgbClr"))
    return clr.get("val") if clr is not None else None


def _run_color(run) -> str | None:
    """HEX цвета текста рана (в деке везде srgbClr, темы нет).

    Принимает и объект python-pptx (`_r`), и «сырой» lxml-элемент a:r: часть
    сканеров ходит по XML напрямую (обращение к text_frame мутирует актив).
    """
    el = getattr(run, "_r", run)
    rPr = el.find(qn("a:rPr"))
    if rPr is None:
        return None
    clr = rPr.find(qn("a:solidFill") + "/" + qn("a:srgbClr"))
    return clr.get("val") if clr is not None else None


def _set_run_color(run, hex_color: str) -> None:
    """Красит ран в srgbClr, сохраняя остальные свойства rPr."""
    rPr = run._r.get_or_add_rPr()
    for old in rPr.findall(qn("a:solidFill")):
        rPr.remove(old)
    fill = etree.Element(qn("a:solidFill"))
    clr = etree.SubElement(fill, qn("a:srgbClr"))
    clr.set("val", hex_color)
    # solidFill идёт первым дочерним элементом rPr — так же, как в деке
    rPr.insert(0, fill)


def _row_first_fill(tbl, row_idx: int) -> str | None:
    rows = tbl._tbl.findall(qn("a:tr"))
    tc = rows[row_idx].find(qn("a:tc"))
    return _cell_fill(tc) if tc is not None else None


def _row_styles(tbl) -> list[str]:
    """Стили всех строк таблицы по заливке первой ячейки."""
    return [FILL_STYLES.get(_row_first_fill(tbl, i) or "", "other")
            for i in range(len(tbl.rows))]


def _row_colors(tbl) -> dict:
    """Заливки строк-прототипов: {стиль: hex} (по ним же красит движок)."""
    out: dict = {}
    for style in _row_styles(tbl):
        if style == "other":
            continue
        fill = FILL_STYLES_INV.get(style)
        if fill:
            out[style] = fill
    return out


FILL_STYLES_INV = {v: k for k, v in FILL_STYLES.items()}


def _cell_paragraph_runs(tc) -> list[list[str]]:
    """Ячейка → [[раны абзаца 1], [абзац 2], …]."""
    txBody = tc.find(qn("a:txBody"))
    if txBody is None:
        return [[""]]
    out = []
    for p in txBody.findall(qn("a:p")):
        out.append([t.text or "" for t in p.findall(qn("a:r") + "/" + qn("a:t"))])
    return out


# ─── Аннотация шейпов ────────────────────────────────────────────────────────

def _text_of(shape) -> str:
    """Текст шейпа БЕЗ обращения к shape.text_frame (см. _shape_kind).

    Чтение через text_frame заставляет python-pptx создать пустой <p:txBody> у
    фигур без него — линии-разделители деки после этого рендерятся как
    текстовые. Здесь читаем только существующий XML.
    """
    txBody = shape._element.find(qn("p:txBody"))
    if txBody is None:
        return ""
    out = []
    for p in txBody.findall(qn("a:p")):
        out.append("".join(t.text or "" for t in p.findall(qn("a:r") + "/" + qn("a:t"))))
    return "\n".join(out)


def _runs_of(shape, para_idx: int = 0) -> list:
    """Раны абзаца из XML (без text_frame)."""
    txBody = shape._element.find(qn("p:txBody"))
    if txBody is None:
        return []
    ps = txBody.findall(qn("a:p"))
    if para_idx >= len(ps):
        return []
    return ps[para_idx].findall(qn("a:r"))


def _para_count(shape) -> int:
    txBody = shape._element.find(qn("p:txBody"))
    return len(txBody.findall(qn("a:p"))) if txBody is not None else 0


def _paragraph_runs_xml(shape) -> list[dict]:
    """Абзацы шейпа из XML: [{"runs": [{"idx": i, "text": …}, …]}, …].

    Читаем напрямую: `shape.text_frame` вставляет пустой <p:txBody/> в фигуры
    без текста (линии-разделители деки) и актив после этого рендерится иначе.
    """
    txBody = shape._element.find(qn("p:txBody"))
    if txBody is None:
        return []
    out = []
    for p in txBody.findall(qn("a:p")):
        out.append({"runs": [{"idx": i, "text": t.text or ""}
                             for i, t in enumerate(p.findall(qn("a:r") + "/"
                                                             + qn("a:t")))]})
    return out


def _resolve(slide, proto_name: str, badge) -> dict:
    """Раскладывает шейпы прототипа по семантическим именам.

    Селектор — (числовой суффикс имени, тип шейпа, ожидаемый текст); каждый
    шейп занимается один раз, несовпадение любого пункта карты — AssertionError
    с диагностикой (шейп не найден / текст не тот).

    badge — ожидаемая подпись бейджа: строка (p17: «чел.», «BY») либо КОРТЕЖ
    абзацев (листы 1–3/7–8: бейдж двухстрочный — «1 ПГ» / «2026», «BY» /
    «ОТДЕЛЫ»). Второй вариант появился с партией A: у года в бейдже одна строка,
    у полугодий — две, и сравнивать надо весь набор абзацев.
    """
    shapes = list(slide.shapes)
    claimed: dict = {}
    badge_paras = ((badge,) if isinstance(badge, str) else tuple(badge))

    def take(spec_id: str, semantic: str, kind: str, expect) -> None:
        for sh in shapes:
            if id(sh._element) in claimed:
                continue
            if _name_suffix(sh.name) != spec_id:
                continue
            if _shape_kind(sh) != kind:
                continue
            got = _text_of(sh)
            if not expect(sh, got):
                continue
            claimed[id(sh._element)] = (semantic, sh)
            sh.name = semantic
            return
        seen = [(sh.name, _shape_kind(sh), _text_of(sh)[:48]) for sh in shapes]
        raise AssertionError(
            "прототип %s: не найден шейп id=%s (%s, %s). Шейпы слайда: %r"
            % (proto_name, spec_id, semantic, kind, seen))

    def is_label(expected: str):
        return lambda sh, got: got.strip() == expected

    def is_title(sh, got) -> bool:
        return (len(_runs_of(sh)) >= 2
                and got.lstrip().startswith("ГК «МАРК ФОРМЭЛЬ»"))

    def is_badge(sh, got) -> bool:
        """Бейдж: раны первого абзаца ровно совпадают с ожидаемыми строками."""
        paras = _paragraph_runs_xml(sh)
        if len(paras) != len(badge_paras):
            return False
        for para, want in zip(paras, badge_paras):
            text = "".join(r["text"] for r in para["runs"])
            if text.strip() != want:
                return False
        return True

    def is_kpi_value(sh, got) -> bool:
        # значение KPI — два рана: число и «  чел.»
        runs = _runs_of(sh)
        if len(runs) != 2:
            return False
        tail = "".join(t.text or "" for t in runs[1].findall(qn("a:t")))
        return tail.strip() == "чел."

    def is_note(sh, got) -> bool:
        return got.strip().startswith("чел.") and len(got) > 12

    def is_obs(sh, got) -> bool:
        return _para_count(sh) >= 2 and got.lstrip().startswith("НАБЛЮДЕНИЯ")

    def is_page(sh, got) -> bool:
        return len(_runs_of(sh)) == 1 and got.strip().isdigit()

    def is_total_bar(sh, got) -> bool:
        return len(_runs_of(sh)) >= 5 and got.lstrip().startswith("Итого (видимые позиции)")

    def no_text(sh, got) -> bool:
        return not got.strip()

    take("0", "title", KIND_TEXT, is_title)
    take("2", "badge", KIND_TEXT, is_badge)

    # KPI-лента: у p17 метки фиксированные (KPI_LABELS), у листов 1–3 свои
    # (B1_KPI_LABELS) — разбираются в _resolve_b1_extra вместе с карточками.
    if proto_name in B1_PROTOS:
        _resolve_b1_extra(proto_name, take, shapes)
    elif proto_name in NB_PROTOS:
        pass        # раскладка листов 4–6 своя — _resolve_nb_extra
    elif proto_name in DEPT_PROTOS:
        _resolve_dept_extra(proto_name, take, shapes)
    else:
        for idx, (spec_id, name) in enumerate(KPI_IDS):
            take(spec_id, name + "_label", KIND_TEXT, is_label(KPI_LABELS[idx]))
            take(str(int(spec_id) + 1), name + "_value", KIND_TEXT, is_kpi_value)

    if proto_name in NB_PROTOS:
        _resolve_nb_extra(proto_name, take, shapes)
    elif proto_name in B1_PROTOS or proto_name in DEPT_PROTOS:
        pass
    elif proto_name == "hc_summary":
        take("17", "note", KIND_TEXT, is_note)
        take("18", "obs_bg", KIND_ROUNDRECT, no_text)
        take("19", "obs", KIND_TEXT, is_obs)
        take("20", "page", KIND_TEXT, is_page)
    elif proto_name == "hc_country_kz":
        # у KZ итог живёт строкой в таблице, полосы total_bar нет
        take("3", "note", KIND_TEXT, is_note)
        take("4", "page", KIND_TEXT, is_page)
    else:
        take("3", "total_bg", KIND_ROUNDRECT, no_text)
        take("4", "total_bar", KIND_TEXT, is_total_bar)
        take("5", "note", KIND_TEXT, is_note)
        take("6", "page", KIND_TEXT, is_page)

    # В карту попадают ВСЕ переименованные шейпы: сетки карточек и панель
    # численности переименовываются напрямую (их ячеек нет в `claimed`), но
    # обезличивание и сборка манифеста должны видеть и их.
    return {sh.name: sh for sh in slide.shapes
            if _is_semantic_name(sh.name)}


#: Префиксы семантических имён, которые проставляет аннотация. Имена деки
#: остаются вида «Shape 5» / «Text 6» / «Table 0» — по ним и различаем.
SEMANTIC_PREFIXES = ("card", "kpi", "hc", "banner", "tbl_", "chart")


def _is_semantic_name(name: str) -> bool:
    """Семантическое ли имя у шейпа (имя деки содержит пробел: «Text 6»)."""
    if " " in str(name):
        return False
    return (name in _SEMANTIC_EXACT
            or any(name.startswith(p) for p in SEMANTIC_PREFIXES))


#: Точные семантические имена, которые не попадают под префиксы.
_SEMANTIC_EXACT = frozenset({
    "title", "subtitle", "badge", "note", "page", "obs", "obs_bg",
    "total_bar", "total_bg", "cards_head", "unit_note", "hc_note",
    "unit_label", "donut_head", "bars_head",
})


def _annotate_tables(slide, proto_name: str) -> dict:
    """Именует graphicFrame'ы таблиц в порядке документа."""
    names = TABLE_NAMES.get(proto_name, DEFAULT_TABLE_NAMES)
    frames = [sh for sh in slide.shapes if getattr(sh, "has_table", False) and sh.has_table]
    if len(frames) != len(names):
        raise AssertionError("прототип %s: таблиц %d, в карте %d (%r)"
                             % (proto_name, len(frames), len(names), names))
    out = {}
    for sh, name in zip(frames, names):
        sh.name = name
        out[name] = sh
    return out




# ─── Листы 1–3 (b1) и 7–8 (dept): разметка ПАТТЕРНАМИ ────────────────────────

#: Роли шейпов карточки листа 7 — порядок ДОКУМЕНТА деки (снят инвентарём
#: slide7.xml, суффиксы идут подряд от 5). Карточка — 190×240:
#:   рамка, имя отдела, разделитель, 3× (подпись / значение / дельта),
#:   между блоками — разделители, 3 круга Δ п.п. со значением и подписью,
#:   две строки численности ниже.
#: Роль — позиция в блоке, поэтому разметка не зависит от координат и от того,
#: какие отделы попали на лист: правка геометрии деку не сломает.
DEPT7_ROLES = (
    "bg",          # рамка roundRect
    "title",       # имя отдела
    "hr1",         # разделитель под заголовком
    "p1_label",    # «1 ПГ 2026»
    "p1_value",    # «7 300 тыс. BYN»
    "p1_dev",      # «+9,1% к факту 25 (+606)»
    "hr2",         # разделитель между первым и вторым периодом
    "p2_label",    # «ТАКТИКА 2 ПГ 2026»
    "p2_value",
    "p2_dev",
    "hr3",         # разделитель перед «Ожиданием»
    "p3_label",    # «ОЖИДАНИЕ, ГОД 2026»
    "p3_value",
    "p3_dev",
    "pp1_circle",  # круг Δ доли ФОТ в выручке, п.п.
    "pp1_text",    # «−0,2» / «п.п.» — два абзаца круга
    "pp1_cap",     # «к факту 25»
    "pp2_circle",
    "pp2_text",
    "pp2_cap",     # «к факту 2П25»
    "pp3_circle",
    "pp3_text",
    "pp3_cap",     # «Год/факт25»
    "hc_line",     # «ПЧ/ФЧ 1ПГ 2026: 372.4/348.9, 94% укомпл.»
    "hc_dev",      # «Откл. ФЧ к 1ПГ25: −31,5»
)

#: Число шейпов карточки листа 7 (см. DEPT7_ROLES).
DEPT7_SHAPES = len(DEPT7_ROLES)

#: Роли шейпов карточки листа 8 — сетка 4×4 по 188×187. Порядок в документе
#: ДРУГОЙ, чем у листа 7: строки численности («ПЧ/ФЧ: …» и «Откл. к 1П25: …»)
#: идут сразу после третьего блока значений, а круги «п.п.» замыкают карточку.
DEPT8_ROLES = (
    "bg",
    "title",
    "hr1",
    "p1_label",
    "p1_value",
    "p1_dev",
    "hr2",
    "p2_label",
    "p2_value",
    "p2_dev",
    "hr3",
    "p3_label",
    "p3_value",
    "p3_dev",
    "hc_line",     # «ПЧ/ФЧ: 24.5/24.5, 100%»
    "hc_dev",      # «Откл. к 1П25: −0,5»
    "pp1_circle",
    "pp1_text",
    "pp1_cap",
    "pp2_circle",
    "pp2_text",
    "pp2_cap",
    "pp3_circle",
    "pp3_text",
    "pp3_cap",
)

#: Число шейпов карточки листа 8 (см. DEPT8_ROLES).
DEPT8_SHAPES = len(DEPT8_ROLES)

# ─── Листы 4–6 деки (p14): направления бизнеса ────────────────────────────────

#: Роли шейпов БОЛЬШОЙ карточки направления (лист 4 деки, 226×244). Порядок
#: ДОКУМЕНТА (снят инвентарём slide4.xml), и он НЕ такой, как у карточек
#: отделов: сначала идут ТРИ КРУГА Δ доли с подписями, потом заголовок и блоки
#: «метка / значение / отклонение» с разделителями, затем две строки численности.
#:
#:   рамка, 3× (круг / значение / подпись), 3× (метка / значение / дельта)
#:   с разделителями ПЕРЕД вторым и третьим блоками, две строки численности.
NB_CARD_ROLES = (
    "bg",          # рамка roundRect
    "pp1_circle",  # круг Δ доли ФОТ в выручке, п.п.
    "pp1_text",    # «−1,6» / «п.п.» — два абзаца круга
    "pp1_cap",     # «к факту 1П25»
    "pp2_circle",
    "pp2_text",
    "pp2_cap",     # «к факту 2П25»
    "pp3_circle",
    "pp3_text",
    "pp3_cap",     # «Ожид.26 / Факт.год25»
    "p1_label",    # «1 ПГ 2026»
    "p1_value",    # «32 123 тыс. BYN»
    "p1_dev",      # «+4,0% к факту 2025 (+1 243)»
    "hr1",         # разделитель перед вторым блоком
    "p2_label",    # «ТАКТИКА 2 ПГ 2026»
    "p2_value",
    "p2_dev",
    "hr2",
    "p3_label",    # «ОЖИДАНИЕ, ГОД 2026»
    "p3_value",
    "p3_dev",
    "hc_line",     # «ПЧ/ФЧ 1ПГ 2026: 2 441,9/2 342,2, 96% укомпл.»
    "hc_dev",      # «Откл. ФЧ к 1ПГ25: +58,3»
)

#: Роли карточки листов 5 и 6 (та же ширина 226 у листа 5 и 160 у листа 6).
#: Порядок ДРУГОЙ: у этих карточек вверху только ДВА круга, третий замыкает
#: карточку после третьего блока значений, а строк численности в карточке нет
#: вовсе (численность листа стоит в шапке).
NB_CARD_ROLES_UNITS = (
    "bg",
    "pp1_circle",
    "pp1_text",
    "pp1_cap",
    "pp2_circle",
    "pp2_text",
    "pp2_cap",
    "p1_label",
    "p1_value",
    "p1_dev",
    "hr1",
    "p2_label",
    "p2_value",
    "p2_dev",
    "hr2",
    "p3_label",
    "p3_value",
    "p3_dev",
    "pp3_circle",
    "pp3_text",
    "pp3_cap",
)

#: Строки отклонения карточки: их генератор деки однажды вынес ИЗ блока.
#: У пятой карточки листа 6 («Интернет-магазин») три шейпа отклонений лежат в
#: самом конце spTree, а не внутри карточки (так собрал генератор деки; на
#: картинке они стоят на своих местах). Актив обязан повторить деку 1:1,
#: поэтому такие шейпы размечаются ролями отклонений ПОСЛЕ разметки сетки.
NB_DEV_ROLES = ("p1_dev", "p2_dev", "p3_dev")

#: Роли карточки листов 4–6, которых может не быть в конкретной ячейке.
#: У листа 6 карточка «Интернет-магазин» короче карты на три строки отклонений
#: (они вынесены в конец листа, см. NB_DEV_ROLES), а её блок вдобавок обрезан
#: номером страницы. Остальные роли обязательны: их отсутствие — другая дека.
NB_OPTIONAL_ROLES = frozenset(NB_DEV_ROLES)

#: Суффикс имени ПОДЛОЖКИ карточки направления у всех трёх листов («Shape 7»):
#: генератор деки начинает сетку одним и тем же номером, дальше подложки идут
#: с шагом карточки (24 у листа 4, 22 у листа 5, 23 у листа 6), поэтому границы
#: блоков берутся по РАЗМЕРУ подложки, а не по суффиксу.
NB_CARD_BG_ID = "7"

#: Число шейпов карточки направления (см. NB_CARD_ROLES).
NB_CARD_SHAPES = len(NB_CARD_ROLES)

#: Карточка страны на листе 1 (год): 4 страны × 11 шейпов, суффиксы подряд от
#: 23 (рамка — «Shape 23»). У ГОДА три строки долей (доля в ФОТ группы + две
#: доли в выручке), и дельта стоит сразу под значением, ДО разделителя.
B1_CARD_ROLES_YEAR = (
    "bg",              # рамка roundRect
    "cc_code",         # «BY»
    "cc_dyn_label",    # «Динамика, п.п. (2026/2025): »
    "cc_value",        # «147,1 млн»
    "cc_dev",          # «+8,2% к 2025 (+11,1 млн)»
    "cc_sep1",         # разделитель после дельты
    "cc_share1",       # «Доля в ФОТ группы: 67,8%»
    "cc_share2",       # «Доля ФОТ в выручке гр. (2026): 21,2%»
    "cc_share3",       # «Доля ФОТ в выручке гр. (2025): 23,8%»
    "cc_sep2",         # разделитель перед численностью
    "cc_hc",           # «ПЧ: … ФЧ 1ПГ 2026: …» / «Откл. (Ф-П): …» (два абзаца)
)

#: Карточка страны на листах 1ПГ/2ПГ: 4 страны × 10 шейпов, суффиксы от 23.
#: У полугодий долей ДВЕ (в выручке 2026 и 2025 — «доля в ФОТ группы» не
#: печатается), а дельта ушла ПОД разделитель, к своей подписи «к Факту …».
B1_CARD_ROLES_HALF = (
    "bg",
    "cc_code",
    "cc_dyn_label",
    "cc_value",
    "cc_sep1",         # разделитель сразу под значением
    "cc_dev",          # «к Факту 1ПГ25: +10,3% (+6,7 млн)»
    "cc_share1",       # «Доля ФОТ в выручке гр. (Факт 1ПГ26): 23,5%»
    "cc_share2",       # «Доля ФОТ в выручке гр. (Факт 1ПГ25): 25,6%»
    "cc_sep2",
    "cc_hc",
)

#: Круги «Динамика, п.п.» карточек стран листов 1–3 — отдельные шейпы в конце
#: листа (DynBadge_N + ZZZTMP_DynBadgeText_N): у них нет числовых суффиксов,
#: поэтому адресуются по ИМЕНИ деки.
B1_BADGE_CIRCLE = "DynBadge_%d"
B1_BADGE_TEXT = "ZZZTMP_DynBadgeText_%d"

#: KPI-лента b1: суффиксы МЕТКИ деки. Листы 1–3 размечены иначе, чем листы
#: численности p17 (там метка = 3/6/9/12/15): здесь блок начинается с «Text 4»,
#: и суффиксы идут 4/7/10/13/16, значения — следующими (5/8/11/14/17).
#: Лента листов 1–3 ШЕСТИблочная: шестой («ДИНАМИКА ВЫРУЧКИ К 2025») стоит
#: последним, но его суффикс НЕ продолжает шаг 3: строка «−3,7 п.п. к факту
#: 2025» (18) и разделитель (19) сдвинули нумерацию, поэтому метка — 20,
#: значение — 21. Блок есть на всех трёх листах, значит в карте обязателен.
B1_KPI_IDS = (("4", "kpi1"), ("7", "kpi2"), ("10", "kpi3"),
              ("13", "kpi4"), ("16", "kpi5"), ("20", "kpi6"))

#: Подпись «п.п. к предпериоду» под пятой карточкой ленты (у деки это
#: отдельный шейп «Text 18»): «−3,7 п.п. к факту 2025» / «−3,2 п.п. к 1П 2025».
B1_KPI_SUB_ID = "18"

#: Метки KPI-ленты по прототипу листов 1–3. Шестая карточка — динамика
#: выручки: у года «к 2025», у 1ПГ — «к 2025», у 2ПГ — «к 2025» (тактика).
B1_KPI_LABELS = {
    "b1_year": ("ФАКТ, ГОД 2025", "ДОЛЯ ФОТ В ВЫРУЧКЕ 2025", "ОЖИДАНИЕ, ГОД 2026",
                "ДИНАМИКА ФОТ К 2025", "ДОЛЯ ФОТ В ВЫРУЧКЕ 2026",
                "ДИНАМИКА ВЫРУЧКИ К 2025"),
    "b1_h1": ("ФАКТ, 1 ПГ 2025", "ДОЛЯ ФОТ В ВЫРУЧКЕ 1П25", "ФАКТ, 1 ПГ 2026",
              "ДИНАМИКА ФОТ К 2025", "ДОЛЯ ФОТ В ВЫРУЧКЕ 1П26",
              "ДИНАМИКА ВЫРУЧКИ К 2025"),
    "b1_h2": ("ФАКТ, 2 ПГ 2025", "ДОЛЯ ФОТ В ВЫРУЧКЕ 2П25", "ТАКТИКА, 2 ПГ 2026",
              "ДИНАМИКА ФОТ К 2025", "ДОЛЯ ФОТ В ВЫРУЧКЕ ТАКТ.2П26",
              "ДИНАМИКА ВЫРУЧКИ К 2025"),
}

#: Панель численности справа от KPI-ленты (листы 1–3): пять колонок «метка /
#: значение», у деки имена уже семантические (HCL700 / HCV701 …). Порядок
#: колонок одинаков у всех трёх листов, различаются только значения.
B1_HC_COLS = 5


def _any_text(sh, got) -> bool:
    """Любой непустой текст — роль шейпа задаётся позицией в блоке, не текстом."""
    return bool(got.strip())


def _no_text(sh, got) -> bool:
    return not got.strip()


def _role_kind(role: str) -> str:
    """Тип шейпа по роли: рамка — roundRect, круги — ellipse, остальное — текст."""
    if role == "bg":
        return KIND_ROUNDRECT
    if role.endswith("_circle"):
        return KIND_ELLIPSE
    if role.endswith("_sep1") or role.endswith("_sep2") or role.startswith("hr"):
        return KIND_LINE
    return KIND_TEXT


def _is_page_num(sh, got) -> bool:
    return len(_runs_of(sh)) == 1 and got.strip().isdigit()


def _is_deck_title(sh, got) -> bool:
    return got.lstrip().startswith("ГК «МАРК ФОРМЭЛЬ»") and len(_runs_of(sh)) >= 2


def _is_hc_note(sh, got) -> bool:
    """Строка «Числ. направления (ПЧ/ФЧ 1ПГ26): …» — у листов 7/8 без суффикса."""
    return bool(got.strip().startswith("Числ. направления"))


def _is_attention_head(sh, got) -> bool:
    return got.strip().upper().startswith("ОТДЕЛЫ, ТРЕБУЮЩИЕ")


def _is_unit_note(sh, got) -> bool:
    """Сноска под таблицей: «тыс. BYN. Итого Отделы приведено по данным свода CFO…»."""
    return got.strip().lower().startswith("тыс.")


def _is_trend_banner(sh, got) -> bool:
    return got.strip().startswith("Тенденции и прогноз")


def _is_good_banner(sh, got) -> bool:
    return got.strip().upper().startswith("ЛУЧШАЯ ТЕНДЕНЦИЯ")


def _is_warn_banner(sh, got) -> bool:
    return got.strip().upper().startswith("ВТОРОЙ ПРИОРИТЕТ")


def _is_cards_head_b1(sh, got) -> bool:
    """Подзаголовок блока карточек стран листов 1–3.

    У года: «ОЖИДАНИЕ 2026 к ФАКТУ 2025, ФОТ»; у полугодий: «ФАКТ 1 ПГ 2026:
    ИСПОЛНЕНИЕ БЮДЖЕТА И ДИНАМИКА К 2025» / «ТАКТИКА 2 ПГ 2026: …» — то есть
    начинается с периода и содержит «к 2025» либо «ФОТ». Одиночный шейп-строка
    (первый абзац целиком), поэтому проверяем начало и длину.
    """
    text = got.strip().upper()
    return (len(text) < 70
            and text.startswith(("ОЖИДАНИЕ", "ФАКТ", "ТАКТИКА"))
            and ("ФОТ" in text or "К 2025" in text))


def _is_kpi_with_unit(sh, got) -> bool:
    """Значение KPI-ленты b1: два рана, второй — единица измерения."""
    runs = _runs_of(sh)
    if len(runs) != 2:
        return False
    tail = "".join(t.text or "" for t in runs[1].findall(qn("a:t"))).strip()
    return tail in ("млн BYN", "млн USD", "млн RUB", "%", "п.п.")


def _resolve_b1_extra(proto_name: str, take, shapes: list) -> None:
    """Дополнительные шейпы листов 1–3: KPI-лента, карточки стран, страница.

    KPI-лента у b1 своя по подписям (B1_KPI_LABELS), карточки стран — паттерн
    4 × 12 шейпов (B1_CARD_ROLES), круги «п.п.» адресуются по именам деки.
    """
    labels = B1_KPI_LABELS[proto_name]
    for idx, (spec_id, name) in enumerate(B1_KPI_IDS):
        take(spec_id, name + "_label", KIND_TEXT,
             lambda s, got, want=labels[idx]: got.strip() == want)
        take(str(int(spec_id) + 1), name + "_value", KIND_TEXT, _is_kpi_with_unit)

    # «млн BYN» — статичная подпись единицы листа (срез «Валюта» её меняет):
    # это КОНСТАНТА деки, как «чел.» у листов численности, а не данные
    take("3", "unit_label", KIND_TEXT,
         lambda s, got: got.strip().split()[-1] in ("BYN", "USD", "RUB"))

    # «−3,7 п.п. к факту 2025» — подпись под пятой карточкой ленты
    take(B1_KPI_SUB_ID, "kpi5_sub", KIND_TEXT,
         lambda s, got: "п.п." in got and "к " in got)

    take("22", "cards_head", KIND_TEXT, _is_cards_head_b1)
    _resolve_b1_cards(shapes, take, proto_name)
    _resolve_b1_badges(shapes)
    _resolve_b1_hc_panel(shapes)
    # Номер страницы: у года суффикс 67, у полугодий — 63 (в деке карточки
    # полугодий короче на строку, и генератор сдвинул нумерацию). Поэтому
    # адресуем по тексту, а не по номеру: это единственный однозначный
    # цифровой шейп в конце листа.
    _take_named(shapes, "page", _is_page_num, "номер страницы")


def _resolve_b1_hc_panel(shapes: list) -> None:
    """Панель численности справа от KPI-ленты: 5 колонок «метка / значение».

    У деки у этих шейпов УЖЕ осмысленные имена (HCL700 / HCV701, у полугодий
    HCL710 / HCV711, HCDiv710 …), но с разными номерами на каждом листе.
    Переименовываем в общий контракт `hc{i}_label` / `hc{i}_value` / `hc{i}_div`,
    чтобы биндер писал по номеру колонки, а не по номеру шейпа деки.
    """
    import re as _re
    labels = {}
    values = {}
    divs = 0
    for sh in shapes:
        m = _re.match(r"^HCL(\d{3})$", sh.name)
        if m:
            labels[int(m.group(1))] = sh
            continue
        m = _re.match(r"^HCV(\d{3})$", sh.name)
        if m:
            values[int(m.group(1))] = sh
            continue
        if sh.name.startswith("HCDiv"):
            sh.name = "hc%d_div" % (divs + 1)
            divs += 1
    if len(labels) != B1_HC_COLS or len(values) != B1_HC_COLS:
        raise AssertionError(
            "b1: панель численности — меток %d, значений %d, ожидалось %d"
            % (len(labels), len(values), B1_HC_COLS))
    # значения деки нумерованы на 1 больше метки (HCL700 → HCV701): пары
    # собираются по порядку номеров, а не по совпадению суффикса
    for i, (lkey, vkey) in enumerate(zip(sorted(labels), sorted(values)), 1):
        labels[lkey].name = "hc%d_label" % i
        values[vkey].name = "hc%d_value" % i


def _resolve_b1_cards(shapes: list, take, proto_name: str) -> int:
    """Карточки стран листов 1–3: раскладывает паттерн, возвращает их число.

    Раскладка внутри карточки у ГОДА и у ПОЛУГОДИЙ разная (у года три строки
    долей, у полугодий — две, и дельта стоит под разделителем), поэтому роли
    берутся из своего кортежа. Блок начинается рамкой с суффиксом 23, дальше
    рамки идут с шагом «число ролей». Разметка ОБЯЗАНА покрыть каждый блок
    целиком: рассинхрон деки падает здесь, а не съезжает тихо в вёрстке.
    """
    roles = (B1_CARD_ROLES_YEAR if proto_name == "b1_year"
             else B1_CARD_ROLES_HALF)
    count = 0
    step = len(roles)
    # Карточки идут подряд до конца блока; последняя карточка занимает
    # суффиксы 23+3·step … 23+4·step−1, страница листа — 67. Ограничение по
    # числу стран отчёта (4) страхует от «съедания» шейпов панели/страницы,
    # если у деки когда-нибудь появится пятая карточка без обновления карты.
    while count < 4 and _has_suffix(shapes, str(23 + count * step)):
        base = 23 + count * step
        for offset, role in enumerate(roles):
            kind = _role_kind(role)
            take(str(base + offset), "card%d_%s" % (count + 1, role), kind,
                 _any_text if kind == KIND_TEXT else _no_text)
        count += 1
    if not count:
        raise AssertionError("b1: карточки стран не размечены (нет суффикса 23)")
    return count


def _resolve_b1_badges(shapes: list) -> None:
    """Круги Δ п.п. карточек стран: DynBadge_N / ZZZTMP_DynBadgeText_N.

    Отдельные шейпы в конце листа без числовых суффиксов имён — переименовываются
    по номеру карточки, к которой примыкают (порядок номеров = порядок стран).
    """
    by_name = {sh.name: sh for sh in shapes}
    idx = 0
    while (B1_BADGE_CIRCLE % idx) in by_name:
        circle = by_name[B1_BADGE_CIRCLE % idx]
        text = by_name.get(B1_BADGE_TEXT % idx)
        if text is None:
            raise AssertionError("b1: у круга %r нет текста" % circle.name)
        if _shape_kind(circle) != KIND_ELLIPSE:
            raise AssertionError("b1: %r не эллипс" % circle.name)
        circle.name = "card%d_pp_circle" % (idx + 1)
        text.name = "card%d_pp_text" % (idx + 1)
        idx += 1
    if not idx:
        raise AssertionError("b1: круги динамики (DynBadge_N) не найдены")


def _has_suffix(shapes: list, sid: str) -> bool:
    return any(_name_suffix(sh.name) == sid for sh in shapes)


def _resolve_dept_extra(proto_name: str, take, shapes: list) -> None:
    """Дополнительные шейпы листов 7/8: строка численности, карточки, баннеры.

    Шапка (title/badge) уже разобрана общей картой прототипа: у листов 7/8
    бейдж двухстрочный, и его проверяет is_badge через кортеж ожидаемых строк.
    """
    _take_named(shapes, "hc_note", _is_hc_note, "строка «Числ. направления»")
    if proto_name == "dept_summary":
        _resolve_dept7(take, shapes)
    else:
        _resolve_dept8(take, shapes)


def _take_named(shapes: list, semantic: str, predicate, what: str) -> None:
    """Шейп по тексту-признаку (у шапки листов 7/8 нет числовых суффиксов).

    Уже размеченные шейпы не трогаются: у листов 1–3 бейдж периода («2026»)
    тоже состоит из цифр, и без этой проверки предикат «номер страницы»
    забирал его вместо настоящего номера в конце листа.
    """
    for sh in shapes:
        if _is_semantic_name(sh.name) or _shape_kind(sh) != KIND_TEXT:
            continue
        if predicate(sh, _text_of(sh)):
            sh.name = semantic
            return
    raise AssertionError("не найден шейп: %s" % what)


def _resolve_dept7(take, shapes: list) -> None:
    """Лист 7: сноска, заголовок блока внимания, 6 карточек, баннер, страница."""
    take("3", "unit_note", KIND_TEXT, _is_unit_note)
    take("4", "cards_head", KIND_TEXT, _is_attention_head)
    _resolve_grid(shapes, take, start=5, roles=DEPT7_ROLES, prefix="card",
                  proto="dept_summary")
    take("155", "banner_bg", KIND_ROUNDRECT, _no_text)
    take("156", "banner", KIND_TEXT, _is_trend_banner)
    take("157", "page", KIND_TEXT, _is_page_num)


def _resolve_dept8(take, shapes: list) -> None:
    """Лист 8: сетка карточек отделов, два баннера, страница."""
    _resolve_grid(shapes, take, start=3, roles=DEPT8_ROLES, prefix="card",
                  proto="dept_cards")
    take("401", "banner_good_bg", KIND_ROUNDRECT, _no_text)
    take("402", "banner_good", KIND_TEXT, _is_good_banner)
    take("403", "banner_warn_bg", KIND_ROUNDRECT, _no_text)
    take("404", "banner_warn", KIND_TEXT, _is_warn_banner)
    take("405", "page", KIND_TEXT, _is_page_num)


# ─── Листы 4–6 деки (p14): направления бизнеса ────────────────────────────────

#: Тексты-признаки листов 4–6 (по ним же сверяется версия деки).
NB_DONUT_HEADS = ("ДОЛЯ ФОТ ПО НАПРАВЛЕНИЯМ,", "ДОЛЯ ФОТ ПО ПОДРАЗДЕЛЕНИЯМ,",
                  "ДОЛЯ ФОТ ПО КАНАЛАМ,")
NB_BAR_HEADS = ("ДОЛЯ ФОТ В ВЫРУЧКЕ ПО НАПРАВЛЕНИЯМ, %",
                "ДОЛЯ ФОТ В ВЫРУЧКЕ ПО ПОДРАЗДЕЛЕНИЯМ, %",
                "ДОЛЯ ФОТ В ВЫРУЧКЕ ПО КАНАЛАМ, %")


def _resolve_nb_extra(proto_name: str, take, shapes: list) -> None:
    """Листы 4–6: диаграммы, подписи блоков, сетка карточек, страница.

    Раскладка листов 4–6 отличается от листов 7/8 сразу по трём осям, и каждая
    ломает «слепое» переиспользование:

      * у карточки направления ТРИ круга Δ доли (а не два), и у листа 4 они
        идут ПЕРВЫМИ в документе — до первого блока значений;
      * блоки значений собраны иначе: у листа 4 после третьего блока идут
        строки численности, у листов 5/6 третий круг замыкает карточку;
      * данные рисуются ещё и диаграммами, которых нет у прототипов партии A:
        донат структуры под двухстрочным заголовком и лента мини-баров, где у
        КАЖДОЙ карточки своя диаграмма, а над первой — заголовок ленты.

    Подписи диаграмм (заголовок доната и ленты) — не данные, а формулировки
    деки, поэтому у барабанной ленты текст остаётся в активе («static»), а
    заголовок доната уходит в шаблон подписей: в нём стоит год среза. Сами
    диаграммы размечаются как `chart_donut` и `cardN_chart` и попадают в
    манифест слотами (тип, число точек, цвета dPt — снятые с деки).
    """
    # Сноска под таблицей: у листа 4 это «тыс. BYN», у листов 5/6 — сноска
    # методики («Развитие розницы» включено в группу «Отделы»). Формулировка
    # деки, данных в ней нет — текст остаётся в активе как константа.
    take("3", "unit_note", KIND_TEXT, _is_unit_note)
    # Строка численности в шапке: у этих листов БЕЗ числового суффикса
    # («Text CTHC804», «Text HC905»), поэтому адресуется по тексту.
    _take_named(shapes, "hc_note", _is_nb_hc_note, "строка «Числ. …»")
    # Двухстрочный заголовок доната: «ДОЛЯ ФОТ ПО НАПРАВЛЕНИЯМ,» / «ОЖИДАНИЕ 2026»
    take("4", "donut_head", KIND_TEXT, _is_nb_donut_head)
    # Диаграмма доната — ПЕРВАЯ в документе (заголовок стоит над ней, до ленты
    # мини-баров). Индекс нулевой: `_take_chart` считает в порядке документа.
    donut = _take_chart(shapes, "chart_donut", 0)
    # Заголовок ленты мини-баров (у листа 6 он с пояснением методики в скобках)
    take("5", "bars_head", KIND_TEXT, _is_nb_bars_head)
    # Номер страницы — ДО сетки: он лежит в блоке последней карточки (у листа 4
    # сразу за её строками), и без имени попал бы в «хвост» карточки.
    _take_named(shapes, "page", _is_page_num, "номер страницы")
    _resolve_nb_grid(proto_name, shapes, donut)


def _is_nb_hc_note(sh, got) -> bool:
    """Строка численности шапки: «Числ. страны …» (лист 4) / «Числ. направления …»."""
    return got.strip().startswith(("Числ. страны", "Числ. направления"))


def _is_nb_donut_head(sh, got) -> bool:
    return got.strip().upper().startswith(NB_DONUT_HEADS)


def _is_nb_bars_head(sh, got) -> bool:
    return got.strip().upper().startswith(NB_BAR_HEADS)


def _take_chart(shapes: list, semantic: str, index: int):
    """N-я диаграмма слайда в порядке документа → семантическое имя.

    Диаграмма — graphicFrame с c:chart: аннотация шейпов её не видит (у неё нет
    текста), поэтому имя ставится здесь. Индекс — по порядку документа, а не по
    имени деки («Chart 3»): имена диаграмм у слайдов 4–6 сплошные, но опираться
    на них значило бы зависеть от того, сколько объектов удалили в редакторе.
    """
    charts = [sh for sh in shapes if _shape_kind(sh) == KIND_CHART]
    if len(charts) <= index:
        raise AssertionError("на слайде %d диаграмм, а нужна №%d (%s)"
                             % (len(charts), index + 1, semantic))
    sh = charts[index]
    sh.name = semantic
    return sh


def _resolve_nb_grid(proto_name: str, shapes: list, donut) -> None:
    """Сетка карточек направления + имена и мини-бары.

    Карточка направления НЕ содержит ни своей диаграммы, ни своего имени: лента
    мини-баров лежит отдельной строкой ВЫШЕ сетки (на карточку — ровно одна
    диаграмма, у листа 6 их семь), а имя стоит шейпом перед диаграммой. Поэтому
    разметка идёт в два прохода:

      1. имена ленты — i-я диаграмма и шейп непосредственно ПЕРЕД ней;
      2. блоки карточек по подложкам — роли внутри блока.

    Блок карточки по подложкам захватывает и шапку СЛЕДУЮЩЕЙ карточки (имя и
    диаграмма стоят после содержимого предыдущей), поэтому остаток блока — это
    уже названные на первом проходе шейпы.

    Отдельная беда листа 6: у пятой карточки («Интернет-магазин») три строки
    отклонений генератор деки вынес в самый конец spTree — на картинке они на
    своих местах, а в документе лежат после номера страницы. В блок такой
    карточки они не попали, поэтому размечаются ПО ГЕОМЕТРИИ: безымянный
    текстовый шейп получает недостающую роль отклонения той карточки, в чей
    прямоугольник попадает его центр. Это часть деки, а не дефект актива:
    актив обязан повторить её 1:1, иначе слайд разъедется.
    """
    roles = (NB_CARD_ROLES if proto_name == "nb_summary"
             else NB_CARD_ROLES_UNITS)
    bars = [sh for sh in shapes if _shape_kind(sh) == KIND_CHART
            and sh.name != donut.name]
    # 1) лента: диаграмма + имя перед ней
    for i, chart in enumerate(bars, 1):
        chart.name = "card%d_chart" % i
        title = _shape_before(shapes, chart)
        if title is None or _shape_kind(title) != KIND_TEXT:
            raise AssertionError("прототип %s: перед диаграммой карточки %d нет "
                                 "шейпа с именем канала" % (proto_name, i))
        title.name = "card%d_title" % i
    spans = _card_spans(shapes, NB_CARD_BG_ID)
    if not spans:
        raise AssertionError("прототип %s: карточки не размечены (нет подложки "
                             "с суффиксом %s)" % (proto_name, NB_CARD_BG_ID))
    if len(bars) != len(spans):
        raise AssertionError(
            "прототип %s: карточек %d, диаграмм ленты %d — раскладка листа "
            "изменилась, разметка не сходится"
            % (proto_name, len(spans), len(bars)))
    # 2) блоки карточек; `missing[i]` — роли, которых в блоке не оказалось
    boxes: list[tuple[int, int, int, int]] = []
    missing: dict[int, list[str]] = {}
    for i, (name, block) in enumerate(spans, 1):
        skipped: list[str] = []
        _annotate_card(block, roles, "card%d_" % i, proto_name, name,
                       optional=NB_OPTIONAL_ROLES, skipped=skipped)
        if skipped:
            missing[i] = skipped
        boxes.append((i, _box_of(block[0])))
    # 3) вынесенные за блок шейпы: по геометрии — в карточку, чей прямоугольник
    #    накрывает центр шейпа; роль — первая недостающая роль отклонения.
    #    Шейпы без текста вне карточек (подложка круга бейджа) не трогаем: они
    #    и у листов 7/8 остаются с именем деки.
    order = {id(sh._element): i for i, sh in enumerate(shapes)}
    loose = sorted((sh for sh in shapes
                    if " " in str(sh.name) and _shape_kind(sh) == KIND_TEXT),
                   key=lambda sh: order[id(sh._element)])
    for sh in loose:
        idx = _card_at(boxes, sh)
        if idx is None:
            raise AssertionError(
                "прототип %s: текстовый шейп %r (%r) не размечен и не попадает "
                "ни в одну карточку — раскладка листа изменилась"
                % (proto_name, sh.name, _text_of(sh)[:40]))
        pool = missing.get(idx) or []
        if not pool:
            raise AssertionError(
                "прототип %s: шейп %r (%r) лежит в карточке %d, но у неё нет "
                "незанятых ролей — раскладка листа изменилась"
                % (proto_name, sh.name, _text_of(sh)[:40], idx))
        sh.name = "card%d_%s" % (idx, pool.pop(0))
    if missing:
        left = {k: v for k, v in missing.items() if v}
        if left:
            raise AssertionError(
                "прототип %s: у карточек %s пропущены роли %s, а вынесенных "
                "шейпов на листе не хватило — раскладка листа изменилась"
                % (proto_name, ", ".join(str(k) for k in sorted(left)),
                   "; ".join("%d: %s" % (k, ",".join(v))
                             for k, v in sorted(left.items()))))


def _card_at(boxes: list, shape) -> int | None:
    """Номер карточки, в чей прямоугольник попадает центр шейпа (или None)."""
    x, y, cx, cy = _box_of(shape)
    px, py = x + cx // 2, y + cy // 2
    for idx, (bx, by, bcx, bcy) in boxes:
        if bx <= px <= bx + bcx and by <= py <= by + bcy:
            return idx
    return None


def _shape_before(shapes: list, target):
    """Шейп, стоящий в документе непосредственно перед `target` (или None)."""
    prev = None
    for sh in shapes:
        if sh._element is target._element:
            return prev
        prev = sh
    return None


def _resolve_grid(shapes: list, take, start: int, roles: tuple, prefix: str,
                  proto: str) -> int:
    """Сетка карточек: card{i}_{роль} по паттерну «шаг = число ролей».

    Суффиксы шейпов внутри блока идут подряд (дека собрана генератором), поэтому
    блок i начинается с суффикса start + i×len(roles). Сколько карточек
    поместилось — столько и размечается: у листа 7 их 6, у листа 8 — 16.
    Число карточек попадает в манифест (`card_count`): по нему биндер удаляет
    лишние ячейки (данных меньше) или открывает вторую страницу (больше).
    """
    # Сетка режется по ПОДЛОЖКАМ: карточка начинается roundRect'ом своего
    # размера, и блок шейпов до следующей такой подложки — её содержимое.
    # Суффиксы для границ не годятся (у листа 7 баннер идёт следующим номером
    # за последней карточкой), а размер — годится: баннеры широкие и низкие.
    spans = _card_spans(shapes, start)
    if not spans:
        raise AssertionError("прототип %s: карточки не размечены (нет подложки "
                             "с суффиксом %d)" % (proto, start))
    for i, (name, block) in enumerate(spans, 1):
        _annotate_card(block, roles, "%s%d_" % (prefix, i), proto, name)
    return len(spans)


def _card_spans(shapes: list, start: int) -> list[tuple[str, list]]:
    """Блоки шейпов карточек: [(имя подложки, [шейпы блока]), …].

    Первая подложка — шейп с суффиксом `start`. Её габариты задают эталон:
    у остальных ячеек они те же до EMU, у баннеров и прочих roundRect — нет.
    Блок карточки — шейпы от её подложки до следующей подложки того же размера
    (или до конца листа). Так число карточек берётся из деки, а не из
    константы: появится пятый ряд — актив пересоберётся без правки биндера.
    """
    first = None
    for sh in shapes:
        if _name_suffix(sh.name) == str(start):
            first = sh
            break
    if first is None or _shape_kind(first) != KIND_ROUNDRECT:
        return []
    want = _box_of(first)[2:]
    marks = [i for i, sh in enumerate(shapes)
             if _shape_kind(sh) == KIND_ROUNDRECT and _box_of(sh)[2:] == want]
    if not marks:
        return []
    out = []
    for n, begin in enumerate(marks):
        end = marks[n + 1] if n + 1 < len(marks) else len(shapes)
        out.append((shapes[begin].name, shapes[begin:end]))
    return out


#: Роли карточки, которых может НЕ быть в конкретной ячейке. У листа 8
#: последняя карточка («Гр. аудита кач-ва») пришла из витрины без численности
#: отдела, и генератор деки просто не создал две строки «ПЧ/ФЧ» и «Откл.»:
#: блок короче на два шейпа. Это ЧАСТЬ деки, а не дефект актива — поэтому
#: роли пропускаются, но только они: любой другой сдвиг остаётся ошибкой.
OPTIONAL_CARD_ROLES = frozenset({"hc_line", "hc_dev"})


def _annotate_card(block: list, roles: tuple, prefix: str, proto: str,
                   bg_name: str, optional: frozenset | None = None,
                   skipped: list | None = None) -> list:
    """Раскладывает один блок карточки по ролям, пропуская необязательные.

    Роли идут в порядке документа; блок может быть короче карты на
    `optional` (по умолчанию OPTIONAL_CARD_ROLES). Несовпадение типа шейпа на
    любой другой роли — AssertionError с именем карточки: это признак другой
    версии деки.

    `skipped` (если передан) наполняется ролями, которых в блоке не оказалось:
    у листа 6 так теряются три строки отклонений пятой карточки (генератор деки
    вынес их за пределы блока), и по этому списку `_resolve_nb_grid` понимает,
    какой карточке отдать «хвостовые» шейпы.

    Возвращает шейпы блока, до которых карта НЕ дотянулась. У листов 7/8 это
    шапка следующей карточки, уже получившая имя на первом проходе разметки.
    """
    optional = OPTIONAL_CARD_ROLES if optional is None else optional
    idx = 0
    for role in roles:
        kind = _role_kind(role)
        if idx >= len(block):
            if role in optional:
                if skipped is not None:
                    skipped.append(role)
                continue
            raise AssertionError("прототип %s: карточка %r короче карты — нет "
                                 "роли %r" % (proto, bg_name, role))
        sh = block[idx]
        got = _shape_kind(sh)
        if got != kind:
            if role in optional:
                # роли нет в блоке: шейп не съеден
                if skipped is not None:
                    skipped.append(role)
                continue
            raise AssertionError(
                "прототип %s: карточка %r, роль %r ждёт %s, а шейп %r — %s"
                % (proto, bg_name, role, kind, sh.name, got))
        if kind == KIND_TEXT and not _text_of(sh).strip():
            raise AssertionError("прототип %s: карточка %r, роль %r — пустой "
                                 "текст" % (proto, bg_name, role))
        sh.name = prefix + role
        idx += 1
    return block[idx:]


# ─── Снятие цветов УФ с прототипов ───────────────────────────────────────────

def _sign_colors(summary_slide, country_slide, kz_slide,
                 half_slides=()) -> dict:
    """Цвета УФ, снятые с прототипов — в биндерах hex'ов быть не должно.

    Красный/зелёный — из Δ-ячеек таблицы стран (слайд 23), светлые варианты —
    из ранов полосы «Итого» (слайд 24), базовые цвета текста — из ячеек тела,
    итоговой строки и шапки.
    """
    gf = next(sh for sh in summary_slide.shapes
              if getattr(sh, "has_table", False) and sh.has_table)
    tbl = gf.table
    up = down = None
    body_text = None
    for row in list(tbl.rows)[1:]:
        for ci, cell in enumerate(row.cells):
            for p in cell.text_frame.paragraphs:
                for run in p.runs:
                    txt = (run.text or "").strip()
                    col = _run_color(run)
                    if col is None or ci < 5:
                        continue
                    if txt.startswith("+"):
                        up = up or col
                    elif txt.startswith(("−", "-")):
                        down = down or col
    first_body = list(tbl.rows)[1]
    for cell in list(first_body.cells)[1:]:
        for p in cell.text_frame.paragraphs:
            for run in p.runs:
                body_text = body_text or _run_color(run)
    # «н/д» в колонке Δ деки — приглушённый серый (пустое отклонение не должно
    # читаться как число светофора). В колонках значений и «% укомпл.» «н/д»
    # остаётся базовым цветом строки — проверено по деке.
    na_text = None
    na_header_text = None
    for slide in (summary_slide, country_slide, kz_slide):
        for sh in slide.shapes:
            if not (getattr(sh, "has_table", False) and sh.has_table):
                continue
            for row in list(sh.table.rows)[1:]:
                cells = list(row.cells)
                for ci, cell in enumerate(cells):
                    if ci < 5:
                        continue
                    for p in cell.text_frame.paragraphs:
                        for run in p.runs:
                            if (run.text or "").strip() == "н/д":
                                na_text = na_text or _run_color(run)
    if na_text is None:
        na_text = body_text
    total_row = next(r for r in tbl.rows
                     if (r.cells and _cell_fill(r.cells[0]._tc) == FILL_STYLES_INV["total"]))
    total_text = _run_color(total_row.cells[1].text_frame.paragraphs[0].runs[0])
    header_text = _run_color(tbl.rows[0].cells[0].text_frame.paragraphs[0].runs[0])

    # Отклонения в ИТОГОВОЙ строке листов полугодий (2 и 3) дека печатает
    # ОДНИМ светлым оттенком, независимо от знака: «+7,6» и «+10,1%» там
    # F0D7DE, как и «−0,5%». Пара delta_*_light (полоса «Итого» слайдов 23/24)
    # для этой строки не подходит — берём её собственный тон.
    total_row_text = None
    for slide in half_slides or (country_slide, kz_slide):
        for sh in slide.shapes:
            if not (getattr(sh, "has_table", False) and sh.has_table):
                continue
            rows = list(sh.table.rows)
            if not rows:
                continue
            last = rows[-1]
            for cell in list(last.cells)[5:]:
                for p in cell.text_frame.paragraphs:
                    for run in p.runs:
                        col = _run_color(run)
                        # белый — базовый цвет текста итога, его не берём:
                        # нужен именно ОТТЕНОК ОТКЛОНЕНИЙ на тёмной полосе
                        if col and col not in (down, up, total_text):
                            total_row_text = total_row_text or col
    if total_row_text is None:
        total_row_text = total_text

    bar = next(sh for sh in country_slide.shapes
               if _text_of(sh).lstrip().startswith("Итого (видимые позиции)"))
    runs = _runs_of(bar)
    up_light, down_light = _run_color(runs[3]), _run_color(runs[4])
    # нейтраль полосы — цвет рана 1 («Утвержд. …»): им красится Δ нуля/«н/д»
    bar_neutral = _run_color(runs[1])

    # контроль: цвет «% укомпл.» — один из пары красный/зелёный
    pct_cols = set()
    for row in list(tbl.rows)[1:-1]:
        for p in row.cells[4].text_frame.paragraphs:
            for run in p.runs:
                pct_cols.add(_run_color(run))

    out = {
        "delta_up": up,                  # рост/перевыполнение — красный
        "delta_down": down,              # снижение — зелёный
        "delta_up_light": up_light,      # те же знаки на тёмной полосе итога
        "delta_down_light": down_light,
        "bar_neutral": bar_neutral,      # нейтраль Δ на полосе (нулевое Δ)
        "na_text": na_text,              # «н/д» — приглушённый серый деки
        "body_text": body_text,          # базовый цвет текста строк тела
        "total_text": total_text,        # базовый цвет текста итоговой строки
        "total_row_text": total_row_text,  # отклонения ИТОГА: один светлый тон
        "header_text": header_text,      # цвет текста шапки
    }
    missing = [k for k, v in out.items() if not v]
    if missing:
        raise AssertionError("не сняты цвета УФ с прототипов: %r" % (missing,))
    stray = pct_cols - {up, down, body_text}
    if stray:
        raise AssertionError("в «%% укомпл.» неожиданные цвета: %r" % (stray,))
    return out


def _pct_bands(slides) -> dict:
    """Границы светофора «% укомпл.», снятые с ячеек деки.

    В деке укомплектованность трёхцветная, и границы видны по фактическим
    заливкам текста: зелёный — недобор, базовый цвет — «в пределах допуска»,
    красный — перевыполнение. По всем 87 ячейкам слайдов 23–27 раскладка
    получается без пересечений: green ≤ 97,0 < neutral ≤ 99,0 < red ≥ 100,0,
    то есть допуск ±3 % вокруг 100. Границы не хардкодим — считаем из деки и
    кладём в манифест, чтобы биндер брал их оттуда.
    """
    green: list[float] = []
    red: list[float] = []
    neutral: list[float] = []
    for slide in slides:
        for sh in slide.shapes:
            if not (getattr(sh, "has_table", False) and sh.has_table):
                continue
            for ri, row in enumerate(list(sh.table.rows)):
                if ri == 0:
                    continue
                cells = list(row.cells)
                if len(cells) < 5:
                    continue
                p = cells[4].text_frame.paragraphs[0]
                if not p.runs:
                    continue
                raw = (p.runs[0].text or "").strip().replace("%", "").replace(",", ".")
                try:
                    value = float(raw)
                except ValueError:
                    continue
                col = _run_color(p.runs[0])
                if col == "1C8A5A":
                    green.append(value)
                elif col == "C0392B":
                    red.append(value)
                else:
                    neutral.append(value)
    if not green or not red:
        raise AssertionError("не удалось снять границы светофора «%% укомпл.»: "
                             "green=%r red=%r" % (green[:5], red[:5]))
    down_max, up_min = max(green), min(red)
    if down_max >= up_min:
        raise AssertionError("границы светофора пересеклись: green ≤ %.1f, "
                             "red ≥ %.1f" % (down_max, up_min))
    # «нейтральные» (базовый цвет) в деке сидят у самой верхней границы
    # допуска: 97,2 / 98 / 99 при красных от 100. Значение 97,2 — единственная
    # ячейка ниже 98, но она идёт в паре с Δ = −5,0 (зелёное) и в пачке таких
    # же «100,0 %»-нейтральных: в деке это ручная правка, а не правило.
    # Поэтому допуск отсекаем по фактическому минимуму нейтральных, но требуем,
    # чтобы все остальные нейтральные лежали выше него — иначе правило ложное.
    neutral_min = min(neutral) if neutral else up_min
    stray = [v for v in neutral if v < neutral_min - 1e-9]
    if stray:
        raise AssertionError("нейтральные ячейки ниже допуска: %r" % (sorted(stray)[:5],))
    bands = {"down_max": down_max, "up_min": up_min, "neutral_min": neutral_min,
             "neutral_values": sorted(set(neutral)),
             "samples": {"green": len(green), "neutral": len(neutral), "red": len(red)}}
    # правило должно покрывать деку целиком: зелёные строго ниже допуска,
    # красные — не ниже верхней границы
    bad_red = [v for v in red if v < up_min]
    if bad_red:
        raise AssertionError("красные ниже границы %r" % (sorted(bad_red)[:5],))
    return bands


# ─── Снятие формата чисел с прототипа ─────────────────────────────────────────

#: Признак числового рана: цифры, знаки, разряды, запятая, процент.
_NUM_RE = re.compile(r"^[+−\-]?[\d\xa0 ]*\d[\d\xa0 ]*(?:,\d+)?%?$")


def _num_shape(shape) -> str:
    """Первый ран шейпа, если он числовой (иначе пустая строка)."""
    runs = _runs_of(shape)
    if not runs:
        return ""
    t = runs[0].find(qn("a:t"))
    text = (t.text or "").strip() if t is not None else ""
    return text if _NUM_RE.match(text) else ""


def _cell_group(slide_list: list) -> bool:
    """Группирует ли дека разряды в ячейках таблиц — решается по ВСЕЙ деке.

    Генератор таблиц в деке один, а четырёхзначные значения есть не у всех
    прототипов (у RU/KZ максимум 203,5) — решать по одному прототипу значило
    бы выключить разряды для живых значений ≥ 1000.
    """
    for slide in slide_list:
        for sh in slide.shapes:
            if not (getattr(sh, "has_table", False) and sh.has_table):
                continue
            for row in list(sh.table.rows)[2:]:
                for cell in list(row.cells)[1:5]:
                    text = (cell.text_frame.paragraphs[0].runs[0].text or "").strip()
                    if _NUM_RE.match(text) and NBSP in text:
                        return True
    return False


def _deck_format(shapes: dict, tables: dict, cell_group: bool,
                 proto_name: str = "") -> dict:
    """Флаги типографики прототипа, снятые с самой деки.

    Дека писалась не одним генератором: в своде и в BY/RU значения KPI идут без
    разрядов («7307,2»), у UZ — с NBSP («2 052,5»); «% укомпл.» в таблице BY
    целое («99%»), в остальных — с одним знаком. Всё это снимается здесь, чтобы
    биндеры не гадали и не держали знаний о конкретной деке.
    """
    # KPI: группировка разрядов по фактическому тексту пяти значений. Шейпов
    # может не быть — у KZ и у листов 1–3 лента своя, поэтому берём то, что
    # размечено, а не падаем по имени.
    kpi_group = True
    kpi_samples = [_num_shape(shapes["kpi%d_value" % k])
                   for k in range(1, 6) if ("kpi%d_value" % k) in shapes]
    with_nbsp = [s for s in kpi_samples if NBSP in s]
    if with_nbsp and len(with_nbsp) == len([s for s in kpi_samples if s]):
        kpi_group = True
    elif kpi_samples and not with_nbsp:
        kpi_group = False

    # «% укомпл.»: знаков после запятой в теле таблицы И в полосе итога
    pct_digits, total_pct_digits = 1, 1
    card_pct_digits = 1
    for gf in tables.values():
        tbl = gf.table
        digits = set()
        for row in list(tbl.rows)[1:]:
            cells = list(row.cells)
            if len(cells) < 5:
                continue
            text = (cells[4].text_frame.paragraphs[0].runs[0].text or "").strip()
            if "%" not in text:
                continue
            digits.add(1 if "," in text else 0)
        if digits:
            if len(digits) > 1:
                raise AssertionError("в таблице %r у процентов разное число "
                                     "знаков: %r" % (gf.name, sorted(digits)))
            pct_digits = digits.pop()
            break

    bar = shapes.get("total_bar")
    if bar is not None:
        texts = [(r.find(qn("a:t")).text or "") for r in _runs_of(bar)]
        for text in texts:
            if "% укомпл." in text:
                token = text.split("% укомпл.")[1].strip().split(" ")[0]
                if "%" in token:
                    total_pct_digits = 1 if "," in token else 0

    # «% укомпл.» листов 4–6 живёт НЕ в таблице, а в строке численности шапки и
    # карточек, и знаки у неё свои: в шапке «96,6%» (один знак), в карточках
    # «96%» (целое). Признак берётся из текста — табличную эвристику выше он
    # не повторяет, потому что таблицы этих листов процентов не несут вовсе.
    if proto_name in NB_PROTOS:
        pct_digits, card_pct_digits = _nb_pct_digits(shapes)

    # примечание: в KZ дека оставила хвостовой пробел — часть её типографики
    note = shapes.get("note")
    note_text = ""
    if note is not None:
        note_runs = _runs_of(note)
        note_text = (note_runs[0].find(qn("a:t")).text or "") if note_runs else ""
    # знаки чисел в таблице и на карточках: у b1 — млн с одним знаком, у
    # p15/p16 — целые тыс. (карточки отделов и таблица листа 7). Решается
    # по фактическому тексту ячеек, как и группировка разрядов.
    cell_digits, pp_digits, pp_zero_digits = _number_digits(shapes, tables)
    return {"kpi_group": kpi_group, "cell_group": cell_group,
            "pct_digits": pct_digits, "total_pct_digits": total_pct_digits,
            # знаки «% укомпл.» в СТРОКЕ КАРТОЧКИ листов 4–6: у деки они свои
            # («96%» в карточке против «96,6%» в шапке того же листа)
            "card_pct_digits": card_pct_digits,
            "note_tail_space": note_text.endswith(" "),
            "cell_digits": cell_digits, "pp_digits": pp_digits,
            "pp_zero_digits": pp_zero_digits,
            # знак дробной части в паре «ПЧ/ФЧ»: листы 7/8 пишут её ТОЧКОЙ,
            # листы 4–6 — запятой, как все прочие числа листа
            "hc_dot": _hc_dot(shapes)}


def _nb_pct_digits(shapes: dict) -> tuple[int, int]:
    """Знаки «% укомпл.» листов 4–6: (шапка листа, карточки).

    Дека печатает процент укомплектованности по-разному в одной и той же
    строке листа: в шапке — с одним знаком («96,6% укомпл.»), в карточке — без
    него («96% укомпл.»). Поэтому знаки снимаются с фактических текстов двух
    шейпов, а не одним флагом на лист: заголовок `hc_note` и строка карточки
    `cardN_hc_line` — разные места деки.
    """
    import re as _re
    pct = _re.compile(r"(\d[\d\xa0 ]*(?:,\d+)?)\s*%\s*укомпл")

    def probe(name: str, default: int) -> int:
        sh = shapes.get(name)
        if sh is None:
            return default
        m = pct.search(_text_of(sh))
        if not m:
            return default
        return 1 if "," in m.group(1) else 0

    cards = 1
    for name in shapes:
        if name.endswith("hc_line"):
            cards = probe(name, 1)
            break
    return probe("hc_note", 1), cards


def _hc_dot(shapes: dict) -> bool:
    """Пишет ли дека пару «ПЧ/ФЧ» точкой (листы 7/8) или запятой (листы 4–6).

    Признак — сам текст ПАРЫ: у карточек отделов «372.4/348.9», у карточек
    направлений «2 441,9/2 342,2». Разряды в паре не группируются, поэтому
    точка внутри пары — всегда десятичная. Подпись перед парой («ПЧ/ФЧ 1ПГ
    2026: ») тоже содержит слэш, и по ней признак не снять: берём только
    фрагмент, где слэш стоит МЕЖДУ числами.
    """
    pair = re.compile(r"(\d[\d.,\s\xa0]*)/(\d[\d.,\s\xa0]*)")

    def probe(name: str) -> bool | None:
        sh = shapes.get(name)
        if sh is None:
            return None
        txBody = sh._element.find(qn("p:txBody"))
        if txBody is None:
            return None
        for t in txBody.iter(qn("a:t")):
            m = pair.search(t.text or "")
            if m:
                return "." in m.group(0)
        return None

    # у листов 7/8 пара стоит в карточках (`hc_line`), у листов 4–6 карточки
    # численности не несут — там она только в шапке (`hc_note`). Карточка
    # приоритетнее: в шапке листов 7/8 пара всегда с ЗАПЯТОЙ, и по ней признак
    # снялся бы неверно.
    for name in sorted(shapes, key=lambda n: (not n.endswith("hc_line"),
                                              n != "hc_note")):
        if not (name.endswith("hc_line") or name == "hc_note"):
            continue
        got = probe(name)
        if got is not None:
            return got
    return True


#: Регулярка числа деки в текстовом шейпе: «147,1 млн», «7 300 тыс. BYN»,
#: «+8,2%», «−0,35».
_NUM_TEXT = re.compile(r"^[+\u2212-]?[\d\xa0 ]*\d(?:[.,]\d+)?%?$")


def _digits_of(text: str) -> int | None:
    """Сколько знаков после запятой в числе-тексте (None — не число)."""
    token = (text or "").strip().split(" ")[0] if text else ""
    token = token.replace("%", "")
    if not _NUM_TEXT.match(token):
        return None
    return len(token.rsplit(",", 1)[1]) if "," in token else 0


def _number_digits(shapes: dict, tables: dict) -> tuple[int, int, int]:
    """Знаки денежных значений (`cell_digits`) и «п.п.» (`pp_digits`) листа.

    Третий элемент — знаки РОВНОГО нуля круга «п.п.»: дека печатает его
    полной точностью листа, и она своя у каждого («0,00» на 7/8, «0,0» на 3-м).

    Дека печатает деньги листов 1–3 в млн с ОДНИМ знаком («147,1»), а листов
    7/8 — целыми тысячами («7 300»). Знаки снимаются с фактических текстов:
    значения карточек отделов дают ноль знаков, карточки стран — один, таблицы
    b1 — один. Круги «п.п.» — до двух знаков («−0,35», «+0,24»), при этом ноль
    дека печатает полностью («0,00»), а прочие значения срезает («−0,1»).
    """
    money_votes: dict[int, int] = {}
    pp_votes: dict[int, int] = {}
    # знаки РОВНОГО нуля круга: дека печатает его полной точностью листа, и
    # точность эта своя у каждого листа («0,00» на 7/8, «0,0» на 3-м)
    pp_zero_votes: dict[int, int] = {}
    for name, sh in shapes.items():
        if _shape_kind(sh) != KIND_TEXT:
            continue
        if name.endswith("_pp_text"):
            # круг «п.п.»: первый абзац — значение, второй — единица
            paras = _paragraph_runs_xml(sh)
            if not paras or not paras[0]["runs"]:
                continue
            text = paras[0]["runs"][0]["text"]
            digits = _digits_of(text)
            if digits is not None:
                pp_votes[digits] = pp_votes.get(digits, 0) + 1
                if _is_zero_number(text):
                    pp_zero_votes[digits] = pp_zero_votes.get(digits, 0) + 1
            continue
        if name.endswith(("_value", "_dev")) or "_share" in name or name == "cc_value":
            runs = _runs_of(sh)
            if not runs:
                continue
            digits = _digits_of("".join(t.text or "" for t in runs[0].findall(qn("a:t"))))
            if digits is not None:
                money_votes[digits] = money_votes.get(digits, 0) + 1
    for gf in tables.values():
        tbl = gf.table
        for row in list(tbl.rows)[1:]:
            cells = list(row.cells)
            for cell in cells[1:]:
                runs = cell.text_frame.paragraphs[0].runs
                if not runs:
                    continue
                digits = _digits_of(runs[0].text)
                if digits is not None:
                    money_votes[digits] = money_votes.get(digits, 0) + 1
    cell_digits = max(money_votes, key=money_votes.get) if money_votes else 1
    # у «п.п.» берётся МАКСИМУМ знаков, а не самое частое: дека срезает
    # хвостовой ноль («−0,1» из −0,10), поэтому одно-знаковых значений на
    # листе всегда больше, и голосование дало бы 1 — тогда «+0,24» печаталось
    # бы как «+0,2». Точность задаёт самое подробное значение листа, срез
    # хвоста делает сам format.pp.
    pp_digits = max(pp_votes) if pp_votes else 2
    pp_zero_digits = (max(pp_zero_votes, key=pp_zero_votes.get)
                      if pp_zero_votes else pp_digits)
    return cell_digits, pp_digits, pp_zero_digits


#: Двузначный «хвост» года в текстах деки: «1ПГ25», «к факту 25», «Факт 1П25»,
#: «Ож.26». Хвост не должен продолжаться цифрой и не должен быть дробной
#: частью («25,6%», «21.2»): исключается и запятая, и ТОЧКА ПОСЛЕ ЦИФРЫ —
#: но не точка-сокращение («Ож.26», «Т.2ПГ26»), где перед ней стоит буква.
_Y2_RE = re.compile(r"(?<![\d,])(?<!\d\.)(\d{2})(?!\d)(?!,\d)(?!\.\d)")
#: Признак того, что двузначное число — ГОД, а не значение: слева маркер
#: периода («факт», «к», «ПГ», «год», «Δ»), справа — «%», «подразделениям»,
#: « п.п.». Без такого соседства «26» в «ПЧ/ФЧ: 28.5/26» — это факт 26 чел.
_YEAR2_LEFT_RE = re.compile(
    r"([12]\s*П(?:Г)?|факт\w*|год\w*|Δ|к|ож\w*|ожид\w*|такт\w*)\s*[.:]?\s*$",
    re.IGNORECASE)
_YEAR2_RIGHT_RE = re.compile(r"^\s*(%|п\.п\.|подраздел\w*|тыс|млн|г\.)", re.IGNORECASE)
#: Полный год в текстах деки («2026», «2025»); запятая после него допустима —
#: «…к ФАКТУ 2025, ФОТ» — это перечисление в подписи, а не число.
_Y4_RE = re.compile(r"(?<!\d)(20\d{2})(?!\d)")


def _label_templates(shapes: dict, consts: dict, proto_name: str) -> dict:
    """Подписи деки с ПРОРЕЗЯМИ вместо годов и значений — для биндеров.

    Подписи карточек, KPI-ленты и панели численности смешивают текст и числа
    («Динамика, п.п. (1ПГ26/1ПГ25): », «Доля ФОТ в выручке гр. (Факт 1ПГ26): »,
    «Откл. ФЧ к 1ПГ25: »). Держать такие формулировки в коде биндера нельзя:
    правка деки (другой год, другая подпись) потребовала бы правки кода, и
    «1:1 с декой» разъехалось бы молча. Поэтому здесь снимается ШАБЛОН:

      * страна листа («Беларусь — Отделы: …») → ``{country}``;
      * год отчётного периода → ``{year}``, предыдущего → ``{prev}``;
      * их двузначные хвосты («1ПГ26», «к факту 25») → ``{y2}`` / ``{p2}``;
      * числа ЗНАЧЕНИЙ → ``{v1}, {v2}, …`` сквозняком по всему шейпу.

    Содержимое манифеста при этом остаётся обезличенным: живых чисел, годов и
    названий стран деки в нём нет, только формулировки и номера прорезей.

    Подписи ячеек сетки (``card1_…``, ``card2_…``) сворачиваются в ``cardN_``,
    если у всех ячеек они совпадают: у листа 8 это 16 одинаковых наборов, и
    без сворачивания манифест распухал вчетверо без пользы.
    """
    year_cur, year_prev = _proto_years(proto_name)
    out: dict = {}
    for name, sh in shapes.items():
        if _shape_kind(sh) != KIND_TEXT or name in CONST_SHAPES:
            continue
        if name in ("obs", "total_bar"):
            continue
        if _is_static_label(name):
            # подпись-константа деки («Δ план»): биндеру там писать нечего,
            # текст остаётся в активе и токена не получает
            continue
        if name in CONST_TEXT_SHAPES:
            # шейп-константа целиком (бейдж блока «BY»/«ОТДЕЛЫ»): текст остаётся
            # в активе, токенов нет — биндер пишет только код страны в первом
            # абзаце, а второй («ОТДЕЛЫ») принадлежит оформлению деки
            if name == "badge":
                paras = _paragraph_runs_xml(sh)
                if len(paras) > 1 and paras[1]["runs"]:
                    consts["badge_block"] = paras[1]["runs"][0]["text"]
                # у листов 1–3 бейдж — это ПЕРИОД (год или «1 ПГ» + год), и
                # печатать его обязан биндер: структура тоже зависит от листа.
                # Отличить его от бейджа отделов (код страны + «ОТДЕЛЫ») можно
                # по содержимому: у периода нет букв, кроме маркера «ПГ».
                badge_slots = _period_badge_slots(paras, year_cur, year_prev)
                if badge_slots:
                    out[name] = badge_slots
            continue
        if name in CONST_RUN_SHAPES:
            # заголовок листа: первый ран — бренд деки («ГК «МАРК ФОРМЭЛЬ»  »),
            # он один на все прототипы и живёт в consts; шаблон подписи
            # строится по ОСТАЛЬНЫМ ранам того же абзаца
            lead, rest = _split_brand(_run_text(sh, 0))
            consts[name + "_lead"] = lead
            if name == "badge":
                # у листов 7/8 бейдж двухстрочный: код страны (данные) и
                # «ОТДЕЛЫ» (название блока деки, одинаковое для всех стран)
                consts[name + "_block"] = _run_text(sh, 1)
            paras = _paragraph_runs_xml(sh)
            if rest:
                # в первом ране после бренда осталась часть подписи (у листов
                # 7/8 это «Беларусь — Отделы: …»): она тоже шаблон, а не
                # константа — страна зависит от среза
                paras[0]["runs"][0]["text"] = rest
            numbered = {"n": 0}
            out[name] = _slot_list(paras, year_cur, year_prev, numbered,
                                   skip_first_run=not rest,
                                   country_slot=(name == "title"),
                                   group_slot=(name == "title"
                                               and proto_name == "nb_prod"))
            continue
        paras = _paragraph_runs_xml(sh)
        numbered = {"n": 0}
        slots: list[dict] = []
        for pi, para in enumerate(paras):
            changed = False
            runs = []
            for run in para["runs"]:
                text, filled = _template_of(run["text"], year_cur, year_prev,
                                            numbered)
                if name == "title":
                    text = _country_slot(text)
                    if proto_name == "nb_prod":
                        text = _group_slot(text)
                # подпись нужна в манифесте и тогда, когда в ране не было
                # ЧИСЕЛ, а только годы («Динамика, п.п. (2026/2025): »):
                # биндер берёт из неё формулировку, а не собирает свою
                runs.append({"text": text, "filled": filled})
                changed = changed or filled > 0 or text != run["text"]
            if changed:
                slots.append({"para": pi, "runs": runs})
        if slots and _is_label_slots(slots):
            out[name] = slots
    return _collapse_cards(out)


def _is_label_slots(slots: list[dict]) -> bool:
    """Подпись ли это, а не значение: в шаблоне есть текст КРОМЕ прорезей.

    Шейпы значений («{v1} млн», «{v1}», круги «п.п.») состоят из прорезей и
    знаков — их биндер пишет напрямую, и шаблон для них был бы лишним
    контрактом (и требовал бы передавать значения через подпись). Подписью
    считается текст, в котором после вырезания прорезей остались БУКВЫ.
    """
    import re as _re
    for slot in slots:
        for run in slot["runs"]:
            text = _SLOT_STRIP_RE.sub("", run["text"])
            if _re.search(r"[A-Za-zА-Яа-яЁё]", text):
                return True
    return False


#: Подписи-КОНСТАНТЫ деки: зависят только от оформления листа, не от данных.
#: Их текст остаётся в активе как есть, токена не ставится, биндер их не
#: трогает. Правило «нет цифр» тут не работает: без цифр бывают и ЗНАЧЕНИЯ
#: (коды стран «BY», «н/д», заголовки отделов), и обезличивать их обязано.
STATIC_LABELS = frozenset({"hc4_label"})


def _is_static_label(name: str) -> bool:
    """Подпись-константа деки (её текст не зависит от данных среза)."""
    import re as _re
    return _re.sub(r"^card\d+_", "cardN_", name) in STATIC_LABELS


#: Шейпы, у которых ПЕРВЫЙ ран абзаца — константа оформления (бренд деки).
CONST_RUN_SHAPES = frozenset({"title"})

#: Шейпы-константы целиком: текст — часть оформления листа, данных в нём нет.
CONST_TEXT_SHAPES = frozenset({"badge"})


#: Бренд деки в заголовке листа и его отбивка (два пробела).
BRAND = "ГК «МАРК ФОРМЭЛЬ»"
BRAND_SEP = "  "


def _split_brand(text: str) -> tuple[str, str]:
    """«ГК «МАРК ФОРМЭЛЬ»  Беларусь — …» → (бренд с отбивкой, остаток).

    Бренд — константа актива (он один на все листы), остаток — подпись листа,
    которая зависит от среза. Разделяем по ДВУМ пробелам: дека отделяет бренд
    именно ими, а в самом бренде двойных пробелов нет.
    """
    if not text.startswith(BRAND):
        return "", text
    tail = text[len(BRAND):]
    if not tail.startswith(BRAND_SEP):
        return BRAND, tail
    return BRAND + BRAND_SEP, tail[len(BRAND_SEP):]


def _run_text(shape, idx: int) -> str:
    """Текст рана idx первого абзаца (без создания txBody)."""
    runs = _runs_of(shape)
    if idx >= len(runs):
        return ""
    t = runs[idx].find(qn("a:t"))
    return (t.text or "") if t is not None else ""


def _period_badge_slots(paras: list[dict], year_cur: int,
                        year_prev: int) -> list[dict]:
    """Бейдж ПЕРИОДА листов 1–3 → шаблон (у отделов возвращает пустой список).

    Дека подписывает лист года одним абзацем («2026»), а листы полугодий —
    двумя («1 ПГ» / «2026»). Печатать это обязан биндер: у листа полугодия
    первая строка это НОМЕР ПГ, а не год, и без шаблона она оставалась бы в
    активе текстом деки.

    Бейдж листов 7/8 («BY» / «ОТДЕЛЫ») периода не несёт: у него во втором
    абзаце слово блока, поэтому он остаётся константой, а код страны пишет
    биндер в первом абзаце. Различаем по буквам: у периода их нет вовсе или
    только маркер «ПГ».
    """
    flat = [run["text"].strip() for para in paras for run in para["runs"]]
    letters = re.sub(r"ПГ?", "", " ".join(flat))
    if re.search(r"[A-Za-zА-Яа-яЁё]", letters):
        return []                       # бейдж отдела, не периода
    numbered = {"n": 0}
    slots: list[dict] = []
    for pi, para in enumerate(paras):
        runs = []
        for run in para["runs"]:
            text, filled = _template_of(run["text"], year_cur, year_prev,
                                        numbered)
            runs.append({"text": text, "filled": filled})
        if runs:
            slots.append({"para": pi, "runs": runs})
    return slots


def _slot_list(paras, year_cur, year_prev, numbered, skip_first_run=False,
               country_slot=False, group_slot=False):
    """Абзацы прототипа → список шаблонов (общий для шейпов и заголовка).

    country_slot=True заменяет название страны листа прорезью ``{country}``,
    group_slot=True — название группы ЦФО прорезью ``{group}``: обе подписи
    зависят от среза, и текстом деки в манифесте им быть нельзя.
    """
    slots: list[dict] = []
    for pi, para in enumerate(paras):
        changed = False
        runs = []
        for ri, run in enumerate(para["runs"]):
            if skip_first_run and pi == 0 and ri == 0:
                continue
            text, filled = _template_of(run["text"], year_cur, year_prev, numbered)
            if country_slot:
                text = _country_slot(text)
            if group_slot:
                text = _group_slot(text)
            runs.append({"text": text, "filled": filled})
            changed = changed or filled > 0 or text != run["text"]
        if changed:
            slots.append({"para": pi, "runs": runs})
    return slots


#: Прорезь шаблона и знаки, которые сами по себе подписью не являются.
_SLOT_STRIP_RE = re.compile(r"\{[a-z]+\d*\}|[^A-Za-zА-Яа-яЁё]+")


def _country_slot(text: str) -> str:
    """«Беларусь — Отделы: …» → «{country} — Отделы: …» (страна — прорезь).

    Заголовки листов деки построены как «<Страна> — <блок>», и название страны
    зависит от среза. Оставлять его текстом нельзя: манифест стал бы
    обезличенным не полностью, а выгрузка другой страны унаследовала бы
    «Беларусь» из актива. Признак — известное название страны перед « — »: у
    листов 1–3 заголовок другой («Бюджет ФОТ — свод по странам группы»), и
    прорези страны там быть не должно.
    """
    if " — " not in text:
        return text
    head, rest = text.split(" — ", 1)
    if head.strip() in DECK_COUNTRIES:
        return "{country} — " + rest
    return text


#: Названия стран, которыми дека подписывает листы (первое слово заголовка).
#: Их место в подписи занимает прорезь ``{country}``: страна листа зависит от
#: среза, а не от актива.
DECK_COUNTRIES = frozenset({
    "Беларусь", "Россия", "Казахстан", "Узбекистан",
    "Belarus", "Russia", "Kazakhstan", "Uzbekistan",
})


def _group_slot(text: str) -> str:
    """«Беларусь — Производство: структура ФОТ …» → «… — {group}: …».

    Лист 5 деки — один из листов «по группам ЦФО»: подпись листа называет
    группу («Производство»), и на других группах она другая. Прорезь ставится
    только у этого прототипа: у листов 4 и 6 слово перед двоеточием — название
    блока деки («ФОТ по направлениям бизнеса», «Продажи»), а не данные.
    """
    if " — " not in text or ":" not in text:
        return text
    head, rest = text.split(" — ", 1)
    before, after = rest.split(":", 1)
    return "%s — {group}:%s" % (head, after)


def _collapse_cards(labels: dict) -> dict:
    """Сворачивает подписи ячеек ``card1_…``/``card2_…`` в ``cardN_``.

    Подпись ячейки — её роль, а не номер места: у всех ячеек сетки листа 8
    («1 ПГ 2026», «ТАКТИКА 2 ПГ 2026», «к факту 25») она одинаковая. Если
    наборы у всех ячеек совпали — в манифесте остаётся один комплект с «N»;
    если хоть одна ячейка отличается, остаются конкретные имена (тихо
    схлопнуть разные подписи в одну нельзя — разъехался бы текст на слайде).
    """
    import re as _re
    groups: dict[str, dict[str, list]] = {}
    for name, slots in labels.items():
        m = _re.match(r"^card(\d+)_(.+)$", name)
        if not m:
            continue
        groups.setdefault(m.group(2), {})[m.group(1)] = slots
    collapsed: dict = {}
    kept: dict = {}
    for role, by_idx in groups.items():
        variants = {json.dumps(slots, ensure_ascii=False, sort_keys=True)
                    for slots in by_idx.values()}
        if len(variants) > 1:
            # подписи разные — остаются КОНКРЕТНЫЕ имена всех ячеек этой роли
            for idx, slots in by_idx.items():
                kept["card%s_%s" % (idx, role)] = slots
            continue
        collapsed["cardN_" + role] = next(iter(by_idx.values()))
    if not collapsed and not kept:
        return labels
    # сворачиваем ПО РОЛЯМ: у листа 7 «к факту 25» совпадает у всех ячеек
    # (сворачивается в cardN_), а «+9,1% к факту 25 (+606)» у каждой своё и
    # остаётся card1_/card2_/… — выбросить его значило бы потерять подпись
    out = {name: slots for name, slots in labels.items()
           if not _re.match(r"^card\d+_", name)}
    out.update(collapsed)
    out.update(kept)
    return out


def _template_of(text: str, year_cur: int, year_prev: int,
                 numbered: dict) -> tuple[str, int]:
    """Текст рана → шаблон с прорезями; второй элемент — сколько в нём данных.

    Порядок замен важен: сначала годы (полные, затем двузначные хвосты), и
    только потом числа значений. Годы подставляются через СЕНТИНЕЛ, а не
    сразу в текст: иначе номер внутри уже вставленного ``{p2}`` был бы принят
    за число значения и превратился бы в ``{p{v2}}``. Счётчик ``numbered``
    сквозной по шейпу («n»), поэтому прорези разных ранов не сталкиваются.

    Номера полугодий («1ПГ», «2 ПГ», «ТАКТИКА 2 ПГ») прорезями НЕ становятся:
    это структура листа, а не данные среза — у листа года блоки всегда 1ПГ и
    2ПГ, у листа полугодия все блоки про его полугодие. Сделай их прорезями,
    и биндеру пришлось бы передавать в каждую подпись свой номер блока,
    хотя он не меняется никогда.
    """
    if not text:
        return text, 0
    s = _Y4_RE.sub(lambda m: _SENT_YEAR if int(m.group(1)) == year_cur
                   else (_SENT_PREV if int(m.group(1)) == year_prev
                         else m.group(0)), text)
    y2, p2 = str(year_cur)[-2:], str(year_prev)[-2:]
    # ДВУЗНАЧНЫЙ хвост года заменяется только там, где он и есть год: за
    # числом-значением («ПЧ/ФЧ: 28.5/26» — факт 26 человек) или перед ним
    # («26 подразделениям»). Признак года в деке — соседний маркер: слово
    # «факт/к/ПГ/год», предлог «к», скобка или слэш после пары ПЧ/ФЧ.
    # Правило: заменяем ТОЛЬКО если в 12 знаках слева есть буква-маркер или
    # двоеточие-«после периода», иначе «26» — это значение
    def _year2(m: re.Match) -> str:
        if m.group(1) not in (y2, p2):
            return m.group(0)
        left = s[max(0, m.start() - 14):m.start()]
        right = s[m.end():m.end() + 14]
        marker = _YEAR2_LEFT_RE.search(left) or _YEAR2_RIGHT_RE.search(right)
        if not marker:
            return m.group(0)
        return _SENT_Y2 if m.group(1) == y2 else _SENT_P2

    s = _Y2_RE.sub(_year2, s)

    def _slot(m: re.Match) -> str:
        numbered["n"] += 1
        return "{v%d}" % numbered["n"]

    # числа-структуры листа (номер полугодия с маркером «1 ПГ», единица «п.п.»)
    # вырезаются из текста на время нумерации значений и возвращаются как есть.
    # Метка — БУКВЕННАЯ (\x02 + буква + \x02): цифровой индекс был бы съеден
    # регуляркой чисел и превратился бы в ложную прорезь значения.
    guarded: list[str] = []

    def _guard(m: re.Match) -> str:
        guarded.append(m.group(0))
        return "\x02%s\x02" % _guard_letter(len(guarded) - 1)

    s = _HALF_MARK_RE.sub(_guard, s)
    s, filled = _NUM_IN_LABEL_RE.subn(_slot, s)
    for i, original in enumerate(guarded):
        s = s.replace("\x02%s\x02" % _guard_letter(i), original)
    for sent, slot in ((_SENT_YEAR, "{year}"), (_SENT_PREV, "{prev}"),
                       (_SENT_Y2, "{y2}"), (_SENT_P2, "{p2}")):
        s = s.replace(sent, slot)
    return s, filled


#: Число значения внутри подписи: знак, разряды, запятая. Процент в шаблон
#: НЕ входит: в деке он стоит ПОСЛЕ значения («21,2%», «0,2%)», «96% укомпл.»)
#: и принадлежит подписи, а биндер пишет только число — иначе знак процента
#: пришлось бы добавлять кодом, и подпись разъехалась бы с декой.
#: Хвост разрядов обязан заканчиваться цифрой: иначе регулярка съедала ПРОБЕЛ
#: после одиночного числа и «детализация 1 полугодия» превращалась в
#: «{v1}полугодия» без отбивки.
#: «н/д» — ТОЖЕ значение, а не текст подписи: в деке так напечатано ОТСУТСТВИЕ
#: данных (у отдела нет базы 2025), и при живом срезе на этом месте будет
#: число. Оставь литерал текстом — и «н/д» застыло бы на слайде навсегда,
#: даже когда база появилась.
_NUM_IN_LABEL_RE = re.compile(r"н/д|[+−-]?\d(?:[\d\xa0 ]*\d)?(?:[.,]\d+)?")
#: Номер полугодия вместе с его маркером («1 ПГ», «1ПГ», «2 П») — структура
#: листа, а не данные среза. Маркер входит в группу, иначе он терялся бы.
_HALF_MARK_RE = re.compile(r"\d+\s*ПГ?")
#: Сентинелы годов на время подстановки чисел (не содержат цифр).
_SENT_YEAR, _SENT_PREV = "\x01Y\x01", "\x01P\x01"
_SENT_Y2, _SENT_P2 = "\x01y\x01", "\x01p\x01"


def _guard_letter(idx: int) -> str:
    """Буквенная метка защищённого фрагмента («a», «b», … «z», «za»)."""
    letters = "abcdefghijklmnopqrstuvwxyz"
    out = ""
    idx += 1
    while idx:
        idx, rem = divmod(idx - 1, 26)
        out = letters[rem] + out
    return out


def _is_zero_number(text: str) -> bool:
    """Ровно ноль ли это («0,0», «0,00», «−0,00»)? Знак деки — минус или дефис."""
    return bool(re.fullmatch(r"[+−\-]?0[.,]0+", (text or "").strip()))


def _proto_years(proto_name: str) -> tuple[int, int]:
    """(год отчётного периода деки, предыдущий) — один на все прототипы.

    Год снимается с листа 1 при сборке актива (`_deck_years`): все листы деки
    подписаны одним и тем же отчётным годом, а `proto_name` оставлен в
    сигнатуре, чтобы вызов читался по месту («годы этого прототипа»).
    """
    return _DECK_YEARS


#: Год деки и предыдущий — заполняются в build() с листа 1 («ФАКТ, ГОД 2025»).
_DECK_YEARS: tuple[int, int] = (0, 0)


def _deck_years(slide) -> tuple[int, int]:
    """Год отчётного периода деки по подписи KPI-ленты листа 1.

    Лист 1 — единственное место, где год назван явно и полностью («ФАКТ, ГОД
    2025» / «ОЖИДАНИЕ, ГОД 2026»). Берём максимальный полный год на листе:
    дека строит отчёт на год вперёд от факта, и он же стоит в бейдже.
    """
    years = set()
    for sh in slide.shapes:
        if _shape_kind(sh) != KIND_TEXT:
            continue
        for m in _Y4_RE.finditer(_text_of(sh)):
            years.add(int(m.group(1)))
    if not years:
        raise AssertionError("на листе 1 нет полного года — год деки не снять")
    cur = max(years)
    return cur, cur - 1


def _total_bar_colors(slides: dict) -> list[str]:
    """Цвета ранов полосы «Итого» — ПОЗИЦИОННЫЕ, а не по знаку отклонения.

    Проверено на всех трёх прототипах со полосой: «Δ2025» всегда красный
    (FF9E9E) — и при +30,5 (BY), и при −49,2 (RU); «Δутвержд.» всегда зелёный
    (8FDDB0) — и при −153,9 (BY), и при +91,3 (UZ). То есть полоса в деке
    раскрашена по месту, а не светофором; УФ ячеек (C0392B/1C8A5A) здесь не
    используется. Поэтому цвета снимаются с деки и подставляются как есть.
    """
    seen: list[tuple | None] = [None] * len(slides)
    for i, gf in enumerate(slides.values()):
        seen[i] = tuple(_run_color(r) for r in _runs_of(gf))
    colors = {c for c in seen if c is not None}
    if len(colors) > 1:
        raise AssertionError("полосы «Итого» раскрашены по-разному: %r"
                             % [c for c in seen if c is not None])
    if not colors:
        raise AssertionError("в активе нет полосы «Итого» — цвета ранов не снять")
    return list(colors.pop())


# ─── Обезличивание ───────────────────────────────────────────────────────────

#: Тексты-константы деки, которые биндер берёт из манифеста, а не пишет сам:
#: подписи единиц и периодов. Их значения снимаются при сборке актива, чтобы
#: правка деки не требовала правки кода (hex и подписи в биндерах запрещены).
CONST_SHAPES = frozenset({"unit_label"})


def _const_text_shapes(proto_name: str) -> frozenset:
    """Шейпы прототипа, чей текст — константа оформления, а не данные.

    У листов 4–6 так подписаны сноска под таблицей («тыс. BYN», у листа 6 ещё
    и оговорка о методике) и заголовок ленты мини-баров («ДОЛЯ ФОТ В ВЫРУЧКЕ
    ПО НАПРАВЛЕНИЯМ, %»). Это формулировки деки: от среза они не зависят, и
    биндер их не пишет — иначе токен остался бы незаполненным.
    """
    if proto_name in NB_PROTOS:
        return frozenset({"unit_note", "bars_head"})
    return frozenset()


def _obfuscate_shapes(shapes: dict, proto_name: str) -> dict:
    """Тексты адресуемых шейпов → плейсхолдеры. Возвращает текстовые константы.

    Правило одно на все прототипы: у КАЖДОГО адресуемого текстового шейпа раны
    заменяются токенами вида «{{<семантическое имя>_r<номер>}}» — имя уже
    семантическое (его проставил `_resolve`), поэтому биндер знает, куда писать,
    и не зависит от текстов деки. Многократные раны (`total_bar`, строки
    карточек) получают по токену на ран: пункт «подпись / значение / единица»
    восстанавливается биндером по своим данным, а не по тексту деки.

    Шейпы из CONST_SHAPES («млн BYN») текстом не обезличиваются: их текст
    кладётся в consts прототипа, и биндер подставляет его как константу —
    иначе в выгрузку уехала бы валюта среза, отличная от деки.

    Баннеры листов 7/8 — отдельный случай: их второй абзац это ПЕРЕМЕННОЕ
    число пар «имя — пояснение», и пошаблонная нумерация ранов на нём не
    работает (в деке 7 пар, в данных бывает 4). У баннера в consts снимаются
    неизменные части — заголовок и хвост-вывод, — а пары биндер строит сам
    копиями рана-образца.
    """
    consts: dict = {}
    static = _const_text_shapes(proto_name)
    for name, sh in shapes.items():
        if _shape_kind(sh) != KIND_TEXT:
            continue
        if name in CONST_SHAPES:
            consts[name] = _text_of(sh).strip()
            continue
        if name in static:
            # сноска и заголовок ленты листов 4–6: формулировка деки, данных в
            # ней нет — текст остаётся в активе, токена не ставим
            consts[name] = _text_of(sh).strip()
            continue
        if name in BANNER_SHAPES:
            consts.update(_banner_consts(sh, name))
            continue
        if name == "obs":
            consts.update(_obfuscate_obs(sh))
            continue
        if name == "total_bar":
            _obfuscate_runs(sh, name, count=5, prefix_reuse=True)
            continue
        if name == "donut_head":
            # двухстрочный заголовок доната: первая строка — формулировка деки
            # («ДОЛЯ ФОТ ПО НАПРАВЛЕНИЯМ,»), вторая — «ОЖИДАНИЕ 2026», и год в
            # ней зависит от фильтра. Первая строка уходит в consts, вторая
            # становится шаблоном подписи (токен на ран)
            consts[name] = _first_para_text(sh)
            txBody = sh._element.find(qn("p:txBody"))
            for p in list(txBody.findall(qn("a:p")))[1:]:
                for i, run in enumerate(p.findall(qn("a:r"))):
                    t = run.find(qn("a:t"))
                    if t is not None:
                        t.text = "{{%s_r%d}}" % (name, i)
            continue
        if _is_static_label(name):
            # подпись-константа деки («Δ план»): токен снимать НЕЛЬЗЯ —
            # биндер его не заполнит, и на слайде остался бы «{{…}}»
            continue
        if _is_value_shape(sh):
            # значение + единица («2649,0» + «  чел.»): единица — константа
            # оформления деки и остаётся ТЕКСТОМ в активе. Делать из неё токен
            # нельзя: биндеры единицу не пишут (они пишут только число), и
            # выгрузка уехала бы с «{{kpi1_value_r1}}» на слайде.
            _obfuscate_first_run(sh, name)
            continue
        _obfuscate_runs(sh, name)
    return consts


def _obfuscate_first_run(shape, name: str) -> None:
    """Токенизирует ТОЛЬКО первый ран значения; единица остаётся текстом."""
    runs = shape.text_frame.paragraphs[0].runs
    if not runs:
        raise AssertionError("шейп %r: абзац без ранов" % name)
    _set_text(runs[0], "{{%s}}" % name)


def _is_value_shape(shape) -> bool:
    """Числовое значение с единицей: первый ран — число, остальные — единица.

    Дека хранит «2649,0  чел.» двумя ранами одного абзаца: значение и отбивку
    с единицей. Второй ран — часть оформления (единица среза, два пробела), а
    не данные: его пишет не биндер, а сама дека. Поэтому обезличивается только
    первый ран, а остальные остаются как есть.
    """
    paras = _paragraph_runs_xml(shape)
    if len(paras) != 1 or len(paras[0]["runs"]) < 2:
        return False
    first = (paras[0]["runs"][0]["text"] or "").strip()
    return bool(_NUM_RE.match(first))


#: Шейпы-баннеры листов 7/8: их текст собирается из данных (пары «имя —
#: пояснение»), поэтому обезличиваются структурно, а не одним токеном на ран.
BANNER_SHAPES = frozenset({"banner", "banner_good", "banner_warn"})


def _banner_consts(shape, name: str) -> dict:
    """Заголовок и хвост баннера → consts; раны-образцы остаются в активе.

    Первый абзац — заголовок деки («ЛУЧШАЯ ТЕНДЕНЦИЯ: …»), он неизменен и
    уходит в consts. Второй абzац — пары «имя» + «(пояснение), »; образцом
    остаётся ПЕРВАЯ пара (её rPr берётся для всех остальных через
    `clone_run`), её текст заменяется токенами. У листа 7 хвостовой вывод
    («Умеренное превышение бюджета…») — отдельный ран в конце второго абзаца:
    он тоже константа деки и снимается в consts, а пары остаются перед ним.
    """
    tf = shape.text_frame
    paras = tf.paragraphs
    consts = {name + "_lead": paras[0].runs[0].text or ""}
    if len(paras) < 2:
        # у листа 7 баннер однострочный: «Тенденции и прогноз:  » + текст
        if len(paras[0].runs) > 1:
            _set_text(paras[0].runs[1], "{{%s_text}}" % name)
        _set_text(paras[0].runs[0], "{{%s_lead}}" % name)
        return consts
    tail_runs = paras[1].runs
    # хвост-вывод: последний ран, начинающийся с пробела (« Умеренное…»). Он
    # снимается ДО проверки чётности — иначе нечётное число ранов выглядело бы
    # поломкой пар, хотя это просто константа деки в конце списка
    tail = None
    last = (tail_runs[-1].text or "") if tail_runs else ""
    if len(tail_runs) > 2 and last.startswith(" "):
        consts[name + "_tail"] = last
        tail_runs[-1]._r.getparent().remove(tail_runs[-1]._r)
        tail_runs = paras[1].runs
    if len(tail_runs) < 2 or len(tail_runs) % 2:
        raise AssertionError("баннер %r: в списке пар %d ранов — ожидалось "
                             "чётное число (имя + пояснение)"
                             % (name, len(tail_runs)))
    pairs = len(tail_runs) // 2
    # в активе остаётся ОДНА пара-образец: остальные биндер клонирует
    for run in list(tail_runs)[2:]:
        run._r.getparent().remove(run._r)
    kept = paras[1].runs
    _set_text(kept[0], "{{%s_name}}" % name)
    _set_text(kept[1], "{{%s_note}}" % name)
    _set_text(paras[0].runs[0], "{{%s_lead}}" % name)
    consts[name + "_pairs"] = pairs
    return consts


def _first_para_text(shape) -> str:
    """Текст ПЕРВОГО абзаца шейпа (без обращения к text_frame)."""
    txBody = shape._element.find(qn("p:txBody"))
    if txBody is None:
        return ""
    paras = txBody.findall(qn("a:p"))
    if not paras:
        return ""
    return "".join(t.text or "" for t in paras[0].findall(qn("a:r") + "/" + qn("a:t")))


def _obfuscate_runs(shape, name: str, count: int | None = None,
                    prefix_reuse: bool = False) -> None:
    """Заменяет раны шейпа токенами: {{name}} для одного, {{name_rN}} для многих.

    Один ран — токен без номера: биндер пишет `set_run(…, 0, текст)`, и лишний
    индекс в имени только мешал бы. Несколько ранов нумеруются, потому что
    каждый несёт свою роль (подпись / значение / единица).

    Абзацев может быть НЕСКОЛЬКО, и это тоже роль, а не оформление:
      * круг «п.п.» листов 7/8 — два абзаца: значение и единица «п.п.»;
      * баннеры листов 7/8 — заголовок отдельным абзацем и перечисление пар
        «имя — пояснение» во втором, где каждая пара это ДВА рана.
    Поэтому абзацы (кроме первого) нумеруются префиксом `p<i>_`: токен
    `{{banner_good_p1_r3}}`. Биндер собирает абзацы сам (clone_paragraph),
    первый абзац прототипа остаётся для образца rPr.

    prefix_reuse=True сохраняет исторические имена полосы итога p17
    (`total_label`, `total_part1…4`): их уже читает биндер, и ломать контракт
    манифеста из-за новой вкладки нельзя.
    """
    tf = shape.text_frame
    paras = tf.paragraphs
    first = paras[0].runs
    if not first:
        raise AssertionError("шейп %r: абзац без ранов" % name)
    if count is not None and len(first) != count:
        raise AssertionError("шейп %r: ранов %d, ожидалось %d"
                             % (name, len(first), count))
    for pi, para in enumerate(paras):
        runs = para.runs
        # пустой абзац шаблона (banker's «a:p» без ранов) — не роль, пропуск
        if not runs:
            continue
        for i, run in enumerate(runs):
            if pi:
                token = "%s_p%d_r%d" % (name, pi, i)
            elif len(runs) == 1:
                token = name
            elif prefix_reuse:
                token = "total_label" if i == 0 else "total_part%d" % i
            else:
                token = "%s_r%d" % (name, i)
            _set_text(run, "{{%s}}" % token)


def _obfuscate_obs(shape) -> dict:
    """Блок наблюдений p17: заголовок + один абзац-буллет из трёх ранов."""
    consts: dict = {}
    tf = shape.text_frame
    _set_text(tf.paragraphs[0].runs[0], "{{obs_header}}")
    bullet = tf.paragraphs[1]
    runs = bullet.runs
    if len(runs) != 3:
        raise AssertionError("obs: у буллета %d ранов, ожидалось 3" % len(runs))
    # маркер «•  » — константа оформления, движку берётся из манифеста
    consts["obs_bullet"] = runs[0].text
    _set_text(runs[0], "{{obs_bullet}}")
    _set_text(runs[1], "{{obs_subject}}")
    _set_text(runs[2], "{{obs_text}}")
    for p in list(tf.paragraphs[2:]):
        p._p.getparent().remove(p._p)
    return consts


def _set_text(run, text: str) -> None:
    """Текст рана без потери rPr (прямое tf.text= запрещено — рвёт форматы)."""
    run.text = text


def _set_cell_placeholder(tc, token: str) -> None:
    """Ячейка → плейсхолдер в первом абзаце, остальные абзацы опустошаются.

    Абзацы-прототипы шапки («Утвержд. числ.» / «страт. 2026») сохраняются как
    структура: движок пишет по строке на абзац и берёт их rPr. Текст в них
    очищается — исходные подписи не должны просочиться в выгрузку.
    """
    txBody = tc.find(qn("a:txBody"))
    ps = txBody.findall(qn("a:p"))
    if not ps:
        raise AssertionError("ячейка таблицы без абзацев — селектор строк сломан")
    for pi, p in enumerate(ps):
        runs = p.findall(qn("a:r"))
        if not runs:
            raise AssertionError("абзац ячейки без ранов — селектор строк сломан")
        for r in runs[1:]:
            p.remove(r)
        t = runs[0].find(qn("a:t"))
        if t is None:
            t = etree.SubElement(runs[0], qn("a:t"))
        t.text = token if pi == 0 else ""


def _header_labels(header_tr, proto_name: str) -> list[dict]:
    """Подписи шапки таблицы: абзацы-строки + прорези годов, как у шейпов.

    Дека ломает длинные подписи на абзацы («Факт» / «1 ПГ 2025»), и это её
    типографика: биндер пишет по строке на абзац. Годы в подписях заменяются
    прорезями (`{year}/{prev}/{y2}/{p2}`) тем же правилом, что и у текстовых
    шейпов, поэтому смена отчётного года не требует правки кода.
    """
    year_cur, year_prev = _proto_years(proto_name)
    numbered: dict = {"n": 0}
    out: list[dict] = []
    for ci, tc in enumerate(header_tr.findall(qn("a:tc"))):
        paras = []
        for p in tc.findall(qn("a:txBody") + "/" + qn("a:p")):
            runs = [t.text or "" for t in p.findall(qn("a:r") + "/" + qn("a:t"))]
            text = "".join(runs)
            template, _filled = _template_of(text, year_cur, year_prev, numbered)
            paras.append(template)
        out.append({"col": ci, "paragraphs": paras})
    return out


def _obfuscate_tables(tables: dict, proto_name: str, dump: dict,
                      headers: dict | None = None) -> dict:
    """Сжатие таблиц до строк-прототипов + плейсхолдеры в ячейках.

    Возвращает {имя таблицы: {row_protos: [...], capacity: N, …}}.

    `headers` (если передан) наполняется шаблонами подписей шапки ДО замены
    ячеек на плейсхолдеры: подписи колонок — формулировки деки («Факт /
    1 ПГ 2025», «Откл. абс. / Факт/Бюджет»), и биндер обязан брать их отсюда,
    а не из своего кода. Ключ — имя колонки в нижнем регистре без пунктуации,
    чтобы биндер мог сопоставить подпись со своей колонкой данных, не зная
    порядка колонок деки.
    """
    out: dict = {}
    for tbl_name, gf in tables.items():
        tbl = gf.table
        trs = tbl._tbl.findall(qn("a:tr"))
        styles = _row_styles(tbl)
        if "header" not in styles:
            raise AssertionError("%s/%s: в таблице нет строки-шапки"
                                 % (proto_name, tbl_name))
        # у KZ итог стоит ПЕРВОЙ строкой после шапки — это отдельный прототип
        total_top = len(styles) > 1 and styles[1] == "total"

        keep: dict[int, str] = {0: "header"}
        # строки-ГРУППЫ листа 6 («Розница РБ», «ОПТ РБ») — своя заливка и жирный
        # текст; в вебе им соответствует kind="direction". В активе нужен один
        # образец: без него биндер не смог бы напечатать групповую строку
        for style in ("group", "body_tinted", "body_plain"):
            for i, st in enumerate(styles):
                if st == style:
                    keep[i] = style
                    break
        for i, st in enumerate(styles):
            if st != "total":
                continue
            keep[i] = "total_top" if (total_top and i == 1) else "total"

        if dump is not None:
            dump[tbl_name] = {
                "capacity": sum(1 for s in styles
                                if s in ("body_tinted", "body_plain")),
                # порядок прототипов = ФИЗИЧЕСКИЙ порядок строк в активе
                # (sorted по исходному индексу): у KZ итог стоит второй строкой,
                # и движок сопоставляет стили строкам по этому списку
                "row_protos": [style for _i, style in sorted(keep.items())],
                "paragraphs": len(trs[0].findall(qn("a:tc"))[0]
                                  .findall(qn("a:txBody") + "/" + qn("a:p"))),
                "header_cells": [_cell_paragraph_runs(tc) for tc in trs[0].findall(qn("a:tc"))],
                "rows": [{"style": styles[i],
                          "cells": [_cell_paragraph_runs(tc) for tc in trs[i].findall(qn("a:tc"))]}
                         for i in range(len(trs))],
            }

        for i, style in keep.items():
            token_fmt = "{{h_c%d}}" if style == "header" else "{{c%d}}"
            if style == "header" and headers is not None:
                headers[tbl_name] = _header_labels(trs[i], proto_name)
            for ci, tc in enumerate(trs[i].findall(qn("a:tc"))):
                _set_cell_placeholder(tc, token_fmt % ci)

        for i in reversed(range(len(trs))):
            if i not in keep:
                tbl._tbl.remove(trs[i])

        out[tbl_name] = {
            # capacity — ТОЛЬКО строки тела (tinted + plain): шапка, итог и
            # строки-группы в неё не входят. Так пагинация не съезжает: у KZ в
            # деке было header + total + 8 body = 10 строк, и 8 — это body.
            # Групповая строка листа 6 («Розница РБ») — тоже НЕ тело: она
            # печатается из данных, а не занимает место расшифровки.
            "capacity": sum(1 for s in styles if s in ("body_tinted", "body_plain")),
            "capacity_basis": "body_rows",
            "total_rows": sum(1 for s in styles if s == "total"),
            # сколько строк было в таблице деки: высота graphicFrame в деке
            # пропорциональна числу строк (~214 тыс. EMU на строку у слайда 24),
            # а в активе осталось 3–4 прототипа при неизменном cy — по этому
            # числу движок восстанавливает высоту на строку
            "deck_rows": len(trs),
            # физический порядок строк актива (см. комментарий к dump выше)
            "row_protos": [style for _i, style in sorted(keep.items())],
        }
    return out


def _normalize_cell_colors(tables: dict, sign: dict) -> None:
    """Плейсхолдеры тела/итога — в базовый цвет их строки.

    В деке Δ-ячейки цветные (+красный / −зелёный); после обезличивания знака
    нет, цвет обязан стать базовым, иначе прототип тянул бы УФ на все строки.
    Шапка остаётся белой.
    """
    for gf in tables.values():
        tbl = gf.table
        styles = _row_styles(tbl)
        for row, style in zip(tbl.rows, styles):
            base = sign["total_text"] if style.startswith("total") else sign["body_text"]
            if style == "header":
                continue
            for cell in row.cells:
                for p in cell.text_frame.paragraphs:
                    for run in p.runs:
                        if "{{" in (run.text or ""):
                            _set_run_color(run, base)


# ─── Диаграммы листов 4–6: обезличивание данных и слоты манифеста ────────────

#: Цвет точки, которую дека НЕ красит: она наследует заливку серии (первый
#: мини-бар). В манифест уходит как None — биндер обязан сохранить разницу.
NB_BAR_POINT_UNTINTED = None


def _chart_ser(shape):
    """Первая `c:ser` диаграммы (дека рисует ровно одну серию)."""
    chart = shape.chart
    space = chart._chartSpace
    return space.find(".//" + qn("c:ser")), space


def _chart_point_colors(shape) -> list[str | None]:
    """Цвета `c:dPt` по индексам точек (None — точка красится серией)."""
    ser, _space = _chart_ser(shape)
    if ser is None:
        return []
    out: list[str | None] = []
    for dpt in ser.findall(qn("c:dPt")):
        idx_el = dpt.find(qn("c:idx"))
        idx = int(idx_el.get("val")) if idx_el is not None else len(out)
        clr = dpt.find(".//" + qn("a:srgbClr"))
        while len(out) < idx:
            out.append(None)
        if len(out) == idx:
            out.append(clr.get("val") if clr is not None else None)
        else:
            out[idx] = clr.get("val") if clr is not None else None
    return out


def _chart_series_name(shape) -> str:
    """Имя серии из `c:tx` — формулировка деки («Доля ФОТ в выручке»)."""
    ser, _space = _chart_ser(shape)
    if ser is None:
        return ""
    tx = ser.find(qn("c:tx"))
    if tx is None:
        return ""
    for v in tx.iter(qn("c:v")):
        return v.text or ""
    return ""


def _chart_num_fmt(shape) -> str | None:
    """Формат подписей `c:dLbls/c:numFmt` («0.0%», «0.0\\"%\\"»)."""
    ser, _space = _chart_ser(shape)
    if ser is None:
        return None
    dLbls = ser.find(qn("c:dLbls"))
    if dLbls is None:
        return None
    nf = dLbls.find(qn("c:numFmt"))
    return nf.get("formatCode") if nf is not None else None


def _chart_meta(shape) -> dict:
    """Слот манифеста по диаграмме прототипа (тип, точки, цвета, подписи)."""
    ser, space = _chart_ser(shape)
    if ser is None:
        raise AssertionError("диаграмма %r: нет c:ser — нечего описывать" % shape.name)
    n_ser = len(space.findall(".//" + qn("c:ser")))
    cats = [str(c) for c in shape.chart.plots[0].categories]
    colors = _chart_point_colors(shape)
    dLbls = ser.find(qn("c:dLbls"))
    kind = str(shape.chart.chart_type).split(" ")[0].lower()
    meta = {
        "type": kind,
        "series": n_ser,
        "points": len(cats),
        # цвета dPt — ПОЗИЦИОННЫЕ, снятые с деки: None — точка без своей
        # заливки (первый мини-бар наследует цвет серии)
        "point_colors": colors,
        "series_name": _chart_series_name(shape),
        "num_fmt": _chart_num_fmt(shape),
        # какие подписи рисует дека: у доната доля, у мини-баров значение
        "show_percent": (dLbls is not None
                         and dLbls.find(qn("c:showPercent")) is not None
                         and dLbls.find(qn("c:showPercent")).get("val") == "1"),
        "show_val": (dLbls is not None
                     and dLbls.find(qn("c:showVal")) is not None
                     and dLbls.find(qn("c:showVal")).get("val") == "1"),
    }
    if kind != "pie":
        # подписи КАТЕГОРИЙ мини-баров — формулировки деки с ГОДАМИ («Факт
        # 1ПГ25», «Ожид.2026»), и год в них из фильтра: в манифест они идут
        # шаблоном, как подписи шейпов. У доната категории — НАЗВАНИЯ
        # направлений: это данные среза, и в манифест они попадать не должны
        numbered: dict = {"n": 0}
        year_cur, year_prev = _DECK_YEARS
        meta["categories"] = [_template_of(c, year_cur, year_prev, numbered)[0]
                              for c in cats]
    return meta


def _scrub_chart(shape) -> None:
    """Заменяет данные диаграммы заглушкой, сохраняя ВСЁ оформление.

    `replace_data` переписывает категории/значения и встроенную книгу, а
    оформление (заливка серии, цвета `c:dPt`, подписи `c:dLbls`, имя серии)
    оставляет. Поэтому в активе остаются нули и текстовые категории, а цвета
    точек, формат подписей и легенда — декины: актив обезличен, вёрстка цела.
    """
    from pptx.chart.data import CategoryChartData
    ser, _space = _chart_ser(shape)
    meta = _chart_meta(shape)
    data = CategoryChartData()
    data.categories = ["к%d" % (i + 1) for i in range(meta["points"])]
    data.add_series(meta["series_name"] or "серия",
                    tuple(0.0 for _ in range(meta["points"])))
    shape.chart.replace_data(data)
    # после подстановки число точек то же (заглушка строится по прототипу),
    # поэтому цвета и подписи пересобирать не надо — но проверим это, иначе
    # актив молча потеряет окрас секторов
    if _chart_point_colors(shape) != meta["point_colors"]:
        raise AssertionError(
            "диаграмма %r: после обезличивания цвета dPt изменились (%r → %r) — "
            "replace_data больше не сохраняет оформление"
            % (shape.name, meta["point_colors"], _chart_point_colors(shape)))


# ─── Чистка пакета ───────────────────────────────────────────────────────────

def _drop_other_slides(prs, keep_numbers: set[int]) -> int:
    """Удаляет все слайды, кроме прототипов; снимает заметки докладчика."""
    sldIdLst = prs.slides._sldIdLst
    ids = list(sldIdLst)
    dropped = 0
    for i in reversed(range(len(ids))):
        if (i + 1) in keep_numbers:
            continue
        prs.part.drop_rel(ids[i].get(qn("r:id")))
        sldIdLst.remove(ids[i])
        dropped += 1
    # заметки — не часть шаблона (в них реплики автора деки)
    for slide in prs.slides:
        for rId, rel in list(slide.part.rels.items()):
            if rel.reltype.endswith("/notesSlide"):
                slide.part.drop_rel(rId)
    return dropped


def _scrub_properties(prs) -> None:
    """Обезличивание метаданных: core-properties."""
    cp = prs.core_properties
    cp.author = ""
    cp.last_modified_by = ""
    cp.title = "ФОТ шаблон"
    cp.subject = ""
    cp.comments = ""
    cp.keywords = ""
    cp.category = ""


def _set_first_slide_num(prs, value: int = 1) -> bool:
    """ppt/presProps.xml firstSlideNum (у python-pptx нет API — правим блоб)."""
    for part in prs.part.package.iter_parts():
        if str(part.partname) != "/ppt/presProps.xml":
            continue
        root = etree.fromstring(part.blob)
        if root.get("firstSlideNum") == str(value):
            return True
        root.set("firstSlideNum", str(value))
        part._blob = etree.tostring(root, xml_declaration=True,
                                    encoding="UTF-8", standalone=True)
        return True
    return False


# ─── Точка входа ─────────────────────────────────────────────────────────────


# ─── Цвета листов 1–3 и 7–8 (свои, не p17) ───────────────────────────────────

def _b1_colors(by_proto: dict) -> dict:
    """Цвета листов 1–3: подложка карточки страны, её рамка и разделители.

    У b1 карточка страны — roundRect с заливкой F8EDF0 и рамкой E6D2D9; строки
    таблицы красятся теми же цветами, что у p17 (row_colors), но разделители
    между KPI-блоками (E0CBD3) и на панели численности (E4DDE0) здесь другие.
    Снимаем ровно те, что видит биндер: hex в коде не держим.
    """
    slide = by_proto["b1_year"]
    card_bg = card_border = sep = hc_div = None
    for sh in slide.shapes:
        kind = _shape_kind(sh)
        prst = sh._element.find(".//" + qn("a:prstGeom"))
        if kind == KIND_ROUNDRECT and card_bg is None:
            spPr = sh._element.find(qn("p:spPr"))
            fill = spPr.find(qn("a:solidFill") + "/" + qn("a:srgbClr"))
            card_bg = fill.get("val") if fill is not None else None
            ln = spPr.find(qn("a:ln") + "/" + qn("a:solidFill") + "/" + qn("a:srgbClr"))
            card_border = ln.get("val") if ln is not None else None
        elif kind == KIND_LINE:
            ln = sh._element.find(qn("p:spPr") + "/" + qn("a:ln") + "/"
                                  + qn("a:solidFill") + "/" + qn("a:srgbClr"))
            color = ln.get("val") if ln is not None else None
            if color is None:
                continue
            # на панели численности разделители свои: имена деки HCDiv…
            if sh.name.startswith("HCDiv"):
                hc_div = hc_div or color
            else:
                sep = sep or color
    out = {"card_bg": card_bg, "card_border": card_border,
           "sep": sep, "hc_div": hc_div}
    missing = [k for k, v in out.items() if not v]
    if missing:
        raise AssertionError("не сняты цвета листов 1–3: %r" % (missing,))
    return out


def _dept_colors(by_proto: dict) -> dict:
    """Цвета листов 7/8: фон карточки отдела, её рамка и разделители.

    У отделов карточка — F8F5F6 с рамкой 9B5A70 и внутренними разделителями
    D9C3CB; это ДРУГАЯ палитра, чем у карточек стран на листах 1–3. Баннеры
    («тенденции» и «второй приоритет») залиты своими цветами — они тоже здесь,
    иначе биндер не отличит зелёный баннер от розового по данным.
    """
    slide = by_proto["dept_summary"]
    card_bg = card_border = div = good_bg = good_border = None
    for sh in slide.shapes:
        kind = _shape_kind(sh)
        spPr = sh._element.find(qn("p:spPr"))
        if kind == KIND_ROUNDRECT:
            fill = spPr.find(qn("a:solidFill") + "/" + qn("a:srgbClr"))
            bg = fill.get("val") if fill is not None else None
            ln = spPr.find(qn("a:ln") + "/" + qn("a:solidFill") + "/" + qn("a:srgbClr"))
            border = ln.get("val") if ln is not None else None
            if card_bg is None and bg is not None:
                card_bg, card_border = bg, border
            elif bg is not None and good_bg is None and bg != card_bg:
                good_bg, good_border = bg, border
        elif kind == KIND_LINE and div is None:
            ln = spPr.find(qn("a:ln") + "/" + qn("a:solidFill") + "/" + qn("a:srgbClr"))
            div = ln.get("val") if ln is not None else None
    # Баннеры листа 8: цвет живёт на рамке-подложке (roundRect), текст — в
    # отдельном шейпе поверх. Имена шейпам ещё НЕ проставлены (цвета снимаются
    # до разметки), поэтому подложка находится по тексту, который лежит над ней:
    # берём roundRect с наибольшим пересечением с текстом баннера.
    slide8 = by_proto["dept_cards"]
    banner = {"good": _banner_box(slide8, "ЛУЧШАЯ ТЕНДЕНЦИЯ"),
              "warn": _banner_box(slide8, "ВТОРОЙ ПРИОРИТЕТ")}
    out = {"card_bg": card_bg, "card_border": card_border, "divider": div,
           "banner_good": banner.get("good"), "banner_warn": banner.get("warn")}
    missing = [k for k, v in out.items() if not v]
    if missing:
        raise AssertionError("не сняты цвета листов 7/8: %r" % (missing,))
    return out


def _nb_colors(by_proto: dict, prs) -> dict:
    """Цвета листов 4–6: подложка карточки направления, рамка и разделители.

    Карточка направления залита F1F1F3 с рамкой 9B5A70 и внутренними
    разделителями D9C3CB — это ТРЕТЬЯ палитра деки (у стран F8EDF0/E6D2D9,
    у отделов F8F5F6). Круги Δ доли держат цвет на ОБВОДКЕ, а единицу «п.п.» и
    подпись круга дека печатает приглушённым серым — он тоже здесь, иначе
    биндер взял бы цвет из другого листа.

    Палитры доната и мини-баров снимаются с САМИХ диаграмм: они лежат в
    манифесте слотами (`charts[].point_colors`), а не здесь, потому что у
    листов 4/5 и 6 порядок цветов разный.
    """
    slide = by_proto["nb_summary"]
    card_bg = card_border = divider = muted = circle_fill = None
    for sh in slide.shapes:
        kind = _shape_kind(sh)
        spPr = sh._element.find(qn("p:spPr"))
        if kind == KIND_ROUNDRECT and card_bg is None:
            fill = spPr.find(qn("a:solidFill") + "/" + qn("a:srgbClr"))
            bg = fill.get("val") if fill is not None else None
            ln = spPr.find(qn("a:ln") + "/" + qn("a:solidFill") + "/" + qn("a:srgbClr"))
            if bg is not None:
                card_bg, card_border = bg, (ln.get("val") if ln is not None else None)
        elif kind == KIND_LINE and divider is None:
            ln = spPr.find(qn("a:ln") + "/" + qn("a:solidFill") + "/" + qn("a:srgbClr"))
            divider = ln.get("val") if ln is not None else None
        elif kind == KIND_ELLIPSE:
            fill = spPr.find(qn("a:solidFill") + "/" + qn("a:srgbClr"))
            color = fill.get("val") if fill is not None else None
            # круг Δ доли залит БЕЛЫМ (цвет несёт обводка), круг бейджа — нет;
            # берём именно белый, иначе в палитру попадёт заливка бейджа
            if color == "FFFFFF" and circle_fill is None:
                circle_fill = color
    # приглушённый серый деки — цвет ЕДИНИЦЫ «п.п.»: у круга Δ доли два абзаца,
    # значение (светофорное) и единица (всегда серая). Ищем шейп, у которого
    # второй абзац — ровно «п.п.», и снимаем цвет его рана
    for sh in slide.shapes:
        if _shape_kind(sh) != KIND_TEXT:
            continue
        txBody = sh._element.find(qn("p:txBody"))
        if txBody is None:
            continue
        paras = txBody.findall(qn("a:p"))
        if len(paras) < 2:
            continue
        unit = "".join(t.text or "" for t in paras[1].findall(qn("a:r") + "/"
                                                             + qn("a:t")))
        if unit.strip() != "п.п.":
            continue
        for run in paras[1].findall(qn("a:r")):
            color = _run_color(run)
            if color:
                muted = color
                break
        if muted:
            break
    out = {"card_bg": card_bg, "card_border": card_border, "divider": divider,
           "circle_fill": circle_fill, "muted": muted,
           # палитра доната — ПРАВИЛО ВЕБА (capDonut): цвета по индексу
           # показанного сегмента, свёрнутые — нейтральным. Порядок задаёт
           # экран, но каждый цвет обязан существовать в деке (проверка ниже)
           "pie_palette": list(NB_PIE_ORDER), "pie_rest": NB_PIE_REST}
    missing = [k for k, v in out.items() if not v]
    if missing:
        raise AssertionError("не сняты цвета листов 4–6: %r" % (missing,))
    _assert_palette_in_deck(prs)
    return out


#: Порядок палитры доната — ПРАВИЛО ВЕБА (capDonut в nuxt/pages/reports/fot.vue):
#: цвета назначаются ПО ИНДЕКСУ показанного сегмента после свёртки, поэтому
#: порядок здесь и есть контракт с экраном. Часть цветов взята с диаграмм деки
#: и проверяется по ней (ниже); остальные экран добавил сам, чтобы девяти
#: сегментам хватило различимых оттенков — они помечены `NB_PIE_WEB_ONLY`.
NB_PIE_ORDER = ("7A1F3D", "F4A6C1", "C2185B", "E85D75", "FFC845",
                "9E6B0F", "9B5A70", "C27B92", "6B3347")
#: Цвета палитры, которых в деке НЕТ: их добавил веб, чтобы различить больше
#: сегментов (палитра `_P14_PIE_COLORS` в app/fot.py). Держим их списком, чтобы
#: проверка «палитра опирается на деку» оставалась честной.
NB_PIE_WEB_ONLY = frozenset({"9E6B0F"})
#: Цвет свёрнутых сегментов («Прочие») — нейтральный, как на экране.
NB_PIE_REST = "C9AEB6"


def _assert_palette_in_deck(prs) -> None:
    """Каждый цвет палитры доната обязан встречаться в деке.

    Экран назначает цвета по индексу показанного сегмента, и его палитра — это
    правило отчёта, а не вольная роспись: цвета взяты с диаграмм деки (dPt
    секторов и заливки серий). Проверка ловит случай, когда палитру правили
    руками и она разъехалась с исходником.
    """
    seen: set[str] = set()
    for slide in prs.slides:
        for sh in slide.shapes:
            if _shape_kind(sh) != KIND_CHART:
                continue
            for color in _chart_point_colors(sh):
                if color:
                    seen.add(color)
            ser, _space = _chart_ser(sh)
            if ser is None:
                continue
            spPr = ser.find(qn("c:spPr"))
            if spPr is not None:
                for clr in spPr.iter(qn("a:srgbClr")):
                    seen.add(clr.get("val"))
    # «Прочие» — цвет заливки СЕРИИ мини-баров (нейтральный столбик деки)
    want = [c for c in NB_PIE_ORDER + (NB_PIE_REST,) if c not in NB_PIE_WEB_ONLY]
    missing = [c for c in want if c not in seen]
    if missing:
        raise AssertionError(
            "палитра доната листов 4–6 разошлась с декой: цветов %r в ней нет — "
            "правило capDonut и палитра экрана должны опираться на деку"
            % (missing,))


def _banner_box(slide, lead: str) -> dict | None:
    """Цвета баннера-подложки по началу его текста («ЛУЧШАЯ ТЕНДЕНЦИЯ…»).

    Подложка и текст баннера — разные шейпы: roundRect залит цветом вывода,
    текстбокс лежит поверх. Находим текст по началу, затем — ближайший
    roundRect, пересекающийся с ним по геометрии (рамка карточки не подойдёт:
    она выше и уже).
    """
    target = None
    for sh in slide.shapes:
        if _shape_kind(sh) != KIND_TEXT:
            continue
        if _text_of(sh).strip().upper().startswith(lead):
            target = sh
            break
    if target is None:
        return None
    tx, ty, tw, th = _box_of(target)
    best, best_area = None, 0
    for sh in slide.shapes:
        if _shape_kind(sh) != KIND_ROUNDRECT:
            continue
        x, y, w, h = _box_of(sh)
        if not (x <= tx and y <= ty and x + w >= tx + tw * 0.5 and y + h >= ty):
            continue
        area = w * h
        if area > best_area:
            best, best_area = sh, area
    if best is None:
        return None
    spPr = best._element.find(qn("p:spPr"))
    fill = spPr.find(qn("a:solidFill") + "/" + qn("a:srgbClr"))
    ln = spPr.find(qn("a:ln") + "/" + qn("a:solidFill") + "/" + qn("a:srgbClr"))
    return {"bg": fill.get("val") if fill is not None else None,
            "border": ln.get("val") if ln is not None else None}


def _box_of(shape) -> tuple[int, int, int, int]:
    """(x, y, cx, cy) шейпа в EMU; нет трансформации — нули."""
    xfrm = shape._element.find(qn("p:spPr") + "/" + qn("a:xfrm"))
    if xfrm is None:
        xfrm = shape._element.find(qn("p:xfrm"))
    if xfrm is None:
        return 0, 0, 0, 0
    off, ext = xfrm.find(qn("a:off")), xfrm.find(qn("a:ext"))
    if off is None or ext is None:
        return 0, 0, 0, 0
    return (int(off.get("x")), int(off.get("y")),
            int(ext.get("cx")), int(ext.get("cy")))


# ─── Служебные карты манифеста ───────────────────────────────────────────────

def _count_cards(slide) -> int | None:
    """Число ячеек сетки карточек в активе (`card{i}_…` в именах шейпов).

    None — у прототипа сетки нет (p17, свод, листы 1–3 с четырьмя карточками
    стран: там ячейки фиксированы декой и не удаляются). Число нужно биндеру:
    данных меньше — лишние ячейки удаляются, больше — вторая страница-клон.
    Считается по САМОМУ СЛАЙДУ: сетки переименовываются напрямую, минуя `take`.
    """
    import re as _re
    # только настоящие ячейки: `card7_p1_value`, но не `cards_head`
    cards = {m.group(1) for m in
             (_re.match(r"^card(\d+)_", sh.name) for sh in slide.shapes) if m}
    return len(cards) or None


def _card_slots(shapes: dict) -> list[str] | None:
    """Коды стран по местам карточек сетки: `["BY", "RU", …]` (структура деки).

    Дека размещает страны по ФИКСИРОВАННЫМ местам: BY в первой ячейке, RU во
    второй и т. д. Это геометрия листа, а не данные среза, поэтому карта
    «место → страна» снимается с деки в манифест: иначе биндеру пришлось бы
    держать её в коде, и перестановка стран в деке разъехалась бы с выгрузкой
    молча. Берётся ДО обезличивания — в активе на месте кода уже токен.
    """
    import re as _re
    slots: dict[int, str] = {}
    for name, sh in shapes.items():
        m = _re.match(r"^card(\d+)_cc_code$", name)
        if not m:
            continue
        text = _text_of(sh).strip()
        if text:
            slots[int(m.group(1))] = text
    if not slots:
        return None
    # места идут подряд с 1: пропуск означал бы разметку не той сетки
    if sorted(slots) != list(range(1, len(slots) + 1)):
        raise AssertionError("места карточек размечены с пропуском: %r"
                             % sorted(slots))
    return [slots[i] for i in range(1, len(slots) + 1)]


def _table_placeholders(table_meta: dict) -> list[str]:
    """Токены ячеек таблиц: ``{{h_cN}}`` для шапки и ``{{cN}}`` для тела.

    Свёрнуты по номеру колонки, как и токены шейпов: актива с 9 колонками и
    шапкой из 9 ячеек даёт две записи вместо восемнадцати.
    """
    if not table_meta:
        return []
    # у каждой таблицы актива есть шапка и тело — оба набора токенов свёрнуты
    # по номеру колонки, поэтому достаточно объявить их по одному разу
    return ["{{h_cN}}", "{{cN}}"]


def _placeholders_of(slide) -> list[str]:
    """Токены прототипа, свёрнутые по номерам: «{{cardN_p1_value}}».

    Движок при сборке сверяет множество токенов актива с объявленными (B5):
    токен, которого нет в этом списке, значит «в активе есть шейп, до которого
    биндер не дотянулся», и выгрузка падает с внятной причиной, а не отдаёт
    заказчику шаблон с плейсхолдерами. Номера свёрнуты в «N», поэтому одна
    запись покрывает kpi1…kpi5 и card1…card16.

    Имена берутся из САМОГО СЛАЙДА (а не из карты аннотации): сетки карточек
    переименовываются напрямую, минуя `take`, и карта про них не знает.

    Диаграммы пропускаются: у них нет текста, и токенов они не несут — данные
    диаграммы живут в отдельной части пакета и подставляются не плейсхолдером.
    """
    import re as _re
    out: list[str] = []
    for sh in slide.shapes:
        if _shape_kind(sh) == KIND_CHART:
            continue
        token = _re.sub(r"^card\d+_", "cardN_", sh.name)
        token = _re.sub(r"^kpi\d+_", "kpiN_", token)
        token = _re.sub(r"^hc\d+_", "hcN_", token)
        # шейпы, оставшиеся с именами деки («Shape 5», «Text 6»), в токены не
        # попадают: их в активе быть не должно, и это ловит обратная проверка
        if " " in token:
            continue
        out.append("{{%s}}" % token)
    return sorted(set(out))


def build(src: Path, out: Path, manifest_path: Path,
          originals_path: Path | None) -> None:
    global _DECK_YEARS
    raw = src.read_bytes()
    sha = hashlib.sha256(raw).hexdigest()
    prs = Presentation(str(src))
    n_src = len(prs.slides)
    if n_src < max(n for n, _, _ in PROTOS):
        raise AssertionError("в деке %d слайдов — прототипов 1–27 нет" % n_src)

    # год отчётного периода деки — снимается с листа 1 ДО обезличивания:
    # в текстах подписей он станет прорезью «{year}», и после этого года в
    # активе уже не будет
    _DECK_YEARS = _deck_years(prs.slides[0])

    # Прототипы ищутся по ИМЕНИ, а не по позиции в PROTOS: порядок в PROTOS —
    # это порядок слайдов ВЫГРУЗКИ (дека: 1,2,3 → 7,8 → 23…27), а цвета УФ и
    # заливки строк живут на листах численности.
    by_proto = {name: prs.slides[num - 1] for num, name, _b in PROTOS}

    # цвета УФ и заливки строк снимаются ДО обезличивания (потом их уже нет)
    sign = _sign_colors(by_proto["hc_summary"], by_proto["hc_country_by"],
                        by_proto["hc_country_kz"],
                        half_slides=(by_proto["b1_h1"], by_proto["b1_h2"]))
    pct_bands = _pct_bands([by_proto[n] for n in
                            ("hc_summary", "hc_country_by", "hc_country_ru",
                             "hc_country_kz", "hc_country_uz")])
    row_colors = _row_colors(next(sh for sh in by_proto["hc_summary"].shapes
                                  if getattr(sh, "has_table", False) and sh.has_table).table)

    # Цвета листов 1–3 и 7–8: свой набор (подложка карточки, разделители, фон
    # блоков), не совпадающий с p17 — снимаются с их прототипов отдельно.
    b1_colors = _b1_colors(by_proto)
    dept_colors = _dept_colors(by_proto)
    nb_colors = _nb_colors(by_proto, prs)

    # разряды в таблицах — свойство генератора деки, а не прототипа: снимаем по
    # всем прототипам сразу (см. _cell_group)
    cell_group = _cell_group([prs.slides[num - 1] for num, _n, _b in PROTOS])

    manifest_protos: dict = {}
    bar_protos: dict = {}
    originals: dict = {"source_deck": src.name, "source_sha256": sha, "slides": {}}
    shape_count = 0

    for order, (num, proto_name, badge) in enumerate(PROTOS):
        slide = prs.slides[num - 1]
        slide._element.find(qn("p:cSld")).set("name", proto_name)
        shapes = _resolve(slide, proto_name, badge)
        tables = _annotate_tables(slide, proto_name)

        # карта оригиналов: тексты ДО обезличивания
        slide_dump: dict = {"shapes": {}, "extras": []}
        annotated = {id(sh._element) for sh in shapes.values()}
        for semantic, sh in shapes.items():
            paras = _paragraph_runs_xml(sh)
            if len(paras) > 1:
                entry = {"paragraphs": [dict(p, idx=pi) for pi, p in enumerate(paras)]}
            else:
                entry = {"runs": paras[0]["runs"] if paras else []}
            slide_dump["shapes"][semantic] = entry
        # полнота карты: любой нестатичный текст вне списка аннотации — в extras
        for sh in slide.shapes:
            if id(sh._element) in annotated:
                continue
            txt = _text_of(sh)
            if not txt.strip():
                continue
            cnv = _cnvpr(sh._element)
            slide_dump["extras"].append({
                "shape_id": cnv.get("id") if cnv is not None else None,
                "name": sh.name,
                "text": txt,
            })

        table_dump: dict = {}
        # типографика прототипа — ДО обезличивания таблиц: после него в ячейках
        # стоят плейсхолдеры, и образцов разрядов/знаков процентов уже нет
        deck_format = _deck_format(shapes, tables, cell_group, proto_name)
        # подписи с прорезями — тоже ДО обезличивания: шаблон снимается с
        # живого текста деки, в котором ещё есть годы и числа
        consts: dict = {}
        labels = _label_templates(shapes, consts, proto_name)
        # слоты диаграмм — ДО обезличивания данных: цвета dPt, имя серии и
        # формат подписей снимаются с деки (после replace_data они те же, но
        # категории и значения уже заглушки)
        chart_slots = {sh.name: _chart_meta(sh) for sh in slide.shapes
                       if _shape_kind(sh) == KIND_CHART}
        slide_dump["charts"] = chart_slots
        # карта «место карточки → страна» (BY/RU/KZ/UZ по местам) — тоже ДО
        # обезличивания: в активе на месте кода уже токен {{cardN_cc_code}}
        card_slots = _card_slots(shapes)
        header_labels: dict = {}
        table_meta = _obfuscate_tables(tables, proto_name, table_dump,
                                       headers=header_labels)
        slide_dump["tables"] = table_dump
        originals["slides"][proto_name] = slide_dump

        consts.update(_obfuscate_shapes(shapes, proto_name))
        _normalize_cell_colors(tables, sign)
        # данные диаграмм → заглушка: актив обезличен, оформление цело
        for sh in slide.shapes:
            if _shape_kind(sh) == KIND_CHART:
                _scrub_chart(sh)

        meta: dict = {
            "slide_sp_order": order,
            "source_slide": num,
            "tables": table_meta,
            "format": deck_format,
        }
        # слоты диаграмм: тип, число серий/точек и цвета dPt прототипа. Данные
        # в них НЕ хранятся — только оформление; биндер подставляет значения
        # через `fot_deck_export.charts.set_data`
        if chart_slots:
            meta["charts"] = chart_slots
        # число ячеек сетки карточек: биндер по нему удаляет лишние или
        # открывает вторую страницу (см. DEPT7_SHAPES / DEPT8_SHAPES)
        cards = _count_cards(slide)
        if cards is not None:
            meta["card_count"] = cards
        # карта «место карточки → страна»: дека размещает страны по фиксированным
        # местам, и это оформление листа, а не данные среза
        if card_slots:
            meta["card_slots"] = card_slots
        if consts:
            meta["consts"] = consts
        # подписи-шаблоны прототипа: текст деки с прорезями «{year}/{prev}» и
        # «{vN}» — биндер берёт формулировку отсюда, а не из своего кода
        if labels:
            meta["labels"] = labels
        # подписи колонок шапки таблиц — тоже формулировки деки с прорезями
        if header_labels:
            meta["header_labels"] = header_labels
        # имена токенов прототипа: их сверяет движок с текстом актива (B5).
        # Табличные ячейки тоже токены ({{h_c0}}, {{c0}}): они не шейпы, и
        # обход имён слайда их не видит, поэтому добавляются отдельно — иначе
        # движок считал бы их «неописанными» и падал на каждой выгрузке.
        meta["placeholders"] = sorted(set(_placeholders_of(slide))
                                      | set(_table_placeholders(table_meta)))
        manifest_protos[proto_name] = meta
        if "total_bar" in shapes:
            bar_protos[proto_name] = shapes["total_bar"]
        shape_count += len(shapes)

    # цвета ранов полосы «Итого» — позиционные, снимаются после обхода слайдов
    bar_colors = _total_bar_colors(bar_protos) if bar_protos else None

    dropped = _drop_other_slides(prs, {n for n, _, _ in PROTOS})
    _scrub_properties(prs)
    first_num = _set_first_slide_num(prs, 1)

    out.parent.mkdir(parents=True, exist_ok=True)
    prs.save(str(out))
    size = out.stat().st_size
    if size >= 3 * 1024 * 1024:
        # прежний порог 1 МБ: актив вырос вместе с партией A (10 прототипов,
        # 16 карточек листа 8 — это ~330 шейпов). Запас оставлен широким, но не
        # безграничным: лишний мусор деки (медиа, диаграммы) ловится тем же.
        raise AssertionError("актив больше 3 МБ (%d КБ) — в пакет попал лишний "
                             "мусор деки" % (size // 1024,))

    manifest = {
        "source_deck": src.name,
        "source_sha256": sha,
        # отпечаток САМОГО файла актива: движок сверяет его при загрузке и
        # отказывается собирать выгрузку, если актив пересобрали/подменили,
        # а манифест остался прежним (B7)
        "asset_sha256": hashlib.sha256(out.read_bytes()).hexdigest(),
        "created_utc": datetime.now(timezone.utc).strftime("%Y-%m-%dT%H:%M:%SZ"),
        "deck_slides": n_src,
        "asset_slides": len(prs.slides),
        "dropped_slides": dropped,
        "first_slide_num": 1 if first_num else None,
        "prototypes": manifest_protos,
        "sign_colors": sign,
        "pct_bands": pct_bands,
        "row_colors": row_colors,
        "b1_colors": b1_colors,
        "dept_colors": dept_colors,
        "nb_colors": nb_colors,
        "total_bar_colors": bar_colors,
        # сводный список токенов по всем прототипам: движок сверяет его с
        # текстом актива (B5), неизвестный токен = незакрытый биндер.
        # Полоса итога p17 держит ИСТОРИЧЕСКИЕ имена частей (`total_label`,
        # `total_part1…4`) вместо `total_bar_r*` — их читает биндер, поэтому
        # объявляем и их: иначе сверка считает шейп неописанным
        "placeholders": sorted({tok for m in manifest_protos.values()
                                for tok in m.get("placeholders") or []}
                               | {"{{total_label}}", "{{total_part*}}"}),
    }
    manifest_path.parent.mkdir(parents=True, exist_ok=True)
    manifest_path.write_text(json.dumps(manifest, ensure_ascii=False, indent=2),
                             encoding="utf-8")
    if originals_path is not None:
        originals_path.parent.mkdir(parents=True, exist_ok=True)
        originals_path.write_text(json.dumps(originals, ensure_ascii=False, indent=2),
                                  encoding="utf-8")

    tables_total = sum(len(m["tables"]) for m in manifest_protos.values())
    print("актив: %s (%d КБ)" % (out, size // 1024))
    print("манифест: %s" % manifest_path)
    print("слайдов в активе: %d (удалено %d), прототипов %d, таблиц %d"
          % (len(prs.slides), dropped, len(manifest_protos), tables_total))
    print("шейпов аннотировано: текстовых %d, табличных %d" % (shape_count, tables_total))
    print("firstSlideNum: %s" % ("1" if first_num else "не найден presProps.xml"))
    if originals_path is not None:
        print("оригиналы (до обезличивания): %s" % originals_path)


def _assert_outside_repo(path: Path) -> None:
    """Дамп оригиналов не должен попасть в репозиторий (A3).

    В файле лежат ИСХОДНЫЕ тексты деки заказчика (названия подразделений,
    числа) — это не обезличенный актив, и в репо ему нельзя ни в каком виде:
    путь проверяется и абсолютный, и относительный (от CWD).
    """
    resolved = path.expanduser().resolve()
    try:
        resolved.relative_to(REPO_ROOT)
    except ValueError:
        return
    raise SystemExit(
        "фикстуры с данными деки не кладутся в репо: %s внутри %s — укажите "
        "путь вне репозитория (по умолчанию /tmp/fot_deck_originals.json)"
        % (resolved, REPO_ROOT))


def main(argv: list[str] | None = None) -> int:
    here = Path(__file__).resolve().parent
    asset_dir = here.parent / "app" / "assets" / "fot_deck"
    ap = argparse.ArgumentParser(description="Сборка актива деки-шаблона P17")
    ap.add_argument("--src", required=True, help="исходная дека заказчика (pptx)")
    ap.add_argument("--out", default=str(asset_dir / "fot_deck_template.pptx"),
                    help="куда положить актив (абсолютный путь разрешён)")
    ap.add_argument("--manifest", default="",
                    help="путь manifest.json (по умолчанию — рядом с активом)")
    ap.add_argument("--dump-originals", default=None,
                    help="JSON с исходными текстами до обезличивания "
                         "(по умолчанию /tmp/fot_deck_originals.json; внутрь "
                         "репозитория писать запрещено)")
    args = ap.parse_args(argv)

    src = Path(os.path.expanduser(args.src)).resolve()
    if not src.is_file():
        print("нет файла деки: %s" % src, file=sys.stderr)
        return 2
    out = Path(os.path.expanduser(args.out)).resolve()
    manifest_path = (Path(os.path.expanduser(args.manifest)).resolve() if args.manifest
                     else out.parent / "manifest.json")
    originals = (Path("/tmp/fot_deck_originals.json") if args.dump_originals is None
                 else Path(os.path.expanduser(args.dump_originals)).resolve())
    _assert_outside_repo(originals)
    build(src, out, manifest_path, originals)
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
