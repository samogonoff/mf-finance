-- 014 — сырые таблицы Payments для отчёта ВГО (DEBT_BACKEND=arh).
--
-- Третий поток отчёта «Задолженность ВГО»: суммы, документы и продажи строятся
-- НЕ из готовых вьюх FinDebt1/3, а из сырых таблиц OLAP (БД Payments, схема dbo):
-- Debt_arh (остатки), Docs (документ/договор/срок оплаты), Wholesales_arh (продажи).
-- Правила сверены на живом OLAP с FinDebt1 до копейки (docs/reports/debt/arh-runbook.md).
--
--   fact_arh_debt  — документное зерно на дату снэпшота (ВГО-контур, Chnl или Folder = ГРУППА КОМПАНИЙ).
--                    Знак строки Debt_arh.Sum: плюс = ДЗ, минус = КЗ (sum_k отрицательна).
--                    Корзины sum30/60/90/999 — просрочка менее 30, 30–60, 60–90, свыше 90
--                    дн. по тому же знаку; Σ корзин меньше остатка, разница = долг без
--                    просрочки (отчёт считает её как остаток минус корзины).
--   fact_arh_sales — строки продаж/возвратов Wholesales_arh с корнем CrAcc IN (90, 6010, 9010).
--                    Страновую привязку 6010 (КЗ) и 9010 (УЗ) делает отчёт, ЭТЛ льёт все три.
--                    amount со знаком как в источнике: плюс = продажа, минус = возврат;
--                    amount_sign (-1/0/1) входит в ключ, чтобы продажа и возврат одного
--                    документа за день не схлопнулись в нетто.
--
-- Валюта хранится числовым ISO-кодом (643 RUB, 933 BYN, 840 USD, 978 EUR, 156 CNY);
-- BYN/USD отчёт считает сам по finance.currency_daily (миграция 010) на дату снэпшота
-- (остатки) или дату события (продажи).
--
-- Движок: ReplacingMergeTree(inserted_at). 1С перезаписывает прошлые даты, поэтому
-- ЭТЛ перезагружает дату снэпшота/события ЦЕЛИКОМ (ALTER TABLE DELETE по дате + вставка).
-- src_changed_at = MAX(DateChange) источника: по нему инкремент находит изменившиеся даты.

CREATE DATABASE IF NOT EXISTS finance;

CREATE TABLE IF NOT EXISTS finance.fact_arh_debt
(
    snapshot_date   Date,                       -- дата снэпшота (Debt_arh.Date без времени)
    company_id      LowCardinality(String),     -- УНП нашего ЮЛ (UNPOrg, trimmed)
    company         String,                     -- Organisation (имя ЮЛ из 1С)
    counterparty_id String,                     -- УНП контрагента (UNP, trimmed)
    counterparty    String,                     -- Contragent (имя)
    acc             LowCardinality(String),     -- полный субсчёт (напр. 62.01.1)
    acc_root        LowCardinality(String),     -- корень счёта (до точки)
    doc_id          String,                     -- Debt_arh.DocID → Docs.ID, пусто = остаток без документа
    doc_number      String,                     -- Docs.Number документа
    doc_date        Date,                       -- Docs.Date (дата документа), 1970-01-01 если нет
    contract_id     String,                     -- Docs.ParentID (договор-родитель), пусто если нет
    contract_number String,                     -- Number договора (родителя)
    contract_title  String,                     -- Description договора (родителя)
    doc_type        String,                     -- Docs.DocType
    delay           Int32,                      -- Docs.Delay — отсрочка, дней
    payment_date    Nullable(Date),             -- Docs.PaymentDate — срок оплаты
    currency_kod    UInt32,                     -- числовой ISO-код валюты учёта строки
    sum_d           Decimal(18, 2),             -- ДЗ (Sum > 0) в валюте учёта
    sum_k           Decimal(18, 2),             -- КЗ (Sum < 0) в валюте учёта, отрицательна
    sum30_d         Decimal(18, 2),             -- ДЗ, просрочка менее 30 дн.
    sum60_d         Decimal(18, 2),             -- ДЗ, просрочка 30–60 дн.
    sum90_d         Decimal(18, 2),             -- ДЗ, просрочка 60–90 дн.
    sum999_d        Decimal(18, 2),             -- ДЗ, просрочка свыше 90 дн.
    sum30_k         Decimal(18, 2),             -- КЗ, просрочка менее 30 дн. (отрицательна)
    sum60_k         Decimal(18, 2),             -- КЗ, просрочка 30–60 дн.
    sum90_k         Decimal(18, 2),             -- КЗ, просрочка 60–90 дн.
    sum999_k        Decimal(18, 2),             -- КЗ, просрочка свыше 90 дн.
    src_changed_at  DateTime,                   -- MAX(Debt_arh.DateChange) снэпшота
    inserted_at     DateTime DEFAULT now()      -- версия ReplacingMergeTree
)
ENGINE = ReplacingMergeTree(inserted_at)
PARTITION BY toYYYYMM(snapshot_date)
ORDER BY (company_id, snapshot_date, counterparty_id, acc, doc_id, currency_kod)
SETTINGS index_granularity = 8192;

CREATE TABLE IF NOT EXISTS finance.fact_arh_sales
(
    event_date      Date,                       -- Wholesales_arh.Date (календарный день события)
    company_id      LowCardinality(String),     -- УНП нашего ЮЛ (UNPOrg, trimmed)
    company         String,                     -- Organisation
    counterparty_id String,                     -- УНП контрагента (UNP, trimmed)
    counterparty    String,                     -- Contragent
    acc             LowCardinality(String),     -- CrAcc — счёт выручки (90.01.1, 6010, 9010)
    acc_root        LowCardinality(String),     -- корень CrAcc: 90 / 6010 / 9010
    doc_id          String,                     -- Wholesales_arh.DocID
    doc_type        String,                     -- DocumentType
    currency_kod    UInt32,                     -- числовой ISO-код валюты строки
    amount_sign     Int8,                       -- знак суммы (1 продажа, -1 возврат, 0 нулевая)
    amount          Decimal(18, 2),             -- сумма со знаком как в источнике
    src_changed_at  DateTime,                   -- MAX(Wholesales_arh.DateChange) за день
    inserted_at     DateTime DEFAULT now()      -- версия ReplacingMergeTree
)
ENGINE = ReplacingMergeTree(inserted_at)
PARTITION BY toYYYYMM(event_date)
ORDER BY (company_id, event_date, counterparty_id, acc, doc_id, currency_kod, amount_sign)
SETTINGS index_granularity = 8192;
