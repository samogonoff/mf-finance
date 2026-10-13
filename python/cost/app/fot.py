# -*- coding: utf-8 -*-
"""ФОТ-дашборд (P9–P13): витрины-кэш в Postgres + загрузка из MSSQL.

Шаг 1 (этот модуль):
  * ROW_TEMPLATE_BY_CODE / ROW_TEMPLATE_BY_GROUP — маппинг статей P&L на
    «шаблон строки» (VALUES-таблицы dim_PL из CFO_ALL_AGG);
  * T-SQL загрузчики витрин — дословный порт M-запросов fact_PL / fact_HR /
    fact_HRPlan / dim_CFO / dim_Closed (трёхчастные имена таблиц: FinDWH.*,
    onec_data.*, Budgeting.*);
  * refresh_fot() — TTL (FOT_REFRESH_MINUTES) + stale-while-revalidate: тяжёлые
    чтения MSSQL идут мимо транзакции, затем все витрины перезаписываются в
    ОДНОЙ транзакции Postgres (TRUNCATE+INSERT каждой), при ошибке данные не
    трогаются и API продолжает отдавать предыдущий снимок;
  * fot_worker() — фоновый воркер для lifespan в app/main.py;
  * filter_options() / dashboard() — сборка ответов вкладок для роутов:
    каскадные фильтры и карточки/графики/таблицы (P9 реализован полностью,
    P10–P13 — каркас до шагов 2b/2c).

Чтение данных дашбордом — только из Postgres; MSSQL нужен лишь refresh'у и
доступен по VPN (учётка OLAP_*, см. db.get_olap_conn()).
"""
from __future__ import annotations

import asyncio
import json
import logging
import os
import time
from datetime import date, datetime, timezone

from app.db import acquire, get_olap_conn, pool
from app.logship import log

# ── Конфигурация ─────────────────────────────────────────────────────────────

FOT_REFRESH_MINUTES = int(os.environ.get("FOT_REFRESH_MINUTES", "60"))
FOT_QUERY_TIMEOUT = int(os.environ.get("FOT_QUERY_TIMEOUT", "300"))
# Информационная база MSSQL, из которой читают загрузчики. Сами запросы всегда
# используют трёхчастные имена (FinDWH.dbo.* / onec_data.dbo.* / Budgeting.dbo.*),
# поэтому переменная не влияет на соединение (его открывает db.get_olap_conn(),
# база подключения — OLAP_DATABASE) — только на запись в логах.
FOT_DATABASE = os.environ.get("FOT_DATABASE", "FinDWH")

# Годы сравнения зафиксированы постановкой: P10–P13 — фиксированные полугодия
# 2025 vs 2026, от срезов дат не зависят. Смена года отчёта — правка констант.
YEAR_PREV = 2025
YEAR_PLAN = 2026

_FOT_TABS = ("p9", "p10", "p11", "p12", "p13")


def fot_mock_enabled() -> bool:
    """Моки без VPN: достаточно любого из COST_MOCK=1 или FOT_MOCK=1."""
    return os.environ.get("COST_MOCK", "").strip() == "1" or os.environ.get("FOT_MOCK", "").strip() == "1"


# ── Маппинг статей на шаблон строки (dim_PL.tmdl, VALUES-таблицы) ────────────

# Уровень кода статьи (CodePL): 162 пары. Источник — inline-таблица v в M-запросе
# dim_PL (CFO_ALL_AGG.SemanticModel), перенесена дословно.
ROW_TEMPLATE_BY_CODE = {
    "10": "Реклама и маркетинг",
    "100": "Прочие доходы и расходы",
    "1001": "Продажи без НДС",
    "1002": "Продажи без НДС",
    "1003": "Продажи без НДС",
    "1004": "Продажи без НДС",
    "1005": "Продажи без НДС",
    "1006": "Продажи без НДС",
    "1007": "Продажи без НДС",
    "1009": "Продажи без НДС",
    "1011": "Продажи без НДС",
    "1012": "Продажи без НДС",
    "1013": "Продажи без НДС",
    "1014": "Продажи без НДС",
    "1015": "Продажи без НДС",
    "1016": "Продажи без НДС",
    "1017": "Продажи с НДС",
    "1018": "Продажи с НДС",
    "1019": "Продажи с НДС",
    "1020": "Продажи с НДС",
    "1021": "Продажи с НДС",
    "1022": "Продажи с НДС",
    "1023": "Продажи с НДС",
    "1025": "Продажи с НДС",
    "1027": "Продажи с НДС",
    "1028": "Продажи с НДС",
    "1029": "Продажи с НДС",
    "1031": "Продажи с НДС",
    "1032": "Продажи с НДС",
    "1043": "Продажи без НДС",
    "1044": "Продажи с НДС",
    "11": "Реклама и маркетинг",
    "1107": "Продажи без НДС",
    "1109": "Продажи без НДС",
    "1123": "Продажи с НДС",
    "1125": "Продажи с НДС",
    "1147": "Продажи без НДС",
    "1148": "Продажи с НДС",
    "12": "Реклама и маркетинг",
    "13": "Реклама и маркетинг",
    "14": "Реклама и маркетинг",
    "15": "Реклама и маркетинг",
    "16": "Реклама и маркетинг",
    "17": "Реклама и маркетинг",
    "18": "Реклама и маркетинг",
    "19": "Реклама и маркетинг",
    "20": "Реклама и маркетинг",
    "2001": "Себестоимость реализованной продукции",
    "2002": "Себестоимость реализованной продукции",
    "2003": "Себестоимость реализованной продукции",
    "2004": "Себестоимость реализованной продукции",
    "2005": "Себестоимость реализованной продукции",
    "2006": "Себестоимость реализованной продукции",
    "2007": "Себестоимость реализованной продукции",
    "2009": "Себестоимость реализованной продукции",
    "2011": "Себестоимость реализованной продукции",
    "2012": "Себестоимость реализованной продукции",
    "2013": "Себестоимость реализованной продукции",
    "2014": "Себестоимость реализованной продукции",
    "2015": "Себестоимость реализованной продукции",
    "2016": "Себестоимость реализованной продукции",
    "2034": "Прочие статьи вне списка финслужбы",
    "2035": "Себестоимость реализованной продукции",
    "2037": "Себестоимость реализованной продукции",
    "2043": "Себестоимость реализованной продукции",
    "21": "Материалы",
    "2107": "Себестоимость реализованной продукции",
    "2109": "Себестоимость реализованной продукции",
    "2147": "Себестоимость реализованной продукции",
    "22": "Реклама и маркетинг",
    "23": "Реклама и маркетинг",
    "24": "Заработная плата",
    "25": "Материалы",
    "26": "Заработная плата",
    "27": "Прочие расходы на персонал",
    "28": "Прочие расходы на персонал",
    "29": "Прочие расходы на персонал",
    "30": "Прочие расходы на персонал",
    "31": "Прочие расходы на персонал",
    "32": "Прочие расходы на персонал",
    "33": "Ремонты и строительство (Opex)",
    "34": "Ремонты и строительство (Opex)",
    "35": "Коммунальные расходы",
    "36": "Коммунальные расходы",
    "37": "Прочие расходы",
    "38": "Материалы",
    "39": "Материалы",
    "40": "Информационно-консультационные услуги",
    "4001": "Проценты к получению",
    "4002": "Продажи без НДС",
    "4003": "Продажи без НДС",
    "4004": "Прочие доходы и расходы",
    "41": "Материалы",
    "42": "Услуги пошива",
    "43": "Ремонты и строительство (Opex)",
    "44": "Расходы на открытие магазина (текущие)",
    "45": "Расходы на магазины",
    "45A": "Материалы",
    "46": "Материалы",
    "47": "Прочие расходы",
    "48": "Расходы на ИТ",
    "49": "Расходы на ИТ",
    "50": "Логистические расходы",
    "51": "Логистические расходы",
    "52": "Логистические расходы",
    "53": "Расходы на охрану и страхование",
    "54": "Аренда",
    "55": "Аренда",
    "56": "Налог на прибыль",
    "57": "Налоги",
    "58": "Банковские расходы",
    "59": "Проценты к уплате",
    "60": "Информационно-консультационные услуги",
    "6002": "Себестоимость пошива, раскроя, печати",
    "6003": "Себестоимость пошива, раскроя, печати",
    "6004": "Себестоимость пошива, раскроя, печати",
    "6005": "Себестоимость пошива, раскроя, печати",
    "6007": "Себестоимость пошива, раскроя, печати",
    "6009": "Себестоимость пошива, раскроя, печати",
    "6035": "Себестоимость пошива, раскроя, печати",
    "61": "Информационно-консультационные услуги",
    "6107": "Себестоимость пошива, раскроя, печати",
    "6109": "Себестоимость пошива, раскроя, печати",
    "62": "Информационно-консультационные услуги",
    "63": "Прочие расходы",
    "64": "Комиссионное вознаграждение",
    "64P": "Финансовая скидка",
    "65": "Прочие расходы на персонал",
    "66": "Прочие расходы",
    "67": "Амортизация",
    "68": "Прочие расходы",
    "70": "Прочие расходы",
    "73": "Прочие расходы",
    "74": "Амортизация",
    "75": "Расходы на открытие магазина (текущие)",
    "76": "Расходы на открытие магазина (текущие)",
    "77": "Расходы на открытие магазина (текущие)",
    "78": "Заработная плата",
    "79": "Заработная плата",
    "80": "Заработная плата",
    "8002": "Себестоимость по отпускным ценам",
    "8003": "Себестоимость по отпускным ценам",
    "8004": "Себестоимость по отпускным ценам",
    "8005": "Себестоимость по отпускным ценам",
    "81": "Заработная плата",
    "82": "Амортизация",
    "83": "Банковские расходы",
    "84": "Расходы на магазины",
    "85": "Расходы на ИТ",
    "86": "Заработная плата",
    "87": "Заработная плата",
    "88": "Заработная плата",
    "89": "Заработная плата",
    "90": "Прочие доходы и расходы",
    "91": "Реклама и маркетинг",
    "92": "Ремонты и строительство (Opex)",
    "93": "Логистические расходы",
    "94": "Результаты инвентаризации",
    "95": "Безвозмездная помощь",
    "98": "Ремонты и строительство (Opex)",
    "99": "Курсовые разницы",
    "V1014": "Продажи с НДС",
}

# Фолбэк по группе статей (GroupPL) — СОКРАЩЁН до рабочего множества дашборда
# (Заработная плата / Прочие расходы на персонал / Амортизация / Продажи без
# НДС; всё прочее → «(прочее)»). Осознанно НЕ переносим фолбэки «ПРОДАЖИ с НДС…»,
# «…(СПП)» и маржинальных групп: схлопывать продажи с НДС в «Продажи без НДС»
# численно неверно, а рабочие коды продаж и так покрыты ROW_TEMPLATE_BY_CODE.
ROW_TEMPLATE_BY_GROUP = {
    "ВЫПЛАТЫ РАБОТНИКАМ": "Заработная плата",
    "АМОРТИЗАЦИЯ": "Амортизация",
    "ПРОДАЖИ": "Продажи без НДС",
}


def _sqlq(s: str) -> str:
    return "'" + s.replace("'", "''") + "'"


# inline-таблица код→шаблон для T-SQL (строится из dict, чтобы не расходиться)
_TPL_TABLE_SQL = (
    "SELECT * FROM (VALUES\n    "
    + ",\n    ".join(
        "(N%s, N%s)" % (_sqlq(c), _sqlq(t)) for c, t in sorted(ROW_TEMPLATE_BY_CODE.items())
    )
    + "\n) AS t(code_pl, row_template)"
)

_GROUP_FALLBACK_SQL = (
    "CASE s.GroupPL\n"
    "                WHEN N'ВЫПЛАТЫ РАБОТНИКАМ' THEN N'Заработная плата'\n"
    "                WHEN N'АМОРТИЗАЦИЯ' THEN N'Амортизация'\n"
    "                WHEN N'ПРОДАЖИ' THEN N'Продажи без НДС'\n"
    "                ELSE N'(прочее)'\n"
    "            END"
)

# ── T-SQL загрузчики (порт M-запросов CFO_ALL_AGG, дословно) ─────────────────

# Порт fact_PL.tmdl БЕЗ UNION синтеза «Продажи с НДС» (VAT-пары в дашборде
# не нужны: ФОТ и численность считают по «Продажам без НДС»). Знак и
# нормализация кодов — дословно; знаменатель KZ (коды 1006/1012) — флаг в зерне.
SQL_FOT_MONTHLY = f"""
SET NOCOUNT ON;
WITH tpl AS ({_TPL_TABLE_SQL}),
src AS (
    SELECT
        CAST(f.[Month] AS date) AS month_date,
        ISNULL(NULLIF(LTRIM(RTRIM(f.CodeCFO)), N''), N'') AS code_cfo,
        ISNULL(NULLIF(LTRIM(RTRIM(f.[Компания])), ''), N'(не указана)') AS legal_entity,
        ISNULL(NULLIF(LTRIM(RTRIM(f.Country)), ''), N'(не указана)') AS country_code,
        ISNULL(CAST(f.Scenario AS nvarchar(50)), N'') AS scenario_src,
        CASE
            WHEN f.Scenario <> N'факт' AND f.CodePL IN (N'2001', N'2002', N'2003', N'2004', N'2005', N'2006', N'2011', N'2012', N'2013', N'6001', N'6002', N'6003', N'6004', N'6005', N'6006', N'6011', N'6012', N'6013', N'8007', N'8009', N'8014', N'8015', N'8035', N'8037', N'8043', N'8147') THEN N'(вне отчёта)'
            ELSE CASE
                WHEN f.[Для отчета] = 'PL' THEN N'P&L'
                WHEN f.[Для отчета] LIKE 'BS%' THEN N'Производство (BS)'
                WHEN f.[Для отчета] = 'PR' THEN N'Прошлые периоды (PR)'
                ELSE N'(вне отчёта)'
            END
        END AS kontur,
        CASE
            WHEN f.CodePL = N'45' AND ISNULL(f.[Метод.нюансы], 0) = 0 THEN N'45A'
            WHEN f.CodePL = N'64' AND ISNULL(f.[Метод.нюансы], 0) = 0 THEN N'64P'
            ELSE ISNULL(NULLIF(LTRIM(RTRIM(f.CodePL)), N''), N'(без кода)')
        END AS code_pl,
        f.GroupPL,
        ISNULL(f.[ВГО], 0) AS is_intercompany,
        CASE
            WHEN f.Scenario = N'факт' AND f.GroupPL IN (N'ПРОДАЖИ', N'ПРОДАЖИ с НДС с самовыв_', N'ПРОЧИЕ ДОХОДЫ', N'ПРОЦЕНТЫ (Депозиты, вложения, ЦБ)', N'ОТГРУЗКИ', N'ОТГРУЗКИ С НДС') THEN -1
            WHEN f.CodePL IN (N'4001', N'4004') THEN -1
            ELSE 1
        END * CASE WHEN f.CodePL IN (N'1037', N'1042', N'2037') THEN 0.55 ELSE 1 END AS amount_sign,
        CAST(ISNULL(f.AmountBYN, 0) AS decimal(19,4)) AS amount_byn,
        CAST(ISNULL(f.AmountUSD, 0) AS decimal(19,4)) AS amount_usd,
        CAST(ISNULL(f.AmountRUB, 0) AS decimal(19,4)) AS amount_rub
    FROM FinDWH.dbo.Table_Fin_PL f
    WHERE f.[Month] >= '2025-01-01'
)
SELECT
    s.month_date,
    s.code_cfo,
    s.legal_entity,
    s.country_code,
    s.scenario_src,
    s.kontur,
    COALESCE(tpl.row_template, {_GROUP_FALLBACK_SQL}) AS row_template,
    s.is_intercompany,
    CASE WHEN s.country_code = N'KZ' AND s.code_pl IN (N'1006', N'1012') THEN 1 ELSE 0 END AS kz_denominator,
    SUM(CAST(s.amount_byn * s.amount_sign AS decimal(19,4))) AS amount_byn,
    SUM(CAST(s.amount_usd * s.amount_sign AS decimal(19,4))) AS amount_usd,
    SUM(CAST(s.amount_rub * s.amount_sign AS decimal(19,4))) AS amount_rub
FROM src s
LEFT JOIN tpl ON tpl.code_pl = s.code_pl
GROUP BY
    s.month_date, s.code_cfo, s.legal_entity, s.country_code, s.scenario_src, s.kontur,
    s.is_intercompany,
    COALESCE(tpl.row_template, {_GROUP_FALLBACK_SQL}),
    CASE WHEN s.country_code = N'KZ' AND s.code_pl IN (N'1006', N'1012') THEN 1 ELSE 0 END
"""

# Порт fact_HR.tmdl: каскад месяцы → ставки (LAG правит «Увольнение» с нулевой
# ставкой) → сегменты (LEAD) → ЦФО подразделения по частоте (ROW_NUMBER) →
# декреты (leave_months обнуляют ставку). Проверен живым прогоном 13.10.2026:
# 2026-06-01 SUM = 7 203,16 — контрольное число PBI.
SQL_FOT_HEADCOUNT = """
SET NOCOUNT ON;
WITH months AS (
    SELECT DISTINCT EOMONTH(period_date) AS month_end
    FROM onec_data.dbo.mot_personnel_history
    WHERE period_date >= '20250101' AND period_date <= EOMONTH(GETDATE())
),
fixed_rates AS (
    SELECT id, country, organization_name, inn, employee_name, personnel_number,
           department_name, period_date, operation_type,
           CASE WHEN operation_type = N'Увольнение' AND (rate_value IS NULL OR rate_value = 0)
                THEN LAG(CASE WHEN rate_value > 0 THEN rate_value END)
                     OVER (PARTITION BY country, organization_name, inn, employee_name, personnel_number
                           ORDER BY period_date, id)
                ELSE rate_value
           END AS rate_fixed
    FROM onec_data.dbo.mot_personnel_history
),
segments AS (
    SELECT *, LEAD(period_date) OVER (PARTITION BY country, organization_name, inn, employee_name, personnel_number
                                      ORDER BY period_date, id) AS next_date
    FROM fixed_rates
),
dept_cfo AS (
    SELECT store_name, country, codecfo
    FROM (
        SELECT store_name, country,
               CASE WHEN country = N'РБ'
                    THEN TRY_CAST(NULLIF(LEFT(cfo_code, CASE WHEN LEN(cfo_code) > 2 THEN LEN(cfo_code) - 2 ELSE 0 END), '') AS int)
                    ELSE TRY_CAST(cfo_code AS int)
               END AS codecfo,
               ROW_NUMBER() OVER (PARTITION BY store_name, country
                                  ORDER BY COUNT(*) DESC,
                                           CASE WHEN country = N'РБ'
                                                THEN TRY_CAST(NULLIF(LEFT(cfo_code, CASE WHEN LEN(cfo_code) > 2 THEN LEN(cfo_code) - 2 ELSE 0 END), '') AS int)
                                                ELSE TRY_CAST(cfo_code AS int)
                                           END) AS rn
        FROM onec_data.dbo.staff_registry
        WHERE store_name IS NOT NULL AND state = N'Активный'
          AND cfo_code IS NOT NULL AND cfo_code <> '0' AND cfo_code <> ''
          AND country IS NOT NULL AND country <> '0'
        GROUP BY store_name, country,
                 CASE WHEN country = N'РБ'
                      THEN TRY_CAST(NULLIF(LEFT(cfo_code, CASE WHEN LEN(cfo_code) > 2 THEN LEN(cfo_code) - 2 ELSE 0 END), '') AS int)
                      ELSE TRY_CAST(cfo_code AS int)
                 END
    ) t
    WHERE rn = 1
),
dept_has_rb AS (
    SELECT store_name, MAX(CASE WHEN country = N'РБ' THEN 1 ELSE 0 END) AS has_rb
    FROM onec_data.dbo.staff_registry
    WHERE store_name IS NOT NULL AND state = N'Активный'
      AND cfo_code IS NOT NULL AND cfo_code <> '0' AND cfo_code <> ''
    GROUP BY store_name
),
dept_cfo_any AS (
    SELECT store_name, codecfo
    FROM (
        SELECT s.store_name,
               CASE WHEN hr.has_rb = 1
                    THEN TRY_CAST(NULLIF(LEFT(s.cfo_code, CASE WHEN LEN(s.cfo_code) > 2 THEN LEN(s.cfo_code) - 2 ELSE 0 END), '') AS int)
                    ELSE TRY_CAST(s.cfo_code AS int)
               END AS codecfo,
               ROW_NUMBER() OVER (PARTITION BY s.store_name
                                  ORDER BY COUNT(*) DESC,
                                           CASE WHEN hr.has_rb = 1
                                                THEN TRY_CAST(NULLIF(LEFT(s.cfo_code, CASE WHEN LEN(s.cfo_code) > 2 THEN LEN(s.cfo_code) - 2 ELSE 0 END), '') AS int)
                                                ELSE TRY_CAST(s.cfo_code AS int)
                                           END) AS rn
        FROM onec_data.dbo.staff_registry s
        JOIN dept_has_rb hr ON s.store_name = hr.store_name
        WHERE s.store_name IS NOT NULL AND s.state = N'Активный'
          AND s.cfo_code IS NOT NULL AND s.cfo_code <> '0' AND s.cfo_code <> ''
        GROUP BY s.store_name,
                 CASE WHEN hr.has_rb = 1
                      THEN TRY_CAST(NULLIF(LEFT(s.cfo_code, CASE WHEN LEN(s.cfo_code) > 2 THEN LEN(s.cfo_code) - 2 ELSE 0 END), '') AS int)
                      ELSE TRY_CAST(s.cfo_code AS int)
                 END
    ) t
    WHERE rn = 1
),
leave_months AS (
    SELECT DISTINCT m.month_end, LTRIM(RTRIM(h.inn)) AS inn, LTRIM(RTRIM(h.personnel_number)) AS personnel_number
    FROM months m
    JOIN onec_data.dbo.staff_registry_history h
      ON h.period_from <= m.month_end AND (h.period_to IS NULL OR h.period_to >= m.month_end)
    WHERE h.state IN (
        N'В отпуске по беременности и родам',
        N'В отпуске по уходу за ребенком',
        N'По уходу за ребенком до 3 лет (ребенком-инвалидом до 18 лет) по болезни матери',
        N'Отпуск по беременности и родам',
        N'Отпуск по уходу за ребенком')
),
with_cfo AS (
    SELECT s.country, s.inn, m.month_end,
           CASE WHEN lv.personnel_number IS NULL THEN s.rate_fixed ELSE 0 END AS rate_fixed,
           COALESCE(dc.codecfo, dca.codecfo) AS CodeCFO
    FROM months m
    JOIN segments s
      ON s.period_date <= m.month_end AND (s.next_date IS NULL OR s.next_date > m.month_end)
     AND NOT (s.operation_type = N'Увольнение' AND s.period_date < m.month_end)
    LEFT JOIN dept_cfo dc
      ON s.department_name = dc.store_name AND (s.country = dc.country OR (s.country IS NULL AND dc.country IS NULL))
    LEFT JOIN dept_cfo_any dca ON s.department_name = dca.store_name
    LEFT JOIN leave_months lv
      ON lv.inn = LTRIM(RTRIM(s.inn)) AND lv.personnel_number = LTRIM(RTRIM(s.personnel_number)) AND lv.month_end = m.month_end
)
SELECT
    DATEFROMPARTS(YEAR(w.month_end), MONTH(w.month_end), 1) AS MonthDate,
    ISNULL(CAST(w.CodeCFO AS nvarchar(20)), N'') AS CodeCFO,
    ISNULL(NULLIF(cm.co, N''), N'(не указана)') AS CompanyID,
    CASE w.country
        WHEN N'РБ' THEN N'BY'
        WHEN N'РФ' THEN N'RU'
        WHEN N'КЗ' THEN N'KZ'
        WHEN N'УЗ' THEN N'UZ'
        ELSE ISNULL(NULLIF(LTRIM(RTRIM(w.country)), N''), N'(не указана)')
    END AS Country,
    CAST(COALESCE(SUM(w.rate_fixed), 0) AS decimal(9,2)) AS Quantity
FROM with_cfo w
LEFT JOIN (
    SELECT LTRIM(RTRIM(UNP)) AS unp, MAX(LTRIM(RTRIM(CompanyMFNameFinDWH))) AS co
    FROM FinDWH.dbo.CompaniesMF
    GROUP BY LTRIM(RTRIM(UNP))
) cm ON cm.unp = LTRIM(RTRIM(w.inn))
GROUP BY w.month_end, w.CodeCFO, cm.co, w.country
"""

# Порт fact_HRPlan.tmdl (Budgeting.dbo.PlanChisl).
SQL_FOT_HEADCOUNT_PLAN = """
SELECT
    LTRIM(RTRIM(pc.[КОД ЦФО])) AS CodeCFO,
    CAST(COALESCE(SUM(pc.[УТВЕРЖДЕННАЯ ЧИСЛЕННОСТЬ]), 0) AS decimal(9,2)) AS PlanApproved,
    CAST(COALESCE(SUM(pc.[Численность тактика]), 0) AS decimal(9,2)) AS PlanTactic
FROM Budgeting.dbo.PlanChisl pc
WHERE NULLIF(LTRIM(RTRIM(pc.[КОД ЦФО])), '') IS NOT NULL
GROUP BY LTRIM(RTRIM(pc.[КОД ЦФО]))
"""

# Порт dim_CFO.tmdl: направление = MAX(GroupCFO3) по ЦФО с 2025 года; тип
# направления — по продажам факта P&L группы (порог 1 млн BYN). В самом
# dim_CFO.tmdl колонки активности НЕТ: в PBI мера «_ЦФО активно» = «по ЦФО есть
# данные в выбранном периоде» (её воспроизводит шаг 2 JOIN'ом с fot_monthly).
# Здесь is_active = «ЦФО не закрыт»: DateClose — ещё не наступившая дата, а
# 1900-01-01 — заглушка пустой даты в источнике (619 из 691 строк).
SQL_FOT_CFO = """
SELECT
    LTRIM(RTRIM(d.CodeCFO)) AS code_cfo,
    ISNULL(NULLIF(LTRIM(RTRIM(d.CFO)), N''), N'(без названия)') AS cfo_name,
    LTRIM(RTRIM(d.CodeCFO)) + N' — ' + ISNULL(NULLIF(LTRIM(RTRIM(d.CFO)), N''), N'(без названия)') AS cfo_label,
    ISNULL(NULLIF(LTRIM(RTRIM(d.GroupCFO1)), N''), N'(не указана)') AS group_cfo1,
    ISNULL(NULLIF(LTRIM(RTRIM(d.GroupCFO2)), N''), N'(не указана)') AS group_cfo2,
    ISNULL(g3.GroupCFO3, N'(не указано)') AS direction,
    CASE WHEN ABS(ISNULL(sl.Sales, 0)) > 1000000 THEN N'Канал продаж' ELSE N'Затратное подразделение' END AS direction_kind,
    ISNULL(NULLIF(LTRIM(RTRIM(d.Country)), N''), N'(не указана)') AS country_code,
    ISNULL(NULLIF(LTRIM(RTRIM(d.Channel)), N''), N'(не указан)') AS sales_channel,
    d.DateOpen AS date_opened,
    d.DateClose AS date_closed,
    CASE WHEN d.DateClose IS NULL
              OR d.DateClose <= '1900-01-02'
              OR d.DateClose >= CAST(GETDATE() AS date)
         THEN 1 ELSE 0 END AS is_active
FROM FinDWH.dbo.[001 CodeCFO] d
LEFT JOIN (
    SELECT f.CodeCFO, MAX(LTRIM(RTRIM(f.GroupCFO3))) AS GroupCFO3
    FROM FinDWH.dbo.Table_Fin_PL f
    WHERE f.[Month] >= '2025-01-01' AND NULLIF(LTRIM(RTRIM(f.GroupCFO3)), '') IS NOT NULL
    GROUP BY f.CodeCFO
) g3 ON g3.CodeCFO = d.CodeCFO
LEFT JOIN (
    SELECT LTRIM(RTRIM(f.GroupCFO3)) AS GroupCFO3, SUM(f.AmountBYN) AS Sales
    FROM FinDWH.dbo.Table_Fin_PL f
    WHERE f.Scenario = N'факт' AND f.[Для отчета] = 'PL' AND f.GroupPL = N'ПРОДАЖИ'
      AND f.[Month] >= '20250101' AND NULLIF(LTRIM(RTRIM(f.GroupCFO3)), '') IS NOT NULL
    GROUP BY LTRIM(RTRIM(f.GroupCFO3))
) sl ON sl.GroupCFO3 = g3.GroupCFO3
"""

# Порт dim_Closed.tmdl.
SQL_CLOSED_PERIOD = """
SELECT CAST(MIN(ClosedPeriod) AS date) AS ClosedPeriod
FROM FinDWH.dbo.CompaniesMF
WHERE ClosedPeriod IS NOT NULL
"""

SQL_INSERT_MONTHLY = """
INSERT INTO fot_monthly (month_date, code_cfo, legal_entity, country_code, scenario_src,
                         kontur, row_template, is_intercompany, kz_denominator,
                         amount_byn, amount_usd, amount_rub)
VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12)
"""

SQL_INSERT_HEADCOUNT = """
INSERT INTO fot_headcount (month_date, code_cfo, legal_entity, country_code, quantity)
VALUES ($1, $2, $3, $4, $5)
"""

SQL_INSERT_HEADCOUNT_PLAN = """
INSERT INTO fot_headcount_plan (code_cfo, plan_approved, plan_tactic)
VALUES ($1, $2, $3)
"""

SQL_INSERT_CFO = """
INSERT INTO fot_cfo (code_cfo, cfo_name, cfo_label, group_cfo1, group_cfo2, direction,
                     direction_kind, country_code, sales_channel, date_opened, date_closed, is_active)
VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12)
"""


# ── Чтение MSSQL (синхронно, вызывается из run_in_executor) ──────────────────

def _chunks(seq: list, size: int = 20000):
    for i in range(0, len(seq), size):
        yield seq[i:i + size]


def _as_date(v):
    if isinstance(v, datetime):
        return v.date()
    return v


def _fetch_mssql_all() -> dict:
    """Выполняет все загрузчики последовательно на одном pyodbc-соединении.

    Возвращает словарь со строками витрин; записи в Postgres делает отдельная
    функция в ОДНОЙ транзакции, поэтому долгие чтения MSSQL (минуты) не держат
    блокировок в БД.
    """
    t0 = time.monotonic()
    conn = get_olap_conn()
    conn.timeout = FOT_QUERY_TIMEOUT
    try:
        cur = conn.cursor()

        log(logging.INFO, "fot: загрузка fot_monthly из MSSQL", database=FOT_DATABASE)
        monthly = [
            (r[0], r[1], r[2], r[3], r[4], r[5], r[6], int(r[7]), bool(r[8]), r[9], r[10], r[11])
            for r in cur.execute(SQL_FOT_MONTHLY).fetchall()
        ]

        log(logging.INFO, "fot: загрузка fot_headcount из MSSQL")
        headcount = [
            (_as_date(r[0]), r[1], r[2], r[3], r[4])
            for r in cur.execute(SQL_FOT_HEADCOUNT).fetchall()
        ]

        log(logging.INFO, "fot: загрузка fot_headcount_plan из MSSQL")
        plan = [tuple(r) for r in cur.execute(SQL_FOT_HEADCOUNT_PLAN).fetchall()]

        log(logging.INFO, "fot: загрузка fot_cfo из MSSQL")
        cfo_seen: set[str] = set()
        cfo = []
        for r in cur.execute(SQL_FOT_CFO).fetchall():
            if r[0] in cfo_seen:  # PK: дубли кода ЦФО в справочнике пропускаем
                continue
            cfo_seen.add(r[0])
            cfo.append((r[0], r[1], r[2], r[3], r[4], r[5], r[6], r[7], r[8],
                        _as_date(r[9]), _as_date(r[10]), bool(r[11])))

        log(logging.INFO, "fot: загрузка закрытого периода из MSSQL")
        closed_row = cur.execute(SQL_CLOSED_PERIOD).fetchone()
        closed_period = _as_date(closed_row[0]) if closed_row else None

        log(logging.INFO, "fot: MSSQL загружен за %.1f с" % (time.monotonic() - t0))
        return {
            "monthly": monthly,
            "headcount": headcount,
            "plan": plan,
            "cfo": cfo,
            "closed_period": closed_period,
        }
    finally:
        try:
            conn.close()
        except Exception:
            pass


async def _write_vitrines(fetched: dict) -> dict:
    """Перезаписывает все витрины в ОДНОЙ транзакции (TRUNCATE+INSERT каждой).

    Читатели через MVCC видят либо целиком старый снимок, либо целиком новый;
    при ошибке посреди загрузки транзакция откатывается и данные не трогаются
    (serve-stale).
    """
    async with pool().acquire() as pg:
        async with pg.transaction():
            await pg.execute("TRUNCATE TABLE fot_monthly")
            for chunk in _chunks(fetched["monthly"]):
                await pg.executemany(SQL_INSERT_MONTHLY, chunk)

            await pg.execute("TRUNCATE TABLE fot_headcount")
            for chunk in _chunks(fetched["headcount"]):
                await pg.executemany(SQL_INSERT_HEADCOUNT, chunk)

            await pg.execute("TRUNCATE TABLE fot_headcount_plan")
            if fetched["plan"]:
                await pg.executemany(SQL_INSERT_HEADCOUNT_PLAN, fetched["plan"])

            await pg.execute("TRUNCATE TABLE fot_cfo")
            for chunk in _chunks(fetched["cfo"]):
                await pg.executemany(SQL_INSERT_CFO, chunk)

            counts = {
                "fot_monthly": len(fetched["monthly"]),
                "fot_headcount": len(fetched["headcount"]),
                "fot_headcount_plan": len(fetched["plan"]),
                "fot_cfo": len(fetched["cfo"]),
            }
            fallback_closed = await pg.fetchval(
                "SELECT MAX(month_date) FROM fot_monthly WHERE scenario_src = 'факт'"
            )
            await pg.execute(
                "UPDATE fot_meta SET closed_period = $1, fallback_closed = $2,"
                " refreshed_at = now(), refreshing_since = NULL, source_counts = $3::jsonb"
                " WHERE id = 1",
                fetched["closed_period"], fallback_closed,
                json.dumps(counts, ensure_ascii=False),
            )
    return counts


# ── Refresh: TTL + stale-while-revalidate ────────────────────────────────────

async def refresh_fot(force: bool = False) -> dict:
    """Обновляет витрины ФОТ, если данные устарели (или по force=True).

    Возвращает {'success': True, 'counts': …, 'elapsed_s': …},
    {'skipped': True, 'reason': …} или {'success': False, 'error': …}.
    Данные при ошибке НЕ трогаются — API продолжает отдавать последний снимок.
    """
    if fot_mock_enabled():
        return {"skipped": True, "reason": "mock"}

    t0 = time.monotonic()
    if not force:
        try:
            async with acquire() as pg:
                row = await pg.fetchrow(
                    "SELECT refreshed_at FROM fot_meta WHERE id = 1"
                )
            if row and row["refreshed_at"] is not None:
                age = (datetime.now(timezone.utc) - row["refreshed_at"]).total_seconds()
                if age < FOT_REFRESH_MINUTES * 60:
                    return {"skipped": True, "reason": "fresh"}
        except Exception:
            pass  # витрин/БД ещё нет — попробуем загрузить

    # Захват через fot_meta.refreshing_since. Не-force прогон пропускает чужую
    # живую загрузку и перехватывает только «застрявший» гард: по опыту полная
    # загрузка укладывается в ~5 минут, поэтому гард старше 15 минут считается
    # мёртвым (упавший прогон не смог его снять). force=True — ручной прогон:
    # снимает чужой гард безусловно.
    if force:
        claim_sql = "UPDATE fot_meta SET refreshing_since = now() WHERE id = 1 RETURNING 1"
    else:
        claim_sql = (
            "UPDATE fot_meta SET refreshing_since = now()"
            " WHERE id = 1 AND (refreshing_since IS NULL"
            " OR refreshing_since < now() - interval '15 minutes')"
            " RETURNING 1"
        )
    async with acquire() as pg:
        claimed = await pg.fetchval(claim_sql)
    if claimed is None:
        return {"skipped": True, "reason": "refresh-in-progress"}

    loop = asyncio.get_running_loop()
    done = False
    try:
        fetched = await loop.run_in_executor(None, _fetch_mssql_all)
        counts = await _write_vitrines(fetched)
        done = True
        elapsed = round(time.monotonic() - t0, 1)
        log(logging.INFO, "fot: витрины обновлены", elapsed_s=elapsed, **counts)
        return {"success": True, "counts": counts, "elapsed_s": elapsed}
    except Exception as e:  # noqa: BLE001 — воркер должен переживать всё
        log(logging.ERROR, "fot: обновление витрин не удалось, отдаём предыдущий снимок",
            error=str(e)[:800])
        return {"success": False, "error": str(e)[:800]}
    finally:
        if not done:
            # Гард снимаем при ЛЮБОМ выходе без записи витрин — ошибка, отмена
            # воркера, смерть процесса: finally выполняется и при CancelledError,
            # так что «застрявший» гард больше не образуется. При успехе гард уже
            # снят в _write_vitrines той же транзакцией, что и данные.
            try:
                async with acquire() as pg:
                    await pg.execute("UPDATE fot_meta SET refreshing_since = NULL WHERE id = 1")
            except Exception:
                pass


async def fot_meta() -> dict:
    """Состояние витрин: закрытый период, свежесть, счётчики источников."""
    async with acquire() as pg:
        row = await pg.fetchrow(
            "SELECT closed_period, fallback_closed, refreshed_at, refreshing_since, source_counts"
            " FROM fot_meta WHERE id = 1"
        )
    if row is None:
        return {"stale": True, "refreshed_at": None, "source_counts": {}}
    refreshed_at = row["refreshed_at"]
    stale = refreshed_at is None or (
        datetime.now(timezone.utc) - refreshed_at
    ).total_seconds() >= FOT_REFRESH_MINUTES * 60
    return {
        "closed_period": row["closed_period"].isoformat() if row["closed_period"] else None,
        "fallback_closed": row["fallback_closed"].isoformat() if row["fallback_closed"] else None,
        "refreshed_at": refreshed_at.isoformat() if refreshed_at else None,
        "refreshing_since": row["refreshing_since"].isoformat() if row["refreshing_since"] else None,
        "source_counts": json.loads(row["source_counts"] or "{}"),
        "stale": stale,
    }


async def fot_worker() -> None:
    """Фоновый воркер витрин ФОТ для lifespan (main.py).

    Тикает раз в минуту; решение «пора ли обновлять» принимает refresh_fot()
    по TTL, так что интервал тика не обязан совпадать с FOT_REFRESH_MINUTES.
    Любая ошибка гасится в лог — воркер не должен укладывать процесс.
    """
    while True:
        try:
            result = await refresh_fot(force=False)
            if result.get("success"):
                log(logging.INFO, "fot: воркер обновил витрины", elapsed_s=result.get("elapsed_s"))
            elif result.get("error"):
                log(logging.WARNING, "fot: воркер: обновление не удалось", error=result.get("error"))
        except asyncio.CancelledError:
            raise
        except Exception as e:  # noqa: BLE001
            log(logging.WARNING, "fot: воркер поймал ошибку", error=str(e)[:500])
        await asyncio.sleep(60)


# ── Публичные сборщики (шаг 2) ───────────────────────────────────────────────

# Валюты ответа: значение среза → колонка витрины. Имя колонки подставляется в
# SQL только из этого белого списка — из запроса приходят лишь значения.
FOT_CURRENCIES = {"BYN": "amount_byn", "USD": "amount_usd", "RUB": "amount_rub"}

# Срез «План»: значение фильтра совпадает с именем сценария в витрине
# (scenario_src). «Бюджет»-колонки P10/P11 жёстко берут сценарий «бюджет».
FOT_PLAN_SCENARIOS = ("бюджет", "тактика", "НовСтрат")

# Статические опции. Страны — только отчётные: прочие country_code источника
# (TR/CN/BR, «Error») — шум выгрузки, срезов по ним в PBI нет.
COUNTRY_OPTIONS = (("BY", "Беларусь"), ("RU", "Россия"), ("KZ", "Казахстан"), ("UZ", "Узбекистан"))
SCENARIO_OPTIONS = (("бюджет", "Бюджет"), ("тактика", "Тактика"), ("НовСтрат", "Новая стратегия"))
IC_OPTIONS = (("0", "Без ВГО"), ("1", "ВГО"))

# Уровни матрицы «По подразделениям» (порядок дрилла) и предел строк выборки.
MATRIX_LEVELS = (
    ("group_cfo1", "Группа ЦФО"),
    ("group_cfo2", "Подгруппа ЦФО"),
    ("direction", "Направление"),
    ("code_cfo", "ЦФО"),
)
MATRIX_ROW_LIMIT = 500

# Семейства мер одним условием: ФОТ-контекст (ЗП в P&L + Производстве) и выручка
# («Продажи без НДС» в P&L). Знаки уже применены при загрузке витрин.
_SQL_FOT_FAMILY = "(m.row_template = 'Заработная плата' AND m.kontur IN ('P&L', 'Производство (BS)'))"
_SQL_REV_FAMILY = "(m.row_template = 'Продажи без НДС' AND m.kontur = 'P&L')"

METHODOLOGY_P9 = (
    "ФОТ = статьи «Заработная плата» в контурах P&L и «Производство (BS)» за выбранный "
    "месяц, не позже закрытого периода. Факт — сценарий «факт»; лимит — сценарий из среза "
    "«План» (Бюджет/Тактика/НовСтрат). «ФОТ % от выручки» = ФОТ / «Продажи без НДС» "
    "(контур P&L; для лимита — тот же план-сценарий). Знаменатель по стране: KZ — продажи "
    "KZ по кодам 1006/1012, UZ — все продажи UZ, остальное (BY/RU/микс) — вся выручка "
    "группы; срезы ЦФО и юрлица на знаменатель не влияют. «% от выручки своей страны» в "
    "матрице берёт страну строки из справочника ЦФО, у строк-смесей — вся выручка группы. "
    "Численность — ставки на 1-е число выбранного месяца; «Числ. план (тактика)» — план "
    "численности PlanChisl по ЦФО (на юрлица не распределён, срез юрлица на него не "
    "влияет). Суммы — в выбранной валюте витрины (BYN/USD/RUB), без пересчёта. График — "
    "месяцы выбранного года с января по выбранный месяц."
)

_SCENARIO_LABELS = dict(SCENARIO_OPTIONS)

# Ключ уровня матрицы → колонка зерна lvl (для условий проваливания).
_LEVEL_SQL_COLUMN = {"group_cfo1": "l.g1", "group_cfo2": "l.g2", "direction": "l.dir"}


def _r2(v) -> float | None:
    """Число для JSON: None остаётся None, остальное — float с двумя знаками."""
    return None if v is None else round(float(v), 2)


def _closed_from_meta(meta: dict) -> date | None:
    """Закрытый период витрин: closed_period, при его отсутствии — fallback."""
    for key in ("closed_period", "fallback_closed"):
        raw = meta.get(key)
        if raw:
            try:
                return date.fromisoformat(str(raw)[:10])
            except ValueError:
                continue
    return None


async def filter_options(year: int | None = None) -> dict:
    """Каскадные опции фильтров дашборда.

    Иерархия ЦФО — из fot_cfo. На подгруппах ЦФО и самих ЦФО оставляем только
    ЦФО с данными fot_monthly в выбранном году — это порт меры «_ЦФО активно»
    из PBI («по ЦФО есть данные в выбранном периоде»); направления и группы
    ЦФО — весь справочник: у части ЦФО без оборота группы пустыми не бывают,
    а фильтр по пустой группе просто даст пустую выборку. Юрлица и годы —
    DISTINCT из fot_monthly. Страны, сценарии плана, ВГО и месяцы — статика.

    *year* ограничивает списки подгрупп и ЦФО (каскад по году); по умолчанию —
    плановый год YEAR_PLAN.
    """
    if year is None:
        year = YEAR_PLAN
    async with acquire() as pg:
        rows = await pg.fetch(
            """
            WITH act AS (
                SELECT DISTINCT code_cfo FROM fot_monthly
                WHERE date_part('year', month_date) = $1::int
            )
            SELECT kind, id, text FROM (
                SELECT DISTINCT 'direction' AS kind, direction AS id, direction AS text
                    FROM fot_cfo WHERE coalesce(direction, '') <> ''
                UNION ALL
                SELECT DISTINCT 'group', group_cfo1, group_cfo1
                    FROM fot_cfo WHERE coalesce(group_cfo1, '') <> ''
                UNION ALL
                SELECT DISTINCT 'subgroup', c.group_cfo2, c.group_cfo2
                    FROM fot_cfo c JOIN act a ON a.code_cfo = c.code_cfo
                    WHERE coalesce(c.group_cfo2, '') <> ''
                UNION ALL
                SELECT 'cfo', c.code_cfo, c.cfo_label
                    FROM fot_cfo c JOIN act a ON a.code_cfo = c.code_cfo
                UNION ALL
                SELECT DISTINCT 'legal', m.legal_entity, m.legal_entity
                    FROM fot_monthly m WHERE coalesce(m.legal_entity, '') <> ''
                UNION ALL
                SELECT DISTINCT 'year', date_part('year', m.month_date)::int::text,
                       date_part('year', m.month_date)::int::text
                    FROM fot_monthly m
            ) t
            ORDER BY
                CASE kind WHEN 'direction' THEN 1 WHEN 'group' THEN 2 WHEN 'subgroup' THEN 3
                          WHEN 'cfo' THEN 4 WHEN 'legal' THEN 5 ELSE 6 END,
                -- коды ЦФО не дополнены нулями: '1001' должен идти до '101'
                CASE WHEN kind IN ('cfo', 'year') AND id ~ '^[0-9]+$'
                     THEN lpad(id, 12, '0') ELSE id END
            """,
            year,
        )
        meta = await fot_meta()

    key_by_kind = {
        "direction": "directions",
        "group": "cfo_groups",
        "subgroup": "cfo_subgroups",
        "cfo": "cfos",
        "legal": "legal_entities",
        "year": "years",
    }
    buckets: dict = {v: [] for v in key_by_kind.values()}
    for r in rows:
        buckets[key_by_kind[r["kind"]]].append({"id": r["id"], "text": r["text"]})
    buckets["countries"] = [{"id": c, "text": t} for c, t in COUNTRY_OPTIONS]
    buckets["scenarios"] = [{"id": s, "text": t} for s, t in SCENARIO_OPTIONS]
    buckets["intercompany"] = [{"id": i, "text": t} for i, t in IC_OPTIONS]
    buckets["months"] = [{"id": str(m), "text": str(m)} for m in range(1, 13)]
    buckets["meta"] = meta
    return buckets


def _parse_int(value, name: str, default: int, lo: int, hi: int) -> int:
    """Целый срез с дефолтом и границами; вне диапазона — ValueError (→ 400)."""
    if value is None or value == "":
        return default
    try:
        n = int(value)
    except (TypeError, ValueError):
        raise ValueError("fot: %s — ожидалось целое число, получено %r" % (name, value))
    if not lo <= n <= hi:
        raise ValueError("fot: %s вне диапазона %d..%d" % (name, lo, hi))
    return n


def _parse_str_list(values) -> list[str]:
    """Списочный срез: пустые и None-значения отбрасываем, пробелы срезаем.

    Одиночная строка трактуется как ОДНО значение, а не как итерируемое по
    символам: иначе прямой вызов dashboard() со скаляром «KZ» молча искал бы
    страны 'K' и 'Z' и отдавал пустую выборку (через HTTP это не грозит —
    qp.getlist всегда даёт список, но контракт функции не должен зависеть от
    способа вызова).
    """
    if isinstance(values, str):
        values = [values]
    return [str(v).strip() for v in (values or []) if v is not None and str(v).strip()]


def _parse_p9_params(params: dict, meta: dict) -> dict:
    """Разбор и валидация срезов P9. Неизвестные значения — ValueError (→ 400).

    Списочные срезы (direction, cfo_group, cfo_subgroup, cfo, legal_entity,
    country, matrix_path) приходят списками, одиночные (scenario_plan,
    intercompany, year, month, currency) — скалярами; все необязательные.
    """
    closed = _closed_from_meta(meta)
    p = {
        "directions": _parse_str_list(params.get("direction")),
        "cfo_groups": _parse_str_list(params.get("cfo_group")),
        "cfo_subgroups": _parse_str_list(params.get("cfo_subgroup")),
        "cfos": _parse_str_list(params.get("cfo")),
        "legal_entities": _parse_str_list(params.get("legal_entity")),
        "countries": _parse_str_list(params.get("country")),
        "matrix_path": _parse_str_list(params.get("matrix_path")),
    }
    p["scenario_plan"] = str(params.get("scenario_plan") or "тактика").strip()
    if p["scenario_plan"] not in FOT_PLAN_SCENARIOS:
        raise ValueError(
            "fot: scenario_plan — ожидалось одно из %s" % (", ".join(FOT_PLAN_SCENARIOS),)
        )
    p["currency"] = str(params.get("currency") or "BYN").strip().upper()
    if p["currency"] not in FOT_CURRENCIES:
        raise ValueError("fot: currency — ожидалось одно из %s" % ", ".join(FOT_CURRENCIES))
    p["intercompany"] = str(params.get("intercompany") or "все").strip().lower()
    if p["intercompany"] == "all":
        p["intercompany"] = "все"
    if p["intercompany"] not in ("все", "0", "1"):
        raise ValueError("fot: intercompany — ожидалось все | 0 | 1")
    p["year"] = _parse_int(params.get("year"), "year", YEAR_PLAN, 2000, 2100)
    p["month"] = _parse_int(
        params.get("month"), "month", closed.month if closed else 12, 1, 12
    )
    return p


async def _dashboard_p9(p: dict, meta: dict) -> dict:
    """Вкладка P9 «ФОТ и численность».

    Всё считается ОДНИМ запросом к витринам (паттерн margin.dashboard):
    карточки выбранного месяца, месячная серия графика и зерно матрицы
    по ЦФО с агрегацией до запрошенного уровня проваливания. Знаменатели
    долей (den) считаются отдельно — без срезов ЦФО и юрлица.
    """
    closed = _closed_from_meta(meta)
    year, month = p["year"], p["month"]
    currency = p["currency"]
    col = FOT_CURRENCIES[currency]
    md = date(year, month, 1)

    # График — месяцы выбранного года с января по выбранный месяц, но не дальше
    # закрытого периода: в источнике есть «факт» будущих месяцев (мусор в
    # контуре Производство (BS)), и без гард-а он попадал бы на график.
    chart_last = md
    if closed:
        chart_last = min(md, date(closed.year, closed.month, 1))
    n_months = chart_last.month if chart_last.year == year else 0

    warning = None
    if closed and md > closed:
        warning = (
            "Выбранный период %s позже закрытого (%s): факт за будущие месяцы в источнике "
            "не заполняется — карточки и матрица будут пустыми." % (md.isoformat(), closed.isoformat())
        )

    # Параметры собираем в порядке появления плейсхолдеров; каждый используется
    # в SQL столько раз, сколько нужно ($n адресные).
    sql_params: list = []

    def _pv(v) -> str:
        sql_params.append(v)
        return "$%d" % len(sql_params)

    p_closed = _pv(closed) if closed else None
    p_md = _pv(md)
    p_scen = _pv(p["scenario_plan"])
    # Знаменатель долей: выбрана ровно одна страна KZ/UZ — её выручка,
    # иначе (BY/RU/микс/пусто) — вся выручка группы.
    p_sel = _pv(p["countries"][0] if len(p["countries"]) == 1 else "")
    p_ystart = _pv(date(year, 1, 1))
    p_chart_last = _pv(chart_last)

    # Срез ВГО. Имена колонок в условиях ниже — из белых списков модуля.
    ic_sql = {"все": "TRUE", "0": "m.is_intercompany = 0", "1": "m.is_intercompany = 1"}[
        p["intercompany"]
    ]

    # Плейсхолдеры списочных срезов строим ОДИН раз: один и тот же список
    # используется в нескольких CTE (факты/численность/план), дубли не нужны.
    def _list_placeholder(values: list[str]):
        return _pv(list(values)) if values else None

    def _any_cond(expr: str, placeholder) -> str:
        if placeholder is None:
            return ""
        return " AND %s = ANY(%s::text[])" % (expr, placeholder)

    ph_g1 = _list_placeholder(p["cfo_groups"])
    ph_g2 = _list_placeholder(p["cfo_subgroups"])
    ph_dir = _list_placeholder(p["directions"])
    ph_cfo = _list_placeholder(p["cfos"])
    ph_country = _list_placeholder(p["countries"])
    ph_legal = _list_placeholder(p["legal_entities"])

    # Иерархия ЦФО живёт в справочнике: срезы — условия в dim (порт срезов
    # dim_CFO в PBI), а не в фактах, иначе «висячие» ЦФО терялись бы молча.
    dim_conds = (
        _any_cond("group_cfo1", ph_g1)
        + _any_cond("group_cfo2", ph_g2)
        + _any_cond("direction", ph_dir)
        + _any_cond("code_cfo", ph_cfo)
    )
    # При активном срезе иерархии строки без справочника отсекаем (LEFT JOIN
    # оставил бы все прочие ЦФО в корзине «(не указана)»); без среза —
    # оставляем: «висячие» коды витрины показываем корзиной.
    hier_gate = " AND c.code_cfo IS NOT NULL" if dim_conds else ""

    m_closed = " AND m.month_date <= %s" % p_closed if p_closed else ""
    h_closed = " AND h.month_date <= %s" % p_closed if p_closed else ""
    m_country = _any_cond("m.country_code", ph_country)
    m_legal = _any_cond("m.legal_entity", ph_legal)
    h_country = _any_cond("h.country_code", ph_country)
    h_legal = _any_cond("h.legal_entity", ph_legal)
    # У плана численности страны и юрлица нет: страна — через справочник ЦФО,
    # юрлицом план не режем вовсе (PlanChisl ведётся по ЦФО).
    p_country = _any_cond("c.country_code", ph_country)

    # Суммы FILTER'ом по семействам мер: один скан фактов даёт сразу ФОТ
    # (факт и план-сценарий) и выручку.
    def _fot_sum(scen_sql: str) -> str:
        return "sum(m.%s) FILTER (WHERE m.scenario_src = %s AND %s)" % (
            col, scen_sql, _SQL_FOT_FAMILY)

    def _rev_sum(scen_sql: str, extra: str = "") -> str:
        return "sum(m.%s) FILTER (WHERE m.scenario_src = %s%s AND %s)" % (
            col, scen_sql, extra, _SQL_REV_FAMILY)

    fact = "'факт'"
    # Знаменатель KZ — только коды 1006/1012 (флаг kz_denominator в зерне).
    kz_extra = " AND m.country_code = 'KZ' AND m.kz_denominator"
    uz_extra = " AND m.country_code = 'UZ'"

    # Уровень матрицы: длина пути определяет группировку (паттерн margin).
    path = p["matrix_path"][: len(MATRIX_LEVELS) - 1]
    level_key, level_label = MATRIX_LEVELS[len(path)]
    can_drill = len(path) < len(MATRIX_LEVELS) - 1
    path_conds = "".join(
        " AND %s = %s" % (_LEVEL_SQL_COLUMN[MATRIX_LEVELS[i][0]], _pv(v))
        for i, v in enumerate(path)
    )
    if level_key == "group_cfo1":
        dim_expr = "l.g1"
        label_expr = "CASE WHEN l.g1 = '' THEN '(не указана)' ELSE l.g1 END"
    elif level_key == "group_cfo2":
        dim_expr = "l.g2"
        label_expr = "CASE WHEN l.g2 = '' THEN '(не указана)' ELSE l.g2 END"
    elif level_key == "direction":
        dim_expr = "l.dir"
        label_expr = "CASE WHEN l.dir = '' THEN '(не указано)' ELSE l.dir END"
    else:  # code_cfo — метка ЦФО из справочника, у «висячих» — сам код
        dim_expr = "l.cfo"
        label_expr = ("coalesce(nullif(max(l.cfo_label), ''),"
                      " CASE WHEN l.cfo = '' THEN '(без кода)' ELSE l.cfo END)")

    sql = f"""
WITH dim AS (
    SELECT code_cfo, cfo_label, group_cfo1, group_cfo2, direction, country_code
    FROM fot_cfo
    WHERE TRUE{dim_conds}
),
den AS (
    -- Знаменатели долей: выручка БЕЗ срезов ЦФО и юрлица (они с знаменателя
    -- снимаются), с срезом ВГО и гардом закрытого периода.
    SELECT
        {_rev_sum(fact)} AS grp_f,
        {_rev_sum(p_scen)} AS grp_p,
        {_rev_sum(fact, kz_extra)} AS kz_f,
        {_rev_sum(p_scen, kz_extra)} AS kz_p,
        {_rev_sum(fact, uz_extra)} AS uz_f,
        {_rev_sum(p_scen, uz_extra)} AS uz_p
    FROM fot_monthly m
    WHERE m.month_date = {p_md}{m_closed} AND {ic_sql}
),
cards AS (
    SELECT
        {_fot_sum(fact)} AS fot_fact,
        {_fot_sum(p_scen)} AS fot_limit,
        {_rev_sum(fact)} AS rev_fact
    FROM fot_monthly m
    LEFT JOIN dim c ON c.code_cfo = m.code_cfo
    WHERE m.month_date = {p_md} AND ({_SQL_FOT_FAMILY} OR {_SQL_REV_FAMILY}){m_closed}
      AND {ic_sql}{m_country}{m_legal}{hier_gate}
),
hc AS (
    SELECT sum(h.quantity) AS q
    FROM fot_headcount h
    LEFT JOIN dim c ON c.code_cfo = h.code_cfo
    WHERE h.month_date = {p_md}{h_closed}{h_country}{h_legal}{hier_gate}
),
months AS (
    SELECT to_char(m.month_date, 'YYYY-MM') AS ym, {_fot_sum(fact)} AS fot_fact
    FROM fot_monthly m
    LEFT JOIN dim c ON c.code_cfo = m.code_cfo
    WHERE m.month_date >= {p_ystart} AND m.month_date <= {p_chart_last}
      AND ({_SQL_FOT_FAMILY} OR {_SQL_REV_FAMILY}) AND {ic_sql}{m_country}{m_legal}{hier_gate}
    GROUP BY 1
),
hcm AS (
    SELECT to_char(h.month_date, 'YYYY-MM') AS ym, sum(h.quantity) AS q
    FROM fot_headcount h
    LEFT JOIN dim c ON c.code_cfo = h.code_cfo
    WHERE h.month_date >= {p_ystart} AND h.month_date <= {p_chart_last}
      {h_country}{h_legal}{hier_gate}
    GROUP BY 1
),
grain AS (
    -- Зерно ЦФО: карточные суммы на каждый код; row_country — страна строки
    -- для «% от выручки своей страны», у смесей стран — NULL (знаменатель ГК).
    SELECT
        coalesce(c.group_cfo1, '') AS g1,
        coalesce(c.group_cfo2, '') AS g2,
        coalesce(c.direction, '') AS dir,
        coalesce(m.code_cfo, '') AS cfo,
        max(c.cfo_label) AS cfo_label,
        CASE WHEN min(c.country_code) = max(c.country_code)
             THEN max(c.country_code) END AS row_country,
        {_fot_sum(fact)} AS fot_fact,
        {_fot_sum(p_scen)} AS fot_limit,
        {_rev_sum(fact)} AS rev_fact
    FROM fot_monthly m
    LEFT JOIN dim c ON c.code_cfo = m.code_cfo
    WHERE m.month_date = {p_md} AND ({_SQL_FOT_FAMILY} OR {_SQL_REV_FAMILY}){m_closed}
      AND {ic_sql}{m_country}{m_legal}{hier_gate}
    GROUP BY 1, 2, 3, 4
),
hc_grain AS (
    SELECT coalesce(c.group_cfo1, '') AS g1, coalesce(c.group_cfo2, '') AS g2,
           coalesce(c.direction, '') AS dir, coalesce(h.code_cfo, '') AS cfo,
           sum(h.quantity) AS q
    FROM fot_headcount h
    LEFT JOIN dim c ON c.code_cfo = h.code_cfo
    WHERE h.month_date = {p_md}{h_closed}{h_country}{h_legal}{hier_gate}
    GROUP BY 1, 2, 3, 4
),
plan_grain AS (
    SELECT coalesce(c.group_cfo1, '') AS g1, coalesce(c.group_cfo2, '') AS g2,
           coalesce(c.direction, '') AS dir, coalesce(p.code_cfo, '') AS cfo,
           sum(p.plan_tactic) AS pt
    FROM fot_headcount_plan p
    LEFT JOIN dim c ON c.code_cfo = p.code_cfo
    WHERE TRUE{p_country}{hier_gate}
    GROUP BY 1, 2, 3, 4
),
lvl AS (
    -- Слияние трёх зёрен по коду ЦФО (UNION ALL + группировка вместо FULL
    -- JOIN): план численности не размножается юрлицами факта.
    SELECT g1, g2, dir, cfo,
           max(cfo_label) AS cfo_label,
           CASE WHEN min(row_country) = max(row_country) THEN max(row_country) END AS row_country,
           sum(fot_fact) AS fot_fact, sum(fot_limit) AS fot_limit, sum(rev_fact) AS rev_fact,
           sum(q) AS hc, sum(pt) AS plan_t
    FROM (
        SELECT g1, g2, dir, cfo, cfo_label, row_country, fot_fact, fot_limit, rev_fact,
               NULL::numeric AS q, NULL::numeric AS pt
        FROM grain
        UNION ALL
        SELECT g1, g2, dir, cfo, NULL, NULL, NULL, NULL, NULL, q, NULL FROM hc_grain
        UNION ALL
        SELECT g1, g2, dir, cfo, NULL, NULL, NULL, NULL, NULL, NULL, pt FROM plan_grain
    ) u
    GROUP BY 1, 2, 3, 4
),
matrix AS (
    SELECT
        {dim_expr} AS drill_value,
        {label_expr} AS label,
        CASE WHEN min(l.row_country) = max(l.row_country) THEN min(l.row_country) END AS row_country,
        sum(l.hc) AS hc, sum(l.plan_t) AS plan_t,
        sum(l.fot_fact) AS fot_fact, sum(l.fot_limit) AS fot_limit,
        sum(l.rev_fact) AS rev_fact
    FROM lvl l
    WHERE TRUE{path_conds}
    GROUP BY 1, 2
),
matrix_full AS (
    SELECT
        x.drill_value, x.label,
        x.hc AS headcount,
        x.plan_t AS headcount_plan,
        x.hc - x.plan_t AS headcount_dev,
        x.fot_fact, x.fot_limit,
        100.0 * x.fot_fact / NULLIF(CASE {p_sel} WHEN 'KZ' THEN d.kz_f WHEN 'UZ' THEN d.uz_f
                                       ELSE d.grp_f END, 0) AS fot_share_pct,
        100.0 * x.fot_limit / NULLIF(CASE {p_sel} WHEN 'KZ' THEN d.kz_p WHEN 'UZ' THEN d.uz_p
                                      ELSE d.grp_p END, 0) AS limit_share_pct,
        100.0 * x.fot_fact / NULLIF(CASE x.row_country WHEN 'KZ' THEN d.kz_f WHEN 'UZ' THEN d.uz_f
                                     ELSE d.grp_f END, 0) AS country_share_pct,
        x.fot_fact / NULLIF(x.hc, 0) AS fot_per_head,
        x.rev_fact / NULLIF(x.hc, 0) AS revenue_per_head
    FROM matrix x
    CROSS JOIN den d
    ORDER BY x.fot_fact DESC NULLS LAST
    LIMIT {MATRIX_ROW_LIMIT + 1}
)
SELECT jsonb_build_object(
    'cards', jsonb_build_object(
        'fot_fact', c.fot_fact,
        'fot_limit', c.fot_limit,
        'fot_share_pct', 100.0 * c.fot_fact / NULLIF(
            CASE {p_sel} WHEN 'KZ' THEN d.kz_f WHEN 'UZ' THEN d.uz_f ELSE d.grp_f END, 0),
        'headcount', h.q,
        'fot_per_head', c.fot_fact / NULLIF(h.q, 0)
    ),
    'months', COALESCE((
        SELECT jsonb_agg(jsonb_build_object('ym', y.ym, 'fot_fact', y.fot_fact,
                                            'headcount', h2.q) ORDER BY y.ym)
        FROM months y LEFT JOIN hcm h2 ON h2.ym = y.ym
    ), '[]'::jsonb),
    'matrix', COALESCE((SELECT jsonb_agg(to_jsonb(x)) FROM matrix_full x), '[]'::jsonb),
    'matrix_truncated', (SELECT count(*) FROM matrix_full) > {MATRIX_ROW_LIMIT}
)::text AS payload
FROM cards c
CROSS JOIN den d
CROSS JOIN hc h
"""
    async with acquire() as pg:
        payload = json.loads(await pg.fetchval(sql, *sql_params))

    # ── Постобработка: форма ответа для фронта ───────────────────────────────
    scenario_label = _SCENARIO_LABELS.get(p["scenario_plan"], p["scenario_plan"])
    cards_raw = payload["cards"] or {}
    cards = [
        {"key": "fot_fact", "label": "ФОТ факт за месяц",
         "value": _r2(cards_raw.get("fot_fact")), "unit": currency},
        {"key": "fot_limit", "label": "ФОТ лимит (%s)" % scenario_label,
         "value": _r2(cards_raw.get("fot_limit")), "unit": currency},
        {"key": "fot_share", "label": "ФОТ, % от выручки",
         "value": _r2(cards_raw.get("fot_share_pct")), "unit": "%"},
        {"key": "headcount", "label": "Численность",
         "value": _r2(cards_raw.get("headcount")), "unit": "ставок"},
        {"key": "fot_per_head", "label": "ФОТ на сотрудника",
         "value": _r2(cards_raw.get("fot_per_head")), "unit": currency},
    ]

    # График: полная сетка месяцев января..последнего — отсутствующие месяцы
    # (нет факта) отдаются None, а не выпадают из оси.
    by_ym = {m["ym"]: m for m in payload["months"]}
    labels: list[str] = []
    fot_series: list = []
    hc_series: list = []
    for mnum in range(1, n_months + 1):
        ym = "%04d-%02d" % (year, mnum)
        row_m = by_ym.get(ym) or {}
        labels.append(ym)
        fot_series.append(_r2(row_m.get("fot_fact")))
        hc_series.append(_r2(row_m.get("headcount")))
    charts = [{
        "key": "monthly",
        "label": "ФОТ и численность по месяцам",
        "type": "bar_line",
        "labels": labels,
        "series": [
            {"key": "fot_fact", "label": "ФОТ факт", "type": "bar",
             "unit": currency, "values": fot_series},
            {"key": "headcount", "label": "Численность", "type": "line", "axis": "y2",
             "unit": "ставок", "values": hc_series},
        ],
    }]

    columns = [
        {"key": "label", "label": level_label},
        {"key": "headcount", "label": "Численность", "unit": "ставок"},
        {"key": "headcount_plan", "label": "Числ. план (тактика)", "unit": "ставок"},
        {"key": "headcount_dev", "label": "Откл. от плана", "unit": "ставок"},
        {"key": "fot_fact", "label": "ФОТ факт", "unit": currency},
        {"key": "fot_limit", "label": "ФОТ лимит", "unit": currency},
        {"key": "fot_share_pct", "label": "ФОТ, % от выручки", "unit": "%"},
        {"key": "limit_share_pct", "label": "Лимит, % от выручки", "unit": "%"},
        {"key": "country_share_pct", "label": "% от выручки своей страны", "unit": "%"},
        {"key": "fot_per_head", "label": "ФОТ на сотрудника", "unit": currency},
        {"key": "revenue_per_head", "label": "Выручка на сотрудника", "unit": currency},
    ]
    rows = [
        {
            "label": r.get("label"),
            "drill_value": r.get("drill_value"),
            "headcount": _r2(r.get("headcount")),
            "headcount_plan": _r2(r.get("headcount_plan")),
            "headcount_dev": _r2(r.get("headcount_dev")),
            "fot_fact": _r2(r.get("fot_fact")),
            "fot_limit": _r2(r.get("fot_limit")),
            "fot_share_pct": _r2(r.get("fot_share_pct")),
            "limit_share_pct": _r2(r.get("limit_share_pct")),
            "country_share_pct": _r2(r.get("country_share_pct")),
            "fot_per_head": _r2(r.get("fot_per_head")),
            "revenue_per_head": _r2(r.get("revenue_per_head")),
        }
        for r in payload["matrix"][:MATRIX_ROW_LIMIT]
    ]
    tables = [{
        "key": "by_department",
        "label": "По подразделениям",
        "columns": columns,
        "rows": rows,
        "meta": {
            "dim": level_key,
            "level_label": level_label,
            "path": p["matrix_path"],
            "can_drill": can_drill,
            "truncated": bool(payload["matrix_truncated"]),
            "row_limit": MATRIX_ROW_LIMIT,
        },
    }]

    meta_out = {
        "tab": "p9",
        "params": p,
        "period": {"year": year, "month": month, "month_date": md.isoformat()},
        "currency": currency,
        "scenario_plan": p["scenario_plan"],
        "scenario_plan_label": scenario_label,
        "closed_period": closed.isoformat() if closed else None,
        "stale": meta.get("stale"),
        "refreshed_at": meta.get("refreshed_at"),
        "matrix": {
            "dim": level_key,
            "label": level_label,
            "path": [
                {"level": MATRIX_LEVELS[i][0], "label": MATRIX_LEVELS[i][1], "value": v}
                for i, v in enumerate(path)
            ],
            "can_drill": can_drill,
            "levels": [{"key": k, "label": lbl} for k, lbl in MATRIX_LEVELS],
        },
        "methodology": METHODOLOGY_P9,
    }
    if warning:
        meta_out["warning"] = warning

    return {"cards": cards, "charts": charts, "tables": tables, "meta": meta_out}


async def dashboard(tab: str, params: dict) -> dict:
    """Собирает одну вкладку P9–P13; чужой tab — ValueError (в роутах → 400).

    P9 — полный сборщик _dashboard_p9; P10–P13 — заглушки шагов 2b/2c с той
    же формой ответа (пустые cards/charts/tables), чтобы фронт уже мог
    рендерить каркас вкладок.
    """
    if tab not in _FOT_TABS:
        raise ValueError("fot: неизвестная вкладка %r" % (tab,))
    if tab != "p9":
        return {
            "cards": [],
            "charts": [],
            "tables": [],
            "meta": {"tab": tab, "params": dict(params), "status": "not_implemented"},
        }
    meta = await fot_meta()
    p = _parse_p9_params(params, meta)
    return await _dashboard_p9(p, meta)


__all__ = [
    "COUNTRY_OPTIONS",
    "FOT_CURRENCIES",
    "FOT_DATABASE",
    "FOT_PLAN_SCENARIOS",
    "FOT_QUERY_TIMEOUT",
    "FOT_REFRESH_MINUTES",
    "IC_OPTIONS",
    "MATRIX_LEVELS",
    "MATRIX_ROW_LIMIT",
    "METHODOLOGY_P9",
    "ROW_TEMPLATE_BY_CODE",
    "ROW_TEMPLATE_BY_GROUP",
    "SCENARIO_OPTIONS",
    "YEAR_PLAN",
    "YEAR_PREV",
    "dashboard",
    "filter_options",
    "fot_meta",
    "fot_mock_enabled",
    "fot_worker",
    "refresh_fot",
]
