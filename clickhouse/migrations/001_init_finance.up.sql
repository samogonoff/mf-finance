-- 001 — БД finance + основная факт-таблица fact_premaster.
-- Источник: [FinDWH].[dbo].[Premaster1C] (203M строк, MSSQL).
-- Дизайн: docs/reports/debt/clickhouse-design.md §6.
--
-- Стратегия движка: ReplacingMergeTree по date_of_change. Premaster иногда
-- ретроактивно обновляет проводки (исправления, сторно) — последняя версия
-- по (doc_id, rw_nm) победит при merge. Финальные SELECT'ы делаются с FINAL
-- либо через argMax по (doc_id, rw_nm); MV-свёртка ниже работает напрямую.
--
-- ORDER BY ставит первым company_id и date — это покрытие 99% запросов отчёта
-- задолженности (фильтр всегда по нашему ЮЛ + период). counterparty_id и acc_root
-- идут следом, чтобы свёртка по контрагенту/счёту читала меньше granules.
--
-- index_granularity = 8192 — дефолт CH, для нашего объёма хватает.

CREATE DATABASE IF NOT EXISTS finance;

CREATE TABLE IF NOT EXISTS finance.fact_premaster
(
    -- бизнес-ключ строки из источника
    company_id              LowCardinality(String),    -- ИНН/УНП нашего ЮЛ (~15 значений)
    counterparty_id         String,                    -- ИНН/УНП партнёра ('' = NULL)
    doc_id                  String,                    -- DocID 1С (binary hex после CONVERT)
    rw_nm                   Int64,                     -- порядок строки в документе

    -- временные метки
    date                    Date,                      -- дата проводки

    -- счета и сумма
    dr_acc                  LowCardinality(String),    -- полный счёт дебета
    cr_acc                  LowCardinality(String),    -- полный счёт кредита
    dr_acc_root             LowCardinality(String),    -- LEFT до точки — для GROUP BY свёртки
    cr_acc_root             LowCardinality(String),
    amount                  Decimal(18, 2),            -- AmountWithVATCurrency источника
    ico                     UInt8,                     -- 1=ВГО, 0=внешний

    -- денормализация (заполняется ETL'ом из chart_of_accounts/Entities)
    country                 LowCardinality(String),    -- РФ / РБ / KZ / ...

    -- описания для drill-down (без LEFT JOIN при детализации документа)
    doc_name_1c             String,                    -- Objects.Name по DocID
    mapping                 String,                    -- Premaster.Mapping
    trans_description       String,                    -- Premaster.TransDescription
    operation_description   String,                    -- Premaster.OperationDescription

    -- технические
    date_of_change          DateTime,                  -- из Premaster.DateOfChange — ключ Replacing
    inserted_at             DateTime DEFAULT now()     -- когда ETL положил строку в CH
)
ENGINE = ReplacingMergeTree(date_of_change)
PARTITION BY toYYYYMM(date)
ORDER BY (company_id, date, counterparty_id, dr_acc_root, cr_acc_root, doc_id, rw_nm)
SETTINGS index_granularity = 8192;
