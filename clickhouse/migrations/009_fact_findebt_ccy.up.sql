-- 009 — fact_findebt_ccy: мультивалютные линзы CUR_FILTER.
-- Заменяет fact_findebt (008), где сумма была жёстко схлопнута в BYN
-- (CUR_FILTER='В бел. рублях' пиннился на стороне extract). Теперь храним ВСЕ
-- три «линзы» одной суммы и построчную валюту, а отчёт выбирает линзу.
--
-- CUR_FILTER (FinDebt3) — это НЕ валюта, а представление одной и той же суммы:
--   { В валюте договора, В бел. рублях, В долларах США }.
-- Native-валюта строки лежит ОТДЕЛЬНО в FinDebt3.Currency (BYN/RUR./USD).
-- Ловушка: без пина одна задолженность приходит в 3 экземплярах (по линзе) →
-- любой SUM обязан фильтроваться ровно одной линзой (cur_filter), иначе ×3.
--
-- Зачем НОВАЯ таблица, а не ALTER: cur_filter входит в КЛЮЧ дедупа
-- ReplacingMergeTree (ORDER BY) — иначе три линзы схлопнулись бы в одну строку
-- на старом ключе и суммы перетёрлись бы. ORDER BY у движка не меняется ALTER'ом,
-- поэтому создаём таблицу заново. Данные восстановимы bootstrap'ом.
--
-- Движок/партиционирование/семантика знака — как в 008: ReplacingMergeTree по
-- inserted_at, PARTITION BY месяц снэпшота, sum_d (ДЗ) положительна, sum_k (КЗ)
-- отрицательна (репо переворачивает КЗ в положительный долг). В отличие от 008
-- суммы уже НЕ обязательно в BYN — единица измерения задаётся линзой cur_filter
-- (в «В валюте договора» это native currency строки).

CREATE DATABASE IF NOT EXISTS finance;

CREATE TABLE IF NOT EXISTS finance.fact_findebt_ccy
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
    cur_filter      LowCardinality(String),     -- линза представления суммы (FinDebt3.CUR_FILTER)
    currency        LowCardinality(String),     -- native-валюта строки (FinDebt3.Currency: BYN/RUR./USD)
    sum_d           Decimal(18, 2),             -- ДЗ (дебет) в единицах линзы cur_filter
    sum_k           Decimal(18, 2),             -- КЗ (кредит) в единицах линзы, отрицательна
    inserted_at     DateTime DEFAULT now()      -- версия ReplacingMergeTree
)
ENGINE = ReplacingMergeTree(inserted_at)
PARTITION BY toYYYYMM(snapshot_date)
ORDER BY (company_id, snapshot_date, counterparty_id, acc, doc_number, doc_date, cur_filter, currency)
SETTINGS index_granularity = 8192;

-- Снимаем одновалютную таблицу 008 — её заменил fact_findebt_ccy. IF EXISTS →
-- идемпотентно (на свежей CH её нет, на существующей дропаем однократно).
DROP TABLE IF EXISTS finance.fact_findebt;
