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
* показатели «выпуска» — взвешенные по объёму и ТОЛЬКО по ФКСС (см. VOLUME_SIGN).

БАЗА РАСЧЁТА
============
Себестоимость пуста у 36% калькуляций, отпускная цена у 38%. В расчёт попадает
61%, и это возвращается в `meta.calc_total` рядом с `tiles.calc_count` — плитка
«маржинальность 50%» без этой пары читается как картина по всему ассортименту.
"""

from __future__ import annotations

import json

from .db import pool

# Признак калькуляции, по которому считается выпуск. Решение заказчика
# 14.08.2026: «выпуск считаем всегда только по ФКСС». Остальные признаки
# (КПСС, ПФКСС, ПКПСС — вместе меньше 2% калькуляций) — плановые и
# предварительные расчёты, объём по ним выпуском не является.
VOLUME_SIGN = "ФКСС"

# Потолок на список значений одного фильтра. Мультиселект рендерит опции в DOM,
# поэтому список нельзя отдавать целиком: артикулов 12 020, моделей 4 255.
# Обрезаем на сервере и СООБЩАЕМ об обрезке (meta.options_truncated), чтобы
# интерфейс не делал вид, будто показал всё. Сам список ищется поиском внутри
# мультиселекта, а каскад сужает его до вменяемого размера.
FILTER_OPTIONS_LIMIT = 1000

# Разрешённые измерения кольцевой диаграммы. Белый список обязателен: имя
# колонки подставляется в SQL, а не передаётся параметром.
#
# «Уровень цен», «Цвет» и «Признак калькуляции» убраны по просьбе заказчика
# (14.08.2026): в разрезе выпуска они бессмысленны — уровень цен и цвет к объёму
# отношения не имеют, а признак калькуляции здесь всегда один (VOLUME_SIGN).
DIMENSIONS: dict[str, str] = {
    "model_name": "Наименование товара",
    "model": "Модель",
    "country": "Страна пр-ва",
    "season": "Сезон",
    "level01": "Level 01",
    "level02": "Level 02",
    "level03": "Level 03",
    "brand_manager": "Бренд-менеджер",
    "family": "Семья",
}

# Меры кольцевой диаграммы — это «структура ВЫПУСКА», поэтому все меры про
# выпуск. Рубли даны в двух базах: в отпускных ценах (товарный выпуск) и по
# себестоимости (производственный). Какая нужна — зависит от вопроса, поэтому
# выбирает пользователь, а не мы за него.
#
# ВАЖНО: до первого успешного refresh кэша после миграций 0036/0037 volume_pcs
# пуст, и все три меры вернут NULL. Это должно быть видно в интерфейсе, а не
# нарисовано нулём.
MEASURES: dict[str, tuple[str, str]] = {
    "volume_pcs": ("Выпуск, шт", "sum(volume_pcs)"),
    "volume_price": ("Выпуск в отпускных ценах, BYN", "sum(volume_pcs * price_b)"),
    "volume_cost": ("Выпуск по себестоимости, BYN", "sum(volume_pcs * cost_b)"),
}

# Иерархия «структуры себестоимости»: сверху вниз, от бренд-менеджера к товару.
# По ней работает проваливание — клик по столбцу берёт следующий уровень и
# фильтрует его выбранным значением (structure_path в запросе).
#
# Это НЕ общие фильтры дашборда: путь применяется только к этому графику.
# Иначе клик по столбцу менял бы и плитки, и динамику, и кольцо — а вопрос
# «из чего складывается себестоимость вот этой группы» задаётся к одному графику.
STRUCTURE_LEVELS: list[tuple[str, str]] = [
    ("brand_manager", "Бренд-менеджер"),
    ("level01", "Level 01"),
    ("level02", "Level 02"),
    ("level03", "Level 03"),
    ("level04", "Level 04"),
    ("level05", "Level 05"),
    ("model_name", "Наименование товара"),
]

# Фильтры: ключ запроса → SQL-выражение витрины. Тоже белый список — выражение
# подставляется в SQL, из запроса приходят только значения.
#
# «Уровень цен» из фильтров убран (заказчик, 14.08.2026), «Модель» и «Артикул»
# добавлены. Год и месяц — отдельно, см. DATE_BASES: их выражение зависит от
# выбранной даты.
FILTERS: dict[str, str] = {
    "model_name": '"model_name"',
    "model": '"model"',
    "articul": '"articul"',
    "country": '"country"',
    "calc_sign": '"calc_sign"',
    "season": '"season"',
    "level01": '"level01"',
    "level02": '"level02"',
    "level03": '"level03"',
    "brand_manager": '"brand_manager"',
}

# По какой дате считаются год, месяц и динамика.
#
# В витрине две даты, и они про РАЗНОЕ:
# * «дата производства» — когда изделие выпущено. Бизнес-дата: 2024-2026,
#   ровное распределение по месяцам (2024 — 3 090 калькуляций, 2025 — 45 976,
#   2026 — 38 116). По ней динамика и выпуск имеют смысл.
# * «дата расчёта» — когда калькуляцию посчитали. Технический штамп источника,
#   и в кэше он всегда свежий: ВСЕ 87 тыс. калькуляций лежат в июне-августе
#   2026 (обновление кэша перезагружает последние месяцы). Разрез по годам по
#   ней даёт один год, а динамика — три точки.
#
# Поэтому по умолчанию — дата производства. Переключатель оставлен: «когда
# пересчитали цены» — тоже законный вопрос, просто другой.
DATE_BASES: dict[str, tuple[str, str]] = {
    "production": ("дате производства", "production_date"),
    "calc": ("дате расчёта", "calc_date"),
}
DEFAULT_DATE_BASIS = "production"

# Ключи фильтров, которые роут забирает из query-параметров.
FILTER_KEYS: tuple[str, ...] = (*FILTERS, "year", "month")


def filter_exprs(basis: str) -> dict[str, str]:
    """Полный набор выражений фильтров для выбранной даты."""
    col = DATE_BASES[basis][1]
    return {
        **FILTERS,
        "year": f"to_char({col}, 'YYYY')",
        "month": f"to_char({col}, 'MM')",
    }


# Базовые условия выборки, общие для всех запросов дашборда.
BASE_CONDITIONS = (
    # Без цены и себестоимости наценку не посчитать.
    "cost_byn > 0",
    # Отсечение выбросов: их единицы, но при медиане около 2 они задирают суммы.
    "cost_byn < 1000",
    "wholesale_price_byn > 0",
)

# Медианы цен — одинаковый набор для плиток и для динамики по месяцам. Держим
# одним списком, чтобы график и плитки не разъехались формулой.
_PRICE_MEDIANS = (
    ("cost_byn", "cost_byn"),
    ("cost_usd", "cost_usd"),
    ("price_byn", "wholesale_price_byn"),
    ("price_usd", "wholesale_price_usd"),
    ("retail_byn", "retail_price_byn"),
    ("retail_usd", "retail_price_usd"),
)


def _medians(columns=_PRICE_MEDIANS) -> str:
    return ",\n               ".join(
        f"percentile_cont(0.5) WITHIN GROUP (ORDER BY {src}) AS {alias}"
        for alias, src in columns
    )


def _collect_params(filters: dict, params: list, exprs: dict[str, str]) -> dict[str, str]:
    """Раскладывает значения фильтров по параметрам ОДИН раз и возвращает
    готовые условия по ключам. Один общий список параметров нужен, чтобы
    десяток вариантов WHERE (для каскада) не плодил десяток копий значений."""
    conditions: dict[str, str] = {}
    for key, expr in exprs.items():
        values = filters.get(key)
        if not values:
            continue
        params.append([str(v) for v in values])
        conditions[key] = f"{expr} = ANY(${len(params)})"
    return conditions


def _where(conditions: dict[str, str], exclude: str | None = None) -> str:
    """Собирает WHERE, при желании выбросив условие одного фильтра.

    `exclude` нужен в двух местах:
    * каскад — варианты фильтра считаются по данным, отфильтрованным всеми
      ОСТАЛЬНЫМИ. Если учитывать и его собственный выбор, список схлопнется до
      уже выбранного, и ни добавить значение, ни снять выбор станет нельзя;
    * динамика по месяцам — она и есть разрез по месяцам, поэтому фильтр
      «месяц» к ней не применяется (иначе график схлопнулся бы в одну точку).
    """
    parts = list(BASE_CONDITIONS)
    parts += [c for key, c in conditions.items() if key != exclude]
    return " AND ".join(parts)


def _as_dict(value):
    return json.loads(value) if isinstance(value, str) else value


async def dashboard(
    filters: dict,
    dimension: str = "model_name",
    measure: str = "volume_pcs",
    structure_path: list[str] | None = None,
    date_basis: str = DEFAULT_DATE_BASIS,
) -> dict:
    if dimension not in DIMENSIONS:
        raise ValueError(f"недопустимое измерение: {dimension}")
    if measure not in MEASURES:
        raise ValueError(f"недопустимая мера: {measure}")
    if date_basis not in DATE_BASES:
        raise ValueError(f"недопустимая база даты: {date_basis}")

    date_label, date_col = DATE_BASES[date_basis]
    exprs = filter_exprs(date_basis)

    params: list = []
    conditions = _collect_params(filters, params, exprs)
    where = _where(conditions)
    measure_expr = MEASURES[measure][1]

    # Проваливание по иерархии. Путь длиной N означает: показать разрез по
    # уровню N, отфильтровав первые N уровней выбранными значениями. Глубже
    # последнего уровня не идём — там уже товар.
    path = [str(v) for v in (structure_path or [])][: len(STRUCTURE_LEVELS) - 1]
    struct_dim = STRUCTURE_LEVELS[len(path)][0]
    struct_conditions = []
    for level, value in zip(STRUCTURE_LEVELS, path):
        params.append(value)
        struct_conditions.append(f'"{level[0]}" = ${len(params)}')
    struct_where = "".join(f" AND {c}" for c in struct_conditions)

    # Динамика игнорирует фильтр «месяц» — см. _where.
    where_months = _where(conditions, exclude="month")

    # Каскад: для каждого фильтра свой список вариантов, посчитанный по данным
    # без его собственного условия. Считаем здесь же, в общем запросе, — иначе
    # каждое изменение фильтра стоило бы двух обращений к серверу.
    option_selects = ",\n            ".join(
        f"""'{key}', (SELECT coalesce(json_agg(v ORDER BY v), '[]'::json) FROM
                (SELECT DISTINCT {expr} AS v FROM cost_calc_mv
                 WHERE {_where(conditions, exclude=key)} AND {expr} IS NOT NULL
                 ORDER BY 1 LIMIT {FILTER_OPTIONS_LIMIT + 1}) o_{i})"""
        for i, (key, expr) in enumerate(exprs.items())
    )

    # base MATERIALIZED — чтобы Postgres прошёл по витрине один раз, а не
    # подставлял CTE заново в каждый из четырёх агрегатов.
    sql = f"""
    WITH base AS MATERIALIZED (
        SELECT
            model, articul, model_name, calc_sign, calc_date, season, country,
            level01, level02, level03, brand_manager, family,
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
            -- Выпуск — только по ФКСС, поэтому FILTER, а не общий sum.
            sum(volume_pcs) FILTER (WHERE calc_sign = '{VOLUME_SIGN}') AS volume_total,
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
            100.0 * sum(markup_b * volume_pcs) FILTER (WHERE calc_sign = '{VOLUME_SIGN}')
                  / nullif(sum(price_b * volume_pcs)
                           FILTER (WHERE calc_sign = '{VOLUME_SIGN}'), 0) AS margin_pct_w,
            100.0 * sum(markup_b * volume_pcs) FILTER (WHERE calc_sign = '{VOLUME_SIGN}')
                  / nullif(sum(cost_b * volume_pcs)
                           FILTER (WHERE calc_sign = '{VOLUME_SIGN}'), 0)  AS profit_pct_w,
            max(calc_date)                                        AS last_calc_date
        FROM base
    ),
    -- Динамика цен по МЕСЯЦАМ (заказчик, 14.08.2026 — раньше здесь были сезоны).
    -- Отдельный проход по витрине, а не по base: фильтр «месяц» к этому разрезу
    -- не применяется, иначе график схлопнулся бы в одну точку. Фильтр «год»
    -- применяется — он и задаёт окно графика.
    months AS (
        SELECT to_char(date_trunc('month', {date_col}), 'YYYY-MM') AS ym,
               {_medians()},
               count(*) AS calc_count
        FROM cost_calc_mv
        WHERE {where_months} AND {date_col} IS NOT NULL
        GROUP BY 1
    ),
    -- Топ-10 по себестоимости, от большей к меньшей — так же, как в Superset,
    -- иначе два дашборда показывают разные наборы и не сравниваются.
    -- Разрез — текущий уровень иерархии (см. STRUCTURE_LEVELS).
    structure AS (
        SELECT "{struct_dim}"::text AS label,
               sum(mat_main_b) AS mat_main, sum(mat_aux_b)  AS mat_aux,
               sum(sewing_b)   AS sewing,   sum(cutting_b)  AS cutting,
               sum(decor_b)    AS decor,    sum(knitting_b) AS knitting,
               sum(other_b)    AS other,    sum(cost_b)     AS cost_total
        FROM base WHERE "{struct_dim}" IS NOT NULL{struct_where}
        GROUP BY 1 ORDER BY sum(cost_b) DESC NULLS LAST LIMIT 10
    ),
    -- Структура ВЫПУСКА: только ФКСС, при любом наборе фильтров.
    ring AS (
        SELECT "{dimension}"::text AS label, ({measure_expr})::numeric AS value
        FROM base
        WHERE "{dimension}" IS NOT NULL AND calc_sign = '{VOLUME_SIGN}'
        GROUP BY 1 ORDER BY 2 DESC NULLS LAST LIMIT 12
    )
    SELECT json_build_object(
        'tiles',     (SELECT row_to_json(t) FROM tiles t),
        'months',    coalesce((SELECT json_agg(m ORDER BY m.ym) FROM months m), '[]'::json),
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
        # Состояние проваливания по «структуре себестоимости»: где мы сейчас,
        # чем отфильтровано и есть ли куда проваливаться дальше.
        "structure_dim": struct_dim,
        "structure_label": STRUCTURE_LEVELS[len(path)][1],
        "structure_path": [
            {"level": STRUCTURE_LEVELS[i][0], "label": STRUCTURE_LEVELS[i][1], "value": v}
            for i, v in enumerate(path)
        ],
        "structure_can_drill": len(path) < len(STRUCTURE_LEVELS) - 1,
        # По какой дате считаются год, месяц и динамика. Подпись обязана быть
        # видна в интерфейсе: два разреза по разным датам выглядят одинаково,
        # а показывают разное (см. DATE_BASES).
        "date_basis": date_basis,
        "date_basis_label": date_label,
        # Признак калькуляции, по которому считается выпуск — чтобы интерфейс
        # подписывал это сам, а не хардкодил «ФКСС» второй раз.
        "volume_sign": VOLUME_SIGN,
    }
    return payload


# Отдельного эндпоинта значений фильтров больше нет: варианты зависят от
# текущего выбора (каскад) и приходят вместе с данными в `payload["options"]`.
# Иначе каждое изменение фильтра стоило бы двух обращений к серверу, а списки
# успевали бы разъехаться с цифрами.
