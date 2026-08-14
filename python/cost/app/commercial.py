"""Дашборд «Коммерческая эффективность и ценообразование».

ОДИН ЗАПРОС НА ВЕСЬ ДАШБОРД
===========================
Плитки, три серии и метаданные приходят из одного SQL. Так сделано намеренно:
тот же макет в Superset разошёлся на двадцать с лишним независимых запросов —
по одному на каждый виджет и каждый фильтр, — и каждый заново сканировал
витрину. Здесь один проход по общему CTE.

Обе валюты возвращаются сразу: переключатель BYN/USD на фронте не ходит на
сервер. Данных вдвое больше, но это килобайты.

ФОРМУЛЫ — из docs/bi/metrics.md, а не изобретены здесь
=====================================================
* наценка = отпускная − себестоимость (база — ОТПУСКНАЯ цена, решение
  заказчика 13.08.2026; розничная анализируется отдельно);
* маржинальность = наценка / отпускная, рентабельность = наценка / себестоимость;
* отношения считаются ИЗ АГРЕГАТОВ (Σнаценка/Σотпускная), а не усреднением
  процентов: среднее от отношений не равно отношению средних, и плитки
  перестали бы сходиться между собой;
* денежные плитки — медианы: средние искажены выбросами (единицы калькуляций
  свыше 1000 BYN при медиане около 2 задирают среднее в разы);
* показатели «выпуска» — взвешенные по объёму. Пока `volume_pcs` не наполнен
  (до первого refresh кэша после миграции 0036), они возвращают NULL, и фронт
  обязан это показать, а не подменять невзвешенными.

БАЗА РАСЧЁТА
============
Себестоимость пуста у 36% калькуляций, отпускная цена у 38%. В расчёт попадает
61%, и это возвращается в `meta.calc_total` рядом с `tiles.calc_count` — плитка
«маржинальность 50%» без этой пары читается как картина по всему ассортименту.
"""

from __future__ import annotations

import json

from .db import pool

# Разрешённые измерения кольцевой диаграммы. Белый список обязателен: имя
# колонки подставляется в SQL, а не передаётся параметром.
DIMENSIONS: dict[str, str] = {
    "model_name": "Наименование товара",
    "country": "Страна пр-ва",
    "season": "Сезон",
    "level01": "Level 01",
    "level02": "Level 02",
    "level03": "Level 03",
    "brand_manager": "Бренд-менеджер",
    "calc_sign": "Признак калькуляции",
    "price_level": "Уровень цен",
    "family": "Семья",
    "color": "Цвет",
}

# Меры кольцевой диаграммы. «Выпуск» пуст до первого refresh кэша после
# миграции 0036 — вернёт NULL, и это должно быть видно, а не нарисовано нулём.
MEASURES: dict[str, tuple[str, str]] = {
    "volume": ("Выпуск, шт", "sum(volume_pcs)"),
    "calcs": ("Калькуляций", "count(*)"),
    "models": ("Моделей", "count(DISTINCT model)"),
    "cost": ("Себестоимость, сумма", "sum(cost_b)"),
    "price": ("Отпускная, сумма", "sum(price_b)"),
    "markup": ("Наценка, сумма", "sum(markup_b)"),
}

# Фильтры: ключ запроса → колонка витрины. Тоже белый список.
FILTERS: dict[str, str] = {
    "model_name": "model_name",
    "model": "model",
    "articul": "articul",
    "country": "country",
    "calc_sign": "calc_sign",
    "season": "season",
    "level01": "level01",
    "level02": "level02",
    "level03": "level03",
    "brand_manager": "brand_manager",
    "price_level": "price_level",
}


# Базовые условия выборки, общие для всех запросов дашборда.
BASE_CONDITIONS = (
    # Без цены и себестоимости наценку не посчитать.
    "cost_byn > 0",
    # Отсечение выбросов: их единицы, но при медиане около 2 они задирают суммы.
    "cost_byn < 1000",
    "wholesale_price_byn > 0",
)


def _collect_params(filters: dict, params: list) -> dict[str, str]:
    """Раскладывает значения фильтров по параметрам ОДИН раз и возвращает
    готовые условия по ключам. Один общий список параметров нужен, чтобы
    девять вариантов WHERE (для каскада) не плодили девять копий значений."""
    conditions: dict[str, str] = {}
    for key, column in FILTERS.items():
        values = filters.get(key)
        if not values:
            continue
        params.append(list(values))
        conditions[key] = f'"{column}" = ANY(${len(params)})'

    if filters.get("date_from"):
        params.append(filters["date_from"])
        conditions["date_from"] = f"calc_date >= ${len(params)}"
    if filters.get("date_to"):
        params.append(filters["date_to"])
        conditions["date_to"] = f"calc_date <= ${len(params)}"
    return conditions


def _where(conditions: dict[str, str], exclude: str | None = None) -> str:
    """Собирает WHERE, при желании выбросив условие одного фильтра.

    `exclude` нужен для каскада: варианты фильтра считаются по данным,
    отфильтрованным всеми ОСТАЛЬНЫМИ. Если учитывать и его собственный выбор,
    список схлопнется до уже выбранного, и ни добавить значение, ни снять
    выбор станет нельзя.
    """
    parts = list(BASE_CONDITIONS)
    parts += [c for key, c in conditions.items() if key != exclude]
    return " AND ".join(parts)


def _as_dict(value):
    return json.loads(value) if isinstance(value, str) else value


async def dashboard(
    filters: dict, dimension: str = "model_name", measure: str = "calcs"
) -> dict:
    if dimension not in DIMENSIONS:
        raise ValueError(f"недопустимое измерение: {dimension}")
    if measure not in MEASURES:
        raise ValueError(f"недопустимая мера: {measure}")

    params: list = []
    conditions = _collect_params(filters, params)
    where = _where(conditions)
    measure_expr = MEASURES[measure][1]

    # Каскад: для каждого фильтра свой список вариантов, посчитанный по данным
    # без его собственного условия. Считаем здесь же, в общем запросе, — иначе
    # каждое изменение фильтра стоило бы двух обращений к серверу.
    option_selects = ",\n            ".join(
        f"""'{key}', (SELECT coalesce(json_agg(v ORDER BY v), '[]'::json) FROM
                (SELECT DISTINCT "{col}" AS v FROM cost_calc_mv
                 WHERE {_where(conditions, exclude=key)} AND "{col}" IS NOT NULL
                 ORDER BY 1 LIMIT {FILTER_OPTIONS_LIMIT + 1}) o_{i})"""
        for i, (key, col) in enumerate(FILTERS.items())
    )

    # base MATERIALIZED — чтобы Postgres прошёл по витрине один раз, а не
    # подставлял CTE заново в каждый из четырёх агрегатов.
    sql = f"""
    WITH base AS MATERIALIZED (
        SELECT
            model, articul, model_name, calc_sign, calc_date, season, country,
            level01, level02, level03, brand_manager, price_level, family, color,
            volume_pcs,
            cost_byn                       AS cost_b,
            cost_usd                       AS cost_u,
            wholesale_price_byn            AS price_b,
            wholesale_price_usd            AS price_u,
            retail_price_byn               AS retail_b,
            retail_price_usd               AS retail_u,
            wholesale_price_byn - cost_byn AS markup_b,
            wholesale_price_usd - cost_usd AS markup_u,
            coalesce(mat_main_byn, 0)      AS mat_main_b,
            coalesce(mat_aux_byn, 0)       AS mat_aux_b,
            coalesce(sewing_byn, 0)        AS sewing_b,
            coalesce(cutting_byn, 0)       AS cutting_b,
            coalesce(decor_byn, 0)         AS decor_b,
            coalesce(knitting_byn, 0)      AS knitting_b,
            greatest(cost_byn - (coalesce(mat_main_byn, 0) + coalesce(mat_aux_byn, 0)
                   + coalesce(sewing_byn, 0) + coalesce(cutting_byn, 0)
                   + coalesce(decor_byn, 0) + coalesce(knitting_byn, 0)), 0) AS other_b
        FROM cost_calc_mv
        WHERE {where}
    ),
    tiles AS (
        SELECT
            count(*)                     AS calc_count,
            count(DISTINCT model)        AS model_count,
            sum(volume_pcs)              AS volume_total,
            percentile_cont(0.5) WITHIN GROUP (ORDER BY cost_b)   AS cost_byn,
            percentile_cont(0.5) WITHIN GROUP (ORDER BY cost_u)   AS cost_usd,
            percentile_cont(0.5) WITHIN GROUP (ORDER BY price_b)  AS price_byn,
            percentile_cont(0.5) WITHIN GROUP (ORDER BY price_u)  AS price_usd,
            percentile_cont(0.5) WITHIN GROUP (ORDER BY retail_b) AS retail_byn,
            percentile_cont(0.5) WITHIN GROUP (ORDER BY retail_u) AS retail_usd,
            percentile_cont(0.5) WITHIN GROUP (ORDER BY markup_b) AS markup_byn,
            percentile_cont(0.5) WITHIN GROUP (ORDER BY markup_u) AS markup_usd,
            100.0 * sum(markup_b) / nullif(sum(price_b), 0)        AS margin_pct,
            100.0 * sum(markup_b) / nullif(sum(cost_b), 0)         AS profit_pct,
            100.0 * sum(markup_b * volume_pcs)
                  / nullif(sum(price_b * volume_pcs), 0)          AS margin_pct_w,
            100.0 * sum(markup_b * volume_pcs)
                  / nullif(sum(cost_b * volume_pcs), 0)           AS profit_pct_w,
            max(calc_date)                                        AS last_calc_date
        FROM base
    ),
    -- Сезоны в ХРОНОЛОГИЧЕСКОМ порядке, а не по алфавиту: иначе AW2026 встаёт
    -- раньше SS2021, и ось «динамики» перестаёт быть временной. Внутри года
    -- SS (весна-лето) идёт перед AW (осень-зима).
    --
    -- Сезон «-» (около 5 тыс. калькуляций) периодом не является — ставим его
    -- первым и подписываем на фронте, а не выбрасываем молча.
    seasons AS (
        SELECT season,
               percentile_cont(0.5) WITHIN GROUP (ORDER BY cost_b)   AS cost_byn,
               percentile_cont(0.5) WITHIN GROUP (ORDER BY cost_u)   AS cost_usd,
               percentile_cont(0.5) WITHIN GROUP (ORDER BY price_b)  AS price_byn,
               percentile_cont(0.5) WITHIN GROUP (ORDER BY price_u)  AS price_usd,
               percentile_cont(0.5) WITHIN GROUP (ORDER BY retail_b) AS retail_byn,
               percentile_cont(0.5) WITHIN GROUP (ORDER BY retail_u) AS retail_usd,
               count(*) AS calc_count,
               coalesce(nullif(regexp_replace(season, '\\D', '', 'g'), '')::int, 0) * 10
                 + CASE upper(left(season, 2)) WHEN 'SS' THEN 1 WHEN 'AW' THEN 2 ELSE 0 END
                 AS sort_key
        FROM base WHERE season IS NOT NULL GROUP BY season
    ),
    structure AS (
        SELECT model_name,
               sum(mat_main_b) AS mat_main, sum(mat_aux_b)  AS mat_aux,
               sum(sewing_b)   AS sewing,   sum(cutting_b)  AS cutting,
               sum(decor_b)    AS decor,    sum(knitting_b) AS knitting,
               sum(other_b)    AS other,    sum(cost_b)     AS cost_total
        FROM base WHERE model_name IS NOT NULL
        GROUP BY model_name ORDER BY sum(cost_b) DESC NULLS LAST LIMIT 15
    ),
    ring AS (
        SELECT "{dimension}"::text AS label, ({measure_expr})::numeric AS value
        FROM base WHERE "{dimension}" IS NOT NULL
        GROUP BY 1 ORDER BY 2 DESC NULLS LAST LIMIT 12
    )
    SELECT json_build_object(
        'tiles',     (SELECT row_to_json(t) FROM tiles t),
        'seasons',   coalesce((SELECT json_agg(s ORDER BY s.sort_key, s.season) FROM seasons s), '[]'::json),
        'structure', coalesce((SELECT json_agg(x) FROM structure x), '[]'::json),
        'ring',      coalesce((SELECT json_agg(r) FROM ring r), '[]'::json),
        'options',   json_build_object(
            {option_selects}
        )
    ) AS payload
    """

    async with pool().acquire() as conn:
        row = await conn.fetchrow(sql, *params)
        status = await conn.fetchrow(
            "SELECT refreshed_at FROM cost_cache_status WHERE id = 1"
        )
        total = await conn.fetchval("SELECT count(*) FROM cost_calc_mv")

    payload = _as_dict(row["payload"])

    # Обрезаем длинные списки и запоминаем, какие именно: фильтр с неполным
    # списком должен выглядеть неполным, а не всеобъемлющим.
    options = payload.get("options") or {}
    options_truncated: list[str] = []
    for key, values in list(options.items()):
        if values and len(values) > FILTER_OPTIONS_LIMIT:
            options_truncated.append(key)
            options[key] = values[:FILTER_OPTIONS_LIMIT]
    payload["options"] = options

    refreshed = status["refreshed_at"] if status else None
    payload["meta"] = {
        "options_truncated": options_truncated,
        # Полнота выборки: сколько всего калькуляций против попавших в расчёт.
        "calc_total": total,
        # Штамп свежести: дашборд по устаревшему кэшу врёт молча, цифры
        # выглядят правдоподобно.
        "cache_refreshed_at": refreshed.isoformat() if refreshed else None,
        "dimension": dimension,
        "dimension_label": DIMENSIONS[dimension],
        "measure": measure,
        "measure_label": MEASURES[measure][0],
        "dimensions": [{"key": k, "label": v} for k, v in DIMENSIONS.items()],
        "measures": [{"key": k, "label": v[0]} for k, v in MEASURES.items()],
    }
    return payload


# Потолок на список значений одного фильтра. Нужен, потому что мультиселект
# рендерит все опции в DOM: артикулов 12 020, моделей 4 255 — на таком списке
# страница подвисает при открытии. Обрезаем на сервере и СООБЩАЕМ об обрезке,
# чтобы интерфейс не делал вид, будто показал всё.
FILTER_OPTIONS_LIMIT = 500


# Отдельного эндпоинта значений фильтров больше нет: варианты зависят от
# текущего выбора (каскад) и приходят вместе с данными в `payload["options"]`.
# Иначе каждое изменение фильтра стоило бы двух обращений к серверу, а списки
# успевали бы разъехаться с цифрами.
