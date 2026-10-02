"""Материалы ФКСС против ПФКСС — блок дашборда «Маржа выпуска».

Задача Б24 661229 (Пушкарчук Н.Н., 01.10.2026): по каждому бренду, списком по
всем артикулам видеть, что посчитано в ФКСС и в ПФКСС отдельно по основным и
вспомогательным материалам, и отклонение между ними. «Бренд» — бренд-менеджер
(решение заказчика 01.10.2026). Выгрузка в Excel собирается на фронте из тех же
строк.

ВЫБОРКА И СОПОСТАВЛЕНИЕ
=======================
Строка — задание ФКСС из выборки дашборда маржи (_MARGIN_BASE и те же фильтры,
период — по дате производства). К нему подбирается ПФКСС той же модели, артикула
и плана (решение заказчика 01.10.2026):

* если есть ПФКСС с тем же номером задания (без пробелов по краям — в источнике
  у ФКСС «М26.3.1437  », у ПФКСС «М26.3.1437») — берётся она, `match = exact`;
* иначе ПФКСС подставляется ко всем заданиям ФКСС, `match = fallback`. Если
  заданий ПФКСС у артикула+плана несколько, а совпадающего нет, берётся самое
  дорогое — то же правило, что при простановке цены (get_max_calc_cost в db.py):
  именно по нему ставилась цена, с ним и сравниваем.

У ПФКСС берётся последняя дата расчёта задания. ПФКСС ищется в cost_data_all, а
не в витрине: туда входят и калькуляции, созданные в приложении копией
(cost_manual_calc), а cost_calc_mv строится только по кэшу источника.

Задания ФКСС без ПФКСС в список не попадают: предварительный расчёт ведут не
для всего ассортимента (на 01.10.2026 — 1 149 заданий из 57 тыс.), и десятки
тысяч пустых строк закрыли бы те, где сравнивать есть что. Покрытие отдаётся в
meta, интерфейс его показывает.

СУММЫ — ПО СТРОКАМ, А НЕ ИЗ ВИТРИНЫ
===================================
Основные и вспомогательные материалы складываются здесь из строк калькуляции.
До миграции 0062 витрина cost_calc_mv брала по статье max() по строкам — самую
дорогую строку: у задания М26.3.1437 (117915 / 26-46847П-0) вспомогательные
0,0615 (один пакет) вместо 0,1282 по строкам. Теперь витрина тоже суммирует
строки, но строки здесь нужны всё равно: ПФКСС берётся из cost_data_all (с
копиями, которых в витрине нет), а раскрытие идёт до материала. Сумма шести
статей по строкам совпадает с «Себестоимость, руб.» у всех калькуляций, кроме
одной (01.10.2026).

ЦЕНА МАТЕРИАЛА — СУММА / НОРМА
==============================
В детализации по материалам цена считается как сумма статьи / норма, а не
берётся из колонки «цена материала, руб.»: у ФКСС Узбекистана в этой колонке
сумы (пакет 236,61 при сумме 0,0615 BYN), и сравнение цен показало бы
отклонения в тысячи раз. Суммы по статьям при этом в рублях и верные.

Единицы: себестоимость ЕДИНИЦЫ в BYN. На уровнях выше задания фронт взвешивает
её по выпуску ФКСС — как лист «Отклонения по артикулам».
"""

from __future__ import annotations

from datetime import date

from .commercial import _as_dict, _collect_params
from .db import _PLAN_OSN_TYPES, _PLAN_VSP_TYPES, pool
from .margin import _DATE, _MARGIN_BASE, DATE_EXPRS, MARGIN_FILTERS

# Предел строк ответа. Пар ФКСС↔ПФКСС за все годы ~1,2 тыс. (01.10.2026), так
# что предел — с большим запасом; при превышении ответ говорит, что обрезан.
ROW_LIMIT = 20_000

# Расхождение материалов больше чем в столько раз — пара несопоставима:
# похоже на калькуляцию на партию, а не на штуку (на плановых этапах такие
# встречаются, см. calc_structure.calc_stages). Строка остаётся в списке с
# пометкой, но в итоги групп не входит — иначе одна такая пара перекашивает
# средневзвешенное по всему бренд-менеджеру.
SUSPECT_RATIO = 20.0

_SUM6 = (
    'coalesce(c."Основные материалы, руб.", 0) + coalesce(c."Вспомогательные материалы, руб.", 0)'
    ' + coalesce(c."Пошив, руб.", 0) + coalesce(c."Раскрой, руб.", 0)'
    ' + coalesce(c."Декоры, руб.", 0) + coalesce(c."Вязание, руб.", 0)'
)


def _pairs_ctes(fk_where: str) -> str:
    """CTE fk → pf_sums → pf → pairs: задания ФКСС выборки и подобранная к
    каждому ПФКСС. Общие для списка и детализации — иначе строка списка и её
    раскрытие могли бы взять разные ПФКСС."""
    return f"""
        fk AS MATERIALIZED (
            SELECT model, articul, coalesce(plan_id, '') AS plan_id,
                   zadanie, coalesce(trim(zadanie), '') AS z,
                   calc_date, trim(brand_manager) AS brand_manager,
                   model_name, volume_pcs AS vol
            FROM cost_calc_mv
            WHERE {fk_where}
        ),
        -- Все ПФКСС раздела — их ~1,5 тыс. калькуляций, дешевле посчитать суммы
        -- разом, чем искать по каждому заданию.
        pf_sums AS MATERIALIZED (
            SELECT trim(c."Модель") AS m, trim(c."Артикул") AS a,
                   coalesce(trim(c."PLAN_ID"), '') AS plan_id,
                   c."Номер задания производства" AS zadanie,
                   coalesce(trim(c."Номер задания производства"), '') AS z,
                   c."дата расчета" AS calc_date,
                   sum(coalesce(c."Основные материалы, руб.", 0)) AS osn,
                   sum(coalesce(c."Вспомогательные материалы, руб.", 0)) AS vsp,
                   sum({_SUM6}) AS cost
            FROM cost_data_all c
            WHERE c."Признак калькуляции" = 'ПФКСС'
            GROUP BY 1, 2, 3, 4, 5, 6
        ),
        pf AS MATERIALIZED (
            SELECT DISTINCT ON (m, a, plan_id, z) *
            FROM pf_sums
            ORDER BY m, a, plan_id, z, calc_date DESC
        ),
        -- Кандидаты — все ПФКСС артикула+плана, выбор — окном. Не LATERAL: тот
        -- перебирал ПФКСС для каждого из десятков тысяч заданий ФКСС выборки
        -- (13,8 с за все годы против 0,7 с у хеш-соединения).
        cand AS (
            SELECT fk.*, p.zadanie AS pf_zadanie, p.z AS pf_z, p.calc_date AS pf_calc_date,
                   p.osn AS pf_osn, p.vsp AS pf_vsp,
                   CASE WHEN p.z = fk.z THEN 'exact' ELSE 'fallback' END AS match,
                   count(*) OVER w AS pf_tasks,
                   row_number() OVER (w ORDER BY (p.z = fk.z) DESC, p.cost DESC NULLS LAST, p.z) AS rn
            FROM fk
            JOIN pf p ON p.m = trim(fk.model) AND p.a = trim(fk.articul)
                     AND p.plan_id = trim(fk.plan_id)
            WINDOW w AS (PARTITION BY fk.model, fk.articul, fk.plan_id, fk.zadanie, fk.calc_date)
        ),
        pairs AS MATERIALIZED (
            SELECT * FROM cand WHERE rn = 1
        )"""


def _fk_where(filters: dict, params: list) -> str:
    """Условия выборки ФКСС — как у листа отклонений margin.deviations()."""
    return " AND ".join([
        *_MARGIN_BASE,
        *_collect_params(filters, params, MARGIN_FILTERS).values(),
        *(c.replace(_DATE, "production_date")
          for c in _collect_params(filters, params, DATE_EXPRS).values()),
    ])


def _f(v) -> float:
    return 0.0 if v is None else float(v)


def _rel(a: float, b: float) -> float | None:
    """Отклонение «ФКСС / ПФКСС − 1» в процентах; база 0 — не определено."""
    return None if not b else 100.0 * (a / b - 1)


def _is_suspect(fk_mat: float, pf_mat: float) -> bool:
    if fk_mat <= 0 or pf_mat <= 0:
        return fk_mat != pf_mat
    ratio = fk_mat / pf_mat
    return ratio > SUSPECT_RATIO or ratio < 1 / SUSPECT_RATIO


def _task_row(r: dict) -> dict:
    fk_osn, fk_vsp = _f(r.get("fk_osn")), _f(r.get("fk_vsp"))
    pf_osn, pf_vsp = _f(r.get("pf_osn")), _f(r.get("pf_vsp"))
    fk_mat, pf_mat = fk_osn + fk_vsp, pf_osn + pf_vsp
    vol = _f(r.get("vol"))
    return {
        "brand_manager": r.get("brand_manager") or "",
        "model": (r.get("model") or "").strip(),
        "articul": (r.get("articul") or "").strip(),
        "name": (r.get("model_name") or "").strip(),
        "plan_id": (r.get("plan_id") or "").strip(),
        "zadanie": r.get("z") or "",
        "pf_zadanie": r.get("pf_z") or "",
        "match": r.get("match"),
        "pf_tasks": int(r.get("pf_tasks") or 0),
        "calc_date": r.get("calc_date"),
        "pf_calc_date": r.get("pf_calc_date"),
        "vol": vol,
        "fk_osn": fk_osn, "pf_osn": pf_osn,
        "d_osn": fk_osn - pf_osn, "d_osn_pct": _rel(fk_osn, pf_osn),
        "fk_vsp": fk_vsp, "pf_vsp": pf_vsp,
        "d_vsp": fk_vsp - pf_vsp, "d_vsp_pct": _rel(fk_vsp, pf_vsp),
        "fk_mat": fk_mat, "pf_mat": pf_mat,
        "d_mat": fk_mat - pf_mat, "d_mat_pct": _rel(fk_mat, pf_mat),
        # Отклонение на весь выпуск задания — во что разница материалов
        # обошлась в деньгах; по нему список сортируется по умолчанию.
        "d_mat_vol": vol * (fk_mat - pf_mat),
        "suspect": _is_suspect(fk_mat, pf_mat),
    }


def empty_result() -> dict:
    """Ответ той же формы без данных — для COST_MOCK=1."""
    return {
        "rows": [], "truncated": False, "row_limit": ROW_LIMIT,
        "coverage": {"fk_tasks": 0, "fk_vol": 0, "pair_tasks": 0, "pair_vol": 0,
                     "exact": 0, "fallback": 0},
        "suspect_ratio": SUSPECT_RATIO, "cache_refreshed_at": None,
    }


async def compare(filters: dict) -> dict:
    """Пары ФКСС↔ПФКСС выборки с суммами материалов единицы и отклонениями."""
    filters = {k: v for k, v in filters.items() if v}
    params: list = []
    sql = f"""
        WITH {_pairs_ctes(_fk_where(filters, params))},
        -- Суммы ФКСС — только по заданиям, к которым нашлась ПФКСС: строк ФКСС
        -- в кэше под миллион, а пар — сотни.
        fk_sums AS (
            SELECT c."Модель" AS model, c."Артикул" AS articul,
                   coalesce(c."PLAN_ID", '') AS plan_id,
                   c."дата расчета" AS calc_date,
                   c."Номер задания производства" AS zadanie,
                   sum(coalesce(c."Основные материалы, руб.", 0)) AS osn,
                   sum(coalesce(c."Вспомогательные материалы, руб.", 0)) AS vsp
            FROM cost_data_all c
            WHERE c."Признак калькуляции" = 'ФКСС'
              AND EXISTS (SELECT 1 FROM pairs p
                          WHERE p.model = c."Модель" AND p.articul = c."Артикул")
            GROUP BY 1, 2, 3, 4, 5
        ),
        out AS (
            SELECT p.brand_manager, p.model, p.articul, p.model_name, p.plan_id,
                   p.z, p.pf_z, p.match, p.pf_tasks, p.vol,
                   p.calc_date::date AS calc_date, p.pf_calc_date::date AS pf_calc_date,
                   s.osn AS fk_osn, s.vsp AS fk_vsp, p.pf_osn, p.pf_vsp
            FROM pairs p
            JOIN fk_sums s
              ON s.model = p.model AND s.articul = p.articul AND s.plan_id = p.plan_id
             AND s.calc_date = p.calc_date AND s.zadanie IS NOT DISTINCT FROM p.zadanie
            ORDER BY p.brand_manager, p.model, p.articul, p.z
            LIMIT {ROW_LIMIT + 1}
        )
        SELECT json_build_object(
            'rows', coalesce((SELECT json_agg(o) FROM out o), '[]'::json),
            'fk_tasks', (SELECT count(*) FROM fk),
            'fk_vol', (SELECT sum(vol) FROM fk),
            'pair_tasks', (SELECT count(*) FROM pairs),
            'pair_vol', (SELECT sum(vol) FROM pairs),
            'exact', (SELECT count(*) FROM pairs WHERE match = 'exact'),
            'fallback', (SELECT count(*) FROM pairs WHERE match = 'fallback')
        )
    """
    async with pool().acquire() as conn:
        raw = _as_dict(await conn.fetchval(sql, *params)) or {}
        status = await conn.fetchrow("SELECT refreshed_at FROM cost_cache_status WHERE id = 1")
    rows = raw.get("rows") or []
    refreshed = status["refreshed_at"] if status else None
    return {
        "rows": [_task_row(r) for r in rows[:ROW_LIMIT]],
        "truncated": len(rows) > ROW_LIMIT,
        "row_limit": ROW_LIMIT,
        "coverage": {
            "fk_tasks": raw.get("fk_tasks") or 0,
            "fk_vol": _f(raw.get("fk_vol")),
            "pair_tasks": raw.get("pair_tasks") or 0,
            "pair_vol": _f(raw.get("pair_vol")),
            "exact": raw.get("exact") or 0,
            "fallback": raw.get("fallback") or 0,
        },
        "suspect_ratio": SUSPECT_RATIO,
        "cache_refreshed_at": refreshed.isoformat() if refreshed else None,
    }


def _type_kind(mat_type: str) -> str | None:
    """Основной или вспомогательный материал по типу строки (легаси-коды источника
    и канонические значения редактора); None — тип не материальный или незнакомый."""
    t = (mat_type or "").strip()
    if t in _PLAN_OSN_TYPES:
        return "osn"
    if t in _PLAN_VSP_TYPES:
        return "vsp"
    return None


async def material_lines(model: str, articul: str, plan_id: str, zadanie: str,
                         calc_date: str | None = None) -> dict:
    """Построчное сравнение материалов одного задания ФКСС и подобранной ПФКСС.

    Материалы сопоставляются по наименованию и артикулу материала, без свойств:
    между этапами свойство меняется («5,0 см соединитель» в ФКСС против «4,0 см»
    в ПФКСС у 117915), и полный ключ показал бы один материал двумя строками —
    «пропал» и «появился». Свойства обеих сторон выводятся рядом. Строки одного
    наименования внутри калькуляции складываются.

    Суммы разносятся ПО КОЛОНКАМ, как в compare(): «Основные материалы» строки —
    в группу основных, «Вспомогательные» — во вспомогательные, независимо от типа
    строки. В источнике есть строки, у которых заполнены обе колонки (у 8
    калькуляций ПФКСС от 30.07.2026 — 122 строки типа «всп», проверено 01.10.2026),
    и разнос по одному типу терял вторую сумму: итог раскрытия не сходился со
    строкой списка. Норма идёт в группу вида по типу строки; такие строки
    помечаются dual, интерфейс об этом говорит.

    calc_date — дата расчёта строки списка: compare() отдаёт строку на каждую
    дату расчёта задания, и раскрытие должно показывать ту же дату. Без неё —
    последняя.
    """
    params: list = [model.strip(), articul.strip(), plan_id.strip(), zadanie.strip()]
    fk_where = (
        "calc_sign = 'ФКСС' AND trim(model) = $1 AND trim(articul) = $2"
        " AND coalesce(trim(plan_id), '') = $3 AND coalesce(trim(zadanie), '') = $4"
    )
    day = None
    if calc_date:
        try:
            day = date.fromisoformat(str(calc_date)[:10])
        except ValueError:
            day = None
    if day is not None:
        params.append(day)
        fk_where += " AND calc_date::date = $5"
    sql = f"""
        WITH {_pairs_ctes(fk_where)},
        pair AS (SELECT * FROM pairs ORDER BY calc_date DESC LIMIT 1)
        SELECT 'fk' AS side, c."Материал/операция/декор(призн)" AS mat_type,
               trim(coalesce(c."Наименование", '')) AS name,
               trim(coalesce(c."артикул материала", '')) AS art,
               concat_ws(', ', nullif(trim(c."свойство1"), ''), nullif(trim(c."свойство2"), ''),
                         nullif(trim(c."свойство3"), '')) AS props,
               c."Норма" AS norm,
               c."Основные материалы, руб." AS osn, c."Вспомогательные материалы, руб." AS vsp
        FROM cost_data_all c, pair p
        WHERE c."Признак калькуляции" = 'ФКСС'
          AND c."Модель" = p.model AND c."Артикул" = p.articul
          AND coalesce(c."PLAN_ID", '') = p.plan_id
          AND c."дата расчета" = p.calc_date
          AND c."Номер задания производства" IS NOT DISTINCT FROM p.zadanie
        UNION ALL
        SELECT 'pf', c."Материал/операция/декор(призн)",
               trim(coalesce(c."Наименование", '')), trim(coalesce(c."артикул материала", '')),
               concat_ws(', ', nullif(trim(c."свойство1"), ''), nullif(trim(c."свойство2"), ''),
                         nullif(trim(c."свойство3"), '')),
               c."Норма", c."Основные материалы, руб.", c."Вспомогательные материалы, руб."
        FROM cost_data_all c, pair p
        WHERE c."Признак калькуляции" = 'ПФКСС'
          AND trim(c."Модель") = trim(p.model) AND trim(c."Артикул") = trim(p.articul)
          AND coalesce(trim(c."PLAN_ID"), '') = trim(p.plan_id)
          AND c."дата расчета" = p.pf_calc_date
          AND c."Номер задания производства" IS NOT DISTINCT FROM p.pf_zadanie
    """
    head_sql = f"""
        WITH {_pairs_ctes(fk_where)}
        SELECT z, pf_z, match, pf_tasks, calc_date::date AS calc_date,
               pf_calc_date::date AS pf_calc_date
        FROM pairs ORDER BY calc_date DESC LIMIT 1
    """
    async with pool().acquire() as conn:
        head = await conn.fetchrow(head_sql, *params)
        lines = await conn.fetch(sql, *params) if head else []
    if not head:
        return {"pair": None, "rows": []}

    groups: dict[tuple[str, str, str], dict] = {}
    totals = {"fk_osn": 0.0, "pf_osn": 0.0, "fk_vsp": 0.0, "pf_vsp": 0.0}
    for ln in lines:
        side = ln["side"]
        osn, vsp = _f(ln["osn"]), _f(ln["vsp"])
        type_kind = _type_kind(ln["mat_type"])
        # Доли строки по колонкам. Обе пустые — строка материала с нулевой
        # суммой: показываем её по типу, иначе она бы пропала из сравнения.
        parts = [(k, a) for k, a in (("osn", osn), ("vsp", vsp)) if a]
        if not parts:
            if type_kind is None:
                continue
            parts = [(type_kind, 0.0)]
        norm_kind = type_kind or parts[0][0]
        dual = len(parts) > 1
        for kind, amount in parts:
            g = groups.setdefault((kind, ln["name"], ln["art"]), {
                "kind": kind, "name": ln["name"], "art": ln["art"],
                "fk_props": set(), "pf_props": set(), "dual": False,
                "fk_norm": None, "pf_norm": None, "fk_sum": None, "pf_sum": None,
            })
            if ln["props"]:
                g[f"{side}_props"].add(ln["props"])
            g[f"{side}_sum"] = (g[f"{side}_sum"] or 0.0) + amount
            totals[f"{side}_{kind}"] += amount
            if kind == norm_kind and ln["norm"] is not None:
                g[f"{side}_norm"] = (g[f"{side}_norm"] or 0.0) + float(ln["norm"])
            g["dual"] = g["dual"] or dual

    # На каком этапе материал есть вообще — в любой статье. Часть строки с двумя
    # заполненными колонками попадает в группу «чужой» статьи, где у другого
    # этапа суммы нет; «только в ПФКСС» там было бы неправдой: материал в ФКСС
    # есть, только в другой статье (status = split).
    present = {"fk": set(), "pf": set()}
    for g in groups.values():
        for side in ("fk", "pf"):
            if g[f"{side}_sum"] is not None:
                present[side].add((g["name"], g["art"]))

    def _status(g) -> str:
        fk_has, pf_has = g["fk_sum"] is not None, g["pf_sum"] is not None
        if fk_has and pf_has:
            return "both"
        other = "pf" if fk_has else "fk"
        if (g["name"], g["art"]) in present[other]:
            return "split"
        # Материал есть только на одном этапе — это тоже отклонение, и его
        # надо видеть, а не терять в «пусто».
        return "fk_only" if fk_has else "pf_only"

    rows = []
    for g in groups.values():
        fk_sum, pf_sum = g["fk_sum"], g["pf_sum"]
        price = lambda s, n: (s / n) if s is not None and n else None  # noqa: E731
        rows.append({
            "kind": g["kind"], "name": g["name"], "art": g["art"],
            "fk_props": "; ".join(sorted(g["fk_props"])),
            "pf_props": "; ".join(sorted(g["pf_props"])),
            "fk_norm": g["fk_norm"], "pf_norm": g["pf_norm"],
            "fk_price": price(fk_sum, g["fk_norm"]), "pf_price": price(pf_sum, g["pf_norm"]),
            "fk_sum": fk_sum, "pf_sum": pf_sum,
            "d_sum": None if fk_sum is None or pf_sum is None else fk_sum - pf_sum,
            "d_sum_pct": None if fk_sum is None or pf_sum is None else _rel(fk_sum, pf_sum),
            "dual": g["dual"],
            "status": _status(g),
        })
    # Сначала основные, внутри — по величине расхождения: читают «где разошлось».
    rows.sort(key=lambda r: (r["kind"] != "osn",
                             -abs((r["fk_sum"] or 0.0) - (r["pf_sum"] or 0.0)), r["name"]))
    return {
        "pair": {
            "zadanie": head["z"], "pf_zadanie": head["pf_z"], "match": head["match"],
            "pf_tasks": head["pf_tasks"],
            "calc_date": head["calc_date"].isoformat() if head["calc_date"] else None,
            "pf_calc_date": head["pf_calc_date"].isoformat() if head["pf_calc_date"] else None,
        },
        "rows": rows,
        # Итоги раскрытия по колонкам — те же суммы, что у строки списка
        # (compare); интерфейс показывает их строкой «Итого».
        "totals": totals,
    }
