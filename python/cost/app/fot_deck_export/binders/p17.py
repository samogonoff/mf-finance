"""Биндеры вкладки p17 «Численность ГК» → слайды деки 23–27.

Каждая функция заполняет клон прототипа живыми данными payload'а (см.
fot_deck_export.collect) — и ничего не знает ни про SQL, ни про цвета: цвета
УФ приходят из manifest.json (сняты с прототипов при сборке актива), числа и
токены стиля считает format.py (биндеры знак НЕ вычисляют — они только
подставляют готовый токен).

Раскладка таблиц повторяет деку, а не веб-сетку: страна на прототипе разбита
на две панели (17 и 16 строк), и заполнение ЖАДНОЕ — сначала первая панель до
своей ёмкости, остаток во вторую; что не влезло в страницу — слайд-продолжение
с тем же заголовком и пометкой «(продолжение)». Итог, примечание и номер
страницы печатаются только на последнем слайде пагинации.
"""

from __future__ import annotations

import logging

from app.fot_deck_export import format as fmt
from app.fot_deck_export import shapes
from app.fot_deck_export.errors import EmptySlice
from app.logship import log

#: Ключи карточек → номера KPI деки (порядок слайда: утвержд. / факт-1 / факт-2
#: / Δ к прошлому году / Δ к утверждённому).
KPI_KEYS = ("plan_approved", "fact_prev", "fact_curr", "delta_25", "delta_appr")

#: Ключи столбцов таблицы в порядке деки (7 колонок: подпись + 6 чисел).
COL_KEYS = ("label", "plan_approved", "q25", "q26", "staff_pct",
            "delta_25", "delta_appr")

#: Служебные ключи строки, которых нет в колонках деки: `total` помечает
#: строку итога в своде по странам, `staff_pct_color` — готовый цвет светофора
#: из сборщика дашборда (выгрузка красит по своим токенам, поле игнорируется).
ROW_META_KEYS = frozenset({"total", "staff_pct_color"})

#: Подпись среза факта в подзаголовке свода (полугодие → как в деке).
HL_LABELS = {0: "года", 1: "1 ПГ", 2: "2 ПГ"}

#: Разделитель «субъект — текст» в наблюдениях деки.
OBS_SEP = " — "

#: Пометка продолжения таблицы на следующем слайде.
CONT_SUFFIX = " (продолжение)"

#: Тексты-константы деки (заголовок блока наблюдений и полосы итога).
OBS_HEADER = "НАБЛЮДЕНИЯ ПО ГРУППЕ:"
TOTAL_LABEL = "Итого (видимые позиции):  "
TOTAL_ROW_LABEL = "Итого (видимые позиции)"


# ─── Общие хелперы разбора payload'а ─────────────────────────────────────────

def _meta(payload: dict) -> dict:
    return payload.get("meta") or {}


def _year_plan(payload: dict) -> int | None:
    """Год плана: из meta.periods, иначе из срезов запроса."""
    periods = _meta(payload).get("periods") or {}
    if periods.get("year_plan"):
        return int(periods["year_plan"])
    params = _meta(payload).get("params") or {}
    if params.get("year"):
        return int(params["year"])
    return None


def _half(payload: dict) -> int:
    """Полугодие среза: из meta.hc (честный срез факта), иначе из параметров."""
    hc = _meta(payload).get("hc") or {}
    if hc.get("half") is not None:
        return int(hc["half"])
    params = _meta(payload).get("params") or {}
    return int(params.get("half") or 2)


def _sign_color(token: str | None, sign: dict, light: bool = False):
    """Токен стиля format.py → цвет деки (HEX) или None (цвет не трогаем)."""
    if token is None or token == fmt.TOKEN_NEUTRAL:
        return None
    if token == fmt.TOKEN_NA:
        return sign.get("na_text")
    suffix = "_light" if light else ""
    if token == fmt.TOKEN_UP:
        return sign.get("delta_up" + suffix)
    if token == fmt.TOKEN_DOWN:
        return sign.get("delta_down" + suffix)
    raise RuntimeError("fot_deck: неизвестный токен стиля %r" % (token,))


def _header_lines(label, n_paras: int) -> list[str]:
    """Подпись шапки → абзацы прототипа («Утв. страт.» → «Утв.» / «страт.»).

    Дека ломает длинные подписи по словам на две строки, чтобы они влезли в
    узкую колонку: «Факт числ. 2 ПГ 2025» → «Факт числ.» / «2 ПГ 2025».
    Делим по словам «с конца»: первая строка короче, период/уточнение
    уезжает во вторую (в деке первая строка — всегда общее название).
    """
    text = str(label or "").strip()
    if n_paras <= 1:
        return [text]
    words = text.split()
    if len(words) <= 1:
        return [text] + [""] * (n_paras - 1)
    parts: list[str] = []
    rest = words
    for left in range(n_paras, 1, -1):
        take = max(1, len(rest) // left) if len(rest) > left else 1
        parts.append(" ".join(rest[:take]))
        rest = rest[take:]
    parts.append(" ".join(rest))
    return parts


def _cell_pair(key: str, value, cfg: dict, bands: dict | None = None):
    """Ячейка строки тела → (строки абзацев, токен стиля)."""
    text, token = fmt.cell(key, value, cfg, bands)
    return [text], token


def _check_row_keys(row: dict, where: str) -> None:
    """Набор ключей строки обязан совпасть с набором колонок деки (B5).

    Опечатка в ключе данных (`dleta_25` вместо `delta_25`) иначе дала бы тихий
    пропуск: ячейка получила бы «н/д», а строка выглядела бы нормальной. Такая
    ошибка должна падать с именем прототипа и списком лишних/недостающих
    ключей, а не уезжать в выгрузку.
    """
    got = set(row or {}) - ROW_META_KEYS
    extra = sorted(got - set(COL_KEYS))
    missing = sorted(set(COL_KEYS) - got)
    if not extra and not missing:
        return
    detail = []
    if missing:
        detail.append("нет ключей: %s" % ", ".join(missing))
    if extra:
        detail.append("лишние ключи: %s" % ", ".join(extra))
    raise RuntimeError("fot_deck: %s — строка не соответствует колонкам деки (%s)"
                       % (where, "; ".join(detail)))


def _row_cells(row: dict, cfg: dict, bands: dict | None = None) -> list[list[str]]:
    return [_cell_pair(key, row.get(key), cfg, bands)[0] for key in COL_KEYS]


def _row_signs(row: dict, sign: dict, cfg: dict, bands: dict | None = None,
               light: bool = False) -> list:
    """УФ по колонкам строки тела — цвета из токенов format.py.

    Токен считает format.py по ОКРУГЛЁННОМУ значению: −0.04 уже токен ``down``
    (зелёный), 99.96 — ``up`` (красный). «н/д» в колонках значений и
    «% укомпл.» остаётся базовым цветом строки, в колонках Δ — приглушённый
    серый деки (пустое отклонение не должно читаться как число светофора).
    """
    colors = []
    for key in COL_KEYS:
        _text, token = fmt.cell(key, row.get(key), cfg, bands)
        if token == fmt.TOKEN_NA and not key.startswith("delta"):
            colors.append(None)
            continue
        colors.append(_sign_color(token, sign, light))
    return colors


def _body_style(idx: int) -> str:
    """Чередование заливки строк тела: первая tinted, дальше зебра деки.

    Индекс — ВНУТРИ ТАБЛИЦЫ, потому что так раскрашена сама дека: и вторая
    панель страны, и таблица на слайде-продолжении начинаются с tinted
    (проверено по деке: слайд 24 — обе таблицы стартуют F8EDF0). «Сквозная»
    зебра через листы сделала бы выгрузку неотличимой от деки только на
    первом листе пагинации, поэтому приоритет у 1:1.
    """
    return "body_tinted" if idx % 2 == 0 else "body_plain"


def _header_spec(frame, columns: list[dict]) -> dict:
    """Строка-шапка: подписи колонок по абзацам-прототипам ячеек."""
    n_paras = _header_para_counts(frame, len(columns))
    return {"style": "header",
            "cells": [_header_lines(col.get("label"), n_paras[i])
                      for i, col in enumerate(columns)]}


def _header_para_counts(frame, n_cols: int) -> list[int]:
    """Число абзацев в ячейках шапки прототипа (дека ломает их на две строки)."""
    tbl = frame._element.find(".//" + shapes.A + "tbl")
    first_tr = tbl.find(shapes.A + "tr")
    if first_tr is None:
        raise RuntimeError("таблица %r: нет строки-шапки" % frame.name)
    return [len(tc.findall(shapes.A + "txBody/" + shapes.A + "p"))
            for tc in first_tr.findall(shapes.A + "tc")[:n_cols]]


# ─── Шапка слайда (общая часть пяти прототипов) ──────────────────────────────

def _fill_head(slide, subtitle: str, badge: str, cards: list[dict],
               cfg: dict) -> None:
    """Подзаголовок, бейдж и пять KPI — одинаково у свода и у стран."""
    shapes.set_run(shapes.require_shape(slide, "title"), 1, subtitle)
    shapes.set_run(shapes.require_shape(slide, "badge"), 0, badge)
    by_key = {c.get("key"): c for c in cards if c.get("key")}
    for num, key in enumerate(KPI_KEYS, 1):
        card = by_key.get(key) or {}
        # ярлык — верхним регистром, как в деке («ФАКТ ЧИСЛ., 1 ПГ 2025»):
        # серверный label пишется строчными, форма деки — капсом
        shapes.set_run(shapes.require_shape(slide, "kpi%d_label" % num), 0,
                       str(card.get("label") or "").upper())
        text, _token = fmt.kpi(key, card.get("value"), cfg)
        # KPI деки не красятся УФ: отклонения на карточках нейтральные
        shapes.set_run(shapes.require_shape(slide, "kpi%d_value" % num), 0, text)


def _fill_note(slide, text: str, cfg: dict) -> None:
    """Примечание слайда: «чел. …» — префикс единицы, как в деке.

    У KZ дека оставила в конце примечания хвостовой пробел — это её
    типографика, флаг приходит из манифеста (`format.note_tail_space`).
    Пустое примечание не даёт «чел. » с висящим пробелом.
    """
    body = str(text or "").strip()
    tail = " " if (cfg.get("note_tail_space") and body) else ""
    shapes.set_run(shapes.require_shape(slide, "note"), 0,
                   ("чел. " + body if body else "") + tail)


def _drop(slide, name: str) -> None:
    sh = shapes.find_shape(slide, name)
    if sh is not None:
        shapes.delete_shape(sh)


# ─── Свод по группе (слайд 23) ───────────────────────────────────────────────

def bind_summary(ctx, payload: dict, variant=None) -> None:
    """Слайд «Численность персонала — свод по группе» (прототип hc_summary)."""
    slide = ctx.slide(0)
    cfg = ctx.fmt
    year = _year_plan(payload)
    half = _half(payload)
    subtitle = "Численность персонала %s — свод по группе (на конец %s)" % (
        year if year else "", HL_LABELS.get(half, HL_LABELS[2]))
    _fill_head(slide, subtitle, "чел.", payload.get("cards") or [], cfg)

    table = next((t for t in payload.get("tables") or []
                  if t.get("key") == "hc_countries"), {}) or {}
    if not (table.get("rows") or []):
        # пустой срез (все страны отфильтрованы) — это не выгрузка «без
        # таблицы», а «данных нет»: сообщаем явно, иначе уехал бы пустой pptx
        raise EmptySlice(ctx.tab)
    _fill_country_table(ctx, slide, table)

    note = payload.get("p17_note") or table.get("note") or ""
    _fill_note(slide, note, cfg)

    # блок наблюдений служит только своду: у прочих прототипов шейпа нет, а
    # висящие obs/obs_bg на клонах стран — признак рассинхрона актива
    _bind_observations(ctx, slide, payload)


def _fill_country_table(ctx, slide, table: dict) -> None:
    """Таблица стран: шапка из колонок, строки с зеброй, итог тёмной строкой."""
    name = "tbl_countries"
    cfg = ctx.fmt
    frame = shapes.require_shape(slide, name)
    columns = table.get("columns") or []
    rows = table.get("rows") or []
    spec = [_header_spec(frame, columns)]
    body_idx = 0
    for row in rows:
        if row.get("total"):
            _check_row_keys(row, "%s/%s: строка итога" % (ctx.proto_name, name))
            spec.append({"style": "total",
                         "cells": _row_cells(row, cfg, ctx.pct_bands)})
            continue
        _check_row_keys(row, "%s/%s: строка тела" % (ctx.proto_name, name))
        spec.append({"style": _body_style(body_idx),
                     "cells": _row_cells(row, cfg, ctx.pct_bands),
                     "signs": _row_signs(row, ctx.sign, cfg, ctx.pct_bands)})
        body_idx += 1
    shapes.fill_table(frame, spec, ctx.table_protos(frame, name), ctx.sign,
                      proto_row_count=ctx.deck_rows(name))
    log(logging.DEBUG, "fot_deck: свод по странам",
        tab=ctx.tab, proto=ctx.proto_name, rows=len(rows),
        capacity=ctx.capacity(name))


def _bind_observations(ctx, slide, payload: dict) -> None:
    """Блок наблюдений: заголовок + буллеты; нет наблюдений — блок убираем."""
    items = payload.get("p17_observations") or payload.get("observations") or []
    items = [str(t).strip() for t in items if str(t or "").strip()]
    obs = shapes.find_shape(slide, "obs")
    if not items:
        # на живых данных блок пуст — текст и подложка удаляются
        _drop(slide, "obs")
        _drop(slide, "obs_bg")
        return
    if obs is None:
        raise RuntimeError("прототип %r: нет шейпа obs, а наблюдения пришли (%d)"
                           % (ctx.proto_name, len(items)))
    # заголовок деки закрыт переводом строки ВНУТРИ текста рана — на нём
    # держится пустая строка между заголовком и списком (см. set_text_raw)
    shapes.set_text_raw(obs, [(0, OBS_HEADER + "\n")])
    bullet = ctx.const("hc_summary", "obs_bullet")
    for i, item in enumerate(items):
        para_idx = 1 if i == 0 else shapes.clone_paragraph(obs, 1)
        text = item.rstrip("\n")
        if OBS_SEP in text:
            subject, rest = text.split(OBS_SEP, 1)
            rest = OBS_SEP + rest
        else:
            subject, _, tail = text.partition(" ")
            rest = (" " + tail) if tail else ""
        runs = obs.text_frame.paragraphs[para_idx].runs
        if len(runs) != 3:
            raise RuntimeError("obs: у абзаца %d ранов %d, ожидалось 3"
                               % (para_idx, len(runs)))
        # ран 0 — маркер списка (константа деки), 1 — субъект (жирный), 2 — текст.
        # Дека закрывает каждый абзац, кроме последнего, переводом строки ВНУТРИ
        # текста рана — на нём держится разрыв строк; пишем a:t напрямую, иначе
        # python-pptx экранирует «\n» в «_x000A_» и разрывы пропадают.
        tail_nl = "\n" if i < len(items) - 1 else ""
        shapes.set_text_raw(obs, [(0, bullet), (1, subject), (2, rest + tail_nl)],
                            para_idx=para_idx)


# ─── Страна (слайды 24–27) ───────────────────────────────────────────────────

def country_variants(cc: str):
    """Вариант слайда страны; нет данных по стране — слайда нет вовсе."""
    def variants(payload: dict) -> list[tuple[dict, dict]]:
        detail = (payload.get("country_details") or {}).get(cc)
        if not detail or not (detail.get("table") or {}).get("rows"):
            return []
        return [({"cc": cc}, detail)]
    return variants


def bind_country(ctx, detail: dict, variant=None) -> None:
    """Слайд страны: шапка, KPI, таблицы по подразделениям, итог, примечание."""
    cc = (variant or {}).get("cc") or ""
    proto = ctx.proto_name
    title = str(detail.get("title") or cc)
    table = detail.get("table") or {}
    columns = table.get("columns") or []
    rows = table.get("rows") or []
    panes = ctx.table_names(proto)
    caps = [ctx.capacity(name) for name in panes]
    pages = _paginate(rows, caps)

    for page_idx, chunk in enumerate(pages):
        slide = ctx.slide(page_idx)
        last = page_idx == len(pages) - 1
        _fill_head(slide, title + (CONT_SUFFIX if page_idx else ""), cc,
                   detail.get("cards") or [], ctx.fmt)
        # таблицы панелей: i-я панель получает свой кусок строк; итог — только
        # на последней странице (строка итога относится ко всей стране)
        for pane_idx, name in enumerate(panes):
            pane_rows = chunk[pane_idx] if pane_idx < len(chunk) else []
            pane_total = detail.get("total") if (last and ctx.is_total_row(name)) else None
            _fill_pane(ctx, slide, name, columns, pane_rows, total=pane_total)
        if last:
            _fill_total_bar(ctx, slide, detail)
            _fill_note(slide, detail.get("note") or "", ctx.fmt)
        else:
            # на продолжении полосы итога и примечания нет — они относятся ко
            # всей стране и печатаются один раз
            _drop(slide, "total_bar")
            _drop(slide, "total_bg")
            _drop(slide, "note")


def _paginate(rows: list[dict], capacities: list[int]) -> list[list[list[dict]]]:
    """Жадная раскладка строк: [[[строки панели A], [строки панели B]], …].

    Ёмкости панелей — из манифеста (дека сама разбита 17 + 16, а не пополам):
    первая панель заполняется до предела, остаток страницы — вторая панель,
    всё, что не влезло, уезжает на слайд-продолжение.
    """
    caps = [int(c) for c in capacities if c] or [len(rows) or 1]
    page_cap = sum(caps)
    pages: list[list[list[dict]]] = []
    for start in range(0, max(len(rows), 1), page_cap):
        chunk = rows[start:start + page_cap]
        panes: list[list[dict]] = []
        pos = 0
        for cap in caps:
            panes.append(chunk[pos:pos + cap])
            pos += cap
        pages.append(panes)
    return pages


def _fill_pane(ctx, slide, name: str, columns: list[dict], rows: list[dict],
               total: dict | None = None) -> None:
    """Заполняет одну таблицу-панель; пустую панель удаляет (не растягивает)."""
    frame = shapes.find_shape(slide, name)
    if frame is None:
        raise RuntimeError("прототип %r: на слайде нет таблицы %r"
                           % (ctx.proto_name, name))
    if not rows and total is None:
        # строк меньше ёмкости первой панели — вторую таблицу просто убираем,
        # геометрию первой не трогаем (решение по раскладке 663620)
        shapes.delete_shape(frame)
        return
    cfg = ctx.fmt
    protos = ctx.table_protos(frame, name)
    spec = [_header_spec(frame, columns)]
    if total is not None:
        # у KZ итог — ПЕРВАЯ строка таблицы (прототип total_top), не полоса
        spec.append({"style": "total_top",
                     "cells": _total_row_cells(total, cfg, ctx.pct_bands)})
    for offset, row in enumerate(rows):
        _check_row_keys(row, "%s/%s: строка тела" % (ctx.proto_name, name))
        spec.append({"style": _body_style(offset),
                     "cells": _row_cells(row, cfg, ctx.pct_bands),
                     "signs": _row_signs(row, ctx.sign, cfg, ctx.pct_bands)})
    shapes.fill_table(frame, spec, protos, ctx.sign,
                      proto_row_count=ctx.deck_rows(name))
    log(logging.DEBUG, "fot_deck: таблица страны заполнена",
        tab=ctx.tab, proto=ctx.proto_name, table=name, rows=len(rows),
        total=bool(total))


def _total_row_cells(total: dict, cfg: dict, bands: dict | None = None) -> list[list[str]]:
    cells = []
    for key in COL_KEYS:
        if key == "label":
            cells.append([TOTAL_ROW_LABEL])
            continue
        text, _token = fmt.total_value(key, total.get(key), cfg, bands)
        cells.append([text])
    return cells


def _fill_total_bar(ctx, slide, detail: dict) -> None:
    """Полоса «Итого» (24/25/27): пять ранов; цвета — позиционные из манифеста.

    Раскраска полосы в деке НЕ светофор: «Δ2025» красное и при +30,5, и при
    −49,2, «Δутвержд.» зелёное и при −153,9, и при +91,3 (проверено на всех
    трёх прототипах). Поэтому цвет берётся по номеру рана из manifest
    (`total_bar_colors`), а не вычисляется по знаку значения.
    """
    bar = shapes.find_shape(slide, "total_bar")
    if bar is None:
        return                      # у KZ итог живёт строкой таблицы
    total = detail.get("total") or {}
    texts = _total_bar_texts(total, detail.get("total_labels") or {},
                             ctx.fmt, ctx.pct_bands)
    shapes.set_text(bar, [(0, TOTAL_LABEL)] + list(enumerate(texts, 1)))
    colors = ctx.total_bar_colors
    if colors:
        for run_idx, color in enumerate(colors):
            if color:
                shapes.paint_run(bar, color, run_idx=run_idx)


def _total_bar_texts(total: dict, labels: dict, cfg: dict,
                     bands: dict | None = None) -> list[str]:
    """Четыре рана полосы — ровно как в деке (три пробела между блоками).

    run1: «Утвержд. 4 537,7   Факт 1П25 4 353,3   »
    run2: «Факт 1П26 4 383,8   % укомпл. 96,6%   »
    run3: «Δ2025 +30,5   »
    run4: «Δутвержд. −153,9»
    """
    def piece(key: str) -> str:
        text, _token = fmt.total_value(key, total.get(key), cfg, bands)
        return "%s %s" % (labels.get(key) or key, text)

    return [
        "%s   %s   " % (piece("plan_approved"), piece("q25")),
        "%s   %s   " % (piece("q26"), piece("staff_pct")),
        "%s   " % piece("delta_25"),
        piece("delta_appr"),
    ]


__all__ = ["COL_KEYS", "CONT_SUFFIX", "KPI_KEYS", "bind_country", "bind_summary",
           "country_variants"]
