-- 010 — справочники для пересчёта валют отчёта ВГО НА ДАТУ ДОКУМЕНТА (Doc_Date).
--
-- Второй поток отчёта «Задолженность ВГО» (DEBT_BACKEND=findebt-docdate,
-- см. docs/reports/debt/vgo-doc-date-methodology.md). Действующий findebt берёт
-- BYN/USD готовыми «линзами» из FinDebt3, пересчитанными по курсу НА ДАТУ СНЭПШОТА
-- → сумма в валюте «плавает» от снэпшота к снэпшоту. Новый поток считает BYN/USD
-- сам: сумма_в_валюте_договора × курс(валюта→BYN на Doc_Date), USD через BYN.
--
-- Эти две таблицы — источники курса. Наполняются cmd/findebt-etl (MODE=currency)
-- из linked-server справочников OLAP:
--   dim_valuta      ← [SRV-SQL].Gpartner.dbo.valuta       (NAIM→KOD, RTRIM)
--   currency_daily  ← [SRV-SQL].Checks.dbo.CurrencyDaily  (KOD,date,curr_rate к BYN)
--
-- currency_daily.ORDER BY (kod, date) обязателен: отчёт подбирает курс через
-- ASOF LEFT JOIN (последний известный курс на дату документа или раньше) — снимает
-- проблему «нет курса ровно на выходной» без молчаливого ISNULL(...,1).

CREATE DATABASE IF NOT EXISTS finance;

CREATE TABLE IF NOT EXISTS finance.dim_valuta
(
    kod         Int32,                  -- числовой код валюты (643 RUR, 840 USD, 933 BYN)
    naim        String,                 -- текстовое наименование (уже RTRIM на этапе ETL): 'RUR.','USD','BYN'
    inserted_at DateTime DEFAULT now()  -- версия ReplacingMergeTree
)
ENGINE = ReplacingMergeTree(inserted_at)
ORDER BY (naim)
SETTINGS index_granularity = 8192;

CREATE TABLE IF NOT EXISTS finance.currency_daily
(
    kod         Int32,                  -- код валюты (как в dim_valuta.kod)
    date        Date,                   -- дата курса (чистый DATE; CONVERT(DATE,...) на этапе ETL)
    curr_rate   Decimal(18, 6),         -- курс валюты к BYN на эту дату
    inserted_at DateTime DEFAULT now()  -- версия ReplacingMergeTree
)
ENGINE = ReplacingMergeTree(inserted_at)
ORDER BY (kod, date)
SETTINGS index_granularity = 8192;
