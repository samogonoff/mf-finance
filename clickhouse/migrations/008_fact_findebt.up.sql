-- 008 — fact_findebt: поток из готового расчётного слоя [Payments].[report].
-- [FinDebt3] (документная детализация ДЗ/КЗ ВГО). Аналитик сверил вьюху с 1С
-- копейка-в-копейку (docs/reports/debt/findebt-verification.md). В отличие от
-- старых fact_glmf/fact_premaster тут не сырые проводки, а уже свёрнутый
-- суточный снэпшот НА УРОВНЕ ДОКУМЕНТА/ДОГОВОРА — свою свёртку не считаем.
--
-- Зерно строки — договор/приложение (doc_number) на дату снэпшота: FinDebt3
-- ключуется номером документа (в ВГО это № договора/приложения, напр. "1.18.",
-- "28/02"). Отчёт агрегирует эту таблицу до любого уровня иерархии
-- (компания → контрагент → счёт → договор), а drill-down раскрывает документы
-- договора. Свод по счёту = SUM по договорам — отдельная svod-таблица не нужна.
--
-- Движок: ReplacingMergeTree(inserted_at). Снэпшот на дату иммутабелен; повторный
-- extract той же даты приходит с бОльшим inserted_at и побеждает при merge.
-- Финальные SELECT'ы — с FINAL. PARTITION BY месяц снэпшота (до июня 2026
-- снэпшоты помесячные, дальше суточные).
--
-- Валюта: BYN-консолидация (CUR_FILTER='В бел. рублях' на стороне extract).
-- Знак FinDebt: sum_d_byn (ДЗ) положительна, sum_k_byn (КЗ) отрицательна;
-- в «положительный долг» КЗ переворачивает репо.
--
-- Срок/просрочка (ТЗ, лист 3): delay = «Отсрочка по договору» (FinDebt3.Delay,
-- срок оплаты из 1С); payment_date = «Дата оплаты по договору»
-- (FinDebt3.Payment_Date); day_delay = «Задолженность в днях» (FinDebt3.DAY_DELAY).

CREATE DATABASE IF NOT EXISTS finance;

CREATE TABLE IF NOT EXISTS finance.fact_findebt
(
    snapshot_date   Date,                       -- FinDebt3.Date — дата суточного снэпшота
    company_id      LowCardinality(String),     -- УНП нашего ЮЛ (UNPOrg, trimmed)
    company         String,                     -- Organisation (имя ЮЛ из 1С)
    counterparty_id String,                     -- УНП контрагента (UNP, trimmed)
    counterparty    String,                     -- Contragent (имя)
    acc             LowCardinality(String),     -- полный субсчёт (напр. 60.01)
    acc_root        LowCardinality(String),     -- корень счёта (до точки) для фильтра/группировки
    doc_number      String,                     -- № договора/приложения (FinDebt3.Doc_Number)
    doc_date        Date,                        -- дата документа (FinDebt3.Doc_Date)
    description     String,                     -- FinDebt3.Doc_Description ("1.18. от 20.01.2025")
    delay           Int32,                      -- «Отсрочка по договору», дней (FinDebt3.Delay)
    payment_date    Nullable(Date),             -- «Дата оплаты по договору» (FinDebt3.Payment_Date)
    day_delay       Int32,                      -- «Задолженность в днях» (FinDebt3.DAY_DELAY)
    sum_d_byn       Decimal(18, 2),             -- ДЗ (дебет) в BYN
    sum_k_byn       Decimal(18, 2),             -- КЗ (кредит) в BYN, отрицательна
    inserted_at     DateTime DEFAULT now()      -- версия ReplacingMergeTree
)
ENGINE = ReplacingMergeTree(inserted_at)
PARTITION BY toYYYYMM(snapshot_date)
ORDER BY (company_id, snapshot_date, counterparty_id, acc, doc_number, doc_date)
SETTINGS index_granularity = 8192;
