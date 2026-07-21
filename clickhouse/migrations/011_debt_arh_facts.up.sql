-- 011 — сырые факты метода аналитика для отчёта ВГО (DEBT_BACKEND=findebt-docdate).
--
-- По уточнённой методике (docs/reports/debt/vgo-doc-date-methodology.md §4-§7):
-- FinDebt3 НЕ источник сумм (Doc_Number не уникален — до 17 документов на один
-- (Number,Date)). Суммы и документы берём напрямую из сырых таблиц Payments, где
-- DocID уникален внутри своей таблицы, а FinDebt1 — только whitelist пар
-- (UNPOrg, Acc) «кого показывать».
--
--   debt_facts     ← Payments.dbo.Debt_arh, дедуп по DocID (снэпшот остатка).
--                    DocDate = MIN(Date) = дата возникновения долга, для курса.
--   turnover_facts ← Payments.dbo.Wholesales_arh, реальные движения по EventDate.
--
-- Пересчёт валют делает отчёт: currency_daily (миграция 010) через ASOF JOIN по
-- currency_kod (в Debt_arh валюта уже числовой KOD — маппинг NAIM→KOD не нужен).
-- Полный reload на каждую синхронизацию (ВГО-контур мал), поэтому обычный
-- MergeTree, дедуп сделан на стороне SQL Server (GROUP BY DocID).

CREATE DATABASE IF NOT EXISTS finance;

CREATE TABLE IF NOT EXISTS finance.debt_facts
(
    doc_id          String,                     -- DocID (уникален в Debt_arh)
    company_id      LowCardinality(String),     -- УНП нашего ЮЛ (UNPOrg)
    counterparty_id String,                     -- УНП контрагента (опционально, '' если нет в источнике)
    contragent      String,                     -- имя контрагента (Name, справочно)
    acc             LowCardinality(String),     -- счёт (Acc)
    acc_root        LowCardinality(String),     -- корень счёта (до точки)
    doc_description String,                      -- описание документа (Description)
    currency_kod    UInt32,                     -- код валюты договора (Currency)
    amount          Decimal(18, 2),             -- сумма в валюте договора, БЕЗ пересчёта (Sum)
    doc_date        Date,                        -- MIN(Date) — дата возникновения (для курса)
    inserted_at     DateTime DEFAULT now()
)
ENGINE = MergeTree
ORDER BY (company_id, acc, doc_date)
SETTINGS index_granularity = 8192;

CREATE TABLE IF NOT EXISTS finance.turnover_facts
(
    doc_id          String,
    company_id      LowCardinality(String),
    counterparty_id String,
    contragent      String,
    acc             LowCardinality(String),     -- DrAcc
    acc_root        LowCardinality(String),
    doc_type        String,                     -- DocumentType ('Реализация...', 'Корректировка долга'...)
    currency_kod    UInt32,
    amount          Decimal(18, 2),
    event_date      Date,                        -- дата проводки (уникальна на движение)
    inserted_at     DateTime DEFAULT now()
)
ENGINE = MergeTree
ORDER BY (company_id, acc, event_date)
SETTINGS index_granularity = 8192;
