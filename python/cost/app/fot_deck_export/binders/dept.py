"""Биндеры вкладок p15/p16 «Отделы» → слайды деки 7 и 8.

Обе вкладки — один и тот же срез отделов страны, показанный двумя листами:

  * p15 (лист 7, прототип dept_summary) — ТАБЛИЦА «отдел × периоды», шесть
    карточек «требующих внимания» и баннер «Тенденции и прогноз»;
  * p16 (лист 8, прототип dept_cards) — СЕТКА 4×4 карточек всех отделов и два
    баннера («Лучшая тенденция», «Второй приоритет»).

Срез один, поэтому оба биндера берут данные из одного payload'а, но листы
строятся по-разному и общего кода, кроме мелких помощников, не имеют.

Страны: дека показывает листы 7/8 для КАЖДОЙ страны присутствия (у деки — BY).
Вариант слайда задаёт страну (`variants` в реестре), данные приходят уже
отфильтрованными по ней — биндер страну не выбирает и не фильтрует.

Подписи (формулировки деки, разбивка на абзацы и раны) — из манифеста
(`labels`), баннеры — из `consts` + пары «имя — пояснение» из данных.
"""

from __future__ import annotations

import logging
import re

from app.fot_deck_export import format as fmt
from app.fot_deck_export import shapes
from app.logship import log

#: Колонки таблицы листа 7 (порядок деки, 9 колонок).
TABLE_KEYS = ("label", "f25_h1", "f25_h2", "f25", "b26_h1", "f26_h1", "b26_h2",
              "t26_h2", "exp26")

#: Колонки-проценты таблицы листа 7 (печатаются с «%»).
PCT_KEYS = frozenset({"b26_h1devprev", "b26_h2devprev"})

#: Роли карточки отдела листа 7: три блока «период → значение → отклонение»,
#: три круга «п.п.» с подписями и две строки численности.
CARD_PERIODS = ("p1", "p2", "p3")

#: Поля карточки отдела, которые пишутся во все три блока подряд.
PERIOD_KEYS = ("f26_h1", "t26_h2", "exp26")

#: Подписи периодов карточки: (метка, значение, отклонение, %-отклонение,
#: абсолютное отклонение) — по блокам p1/p2/p3 листа 7.
PERIOD_DEVS = (("dev_h1_pct", "dev_h1_abs"),
               ("dev_h2_pct", "dev_h2_abs"),
               ("dev_year_pct", "dev_year_abs"))

#: Круги «п.п.» карточки: ключ отклонения доли и подпись-шаблон.
PP_KEYS = ("pp_h1", "pp_h2", "pp_year")

#: Пометка продолжения (как у p17 и b1).
CONT_SUFFIX = " (продолжение)"

#: Ёмкость сетки карточки листа 8 (прототип = 4×4).
GRID_CELLS = 16


def _year_plan(payload: dict) -> int | None:
    periods = (payload.get("meta") or {}).get("periods") or {}
    if periods.get("year_plan"):
        return int(periods["year_plan"])
    params = (payload.get("meta") or {}).get("params") or {}
    if params.get("year"):
        return int(params["year"])
    return None


def _half_no(payload: dict) -> str:
    """Номер полугодия среза: у листов 7/8 факт — 1 ПГ (дека так подписана).

    Срез полугодия приходит в `meta.params.half` (0 — год), но листы отделов
    дека всегда строит по 1 ПГ: факт направления на середину года. Если фильтр
    просит 2 ПГ, номер уходит в подписи как есть — текст деки остаётся верным.
    """
    half = ((payload.get("meta") or {}).get("params") or {}).get("half")
    if half in (1, "1", 2, "2"):
        return str(half)
    return "1"


def _country(payload: dict, variant) -> tuple[str, str]:
    """(код, подпись) страны слайда: из варианта реестра, иначе из meta."""
    cc = (variant or {}).get("cc")
    slide_meta = (payload.get("meta") or {}).get("slide") or {}
    if not cc:
        cc = slide_meta.get("cc")
    label = slide_meta.get("label") or cc or ""
    return str(cc or ""), str(label)


def _sign_color(token: str | None, sign: dict) -> str | None:
    """Токен стиля → цвет деки; None — цвет не трогаем, ноль — не светофор."""
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


def _put_label(ctx, slide, name: str, *, year, prev=None, country=None,
               values=(), para_idx: int = 0) -> bool:
    """Пишет подпись шейпа по шаблону деки; нет шейпа/шаблона — False.

    Подписи ячеек сетки лежат в манифесте свёрнутыми по номеру места
    (`cardN_p1_label`): формулировка у всех ячеек одна, номер места — это
    геометрия, а не текст. Поэтому имя для манифеста сворачивается здесь, а
    шейп на слайде ищется по конкретному номеру.
    """
    sh = shapes.find_shape(slide, name)
    if sh is None:
        return False
    key = _label_name(ctx, name)
    if not ctx.has_label(key):
        return False
    if prev is None and year:
        prev = int(year) - 1
    paras = ctx.label(key, year=year, prev=prev, country=country, values=values)
    if para_idx >= len(paras):
        raise RuntimeError("fot_deck: подпись %r короче, чем ожидает биндер "
                           "(абзацев %d, нужен %d)"
                           % (key, len(paras), para_idx + 1))
    shapes.set_text(sh, list(enumerate(paras[para_idx])))
    return True


def _value_slots(ctx, key: str) -> list[int]:
    """Номера прорезей значений ``{vN}`` в подписи (для повторной подстановки)."""
    meta = ctx._engine.manifest["prototypes"][ctx.proto_name]  # noqa: SLF001
    text = "".join(r["text"] for p in (meta.get("labels") or {}).get(key) or []
                   for r in p["runs"])
    return sorted({int(n[1:]) for n in fmt.slot_names(text) if n[1:].isdigit()})


def _label_key(name: str) -> str:
    """Имя подписи в манифесте: ``card7_p1_label`` → ``cardN_p1_label``."""
    import re as _re
    return _re.sub(r"^card\d+_", "cardN_", name)


def _label_name(ctx, name: str) -> str:
    """Ключ подписи в манифесте: точное имя ячейки, иначе свёрнутое по месту.

    Свёртывание (`cardN_…`) возможно не всегда: подпись, которая у ячеек
    РАЗНАЯ («+9,1% к факту 25 (+606)» — значение своей ячейки), остаётся в
    манифесте под конкретным номером места. Поэтому сначала ищется точное имя
    и только потом свёрнутое — иначе такая подпись считалась бы отсутствующей,
    и биндер собрал бы текст сам, потеряв формулировку деки.
    """
    if ctx.has_label(name):
        return name
    return _label_key(name)


def _money(value, cfg: dict) -> str:
    """Денежное значение листа 7/8: целые тыс., БЕЗ плюса (это сумма).

    Плюс в деке означает отклонение, а не значение: ФОТ направления печатается
    «2 860», а динамика — «+16,0%». Поэтому формат один, а флаг разный.
    """
    text, _token = fmt.money(value, group=bool(cfg.get("cell_group")),
                             digits=int(cfg.get("cell_digits") or 0),
                             plus=False)
    return text


# ─── Лист 7: таблица отделов, карточки внимания, баннер ─────────────────────

def bind_summary(ctx, payload: dict, variant=None) -> None:
    """Лист 7: «Отделы: свод по подразделениям» (прототип dept_summary)."""
    depts = payload.get("cards_by_department") or []
    table = next((t for t in payload.get("tables") or [] if t.get("rows")), None)
    if not depts or not table:
        # нет отделов в срезе — это отсутствие данных, а не пустой лист
        from app.fot_deck_export.errors import EmptySlice
        raise EmptySlice("p15: в срезе нет отделов — выгрузка листа невозможна")
    year = _year_plan(payload)
    prev = (year - 1) if year else None
    cc, label = _country(payload, variant)
    caps = ctx.capacity(ctx.table_names()[0]) if ctx.table_names() else 0
    rows = table.get("rows") or []
    # ИТОГ стоит в деке ПЕРВОЙ строкой (прототип total_top) и в ёмкость тела
    # не входит: иначе 22 отдела + итог не влезали в 22 места, и лист рвался
    # на продолжение с одной строкой
    total = next((r for r in rows if r.get("total")), None)
    body = [r for r in rows if not r.get("total")]
    pages = _paginate(body, caps)

    for page_idx, chunk in enumerate(pages):
        slide = ctx.slide(page_idx)
        last = page_idx == len(pages) - 1
        _fill_head(ctx, slide, label, cc, payload, year, prev,
                   suffix=CONT_SUFFIX if page_idx else "")
        _fill_dept_table(ctx, slide, table, chunk, year, prev,
                         total=total if page_idx == 0 else None)
        if last:
            _fill_attention_head(ctx, slide, year, prev)
            _fill_unit_note(ctx, slide)
            _fill_attention(ctx, slide, depts, year, prev)
            _fill_trend_banner(ctx, slide, payload)
        else:
            # карточки внимания и баннер относятся ко всему листу: на
            # продолжении их нет (как полосы итога у p17)
            _drop_cards(ctx, slide)
            _drop_banner(slide, ("banner", "banner_bg", "cards_head", "unit_note"))
    log(logging.INFO, "fot_deck: лист 7 собран", tab=ctx.tab, country=cc,
        pages=len(pages), rows=len(rows))


def _paginate(rows: list[dict], capacity: int) -> list[list[dict]]:
    """Раскладка строк таблицы по страницам ёмкости прототипа."""
    cap = int(capacity or len(rows) or 1)
    return [rows[start:start + cap] for start in range(0, max(len(rows), 1), cap)]


def _fill_head(ctx, slide, label: str, cc: str, payload: dict, year, prev,
               suffix: str = "") -> None:
    """Шапка листов 7/8: заголовок страны, бейдж, строка численности."""
    _fill_title(ctx, slide, label, suffix,
                len(payload.get("cards_by_department") or []), year, prev)
    _fill_badge(ctx, slide, cc)
    _fill_hc_note(ctx, slide, payload.get("headcount") or {}, year, prev,
                  _half_no(payload))


def _fill_title(ctx, slide, label: str, suffix: str, depts: int = 0,
                year=None, prev=None) -> None:
    """Подзаголовок листа: бренд остаётся декиным, страна — из среза.

    У заголовка ДВА рана: «ГК «МАРК ФОРМЭЛЬ»  » и подпись блока, в которой
    страна стоит прорезью ``{country}``. Первый ран не трогаем — это бренд
    актива, второй собираем по шаблону и дописываем пометку продолжения.
    """
    title = shapes.require_shape(slide, "title")
    consts = ctx._engine.manifest["prototypes"][ctx.proto_name].get("consts") or {}  # noqa: SLF001
    lead = consts.get("title_lead")
    if lead and len(title.text_frame.paragraphs[0].runs) >= 2:
        shapes.set_run(title, 0, lead)
    if not ctx.has_label("title"):
        raise RuntimeError("fot_deck: у прототипа %r нет подписи title — "
                           "пересоберите актив" % ctx.proto_name)
    # отбивку между числом и словом держит ШАБЛОН деки («свод по {v1}
    # подразделениям»): сборка актива сохраняет пробел за прорезью, поэтому
    # дописывать его кодом значило бы получить двойной пробел
    paras = ctx.label("title", country=label, year=year, prev=prev,
                      values=(str(depts),))
    subtitle = "".join(paras[0]) + suffix
    # в шаблоне бренд вынесен в consts, поэтому подпись — это первый (и
    # единственный) ран шаблона; на слайде она идёт ВТОРЫМ раном после бренда
    if len(title.text_frame.paragraphs[0].runs) >= 2:
        shapes.set_run(title, 1, subtitle)
    else:
        shapes.set_run(title, 0, subtitle)


def _fill_badge(ctx, slide, cc: str) -> None:
    """Бейдж листа 7/8: код страны в первом абзаце, блок — во втором.

    У дека бейдж двухстрочный («BY» / «ОТДЕЛЫ»), поэтому второй абзац пишется
    явно: иначе в выгрузке остался бы плейсхолдер актива.
    """
    badge = shapes.require_shape(slide, "badge")
    shapes.set_run(badge, 0, str(cc or ""))
    # вторая строка бейджа («ОТДЕЛЫ») — константа деки: она не зависит ни от
    # среза, ни от страны, поэтому лежит в consts и остаётся в активе
    tf = badge.text_frame
    if len(tf.paragraphs) > 1:
        # вторая строка бейджа — блок деки («ОТДЕЛЫ»): текст остался в активе
        # как константа, но токен всё равно снимаем — его никто не заполнит
        block = _const_or_none(ctx, "badge_block") or ""
        if block:
            shapes.set_text(badge, [(0, block)], para_idx=1)
        elif tf.paragraphs[1].runs:
            shapes.set_text(badge, [(0, "")], para_idx=1)


def _const_or_none(ctx, key: str) -> str | None:
    """Константа деки из манифеста прототипа (None — её там нет)."""
    meta = ctx._engine.manifest["prototypes"][ctx.proto_name]  # noqa: SLF001
    return (meta.get("consts") or {}).get(key)


def _fill_hc_note(ctx, slide, hc: dict, year, prev, half_no: str = "1") -> None:
    """Строка «Числ. направления (ПЧ/ФЧ 1ПГ26): 821,2/792,3, 96% укомпл.»."""
    plan, _t = fmt.money(hc.get("plan_approved"), group=False, digits=1, plus=False)
    fact, _t = fmt.money(hc.get("fact_2026"), group=False, digits=1, plus=False)
    pct_text, _t = fmt.pct(hc.get("staffing"), 0, ctx.pct_bands)
    # значение — ЧИСЛО: «%» стоит в шаблоне деки за прорезью («{v3}% укомпл.»)
    _put_label(ctx, slide, "hc_note", year=year, prev=prev,
               values=(plan, fact, pct_text.rstrip("%")))


def _fill_dept_table(ctx, slide, table: dict, rows: list[dict],
                     year=None, prev=None, total: dict | None = None) -> None:
    """Таблица «отдел × периоды»: шапка из манифеста, итог — первой строкой."""
    name = ctx.table_names()[0] if ctx.table_names() else "tbl_depts"
    frame = shapes.find_shape(slide, name)
    if frame is None:
        raise RuntimeError("прототип %r: на слайде нет таблицы %r"
                           % (ctx.proto_name, name))
    keys = TABLE_KEYS
    spec = [{"style": "header",
             "cells": ctx.header_labels(name, year=year, prev=prev)}]
    if total is not None:
        # итог — ПЕРВОЙ строкой после шапки, как в деке (прототип total_top)
        spec.append({"style": "total_top",
                     "cells": [_cell(ctx, key, total) for key in keys]})
    body_idx = 0
    for row in rows:
        cells = [_cell(ctx, key, row) for key in keys]
        if row.get("total"):
            continue
        spec.append({"style": "body_tinted" if body_idx % 2 == 0 else "body_plain",
                     "cells": cells})
        body_idx += 1
    shapes.fill_table(frame, spec, ctx.table_protos(frame, name), ctx.sign,
                      proto_row_count=ctx.deck_rows(name))


def _cell(ctx, key: str, row: dict) -> list[str]:
    """Ячейка таблицы листа 7: подпись либо целое тыс. BYN."""
    if key == "label":
        return [str(row.get("label") or "")]
    if key in PCT_KEYS:
        # знак «%» есть в подписи колонки деки — ячейка несёт только число
        text, _t = fmt.signed(row.get(key), digits=1, group=False)
        return [text]
    return [_money(row.get(key), ctx.fmt)]


def _fill_attention(ctx, slide, depts: list[dict], year, prev) -> None:
    """Шесть карточек «требующих внимания»: по рангу `attn_rank` из данных.

    Дека отбирает отделы по правилам сборщика (`attention` + `attn_rank`), а
    не по порядку строк таблицы: карточки идут от худшего к лучшему, и биндер
    порядок не пересчитывает — только берёт готовый.
    """
    used = _attention_list(depts, ctx.proto_name)
    count = ctx.card_count()
    for i in range(1, count + 1):
        if i <= len(used):
            _annotate_card(ctx, slide, i, used[i - 1], year, prev)
        else:
            _drop_card(slide, i)


def _fill_attention_head(ctx, slide, year, prev) -> None:
    """Подзаголовок блока «требующих внимания» — формулировка деки с годом."""
    _put_label(ctx, slide, "cards_head", year=year, prev=prev)


def _fill_unit_note(ctx, slide) -> None:
    """Сноска под таблицей: единица среза и оговорка о методике.

    Текст сноски принадлежит деке (в нём есть и число «в пределах ~0,2%», и
    описание источника), поэтому берётся из шаблона как есть: биндер не
    сочиняет методику, а печатает ту, что заявлена в деке.
    """
    sh = shapes.find_shape(slide, "unit_note")
    if sh is None:
        return
    key = "unit_note"
    if not ctx.has_label(key):
        # подпись без данных: текст остаётся в активе
        return
    paras = ctx.label(key, values=("0,2",))
    shapes.set_text(sh, list(enumerate(paras[0])))


def _attention_list(depts: list[dict], proto: str) -> list[dict]:
    """Отделы для сетки: лист 7 — только «требующие внимания», лист 8 — ВСЕ.

    Лист 8 деки («анализ по подразделениям») печатает КАЖДЫЙ отдел среза
    сеткой 4×4: у деки там 16 карточек при 16 отделах, включая те, что на
    листе 7 попали в блок внимания. Отбрасывать `attention`-отделы нельзя —
    они пропадали из выгрузки вовсе (проверено на срезе с attention=1/17:
    на листе оставалось 11 карточек вместо 17).
    """
    if proto == "dept_summary":
        attention = [d for d in depts if d.get("attention")]
        attention.sort(key=lambda d: d.get("attn_rank") or 10**6)
        return attention
    return list(depts)


def _fill_trend_banner(ctx, slide, payload: dict) -> None:
    """Баннер «Тенденции и прогноз»: заголовок деки + текст из данных."""
    banner = shapes.find_shape(slide, "banner")
    if banner is None:
        return
    trends = payload.get("dept_trends") or []
    if not trends:
        _drop_banner(slide, ("banner", "banner_bg"))
        return
    lead = ctx.const("dept_summary", "banner_lead")
    shapes.set_text(banner, [(0, lead), (1, " ".join(str(t) for t in trends))])


def _annotate_card(ctx, slide, idx: int, dept: dict, year, prev) -> None:
    """Заполняет карточку отдела N (лист 7): три периода, три круга, численность."""
    prefix = "card%d_" % idx
    cfg = ctx.fmt

    title = shapes.find_shape(slide, prefix + "title")
    if title is not None:
        shapes.set_run(title, 0, str(dept.get("label") or ""))

    for pk, (period, value_key, (pct_key, abs_key)) in enumerate(
            zip(CARD_PERIODS, PERIOD_KEYS, PERIOD_DEVS)):
        _put_label(ctx, slide, prefix + period + "_label", year=year, prev=prev,
                   values=(_half_for(pk),))
        # отбивку перед единицей держит ШАБЛОН деки («{v1} тыс. BYN» — пробел
        # уже стоит после прорези): дописывать её кодом значило бы получить
        # двойной пробел на слайде
        _put_label(ctx, slide, prefix + period + "_value", year=year, prev=prev,
                   values=(_money(dept.get(value_key), cfg),))
        _put_dept_dev(ctx, slide, prefix + period + "_dev", dept, pct_key,
                      abs_key, year, prev)
        _put_pp(ctx, slide, prefix, pk + 1, dept.get(PP_KEYS[pk]), year, prev)

    _fill_card_hc(ctx, slide, prefix, dept, year, prev)


def _half_for(idx: int) -> str:
    """Номер полугодия блока: у первого блока — 1, у прочих — 2."""
    return "1" if idx == 0 else "2"


def _put_dept_dev(ctx, slide, name: str, dept: dict, pct_key: str,
                  abs_key: str, year, prev) -> None:
    """Строка отклонения блока карточки: «+9,1% к факту 25 (+606)».

    Знак «%» стоит в шаблоне деки сразу за прорезью («{v1}% к факту {p2}»),
    поэтому значение пишется числом: держать пунктуацию подписи в биндере
    значило бы разъехаться с декой на первой же её правке.
    """
    pct_text, token = fmt.signed(dept.get(pct_key), digits=1, group=False)
    # абсолютное отклонение дека печатает СО ЗНАКОМ («(+606)», «(−16)»): это
    # отклонение, а не сумма, поэтому плюс здесь обязателен
    abs_text, _t = fmt.money(dept.get(abs_key), group=False,
                             digits=int(ctx.fmt.get("cell_digits") or 0))
    if token == fmt.TOKEN_NA:
        # базы 2025 нет → процента в строке нет («н/д факт25 (+198)»). Декa
        # красит ТАКИЕ строки зелёным всегда, независимо от знака абс.
        # отклонения: проверено по дампу слайдов 7/8 — все восемь строк без
        # базы (ОУиР ЧНП, Отдел ОТ, новое направление, Группа аудита…)
        # напечатаны `1C8A5A`, хотя абс. отклонение у них положительное.
        # Правило воспроизводим как есть (1:1), вопрос заказчику — в отчёте.
        token = fmt.TOKEN_DOWN
    sh = shapes.find_shape(slide, name)
    if sh is None:
        return
    if _put_label(ctx, slide, name, year=year, prev=prev,
                  values=(pct_text, abs_text)):
        color = _sign_color(token, ctx.sign)
        if color:
            shapes.paint_run(sh, color, run_idx=0)
        return
    # у листа 8 подпись отклонения короче и без периода («+13,5% факт25 (+71)»)
    # и в манифест не попала (нет ни года, ни прорезей). Тогда собираем текст
    # по ОСТАТКАМ актива: берём его раны, подставляя значения и сохраняя
    # декины «%», «к факту», «(…)» — формулировку не сочиняем
    _fill_dev_raw(sh, pct_text, abs_text)


def _drop_second_para_tokens(shape, unit: str) -> None:
    """Второй абзац-единица («п.п.») — константа деки, а не плейсхолдер.

    Актив хранит круг «п.п.» двумя абзацами: значение и единицу. Единица не
    зависит от данных, но при сборке актива абзацев у шейпа несколько, и
    единица тоже стала токеном. Здесь она возвращается к декиному тексту,
    иначе в выгрузке остался бы «{{…_p1_r0}}».
    """
    tf = shape.text_frame
    if len(tf.paragraphs) < 2:
        return
    para = tf.paragraphs[1]
    if not para.runs:
        return
    shapes.set_text(shape, [(0, unit)], para_idx=1)


def _fill_dev_raw(shape, pct_text: str, abs_text: str) -> None:
    """Строка отклонения без шаблона: значения по ранам, знаки — декины.

    В активе такая подпись выглядит как один ран с плейсхолдером (шаблон не
    снят), поэтому восстановить формулировку из него нельзя. Пишем оба
    значения в принятом декой виде «<Δ%>% факт<год> (<Δ>)»: процент со знаком
    и абсолют — данные, а связка между ними — типографика деки, повторённая
    здесь один раз для листа с сокращённой подписью.
    """
    shapes.set_run(shape, 0, "%s%% (%s)" % (pct_text.rstrip("%"), abs_text))


def _put_pp(ctx, slide, prefix: str, num: int, value, year, prev) -> None:
    """Круг «п.п.» N: значение по шаблону, обводка — по знаку отклонения."""
    text, token = fmt.pp(value, int(ctx.fmt.get("pp_digits") or 2),
                         int(ctx.fmt.get("pp_zero_digits") or 2))
    text_sh = shapes.find_shape(slide, "%spp%d_text" % (prefix, num))
    if text_sh is not None:
        # значение круга — данные: первый абзац число, второй — единица «п.п.»
        # (единица остаётся из актива, как и у KPI-ленты)
        shapes.set_run(text_sh, 0, text)
        # единица круга — ВТОРОЙ АБЗАЦ без ведущего пробела: у карточек листов
        # 7/8 дека держит «п.п.» отдельным абзацем ровно встык, и лишний
        # пробел сдвигал бы строку на ползнака
        _drop_second_para_tokens(text_sh, "п.п.")
        # значение красится по знаку (как в деке: зелёный при снижении доли,
        # красный при росте), а единица «п.п.» у деки ВСЕГДА серая — иначе
        # цвет рана значения утёк бы на единицу
        color = _sign_color(token, ctx.sign)
        if color:
            shapes.paint_run(text_sh, color, run_idx=0)
            # единица — ВТОРОЙ абзац того же шейпа: без para_idx покраска
            # легла бы снова на значение и затёрла его цвет
            if len(text_sh.text_frame.paragraphs) > 1:
                shapes.paint_run(text_sh, ctx.sign.get("na_text"), para_idx=1,
                                 run_idx=0)
    circle = shapes.find_shape(slide, "%spp%d_circle" % (prefix, num))
    if circle is not None:
        color = _pp_color(token, ctx.sign)
        if color:
            shapes.paint_outline(circle, color)
    # подпись круга зависит от периода блока: у 1ПГ/2ПГ — «к факту 1П25» и
    # «к факту 2П25», у года — «Год/факт25». Номер полугодия здесь СТРУКТУРНЫЙ
    # (он есть в шаблоне текстом), поэтому values не передаются
    _put_label(ctx, slide, "%spp%d_cap" % (prefix, num), year=year, prev=prev)


def _fill_hc_line_raw(ctx, shape, plan: str, fact: str, pct: str) -> None:
    """Строка ПЧ/ФЧ без подписи в манифесте: значения по ранам актива.

    У листа 8 подпись «ПЧ/ФЧ: <пара>, <укомпл>%» не содержит ни года, ни
    периода, поэтому в манифест не попала — её форму целиком задаёт актив.
    Тогда биндер пишет три рана (пара и укомпл.), не сочиняя свою подпись:
    текст «ПЧ/ФЧ:» и «, » остаётся декиным.
    """
    shapes.set_run(shape, 0, "ПЧ/ФЧ: %s/%s, %s%%" % (plan, fact, pct))
    # остальные раны актива пусты: их текст уже вошёл в первый ран выше
    for idx in range(1, len(shape.text_frame.paragraphs[0].runs)):
        shapes.set_run(shape, idx, "")


def _trim_zero(text: str) -> str:
    """Срезает нулевую дробную часть: «91,0» → «91» (так пишет дека).

    Пара ПЧ/ФЧ — единственное место листов 7/8, где значение печатается без
    выравнивания по знакам: «372.4/348.9, 94%», но «94.5/91, 96%». Отдельная
    функция, потому что `fmt.money` знаки задаёт прототипом и срезать их там
    значило бы менять форматы всех остальных ячеек.
    """
    return re.sub(r"([.,])0+$", "", text or "")


def _fill_card_hc(ctx, slide, prefix: str, dept: dict, year, prev) -> None:
    """Строка численности карточки: «ПЧ/ФЧ 1ПГ 2026: 372.4/348.9, 94% укомпл.»."""
    plan, _t = fmt.money(dept.get("hc_plan"), group=False, digits=1, plus=False)
    fact, _t = fmt.money(dept.get("hc_fact"), group=False, digits=1, plus=False)
    # пара ПЧ/ФЧ печатается СРЕЗАННОЙ («94.5/91», а не «94.5/91.0»): дека
    # пишет целые без дробной части, дробные — с одним знаком. Ноль после
    # запятой срезается, значащий знак остаётся
    plan, fact = _trim_zero(plan), _trim_zero(fact)
    if ctx.fmt.get("hc_dot"):
        # листы 7/8 пишут пару ПЧ/ФЧ ТОЧКОЙ («372.4/348.9») — типографика деки
        plan, fact = plan.replace(",", "."), fact.replace(",", ".")
    pct_text, _t = fmt.pct(dept.get("hc_staffing"), 0, ctx.pct_bands)
    line = shapes.find_shape(slide, prefix + "hc_line")
    if line is not None:
        key = _label_name(ctx, prefix + "hc_line")
        if ctx.has_label(key):
            _put_label(ctx, slide, prefix + "hc_line", year=year, prev=prev,
                       values=(plan, fact, pct_text.rstrip("%")))
        else:
            # у листа 8 подпись короче и без периода («ПЧ/ФЧ: 11/10, 91%»):
            # форму задаёт декa, поэтому берём её остатки из актива, подставив
            # только значения — первая пара ранов и есть шаблон ряда
            _fill_hc_line_raw(ctx, line, plan, fact, pct_text.rstrip("%"))
    dev = dept.get("hc_dev")
    if dev is None:
        # отклонения нет (новая карточка без базы) — токен снимаем, иначе он
        # уедет на слайд; сама строка остаётся, но пустой
        dev_sh = shapes.find_shape(slide, prefix + "hc_dev")
        if dev_sh is not None:
            # чистим ВСЕ раны подписи: их может быть два (метка периода и
            # значение), и пустой первый оставил бы токен во втором
            for idx in range(len(dev_sh.text_frame.paragraphs[0].runs)):
                shapes.set_run(dev_sh, idx, "")
        return
    dev_text, token = fmt.signed(dev, digits=1, group=False)
    # прорезей значений в подписи может быть больше одной (у листа 8 маркер
    # периода вырезан структурой, у листа 7 он защищён — состав разный),
    # поэтому значение передаётся на КАЖДУЮ прорезь
    n = 1
    if _put_label(ctx, slide, prefix + "hc_dev", year=year, prev=prev,
                  values=(dev_text,) * max(n, 1)):
        sh = shapes.find_shape(slide, prefix + "hc_dev")
        color = _sign_color(token, ctx.sign)
        if sh is not None and color:
            shapes.paint_run(sh, color, run_idx=1)


# ─── Лист 8: сетка карточек всех отделов и два баннера ──────────────────────

def bind_cards(ctx, payload: dict, variant=None) -> None:
    """Лист 8: «Отделы: анализ по подразделениям» (прототип dept_cards)."""
    depts = payload.get("cards_by_department") or []
    if not depts:
        from app.fot_deck_export.errors import EmptySlice
        raise EmptySlice("p16: в срезе нет отделов — выгрузка листа невозможна")
    year = _year_plan(payload)
    prev = (year - 1) if year else None
    cc, label = _country(payload, variant)
    ordered = _attention_list(depts, ctx.proto_name)
    pages = [ordered[i:i + GRID_CELLS]
             for i in range(0, len(ordered), GRID_CELLS)] or [[]]

    for page_idx, chunk in enumerate(pages):
        slide = ctx.slide(page_idx)
        last = page_idx == len(pages) - 1
        _fill_head(ctx, slide, label, cc, payload, year, prev,
                   suffix=CONT_SUFFIX if page_idx else "")
        for i in range(1, GRID_CELLS + 1):
            if i <= len(chunk):
                _fill_grid_card(ctx, slide, i, chunk[i - 1], year, prev)
            else:
                # карточек меньше 16 — ЛИШНИЕ ЯЧЕЙКИ УДАЛЯЮТСЯ, остальные не
                # сдвигаются: дека раскладывает отделы по местам сетки
                _drop_card(slide, i)
        if last:
            banners = payload.get("dept_banners") or {}
            _fill_banner(ctx, slide, "good", banners.get("good"), year, prev)
            _fill_banner(ctx, slide, "warn", banners.get("warn"), year, prev)
        else:
            _drop_banner(slide, ("banner_good", "banner_good_bg",
                                 "banner_warn", "banner_warn_bg"))
    log(logging.INFO, "fot_deck: лист 8 собран", tab=ctx.tab, country=cc,
        pages=len(pages), depts=len(ordered))


def _fill_grid_card(ctx, slide, idx: int, dept: dict, year, prev) -> None:
    """Карточка сетки листа 8: три периода, численность, три круга «п.п.»."""
    prefix = "card%d_" % idx
    cfg = ctx.fmt
    title = shapes.find_shape(slide, prefix + "title")
    if title is not None:
        shapes.set_run(title, 0, str(dept.get("label") or ""))

    for pk, (period, value_key, (pct_key, abs_key)) in enumerate(
            zip(CARD_PERIODS, PERIOD_KEYS, PERIOD_DEVS)):
        _put_label(ctx, slide, prefix + period + "_label", year=year, prev=prev,
                   values=(_half_for(pk),))
        # отбивку перед единицей держит ШАБЛОН деки («{v1} тыс. BYN» — пробел
        # уже стоит после прорези): дописывать её кодом значило бы получить
        # двойной пробел на слайде
        _put_label(ctx, slide, prefix + period + "_value", year=year, prev=prev,
                   values=(_money(dept.get(value_key), cfg),))
        _put_dept_dev(ctx, slide, prefix + period + "_dev", dept, pct_key,
                      abs_key, year, prev)
    _fill_card_hc(ctx, slide, prefix, dept, year, prev)
    for num, key in enumerate(PP_KEYS, 1):
        _put_pp(ctx, slide, prefix, num, dept.get(key), year, prev)


def _fill_banner(ctx, slide, kind: str, banner: dict | None, year, prev) -> None:
    """Баннер листа 8: заголовок и хвост деки + пары «имя — пояснение» из данных.

    Пары переменные (в деке 7 и 5, в данных бывает меньше), поэтому в активе
    остался ОДИН образец пары: остальные клонируются, rPr берётся с образца.
    Хвост-вывод — константа деки, но в нём есть номер полугодия, поэтому
    подстановка идёт через шаблон `labels`, а не из consts как есть.
    """
    name = "banner_%s" % kind
    shape = shapes.find_shape(slide, name)
    if shape is None:
        return
    if not banner or not (banner.get("items") or []):
        _drop_banner(slide, (name, name + "_bg"))
        return
    lead = ctx.const(ctx.proto_name, name + "_lead")
    shapes.set_text(shape, [(0, lead)])
    items = banner.get("items") or []
    tail = _banner_tail(ctx, kind, year, prev)
    _fill_pairs(shape, items, tail)


def _banner_tail(ctx, kind: str, year, prev) -> str:
    """Хвост-вывод баннера: из consts, с подстановкой полугодий шаблоном."""
    meta = ctx._engine.manifest["prototypes"][ctx.proto_name]  # noqa: SLF001
    consts = meta.get("consts") or {}
    tail = consts.get("banner_%s_tail" % kind)
    if not tail:
        return ""
    slots = (meta.get("labels") or {}).get("banner_%s_tail" % kind)
    if slots:
        paras = ctx.label("banner_%s_tail" % kind, year=year, prev=prev)
        return "".join(paras[0])
    return tail


def _fill_pairs(shape, items: list[dict], tail: str) -> None:
    """Список пар «имя — (пояснение), » во втором абзаце баннера.

    Образец пары — первые два рана актива (имя и пояснение). На каждую
    следующую пару клонируются оба рана, тексты пишутся в клоны; хвост-вывод
    дописывается в последний ран последней пары (дека держит его там же).
    """
    tf = shape.text_frame
    if len(tf.paragraphs) < 2:
        raise RuntimeError("баннер %r: в активе нет абзаца списка пар"
                           % shape.name)
    para = tf.paragraphs[1]
    runs = para.runs
    if len(runs) != 2:
        raise RuntimeError("баннер %r: у образца пары %d ранов, ожидалось 2"
                           % (shape.name, len(runs)))
    name_run, note_run = runs[0], runs[1]
    for i, item in enumerate(items):
        if i == 0:
            targets = (0, 1)
        else:
            targets = (shapes.clone_run(shape, 1, 0),
                       shapes.clone_run(shape, 1, 1))
        pairs = [(targets[0], str(item.get("name") or "")),
                 (targets[1], str(item.get("note") or ""))]
        for run_idx, text in pairs:
            shapes.set_text(shape, [(run_idx, text)], para_idx=1)
    if tail:
        # дека держит вывод ОТДЕЛЬНЫМ РАНОМ с тем же rPr, что у пояснения:
        # при склейке с пояснением последней пары текст выглядит так же, но
        # ран несёт ещё и «).» пояснения, и строка уезжает по переносам
        idx = shapes.clone_run(shape, 1, 1)
        shapes.set_text(shape, [(idx, tail)], para_idx=1)


# ─── Общие удаления ─────────────────────────────────────────────────────────

def _drop_card(slide, idx: int) -> None:
    """Удаляет все шейпы карточки N по префиксу имени."""
    prefix = "card%d_" % idx
    for sh in list(slide.shapes):
        if sh.name.startswith(prefix):
            shapes.delete_shape(sh)


def _drop_cards(ctx, slide) -> None:
    """Убирает сетку карточек целиком (на продолжении листа 7)."""
    meta = ctx._engine.manifest["prototypes"][ctx.proto_name]  # noqa: SLF001
    count = int(meta.get("card_count") or 0)
    for i in range(1, count + 1):
        _drop_card(slide, i)


def _drop_banner(slide, names) -> None:
    """Удаляет баннер вместе с подложкой (нет данных — нет блока)."""
    for name in names:
        sh = shapes.find_shape(slide, name)
        if sh is not None:
            shapes.delete_shape(sh)


__all__ = ["CARD_PERIODS", "CONT_SUFFIX", "GRID_CELLS", "PERIOD_KEYS", "PP_KEYS",
           "TABLE_KEYS", "bind_cards", "bind_summary"]


#: Порядок стран деки: карточки/листы идут BY → RU → KZ → UZ.
COUNTRY_ORDER = ("BY", "RU", "KZ", "UZ")


def country_variants(payload: dict) -> list[tuple[dict, None]]:
    """Варианты листов 7/8: по слайду на КАЖДУЮ страну среза.

    Дека показывает эти листы для страны (у неё — BY). Если сборщик отдал
    `meta.slide.cc` (срез уже сужен фильтром страны) — вариант один. Если cc
    пуст (срез группы), вариантов столько, сколько стран пришло в данных: лист
    «Отделы» дека печатает постранично, по одному на страну, а не сводом.
    Страны без единого отдела варианта не дают — пустой лист не печатается.
    Данные варианта — тот же payload: сборщик отфильтрован по стране, и биндер
    берёт страну из `meta.slide` (её выставляет `filters['countries']`).
    """
    depts = payload.get("cards_by_department") or []
    if not depts:
        return []
    slide = (payload.get("meta") or {}).get("slide") or {}
    cc = slide.get("cc")
    if cc:
        return [({"cc": cc}, payload)]
    present = []
    for d in depts:
        code = d.get("cc")
        if code and code not in present:
            present.append(code)
    present.sort(key=lambda c: (COUNTRY_ORDER.index(c)
                                if c in COUNTRY_ORDER else len(COUNTRY_ORDER)))
    return [({"cc": code}, payload) for code in present]


__all__ += ["country_variants"]
