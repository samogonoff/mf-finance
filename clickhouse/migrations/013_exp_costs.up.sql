-- 013 — затраты для формы «Расходы ЦП» (этап 1.5): ночной снимок результата процедуры
-- Budgeting.dbo.ZATRATY_DLYA_OKSANY_BYSTRYE.
--
-- Зачем: процедура без параметров отдаёт весь бюджет (~1 млн строк) и работает 4–5
-- минут, поэтому формы не могут ходить в неё при открытии. Раз в сутки (5:00 по часам
-- MSSQL) go-api / cmd/findebt-etl MODE=expcosts забирает её результат сюда, а форма
-- читает из CH за секунды. Код: go/internal/etl/exp_costs.go, читатель:
-- go/internal/plans/exp_ch_source.go. Данные «как в источнике», история с 2024 года.
--
-- Обновление — атомарно: снимок собирается в finance.exp_costs__new и подменяется
-- EXCHANGE TABLES (как у findwh_table_fin_pl): форма не видит пустую или полузаливную
-- таблицу, при ошибке остаётся вчерашний снимок.
-- Строки NOT NULL: NULL источника приходит пустой строкой/нулём (дефолт типа).

CREATE DATABASE IF NOT EXISTS finance;

CREATE TABLE IF NOT EXISTS finance.exp_costs
(
    scenario       LowCardinality(String),
    country        LowCardinality(String),
    month          Date,
    code_pl        Int32,
    group_pl       LowCardinality(String),
    group_pl_new   LowCardinality(String),
    expense        String,
    cf_item        LowCardinality(String),
    special_access LowCardinality(String),
    code_cfo       Int32,
    cfo            String,
    group_cfo1     LowCardinality(String),
    group_cfo2     String,
    group_cfo3     String,
    project_name   LowCardinality(String),
    amount         Decimal(38, 6),   -- в национальной валюте страны
    amount_byn     Decimal(38, 6),
    amount_usd     Decimal(38, 6)
)
ENGINE = MergeTree
PARTITION BY toYear(month)
ORDER BY (group_cfo1, country, scenario, month, code_cfo, code_pl);

-- Журнал заливок: свежесть снимка («данные на …» на форме) и защита от повторов.
CREATE TABLE IF NOT EXISTS finance.exp_costs_etl_runs
(
    source_date   Date,                     -- дата по часам MSSQL
    started_at    DateTime,
    finished_at   DateTime,
    status        LowCardinality(String),   -- ok | error
    rows          UInt64,                   -- залито
    skipped       UInt64,                   -- отброшено (старше 2024 года)
    details       String,
    triggered_by  LowCardinality(String)    -- cron | cli | api
)
ENGINE = MergeTree
ORDER BY (started_at);