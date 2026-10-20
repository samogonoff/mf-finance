-- 014 — глубина задолженности и объём продаж в потоке FinDebt.
--
-- 1) Корзины просрочки FinDebt3.SUM{30,60,90,999}_{D,K} → колонки fact_findebt_ccy.
--    Семантика (Excel «Ежедневная ДЗ и КЗ на витрине»): корзины ПРОСРОЧКИ,
--    sum30 = менее 30 дн., sum60 = 30–60, sum90 = 60–90, sum999 = свыше 90.
--    Сумма корзин меньше остатка (≈9% ДЗ на 14.10.2026) — разница это долг без
--    просрочки, отчёт считает её как остаток минус корзины. Знак как у sum_d/sum_k
--    (КЗ отрицательна). Колонки добавляются ALTER'ом (ORDER BY не меняется) с
--    DEFAULT 0: уже залитые снэпшоты получат нули, нужен повторный bootstrap
--    (`MODE=bootstrap /findebt-etl`), чтобы заполнить историю.
--
-- 2) fact_findebt_turnover — оборотные строки FinDebt1 с DEBT_FILTER=
--    'Отображать платежи и отгр.' (ВГО-контур). Значения НАКОПИТЕЛЬНЫЕ с начала
--    месяца: date_from = 1-е число, date_to = дата снэпшота (до июня 2026 снэпшоты
--    помесячные → date_to = конец месяца). Объём продаж = (k_wholesales + d_wholesales)
--    по счетам выручки (ТЗ): РБ/РФ — 90.x, КЗ — 6010, УЗ — 9010 (кредитовый
--    оборот); k_wholesales по 60.x — поступление товара, d_wholesales на 41/62 —
--    корректировки. Зерно: снэпшот ×
--    ЮЛ × контрагент × счёт × линза × валюта (каналы/сотрудники свёрнуты на extract).
--    Знак как во вьюхе: D_* отрицательны, K_* положительны.

CREATE DATABASE IF NOT EXISTS finance;

ALTER TABLE finance.fact_findebt_ccy ADD COLUMN IF NOT EXISTS sum30_d  Decimal(18, 2) DEFAULT 0;
ALTER TABLE finance.fact_findebt_ccy ADD COLUMN IF NOT EXISTS sum60_d  Decimal(18, 2) DEFAULT 0;
ALTER TABLE finance.fact_findebt_ccy ADD COLUMN IF NOT EXISTS sum90_d  Decimal(18, 2) DEFAULT 0;
ALTER TABLE finance.fact_findebt_ccy ADD COLUMN IF NOT EXISTS sum999_d Decimal(18, 2) DEFAULT 0;
ALTER TABLE finance.fact_findebt_ccy ADD COLUMN IF NOT EXISTS sum30_k  Decimal(18, 2) DEFAULT 0;
ALTER TABLE finance.fact_findebt_ccy ADD COLUMN IF NOT EXISTS sum60_k  Decimal(18, 2) DEFAULT 0;
ALTER TABLE finance.fact_findebt_ccy ADD COLUMN IF NOT EXISTS sum90_k  Decimal(18, 2) DEFAULT 0;
ALTER TABLE finance.fact_findebt_ccy ADD COLUMN IF NOT EXISTS sum999_k Decimal(18, 2) DEFAULT 0;

CREATE TABLE IF NOT EXISTS finance.fact_findebt_turnover
(
    snapshot_date   Date,                       -- FinDebt1.Date
    date_from       Date,                       -- FinDebt1.DATE_F (начало месяца)
    company_id      LowCardinality(String),     -- УНП нашего ЮЛ (UNPOrg, trimmed)
    company         String,                     -- Organisation
    counterparty_id String,                     -- УНП контрагента (UNP, trimmed)
    counterparty    String,                     -- Contragent
    acc             LowCardinality(String),     -- полный счёт (90.1.1, 60.1.1 …)
    acc_root        LowCardinality(String),     -- корень счёта (до точки / 2 первые цифры)
    cur_filter      LowCardinality(String),     -- линза суммы (FinDebt1.CUR_FILTER)
    currency        LowCardinality(String),     -- native-валюта строки
    d_wholesales    Decimal(18, 2),             -- D_Wholesales (отрицательна)
    k_wholesales    Decimal(18, 2),             -- K_Wholesales
    d_payment       Decimal(18, 2),             -- D_Payment (отрицательна)
    k_payment       Decimal(18, 2),             -- K_Payment
    inserted_at     DateTime DEFAULT now()      -- версия ReplacingMergeTree
)
ENGINE = ReplacingMergeTree(inserted_at)
PARTITION BY toYYYYMM(snapshot_date)
ORDER BY (company_id, snapshot_date, counterparty_id, acc, cur_filter, currency)
SETTINGS index_granularity = 8192;
