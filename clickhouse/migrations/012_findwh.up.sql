-- 012 — сырая реплика FinDWH для аналитиков (GLMF, Table_Fin_PL, ExRates):
-- таблицы finance.findwh_* в общей БД finance.
--
-- Почему не отдельная БД findwh: прод-пользователь CH имеет права только на
-- finance.* (GRANT ALL ON finance.*), CREATE DATABASE findwh падает с
-- ACCESS_DENIED (так упал первый деплой этой миграции). Префикс findwh_ отделяет
-- реплику от отчётных витрин ВГО; аналитикам права выдаются на эти таблицы.
--
-- Дизайн и замеры: docs/bi/findwh-etl.md. Наполняет go-api (воркер
-- FINDWH_SYNC_INTERVAL) или cmd/findebt-etl MODE=findwh, код internal/etl/findwh*.go.
-- Реплика «как в источнике», без бизнес-логики: отчётные витрины остаются в finance.
--
-- Имена колонок — snake_case от исходных (опечатка источника ResposiblePerson*
-- сохранена, чтобы колонку находили по имени из FinDWH). Строки NOT NULL: NULL
-- источника приходит пустой строкой. Числа и даты, где NULL осмыслен, — Nullable.

CREATE DATABASE IF NOT EXISTS finance;

-- GLMF (= Premaster1C + классификация), с FINDWH_GLMF_FROM (дефолт 2025-01-01).
-- Ключа строки в источнике нет: GLMF.Num — identity, новый при каждой ночной
-- пересборке, поэтому в реплику не переносится. Обновление — заменой секций:
-- секция (company, месяц) собирается в finance.findwh_glmf_stage и ставится через
-- REPLACE PARTITION, см. glmf_day_fp.
CREATE TABLE IF NOT EXISTS finance.findwh_glmf
(
    base_doc_id                         String,
    doc_id                              String,                   -- 1С-ссылка {"#",...}
    doc_name_1c                         LowCardinality(String),
    company_id                          LowCardinality(String),   -- ИНН/УНП нашего ЮЛ
    code_cfo                            LowCardinality(String),
    code_pl                             LowCardinality(String),
    date                                DateTime,                 -- GLMF.Date (дата проводки)
    trans_description                   String,
    operation_description               String,
    counterparty_id                     String,
    counterparty_name                   String,
    performer_1c                        LowCardinality(String),
    amount_wovat_currency               Nullable(Decimal(18, 2)),
    amount_with_vat_currency            Nullable(Decimal(18, 2)),
    dr_acc                              LowCardinality(String),
    cr_acc                              LowCardinality(String),
    resposible_person_dept              LowCardinality(String),
    amount_wovat_currency_fact          Nullable(Decimal(18, 2)),
    amount_with_vat_currency_fact       Nullable(Decimal(18, 2)),
    ex_rate                             Nullable(Float64),
    amount_wovat_byn_fact               Nullable(Decimal(18, 2)),
    amount_with_vat_byn_fact            Nullable(Decimal(18, 2)),
    country                             LowCardinality(String),   -- BY/RU/KZ/UZ, как в источнике
    group_cfo1                          LowCardinality(String),
    group_cfo2                          LowCardinality(String),
    department                          LowCardinality(String),
    group_pl                            LowCardinality(String),
    expense                             LowCardinality(String),
    month                               Date,                     -- первое число месяца проводки
    resposible_person_fin_contr         LowCardinality(String),
    company                             LowCardinality(String),   -- код ЮЛ: MF, TDMF, MFT, MFUz, MFKaz, ...
    strat_budget_amount_wovat_currency  Nullable(Decimal(18, 2)),
    strat_budget_amount_wovat_byn       Nullable(Decimal(18, 2)),
    budget_amount_wovat_currency        Nullable(Decimal(18, 2)),
    budget_amount_wovat_byn             Nullable(Decimal(18, 2)),
    db_analytics1                       LowCardinality(String),
    pl_descr_bk                         LowCardinality(String),
    ico                                 UInt8,                    -- 1 = ВГО
    date_of_load                        Nullable(Date),
    dr_subconto1_id                     Nullable(Int32),          -- ключи FinDWH.dbo.D_Subconto
    dr_subconto2_id                     Nullable(Int32),
    dr_subconto3_id                     Nullable(Int32),
    dr_subconto4_id                     Nullable(Int32),
    cr_subconto1_id                     Nullable(Int32),
    cr_subconto2_id                     Nullable(Int32),
    cr_subconto3_id                     Nullable(Int32),
    cr_subconto4_id                     Nullable(Int32),
    loaded_at                           DateTime DEFAULT now()    -- когда строка пришла в реплику
)
ENGINE = MergeTree
PARTITION BY (company, toYYYYMM(date))
ORDER BY (company, date, dr_acc, cr_acc)
SETTINGS index_granularity = 8192;

-- Отпечатки дней GLMF, которые сейчас лежат в finance.findwh_glmf. ETL считает такие же
-- на стороне MSSQL и перегружает только разошедшиеся дни. is_deleted=1 — день
-- пропал из источника (и из реплики). Читать с FINAL.
CREATE TABLE IF NOT EXISTS finance.findwh_glmf_day_fp
(
    company     String,
    date        Date,
    n           UInt64,                 -- COUNT_BIG(*)
    sum_byn     Decimal(38, 2),         -- SUM(AmountWithVATBelRubFact)
    fp_xor      Int32,                  -- CHECKSUM_AGG(BINARY_CHECKSUM(...))
    fp_sum      Int64,                  -- SUM(CAST(BINARY_CHECKSUM(...) AS BIGINT))
    checked_at  DateTime64(3),
    is_deleted  UInt8
)
ENGINE = ReplacingMergeTree(checked_at, is_deleted)
ORDER BY (company, date);

-- Table_Fin_PL (ОПУ: факт, бюджет, тактика, ручные блоки). Полный снимок раз в
-- день, подмена через EXCHANGE TABLES. Кириллические колонки источника:
-- Компания→company, Корр-ка→counterparty, ВГО→vgo, Блок→block_name,
-- Метод.нюансы→method_nuances, КлючСвязи→link_key,
-- Для отчета_скрипт→for_report_script, Для отчета→for_report.
CREATE TABLE IF NOT EXISTS finance.findwh_table_fin_pl
(
    block_num                Nullable(Int32),
    dr_cr                    LowCardinality(String),
    dr_acc                   LowCardinality(String),
    cr_acc                   LowCardinality(String),
    doc_name_1c              LowCardinality(String),
    code_pl                  LowCardinality(String),
    country                  LowCardinality(String),
    month                    Date,
    company                  LowCardinality(String),
    counterparty             String,
    vgo                      Nullable(Int32),
    operation_description    String,
    counterparty_name        String,
    group_pl                 LowCardinality(String),
    group_pl_new             LowCardinality(String),
    amount_byn               Nullable(Decimal(28, 2)),
    amount                   Nullable(Decimal(28, 2)),
    amount_usd               Nullable(Decimal(28, 2)),
    amount_rub               Nullable(Decimal(28, 2)),
    scenario                 LowCardinality(String),   -- факт / бюджет / тактика / НовСтрат
    code_cfo                 LowCardinality(String),
    cfo                      LowCardinality(String),
    group_cfo1               LowCardinality(String),
    group_cfo2               LowCardinality(String),
    group_cfo3               LowCardinality(String),
    block_name               LowCardinality(String),
    method_nuances           Nullable(Int32),
    link_key                 String,
    for_report_script        LowCardinality(String),
    for_report               LowCardinality(String),
    expense                  LowCardinality(String),
    cf_item                  LowCardinality(String),
    lfl_lisa                 LowCardinality(String),
    discount_amount_vat_byn  Nullable(Decimal(28, 6)),
    markdown_amount_vat_byn  Nullable(Decimal(28, 6)),
    loaded_at                DateTime DEFAULT now()
)
ENGINE = MergeTree
ORDER BY (month, scenario, country, code_pl)
SETTINGS index_granularity = 8192;

-- ExRates* (курс USD из Gpartner.valuta1): все страновые таблицы в одной.
-- country: '' = dbo.ExRates, иначе суффикс таблицы (BY, RU, KZ, UZ, TR).
CREATE TABLE IF NOT EXISTS finance.findwh_ex_rates
(
    country     LowCardinality(String),
    data        Date,
    kurs_bank   Decimal(15, 8),
    valuta_id   Int32,
    loaded_at   DateTime DEFAULT now()
)
ENGINE = MergeTree
ORDER BY (country, valuta_id, data);

-- Журнал прогонов ETL: по нему воркер понимает, что сегодняшняя загрузка уже
-- прошла (source_date = дата на сервере MSSQL), а аналитик — насколько свежи данные.
CREATE TABLE IF NOT EXISTS finance.findwh_etl_runs
(
    table_name    LowCardinality(String),
    source_date   Date,
    started_at    DateTime,
    finished_at   DateTime,
    status        LowCardinality(String),   -- ok | error
    rows          UInt64,
    details       String,
    triggered_by  LowCardinality(String)    -- cron | cli
)
ENGINE = MergeTree
ORDER BY (table_name, started_at);
