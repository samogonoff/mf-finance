"""Диаграммы актива: чтение прототипа и подстановка живых данных.

Слайды 4–6 деки несут по три chart-part (донат «Ожидание 2026» и мини-бары
«доля ФОТ в выручке» на карточку). Диаграмма — не шейп: её данные живут в
отдельной части пакета (`ppt/charts/chartN.xml`), поэтому плейсхолдеры в
тексте слайда её не покрывают, и подстановка идёт через `replace_data`.

Грабли `replace_data` (проверено на python-pptx 1.0.2, см. probe_bar.py):
  * данные и категории он переписывает, а оформление серии (`c:spPr`), цвета
    точек (`c:dPt`) и подписи (`c:dLbls`) оставляет как были — поэтому цвета
    НЕ надо восстанавливать после подстановки, но их надо пересчитать, если
    точек стало больше, чем в прототипе;
  * `c:dLbl` по точкам тоже остаются: у доната их ровно столько, сколько
    сегментов в деке, и лишние сегменты получили бы родительский стиль (18 pt,
    чёрный вместо белого 8 pt). Поэтому подписи пересобираются по числу точек
    из первой как образца;
  * имя серии уходит в `c:tx` из `CategoryChartData`, поэтому его надо
    передавать тем же, что в прототипе, — иначе легенда/подсказки поменяются.

Число СЕРИЙ фиксировано прототипом (везде одна): вторая серия потребовала бы
перестройки plotArea, а дека её не рисует.
"""

from __future__ import annotations

from copy import deepcopy

from lxml import etree

from app.fot_deck_export import shapes

#: Пространства имён: Chart (данные диаграммы) и DrawingML (заливки точек).
C = "{http://schemas.openxmlformats.org/drawingml/2006/chart}"
A = "{http://schemas.openxmlformats.org/drawingml/2006/main}"


class ChartShape(RuntimeError):
    """Диаграмма не той формы, что ждёт биндер (число серий/точек)."""


def _chart_space(shape):
    chart = shape.chart
    return chart, chart._chartSpace  # noqa: SLF001 — публичного доступа к XML нет


def _series(chart) -> list:
    """Элементы `c:ser` в порядке документа (серии всех plot'ов)."""
    return chart._chartSpace.findall(".//" + C + "ser")  # noqa: SLF001


def series_count(shape) -> int:
    """Сколько серий у диаграммы (дека везде рисует одну)."""
    return len(_series(_chart_space(shape)[0]))


def point_count(shape) -> int:
    """Сколько точек в первой серии (по кэшу категорий)."""
    chart = _chart_space(shape)[0]
    ser = _series(chart)
    if not ser:
        return 0
    cat = ser[0].find(C + "cat")
    if cat is None:
        return 0
    pts = cat.findall(".//" + C + "pt")
    return len(pts)


def series_name(shape) -> str:
    """Имя серии из `c:tx` (в деке — «Ожидание 2026» / «Доля ФОТ в выручке»)."""
    ser = _series(_chart_space(shape)[0])
    if not ser:
        return ""
    tx = ser[0].find(C + "tx")
    if tx is None:
        return ""
    for v in tx.iter(C + "v"):
        return v.text or ""
    return ""


def point_colors(shape) -> list[str | None]:
    """Цвета точек `c:dPt` по индексам: [None, …] — точка красится серией.

    У доната цвет задан каждой точке, у мини-баров первая точка без `spPr`
    (наследует цвет серии) — эта разница воспроизводится как есть, поэтому
    ``None`` значим и не заменяется на цвет серии.
    """
    ser = _series(_chart_space(shape)[0])
    if not ser:
        return []
    out: list[str | None] = []
    for dpt in ser[0].findall(C + "dPt"):
        idx_el = dpt.find(C + "idx")
        idx = int(idx_el.get("val")) if idx_el is not None else len(out)
        clr = dpt.find(".//" + A + "srgbClr")
        while len(out) < idx:
            out.append(None)
        if len(out) == idx:
            out.append(clr.get("val") if clr is not None else None)
        else:
            out[idx] = clr.get("val") if clr is not None else None
    return out


def categories(shape) -> list[str]:
    """Категории первой серии (текст кэша, порядок точек)."""
    chart = _chart_space(shape)[0]
    return [str(c) for c in chart.plots[0].categories]


def values(shape) -> list[float]:
    """Значения первой серии в порядке точек (None → 0.0)."""
    chart = _chart_space(shape)[0]
    ser = chart.plots[0].series[0]
    return [float(v) if v is not None else 0.0 for v in ser.values]


def _drop_dpt(ser) -> None:
    for dpt in ser.findall(C + "dPt"):
        ser.remove(dpt)


def hex_color(color: str | None) -> str | None:
    """Цвет в виде, который ждёт `srgbClr/@val`: шесть hex-цифр без решётки.

    Сборщики листа 4/5 отдают цвета с `#` (так их пишет CSS-палитра веба), а
    схема DrawingML решётку не принимает: с ней PowerPoint считает файл
    повреждённым и красит точки по умолчанию. Поэтому цвет приводится здесь —
    на единственной двери, через которую он попадает в диаграмму.
    """
    if not color:
        return None
    val = str(color).strip().lstrip("#").upper()
    if len(val) != 6 or any(ch not in "0123456789ABCDEF" for ch in val):
        raise ChartShape("цвет %r — не шесть hex-цифр" % (color,))
    return val


def _make_dpt(template, idx: int, color: str | None):
    """Точка `c:dPt` по образцу: индекс, заливка; uniqueId образца не тащим."""
    el = deepcopy(template)
    idx_el = el.find(C + "idx")
    if idx_el is None:
        idx_el = etree.Element(C + "idx")
        el.insert(0, idx_el)
    idx_el.set("val", str(idx))
    if color is None:
        # без spPr точка наследует заливку серии — так размечена первая точка
        spPr = el.find(C + "spPr")
        if spPr is not None:
            el.remove(spPr)
    else:
        spPr = el.find(C + "spPr")
        if spPr is None:
            spPr = etree.SubElement(el, C + "spPr")
        fill = spPr.find(A + "solidFill")
        if fill is None:
            fill = etree.SubElement(spPr, A + "solidFill")
        for old in list(fill):
            fill.remove(old)
        srgb = etree.SubElement(fill, A + "srgbClr")
        srgb.set("val", color)
    for ext in el.findall(C + "extLst"):
        el.remove(ext)
    return el


def _insert_after(ser, anchor_tag: str, elements: list) -> None:
    """Вставляет элементы ПОСЛЕ последнего `anchor_tag` (порядок схемы)."""
    anchor = None
    for el in ser:
        if el.tag == anchor_tag:
            anchor = el
    if anchor is None:
        for el in ser:
            if el.tag in (C + "spPr", C + "explosion"):
                anchor = el
    for el in elements:
        if anchor is None:
            ser.insert(0, el)
        else:
            anchor.addnext(el)
            anchor = el


def _rebuild_dpt(ser, colors: list[str | None]) -> None:
    """Пересобирает точки под новое число сегментов, сохраняя окрас образца."""
    templates = ser.findall(C + "dPt")
    template = templates[0] if templates else None
    if template is None:
        # точек не было — не выдумываем: цвет серии остаётся как в прототипе
        return
    _drop_dpt(ser)
    made = [_make_dpt(template, idx, hex_color(color))
            for idx, color in enumerate(colors)]
    # dPt идут после spPr/explosion и до dLbls/cat/val
    if made:
        _insert_after(ser, C + "spPr", made)


def _rebuild_labels(ser, count: int) -> None:
    """По одной подписи `c:dLbl` на точку — образец первой, uniqueId не копируем."""
    dLbls = ser.find(C + "dLbls")
    if dLbls is None:
        return
    old = dLbls.findall(C + "dLbl")
    if not old:
        return
    template = old[0]
    for el in old:
        dLbls.remove(el)
    # новые подписи — в начало dLbls (до общих настроек блока)
    anchor = None
    for idx in range(count):
        el = deepcopy(template)
        idx_el = el.find(C + "idx")
        if idx_el is not None:
            idx_el.set("val", str(idx))
        for ext in el.findall(C + "extLst"):
            el.remove(ext)
        if anchor is None:
            dLbls.insert(0, el)
        else:
            anchor.addnext(el)
        anchor = el


def set_data(shape, cats: list[str], vals: list[float],
             colors: list[str | None] | None = None) -> None:
    """Подставляет живые категории и значения; цвета точек — позиционно.

    `colors` — палитра деки по индексу сегмента (манифест). Если она короче
    данных, хвост красится последним цветом палитры: у доната это «Прочие»,
    у баров точек всегда четыре.
    """
    from pptx.chart.data import CategoryChartData

    chart, _space = _chart_space(shape)
    if series_count(shape) != 1:
        raise ChartShape("диаграмма %r: серий %d, дека рисует одну"
                         % (shape.name, series_count(shape)))
    name = series_name(shape)
    data = CategoryChartData()
    data.categories = [str(c) for c in cats]
    data.add_series(name, tuple(float(v) for v in vals))
    chart.replace_data(data)

    ser = _series(chart)[0]
    if colors is None:
        colors = point_colors(shape)
    palette = list(colors) + [colors[-1] if colors else None] * max(
        0, len(cats) - len(colors))
    _rebuild_dpt(ser, palette[:len(cats)])
    _rebuild_labels(ser, len(cats))


__all__ = ["C", "ChartShape", "categories", "hex_color", "point_colors",
           "point_count", "series_count", "series_name", "set_data", "values"]
