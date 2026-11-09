"""Биндеры вкладки p14 «ФОТ по направлениям бизнеса» → слайды деки 4–6.

Вкладка повторяет ТРИ листа деки, и данные у них разные:

  * nb_summary (лист 4) — свод направлений: таблица «направление × периоды»,
    донат структуры ФОТ ожидания, карточки направлений с тремя кругами Δ доли
    и строками численности;
  * nb_prod (лист 5) — по СЛАЙДУ на каждую группу ЦФО из
    `group_detail.sections`: та же таблица/донат плюс карточки подразделений
    группы («подразделения» деки — это направления внутри группы ЦФО);
  * nb_sales (лист 6) — детализация `direction_detail`: направления, их
    расшифровки и ЦФО группы «Отдел», карточки листовых строк.

Донат строится по ПРАВИЛУ ЭКРАНА (capDonut в `nuxt/pages/reports/fot.vue`):
сегменты с неположительной долей отбрасываются, доли нормируются по сумме
положительных, при числе сегментов больше DONUT_MAX+1 остаются топ-9 и
«Прочие» суммой. Правило серверное, потому что число сегментов в живых данных
(десятки ЦФО) не совпадает с декой (5–7), и резать его на клиенте значило бы
рисовать разное число секторов на экране и в выгрузке. Красится донат ПОСЛЕ
свёртки и по индексу показанного сегмента — иначе хвост за концом палитры
слипался бы одним цветом.

Лист 4 идёт БЕЗ свёртки: на экране его донат рисуется из `pie_pct`
направлений как есть (`pieSegs`), и цвет у сегмента — цвет направления из
сборщика.

Подписи (формулировки деки, разбивка на абзацы и раны) — из манифеста
(`labels`/`header_labels`); цвета — тоже из манифеста (`nb_colors` и слоты
диаграмм), hex'ов в коде нет.
"""

from __future__ import annotations

import logging
import re

from app.fot_deck_export import charts
from app.fot_deck_export import format as fmt
from app.fot_deck_export import shapes
from app.fot_deck_export.errors import EmptySlice
from app.logship import log

#: Колонки таблиц листов 4–6 (порядок деки, 9 колонок): подпись и восемь
#: периодов. Δ-колонок в таблицах нет — отклонения живут в карточках.
TABLE_KEYS = ("label", "f25_h1", "f25_h2", "f25", "b26_h1", "f26_h1",
              "b26_h2", "t26_h2", "exp26")

#: Три блока карточки: ключ значения и пара Δ (процент, абсолют).
CARD_BLOCKS = (("f26_h1", "dev_h1_pct", "dev_h1_abs"),
               ("t26_h2", "dev_h2_pct", "dev_h2_abs"),
               ("exp26", "dev_year_pct", "dev_year_abs"))

#: Круги Δ доли карточки в порядке блоков.
PP_KEYS = ("pp_h1", "pp_h2", "pp_year")

#: Поля строки, которые печатают мини-бары карточки — четыре периода доли
#: ФОТ в выручке (порядок деки: 1ПГ прошлого года → ожидание года).
BAR_KEYS = ("share_h1_25", "share_h1_26", "share_h2_tac", "share_exp")

#: Предел сегментов доната до свёртки (правило capDonut: топ-9 + «Прочие»).
DONUT_MAX = 9

#: Пометка продолжения листа.
CONT_SUFFIX = " (продолжение)"

#: Слова подписи, которые зависят от закрытости 2 ПГ: пока полугодие не
#: закрыто, дека печатает «Тактика»/«ТАКТИКА», после закрытия — «Факт»/«ФАКТ»
#: (правило экрана `h2CardLabel` и `s56Cols`). Шаблон подписи снят с деки того
#: периода, когда 2 ПГ ещё не был закрыт, поэтому замена — по месту.
_H2_WORDS = (("ТАКТИКА", "ФАКТ"), ("Тактика", "Факт"), ("тактика", "факт"))


# ─── Общие помощники ────────────────────────────────────────────────────────

def _year_plan(payload: dict) -> int | None:
    """Год плана среза: из meta.periods (сборщик) либо из параметров."""
    periods = (payload.get("meta") or {}).get("periods") or {}
    if periods.get("year_plan"):
        return int(periods["year_plan"])
    params = (payload.get("meta") or {}).get("params") or {}
    if params.get("year"):
        return int(params["year"])
    return None


def _h2_closed(payload: dict) -> bool:
    """Закрыто ли 2 ПГ года фильтра (meta.expect.close_month ≥ 12)."""
    close = ((payload.get("meta") or {}).get("expect") or {}).get("close_month")
    return close is not None and int(close) >= 12


def _country(payload: dict, variant) -> tuple[str, str]:
    """(код, подпись) страны листа: из варианта реестра, иначе из meta."""
    cc = (variant or {}).get("cc") if isinstance(variant, dict) else None
    slide_meta = (payload.get("meta") or {}).get("slide") or {}
    if not cc:
        cc = slide_meta.get("cc")
    label = slide_meta.get("label") or cc or ""
    return str(cc or ""), str(label)


def _sign_color(token: str | None, sign: dict) -> str | None:
    """Токен стиля → цвет деки; ноль и «нейтрально» цвет не меняют."""
    if token in (None, fmt.TOKEN_NEUTRAL, fmt.TOKEN_ZERO):
        return None
    if token == fmt.TOKEN_NA:
        return sign.get("na_text")
    if token == fmt.TOKEN_UP:
        return sign.get("delta_up")
    if token == fmt.TOKEN_DOWN:
        return sign.get("delta_down")
    raise RuntimeError("fot_deck: неизвестный токен стиля %r" % (token,))


def _pp_color(token: str | None, sign: dict) -> str | None:
    """Цвет обводки круга «п.п.»: ноль и «н/д» — приглушённый серый деки."""
    if token in (None, fmt.TOKEN_ZERO, fmt.TOKEN_NEUTRAL):
        return sign.get("na_text")
    return _sign_color(token, sign)


def _label_key(name: str) -> str:
    """Имя подписи в манифесте: ``card7_p1_label`` → ``cardN_p1_label``."""
    return re.sub(r"^card\d+_", "cardN_", name)


def _label_name(ctx, name: str) -> str:
    """Ключ подписи в манифесте: точное имя ячейки, иначе свёрнутое по месту.

    Подписи, которые у ячеек РАЗНЫЕ («+4,0% к факту 2025 (+1 243)» — своё
    значение и своя разбивка на раны), остаются в манифесте под конкретным
    номером места; одинаковые («1 ПГ 2026») свёрнуты в ``cardN_``. Поэтому
    сначала ищется точное имя: иначе такая подпись считалась бы отсутствующей
    и биндер собрал бы текст сам, потеряв формулировку деки.
    """
    if ctx.has_label(name):
        return name
    return _label_key(name)


def _fill_label(ctx, slide, name: str, *, year, prev=None, country=None,
                group=None, values=(), para_idx: int = 0,
                write_para: int | None = None,
                closed: bool = False) -> list[str] | None:
    """Пишет подпись шейпа по шаблону деки; нет шейпа/шаблона — None.

    `para_idx` — номер АБЗАЦА ШАБЛОНА в манифесте (сборка актива выбрасывает
    абзацы, в которых нечего подставлять, поэтому нумерация шаблона не всегда
    совпадает с нумерацией абзацев шейпа), `write_para` — абзац ШЕЙПА, куда
    писать; по умолчанию тот же. Двухстрочный заголовок доната — как раз такой
    случай: в шаблоне один абзац («ОЖИДАНИЕ {year}»), а на слайде он второй.

    `closed` включает подстановку слова закрытого 2 ПГ («Тактика»→«Факт»):
    шаблон снят с деки незакрытого периода, а подпись зависит от закрытости.
    """
    sh = shapes.find_shape(slide, name)
    if sh is None:
        return None
    key = _label_name(ctx, name)
    if not ctx.has_label(key):
        return None
    if prev is None and year:
        prev = int(year) - 1
    paras = ctx.label(key, year=year, prev=prev, country=country, group=group,
                      values=values)
    if para_idx >= len(paras):
        raise RuntimeError("fot_deck: подпись %r короче, чем ожидает биндер "
                           "(абзацев %d, нужен %d)"
                           % (key, len(paras), para_idx + 1))
    runs = list(paras[para_idx])
    if closed:
        runs = [_closed_word(text) for text in runs]
    # у пятой карточки деки знак отклонения вынесен ОТДЕЛЬНЫМ раном («+»,
    # «2,8», «% к факту 2025 (+», «7», «)»), а биндер пишет значение целиком
    # со знаком — без снятия рана-знака строка напечаталась бы «+−3»
    if len(runs) > 1 and runs[0].strip() in ("+", fmt.MINUS, "-"):
        runs[0] = ""
    shapes.set_text(sh, list(enumerate(runs)),
                    para_idx=para_idx if write_para is None else write_para)
    return runs


def _closed_word(text: str) -> str:
    """«ТАКТИКА 2 ПГ 2026» → «ФАКТ 2 ПГ 2026» (2 ПГ года уже закрыт)."""
    for src, dst in _H2_WORDS:
        if src in text:
            return text.replace(src, dst, 1)
    return text


def _money(value, cfg: dict) -> str:
    """Денежное значение листов 4–6: целые тыс. без плюса (это сумма)."""
    text, _token = fmt.money(value, group=bool(cfg.get("cell_group")),
                             digits=int(cfg.get("cell_digits") or 0),
                             plus=False)
    return text


def _paginate(rows: list[dict], capacity: int) -> list[list[dict]]:
    """Раскладка строк по страницам ёмкости прототипа (пустой — одна)."""
    cap = int(capacity or 0) or max(len(rows), 1)
    pages: list[list[dict]] = []
    page: list[dict] = []
    used = 0
    for row in rows:
        if page and used >= cap:
            pages.append(page)
            page, used = [], 0
        page.append(row)
        used += 1
    pages.append(page)
    return pages or [[]]


def _pad(pages: list[list], count: int) -> list[list]:
    """Дополняет список страниц пустыми до нужного числа."""
    out = [list(p) for p in pages]
    while len(out) < count:
        out.append([])
    return out


def _pages_by_leaves(rows: list[dict], leaves: list[dict],
                     per_page: int) -> list[list[dict]]:
    """Страницы строк таблицы, выровненные со страницами карточек.

    Карточки пагинируются по местам сетки, а таблица — по ёмкости строк тела;
    когда листьев больше, чем мест (у листа 6 их 7, а листьев с расшифровками
    бывает 8+), таблица уходила вперёд и продолжение оставалось с ПУСТОЙ
    таблицей. Поэтому страница режется по `per_page` листьев (минимум из двух
    ёмкостей), а каждая строка попадает на страницу своего листа: строка-группа
    с расшифровками — на страницу первой расшифровки.
    """
    if not rows:
        return [[]]
    step = max(int(per_page or 1), 1)
    leaf_page = {id(leaf): idx // step for idx, leaf in enumerate(leaves)}
    count = max(1, (len(leaves) + step - 1) // step)
    pages: list[list[dict]] = [[] for _ in range(count)]
    for row in rows:
        page = leaf_page.get(id(row))
        if page is None:
            page = next((leaf_page[id(leaf)] for leaf in leaves
                         if leaf.get("parent") == row.get("label")), 0)
        pages[page].append(row)
    return pages


# ─── Шапка листов 4–6 ───────────────────────────────────────────────────────

def _fill_head(ctx, slide, payload: dict, variant, *, year, prev,
               group: str | None = None, hc: dict | None = None,
               suffix: str = "") -> None:
    """Заголовок, бейдж и строка численности листа."""
    cc, label = _country(payload, variant)
    _fill_title(ctx, slide, label, suffix, year=year, prev=prev, group=group)
    _fill_badge(ctx, slide, cc)
    _fill_hc_note(ctx, slide, hc if hc is not None
                  else (payload.get("headcount") or {}), year=year, prev=prev)


def _fill_title(ctx, slide, country: str, suffix: str, *, year, prev,
                group: str | None = None) -> None:
    """Подзаголовок листа: бренд деки + подпись среза (страна и группа).

    У заголовка ДВА рана: «ГК «МАРК ФОРМЭЛЬ»  » (константа из манифеста) и
    подпись блока, в которой страна и группа стоят прорезями.
    """
    title = shapes.require_shape(slide, "title")
    lead = ctx.const(ctx.proto_name, "title_lead")
    if lead and len(title.text_frame.paragraphs[0].runs) >= 2:
        shapes.set_run(title, 0, lead)
    if not ctx.has_label("title"):
        raise RuntimeError("fot_deck: у прототипа %r нет подписи title — "
                           "пересоберите актив" % ctx.proto_name)
    paras = ctx.label("title", country=country, year=year, prev=prev,
                      group=group)
    subtitle = "".join(paras[0]) + suffix
    if len(title.text_frame.paragraphs[0].runs) >= 2:
        shapes.set_run(title, 1, subtitle)
    else:
        shapes.set_run(title, 0, subtitle)


def _fill_badge(ctx, slide, cc: str) -> None:
    """Бейдж листа: код страны в первом абзаце, блок деки — во втором.

    У листов 5/6 бейдж двухстрочный («BY» / «ПРОИЗВ.»), у листа 4 — один код
    страны. Второй абзац — константа деки из манифеста, но токен с него снят
    при сборке актива, поэтому писать его обязан биндер.
    """
    badge = shapes.find_shape(slide, "badge")
    if badge is None:
        return
    shapes.set_run(badge, 0, str(cc or ""))
    if len(badge.text_frame.paragraphs) > 1:
        block = (ctx._engine.manifest["prototypes"][ctx.proto_name]  # noqa: SLF001
                 .get("consts") or {}).get("badge_block") or ""
        shapes.set_text(badge, [(0, block)], para_idx=1)


def _fill_hc_note(ctx, slide, hc: dict, year, prev) -> None:
    """Строка «Числ. страны (ПЧ/ФЧ 1ПГ26): 4 537,7/4 383,8, 96,6% укомпл.»."""
    if not hc:
        return
    # у свода листа 4 ключи сборщика (plan_approved/fact_2026), у секций и
    # листа 6 — короткие (plan/fact): веб читает их напрямую, поэтому биндер
    # понимает оба вида, иначе шапка листов 5/6 печатала «н/д/н/д»
    plan_raw = hc["plan_approved"] if "plan_approved" in hc else hc.get("plan")
    fact_raw = hc["fact_2026"] if "fact_2026" in hc else hc.get("fact")
    plan, fact = _hc_pair(plan_raw, fact_raw, ctx.fmt).split("/", 1)
    # знаки процента — из манифеста, и ноль здесь значим: у листов 5/6 дека
    # печатает «96% укомпл.», у листа 4 — «96,6%». `or 1` превратил бы ноль
    # в один знак и разъехался бы с декой
    digits = ctx.fmt.get("pct_digits")
    pct_text, _t = fmt.pct(hc.get("staffing"),
                           int(digits) if digits is not None else 1,
                           ctx.pct_bands)
    _fill_label(ctx, slide, "hc_note", year=year, prev=prev,
                values=(plan, fact, pct_text.rstrip("%")))


def _hc_pair(plan, fact, cfg: dict) -> str:
    """Пара «ПЧ/ФЧ» листов 4–6: запятая и разряды, как у остальных чисел.

    Точка вместо запятой — типографика листов 7/8 (`hc_dot` в манифесте); у
    листов 4–6 флаг выключен, но правило остаётся общим, чтобы лист не зависел
    от того, кто его печатает.
    """
    digits = 1
    left, _t = fmt.money(plan, group=bool(cfg.get("cell_group")), digits=digits,
                         plus=False)
    right, _t = fmt.money(fact, group=bool(cfg.get("cell_group")), digits=digits,
                          plus=False)
    if cfg.get("hc_dot"):
        left, right = left.replace(",", "."), right.replace(",", ".")
    return "%s/%s" % (left, right)


# ─── Таблица «периоды × строки» ─────────────────────────────────────────────

def _fill_table(ctx, slide, rows: list[dict], *, total: dict | None,
                year, prev, group_rows: bool, closed: bool) -> None:
    """Таблица листов 4–6: шапка из манифеста, итог — ПЕРВОЙ строкой.

    Порядок строк — как у деки и веба (`_p14_assemble`/`_p14_slide_entry`):
    итог, затем строки среза. Групповые строки (лист 6, kind='direction')
    печатаются прототипом `group` — у деки они жирные и своей заливкой.
    """
    name = ctx.table_names()[0] if ctx.table_names() else "tbl_directions"
    frame = shapes.find_shape(slide, name)
    if frame is None:
        raise RuntimeError("прототип %r: на слайде нет таблицы %r"
                           % (ctx.proto_name, name))
    header = ctx.header_labels(name, year=year, prev=prev)
    if closed:
        header = [[_closed_word(text) for text in col] for col in header]
    spec = [{"style": "header", "cells": header}]
    if total is not None:
        spec.append({"style": "total_top", "cells": _row_cells(ctx, total)})
    zebra = 0
    for row in rows:
        if group_rows and row.get("kind") == "direction":
            style = "group"
        else:
            style = "body_tinted" if zebra % 2 == 0 else "body_plain"
            zebra += 1
        spec.append({"style": style, "cells": _row_cells(ctx, row)})
    shapes.fill_table(frame, spec, ctx.table_protos(frame, name), ctx.sign,
                      proto_row_count=ctx.deck_rows(name))


def _row_cells(ctx, row: dict) -> list[list[str]]:
    """Ячейки строки таблицы: подпись как есть, периоды — целыми тыс."""
    cells = []
    for key in TABLE_KEYS:
        if key == "label":
            cells.append([str(row.get("label") or "")])
        else:
            cells.append([_money(row.get(key), ctx.fmt)])
    return cells


# ─── Донат ──────────────────────────────────────────────────────────────────

def _direction_segments(directions: list[dict]) -> list[dict]:
    """Сегменты доната листа 4: у направлений доля лежит в `pie_pct`.

    Сборщик листа 4 кладёт долю «Ожидания» направления в итоге среза отдельным
    полем (`pie_pct`), а у слайдов 5–6 то же самое называется `pct`. Приводим
    к общему виду здесь, чтобы правило свёртки было одно на все три листа.
    """
    return [{"label": d.get("label"), "value": d.get("exp26"),
             "pct": d.get("pie_pct"), "color": d.get("color")}
            for d in directions or []]


def _cap_donut(segments: list[dict], palette: list, rest_color: str | None,
               cap: bool) -> list[dict]:
    """Правило экрана (capDonut): топ-9 по доле + «Прочие», доли нормированы.

    Копия клиентского правила 1:1, потому что число сегментов в живых данных
    (десятки ЦФО) не совпадает с декой: без него выгрузка рисовала бы десятки
    секторов, а экран — девять и «Прочие».

    `cap=False` — лист 4: его донат экран рисует из всех направлений среза
    (`pieSegs`), без свёртки, и цвет у сегмента — цвет направления.
    """
    src = [dict(s) for s in segments or []
           if s.get("pct") is not None and float(s["pct"]) > 0]
    if not src:
        return []
    pos = sum(float(s["pct"]) for s in src)
    if pos > 0 and abs(pos - 100) > 0.05:
        for s in src:
            s["pct"] = float(s["pct"]) / pos * 100
    else:
        for s in src:
            s["pct"] = float(s["pct"])
    if cap and len(src) > DONUT_MAX + 1:
        top = sorted(src, key=lambda s: float(s["pct"]), reverse=True)[:DONUT_MAX]
        rest_pct = (sum(float(s["pct"]) for s in src)
                    - sum(float(s["pct"]) for s in top))
        rest_val = (sum(float(s.get("value") or 0) for s in src)
                    - sum(float(s.get("value") or 0) for s in top))
        out = _paint(top, palette, rest_color, by_index=True)
        out.append({"label": "Прочие", "value": rest_val, "pct": rest_pct,
                    "color": rest_color})
        return out
    return _paint(src, palette, rest_color, by_index=cap)


def _paint(items: list[dict], palette: list, rest_color: str | None,
           *, by_index: bool) -> list[dict]:
    """Цвета сегментов ПО ИНДЕКСУ показанного сегмента (после свёртки).

    Палитра — цвета dPt прототипа в порядке деки, затем общая палитра листа.

    `by_index=True` (слайды 5–6) — как `capDonut` экрана: цвет берётся из
    палитры по позиции, свой цвет сегмента игнорируется. Иначе листья за
    концом серверной палитры (она обрезана на седьмом цвете) слипались бы в
    один. `by_index=False` (лист 4) — цвет направления из сборщика, он и есть
    палитра деки, экран рисует `pieSegs` тем же полем.
    """
    full = [c for c in list(palette or []) if c]
    out = []
    for i, seg in enumerate(items):
        seg = dict(seg)
        if by_index or not seg.get("color"):
            seg["color"] = full[i] if i < len(full) else rest_color
        out.append(seg)
    return out


def _fill_donut(ctx, slide, segments: list[dict], *, year, cap: bool) -> None:
    """Донат структуры ФОТ: правило экрана + цвета деки/палитры."""
    sh = shapes.find_shape(slide, "chart_donut")
    if sh is None:
        return
    slot = ctx.chart_slot("chart_donut")
    colors_cfg = ctx.colors("nb_colors")
    # палитра прототипа (порядок dPt деки) + хвост общей палитры листа: без
    # него сегменты за концом короткой палитры слиплись бы одним цветом
    palette = list(slot.get("point_colors") or [])
    palette += [c for c in colors_cfg.get("pie_palette") or []
                if c not in palette]
    capped = _cap_donut(segments, palette, colors_cfg.get("pie_rest"), cap)
    if not capped:
        # сегментов нет — нет и блока: заголовок доната уходит вместе с ним,
        # иначе в выгрузке остался бы его незаполненный токен
        _drop_charts(ctx, slide)
        return
    charts.set_data(sh, [str(s["label"]) for s in capped],
                    [float(s["pct"]) for s in capped],
                    colors=[s.get("color") for s in capped])
    _fill_label(ctx, slide, "donut_head", year=year,
                prev=(year - 1) if year else None, write_para=1)


# ─── Мини-бары карточки ─────────────────────────────────────────────────────

def _bar_caps(ctx, slot: dict, year, prev) -> list[str]:
    """Подписи точек мини-бара: шаблоны деки с прорезями годов.

    Год в подписях периодов — из фильтра, поэтому в манифесте они лежат
    прорезями (`Факт 1ПГ{p2}`), а не текстом деки.
    """
    runs = [{"text": str(t)} for t in (slot.get("categories") or [])]
    if not runs:
        return []
    paras = fmt.fill_label([{"para": 0, "runs": runs}], year=year, prev=prev)
    return list(paras[0])


def _fill_bars(ctx, slide, name: str, row: dict, year, prev) -> None:
    """Мини-бары карточки: четыре периода доли ФОТ в выручке."""
    sh = shapes.find_shape(slide, name)
    if sh is None:
        return
    slot = ctx.chart_slot(name)
    caps = _bar_caps(ctx, slot, year, prev)
    if len(caps) != len(BAR_KEYS):
        raise RuntimeError(
            "fot_deck: у мини-бара %r %d подписей, а периодов %d — "
            "пересоберите актив" % (name, len(caps), len(BAR_KEYS)))
    values = []
    for key in BAR_KEYS:
        value = row.get(key)
        values.append(float(value) if value is not None else 0.0)
    charts.set_data(sh, caps, values, colors=slot.get("point_colors"))


# ─── Карточки ───────────────────────────────────────────────────────────────

def _fill_cards(ctx, slide, cards: list[dict], *, year, prev, closed: bool,
                with_hc: bool) -> None:
    """Сетка карточек: заполняет поместившиеся, у лишних ячеек — удаление."""
    count = ctx.card_count()
    for i in range(1, count + 1):
        if i <= len(cards):
            _fill_card(ctx, slide, i, cards[i - 1], year=year, prev=prev,
                       closed=closed, with_hc=with_hc)
        else:
            _drop_card(slide, i)


def _fill_card(ctx, slide, idx: int, row: dict, *, year, prev, closed: bool,
               with_hc: bool) -> None:
    """Карточка строки: имя, мини-бары, три блока значений, три круга, ПЧ."""
    prefix = "card%d_" % idx
    title = shapes.find_shape(slide, prefix + "title")
    if title is not None:
        shapes.set_run(title, 0, str(row.get("label") or ""))
    _fill_bars(ctx, slide, prefix + "chart", row, year, prev)
    for num, (value_key, pct_key, abs_key) in enumerate(CARD_BLOCKS, 1):
        _fill_label(ctx, slide, "%sp%d_label" % (prefix, num), year=year,
                    prev=prev, closed=closed)
        _fill_label(ctx, slide, "%sp%d_value" % (prefix, num), year=year,
                    prev=prev, values=(_money(row.get(value_key), ctx.fmt),))
        _put_dev(ctx, slide, "%sp%d_dev" % (prefix, num), row, pct_key, abs_key,
                 year=year, prev=prev, closed=closed)
        _put_pp(ctx, slide, prefix, num, row.get(PP_KEYS[num - 1]), year, prev)
    if with_hc:
        _fill_card_hc(ctx, slide, prefix, row, year=year, prev=prev)


def _abs_dev_text(ctx, name: str, value) -> str:
    """Абсолютное отклонение в строке карточки — как его печатает дека.

    Два свойства деки, которые нельзя задать одним форматом:
      * разряды в скобках разделены ОБЫЧНЫМ пробелом («(+1 243)»), хотя в
        таблицах и значениях карточек дека ставит неразрывный;
      * у части карточек знак «+» вынесен В ЛИТЕРАЛ подписи («(+{v2})»), и
        значение печатается без своего плюса, иначе вышло бы «(++7)».
    """
    plus = not _has_literal_plus(ctx, name, 2)
    text, _t = fmt.money(value, group=bool(ctx.fmt.get("cell_group")),
                         digits=int(ctx.fmt.get("cell_digits") or 0),
                         plus=plus, nbsp=False)
    return text


def _has_literal_plus(ctx, name: str, slot: int) -> bool:
    """Стоит ли «+» литералом прямо перед прорезью ``{vN}`` подписи."""
    key = _label_name(ctx, name)
    if not ctx.has_label(key):
        return False
    meta = ctx._engine.manifest["prototypes"][ctx.proto_name]  # noqa: SLF001
    slots = (meta.get("labels") or {}).get(key) or []
    runs = [run for para in slots for run in para.get("runs") or []]
    for i, run in enumerate(runs):
        if ("{v%d}" % slot) not in (run.get("text") or ""):
            continue
        return i > 0 and (runs[i - 1].get("text") or "").endswith("+")
    return False


def _put_dev(ctx, slide, name: str, row: dict, pct_key: str, abs_key: str,
             *, year, prev, closed: bool) -> None:
    """Строка отклонения блока: «+4,0% к факту 2025 (+1 243)».

    Процент считается только от ПОЛОЖИТЕЛЬНОЙ базы (правило сборщика): при
    нулевой или отрицательной базе его нет, и в подписи остаётся «н/д». Такие
    строки дека печатает зелёным (правило листов 7/8, воспроизводится 1:1).
    """
    sh = shapes.find_shape(slide, name)
    if sh is None:
        return
    cfg = ctx.fmt
    pct_text, token = fmt.signed(row.get(pct_key), digits=1,
                                 group=bool(cfg.get("cell_group")))
    abs_text = _abs_dev_text(ctx, name, row.get(abs_key))
    if token == fmt.TOKEN_NA:
        token = fmt.TOKEN_DOWN
    runs = _fill_label(ctx, slide, name, year=year, prev=prev, closed=closed,
                       values=(pct_text, abs_text))
    if runs is None:
        # подпись не снята шаблоном — пишем значения в единственный ран,
        # сохраняя декину связку «% к факту» вокруг них
        shapes.set_run(sh, 0, "%s%% к факту %s (%s)"
                       % (pct_text.rstrip("%"), prev or "", abs_text))
        return
    color = _sign_color(token, ctx.sign)
    if color:
        # цвет несёт ВСЯ строка отклонения (у деки все её раны одного цвета):
        # у пятой карточки листа 4 знак вынесен отдельным раном, и покраска
        # только первого рана оставила бы значение чёрным
        for idx in range(len(sh.text_frame.paragraphs[0].runs)):
            shapes.paint_run(sh, color, run_idx=idx)


def _put_pp(ctx, slide, prefix: str, num: int, value, year, prev) -> None:
    """Круг Δ доли: значение, единица «п.п.» и обводка по знаку."""
    cfg = ctx.fmt
    text, token = fmt.pp(value, int(cfg.get("pp_digits") or 2),
                         int(cfg.get("pp_zero_digits") or 2))
    y2 = str(year)[-2:] if year else ""
    text_sh = shapes.find_shape(slide, "%spp%d_text" % (prefix, num))
    if text_sh is not None:
        shapes.set_run(text_sh, 0, text)
        if len(text_sh.text_frame.paragraphs) > 1:
            # единица «п.п.» — второй абзац круга, у деки она ВСЕГДА серая,
            # поэтому цвет значения на неё не переносится
            shapes.set_text(text_sh, [(0, "п.п.")], para_idx=1)
            muted = ctx.colors("nb_colors").get("muted")
            if muted:
                shapes.paint_run(text_sh, muted, para_idx=1, run_idx=0)
        color = _sign_color(token, ctx.sign)
        if color:
            shapes.paint_run(text_sh, color, run_idx=0)
    circle = shapes.find_shape(slide, "%spp%d_circle" % (prefix, num))
    if circle is not None:
        color = _pp_color(token, ctx.sign)
        if color:
            shapes.paint_outline(circle, color)
    _fill_label(ctx, slide, "%spp%d_cap" % (prefix, num), year=year, prev=prev,
                values=(y2,))


def _fill_card_hc(ctx, slide, prefix: str, row: dict, *, year, prev) -> None:
    """Строки численности карточки: пара ПЧ/ФЧ и отклонение ФЧ."""
    cfg = ctx.fmt
    line = shapes.find_shape(slide, prefix + "hc_line")
    if line is not None:
        plan, fact = _hc_pair(row.get("hc_plan"), row.get("hc_fact"),
                              cfg).split("/", 1)
        # знаки «% укомпл.» в карточке свои (у деки «96%» против «96,6%» в
        # шапке того же листа) — сняты с деки отдельным флагом формата
        pct_text, _t = fmt.pct(row.get("hc_staffing"),
                               int(cfg.get("card_pct_digits") or 0),
                               ctx.pct_bands)
        runs = _fill_label(ctx, slide, prefix + "hc_line", year=year, prev=prev,
                           values=(plan, fact, pct_text.rstrip("%")))
        if runs is None:
            shapes.set_run(line, 0, "ПЧ/ФЧ 1ПГ %s: %s/%s, %s%%"
                           % (year, plan, fact, pct_text))
            for idx in range(1, len(line.text_frame.paragraphs[0].runs)):
                shapes.set_run(line, idx, "")
    dev_sh = shapes.find_shape(slide, prefix + "hc_dev")
    if dev_sh is None:
        return
    dev = row.get("hc_dev")
    if dev is None:
        for idx in range(len(dev_sh.text_frame.paragraphs[0].runs)):
            shapes.set_run(dev_sh, idx, "")
        return
    dev_text, token = fmt.signed(dev, digits=1, group=False)
    if _fill_label(ctx, slide, prefix + "hc_dev", year=year, prev=prev,
                   values=(dev_text,)) is None:
        shapes.set_run(dev_sh, 0, dev_text)
        return
    color = _sign_color(token, ctx.sign)
    if color:
        shapes.paint_run(dev_sh, color, run_idx=1)


def _drop_card(slide, idx: int) -> None:
    """Удаляет все шейпы карточки N по префиксу имени (вместе с мини-барами)."""
    prefix = "card%d_" % idx
    for sh in list(slide.shapes):
        if sh.name.startswith(prefix):
            shapes.delete_shape(sh)


def _drop_charts(ctx, slide) -> None:
    """Убирает донат и его заголовок (продолжение листа без диаграмм)."""
    for name in ("chart_donut", "donut_head"):
        sh = shapes.find_shape(slide, name)
        if sh is not None:
            shapes.delete_shape(sh)


# ─── Лист 4: свод направлений ───────────────────────────────────────────────

def bind_summary(ctx, payload: dict, variant=None) -> None:
    """Лист 4: «ФОТ по направлениям бизнеса: свод, структура и динамика»."""
    directions = payload.get("directions") or []
    table = next((t for t in payload.get("tables") or []
                  if t.get("rows")), None) or {}
    rows = table.get("rows") or []
    if not directions or not rows:
        raise EmptySlice("p14: в срезе нет направлений — выгрузка листа "
                         "невозможна")
    year = _year_plan(payload)
    prev = (year - 1) if year else None
    closed = _h2_closed(payload)
    total = next((r for r in rows if r.get("total")), None)
    body = [r for r in rows if not r.get("total")]
    table_name = ctx.table_names()[0]
    table_pages = _paginate(body, ctx.capacity(table_name))
    card_pages = _paginate(directions, ctx.card_count())
    count = max(len(table_pages), len(card_pages))
    table_pages, card_pages = _pad(table_pages, count), _pad(card_pages, count)

    for page_idx in range(count):
        slide = ctx.slide(page_idx)
        _fill_head(ctx, slide, payload, variant, year=year, prev=prev,
                   suffix=CONT_SUFFIX if page_idx else "")
        _fill_table(ctx, slide, table_pages[page_idx],
                    total=total if page_idx == 0 else None, year=year, prev=prev,
                    group_rows=False, closed=closed)
        if page_idx == 0:
            # экран рисует донат листа 4 из ВСЕХ направлений среза (pieSegs),
            # без свёртки и с цветами направлений
            _fill_donut(ctx, slide, _direction_segments(directions), year=year,
                        cap=False)
        else:
            _drop_charts(ctx, slide)
        _fill_cards(ctx, slide, card_pages[page_idx], year=year, prev=prev,
                    closed=closed, with_hc=True)
    log(logging.INFO, "fot_deck: лист 4 собран", tab=ctx.tab, pages=count,
        directions=len(directions))


# ─── Лист 5: группы ЦФО ─────────────────────────────────────────────────────

def bind_prod(ctx, payload: dict, variant=None) -> None:
    """Лист 5: «<Группа ЦФО>: структура ФОТ по подразделениям и динамика»."""
    section = (variant or {}).get("section") if isinstance(variant, dict) else None
    if not section:
        raise EmptySlice("p14: нет группы ЦФО для листа 5")
    year = _year_plan(payload)
    prev = (year - 1) if year else None
    closed = _h2_closed(payload)
    rows = section.get("rows") or []
    if not rows:
        raise EmptySlice("p14: в группе %r нет строк — лист 5 не строится"
                         % (section.get("label"),))
    total = next((r for r in rows if r.get("kind") == "total"), None)
    body = [r for r in rows if r.get("kind") != "total"]
    table_name = ctx.table_names()[0]
    # строки и карточки листа 5 — один и тот же список, поэтому страницы
    # совпадают по построению (обе ёмкости в деке равны)
    per_page = min(ctx.capacity(table_name), ctx.card_count())
    table_pages = _pages_by_leaves(body, body, per_page)
    card_pages = _paginate(body, ctx.card_count())
    count = max(len(table_pages), len(card_pages))
    table_pages, card_pages = _pad(table_pages, count), _pad(card_pages, count)

    for page_idx in range(count):
        slide = ctx.slide(page_idx)
        _fill_head(ctx, slide, payload, variant, year=year, prev=prev,
                   group=section.get("label"), hc=section.get("headcount") or {},
                   suffix=CONT_SUFFIX if page_idx else "")
        _fill_table(ctx, slide, table_pages[page_idx],
                    total=total if page_idx == 0 else None, year=year, prev=prev,
                    # строк-групп на листе 5 нет: его «подразделения» —
                    # направления группы, у деки они идут обычной зеброй
                    group_rows=False, closed=closed)
        if page_idx == 0:
            _fill_donut(ctx, slide, section.get("donut") or [], year=year,
                        cap=True)
        else:
            _drop_charts(ctx, slide)
        _fill_cards(ctx, slide, card_pages[page_idx], year=year, prev=prev,
                    closed=closed, with_hc=False)
    log(logging.INFO, "fot_deck: лист 5 собран", tab=ctx.tab,
        group=section.get("label"), pages=count, rows=len(body))


# ─── Лист 6: детализация направлений ────────────────────────────────────────

def bind_sales(ctx, payload: dict, variant=None) -> None:
    """Лист 6: «Продажи: структура ФОТ по каналам и динамика»."""
    detail = ((variant or {}).get("detail") if isinstance(variant, dict) else None) \
        or payload.get("direction_detail")
    if not detail:
        raise EmptySlice("p14: нет детализации направлений — лист 6 не строится")
    year = _year_plan(payload)
    prev = (year - 1) if year else None
    closed = _h2_closed(payload)
    rows = detail.get("rows") or []
    if not rows:
        raise EmptySlice("p14: в детализации нет строк — лист 6 не строится")
    total = next((r for r in rows if r.get("kind") == "total"), None)
    body = [r for r in rows if r.get("kind") != "total"]
    cards = detail.get("cards") or []
    table_name = ctx.table_names()[0]
    # Групповые строки (kind='direction') места в таблице не занимают — их
    # печатает прототип `group`, а карточки (листья) занимают место в сетке.
    # Страница режется по первому исчерпанному лимиту, поэтому продолжение
    # листа 6 не остаётся с пустой таблицей, когда карточек больше, чем мест.
    per_page = min(ctx.capacity(table_name), ctx.card_count())
    table_pages = _pages_by_leaves(body, cards, per_page)
    card_pages = _paginate(cards, ctx.card_count())
    count = max(len(table_pages), len(card_pages))
    table_pages, card_pages = _pad(table_pages, count), _pad(card_pages, count)

    for page_idx in range(count):
        slide = ctx.slide(page_idx)
        _fill_head(ctx, slide, payload, variant, year=year, prev=prev,
                   hc=detail.get("headcount") or {},
                   suffix=CONT_SUFFIX if page_idx else "")
        _fill_table(ctx, slide, table_pages[page_idx],
                    total=total if page_idx == 0 else None, year=year, prev=prev,
                    group_rows=True, closed=closed)
        if page_idx == 0:
            _fill_donut(ctx, slide, detail.get("donut") or [], year=year,
                        cap=True)
        else:
            _drop_charts(ctx, slide)
        _fill_cards(ctx, slide, card_pages[page_idx], year=year, prev=prev,
                    closed=closed, with_hc=True)
    log(logging.INFO, "fot_deck: лист 6 собран", tab=ctx.tab, pages=count,
        rows=len(body), cards=len(cards))


# ─── Варианты листов: по стране и по группе ЦФО ─────────────────────────────

#: Порядок стран деки: листы 4–6 идут BY → RU → KZ → UZ.
COUNTRY_ORDER = ("BY", "RU", "KZ", "UZ")


def country_variants(payload: dict):
    """Варианты листа 4: по слайду на каждую страну среза.

    Дека показывает лист 4 для страны (у неё — BY). Если сборщик отдал
    `meta.slide.cc` (срез уже сужен фильтром), вариант один; иначе лист
    печатается постранично по странам, как листы 7/8.
    """
    slide_meta = (payload.get("meta") or {}).get("slide") or {}
    cc = slide_meta.get("cc")
    if cc:
        return [({"cc": cc}, payload)]
    present = sorted({d.get("cc") for d in payload.get("directions") or []
                      if d.get("cc")},
                     key=lambda c: (COUNTRY_ORDER.index(c)
                                    if c in COUNTRY_ORDER else len(COUNTRY_ORDER)))
    if not present:
        return [({"cc": ""}, payload)]
    return [({"cc": code}, payload) for code in present]


def _production(section: dict) -> bool:
    """Секция — производство (лист 5 деки), а не продажи и не отделы.

    Признак `kind` ставит сборщик по каналу сбыта справочника (fot_cfo.
    sales_channel); payload'ы без поля (старые моки, проверочные фикстуры)
    опознаются по имени группы — у деки это «Производство» и «Производство
    картонажное».
    """
    kind = section.get("kind")
    if kind:
        return kind == "production"
    return str(section.get("label") or "").startswith("Производство")


def section_variants(payload: dict):
    """Варианты листа 5: слайд на каждую ПРОИЗВОДСТВЕННУЮ группу ЦФО среза.

    Лист 5 деки — только производство: продажные каналы печатает лист 6
    (nb_sales), отделы — листы 7/8 (вкладки p15/p16). Раньше слайд строился
    на КАЖДУЮ группу ЦФО, и в производственную секцию попадали отделы,
    магазины и интернет-магазин.
    """
    sections = [s for s in
                (payload.get("group_detail") or {}).get("sections") or []
                if _production(s)]
    if not sections:
        return []
    return [({"cc": variant.get("cc"), "section": section}, data)
            for variant, data in country_variants(payload)
            for section in sections]


def detail_variants(payload: dict):
    """Варианты листа 6: детализация ПРОДАЖ по каждой стране среза.

    Сборщик кладёт sales_detail — те же строки, что у direction_detail, но
    только по каналам продаж (розница/опт/маркетплейсы/интернет-магазин):
    лист 6 деки «Продажи» не печатает ни цеха, ни отделы. Ключ есть всегда,
    когда payload собран новым сборщиком: None означает «в срезе нет
    продажных направлений», и лист не строится (у деки так у Турции).
    Payload'ы без ключа (моки, проверочные фикстуры) — прежний
    direction_detail.
    """
    detail = (payload["sales_detail"] if "sales_detail" in payload
              else payload.get("direction_detail"))
    if not detail or not (detail.get("rows") or []):
        return []
    return [({"cc": variant.get("cc"), "detail": detail}, data)
            for variant, data in country_variants(payload)]


__all__ = ["BAR_KEYS", "CARD_BLOCKS", "CONT_SUFFIX", "DONUT_MAX", "PP_KEYS",
           "TABLE_KEYS", "bind_prod", "bind_sales", "bind_summary",
           "country_variants", "detail_variants", "section_variants"]
