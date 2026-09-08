"""Структура себестоимости по статьям и сравнение этапов калькуляции.

Оба разреза нужны углублённому исследованию (`insight_agent.py`): дашборд
«Маржа выпуска» отвечает на вопрос «где просадка», а на «из чего она сложилась»
ответить было нечем — агент доходил до артикула и останавливался, потому что
дальше витрина отдавала только итоговую себестоимость (просьба заказчика
08.09.2026: «нужно, чтобы агент мог копнуть глубже в структуру себестоимости:
материалы, пошив, раскрой, декоры и т.д., сравнивать разные этапы калькуляций»).

ЧТО ЗДЕСЬ ЕСТЬ И ЧЕГО НЕТ
=========================
Источник раскладывает себестоимость калькуляции на шесть статей: основные и
вспомогательные материалы, пошив, раскрой, декоры, вязание. Их сумма ВСЕГДА
меньше себестоимости: медиана покрытия 79%, минимум 20% (замер 08.09.2026 по
августу — 3255 калькуляций выпуска). Остаток отдаётся отдельной строкой
«прочее» с явной пометкой, что источник его не раскладывает. Молча его
опускать нельзя: на 21% себестоимости строятся выводы о причинах.

Глубже статьи — до конкретного материала («какая нить подорожала») — здесь НЕ
реализовано, хотя построчная детализация в `cost_data_cache` есть
(«Наименование», «Норма», «цена материала, руб.», признак строки). Причина:
свести строки к статье не удалось. У калькуляции 70 TS N / план 9298 сумма
«Норма × цена» по 13 строкам вспомогательных материалов даёт 61,49, с
множителем `cost_factor_rub` — 2,2423, а колонка «Вспомогательные материалы,
руб.» той же калькуляции равна 0,590. Ни одно из двух чисел не сходится, то
есть смысл `cost_factor_rub` и размерность «Нормы» надо выяснять у тех, кто
ведёт калькуляции. Инструмент, который выдаёт финансисту неверную арифметику,
хуже отсутствующего.

ПОЧЕМУ ОТДЕЛЬНЫЙ МОДУЛЬ, А НЕ margin.py
=======================================
`margin.dashboard()` — один большой запрос со сдвинутым объединением трёх
периодов; вставить в него семь статей значит переписать его целиком. Здесь
запросы простые и независимые, а согласованность с дашбордом обеспечена тем,
что база выборки и белый список фильтров берутся ИЗ НЕГО (`_MARGIN_BASE`,
`MARGIN_FILTERS`): те же исключения полуфабрикатов и мультипаков, тот же
признак выпуска. Иначе структура затрат не билась бы с маржой на той же
странице.
"""

from __future__ import annotations

from .commercial import _collect_params
from .db import pool
from .margin import DATE_EXPRS, MARGIN_FILTERS, _FACT_B, _MARGIN_BASE, _ratio
from .margin import _DATE

# Статьи себестоимости источника: ключ выдачи → колонка витрины. Порядок —
# как читает человек: сначала материалы, потом операции, потом декор.
COST_ITEMS: tuple[tuple[str, str], ...] = (
    ("материалы_основные", "mat_main_byn"),
    ("материалы_вспомогательные", "mat_aux_byn"),
    ("пошив", "sewing_byn"),
    ("раскрой", "cutting_byn"),
    ("декоры", "decor_byn"),
    ("вязание", "knitting_byn"),
)

# Операции, у которых источник ведёт вторую стоимость — по ФАКТИЧЕСКОЙ ставке
# минуты (с 01.01.2026, миграция 0042). Только у пошива и раскроя: у материалов
# и декоров «факта» в этом смысле нет.
#
# МИНУТЫ У ФАКТА И НОРМАТИВА ОДНИ И ТЕ ЖЕ. Проверено на августе 2026: из 3255
# калькуляций выпуска `sewing_fact_minutes` отличается от `sewing_minutes` в
# НУЛЕ строк, `cutting_fact_minutes` — тоже в нуле. Различается только сумма
# пошива (1952 калькуляции), а сумма раскроя не отличается ни в одной, то есть
# фактическая ставка минуты раскроя равна нормативной или не ведётся.
#
# Поэтому минуты отдаются ОДНОЙ величиной, а разница норматива и факта
# выражается через СТАВКУ МИНУТЫ (BYN за минуту). Иначе модель, увидев два
# похожих поля «минут норматив» и «минут факт», напишет «минуты выросли» — на
# ровном месте, потому что это одно и то же число.
FACT_OPS: tuple[tuple[str, str, str, str], ...] = (
    # ключ, норматив BYN, факт BYN, минуты
    ("пошив", "sewing_byn", "sewing_fact_byn", "sewing_minutes"),
    ("раскрой", "cutting_byn", "cutting_fact_byn", "cutting_minutes"),
)

# Разрезы, по которым структуру можно развернуть. Тот же смысл, что у `by` в
# breakdown: понять, у какой группы поехала конкретная статья.
STRUCTURE_DIMS: dict[str, str] = {
    "level01": "level01",
    "level02": "level02",
    "level03": "level03",
    "level04": "level04",
    "brand_manager": "brand_manager",
    "articul": "articul",
    "model_name": "model_name",
}

# Строк в разрезе. Двадцати хватает, чтобы увидеть картину, и они не вытесняют
# из контекста модели остальную часть исследования.
STRUCTURE_ROW_LIMIT = 20

# Месяцев в динамике. Два года — вопросы про сезонность требуют прошлого года.
STRUCTURE_MONTH_LIMIT = 24

# Калькуляций на один артикул в сравнении этапов. У 70 TS N на ОДИН план 9298
# приходится 19 калькуляций (разные задания и даты расчёта) — без предела
# выдача по «ходовому» артикулу вытеснила бы всё остальное.
STAGE_ROW_LIMIT = 40

# Себестоимость выше этого — не деньги за штуку. В базе выпуска такие строки
# отсечены (`cost_byn < 1000` в BASE_CONDITIONS), но сравнение этапов смотрит и
# ПЛАНОВЫЕ признаки, где средняя себестоимость 1099 BYN против 6,50 у ФКСС
# (замер 08.09.2026): там встречаются суммы на партию, а не на единицу. Строку
# не выбрасываем — помечаем, иначе этап просто исчез бы из сравнения и разница
# «план против факта» читалась бы как «плана не было».
STAGE_OUTLIER_BYN = 1000


def _sum(expr: str, alias: str) -> str:
    """Взвешенная по выпуску сумма: статьи в витрине лежат НА ШТУКУ."""
    return f"sum(volume_pcs * coalesce({expr}, 0)) AS {alias}"


def _src_columns() -> str:
    """Величины, по которым считается структура."""
    parts = [
        "sum(volume_pcs)                    AS vol",
        "sum(volume_pcs * cost_byn)         AS cost_b",
        # Фактическая себестоимость рядом с нормативной: дашборд маржи по
        # умолчанию считает по факту, и без этой пары структура (она от
        # норматива) читалась бы как расклад чужого итога.
        #
        # Выражение ровно то же, что у дашборда при cost_basis='fact':
        # годный факт, иначе норматив. Прямая сумма `cost_fact_byn` давала
        # 4,4330 BYN/шт против 4,5288 на экране (август 2026) — расхождение на
        # ровном месте, потому что там, где факт негоден (см. _FACT_OK) или
        # пуст, дашборд берёт норматив, а сумма с coalesce(...,0) — ноль.
        f"sum(volume_pcs * coalesce({_FACT_B}, cost_byn)) AS cost_fact_b",
        "count(*)                           AS calc_count",
    ]
    parts += [_sum(col, f"item_{i}") for i, (_, col) in enumerate(COST_ITEMS)]

    for i, (_, norm_b, fact_b, minutes) in enumerate(FACT_OPS):
        parts += [
            _sum(fact_b, f"fact_{i}_byn"),
            _sum(minutes, f"min_{i}"),
            # Знаменатель минут считается отдельно от суммы: у 56% калькуляций
            # минут пошива нет вовсе (NULL), и «минут на штуку» по всему
            # объёму занижало бы показатель втрое. Тот же приём, что sew_vol в
            # margin.py.
            f"sum(CASE WHEN coalesce({minutes}, 0) > 0 THEN volume_pcs END)"
            f"                               AS min_{i}_vol",
            # Ставка минуты считается только там, где известны И сумма, И
            # минуты — иначе в знаменателе окажется объём без минут, и ставка
            # выйдет заниженной.
            f"sum(CASE WHEN coalesce({minutes}, 0) > 0 THEN volume_pcs * coalesce({norm_b}, 0) END)"
            f"                               AS norm_{i}_byn_onmin",
            f"sum(CASE WHEN coalesce({minutes}, 0) > 0 THEN volume_pcs * coalesce({fact_b}, 0) END)"
            f"                               AS fact_{i}_byn_onmin",
            f"sum(CASE WHEN coalesce({minutes}, 0) > 0 THEN volume_pcs * {minutes} END)"
            f"                               AS min_{i}_onmin",
            # Покрытие факта по ОПЕРАЦИИ: доля выпуска, где норматив есть, а
            # факт заведён. Без него «пошив по факту дешевле норматива» может
            # означать всего лишь незаполненную ставку минуты (см. _FACT_OK).
            f"sum(CASE WHEN coalesce({norm_b}, 0) > 0 THEN volume_pcs END)"
            f"                               AS norm_{i}_vol",
            f"sum(CASE WHEN coalesce({norm_b}, 0) > 0 AND coalesce({fact_b}, 0) > 0"
            f"          THEN volume_pcs END)  AS fact_{i}_vol",
        ]
    return ",\n            ".join(parts)


def _row_to_structure(row: dict) -> dict:
    """Строка агрегата → статьи на штуку с долями и остатком «прочее»."""
    vol = row.get("vol")
    cost_b = row.get("cost_b")
    unit_cost = _ratio(cost_b, vol)

    items: dict[str, dict] = {}
    covered = 0.0
    for i, (name, _) in enumerate(COST_ITEMS):
        total = row.get(f"item_{i}")
        if total is None:
            continue
        total = float(total)
        covered += total
        unit = _ratio(total, vol)
        # Статью, которой в срезе нет вовсе, не показываем: пустое «вязание:
        # 0,00 (0%)» в каждом ответе про трикотаж только отвлекает.
        if not total:
            continue
        items[name] = {
            "на_штуку_byn": _r(unit),
            "доля_в_себестоимости_pct": _pct(total, cost_b),
        }

    residual = None if cost_b is None else float(cost_b) - covered
    out: dict = {
        "выпуск_шт": None if vol is None else float(vol),
        "калькуляций": row.get("calc_count"),
        "себестоимость_на_штуку_byn_норматив": _r(unit_cost),
        "себестоимость_на_штуку_byn_факт": _r(_ratio(row.get("cost_fact_b"), vol)),
        "статьи": items,
        "прочее": {
            "на_штуку_byn": _r(_ratio(residual, vol)),
            "доля_в_себестоимости_pct": _pct(residual, cost_b),
            "пояснение": "источник не раскладывает эту часть себестоимости по статьям",
        },
    }

    ops: dict[str, dict] = {}
    for i, (name, norm_col, _, _) in enumerate(FACT_OPS):
        norm_total = row.get(f"item_{_item_index(norm_col)}")
        fact_total = row.get(f"fact_{i}_byn")
        minutes = _ratio(row.get(f"min_{i}"), row.get(f"min_{i}_vol"))
        rate_norm = _ratio(row.get(f"norm_{i}_byn_onmin"), row.get(f"min_{i}_onmin"))
        rate_fact = _ratio(row.get(f"fact_{i}_byn_onmin"), row.get(f"min_{i}_onmin"))
        entry = {
            "норматив_на_штуку_byn": _r(_ratio(norm_total, vol)),
            "факт_на_штуку_byn": _r(_ratio(fact_total, vol)),
            "минут_на_штуку": _r(minutes),
            "ставка_минуты_byn_норматив": _r(rate_norm),
            "ставка_минуты_byn_факт": _r(rate_fact),
            "покрытие_факта_pct": _pct(row.get(f"fact_{i}_vol"), row.get(f"norm_{i}_vol")),
        }
        if any(v is not None for v in entry.values()):
            ops[name] = entry
    if ops:
        out["операции_норматив_против_факта"] = ops
    return out


# Пояснение к операциям отдаётся ОДИН РАЗ на весь ответ, а не в каждой строке:
# строк бывает 24 месяца плюс 20 в разрезе, и повтор одного и того же абзаца
# сорок четыре раза вытеснял бы из контекста модели сами данные.
OPS_NOTE = (
    "минуты у норматива и факта ОДНИ И ТЕ ЖЕ — источник ведёт только "
    "фактическую ставку минуты, поэтому «минут на штуку» одно, а различаются "
    "ставка и сумма. Раскрой по факту обычно совпадает с нормативом: "
    "фактическая ставка раскроя либо равна нормативной, либо не ведётся. "
    "Себестоимость «норматив» и «факт» — те же две базы, что переключатель на "
    "дашборде; статьи разложены от НОРМАТИВНОЙ."
)


def _item_index(column: str) -> int:
    """Порядковый номер статьи в COST_ITEMS по её колонке."""
    for i, (_, col) in enumerate(COST_ITEMS):
        if col == column:
            return i
    raise KeyError(column)


def _r(value) -> float | None:
    """Округление величин на штуку. Копейки в себестоимости единицы значимы —
    четыре знака, потому что вспомогательные материалы бывают по 0,0003 BYN."""
    return None if value is None else round(float(value), 4)


def _pct(num, den) -> float | None:
    ratio = _ratio(num, den)
    return None if ratio is None else round(ratio * 100, 1)


async def cost_structure(filters: dict, by: str | None = None) -> dict:
    """Структура себестоимости выпуска по статьям.

    Возвращает итог по срезу, динамику по месяцам и, если задан *by*, разрез по
    измерению. Числа согласованы с дашбордом маржи: та же база выборки и тот же
    белый список фильтров.
    """
    if by is not None and by not in STRUCTURE_DIMS:
        raise ValueError(f"недопустимый разрез структуры: {by}")

    filters = {k: v for k, v in (filters or {}).items() if v}
    params: list = []
    dim_conditions = _collect_params(filters, params, MARGIN_FILTERS)
    date_conditions = _collect_params(filters, params, DATE_EXPRS)

    parts = list(_MARGIN_BASE)
    parts += list(dim_conditions.values())
    parts += [c.replace(_DATE, "production_date") for c in date_conditions.values()]
    where = " AND ".join(parts)
    cols = _src_columns()

    async with pool().acquire() as conn:
        total = await conn.fetchrow(
            f"SELECT {cols} FROM cost_calc_mv WHERE {where}", *params)

        months = await conn.fetch(
            f"""SELECT to_char(date_trunc('month', production_date), 'YYYY-MM') AS ym,
                       {cols}
                FROM cost_calc_mv WHERE {where}
                GROUP BY 1 ORDER BY 1 DESC LIMIT {STRUCTURE_MONTH_LIMIT}""",
            *params)

        rows = []
        if by:
            col = STRUCTURE_DIMS[by]
            # Сортировка по себестоимости выпуска, а не по объёму: предмет
            # разбора — деньги, и группа с дорогими изделиями важнее группы с
            # большим тиражом дешёвых.
            rows = await conn.fetch(
                f"""SELECT {col} AS k, {cols}
                    FROM cost_calc_mv WHERE {where} AND {col} IS NOT NULL
                    GROUP BY 1 ORDER BY sum(volume_pcs * cost_byn) DESC NULLS LAST
                    LIMIT {STRUCTURE_ROW_LIMIT}""",
                *params)

    out: dict = {
        "срез": filters or "весь выпуск",
        "итого": _row_to_structure(dict(total)) if total else {},
        "по_месяцам": [
            {"месяц": r["ym"], **_row_to_structure(dict(r))}
            for r in reversed(months)
        ],
        "как_читать": OPS_NOTE,
    }
    if by:
        out["разрез"] = {"по": by, "строки": [
            {"значение": r["k"], **_row_to_structure(dict(r))} for r in rows]}
    return out


# ─── Этапы калькуляции ───────────────────────────────────────────────────────

# Признаки калькуляции в порядке жизненного цикла. Названия — из источника,
# расшифровка — из глоссария раздела: П- впереди означает «предварительная».
# Выпуск считается только по ФКСС (решение заказчика 14.08.2026), остальные
# этапы — плановые и предварительные расчёты, вместе меньше 2% калькуляций.
STAGE_ORDER: dict[str, int] = {"ПКПСС": 1, "КПСС": 2, "ПФКСС": 3, "ФКСС": 4}


async def calc_stages(articul: str, plan_id: str | None = None) -> dict:
    """Все этапы калькуляции одного артикула: как менялась себестоимость.

    Именно по артикулу, а не по срезу: этапы сравнимы только внутри одной
    позиции. Из 12 935 артикулов витрины больше одного этапа есть у 842
    (замер 08.09.2026), поэтому пустой ответ — норма, а не сбой.

    База выборки здесь НЕ `_MARGIN_BASE`: тот отбирает только ФКСС с объёмом,
    то есть ровно один этап из четырёх. Ограничения по себестоимости тоже
    сняты — вместо отбрасывания выбросов строка помечается (см.
    STAGE_OUTLIER_BYN).
    """
    articul = str(articul or "").strip()
    if not articul:
        raise ValueError("для сравнения этапов нужен артикул")

    params: list = [articul]
    where = "articul = $1"
    if plan_id:
        params.append(str(plan_id).strip())
        where += f" AND plan_id = ${len(params)}"

    item_cols = ", ".join(col for _, col in COST_ITEMS)
    fact_cols = ", ".join(
        f"{fact_b}, {minutes}" for _, _, fact_b, minutes in FACT_OPS)

    async with pool().acquire() as conn:
        rows = await conn.fetch(
            f"""SELECT calc_sign, plan_id, zadanie, model, model_name,
                       calc_date::date AS calc_date, production_date::date AS production_date,
                       volume_pcs, cost_byn, cost_fact_byn, wholesale_price_byn,
                       {item_cols}, {fact_cols}
                FROM cost_calc_mv
                WHERE {where}
                ORDER BY calc_date DESC
                LIMIT {STAGE_ROW_LIMIT}""",
            *params)

    # Какие статьи заполнены ХОТЯ БЫ НА ОДНОМ этапе этого артикула. Только
    # относительно них имеет смысл говорить «статья пропала»: вязание пусто у
    # любого швейного изделия законно, и в первой версии модель приняла это за
    # дефект — «во всех калькуляциях отсутствует вязание, что указывает на
    # неполноту разложения» (проверено 08.09.2026 на толстовке). Настоящий
    # признак дефекта — статья, которая на других этапах ЕСТЬ, а на этом нет:
    # у 25-36412П-7П-3 пошив и раскрой заполнены в КПСС и ПФКСС, но пусты в
    # ФКСС, из-за чего себестоимость 11,23 против 19,52 — не экономия, а
    # потерянные операции.
    present_somewhere = {
        name for name, col in COST_ITEMS
        if any(dict(r).get(col) is not None and float(dict(r)[col]) for r in rows)
    }

    stages = []
    for r in rows:
        row = dict(r)
        cost = row.get("cost_byn")
        items = {}
        missing = []
        for name, col in COST_ITEMS:
            value = row.get(col)
            if value is None or not float(value):
                if name in present_somewhere:
                    missing.append(name)
                continue
            items[name] = _r(value)
        entry = {
            "этап": row.get("calc_sign"),
            "порядок": STAGE_ORDER.get(row.get("calc_sign") or "", 99),
            "дата_расчёта": str(row.get("calc_date") or ""),
            "план": row.get("plan_id"),
            "задание": row.get("zadanie"),
            "выпуск_шт": None if row.get("volume_pcs") is None else float(row["volume_pcs"]),
            "себестоимость_byn": _r(cost),
            "себестоимость_факт_byn": _r(row.get("cost_fact_byn")),
            "отпускная_цена_byn": _r(row.get("wholesale_price_byn")),
            "статьи": items,
        }

        # Операции по этапу: сумма по факту и минуты. Ставку минуты на одной
        # калькуляции считаем прямым делением — это не агрегат, взвешивать
        # нечего.
        for name, norm_b, fact_b, minutes_col in FACT_OPS:
            mins = row.get(minutes_col)
            if not mins:
                continue
            op = {"минут": _r(mins), "ставка_минуты_byn_норматив": _r(_ratio(row.get(norm_b), mins))}
            if row.get(fact_b) is not None:
                op["факт_byn"] = _r(row.get(fact_b))
                op["ставка_минуты_byn_факт"] = _r(_ratio(row.get(fact_b), mins))
            entry.setdefault("операции", {})[name] = op
        if missing:
            entry["статьи_пропали_на_этом_этапе"] = missing
        if cost is not None and float(cost) >= STAGE_OUTLIER_BYN:
            entry["предупреждение"] = (
                f"себестоимость {_r(cost)} BYN — не похоже на цену за штуку; "
                "на плановых этапах встречаются суммы на партию, "
                "сравнивать с ФКСС нельзя")
        stages.append(entry)

    stages.sort(key=lambda s: (s["порядок"], s["дата_расчёта"]))
    return {
        "артикул": articul,
        "план": plan_id or "все",
        "модель": (dict(rows[0]).get("model") if rows else None),
        "наименование": (dict(rows[0]).get("model_name") if rows else None),
        "этапы": stages,
        "пояснение": (
            "ПКПСС и КПСС — плановые расчёты, ПФКСС — предварительный факт, "
            "ФКСС — факт выпуска (по нему считается дашборд). "
            "«статьи_пропали_на_этом_этапе» — статьи, заполненные на других "
            "этапах этого же артикула, но пустые здесь: сравнивая "
            "себестоимость этапов, проверяй это поле первым, потому что "
            "пропавшая операция снижает себестоимость и выглядит как экономия. "
            "Статьи, пустые на ВСЕХ этапах, в это поле не попадают — они к "
            "изделию просто не применяются (вязание у швейных изделий)."
            if stages else
            "по этому артикулу этап только один — сравнивать нечего"),
    }
