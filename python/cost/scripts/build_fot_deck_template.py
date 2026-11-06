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
#:  бейджа). Порядок = порядок слайдов в активе и в выгрузке.
PROTOS = (
    (23, "hc_summary", "чел."),
    (24, "hc_country_by", "BY"),
    (25, "hc_country_ru", "RU"),
    (26, "hc_country_kz", "KZ"),
    (27, "hc_country_uz", "UZ"),
)

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
#: Заливка первой ячейки строки → стиль строки (в деке ровно эти четыре).
FILL_STYLES = {
    "9B5A70": "header",
    "F8EDF0": "body_tinted",
    "FFFFFF": "body_plain",
    "6B3347": "total",
}

#: Таблицы прототипов в порядке документа.
TABLE_NAMES = {
    "hc_summary": ("tbl_countries",),
    "hc_country_kz": ("tbl_dept_a",),
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
    if prst is not None and prst.get("prst") == "roundRect":
        return KIND_ROUNDRECT
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


def _resolve(slide, proto_name: str, badge: str) -> dict:
    """Раскладывает шейпы прототипа по семантическим именам.

    Селектор — (числовой суффикс имени, тип шейпа, ожидаемый текст); каждый
    шейп занимается один раз, несовпадение любого пункта карты — AssertionError
    с диагностикой (шейп не найден / текст не тот).
    """
    shapes = list(slide.shapes)
    claimed: dict = {}

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
        return len(_runs_of(sh)) == 1 and got.strip() == badge

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
    for idx, (spec_id, name) in enumerate(KPI_IDS):
        take(spec_id, name + "_label", KIND_TEXT, is_label(KPI_LABELS[idx]))
        take(str(int(spec_id) + 1), name + "_value", KIND_TEXT, is_kpi_value)

    if proto_name == "hc_summary":
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

    return {semantic: sh for semantic, sh in claimed.values()}


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


# ─── Снятие цветов УФ с прототипов ───────────────────────────────────────────

def _sign_colors(summary_slide, country_slide, kz_slide) -> dict:
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


def _deck_format(shapes: dict, tables: dict, cell_group: bool) -> dict:
    """Флаги типографики прототипа, снятые с самой деки.

    Дека писалась не одним генератором: в своде и в BY/RU значения KPI идут без
    разрядов («7307,2»), у UZ — с NBSP («2 052,5»); «% укомпл.» в таблице BY
    целое («99%»), в остальных — с одним знаком. Всё это снимается здесь, чтобы
    биндеры не гадали и не держали знаний о конкретной деке.
    """
    # KPI: группировка разрядов по фактическому тексту пяти значений
    kpi_group = True
    kpi_samples = [_num_shape(shapes["kpi%d_value" % k]) for k in range(1, 6)]
    with_nbsp = [s for s in kpi_samples if NBSP in s]
    if with_nbsp and len(with_nbsp) == len([s for s in kpi_samples if s]):
        kpi_group = True
    elif kpi_samples and not with_nbsp:
        kpi_group = False

    # «% укомпл.»: знаков после запятой в теле таблицы И в полосе итога
    pct_digits, total_pct_digits = 1, 1
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

    # примечание: в KZ дека оставила хвостовой пробел — часть её типографики
    note_runs = _runs_of(shapes["note"])
    note_text = (note_runs[0].find(qn("a:t")).text or "") if note_runs else ""
    return {"kpi_group": kpi_group, "cell_group": cell_group,
            "pct_digits": pct_digits, "total_pct_digits": total_pct_digits,
            "note_tail_space": note_text.endswith(" ")}


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

def _obfuscate_shapes(shapes: dict, proto_name: str) -> dict:
    """Тексты адресуемых шейпов → плейсхолдеры. Возвращает текстовые константы."""
    # title: run0 «ГК «МАРК ФОРМЭЛЬ»  » — статичный бренд, run1 — подзаголовок
    _set_text(shapes["title"].text_frame.paragraphs[0].runs[1], "{{subtitle}}")

    for k in range(1, 6):
        label_p = shapes["kpi%d_label" % k].text_frame.paragraphs[0]
        _set_text(label_p.runs[0], "{{kpi%d_label}}" % k)
        value_p = shapes["kpi%d_value" % k].text_frame.paragraphs[0]
        _set_text(value_p.runs[0], "{{kpi%d_value}}" % k)
        # runs[1] («  чел.») — статичный хвост, не трогаем

    _set_text(shapes["badge"].text_frame.paragraphs[0].runs[0], "{{badge}}")
    _set_text(shapes["note"].text_frame.paragraphs[0].runs[0], "{{note}}")
    _set_text(shapes["page"].text_frame.paragraphs[0].runs[0], "{{page}}")

    consts: dict = {}
    if proto_name == "hc_summary":
        tf = shapes["obs"].text_frame
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
    else:
        # у KZ полосы итога нет — итог живёт строкой в самой таблице
        if "total_bar" in shapes:
            bar = shapes["total_bar"].text_frame.paragraphs[0]
            runs = bar.runs
            if len(runs) != 5:
                raise AssertionError("total_bar: ранов %d, ожидалось 5" % len(runs))
            for i, run in enumerate(runs):
                token = "total_label" if i == 0 else "total_part%d" % i
                _set_text(run, "{{%s}}" % token)
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


def _obfuscate_tables(tables: dict, proto_name: str, dump: dict) -> dict:
    """Сжатие таблиц до строк-прототипов + плейсхолдеры в ячейках.

    Возвращает {имя таблицы: {row_protos: [...], capacity: N, paragraphs: [...]}}.
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
        for style in ("body_tinted", "body_plain"):
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
                "capacity": sum(1 for s in styles if s in ("body_tinted", "body_plain")),
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
            for ci, tc in enumerate(trs[i].findall(qn("a:tc"))):
                _set_cell_placeholder(tc, token_fmt % ci)

        for i in reversed(range(len(trs))):
            if i not in keep:
                tbl._tbl.remove(trs[i])

        out[tbl_name] = {
            # capacity — ТОЛЬКО строки тела (tinted + plain): шапка и итог в
            # неё не входят. Так пагинация не съезжает: у KZ в деке было
            # header + total + 8 body = 10 строк, и 8 — это именно body
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

def build(src: Path, out: Path, manifest_path: Path,
          originals_path: Path | None) -> None:
    raw = src.read_bytes()
    sha = hashlib.sha256(raw).hexdigest()
    prs = Presentation(str(src))
    n_src = len(prs.slides)
    if n_src < max(n for n, _, _ in PROTOS):
        raise AssertionError("в деке %d слайдов — прототипов 23–27 нет" % n_src)

    # цвета УФ и заливки строк снимаются ДО обезличивания (потом их уже нет)
    sign = _sign_colors(prs.slides[PROTOS[0][0] - 1], prs.slides[PROTOS[1][0] - 1],
                        prs.slides[PROTOS[3][0] - 1])
    pct_bands = _pct_bands([prs.slides[num - 1] for num, _n, _b in PROTOS])
    row_colors = _row_colors(next(sh for sh in prs.slides[PROTOS[0][0] - 1].shapes
                                  if getattr(sh, "has_table", False) and sh.has_table).table)

    # разряды в таблицах — свойство генератора деки, а не прототипа: снимаем по
    # всем пяти слайдам сразу (см. _cell_group)
    cell_group = _cell_group([prs.slides[num - 1] for num, _name, _b in PROTOS])

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
        deck_format = _deck_format(shapes, tables, cell_group)
        table_meta = _obfuscate_tables(tables, proto_name, table_dump)
        slide_dump["tables"] = table_dump
        originals["slides"][proto_name] = slide_dump

        consts = _obfuscate_shapes(shapes, proto_name)
        _normalize_cell_colors(tables, sign)

        manifest_protos[proto_name] = {
            "slide_sp_order": order,
            "source_slide": num,
            "tables": table_meta,
            "format": deck_format,
        }
        if consts:
            manifest_protos[proto_name]["consts"] = consts
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
    if size >= 1024 * 1024:
        raise AssertionError("актив больше 1 МБ (%d КБ) — в пакет попал лишний "
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
        "total_bar_colors": bar_colors,
        "placeholders": {
            "title": "{{subtitle}}",
            "kpi": ["{{kpiN_label}}", "{{kpiN_value}}"],
            "badge": "{{badge}}", "note": "{{note}}", "page": "{{page}}",
            "obs": ["{{obs_header}}", "{{obs_bullet}}", "{{obs_subject}}", "{{obs_text}}"],
            "total_bar": ["{{total_label}}", "{{total_part1}}", "{{total_part2}}",
                          "{{total_part3}}", "{{total_part4}}"],
            "table_header": "{{h_cN}}", "table_body": "{{cN}}",
        },
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
