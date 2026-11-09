"""Работа с XML актива деки: клонирование слайдов, замена ранов, таблицы.

Актив `app/assets/fot_deck/fot_deck_template.pptx` — обезличенная обрезка деки
заказчика. Экспорт не рисует слайды заново, а клонирует слайд-прототип
целиком (deepcopy spTree) и подставляет живые значения в плейсхолдеры — так
вёрстка, шрифты, заливки и геометрия остаются 1:1 с декой.

Инварианты, на которых держится подход:
  * прототипы чистые: ни в одном дереве нет r:id / r:embed (картинок, диаграмм,
    связей нет) — проверяется при клонировании, иначе клон унаследовал бы
    rels исходного слайда и развалился;
  * текст меняется ТОЛЬКО по ранам (rPr сохраняется): прямое
    text_frame.text = … стирает форматы, а в деке весь вид живёт в rPr;
  * цвета УФ не хардкодятся: их источник — manifest.json, снятый при сборке
    актива с самих прототипов.
"""

from __future__ import annotations

import json
import re
from copy import deepcopy
from pathlib import Path

from lxml import etree

try:
    from pptx import Presentation  # noqa: F401 - проверка наличия python-pptx
except ImportError as exc:  # python-pptx — опциональная зависимость образа
    Presentation = None
    PPTX_IMPORT_ERROR = str(exc)
else:
    PPTX_IMPORT_ERROR = None
    from pptx.opc.package import XmlPart

ASSET_DIR = Path(__file__).resolve().parent.parent / "assets" / "fot_deck"
TEMPLATE_PATH = ASSET_DIR / "fot_deck_template.pptx"
MANIFEST_PATH = ASSET_DIR / "manifest.json"

_P_NS = "{http://schemas.openxmlformats.org/presentationml/2006/main}"
_A_NS = "{http://schemas.openxmlformats.org/drawingml/2006/main}"
_R_NS = "{http://schemas.openxmlformats.org/officeDocument/2006/relationships}"
_C_NS = "{http://schemas.openxmlformats.org/drawingml/2006/chart}"

#: Префиксы неймспейсов DrawingML/PresentationML для прямых выборок из XML
#: (в биндерах и тестах: `el.find(shapes.A + "tc")`).
A = _A_NS
P = _P_NS

#: Неймспейсы для XPath-выборок (find_shape: имя шейпа = cNvPr/@name).
#: Выражение компилируется один раз: `tree.xpath()` python-pptx переменных не
#: принимает, а имя шейпа приходит данными — подставлять его в строку запроса
#: нельзя (апостроф в имени сломал бы выражение).
_XPATH_NS = {"p": _P_NS[1:-1], "a": _A_NS[1:-1]}
_SHAPE_BY_NAME = etree.XPath(
    "./*/*/p:cNvPr[@name = $name]", namespaces=_XPATH_NS)

#: Префикс имени выходного слайда: «fot:p17:hc_country_by:2» — по нему в
#: PowerPoint и в отладке видно, из какого прототипа и страницы слайд собран.
SLIDE_NAME_PREFIX = "fot:"


def load_manifest(path: Path | None = None) -> dict:
    """manifest.json актива (кеша нет: файл читается один раз на запрос)."""
    return json.loads((path or MANIFEST_PATH).read_text(encoding="utf-8"))


# ─── Клонирование ────────────────────────────────────────────────────────────

#: Разрешённые типы связей прототипа и способ их переноса в клон:
#:   image — часть переиспользуется (картинка неизменяема, две ссылки на одну
#:           часть — норма OPC), клону заводится своя связь с тем же rId-слотом;
#:   chart — часть КОПИРУЕТСЯ целиком под каждого потребителя: `replace_data`
#:           мутирует XML части, и общая часть отдала бы данные прошлого слайда.
#: Всё остальное — ошибка сборки актива: молча унаследованная связь дала бы
#: слайд, ссылающийся на чужую часть.
REL_IMAGE = "http://schemas.openxmlformats.org/officeDocument/2006/relationships/image"
REL_CHART = "http://schemas.openxmlformats.org/officeDocument/2006/relationships/chart"


def _rel_refs(spTree) -> list[tuple[str, str]]:
    """Ссылки на части пакета в дереве шейпов: [(rId, имя атрибута), …]."""
    out: list[tuple[str, str]] = []
    for el in spTree.iter():
        for k, v in el.attrib.items():
            if k.startswith(_R_NS):
                out.append((v, k))
    return out


def _next_partname(package, partname) -> str:
    """Свободное имя части того же вида: «/ppt/charts/chart7.xml» → «chart8»."""
    m = re.match(r"^(.*?)(\d*)(\.[A-Za-z0-9]+)$", str(partname))
    if not m:
        raise RuntimeError("не разобрать имя части %r — копирование невозможно"
                           % str(partname))
    return package.next_partname(m.group(1) + "%d" + m.group(3))


def _copy_part(part, package):
    """Глубокая копия части пакета со ВСЕМИ её связями (диаграмма + её xlsx).

    Копируется и XML, и вложенный xlsx (`c:externalData`): PowerPoint считает
    данные диаграммы из встроенной книги, и без своей копии книги вторая
    диаграмма показывала бы данные первой (или падала на «нет данных»).
    """
    partname = _next_partname(package, part.partname)
    if isinstance(part, XmlPart):
        new = type(part)(partname, part.content_type, package,
                         element=deepcopy(part._element))
    else:
        new = type(part).load(partname, part.content_type, package, part.blob)
    rid_map: dict[str, str] = {}
    for rId, rel in list(part.rels.items()):
        if rel.is_external:
            rid_map[rId] = new.relate_to(rel.target_ref, rel.reltype, is_external=True)
        else:
            rid_map[rId] = new.relate_to(_copy_part(rel.target_part, package),
                                         rel.reltype)
    if isinstance(new, XmlPart) and rid_map:
        _rewrite_rel_ids(new._element, rid_map)
    return new


def _rewrite_rel_ids(el, rid_map: dict[str, str]) -> None:
    """Переписывает r:id / r:embed в поддереве по карте соответствия."""
    for node in el.iter():
        for k, v in list(node.attrib.items()):
            if k.startswith(_R_NS) and v in rid_map:
                node.set(k, rid_map[v])


def clone_slide(prs, proto_slide, name: str):
    """Добавляет в конец презентации клон прототипа; возвращает новый слайд.

    Копируется cSld/spTree, а ссылки на части пакета (r:id / r:embed) получают
    у клона СВОИ связи: картинки переиспользуются, диаграммы копируются
    (см. REL_IMAGE / REL_CHART). Без этого клон унаследовал бы rId прототипа и
    показывал чужую диаграмму — или падал при открытии в PowerPoint.
    """
    proto_cSld = proto_slide._element.find(_P_NS + "cSld")
    proto_tree = proto_cSld.find(_P_NS + "spTree")

    new = prs.slides.add_slide(proto_slide.slide_layout)
    new_cSld = new._element.find(_P_NS + "cSld")
    # карта «rId прототипа → rId клона»: связи заводятся ДО вставки дерева,
    # чтобы копии шейпов сразу содержали правильные идентификаторы
    rid_map: dict[str, str] = {}
    for rId, _attr in _rel_refs(proto_tree):
        if rId in rid_map:
            continue
        rel = proto_slide.part.rels[rId]
        if rel.reltype == REL_IMAGE:
            # картинка неизменяема: клон ссылается на ту же часть пакета
            rid_map[rId] = new.part.relate_to(rel.target_part, rel.reltype)
        elif rel.reltype == REL_CHART:
            rid_map[rId] = new.part.relate_to(
                _copy_part(rel.target_part, prs.part.package), rel.reltype)
        else:
            raise RuntimeError(
                "прототип %s: связь %r типа %s не поддержана клонированием "
                "(разрешены картинка и диаграмма) — расширьте clone_slide"
                % (proto_cSld.get("name"), rId, rel.reltype))

    # add_slide кладёт пустой spTree с placeholders layout'а — наполняем его
    # детьми прототипа (поэлементно: replace() корневого элемента оставлял
    # в Slide.shapes старый узел)
    new_tree = new_cSld.find(_P_NS + "spTree")
    for child in list(new_tree):
        new_tree.remove(child)
    for child in proto_tree:
        new_tree.append(deepcopy(child))
    if rid_map:
        _rewrite_rel_ids(new_tree, rid_map)
    # фон слайда (p:bg) — часть cSld, но не spTree: переносим отдельно
    proto_bg = proto_cSld.find(_P_NS + "bg")
    if proto_bg is not None:
        for old_bg in new_cSld.findall(_P_NS + "bg"):
            new_cSld.remove(old_bg)
        new_cSld.insert(0, deepcopy(proto_bg))
    new_cSld.set("name", name)
    return new


def delete_slide(prs, slide) -> None:
    """Удаляет слайд из презентации вместе со связью (rels)."""
    sldIdLst = prs.slides._sldIdLst
    for sldId in list(sldIdLst):
        if prs.part.related_part(sldId.get(_R_NS + "id")) is slide.part:
            prs.part.drop_rel(sldId.get(_R_NS + "id"))
            sldIdLst.remove(sldId)
            return
    raise RuntimeError("слайд не найден в sldIdLst — удаление не удалось")


def delete_shape(shape) -> None:
    """Удаляет шейп из дерева слайда (по XML-элементу)."""
    el = shape._element
    el.getparent().remove(el)


def find_shape(slide, name: str):
    """Шейп слайда по имени (семантические имена проставляет сборка актива).

    Выборка XPath по XML, а не обход `slide.shapes`: тот на КАЖДОМ шейпе
    строит объект python-pptx и читает имя через отдельный xpath, поэтому на
    полной выгрузке (тысячи поисков × десятки шейпов слайда) один этот вызов
    съедал больше времени, чем вся остальная сборка. Здесь один запрос в C по
    схеме «шейп → его nv*Pr → cNvPr/@name», объект python-pptx создаётся
    только для найденного элемента.

    Глубина — как у `slide.shapes`: верхний уровень spTree (у группы имя своё,
    до имён её детей биндеры не ищут).
    """
    tree = slide._element.find(_P_NS + "cSld").find(_P_NS + "spTree")
    if tree is None:
        return None
    found = _SHAPE_BY_NAME(tree, name=name)
    if not found:
        return None
    # cNvPr → nv*Pr → сам шейп
    return _wrap_shape(slide, found[0].getparent().getparent())


def require_shape(slide, name: str):
    sh = find_shape(slide, name)
    if sh is None:
        raise RuntimeError("в слайде %r нет шейпа %r"
                           % (slide._element.find(_P_NS + "cSld").get("name"), name))
    return sh


# ─── Текст по ранам ──────────────────────────────────────────────────────────

def set_text(shape, pairs, para_idx: int = 0) -> None:
    """Заменяет тексты конкретных ранов: [(индекс_рана, текст), …].

    Параграф по умолчанию первый; rPr каждого рана сохраняется как есть.
    Прямое `text_frame.text = …` запрещено — оно сносит форматы ранов, на
    которых держится весь вид деки.
    """
    para = shape.text_frame.paragraphs[para_idx]
    runs = para.runs
    for idx, text in pairs:
        if idx >= len(runs):
            raise RuntimeError("шейп %r: нет рана %d (ранов %d)"
                               % (shape.name, idx, len(runs)))
        runs[idx].text = text


def set_run(shape, idx: int, text: str) -> None:
    """Один ран одним значением (обёртка над set_text)."""
    set_text(shape, [(idx, text)])


def set_text_raw(shape, pairs, para_idx: int = 0) -> None:
    """Заменяет тексты ранов напрямую в a:t — без экранирования символов.

    `run.text = …` из python-pptx экранирует управляющие символы
    («\\n» → «_x000A_»), а дека хранит перевод строки в тексте рана как есть:
    на нём держится разрыв строк блока наблюдений (каждый абзац, кроме
    последнего, закрыт «\\n»). Для таких текстов пишем узел a:t напрямую —
    иначе вместо разрыва в выгрузке окажется мусорная последовательность.
    """
    runs = shape.text_frame.paragraphs[para_idx].runs
    for idx, text in pairs:
        if idx >= len(runs):
            raise RuntimeError("шейп %r: нет рана %d (ранов %d)"
                               % (shape.name, idx, len(runs)))
        r = runs[idx]._r
        t = r.find(_A_NS + "t")
        if t is None:
            t = etree.SubElement(r, _A_NS + "t")
        t.text = text


def run_color(shape, para_idx: int = 0, run_idx: int = 0) -> str | None:
    """HEX цвета рана (для сверки с manifest в тестах)."""
    runs = shape.text_frame.paragraphs[para_idx].runs
    if run_idx >= len(runs):
        return None
    rPr = runs[run_idx]._r.find(_A_NS + "rPr")
    if rPr is None:
        return None
    clr = rPr.find(_A_NS + "solidFill/" + _A_NS + "srgbClr")
    return clr.get("val") if clr is not None else None


def paint_run(shape, hex_color: str, para_idx: int = 0, run_idx: int = 0) -> None:
    """Красит ран: цвет УФ берётся из manifest, не из литерала."""
    runs = shape.text_frame.paragraphs[para_idx].runs
    if run_idx >= len(runs):
        raise RuntimeError("шейп %r: нет рана %d для покраски" % (shape.name, run_idx))
    rPr = runs[run_idx]._r.get_or_add_rPr()
    for old in rPr.findall(_A_NS + "solidFill"):
        rPr.remove(old)
    fill = etree.Element(_A_NS + "solidFill")
    clr = etree.SubElement(fill, _A_NS + "srgbClr")
    clr.set("val", hex_color)
    rPr.insert(0, fill)


# ─── Таблицы ─────────────────────────────────────────────────────────────────

def _cell_paras(tc) -> list:
    """Абзацы ячейки таблицы (a:tc/a:txBody/a:p)."""
    txBody = tc.find(_A_NS + "txBody")
    return txBody.findall(_A_NS + "p") if txBody is not None else []


def _write_cell(tc, lines) -> None:
    """Пишет текст в ячейку по абзацам-прототипам, сохраняя их rPr.

    lines — список строк: первая идёт в первый абзац, каждая следующая — в
    свой (шапка деки двухстрочная: «Утвержд. числ.» / «страт. 2026»). Если
    строк больше, чем абзацев в прототипе, лишние склеиваются в первый;
    лишние абзацы прототипа остаются пустыми (формат шапки сохраняется).
    """
    ps = _cell_paras(tc)
    if not ps:
        raise RuntimeError("ячейка без абзацев — прототип таблицы испорчен")
    texts = list(lines) or [""]
    for pi, p in enumerate(ps):
        runs = p.findall(_A_NS + "r")
        if not runs:
            raise RuntimeError("абзац ячейки без ранов — прототип таблицы испорчен")
        for r in runs[1:]:
            p.remove(r)
        t = runs[0].find(_A_NS + "t")
        if t is None:
            t = etree.SubElement(runs[0], _A_NS + "t")
        t.text = texts[pi] if pi < len(texts) else ""


def fill_table(graphic_frame, rows_spec, row_protos, sign,
               proto_row_count: int | None = None) -> None:
    """Пересобирает таблицу: строки-прототипы + живые значения + высота фрейма.

    rows_spec — [{'style': 'header'|'body_tinted'|'body_plain'|'total'|'total_top',
                  'cells': [[строка, …], …],
                  'signs': [None|'up'|'down'|'#RRGGBB', …] (по колонкам)}].
    row_protos — {стиль: a:tr} из актива (клонируются deepcopy).
    sign — {'delta_up', 'delta_down', 'delta_up_light', 'delta_down_light'};
    какие цвета брать (светлые — на тёмной полосе итога) решает знак `light`
    в спецификации строки. Элемент `signs` может быть и готовым HEX (например
    «н/д» — приглушённый серый деки): тогда он применяется как есть.
    proto_row_count — сколько строк было в прототипе (len(row_protos)): по нему
    высота фрейма масштабируется пропорционально, как в деке.
    """
    tbl = graphic_frame._element.find(".//" + _A_NS + "tbl")
    if tbl is None:
        raise RuntimeError("graphicFrame %r: внутри нет a:tbl" % graphic_frame.name)
    ext = graphic_frame._element.find(_P_NS + "xfrm/" + _A_NS + "ext")
    if ext is None:
        raise RuntimeError("graphicFrame %r: нет p:xfrm/a:ext — прототип таблицы "
                           "испорчен" % graphic_frame.name)
    # авторская высота прототипа — снимается до замены строк, её и масштабируем
    proto_cy = int(ext.get("cy") or 0)

    trs = tbl.findall(_A_NS + "tr")
    for tr in trs:
        tbl.remove(tr)

    light = {True: {"up": sign["delta_up_light"], "down": sign["delta_down_light"]},
             False: {"up": sign["delta_up"], "down": sign["delta_down"]}}

    for row in rows_spec:
        proto = row_protos[row["style"]]
        tr = deepcopy(proto)
        cells = tr.findall(_A_NS + "tc")
        specs = row["cells"]
        if len(specs) != len(cells):
            raise RuntimeError("таблица %r: колонок %d, значений %d"
                               % (graphic_frame.name, len(cells), len(specs)))
        for tc, lines, sign_key in zip(cells, specs,
                                       row.get("signs") or [None] * len(cells)):
            _write_cell(tc, lines if isinstance(lines, (list, tuple)) else [lines])
            if sign_key is None:
                continue
            if sign_key in ("up", "down"):
                color = light[bool(row.get("light"))][sign_key]
            else:
                # готовый HEX («н/д» и подобные константы оформления деки)
                color = sign_key
            for p in _cell_paras(tc):
                for run in p.findall(_A_NS + "r"):
                    rPr = run.find(_A_NS + "rPr")
                    if rPr is None:
                        continue
                    for old in rPr.findall(_A_NS + "solidFill"):
                        rPr.remove(old)
                    fill = etree.Element(_A_NS + "solidFill")
                    clr = etree.SubElement(fill, _A_NS + "srgbClr")
                    clr.set("val", color)
                    rPr.insert(0, fill)
        tbl.append(tr)

    # Высота graphicFrame. В деке cy пропорционален числу строк (≈214 тыс. EMU
    # на строку у слайда 24), но НЕ равен их сумме (3852672 против 3160170):
    # авторский «воздух» строк в a:tr/@h не заложен. В активе осталось 3–4
    # строки-прототипа при неизменном cy, поэтому высоту восстанавливаем по
    # числу строк деки: cy = (cy_актива / deck_rows) × строк_в_выгрузке.
    proto_rows = proto_row_count or len(rows_spec)
    if proto_cy and proto_rows:
        ext.set("cy", str(int(round(proto_cy * len(rows_spec) / proto_rows))))


def table_protos(graphic_frame, row_proto_names, row_colors=None) -> dict:
    """Снимает копии строк-прототипов таблицы: {стиль: a:tr}.

    Строки сопоставляются по ПОРЯДКУ из манифеста — сборка актива оставила в
    каждой таблице ровно строки-прототипы, и записала их в физическом порядке
    (у KZ «total_top» идёт второй строкой, сразу после шапки). Если передан
    row_colors ({стиль: HEX заливки} из манифеста), для каждой строки сверяется
    заливка первой ячейки: несовпадение = рассинхрон манифеста и актива, и
    лучше упасть, чем молча отдать чужие цвета текста (баг 663620).
    """
    tbl = graphic_frame._element.find(".//" + _A_NS + "tbl")
    trs = tbl.findall(_A_NS + "tr")
    if len(trs) != len(row_proto_names):
        raise RuntimeError("таблица %r: строк %d, прототипов в манифесте %d (%r)"
                           % (graphic_frame.name, len(trs),
                              len(row_proto_names), row_proto_names))
    if row_colors:
        for name, tr in zip(row_proto_names, trs):
            tc = tr.find(_A_NS + "tc")
            tcPr = tc.find(_A_NS + "tcPr") if tc is not None else None
            clr = (tcPr.find(_A_NS + "solidFill/" + _A_NS + "srgbClr")
                   if tcPr is not None else None)
            got = clr.get("val") if clr is not None else None
            want = row_colors.get(name if name != "total_top" else "total")
            if want and got != want:
                raise RuntimeError(
                    "таблица %r: строка-прототип %r имеет заливку %s, манифест "
                    "ждёт %s — манифест рассинхронизирован с активом"
                    % (graphic_frame.name, name, got, want))
    return {name: tr for name, tr in zip(row_proto_names, trs)}



def clone_paragraph(shape, src_idx: int):
    """Клонирует абзац-прототип шейпа в конец его текста; возвращает индекс.

    Нужно для списков наблюдений: в активе оставлен ровно один абзац-буллет с
    тремя ранами (маркер / код страны / текст), остальные строятся из него.
    """
    txBody = shape.text_frame._txBody
    src = shape.text_frame.paragraphs[src_idx]._p
    new = deepcopy(src)
    # a:p — последние дочерние элементы txBody: bodyPr и lstStyle идут раньше
    txBody.append(new)
    return len(shape.text_frame.paragraphs) - 1


def clone_run(shape, para_idx: int, src_idx: int):
    """Клонирует ран-прототип абзаца в его конец; возвращает индекс рана.

    Баннеры листов 7/8 перечисляют отделы парами ранов (имя — жирным, пояснение
    обычным), и число пар задаёт данные. В активе остаётся ОДНА пара-прототип,
    остальные строятся её копиями (rPr сохраняется — оформление деки).
    """
    para = shape.text_frame.paragraphs[para_idx]
    src = para.runs[src_idx]._r
    new = deepcopy(src)
    # клон добавляется в КОНЕЦ абзаца, а не сразу за источником: биндер
    # клонирует пару по ранам 0 и 1 подряд, и при вставке «за источником»
    # второй клон копировал бы уже ПЕРВЫЙ клон (индексы съезжают), а порядок
    # ранов становился бы «имя, имя, пояснение, пояснение». В деке это
    # выглядело как один отдел, повторённый восемь раз
    runs = para.runs
    runs[-1]._r.addnext(new)
    return len(para.runs) - 1


# ─── Клонирование блоков шейпов и сдвиг ─────────────────────────────────────

def _next_shape_id(slide) -> int:
    """Свободный cNvPr/@id слайда (PowerPoint требует уникальности в слайде)."""
    top = 0
    for el in slide._element.iter(_P_NS + "cNvPr"):
        try:
            top = max(top, int(el.get("id") or 0))
        except ValueError:
            continue
    return top + 1


def _xfrm_of(shape):
    """p:spPr/a:xfrm шейпа, а для graphicFrame — p:xfrm (или None)."""
    el = shape._element
    if el.tag == _P_NS + "graphicFrame":
        return el.find(_P_NS + "xfrm")
    return el.find(_P_NS + "spPr/" + _A_NS + "xfrm")


def shape_offset(shape) -> tuple[int, int]:
    """(x, y) шейпа в EMU; нет трансформации — нули."""
    xfrm = _xfrm_of(shape)
    if xfrm is None:
        return 0, 0
    off = xfrm.find(_A_NS + "off")
    if off is None:
        return 0, 0
    return int(off.get("x") or 0), int(off.get("y") or 0)


def move_shape(shape, dy: int = 0, dx: int = 0) -> None:
    """Сдвигает шейп на (dx, dy) EMU, сохраняя остальную геометрию."""
    if not dx and not dy:
        return
    xfrm = _xfrm_of(shape)
    if xfrm is None:
        raise RuntimeError("шейп %r: нет xfrm — сдвинуть нельзя" % shape.name)
    off = xfrm.find(_A_NS + "off")
    if off is None:
        raise RuntimeError("шейп %r: нет a:off — сдвинуть нельзя" % shape.name)
    x, y = shape_offset(shape)
    off.set("x", str(x + dx))
    off.set("y", str(y + dy))


def _wrap_shape(slide, element):
    """Объект python-pptx для только что вставленного элемента дерева."""
    from pptx.shapes.autoshape import Shape
    from pptx.shapes.graphfrm import GraphicFrame
    if element.tag == _P_NS + "graphicFrame":
        return GraphicFrame(element, slide.shapes)
    return Shape(element, slide.shapes)


def clone_shapes(slide, protos, prefix_src: str, prefix_dst: str,
                 dy: int = 0, dx: int = 0) -> list:
    """Клонирует блок шейпов карточки под новое имя и сдвигает копии.

    Зачем: у дек часть сеток фиксирована (на листе 28 деки четыре карточки
    стран, на листе 30 — пять карточек статей), а живых строк бывает больше
    (у направления до пяти стран, у статей — до шести групп: в деке шестой
    карточки нет вовсе, потому что у HR «Вложений» не было). Тогда блок
    последнего места клонируется на свободное место сетки: вёрстка карточки
    остаётся декиной (её же XML), а не собирается кодом.

    Имя клона — `prefix_dst` + роль источника (`card1_p1_label` →
    `card5_p1_label`): биндер пишет по роли и не знает, что место в сетке
    новое. Идентификаторы cNvPr делаются уникальными.
    """
    tree = slide._element.find(_P_NS + "cSld").find(_P_NS + "spTree")
    if tree is None:
        raise RuntimeError("слайд %r: нет spTree — клонировать некуда"
                           % slide._element.find(_P_NS + "cSld").get("name"))
    next_id = _next_shape_id(slide)
    made = []
    for sh in protos:
        if not sh.name.startswith(prefix_src):
            raise RuntimeError("шейп %r не принадлежит блоку %r"
                               % (sh.name, prefix_src))
        role = sh.name[len(prefix_src):]
        el = deepcopy(sh._element)
        cnv = el.find(".//" + _P_NS + "cNvPr")
        if cnv is not None:
            cnv.set("id", str(next_id))
            cnv.set("name", prefix_dst + role)
        next_id += 1
        tree.append(el)
        made.append(_wrap_shape(slide, el))
    for sh in made:
        move_shape(sh, dy=dy, dx=dx)
    return made


# ─── Обводка фигур (круги «п.п.») ────────────────────────────────────────────

def paint_outline(shape, hex_color: str) -> None:
    """Красит ОБВОДКУ фигуры (круг «п.п.» — цвет рамки, не заливки).

    У кругов деки цвет несёт линия (заливка белая), и он кодирует знак
    отклонения: красная рамка — рост доли, зелёная — снижение, серая — ноль или
    «н/д». Толщину и прочие свойства линии оставляем как в прототипе: меняется
    только a:srgbClr, поэтому рамка остаётся той же толщины, что у деки.
    """
    spPr = shape._element.find(_P_NS + "spPr")
    if spPr is None:
        raise RuntimeError("шейп %r: нет p:spPr для обводки" % shape.name)
    ln = spPr.find(_A_NS + "ln")
    if ln is None:
        raise RuntimeError("шейп %r: нет a:ln — это не обведённая фигура" % shape.name)
    fill = ln.find(_A_NS + "solidFill")
    if fill is None:
        raise RuntimeError("шейп %r: обводка без a:solidFill" % shape.name)
    for old in fill.findall(_A_NS + "srgbClr"):
        fill.remove(old)
    clr = etree.SubElement(fill, _A_NS + "srgbClr")
    clr.set("val", hex_color)


__all__ = [
    "A", "ASSET_DIR", "MANIFEST_PATH", "P", "SLIDE_NAME_PREFIX", "TEMPLATE_PATH",
    "clone_paragraph", "clone_run", "clone_shapes", "clone_slide",
    "delete_shape", "delete_slide", "fill_table", "find_shape", "load_manifest",
    "move_shape", "paint_outline", "paint_run", "require_shape", "run_color",
    "set_run", "set_text", "set_text_raw", "shape_offset", "table_protos",
]
