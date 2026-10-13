-- 0066 — витрины-кэш ФОТ-дашборда (P9–P13), наполняет python/cost/app/fot.py.
--
-- Дашборд «ФОТ и численность» читает данные ТОЛЬКО из этих таблиц (быстрые
-- запросы из Postgres), а из MSSQL FinDWH/onec_data/Budgeting их тянет фоновый
-- refresh_fot(): тяжёлые T-SQL (порт fact_PL/fact_HR/fact_HRPlan/dim_CFO из
-- CFO_ALL_AGG) выполняются по таймеру, а не при открытии страницы.
--
-- Перезапись данных — TRUNCATE+INSERT каждой витрины внутри ОДНОЙ транзакции
-- (см. fot.refresh_fot): читатели через MVCC видят либо старый снимок целиком,
-- либо новый, частичных состояний не бывает. Поэтому PK здесь нужен только
-- справочникам (fot_headcount_plan, fot_cfo), фактам достаточно индексов.
--
-- Срез fot_monthly (зерно = все 9 колонок): kz_denominator — часть зерна
-- намеренно. Выручка KZ для знаменателя доли ФОТ считается только по кодам
-- 1006/1012 (см. _Меры.tmdl), поэтому KZ-продажи разбиты на два ряда; сумма
-- по KZ при этом не меняется.

CREATE TABLE IF NOT EXISTS fot_monthly (
    month_date      date NOT NULL,
    code_cfo        text NOT NULL,
    legal_entity    text NOT NULL,
    country_code    text NOT NULL,
    scenario_src    text NOT NULL,
    kontur          text NOT NULL,
    row_template    text NOT NULL,
    is_intercompany integer NOT NULL DEFAULT 0,
    kz_denominator  boolean NOT NULL DEFAULT false,
    amount_byn      numeric(19,4) NOT NULL DEFAULT 0,
    amount_usd      numeric(19,4) NOT NULL DEFAULT 0,
    amount_rub      numeric(19,4) NOT NULL DEFAULT 0
);
CREATE INDEX IF NOT EXISTS fot_monthly_month_idx
    ON fot_monthly (month_date);
CREATE INDEX IF NOT EXISTS fot_monthly_cfo_idx
    ON fot_monthly (code_cfo);
CREATE INDEX IF NOT EXISTS fot_monthly_scenario_idx
    ON fot_monthly (scenario_src, kontur, row_template);

CREATE TABLE IF NOT EXISTS fot_headcount (
    month_date   date,
    code_cfo     text,
    legal_entity text,
    country_code text,
    quantity     numeric(9,2)
);
CREATE INDEX IF NOT EXISTS fot_headcount_month_idx
    ON fot_headcount (month_date);
CREATE INDEX IF NOT EXISTS fot_headcount_cfo_idx
    ON fot_headcount (code_cfo);

CREATE TABLE IF NOT EXISTS fot_headcount_plan (
    code_cfo      text PRIMARY KEY,
    plan_approved numeric(9,2),
    plan_tactic   numeric(9,2)
);

CREATE TABLE IF NOT EXISTS fot_cfo (
    code_cfo       text PRIMARY KEY,
    cfo_name       text,
    cfo_label      text,
    group_cfo1     text,
    group_cfo2     text,
    direction      text,
    direction_kind text,
    country_code   text,
    sales_channel  text,
    date_opened    date,
    date_closed    date,
    is_active      boolean NOT NULL DEFAULT true
);

CREATE TABLE IF NOT EXISTS fot_meta (
    id               integer PRIMARY KEY DEFAULT 1 CHECK (id = 1),
    closed_period    date,
    fallback_closed  date,
    refreshed_at     timestamptz,
    refreshing_since timestamptz,
    source_counts    jsonb NOT NULL DEFAULT '{}'::jsonb
);
INSERT INTO fot_meta (id) VALUES (1) ON CONFLICT (id) DO NOTHING;

COMMENT ON TABLE fot_monthly IS
    'ФОТ-дашборд: P&L по месяцам (порт fact_PL CFO_ALL_AGG). Зерно: месяц/ЦФО/ЮЛ/страна/сценарий/контур/шаблон строки/ВГО/флаг знаменателя KZ.';
COMMENT ON TABLE fot_headcount IS
    'ФОТ-дашборд: численность (ставки) по месяцам (порт fact_HR). MonthDate — первые числа месяцев.';
COMMENT ON TABLE fot_headcount_plan IS
    'ФОТ-дашборд: плановая численность по ЦФО (порт fact_HRPlan из Budgeting.dbo.PlanChisl).';
COMMENT ON TABLE fot_cfo IS
    'ФОТ-дашборд: справочник ЦФО (порт dim_CFO).';
COMMENT ON TABLE fot_meta IS
    'ФОТ-дашборд: состояние витрин — закрытый период, TTL, счётчики источников.';
