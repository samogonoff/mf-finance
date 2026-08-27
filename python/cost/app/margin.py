"""Дашборд «Маржа выпуска» — перенос двух отчётов Power BI заказчика
(«Маржа выпуска ЧНИ», «Маржа выпуска Трикотаж») на витрину проекта.

Что переносится и что нет — docs/bi/pbi-margin-port.md. Формулы — docs/bi/metrics.md.

ОДИН ЗАПРОС НА ВЕСЬ ДАШБОРД
===========================
Как и в commercial.py: плитки, динамика, разрезы, матрица и варианты фильтров
приходят одним SQL. Обе валюты в ответе сразу — переключатель BYN/USD на фронте
на сервер не ходит.

СРАВНЕНИЕ С ПРОШЛЫМ МЕСЯЦЕМ И ГОДОМ — СДВИНУТОЕ ОБЪЕДИНЕНИЕ
==========================================================
В Power BI «предыдущий месяц» и «предыдущий год» — это DATEADD по календарю:
набор дат из фильтра сдвигается целиком. Выбран июль 2026 → сравниваем с июнем
2026 и июлем 2025. Выбран 2026 год без месяца → с декабрём 2025…июлем 2026 и с
2025 годом. То же самое здесь делает объединение трёх копий строк:

    cur — строка как есть, метка месяца m;
    pm  — та же строка с меткой m + 1 месяц («она же — прошлый месяц для m+1»);
    py  — та же строка с меткой m + 1 год.

Фильтр по году и месяцу накладывается на МЕТКУ, и любой агрегат с
`FILTER (WHERE k = 'py')` даёт значение прошлого года ровно для выбранного
периода — на любом уровне группировки, с любыми фильтрами по измерениям, без
оконных функций и самосоединений. Тот же приём воспроизводится в Superset
обычными метриками датасета (см. swarm/superset/README.md).

Одно отличие от DATEADD: копии с меткой позже последнего месяца с выпуском
отбрасываются. Иначе «2026 год» при восьми месяцах данных сравнивался бы с
ПОЛНЫМ 2025-м — Power BI так и делает, и для сумм это нечестно. Здесь январь-
август 2026 сравнивается с январём-августом 2025.

ЧТО СЧИТАЕТСЯ ВЫПУСКОМ
======================
Только ФКСС (решение заказчика 14.08.2026) и только строки с объёмом: без тиража
маржи выпуска нет. В Лисе (источник Power BI) признака калькуляции не было —
там выпуск и есть факт производства. У нас на модель-артикул четыре этапа, и без
этого условия объём учетверился бы.

Проценты (маржинальность, рентабельность, отклонения) считаются в BYN при любой
валюте: доллар у нас пересчитан по курсу на дату расчёта строки, и отношение
сумм в USD отличается от BYN на десятые доли процента — переключатель валюты,
меняющий проценты, читался бы как ошибка. Валюта переключает только суммы.
"""

from __future__ import annotations

from .commercial import (
    BASE_CONDITIONS,
    FILTER_OPTIONS_LIMIT,
    FILTERS,
    VOLUME_SIGN,
    _as_dict,
    _collect_params,
)
from .db import pool

# Фильтры дашборда: те же измерения витрины, что у коммерческого (белый список
# оттуда же), кроме признака калькуляции — здесь он всегда VOLUME_SIGN.
MARGIN_FILTERS: dict[str, str] = {k: v for k, v in FILTERS.items() if k != "calc_sign"}

# Год и месяц накладываются на метку месяца, а у метки в разных частях запроса
# разное имя: в каскаде вариантов это production_date витрины, в агрегатах —
# колонка m сдвинутого объединения. Токен подменяется перед сборкой.
_DATE = "__DATE__"
DATE_EXPRS: dict[str, str] = {
    "year": f"to_char({_DATE}, 'YYYY')",
    "month": f"to_char({_DATE}, 'MM')",
}

FILTER_KEYS: tuple[str, ...] = (*MARGIN_FILTERS, *DATE_EXPRS)

# Иерархия матрицы — как в Power BI («Модели для продаж и ост.» Level 02…06 →
# артикул → № задания; их Level 02 — это наш level01). Проваливание — клик по
# строке, путь приходит в matrix_path. Как и у структуры себестоимости в
# commercial.py, путь применяется только к матрице и водопадам, а не ко всему
# дашборду.
MATRIX_LEVELS: list[tuple[str, str]] = [
    ("level01", "Level 01"),
    ("level02", "Level 02"),
    ("level03", "Level 03"),
    ("level04", "Level 04"),
    ("level05", "Level 05"),
    ("articul", "Артикул"),
    ("zadanie", "№ задания"),
]
MATRIX_ROW_LIMIT = 300

# Выпуск — только строки с объёмом и только по признаку выпуска. Остальные
# условия общие с коммерческим дашбордом (без цены и себестоимости наценки нет,
# выбросы, полуфабрикаты).
_MARGIN_BASE = (
    *BASE_CONDITIONS,
    f"calc_sign = '{VOLUME_SIGN}'",
    "volume_pcs > 0",
    "production_date IS NOT NULL",
)

# Измерения, которые несут копии строк. Перечислены явно: `s.*` в UNION ALL
# продублировал бы колонку m.
_DIM_COLS = (
    "brand_manager", "level01", "level02", "level03", "level04", "level05",
    "model_name", "model", "articul", "country", "season", "zadanie",
)
# Величины, которые суммируются. *_b — BYN, *_u — USD.
_VALUE_COLS = (
    "vol", "rev_b", "rev_u", "cost_b", "cost_u", "raw_b", "raw_u",
    # Минуты пошива на выпуск и объём тех строк, где минуты известны — иначе
    # «минут на штуку» занизят строки без минут (их 56%).
    "sew_min", "sew_vol",
)
_PERIODS = (("", "cur"), ("_pm", "pm"), ("_py", "py"))


def _sums() -> str:
    """Суммы величин по трём периодам + счётчики и целевая маржинальность."""
    parts = []
    for suffix, k in _PERIODS:
        parts += [f"sum({c}) FILTER (WHERE k = '{k}') AS {c}{suffix}" for c in _VALUE_COLS]
    parts += [
        "count(*) FILTER (WHERE k = 'cur')              AS calc_count",
        "count(DISTINCT model) FILTER (WHERE k = 'cur') AS model_count",
        # Норма маржинальности — из cost_margin_targets по Level 01, взвешенная
        # по выручке. Строки без нормы (0 в таблице = не задана) не входят ни в
        # числитель, ни в знаменатель; доля выручки с нормой отдаётся отдельно,
        # чтобы «норма 48%» при покрытии 30% не читалась как норма для всего.
        "sum(rev_b * target_frac) FILTER (WHERE k = 'cur' AND target_frac IS NOT NULL) AS target_num",
        "sum(rev_b)               FILTER (WHERE k = 'cur' AND target_frac IS NOT NULL) AS target_den",
    ]
    return ",\n            ".join(parts)


def _ratio(num, den):
    if num is None or den is None or den == 0:
        return None
    return float(num) / float(den)


def _pct(num, den):
    r = _ratio(num, den)
    return None if r is None else 100.0 * r


def _sub(a, b):
    if a is None or b is None:
        return None
    return float(a) - float(b)


def _derive(row: dict) -> dict:
    """Производные показатели из сумм — формулы из docs/bi/metrics.md.

    Считаются здесь один раз, а не на фронте: у фронта и у Superset формулы
    должны совпадать с одним источником, и этот источник — документ, а не два
    независимых набора кода.
    """
    out = dict(row)
    for suffix, _ in _PERIODS:
        vol = row.get(f"vol{suffix}")
        for c in ("b", "u"):
            cur = "byn" if c == "b" else "usd"
            rev, cost, raw = (row.get(f"{x}_{c}{suffix}") for x in ("rev", "cost", "raw"))
            margin = _sub(rev, cost)
            out[f"rev_{cur}{suffix}"] = None if rev is None else float(rev)
            out[f"cost_{cur}{suffix}"] = None if cost is None else float(cost)
            out[f"raw_{cur}{suffix}"] = None if raw is None else float(raw)
            out[f"margin_{cur}{suffix}"] = margin
            out[f"unit_cost_{cur}{suffix}"] = _ratio(cost, vol)
            out[f"unit_price_{cur}{suffix}"] = _ratio(rev, vol)
            out[f"unit_raw_{cur}{suffix}"] = _ratio(raw, vol)
            out[f"unit_margin_{cur}{suffix}"] = _ratio(margin, vol)
            # Рекомендуемая цена (Power BI «Рекомендуемая цена ОЦ»): если
            # маржинальность ниже нормы — цена, при которой норма достигается,
            # иначе текущая средняя отпускная. Норма — взвешенная по строкам.
            out.pop(f"rev_{c}{suffix}", None)
            out.pop(f"cost_{c}{suffix}", None)
            out.pop(f"raw_{c}{suffix}", None)
        rev_b, cost_b = row.get(f"rev_b{suffix}"), row.get(f"cost_b{suffix}")
        margin_b = _sub(rev_b, cost_b)
        # Проценты — всегда в BYN, см. докстринг модуля.
        out[f"margin_pct{suffix}"] = _pct(margin_b, rev_b)
        out[f"profit_pct{suffix}"] = _pct(margin_b, cost_b)
        out[f"min_per_unit{suffix}"] = _ratio(row.get(f"sew_min{suffix}"), row.get(f"sew_vol{suffix}"))
        out[f"vol{suffix}"] = None if vol is None else float(vol)
        out.pop(f"sew_min{suffix}", None)
        out.pop(f"sew_vol{suffix}", None)

    # Отклонения: маржинальность — в процентных пунктах, суммы — в валюте,
    # себестоимость штуки — в процентах к прошлому периоду.
    for suffix in ("_pm", "_py"):
        out[f"margin_pct_dev{suffix}"] = _sub(out["margin_pct"], out[f"margin_pct{suffix}"])
        for cur in ("byn", "usd"):
            out[f"margin_dev_{cur}{suffix}"] = _sub(out[f"margin_{cur}"], out[f"margin_{cur}{suffix}"])
            prev = out[f"unit_cost_{cur}{suffix}"]
            out[f"unit_cost_dev_pct_{cur}{suffix}"] = (
                None if prev in (None, 0) or out[f"unit_cost_{cur}"] is None
                else 100.0 * (out[f"unit_cost_{cur}"] / prev - 1)
            )
            prev_raw = out[f"unit_raw_{cur}{suffix}"]
            out[f"unit_raw_dev_pct_{cur}{suffix}"] = (
                None if prev_raw in (None, 0) or out[f"unit_raw_{cur}"] is None
                else 100.0 * (out[f"unit_raw_{cur}"] / prev_raw - 1)
            )
        # Темп роста маржи (Power BI «Темп роста к прошлому году»): маржа /
        # маржа прошлого периода − 1. При отрицательной или нулевой базе
        # отношение теряет смысл — отдаём пусто, а не сотни процентов.
        base = out[f"margin_byn{suffix}"]
        out[f"growth_pct{suffix}"] = (
            None if base is None or base <= 0 or out["margin_byn"] is None
            else 100.0 * (out["margin_byn"] / base - 1)
        )

    # Норма маржинальности и её покрытие.
    out["target_pct"] = _pct(row.get("target_num"), row.get("target_den"))
    # Покрытие нормой: NULL из пустого FILTER — это ноль покрытия, а не «неизвестно».
    out["target_coverage_pct"] = _pct(row.get("target_den") or 0, row.get("rev_b"))
    out["margin_pct_dev_target"] = _sub(out["margin_pct"], out["target_pct"])
    # Ниже нормы — только если норма известна.
    out["below_target"] = (
        out["target_pct"] is not None and out["margin_pct"] is not None
        and out["margin_pct"] < out["target_pct"]
    )
    for k in ("target_num", "target_den"):
        out.pop(k, None)
    return out


async def _max_year(conn) -> str | None:
    return await conn.fetchval(
        f"SELECT to_char(max(production_date), 'YYYY') FROM cost_calc_mv WHERE {' AND '.join(_MARGIN_BASE)}"
    )


async def dashboard(
    filters: dict,
    matrix_path: list[str] | None = None,
    default_period: bool = False,
) -> dict:
    """Весь дашборд одним запросом.

    default_period — если год не выбран, взять последний год с выпуском.
    Сравнения «с прошлым месяцем/годом» без ограниченного периода определены,
    но бессмысленны, поэтому первая загрузка страницы просит период по
    умолчанию; дальше фильтры приходят явно, и снятый год означает «все годы».
    """
    filters = {k: v for k, v in filters.items() if v}
    year_defaulted = False

    async with pool().acquire() as conn:
        if default_period and not filters.get("year"):
            latest = await _max_year(conn)
            if latest:
                filters["year"] = [latest]
                year_defaulted = True

        params: list = []
        dim_conditions = _collect_params(filters, params, MARGIN_FILTERS)
        date_conditions = _collect_params(filters, params, DATE_EXPRS)

        def where(exclude: str | None = None, with_dates: bool = True) -> str:
            """Условия по витрине. with_dates=False — без года и месяца: так
            строится src, потому что период накладывается на МЕТКУ сдвинутого
            объединения (см. докстринг модуля), а не на production_date —
            иначе в src не попадут строки прошлого месяца и прошлого года, и
            сравнивать будет нечего."""
            parts = list(_MARGIN_BASE)
            parts += [c for k, c in dim_conditions.items() if k != exclude]
            if with_dates:
                parts += [c.replace(_DATE, "production_date")
                          for k, c in date_conditions.items() if k != exclude]
            return " AND ".join(parts)

        # Условия по метке месяца сдвинутого объединения.
        def on_label(exclude: str | None = None) -> str:
            parts = [c.replace(_DATE, "m") for k, c in date_conditions.items() if k != exclude]
            return (" WHERE " + " AND ".join(parts)) if parts else ""

        # Проваливание по матрице: пустое значение — это NULL уровня (у 31 721
        # калькуляции нет level01), иначе строки без уровня было бы не открыть.
        path = [str(v) for v in (matrix_path or [])][: len(MATRIX_LEVELS) - 1]
        matrix_dim = MATRIX_LEVELS[len(path)][0]
        matrix_conditions = []
        for (level, _), value in zip(MATRIX_LEVELS, path):
            params.append(value)
            matrix_conditions.append(f"coalesce({level}, '') = ${len(params)}")
        matrix_where = "".join(f" AND {c}" for c in matrix_conditions)
        # На уровнях артикула и задания сам ярлык нечитаем — добавляем имя товара.
        matrix_name = (
            "min(model_name)" if matrix_dim in ("articul", "zadanie") else "NULL::text"
        )

        # Каскад вариантов фильтров — как в commercial.py: каждый список считается
        # без собственного условия, по строкам выпуска.
        all_exprs = {**MARGIN_FILTERS, **{k: v.replace(_DATE, "production_date") for k, v in DATE_EXPRS.items()}}
        option_selects = ",\n            ".join(
            f"""'{key}', (SELECT coalesce(json_agg(v ORDER BY v), '[]'::json) FROM
                (SELECT DISTINCT {expr} AS v FROM cost_calc_mv
                 WHERE {where(exclude=key)} AND {expr} IS NOT NULL
                 ORDER BY 1 LIMIT {FILTER_OPTIONS_LIMIT + 1}) o_{i})"""
            for i, (key, expr) in enumerate(all_exprs.items())
        )

        dims = ", ".join(_DIM_COLS)
        values = ", ".join(_VALUE_COLS)
        sums = _sums()

        sql = f"""
        WITH src AS MATERIALIZED (
            SELECT
                date_trunc('month', c.production_date)::date AS m,
                {dims},
                volume_pcs                                              AS vol,
                volume_pcs * wholesale_price_byn                        AS rev_b,
                volume_pcs * wholesale_price_usd                        AS rev_u,
                volume_pcs * cost_byn                                   AS cost_b,
                volume_pcs * cost_usd                                   AS cost_u,
                volume_pcs * (coalesce(mat_main_byn, 0) + coalesce(mat_aux_byn, 0)) AS raw_b,
                volume_pcs * (coalesce(mat_main_usd, 0) + coalesce(mat_aux_usd, 0)) AS raw_u,
                volume_pcs * sewing_minutes                             AS sew_min,
                CASE WHEN sewing_minutes IS NOT NULL THEN volume_pcs END AS sew_vol,
                t.target_margin_pct / 100.0                             AS target_frac
            FROM cost_calc_mv c
            LEFT JOIN cost_margin_targets t
                   ON t.level1 = c.level01 AND t.target_margin_pct > 0
            WHERE {where(with_dates=False)}
        ),
        -- Горизонт данных: последний месяц с выпуском по всей витрине (без фильтров
        -- измерений — иначе у бренд-менеджера, чей выпуск кончился в марте, горизонт
        -- уехал бы на март).
        horizon AS (
            SELECT date_trunc('month', max(production_date))::date AS m
            FROM cost_calc_mv WHERE {' AND '.join(_MARGIN_BASE)}
        ),
        -- Три копии строк: текущая и сдвинутые вперёд на месяц и на год. Фильтр
        -- по году/месяцу ниже накладывается на метку m — см. докстринг модуля.
        --
        -- Копии с меткой ПОЗЖЕ горизонта отбрасываются: сравнивать их не с чем, а
        -- без отсечки «2026 год» сравнивался бы с ПОЛНЫМ 2025-м при восьми
        -- месяцах текущего. С отсечкой — январь-август 2026 против января-августа
        -- 2025 и против декабря 2025 - июля 2026. Та же отсечка в витрине Superset.
        shifted AS MATERIALIZED (
            SELECT * FROM (
                SELECT m,                                'cur' AS k, {dims}, {values}, target_frac FROM src
                UNION ALL
                SELECT (m + interval '1 month')::date,  'pm',       {dims}, {values}, target_frac FROM src
                UNION ALL
                SELECT (m + interval '1 year')::date,   'py',       {dims}, {values}, target_frac FROM src
            ) u
            WHERE u.m <= (SELECT m FROM horizon)
        ),
        period AS (SELECT * FROM shifted{on_label()}),
        tiles AS (
            SELECT {sums}
            FROM period
        ),
        -- Динамика по месяцам: фильтр «месяц» не применяется (это и есть разрез
        -- по месяцам), «год» применяется и задаёт окно. Месяцы без текущих
        -- данных (только сдвинутые копии) не показываем.
        months AS (
            SELECT to_char(m, 'YYYY-MM') AS ym, {sums}
            FROM shifted{on_label(exclude="month")}
            GROUP BY m HAVING bool_or(k = 'cur')
            ORDER BY m
        ),
        -- Бренд-менеджеры: текущий период против прошлого года. Те, у кого
        -- есть только прошлогодний выпуск, остаются — исчезнувший выпуск тоже
        -- информация.
        by_bm AS (
            SELECT coalesce(brand_manager, '') AS label, {sums}
            FROM period
            GROUP BY 1 HAVING bool_or(k IN ('cur', 'py'))
            ORDER BY sum(rev_b - cost_b) FILTER (WHERE k = 'cur') DESC NULLS LAST
        ),
        by_level01 AS (
            SELECT coalesce(level01, '') AS label, {sums}
            FROM period
            GROUP BY 1 HAVING bool_or(k = 'cur')
            ORDER BY sum(rev_b - cost_b) FILTER (WHERE k = 'cur') DESC NULLS LAST
        ),
        matrix AS (
            SELECT coalesce({matrix_dim}, '') AS label, {matrix_name} AS name, {sums}
            FROM period
            WHERE TRUE{matrix_where}
            GROUP BY 1 HAVING bool_or(k = 'cur')
            ORDER BY sum(rev_b - cost_b) FILTER (WHERE k = 'cur') DESC NULLS LAST
            LIMIT {MATRIX_ROW_LIMIT + 1}
        )
        SELECT json_build_object(
            'tiles',      (SELECT row_to_json(t) FROM tiles t),
            'months',     coalesce((SELECT json_agg(x) FROM months x), '[]'::json),
            'by_bm',      coalesce((SELECT json_agg(x) FROM by_bm x), '[]'::json),
            'by_level01', coalesce((SELECT json_agg(x) FROM by_level01 x), '[]'::json),
            'matrix',     coalesce((SELECT json_agg(x) FROM matrix x), '[]'::json),
            'options',    json_build_object(
                {option_selects}
            )
        ) AS payload
        """
        row = await conn.fetchrow(sql, *params)
        status = await conn.fetchrow("SELECT refreshed_at FROM cost_cache_status WHERE id = 1")
        # Полнота: сколько калькуляций выпуска ТОГО ЖЕ периода и тех же фильтров
        # попало в расчёт. Знаменатель без условий на цену, себестоимость и объём
        # — ровно то, что эти условия отсекают. Считать против всей истории
        # нельзя: «24 тыс. из 87 тыс.» для одного года читалось бы как 28%
        # полноты, хотя это просто год против трёх лет.
        total_params: list = []
        total_conditions = [
            f"calc_sign = '{VOLUME_SIGN}'", "production_date IS NOT NULL",
            *_collect_params(filters, total_params, MARGIN_FILTERS).values(),
            *(c.replace(_DATE, "production_date")
              for c in _collect_params(filters, total_params, DATE_EXPRS).values()),
        ]
        total = await conn.fetchval(
            f"SELECT count(*) FROM cost_calc_mv WHERE {' AND '.join(total_conditions)}",
            *total_params,
        )

    payload = _as_dict(row["payload"])
    payload["tiles"] = _derive(payload.get("tiles") or {})
    for key in ("months", "by_bm", "by_level01", "matrix"):
        payload[key] = [_derive(r) for r in payload.get(key) or []]

    matrix_truncated = len(payload["matrix"]) > MATRIX_ROW_LIMIT
    payload["matrix"] = payload["matrix"][:MATRIX_ROW_LIMIT]

    options = payload.get("options") or {}
    options_truncated: list[str] = []
    for key, vals in list(options.items()):
        if vals and len(vals) > FILTER_OPTIONS_LIMIT:
            options_truncated.append(key)
            options[key] = vals[:FILTER_OPTIONS_LIMIT]
    payload["options"] = options

    refreshed = status["refreshed_at"] if status else None
    payload["meta"] = {
        "options_truncated": options_truncated,
        "matrix_truncated": matrix_truncated,
        "matrix_row_limit": MATRIX_ROW_LIMIT,
        # Полнота: сколько калькуляций выпуска в расчёте против всех ФКСС.
        "calc_total": total,
        "cache_refreshed_at": refreshed.isoformat() if refreshed else None,
        "volume_sign": VOLUME_SIGN,
        # Период, к которому относятся плитки, разрезы и матрица; сравнения —
        # с тем же набором месяцев, сдвинутым на месяц и на год назад.
        "period": {"year": filters.get("year") or [], "month": filters.get("month") or []},
        "year_defaulted": year_defaulted,
        # Состояние проваливания по матрице.
        "matrix_dim": matrix_dim,
        "matrix_label": MATRIX_LEVELS[len(path)][1],
        "matrix_path": [
            {"level": MATRIX_LEVELS[i][0], "label": MATRIX_LEVELS[i][1], "value": v}
            for i, v in enumerate(path)
        ],
        "matrix_can_drill": len(path) < len(MATRIX_LEVELS) - 1,
        "matrix_levels": [{"key": k, "label": v} for k, v in MATRIX_LEVELS],
    }
    return payload
