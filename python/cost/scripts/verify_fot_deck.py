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
          registry.ALL_TABS == ("b1", "p15", "p16", "p17"),
          "%r" % (registry.ALL_TABS,))
    for tab in ("b1", "p15", "p16", "p17"):
        check("(A1) вкладка %r есть в реестре" % tab,
              tab in registry.SHEETS_BY_TAB)
    check("(A1) b1 - три листа деки",
          len(registry.sheet_for("b1").slides) == 3)
    payloads = {"b1": _b1_payload(), "p15": _dept_payload("BY", 3),
                "p16": _dept_payload("BY", 3, with_table=False)}
    names = _slide_names(render(payloads))
    check("(A1) b1 даёт три слайда подряд",
          names[:3] == ["fot:b1:b1_year:0", "fot:b1:b1_h1:0", "fot:b1:b1_h2:0"],
          "%r" % (names[:3],))
    check("(A1) p15 идёт после b1",
          "fot:p15:dept_summary:0" in names
          and names.index("fot:p15:dept_summary:0") > 2, "%r" % (names,))
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


def test_no_placeholders_all_sheets() -> None:
    """(A3) Во всех листах реестра нет «{{» и нумерация сквозная."""
    payloads = {"b1": _b1_payload(), "p15": _dept_payload("BY", 4),
                "p16": _dept_payload("BY", 4, with_table=False),
                "p17": payload({"BY": [row("A"), row("B")]})}
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


def main() -> int:
    logging.disable(logging.CRITICAL)
    tests = (test_format_boundaries, test_layout_capacities, test_empty_slice,
             test_paging_and_style, test_kz_total_top, test_row_keys_guard,
             test_extra_columns_tolerated, test_asset_sha_guard,
             # листы 1-3 и 7-8 деки (партия A)
             test_registry_order, test_country_variants,
             test_p16_grid_overflow, test_p15_pagination,
             test_b1_three_slides, test_no_placeholders_all_sheets)
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
