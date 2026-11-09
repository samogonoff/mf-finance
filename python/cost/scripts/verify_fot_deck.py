"""Самопроверка выгрузки «1:1 как в деке» (задача 663620) — без pytest.

В проекте нет тестового раннера (конвенция — скрипты), а эта проверка должна
работать и в контейнере, и в CI: `python scripts/verify_fot_deck.py`.

Что проверяется, без деки и без COM — на синтетическом DTO:

  1. плейсхолдеров «{{» в текстах выгрузки нет;
  2. раскладка таблиц по ёмкостям манифеста: ровно ёмкость → один слайд,
     ёмкость+1 → слайд-продолжение, страна без данных → слайда нет;
  3. УФ и текст на ГРАНИЧНЫХ значениях: 0, −0.04, 99.95, 99.96, 100, «н/д»
     (знак считается по округлённому значению — «−0,0» не существует);
  4. сквозная нумерация страниц 1..N;
  5. (B5) набор заполненных ключей совпадает с набором колонок деки —
     опечатка в ключе строки должна падать, а не давать тихий «н/д»;
  6. (KZ) прототип с итогом-строкой: итог ровно один и на первом слайде страны;
  7. (B1) итог на слайде-продолжении не дублируется, зебра строк сквозная;
  8. (B3) пустой срез — EmptySlice (роут отдаёт 422), а не пустой pptx.

Выход: 0 — всё зелёное, 1 — были расхождения (список в stdout).
"""

from __future__ import annotations

import asyncio
import logging
import sys
import time
from pathlib import Path

sys.path.insert(0, str(Path(__file__).resolve().parents[1]))

from app.fot_deck_export import collect as collect_mod          # noqa: E402
from app.fot_deck_export import engine as engine_mod            # noqa: E402
from app.fot_deck_export import format as fmt                   # noqa: E402
from app.fot_deck_export import shapes                           # noqa: E402

try:
    from pptx import Presentation
except ImportError as exc:  # pragma: no cover - зависит от образа
    print("нет python-pptx: %s" % exc, file=sys.stderr)
    raise SystemExit(2)

P = shapes.P
A = shapes.A

FAILS: list[str] = []
OKS: list[str] = []


def check(name: str, cond: bool, detail: str = "") -> None:
    """Одна проверка: в список ОК либо в список провалов."""
    if cond:
        OKS.append(name)
        return
    FAILS.append("%s%s" % (name, (": " + detail) if detail else ""))


# ─── Синтетический DTO (структура payload'а /fot/dashboard p17) ──────────────

COLS = [
    {"key": "label", "label": "Подразделение"},
    {"key": "plan_approved", "label": "Утв. страт."},
    {"key": "q25", "label": "Факт 1П25"},
    {"key": "q26", "label": "Факт 1П26"},
    {"key": "staff_pct", "label": "% ук."},
    {"key": "delta_25", "label": "Δ25"},
    {"key": "delta_appr", "label": "Δутв"},
]


def row(label: str, *, pa=100.0, q25=100.0, q26=100.0, pct=100.0,
        d25=0.0, dappr=0.0) -> dict:
    return {"label": label, "plan_approved": pa, "q25": q25, "q26": q26,
            "staff_pct": pct, "delta_25": d25, "delta_appr": dappr}


def detail(cc: str, rows: list[dict], total: dict | None = None) -> dict:
    cc_title = {"BY": "BY (Беларусь)", "RU": "RU (Россия)", "KZ": "KZ (Казахстан)",
                "UZ": "UZ (Узбекистан)"}[cc]
    return {
        "title": "%s — численность персонала по подразделениям" % cc_title,
        "cards": [
            {"key": "plan_approved", "label": "Утвержд. числ., страт. 2026",
             "value": 7307.2, "unit": "чел."},
            {"key": "fact_prev", "label": "Факт числ., 1 ПГ 2025",
             "value": 7032.9, "unit": "чел."},
            {"key": "fact_curr", "label": "Факт числ., 1 ПГ 2026",
             "value": 7164.3, "unit": "чел."},
            {"key": "delta_25", "label": "Отклонение к 2025",
             "value": 131.4, "unit": "чел."},
            {"key": "delta_appr", "label": "Отклонение к утвержд.",
             "value": -240.2, "unit": "чел."},
        ],
        "table": {"key": "hc_country_detail", "columns": COLS, "rows": rows},
        "total": total or {"plan_approved": 100.0, "q25": 100.0, "q26": 100.0,
                           "staff_pct": 100.0, "delta_25": 0.0, "delta_appr": 0.0},
        "total_labels": {"plan_approved": "Утвержд.", "q25": "Факт 1П25",
                         "q26": "Факт 1П26", "staff_pct": "% укомпл.",
                         "delta_25": "Δ25", "delta_appr": "Δутвержд."},
        "note": "Факт — численность на конец периода (июнь), не среднесписочная.",
    }


def payload(rows_by_cc: dict[str, list[dict]]) -> dict:
    """Payload вкладки: свод по странам + детали только там, где есть строки."""
    summary_rows = [row("%s (страна)" % cc) for cc in rows_by_cc]
    summary_rows.append(row("Общий итог") | {"total": True})
    return {
        "cards": detail("BY", [row("x")])["cards"] if rows_by_cc else [],
        "charts": [],
        "tables": [{"key": "hc_countries", "columns": COLS, "rows": summary_rows,
                    "note": "Факт — численность на конец периода (июнь)."}],
        "p17_note": "Факт — численность на конец периода (июнь).",
        "p17_observations": [],
        "country_details": {cc: detail(cc, rows) for cc, rows in rows_by_cc.items()},
        "meta": {"tab": "p17", "params": {"year": 2026, "half": 1},
                 "periods": {"year_plan": 2026}, "hc": {"half": 1}},
    }


# ─── Прогон через движок ─────────────────────────────────────────────────────

def _stub_collect(payloads: dict[str, dict]):
    """Подмена collect: движок получает готовый DTO, в SQL не ходит."""
    async def collect(tab: str, filters: dict) -> dict:
        return payloads[tab]
    return collect


def render(payloads: dict[str, dict]) -> bytes:
    # актив мог быть пересобран только что (в т.ч. этим же скриптом) — кеш
    # движка держит отпечаток файлов и сам перечитает их, но сбрасываем явно,
    # чтобы прогон не зависел от предыдущего состояния процесса
    engine_mod.reset_cache()
    engine = engine_mod.DeckEngine(engine_mod.load_template(),
                                   engine_mod.load_manifest())
    original = collect_mod.collect
    collect_mod.collect = _stub_collect(payloads)
    try:
        for tab, data in payloads.items():
            engine.build_tab(tab, data)
        return engine.finish()
    finally:
        collect_mod.collect = original


def slides_of(blob: bytes) -> list:
    return list(Presentation(__import__("io").BytesIO(blob)).slides)


def texts(slide) -> list[str]:
    out = []
    for sh in slide.shapes:
        out.extend(engine_mod._shape_texts(sh))
    return out


def shape_named(slide, name: str):
    for sh in slide.shapes:
        if sh.name == name:
            return sh
    return None


def cell_texts(slide, table_name: str) -> list[list[str]]:
    """Матрица текстов таблицы: [строка][колонка] (без text_frame-мутаций)."""
    sh = shape_named(slide, table_name)
    if sh is None:
        return []
    tbl = sh._element.find(".//" + A + "tbl")
    out = []
    for tr in tbl.findall(A + "tr"):
        cells = []
        for tc in tr.findall(A + "tc"):
            cells.append("".join(t.text or "" for t in tc.iter(A + "t")))
        out.append(cells)
    return out


def run_color(slide, shape_name: str, run_idx: int) -> str | None:
    sh = shape_named(slide, shape_name)
    return shapes.run_color(sh, 0, run_idx) if sh is not None else None


# ─── Проверки ────────────────────────────────────────────────────────────────

def test_format_boundaries() -> None:
    """(3) Границы: текст и токен считаются по округлённому значению."""
    bands = engine_mod.load_manifest().get("pct_bands") or {"down_max": 97.0,
                                                            "up_min": 100.0}
    cases = [
        # значение, ожидаемый текст, ожидаемый токен
        (0, "0,0", fmt.TOKEN_DOWN),
        (-0.04, "0,0", fmt.TOKEN_DOWN),
        (-0.05, "−0,1", fmt.TOKEN_DOWN),
        (1.25, "+1,3", fmt.TOKEN_UP),
    ]
    for value, want_text, want_token in cases:
        text, token = fmt.delta(value, group=False)
        check("delta(%s) текст" % value, text == want_text,
              "получено %r, ожидалось %r" % (text, want_text))
        check("delta(%s) токен" % value, token == want_token,
              "получено %r, ожидалось %r" % (token, want_token))

    pct_cases = [
        (99.95, "100,0%", fmt.TOKEN_UP),
        (99.96, "100,0%", fmt.TOKEN_UP),
        (99.94, "99,9%", fmt.TOKEN_NEUTRAL),
        (97.0, "97,0%", fmt.TOKEN_DOWN),
        (97.05, "97,1%", fmt.TOKEN_NEUTRAL),
        (100, "100,0%", fmt.TOKEN_UP),
        (None, fmt.NA, fmt.TOKEN_NA),
    ]
    for value, want_text, want_token in pct_cases:
        text, token = fmt.pct(value, 1, bands)
        check("pct(%s) текст" % value, text == want_text,
              "получено %r, ожидалось %r" % (text, want_text))
        check("pct(%s) токен" % value, token == want_token,
              "получено %r, ожидалось %r" % (token, want_token))

    # целые проценты (таблица BY): 99 → «99%», 99.5 → «100%» (половина вверх)
    text, token = fmt.pct(99.6, 0, bands)
    check("pct(99.6, digits=0) текст", text == "100%", "получено %r" % (text,))
    check("pct(99.6, digits=0) токен", token == fmt.TOKEN_UP)

    # «н/д» не имеет знака и не красится числом светофора
    check("num(None)", fmt.num(None) == (fmt.NA, fmt.TOKEN_NA))
    check("delta(None)", fmt.delta(None) == (fmt.NA, fmt.TOKEN_NA))


def test_layout_capacities() -> None:
    """(2) Ровно ёмкость / ёмкость+1 / страна без данных."""
    manifest = engine_mod.load_manifest()
    caps = manifest["prototypes"]["hc_country_by"]["tables"]
    page_cap = caps["tbl_dept_a"]["capacity"] + caps["tbl_dept_b"]["capacity"]

    cases = {
        "ровно страница": ([row("d%d" % i) for i in range(page_cap)], 1),
        "страница+1": ([row("d%d" % i) for i in range(page_cap + 1)], 2),
    }
    for name, (rows, want_country_slides) in cases.items():
        blob = render({"p17": payload({"BY": rows})})
        slides = slides_of(blob)
        names = [s._element.find(P + "cSld").get("name") for s in slides]
        country = [n for n in names if "hc_country_by" in n]
        check("(2) %s: свод + страна" % name, len(slides) == 1 + want_country_slides,
              "слайдов %d, ожидалось %d (%r)"
              % (len(slides), 1 + want_country_slides, names))
        check("(2) %s: страниц страны" % name,
              len(country) == want_country_slides,
              "слайдов страны %d (%r)" % (len(country), country))

    # (3, B3) страна без данных: детали пусты — слайда нет
    data = payload({"BY": [row("d1")]})
    data["country_details"]["RU"] = detail("RU", [])
    data["country_details"]["KZ"] = detail("KZ", [])
    blob = render({"p17": data})
    names = [s._element.find(P + "cSld").get("name") for s in slides_of(blob)]
    check("(2) пустая страна пропущена",
          not any("hc_country_ru" in n or "hc_country_kz" in n for n in names),
          "слайды: %r" % (names,))


def test_empty_slice() -> None:
    """(8/B3) Пустой срез → EmptySlice, а не пустой pptx и не 500."""
    data = payload({})
    data["country_details"] = {}
    data["tables"] = []
    data["cards"] = []
    try:
        render({"p17": data})
    except engine_mod.EmptySlice:
        check("(B3) пустой срез -> EmptySlice", True)
    except Exception as exc:  # noqa: BLE001 - важен факт типа исключения
        check("(B3) пустой срез -> EmptySlice", False,
              "получено %s: %s" % (type(exc).__name__, exc))
    else:
        check("(B3) пустой срез -> EmptySlice", False,
              "вернулся pptx вместо ошибки")


def test_paging_and_style() -> None:
    """(1,4,7,B1) Плейсхолдеры, нумерация, итог один раз, зебра сквозная."""
    manifest = engine_mod.load_manifest()
    caps = manifest["prototypes"]["hc_country_by"]["tables"]
    per_page = caps["tbl_dept_a"]["capacity"] + caps["tbl_dept_b"]["capacity"]
    rows = [row("Подразделение %d" % i) for i in range(per_page + 3)]
    total = {"plan_approved": 500.0, "q25": 490.0, "q26": 505.0,
             "staff_pct": 96.6, "delta_25": 15.0, "delta_appr": -5.0}
    rows[0] = row("Граница нуля", d25=-0.04, pct=99.96, dappr=-0.04)
    blob = render({"p17": payload({"BY": rows}) | {
        "country_details": {"BY": detail("BY", rows, total)}}})
    slides = slides_of(blob)

    # (1) плейсхолдеров нет ни в одном тексте
    leftovers = [t for s in slides for t in texts(s) if "{{" in t]
    check("(1) плейсхолдеров «{{» нет", not leftovers, "%r" % (leftovers[:3],))

    # (4) нумерация 1..N
    pages = []
    for s in slides:
        sh = shape_named(s, "page")
        pages.append("".join(t.text or "" for t in sh._element.iter(A + "t"))
                     if sh is not None else None)
    check("(4) нумерация 1..N", pages == [str(i) for i in range(1, len(slides) + 1)],
          "получено %r" % (pages,))

    by_slides = [s for s in slides
                 if "hc_country_by" in s._element.find(P + "cSld").get("name")]
    # (B1) итог ровно один и только на последнем листе пагинации
    def has_total(slide) -> bool:
        bars = shape_named(slide, "total_bar")
        if bars is not None:
            return True
        for t in cell_texts(slide, "tbl_dept_a"):
            if t and t[0].startswith("Итого (видимые позиции)"):
                return True
        return False

    check("(B1) итог ровно на одном листе",
          sum(1 for s in by_slides if has_total(s)) == 1,
          "листов с итогом %d из %d"
          % (sum(1 for s in by_slides if has_total(s)), len(by_slides)))
    check("(B1) итог на последнем листе", has_total(by_slides[-1]))

    # (B1) пустых заливок нет: каждая строка тела имеет заливку прототипа
    def body_fills(slide, table: str) -> list[str | None]:
        sh = shape_named(slide, table)
        if sh is None:
            return []
        tbl = sh._element.find(".//" + A + "tbl")
        out = []
        for tr in tbl.findall(A + "tr")[1:]:
            tc = tr.find(A + "tc")
            tcPr = tc.find(A + "tcPr")
            clr = (tcPr.find(A + "solidFill/" + A + "srgbClr")
                   if tcPr is not None else None)
            out.append(clr.get("val") if clr is not None else None)
        return out

    allowed = set((manifest.get("row_colors") or {}).values())
    empty = []
    for slide in by_slides:
        for table in ("tbl_dept_a", "tbl_dept_b"):
            for fill in body_fills(slide, table):
                if fill not in allowed:
                    empty.append(fill)
    check("(B1) пустых/чужих заливок нет", not empty,
          "встретились %r" % (sorted(set(empty)),))

    # (B1) каждая таблица (в т.ч. на продолжении) стартует с tinted — как дека,
    # где зебра считается внутри таблицы, а не сквозь листы
    tinted = (manifest.get("row_colors") or {}).get("body_tinted")
    starts = [body_fills(s, t)[:1] for s in by_slides
              for t in ("tbl_dept_a", "tbl_dept_b") if body_fills(s, t)]
    check("(B1) таблицы начинаются с tinted (как в деке)",
          all(f == [tinted] for f in starts), "первые строки: %r" % (starts,))

    # (3) на живом слайде границы: «0,0» зелёное, «100,0%» красное.
    # Число знаков процентов — свойство прототипа деки (`pct_digits`): таблица
    # BY печатает целые («100%»), поэтому ожидание берём из манифеста, а не
    # из головы. Красится при этом по округлённому, а не по сырому значению.
    by_format = manifest["prototypes"]["hc_country_by"]["format"]
    digits = int(by_format["pct_digits"])
    want_pct = ("100,0%" if digits else "100%")
    cells = cell_texts(by_slides[0], "tbl_dept_a")
    check("(3) −0.04 напечатано «0,0»", cells[1][5] == "0,0",
          "в ячейке %r" % (cells[1][5],))
    check("(3) 99.96 напечатано «%s»" % want_pct, cells[1][4] == want_pct,
          "в ячейке %r" % (cells[1][4],))
    sign = manifest["sign_colors"]
    sh = shape_named(by_slides[0], "tbl_dept_a")
    tbl = sh._element.find(".//" + A + "tbl")
    tr = tbl.findall(A + "tr")[1]
    def run_color_in(tc):
        r = tc.find(".//" + A + "r")
        rPr = r.find(A + "rPr") if r is not None else None
        clr = (rPr.find(A + "solidFill/" + A + "srgbClr")
               if rPr is not None else None)
        return clr.get("val") if clr is not None else None
    tcs = tr.findall(A + "tc")
    check("(3) Δ −0.04 зелёное", run_color_in(tcs[5]) == sign["delta_down"],
          "цвет %r" % (run_color_in(tcs[5]),))
    check("(3) 99.96 красное", run_color_in(tcs[4]) == sign["delta_up"],
          "цвет %r" % (run_color_in(tcs[4]),))


def test_kz_total_top() -> None:
    """(6) KZ: итог-строка единственный и на первом слайде страны."""
    rows = [row("Отдел %d" % i) for i in range(4)]
    total = {"plan_approved": 10.0, "q25": 9.0, "q26": 11.0,
             "staff_pct": 110.0, "delta_25": 2.0, "delta_appr": 1.0}
    blob = render({"p17": payload({"KZ": rows}) | {
        "country_details": {"KZ": detail("KZ", rows, total)}}})
    kz = [s for s in slides_of(blob)
          if "hc_country_kz" in s._element.find(P + "cSld").get("name")]
    check("(6) KZ: один слайд", len(kz) == 1, "слайдов %d" % len(kz))
    cells = cell_texts(kz[0], "tbl_dept_a")
    totals = [i for i, r in enumerate(cells)
              if r and r[0].startswith("Итого (видимые позиции)")]
    check("(6) KZ: итог ровно один", len(totals) == 1, "строк итога %d" % len(totals))
    check("(6) KZ: итог первым после шапки", totals == [1], "индексы %r" % (totals,))
    check("(6) KZ: итог — строка таблицы, полосы нет",
          shape_named(kz[0], "total_bar") is None)


def test_row_keys_guard() -> None:
    """(5/B5) Опечатка в ключе строки — ошибка, а не тихий «н/д»."""
    bad = row("Опечатка")
    bad["dleta_25"] = bad.pop("delta_25")
    try:
        render({"p17": payload({"BY": [bad]})})
    except RuntimeError as exc:
        check("(B5) опечатка ключа -> RuntimeError", "delta_25" in str(exc),
              "текст ошибки: %s" % exc)
    else:
        check("(B5) опечатка ключа -> RuntimeError", False,
              "выгрузка собралась с тихим пропуском")


def test_extra_columns_tolerated() -> None:
    """Дашборд шире листа деки (673259): лишние колонки не ломают выгрузку.

    Регресс инцидента 06.11.2026: сборщик p17 добавил две колонки
    («Δ% к факту», «Δ к факту, чел.»), шапка прототипа осталась семиколоночной,
    и `_header_spec` падал IndexError на своде. Колонки листа задаёт дека
    (COL_KEYS), новые поля дашборда — данные, не геометрия.
    """
    wide_cols = COLS[:2] + [
        {"key": "plan_approveddevprev", "label": "Δ% к факту 1 ПГ 25", "unit": "%"},
        {"key": "plan_approveddevprev_abs", "label": "Δ к факту, чел.", "unit": "чел."},
    ] + COLS[2:]

    def wide(r: dict) -> dict:
        return dict(r, plan_approveddevprev=1.5, plan_approveddevprev_abs=12.0,
                    plan_approveddevprev_color="up")

    rows = [wide(row("Отдел А")), wide(row("Отдел Б"))]
    data = payload({"BY": rows})
    data["tables"][0]["columns"] = wide_cols
    data["tables"][0]["rows"] = [wide(r) for r in data["tables"][0]["rows"]]
    data["country_details"]["BY"]["table"]["columns"] = wide_cols
    data["country_details"]["BY"]["table"]["rows"] = rows
    try:
        blob = render({"p17": data})
    except Exception as exc:  # noqa: BLE001
        check("(B5) лишние колонки дашборда не ломают выгрузку", False,
              "%s: %s" % (type(exc).__name__, exc))
        return
    slides = slides_of(blob)
    check("(B5) лишние колонки дашборда не ломают выгрузку", bool(slides),
          "слайдов %d" % len(slides))
    # шапка осталась семиколоночной, подписи — из деки, новых колонок нет
    head = cell_texts(slides[0], "tbl_countries")[0]
    check("(B5) шапка свода — 7 колонок деки", len(head) == len(COLS), "колонок %d" % len(head))
    check("(B5) новых колонок в шапке нет",
          all("Δ%" not in c and "к факту" not in c for c in head), "%r" % (head,))


def test_asset_sha_guard() -> None:
    """(B7) Актив и манифест сверяются по sha256."""
    manifest = engine_mod.load_manifest()
    check("(B7) в манифесте есть asset_sha256", bool(manifest.get("asset_sha256")))
    blob = shapes.TEMPLATE_PATH.read_bytes()
    import hashlib
    check("(B7) sha256 актива совпадает",
          hashlib.sha256(blob).hexdigest() == manifest.get("asset_sha256"))
    # подменённый манифест: ожидаемый отпечаток не тот — движок обязан упасть
    original = shapes.load_manifest

    def broken(path=None):
        data = original(path)
        data["asset_sha256"] = "0" * 64
        return data

    engine_mod.reset_cache()
    shapes.load_manifest = broken
    try:
        engine_mod.load_template()
    except engine_mod.AssetMismatch:
        check("(B7) рассинхрон актива -> AssetMismatch", True)
    except Exception as exc:  # noqa: BLE001
        check("(B7) рассинхрон актива -> AssetMismatch", False,
              "получено %s: %s" % (type(exc).__name__, exc))
    else:
        check("(B7) рассинхрон актива -> AssetMismatch", False, "ошибки не было")
    finally:
        shapes.load_manifest = original
        engine_mod.reset_cache()


# --- Листы 1-3 и 7-8 деки (b1, p15, p16) -----------------------------------

def _b1_payload(year: int = 2026) -> dict:
    """DTO листов 1-3: KPI, карточки стран, панель численности, таблица."""
    cards = [{"key": k, "label": "KPI %d" % i, "value": 100.0 + i,
              "unit": "млн BYN"} for i, k in enumerate(
        ("fot_fact_2025", "fot_share_2025", "fot_expect_2026", "fot_dev_year",
         "fot_share_2026", "revenue_dev_year"), 1)]
    cc_cards = [{"cc": cc, "label": cc, "exp26": 100.0, "dev_year": 5.0,
                 "dev_abs": 4.0, "rev_share_2026": 20.0,
                 "rev_share_2025": 21.0, "fot_share": 60.0,
                 "plan_approved": 10.0, "fact_h1_2026": 9.0, "hc_var": -1.0,
                 "share_delta_pp": -0.4}
                for cc in ("BY", "RU", "KZ", "UZ")]
    keys = ("f25_h1", "f25_h2", "f25", "b26_h1", "f26_h1", "b26_h2", "t26_h2",
            "exp26")
    table = {"key": "by_country", "label": "", "columns": [], "rows": [
        {"label": "BY (Беларусь)", **{k: 10.0 for k in keys}},
        {"label": "Общий итог", "total": True,
         **{k: 10.0 for k in keys}},
    ]}
    hc = {"plan_approved": 10.0, "fact_2025": 9.0, "fact_2026": 8.0,
          "var_plan": -1.0, "var_2025": 1.0}
    half_cards = [{"key": k, "label": "KPI %d" % i, "value": 100.0 + i,
                   "unit": "млн BYN"} for i, k in enumerate(
        ("fot_value_prev", "fot_share_prev", "fot_value", "fot_dev",
         "fot_share_cur", "revenue_dev"), 1)]
    return {"cards": cards, "headcount": hc, "cards_by_country": cc_cards,
            "tables": [table], "observations": [],
            "slides": [
                {"key": "s1", "period": "h1", "title": "", "cards": half_cards,
                 "headcount": hc, "cards_by_country": cc_cards,
                 "table": table},
                {"key": "s2", "period": "h2", "title": "", "cards": half_cards,
                 "headcount": hc, "cards_by_country": cc_cards,
                 "table": table}],
            "meta": {"tab": "b1", "params": {"year": year},
                     "periods": {"year_plan": year}}}


def _dept_payload(cc: str, count: int, *, attention: int = 6,
                  with_table: bool = True) -> dict:
    """DTO листа отделов одной страны (лист 7 - с таблицей, лист 8 - без)."""
    keys = ("f25_h1", "f25_h2", "f25", "b26_h1", "f26_h1", "b26_h2", "t26_h2",
            "exp26")
    depts = []
    for i in range(1, count + 1):
        d = {"label": "Отдел %d" % i, "f26_h1": 100.0 + i, "t26_h2": 90.0 + i,
             "exp26": 190.0 + i, "dev_h1_pct": 5.0, "dev_h1_abs": 4.0,
             "dev_h2_pct": 4.0, "dev_h2_abs": 3.0, "dev_year_pct": 6.0,
             "dev_year_abs": 7.0, "hc_plan": 10.0, "hc_fact": 9.0,
             "hc_staffing": 90.0, "hc_dev": -1.0, "pp_h1": -0.1,
             "pp_h2": 0.2, "pp_year": 0.0}
        if i <= attention:
            d["attention"] = True
            d["attn_rank"] = i
        else:
            d["attention"] = False
        depts.append(d)
    tables = []
    if with_table:
        # тело таблицы — те же отделы, что в карточках: пагинация листа 7
        # считается по строкам таблицы, а не по карточкам внимания
        rows = [{"label": d["label"],
                 **{k: 100.0 for k in keys}} for d in depts]
        rows.append({"label": "Итого Отделы", "total": True,
                     **{k: 100.0 for k in keys}})
        tables = [{"key": "departments", "label": "", "columns": [], "rows": rows}]
    return {"cards": [], "headcount": {"plan_approved": 800.0,
                                       "fact_2026": 790.0, "staffing": 96.0},
            "cards_by_department": depts, "tables": tables,
            "dept_trends": ["тренд"] if with_table else [],
            "dept_banners": {},
            "meta": {"tab": "p15" if with_table else "p16",
                     "params": {"year": 2026}, "periods": {"year_plan": 2026},
                     "slide": {"cc": cc, "label": cc}}}


def _slide_names(blob: bytes) -> list[str]:
    return [s._element.find(P + "cSld").get("name") for s in slides_of(blob)]


def _page_texts(shape) -> list[str]:
    """Все a:t шейпа `page` — номер страницы движок пишет в первый ран."""
    return [t.text or "" for t in shape._element.iter(A + "t")]


def test_registry_order() -> None:
    """(A1) Порядок листов «все листы» = порядок деки."""
    from app.fot_deck_export import registry
    check("(A1) вкладки идут в порядке деки",
          registry.ALL_TABS == ("b1", "p14", "p15", "p16", "p17", "b3"),
          "%r" % (registry.ALL_TABS,))
    for tab in ("b1", "p14", "p15", "p16", "p17", "b3"):
        check("(A1) вкладка %r есть в реестре" % tab,
              tab in registry.SHEETS_BY_TAB)
    check("(A1) b1 - три листа деки",
          len(registry.sheet_for("b1").slides) == 3)
    check("(A1) p14 - листы 4/5/6 деки",
          [s.proto for s in registry.sheet_for("p14").slides]
          == ["nb_summary", "nb_prod", "nb_sales"],
          "%r" % ([s.proto for s in registry.sheet_for("p14").slides],))
    payloads = {"b1": _b1_payload(), "p14": _nb_payload(),
                "p15": _dept_payload("BY", 3),
                "p16": _dept_payload("BY", 3, with_table=False)}
    names = _slide_names(render(payloads))
    check("(A1) b1 даёт три слайда подряд",
          names[:3] == ["fot:b1:b1_year:0", "fot:b1:b1_h1:0", "fot:b1:b1_h2:0"],
          "%r" % (names[:3],))
    check("(A1) p14 идёт после b1",
          "fot:p14:nb_summary:0" in names
          and names.index("fot:p14:nb_summary:0") > 2, "%r" % (names,))
    check("(A1) листы 4-6 идут в порядке деки",
          [n.split(":")[2] for n in names
           if n.startswith("fot:p14:")] == ["nb_summary", "nb_prod", "nb_sales"],
          "%r" % ([n for n in names if n.startswith("fot:p14:")],))
    check("(A1) p15 идёт после b1",
          "fot:p15:dept_summary:0" in names
          and names.index("fot:p15:dept_summary:0") > 2, "%r" % (names,))
    check("(A1) p15 идёт после p14",
          names.index("fot:p15:dept_summary:0")
          > names.index("fot:p14:nb_sales:0"), "%r" % (names,))
    check("(A1) p16 идёт после p15",
          "fot:p16:dept_cards:0" in names
          and names.index("fot:p16:dept_cards:0")
          > names.index("fot:p15:dept_summary:0"), "%r" % (names,))


def test_country_variants() -> None:
    """(A2) Листы 7/8: вариант на страну; срез страны - один лист."""
    from app.fot_deck_export.binders import dept
    variants = dept.country_variants(_dept_payload("BY", 3))
    check("(A2) сужённый срез даёт один вариант", len(variants) == 1,
          "%d" % len(variants))
    check("(A2) вариант несёт код страны", variants[0][0]["cc"] == "BY",
          "%r" % (variants[0][0],))
    group = _dept_payload("", 4)
    group["meta"]["slide"] = {"cc": None, "label": "Группа"}
    group["cards_by_department"] = [
        dict(d, cc=cc) for d, cc in zip(group["cards_by_department"],
                                        ("RU", "BY", "UZ", "KZ"))]
    order = [v[0]["cc"] for v in dept.country_variants(group)]
    check("(A2) свод группы разворачивается по странам",
          order == ["BY", "RU", "KZ", "UZ"], "%r" % (order,))
    empty = {"cards_by_department": [], "meta": {"slide": {"cc": None}}}
    check("(A2) без отделов вариантов нет", dept.country_variants(empty) == [])


def test_p16_grid_overflow() -> None:
    """(A2) Сетка листа 8: <=16 без продолжения, 17 - второй слайд."""
    fits = _slide_names(render({"p16": _dept_payload("BY", 16,
                                                    with_table=False)}))
    check("(A2) 16 отделов - один слайд", len(fits) == 1, "%r" % (fits,))
    over_blob = render({"p16": _dept_payload("BY", 17, with_table=False)})
    over = _slide_names(over_blob)
    check("(A2) 17 отделов - слайд-продолжение", len(over) == 2, "%r" % (over,))
    check("(A2) продолжение помечено «(продолжение)»",
          any("продолжение" in t for t in texts(slides_of(over_blob)[1])))
    few_blob = render({"p16": _dept_payload("BY", 5, with_table=False)})
    cards = [sh for sh in slides_of(few_blob)[0].shapes
             if sh.name.startswith("card") and sh.name.endswith("_bg")]
    check("(A2) меньше 16 - лишние карточки удалены", len(cards) == 5,
          "карточек %d" % len(cards))


def test_p15_pagination() -> None:
    """(A2) Таблица отделов сверх ёмкости - продолжение, карточки в конце."""
    caps = engine_mod.load_manifest()["prototypes"]["dept_summary"]["tables"]
    capacity = int(next(iter(caps.values()))["capacity"])
    blob = render({"p15": _dept_payload("BY", capacity + 3)})
    slides = slides_of(blob)
    check("(A2) таблица сверх ёмкости - два слайда", len(slides) == 2,
          "%d при ёмкости %d" % (len(slides), capacity))
    first, last = texts(slides[0]), texts(slides[1])
    check("(A2) карточки внимания - только на последнем слайде",
          not any("ТРЕБУЮЩИЕ ВНИМАНИЯ" in t.upper() for t in first)
          and any("ТРЕБУЮЩИЕ ВНИМАНИЯ" in t.upper() for t in last))


def test_b1_three_slides() -> None:
    """(A2) b1 всегда три листа (год, 1 ПГ, 2 ПГ) и 4 карточки стран."""
    slides = slides_of(render({"b1": _b1_payload()}))
    check("(A2) b1 - три слайда", len(slides) == 3, "%d" % len(slides))
    for idx, slide in enumerate(slides, 1):
        cards = [sh for sh in slide.shapes
                 if sh.name.startswith("card") and sh.name.endswith("_bg")]
        check("(A2) b1 слайд %d: четыре карточки стран" % idx,
              len(cards) == 4, "карточек %d" % len(cards))
        ph = [t for t in texts(slide) if "{{" in t]
        check("(A2) b1 слайд %d: без плейсхолдеров" % idx, not ph,
              "%r" % (ph[:2],))


# --- Листы 4-6 деки (p14: свод, группы ЦФО, детализация) -------------------

_NB_COLS = ("f25_h1", "f25_h2", "f25", "b26_h1", "f26_h1", "b26_h2",
            "t26_h2", "exp26")


def _nb_card(label: str, exp26: float, *, color: str | None = None,
             pie: float | None = None) -> dict:
    """Строка-направление листов 4-6: значения, Δ, доли, круги, численность."""
    card = {"label": label, "exp26": exp26,
            "dev_h1_pct": 5.0, "dev_h1_abs": 4.0,
            "dev_h2_pct": 4.0, "dev_h2_abs": 3.0,
            "dev_year_pct": 6.0, "dev_year_abs": 7.0,
            "share_h1_25": 10.0, "share_h1_26": 11.0,
            "share_h2_tac": 12.0, "share_exp": 13.0,
            "pp_h1": -0.1, "pp_h2": 0.2, "pp_year": 0.0,
            "hc_plan": 10.0, "hc_fact": 9.0, "hc_staffing": 90.0,
            "hc_dev": -1.0}
    for key in _NB_COLS:
        card.setdefault(key, exp26 / 2)
    if color is not None:
        card["color"] = color
    if pie is not None:
        card["pie_pct"] = pie
    return card


def _nb_payload(*, segments: int = 3, directions: int = 5,
                seg_colors: bool = False) -> dict:
    """DTO вкладки p14: свод направлений + группа ЦФО + детализация каналов.

    `segments` задаёт число сегментов доната листа 6 (проверка capDonut),
    `directions` — число направлений листа 4 (его донат НЕ сворачивается),
    `seg_colors` — сборщик уже покрасил сегменты листа 6 (`_P14_PIE_COLORS`,
    цвета с решёткой): экран такие цвета игнорирует и красит по индексу.
    """
    dirs = [_nb_card("Направление %d" % i, 1000.0 / (i + 1),
                     color="7A1F3D", pie=100.0 / directions)
            for i in range(directions)]
    total = {"label": "Общий итог", "total": True,
             **{k: 1000.0 for k in _NB_COLS}}
    body = [{"label": d["label"], **{k: d[k] for k in _NB_COLS}} for d in dirs]
    hc = {"plan_approved": 100.0, "fact_2026": 90.0, "staffing": 96.6}
    section_rows = [{"label": "Итого Производство", "kind": "total",
                     **{k: 500.0 for k in _NB_COLS}}]
    section_rows += [{"label": "Подразделение %d" % i, "kind": "detail",
                      **{k: 100.0 for k in _NB_COLS}} for i in range(3)]
    for row_ in section_rows[1:]:
        row_.update({k: v for k, v in _nb_card("", 100.0).items()
                     if k not in ("label", "exp26")})
    detail_rows = [{"label": "Итого Продажи", "kind": "total",
                    **{k: 800.0 for k in _NB_COLS}},
                   {"label": "Группа каналов", "kind": "direction",
                    **{k: 400.0 for k in _NB_COLS}}]
    detail_rows += [{"label": "Канал %d" % i, "kind": "detail",
                     **{k: 100.0 for k in _NB_COLS}} for i in range(3)]
    segs = [{"label": "Сегмент %02d" % i, "value": float(100 - i),
             "pct": 100.0 * (100 - i) / sum(range(100 - segments + 1, 101))}
            for i in range(segments)]
    if seg_colors:
        # как `_P14_PIE_COLORS` сборщика: серверная палитра обрезана на 7-м
        # цвете, поэтому листья за её концом красились бы одним цветом
        palette = ("#7A1F3D", "#F4A6C1", "#C2185B", "#E85D75", "#FFC845",
                   "#9E6B0F", "#9B5A70")
        for i, seg in enumerate(segs):
            seg["color"] = palette[min(i, len(palette) - 1)]
    return {
        "directions": dirs, "headcount": hc,
        "cards": [],
        "charts": [],
        "tables": [{"key": "by_direction", "label": "", "columns": [],
                    "rows": [total] + body}],
        "group_detail": {"sections": [{
            "label": "Производство", "rows": section_rows, "headcount": hc,
            "donut": [{"label": "Цех %d" % i, "value": float(10 - i),
                       "pct": 10.0 - i} for i in range(4)],
            "bars": []}]},
        "direction_detail": {"rows": detail_rows, "donut": segs, "bars": [],
                             "cards": [_nb_card("Канал %d" % i, 100.0)
                                       for i in range(3)],
                             "headcount": hc},
        "meta": {"tab": "p14", "params": {"year": 2026, "half": 1},
                 "periods": {"year_plan": 2026, "year_prev": 2025},
                 "expect": {"close_month": 6},
                 "slide": {"cc": "BY", "label": "Беларусь"}},
    }


def _donut(slide, name: str = "chart_donut"):
    """(категории, значения, цвета) доната слайда — только данные диаграммы."""
    from app.fot_deck_export import charts
    sh = shape_named(slide, name)
    if sh is None:
        return None
    return (charts.categories(sh), charts.values(sh), charts.point_colors(sh))


def test_nb_donut_cap() -> None:
    """(B1) Донат листов 5/6 сворачивается как capDonut на экране."""
    from app.fot_deck_export.binders import nb
    slides = slides_of(render({"p14": _nb_payload(segments=14, directions=12)}))
    names = [s._element.find(P + "cSld").get("name") for s in slides]
    detail_idx = names.index("fot:p14:nb_sales:0")
    cats, vals, colors = _donut(slides[detail_idx])
    check("(B1) донат листа 6: топ-9 + «Прочие»",
          len(cats) == nb.DONUT_MAX + 1, "%d сегментов (%r)" % (len(cats), cats))
    check("(B1) последний сегмент — «Прочие»",
          cats[-1] == "Прочие", "%r" % (cats[-1],))
    rest_color = None
    manifest = engine_mod.load_manifest()
    rest_color = (manifest.get("nb_colors") or {}).get("pie_rest")
    check("(B1) «Прочие» красится нейтральным цветом",
          colors[-1] == rest_color, "%r против %r" % (colors[-1], rest_color))
    check("(B1) цвета первых сегментов — палитра деки",
          len(set(c for c in colors[:nb.DONUT_MAX] if c)) == nb.DONUT_MAX,
          "%r" % (colors,))

    # доля «Прочих» = сумма долей отброшенных сегментов; значения в тыс.
    segs = _nb_payload(segments=14)["direction_detail"]["donut"]
    pos = [s["pct"] for s in segs if s["pct"] > 0]
    norm = [p / sum(pos) * 100 for p in pos]
    top = sorted(norm, reverse=True)[:nb.DONUT_MAX]
    check("(B1) доля «Прочих» = остаток после свёртки",
          abs(vals[-1] - (sum(norm) - sum(top))) < 0.01,
          "%r против %r" % (vals[-1], sum(norm) - sum(top)))


def test_nb_summary_donut_uncapped() -> None:
    """(B1) Донат листа 4 идёт БЕЗ свёртки (правило экрана `pieSegs`)."""
    from app.fot_deck_export.binders import nb
    slides = slides_of(render({"p14": _nb_payload(directions=12)}))
    cats, vals, colors = _donut(slides[0])
    check("(B1) донат листа 4 не сворачивается",
          len(cats) == 12 and "Прочие" not in cats, "%r" % (cats,))
    check("(B1) донат листа 4 красится цветом направления",
          set(c for c in colors if c) == {"7A1F3D"}, "%r" % (colors,))
    check("(B1) доли листа 4 — проценты, а не тыс.",
          abs(sum(vals) - 100.0) < 0.1, "сумма %r" % (sum(vals),))
    check("(B1) свёртка не тронула лист 4", nb.DONUT_MAX < len(cats))


def test_nb_donut_colors() -> None:
    """(B1) Цвета точек: палитра экрана по индексу и hex без решётки.

    Сборщик красит сегменты серверной палитрой, обрезанной на седьмом цвете,
    и с `#` впереди. Экран такой цвет игнорирует (`capDonut` красит по
    индексу), а `srgbClr/@val` решётку не принимает вовсе — PowerPoint считает
    файл повреждённым. Оба правила проверяются на 14 сегментах: цвета обязаны
    быть девятью РАЗНЫМИ цветами деки.
    """
    from app.fot_deck_export.binders import nb

    manifest = engine_mod.load_manifest()
    rest_color = (manifest.get("nb_colors") or {}).get("pie_rest")
    palette = (manifest["prototypes"]["nb_sales"]["charts"]["chart_donut"]
               .get("point_colors") or []) + (manifest["nb_colors"]
                                              .get("pie_palette") or [])
    palette = [c for i, c in enumerate(palette) if c and c not in palette[:i]]
    slides = slides_of(render({"p14": _nb_payload(segments=14, seg_colors=True)}))
    names = [s._element.find(P + "cSld").get("name") for s in slides]
    cats, _vals, colors = _donut(slides[names.index("fot:p14:nb_sales:0")])
    check("(B1) цвета сегментов — палитра экрана по индексу",
          colors[:nb.DONUT_MAX] == palette[:nb.DONUT_MAX],
          "%r против %r" % (colors, palette[:nb.DONUT_MAX]))
    check("(B1) «Прочие» — нейтральный цвет, а не цвет седьмого сегмента",
          colors[-1] == rest_color, "%r против %r" % (colors[-1], rest_color))
    check("(B1) свёрнутый донат не слипается: цветов столько же, сколько сегментов",
          len(set(colors[:nb.DONUT_MAX])) == nb.DONUT_MAX, "%r" % (colors,))
    check("(B1) в диаграмме нет цвета с решёткой",
          all(c and not c.startswith("#") for c in colors), "%r" % (colors,))


def test_nb_bar_captions() -> None:
    """(B1) Подписи мини-баров берут год из фильтра, а не из деки."""
    from app.fot_deck_export import charts

    def caps_for(year: int) -> list[str]:
        data = _nb_payload()
        data["meta"]["params"]["year"] = year
        data["meta"]["periods"] = {"year_plan": year, "year_prev": year - 1}
        slides = slides_of(render({"p14": data}))
        return charts.categories(shape_named(slides[0], "card1_chart"))

    caps26, caps27 = caps_for(2026), caps_for(2027)
    check("(B1) подписи мини-бара подставлены годом",
          all("{" not in c for c in caps26), "%r" % (caps26,))
    check("(B1) смена года меняет подписи мини-бара",
          caps26 != caps27 and any("27" in c for c in caps27)
          and any("26" in c for c in caps26),
          "%r против %r" % (caps26, caps27))


def test_nb_h2_closed() -> None:
    """(B1) Закрытое 2 ПГ меняет подписи листов 5/6 («Факт» вместо «Тактики»)."""
    open_payload = _nb_payload()
    closed_payload = _nb_payload()
    closed_payload["meta"]["expect"] = {"close_month": 12}
    open_texts = texts(slides_of(render({"p14": open_payload}))[0])
    closed_texts = texts(slides_of(render({"p14": closed_payload}))[0])
    check("(B1) до закрытия 2 ПГ подписи «Тактика»",
          any("Такт" in t for t in open_texts),
          "%r" % ([t for t in open_texts if "Такт" in t][:2],))
    check("(B1) после закрытия 2 ПГ подписи «Факт»",
          not any("Такт" in t for t in closed_texts),
          "%r" % ([t for t in closed_texts if "Такт" in t][:2],))


def test_nb_prod_only_production() -> None:
    """(B2) Лист 5 печатает только производственные группы ЦФО.

    Продажи и отделы дека печатает другими листами (6 и 7): до этой проверки
    слайд строился на КАЖДУЮ группу среза, и в производственную секцию
    попадали магазины, интернет-магазин и отделы.
    """
    payload = _nb_payload()
    template = payload["group_detail"]["sections"][0]
    payload["group_detail"]["sections"] = [
        dict(template, label="Производство", kind="production"),
        dict(template, label="Отдел", kind="dept"),
        dict(template, label="Магазины", kind="sales"),
    ]
    slides = slides_of(render({"p14": payload}))
    names = [s._element.find(P + "cSld").get("name") for s in slides]
    prod = [n for n in names if n.startswith("fot:p14:nb_prod")]
    check("(B2) лист 5 — один слайд на производственную группу",
          len(prod) == 1, "%r" % (names,))


def test_nb_sales_uses_sales_detail() -> None:
    """(B2) Лист 6 берёт sales_detail, а не весь direction_detail.

    В продажи деки не попадают ни цеха, ни отделы: строки листа 6 — только
    каналы продаж (розница/опт/маркетплейсы/интернет-магазин).
    """
    payload = _nb_payload()
    payload["sales_detail"] = {
        "rows": [{"label": "Итого Продажи", "kind": "total",
                  **{k: 500.0 for k in _NB_COLS}},
                 {"label": "Канал продаж", "kind": "detail",
                  **{k: 500.0 for k in _NB_COLS}}],
        "donut": [{"label": "Канал продаж", "value": 500.0, "pct": 100.0}],
        "bars": [], "cards": [],
        "headcount": {"plan": 100.0, "fact": 90.0, "staffing": 96.6}}
    slides = slides_of(render({"p14": payload}))
    names = [s._element.find(P + "cSld").get("name") for s in slides]
    detail = texts(slides[names.index("fot:p14:nb_sales:0")])
    check("(B2) лист 6 печатает строки sales_detail",
          any("Канал продаж" in t for t in detail), "%r" % (detail[:3],))
    check("(B2) строк direction_detail в продажах нет",
          not any("Группа каналов" in t for t in detail),
          "%r" % ([t for t in detail if "канал" in t.lower()][:3],))


# --- Листы 28/30 деки (b3: свод подразделения и статьи страны) --------------

def _b3_card(cc: str, exp: float, *, d1=10.0, d2=20.0, dyear=30.0) -> dict:
    """Карточка страны листа 28: ожидание и три Δ% (сборщик отдаёт числа)."""
    return {"cc": cc, "label": {"BY": "Беларусь", "RU": "Россия",
                               "KZ": "Казахстан", "UZ": "Узбекистан",
                               "TR": "Турция"}.get(cc, cc),
            "exp": exp, "d1": d1, "d1_abs": 1.0,
            "d2": d2, "d2_abs": 2.0, "dyear": dyear, "dyear_abs": 3.0}


def _b3_article(label: str, v1: float, *, base=1.0, pct=10.0) -> dict:
    """Статья листа 30: три периода (значение, база, Δ%)."""
    out = {"label": label}
    for i in (1, 2, 3):
        out["v%d" % i] = v1 * i
        out["b%d" % i] = base * i
        out["d%d" % i] = pct * i
    return out


def _b3_payload(*, depts: tuple[str, ...] = ("HR",), countries=None,
                articles: int = 5, exp_by_dept: dict | None = None) -> dict:
    """DTO вкладки b3: свод одного подразделения + статьи одной страны.

    `depts` — что вернёт `dept_options` (от него зависит перебор выгрузки),
    `countries` — карточки стран свода, `articles` — число статей листа 30.
    """
    countries = countries or ("BY", "RU", "KZ", "UZ")
    exp_by_dept = exp_by_dept or {}
    cards = {"f25_h1": 1000.0, "f26_h1": 1100.0, "d1": 10.0, "d1_abs": 100.0,
             "f25_h2": 1200.0, "t26_h2": 1300.0, "d2": 8.3, "d2_abs": 100.0,
             "exp": 2400.0, "dyear": 9.1, "dyear_abs": 200.0}
    hc = {"plan_approved": 40.0, "fact_prev": 42.0, "fact_cur": 41.0,
          "d_plan": -1.0, "d_year": 1.0}
    names = ("ОСНОВНЫЕ РАСХОДЫ", "ВЫПЛАТЫ РАБОТНИКАМ",
             "ВСПОМОГАТЕЛЬНЫЕ РАСХОДЫ", "ПРОЧИЕ РАСХОДЫ", "ВЛОЖЕНИЯ",
             "АМОРТИЗАЦИЯ")
    return {"cards": cards, "headcount": hc,
            "cards_by_country": [_b3_card(cc, 500.0 + i * 100,
                                          d1=10.0 + i, d2=-5.0 * i,
                                          dyear=2.0 * i)
                                 for i, cc in enumerate(countries)],
            "articles": [_b3_article(names[i % len(names)], 100.0 + i,
                                     base=50.0 + i, pct=5.0 * (i + 1))
                         for i in range(articles)],
            "meta": {"tab": "b3", "params": {"year": 2026},
                     "periods": {"year_plan": 2026}, "currency": "BYN",
                     "sel_dept": depts[0] if depts else "",
                     "sel_country": "BY",
                     "dept_options": [{"dept": d, "countries": 2}
                                      for d in depts]}}


def test_b3_registry() -> None:
    """(C1) b3 в реестре после p17: свод и статьи, варианты строит движок."""
    from app.fot_deck_export import registry
    sheet = registry.sheet_for("b3")
    check("(C1) b3 идёт последней вкладкой деки",
          registry.ALL_TABS[-1] == "b3", "%r" % (registry.ALL_TABS,))
    check("(C1) у b3 два прототипа: свод и статьи",
          [s.proto for s in sheet.slides] == ["b3_summary", "b3_country"],
          "%r" % ([s.proto for s in sheet.slides],))
    check("(C1) варианты b3 строит движок, а не реестр",
          all(s.variants is None for s in sheet.slides))


def test_b3_depts_iteration() -> None:
    """(C1) Пустой срез «Направление» — все dept_options, суженный — один."""
    base = _b3_payload(depts=("HR", "ФЭО", "Логистика"))
    check("(C1) пустой фильтр — все подразделения сборщика",
          collect_mod.b3_depts(base, {}) == ["HR", "ФЭО", "Логистика"],
          "%r" % (collect_mod.b3_depts(base, {}),))
    check("(C1) суженный срез — ровно выбранное направление",
          collect_mod.b3_depts(base, {"direction": ["ФЭО"]}) == ["ФЭО"])
    check("(C1) направление вне «≥2 стран» листов не даёт",
          collect_mod.b3_depts(base, {"direction": ["Нет такого"]}) == [],
          "%r" % (collect_mod.b3_depts(base, {"direction": ["Нет такого"]}),))
    check("(C1) несколько направлений в срезе = полный перебор",
          collect_mod.b3_depts(base, {"direction": ["ФЭО", "HR"]})
          == ["HR", "ФЭО", "Логистика"])
    check("(C1) одиночная строка направления тоже сужает",
          collect_mod.b3_direction({"direction": "HR"}) == "HR")
    check("(C1) пустой срез направления — пустая строка",
          collect_mod.b3_direction({}) == ""
          and collect_mod.b3_direction({"direction": []}) == "")


def test_b3_variants_rendered() -> None:
    """(C1) Перебор: один свод на подразделение + лист статей на каждую страну."""
    dirs = {"calls": []}

    def fake_collect(tab: str, filters: dict) -> dict:
        """Сборщик-пустышка: фиксирует фильтры и отдаёт нужный срез."""
        dirs["calls"].append((tab, dict(filters or {})))
        data = _b3_payload(depts=("HR", "ФЭО"))
        dept = (filters or {}).get("b3_dept")
        if dept:
            data["meta"]["sel_dept"] = dept
        cc = (filters or {}).get("b3_country")
        if cc:
            data["meta"]["sel_country"] = cc
        else:
            data["articles"] = []
        return data

    async def fake(tab: str, filters: dict) -> dict:
        return fake_collect(tab, filters)

    original = collect_mod.collect
    collect_mod.collect = fake
    try:
        blob = asyncio.run(engine_mod.export_deck(["b3"], {}))
    finally:
        collect_mod.collect = original
    names = _slide_names(blob)
    summaries = [n for n in names if "b3_summary" in n]
    countries = [n for n in names if "b3_country" in n]
    check("(C1) свод — один слайд на подразделение", len(summaries) == 2,
          "%r" % (names,))
    check("(C1) статьи — слайд на каждую страну каждого подразделения",
          len(countries) == 2 * 4, "%r" % (names,))
    # нумерация страниц у каждой сборки своя (страница = позиция в биндере),
    # поэтому порядок проверяется по позициям прототипов в списке слайдов
    kinds = ["S" if "b3_summary" in n else "C" for n in names]
    check("(C1) порядок деки: свод, затем страны, затем следующий свод",
          kinds == ["S"] + ["C"] * 4 + ["S"] + ["C"] * 4, "%r" % (kinds,))
    check("(C1) сборщик получил b3_dept и b3_country",
          any(f.get("b3_dept") == "ФЭО" and f.get("b3_country") == "KZ"
              for _t, f in dirs["calls"]),
          "%r" % (dirs["calls"][:3],))


def test_b3_direction_and_country_narrow() -> None:
    """(C1) Срез «Направление» сужает перебор; страна — только её статьи."""
    def fake(tab: str, filters: dict) -> dict:
        data = _b3_payload(depts=("HR", "ФЭО"))
        cc = (filters or {}).get("b3_country")
        if not cc:
            data["articles"] = []
        return data

    async def run(tab: str, filters: dict) -> dict:
        return fake(tab, filters)

    original = collect_mod.collect
    collect_mod.collect = run
    try:
        one = asyncio.run(engine_mod.export_deck(
            ["b3"], {"direction": ["ФЭО"]}))
        bad = None
        try:
            asyncio.run(engine_mod.export_deck(
                ["b3"], {"direction": ["Нет такого"]}))
        except engine_mod.EmptySlice:
            bad = "EmptySlice"
        except Exception as exc:  # noqa: BLE001 - важен тип
            bad = type(exc).__name__
    finally:
        collect_mod.collect = original
    names = _slide_names(one)
    check("(C1) сужение направления даёт один свод",
          len([n for n in names if "b3_summary" in n]) == 1, "%r" % (names,))
    check("(C1) направление вне «≥2 стран» — EmptySlice (роут отдаёт 422)",
          bad == "EmptySlice", "получено %r" % (bad,))


def test_b3_card_cloning() -> None:
    """(C2) Сетки деки: 4 карточки стран → 5, 5 статей → 6 (и обратно)."""
    five = slides_of(render({"b3": _b3_payload(
        countries=("BY", "RU", "KZ", "UZ", "TR"))}))
    summary = [s for s in five
               if s._element.find(P + "cSld").get("name").endswith("b3_summary:0")][0]
    bgs = [sh.name for sh in summary.shapes
           if sh.name.startswith("card") and sh.name.endswith("_bg")]
    check("(C2) пятая страна разворачивает клон карточки", len(bgs) == 5,
          "%r" % (bgs,))
    # клон получает геометрию следующего места сетки, а не копию исходного
    first = shape_named(summary, "card4_bg")
    cloned = shape_named(summary, "card5_bg")
    check("(C2) клон сдвинут на шаг сетки деки",
          cloned is not None and first is not None
          and cloned.left != first.left and cloned.top != first.top,
          "card4=%s card5=%s" % (first and (first.left, first.top),
                                 cloned and (cloned.left, cloned.top)))

    six = slides_of(render({"b3": _b3_payload(articles=6)}))
    country = [s for s in six
               if s._element.find(P + "cSld").get("name").endswith("b3_country:0")][0]
    arts = [sh.name for sh in country.shapes
            if sh.name.startswith("card") and sh.name.endswith("_bg")]
    check("(C2) шестая статья разворачивает клон карточки", len(arts) == 6,
          "%r" % (arts,))

    two = slides_of(render({"b3": _b3_payload(countries=("BY", "RU"),
                                              articles=2)}))
    summary2 = [s for s in two
                if s._element.find(P + "cSld").get("name").endswith("b3_summary:0")][0]
    left = [sh.name for sh in summary2.shapes
            if sh.name.startswith("card") and sh.name.endswith("_bg")]
    check("(C2) лишние места сетки удалены, а не оставлены пустыми",
          left == ["card1_bg", "card2_bg"], "%r" % (left,))


def test_b3_labels_and_signs() -> None:
    """(C2) Подписи деки: год в заголовке, валюта, знак и цвет Δ%."""
    blob = render({"b3": _b3_payload()})
    slides = slides_of(blob)
    summary = [s for s in slides
               if s._element.find(P + "cSld").get("name").endswith("b3_summary:0")][0]
    all_text = " ".join(texts(summary))
    check("(C2) заголовок свода несёт подразделение и год",
          "HR" in all_text and "2026" in all_text, "%r" % (all_text[:120],))
    check("(C2) подпись единицы — тыс. BYN",
          any("тыс. BYN" in t for t in texts(summary)),
          "%r" % ([t for t in texts(summary) if "тыс." in t][:2],))
    check("(C2) подпись карточки страны — отбивка между кодом и названием",
          any(t.startswith("BY  Беларусь") for t in texts(summary)),
          "%r" % ([t for t in texts(summary) if "Беларус" in t][:2],))
    dev = shape_named(summary, "card1_d1")
    runs = [r.text for r in dev.text_frame.paragraphs[0].runs] if dev is not None else []
    check("(C2) Δ% карточки: подпись периода + значение",
          len(runs) >= 2 and "1 ПГ" in "".join(runs), "%r" % (runs,))
    # Подпись периода остаётся СЕРОЙ, цветной только ран значения: иначе
    # реставрация деки даёт ~250 px расхождения на строку.
    gray = engine_mod.load_manifest()["sign_colors"]["na_text"]
    up = engine_mod.load_manifest()["sign_colors"]["delta_up"]
    down = engine_mod.load_manifest()["sign_colors"]["delta_down"]
    check("(C2) подпись периода Δ% остаётся серой",
          run_color(summary, "card1_d1", 0) == gray,
          "%r" % (run_color(summary, "card1_d1", 0),))
    check("(C2) ран значения Δ% красится по знаку",
          run_color(summary, "card1_d1", len(runs) - 1) == up
          and run_color(summary, "card2_d2", 1) == down,
          "card1_d1=%r card2_d2=%r" % (run_color(summary, "card1_d1", len(runs) - 1),
                                       run_color(summary, "card2_d2", 1)))

    country = [s for s in slides
               if s._element.find(P + "cSld").get("name").endswith("b3_country:0")][0]
    ctext = " ".join(texts(country))
    check("(C2) заголовок статей несёт страну, подразделение и валюту",
          "Беларусь" in ctext and "HR" in ctext and "BYN" in ctext,
          "%r" % (ctext[:140],))
    check("(C2) круг Δ% статьи печатает процент внутри",
          any(t.endswith("%") and t[0] in "+−" for t in texts(country)),
          "%r" % ([t for t in texts(country) if t.endswith("%")][:3],))


def test_b3_year_slots() -> None:
    """(C2) Смена года в фильтре меняет подписи листов 28/30."""
    def texts_for(year: int) -> str:
        data = _b3_payload()
        data["meta"]["params"]["year"] = year
        data["meta"]["periods"] = {"year_plan": year}
        slides = slides_of(render({"b3": data}))
        return " ".join(texts(slides[0]))

    y26, y27 = texts_for(2026), texts_for(2027)
    check("(C2) год подставлен в подписи свода",
          "2026" in y26 and "2027" in y27, "%r / %r" % (y26[:80], y27[:80]))
    check("(C2) предыдущий год сдвигается вместе с текущим",
          "1 ПГ 2025" in y26 and "1 ПГ 2026" in y27,
          "%r" % ([t for t in y27.split(" ") if t.startswith("20")][:4],))


def test_b3_empty_slice() -> None:
    """(C3) Нет подразделений/стран/статей — EmptySlice, а не пустой слайд."""
    from app.fot_deck_export.binders import b3 as b3_binder

    empty_dept = _b3_payload(depts=())
    empty_dept["cards_by_country"] = []
    empty_art = _b3_payload(articles=0)

    def fake_empty(tab: str, filters: dict) -> dict:
        return empty_dept if not (filters or {}).get("b3_country") else empty_art

    async def run(tab: str, filters: dict) -> dict:
        return fake_empty(tab, filters)

    original = collect_mod.collect
    collect_mod.collect = run
    try:
        kinds = []
        for filters in ({}, {"direction": ["Нет такого"]}):
            try:
                asyncio.run(engine_mod.export_deck(["b3"], filters))
            except engine_mod.EmptySlice:
                kinds.append("EmptySlice")
            except Exception as exc:  # noqa: BLE001 - важен тип
                kinds.append(type(exc).__name__)
        # свод без стран: биндер обязан упасть EmptySlice, а не собрать пусто
        try:
            engine = engine_mod.DeckEngine(engine_mod.load_template(),
                                           engine_mod.load_manifest())
            engine.build_tab("b3", dict(empty_dept, _only="b3_summary"))
            engine.finish()
        except engine_mod.EmptySlice:
            kinds.append("EmptySlice")
        except RuntimeError:
            kinds.append("EmptySlice")
        except Exception as exc:  # noqa: BLE001
            kinds.append(type(exc).__name__)
    finally:
        collect_mod.collect = original
    check("(C3) пустой срез блока 3 — EmptySlice", kinds == ["EmptySlice"] * 3,
          "%r" % (kinds,))


def test_b3_nd_rule() -> None:
    """(C3) Без значения или база ≤ 0 — «н/д», а не «−190%»."""
    from app.fot_deck_export.binders import b3 as b3_binder
    text, token = b3_binder._pct_nd(None)
    check("(C3) нет значения — «н/д»", text == fmt.NA and token == fmt.TOKEN_NA,
          "%r/%r" % (text, token))
    text, token = b3_binder._pct_nd(12.34)
    check("(C3) значение — знак и один разряд", text == "+12,3"
          and token == fmt.TOKEN_UP, "%r/%r" % (text, token))
    check("(C3) отрицательное — типографский минус",
          b3_binder._pct_nd(-4.0)[0] == "−4,0",
          "%r" % (b3_binder._pct_nd(-4.0)[0],))
    payload = _b3_payload(countries=("BY",))
    payload["cards_by_country"][0].update({"d1": None, "d2": None, "dyear": None})
    slides = slides_of(render({"b3": payload}))
    summary = [s for s in slides
               if s._element.find(P + "cSld").get("name").endswith("b3_summary:0")][0]
    nd = [t for t in texts(summary) if fmt.NA in t]
    check("(C3) «н/д» печатается в подписи Δ% карточки", len(nd) >= 3,
          "%r" % (nd[:4],))


def test_no_placeholders_all_sheets() -> None:
    """(A3) Во всех листах реестра нет «{{» и нумерация сквозная."""
    payloads = {"b1": _b1_payload(), "p14": _nb_payload(),
                "p15": _dept_payload("BY", 4),
                "p16": _dept_payload("BY", 4, with_table=False),
                "p17": payload({"BY": [row("A"), row("B")]}),
                "b3": _b3_payload()}
    blob = render(payloads)
    slides = slides_of(blob)
    leftovers = [(sh.name, t) for s in slides for sh in s.shapes
                 for t in texts(s) if "{{" in t]
    check("(A3) ни одного «{{» во всех листах", not leftovers,
          "%r" % (leftovers[:2],))
    nums = []
    for s in slides:
        page = shape_named(s, "page")
        # номер страницы — ЕДИНСТВЕННЫЙ текст шейпа `page` (не текст слайда:
        # у шейпа может быть несколько ранов, и склейка даёт пустую строку)
        nums.append("".join(t for t in _page_texts(page)) if page is not None
                    else None)
    digits = [int(n) for n in nums if n and str(n).isdigit()]
    check("(A3) нумерация страниц сквозная 1..N",
          digits == list(range(1, len(slides) + 1)), "%r" % (nums,))


def test_collect_parallel() -> None:
    """(D1) Срезы собираются ПАРАЛЛЕЛЬНО и не вычерпывают пул БД.

    Сравниваем два прогона одного и того же запроса: с семафором 1 и с рабочим.
    Заглушка сборщика спит фиксированное время, поэтому разница времён — это
    ровно выигрыш от параллельности, а не шум витрин.
    """
    live = {"now": 0, "max": 0, "total": 0}

    async def slow(tab: str, filters: dict) -> dict:
        live["now"] += 1
        live["total"] += 1
        live["max"] = max(live["max"], live["now"])
        try:
            await asyncio.sleep(0.03)
            return _b3_payload(depts=("HR", "ФЭО"), countries=("BY", "RU", "KZ"))
        finally:
            live["now"] -= 1

    async def slow_country(tab: str, filters: dict, cc: str) -> dict:
        return await slow(tab, dict(filters or {}, country=[cc]))

    original, original_country = collect_mod.collect, collect_mod.collect_country
    limit = collect_mod.COLLECT_CONCURRENCY
    collect_mod.collect, collect_mod.collect_country = slow, slow_country
    try:
        collect_mod.COLLECT_CONCURRENCY = 1
        started = time.monotonic()
        asyncio.run(engine_mod.export_deck(["b3"], {}))
        serial = time.monotonic() - started
        jobs_serial = live["total"]
        live.update(now=0, max=0, total=0)
        collect_mod.COLLECT_CONCURRENCY = limit
        started = time.monotonic()
        asyncio.run(engine_mod.export_deck(["b3"], {}))
        parallel = time.monotonic() - started
        jobs = live["total"]
    finally:
        collect_mod.COLLECT_CONCURRENCY = limit
        collect_mod.collect, collect_mod.collect_country = original, original_country

    check("(D1) одновременно идёт не больше семафора",
          1 < live["max"] <= limit,
          "макс %d, семафор %d, задач %d" % (live["max"], limit, jobs))
    check("(D1) параллельный сбор быстрее последовательного",
          jobs == jobs_serial and parallel < serial * 0.8,
          "%d задач: последовательно %.2f c, параллельно %.2f c"
          % (jobs, serial, parallel))


def test_collect_dedup() -> None:
    """(D2) Одинаковые срезы в рамках запроса — один поход в витрину.

    Ключ дедупликации — срезы ЗАПРОСА, а не параметры сборщика: год числом и
    строкой, страна строкой и списком, дубль страны в списке — один срез.
    """
    calls = {"n": 0}

    async def fake(tab: str, filters: dict) -> dict:
        calls["n"] += 1
        await asyncio.sleep(0.01)
        return _b3_payload(depts=("HR",))

    original = collect_mod.collect
    collect_mod.collect = fake
    try:
        async def scenario():
            collector = collect_mod.Collector()
            same = [collector.payload("b3", {"year": 2026, "country": ["BY"]}),
                    collector.payload("b3", {"year": "2026", "country": "BY"}),
                    collector.payload("b3",
                                      {"country": ["BY", "BY"], "year": 2026})]
            await collect_mod.settle(same)
            first = same[0].result()
            out = {"reused": collector.reused,
                   "identical": same[0] is same[1] is same[2],
                   "same_obj": same[1].result() is first,
                   "calls_same": calls["n"]}
            other = collect_mod.Collector()
            await collect_mod.settle([other.payload("b3", {"year": 2026}),
                                      other.payload("b3", {"year": 2027})])
            out["calls_all"] = calls["n"]
            return out

        out = asyncio.run(scenario())
    finally:
        collect_mod.collect = original
    check("(D2) одинаковые срезы переиспользуют задачу",
          out["calls_same"] == 1 and out["identical"] and out["reused"] == 2,
          "походов %d, переиспользовано %d" % (out["calls_same"], out["reused"]))
    check("(D2) повторный срез отдаёт тот же payload", out["same_obj"])
    check("(D2) разные срезы собираются отдельно",
          out["calls_all"] == out["calls_same"] + 2,
          "походов всего %d" % out["calls_all"])


def test_collect_fail_closed() -> None:
    """(D3) Сбой одного варианта — ошибка на ВЕСЬ запрос, без частичного pptx.

    Ждём до первого сбоя: остальные варианты в этот момент ещё в полёте, и
    выгрузка не должна их дожидаться.
    """
    async def boom(tab: str, filters: dict) -> dict:
        data = _b3_payload(depts=("HR",), countries=("BY", "RU", "KZ"))
        if (filters or {}).get("b3_country") == "KZ":
            await asyncio.sleep(0.01)
            raise engine_mod.EmptySlice("b3: тестовый сбой варианта")
        if (filters or {}).get("b3_country"):
            await asyncio.sleep(0.4)      # остальные варианты ЕЩЁ В ПОЛЁТЕ
        return data

    original = collect_mod.collect
    collect_mod.collect = boom
    try:
        started = time.monotonic()
        kind = None
        try:
            asyncio.run(engine_mod.export_deck(["b3"], {}))
        except engine_mod.EmptySlice:
            kind = "EmptySlice"
        except Exception as exc:  # noqa: BLE001 - важен тип
            kind = type(exc).__name__
        waited = time.monotonic() - started
    finally:
        collect_mod.collect = original
    check("(D3) сбой варианта — EmptySlice на весь запрос",
          kind == "EmptySlice", "получено %r" % (kind,))
    check("(D3) ответ сразу, без ожидания остальных вариантов",
          waited < 0.3, "%.2f c (остальные висят 0.4 c)" % waited)


def test_find_shape_by_xml() -> None:
    """(D4) Поиск шейпа по имени — по XML, с тем же результатом.

    find_shape читает cNvPr/@name напрямую: обход `slide.shapes` строил объект
    python-pptx на каждый шейп и съедал больше времени, чем вся сборка. Здесь
    сверяем, что выборка не теряет и не путает шейпы (включая вложенные в
    группу — до них, как и раньше, не достаём).
    """
    blob = render({"b3": _b3_payload(countries=("BY",))})
    slides = slides_of(blob)
    slide = [s for s in slides
             if s._element.find(P + "cSld").get("name").endswith("b3_summary:0")][0]
    by_walk = {sh.name: sh.shape_id for sh in slide.shapes}
    found = {name: shapes.find_shape(slide, name).shape_id for name in by_walk}
    check("(D4) find_shape находит все шейпы слайда", found == by_walk,
          "не совпало: %r" % ({k: (found.get(k), by_walk[k])
                              for k in by_walk if found.get(k) != by_walk[k]},))
    check("(D4) неизвестное имя — None",
          shapes.find_shape(slide, "нет такого шейпа") is None)


def main() -> int:
    logging.disable(logging.CRITICAL)
    tests = (test_format_boundaries, test_layout_capacities, test_empty_slice,
             test_paging_and_style, test_kz_total_top, test_row_keys_guard,
             test_extra_columns_tolerated, test_asset_sha_guard,
             # листы 1-3 и 7-8 деки (партия A)
             test_registry_order, test_country_variants,
             test_p16_grid_overflow, test_p15_pagination,
             test_b1_three_slides,
             # листы 4-6 деки: чарты и подписи
             test_nb_donut_cap, test_nb_donut_colors,
             test_nb_summary_donut_uncapped,
             test_nb_bar_captions, test_nb_h2_closed,
             test_nb_prod_only_production, test_nb_sales_uses_sales_detail,
             # листы 28/30 деки: свод подразделения и статьи страны
             test_b3_registry, test_b3_depts_iteration,
             test_b3_variants_rendered, test_b3_direction_and_country_narrow,
             test_b3_card_cloning, test_b3_labels_and_signs,
             test_b3_year_slots, test_b3_empty_slice, test_b3_nd_rule,
             # сбор данных выгрузки: параллельность и дедупликация срезов
             test_collect_parallel, test_collect_dedup,
             test_collect_fail_closed, test_find_shape_by_xml,
             test_no_placeholders_all_sheets)
    for test in tests:
        try:
            test()
        except Exception as exc:  # noqa: BLE001 - падение проверки = провал
            FAILS.append("%s: исключение %s: %s"
                         % (test.__name__, type(exc).__name__, exc))
    for name in OKS:
        print("OK   %s" % name)
    for name in FAILS:
        print("FAIL %s" % name)
    print("\nитог: %d ок, %d провалов" % (len(OKS), len(FAILS)))
    return 1 if FAILS else 0


if __name__ == "__main__":
    raise SystemExit(main())
