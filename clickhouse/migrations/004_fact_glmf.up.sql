-- 004 — fact_glmf: поток 1 (числа/классификация) из [FinDWH].[dbo].[GLMF].
-- GLMF = Premaster + каноничная классификация (CodePL/GroupPL/ICO/Country/имена),
-- наполняется ежедневно процедурой GLMF_exec. Полнее Premaster1C (тот неполон по
-- части периодов), несёт ИНН напрямую и DocID для связи с dim_contract.
-- Дизайн: SPEC §4, docs/reports/debt/{prod-verification,tz-requirements}.md.
--
-- Движок: ReplacingMergeTree(date_of_load). GLMF_exec пересобирает таблицу
-- (TRUNCATE+INSERT), каждая строка несёт DateOfLoad (унаследован из Premaster) —
-- при ретро-правке строка приходит с бОльшим date_of_load и побеждает при merge.
-- Финальные SELECT'ы — с FINAL или argMax по (doc_id, num).
--
-- USD пока не материализуем (нет в базовой GLMF; есть только во view vGLMFAddUSD,
-- но view не несёт DateOfLoad-watermark). Колонки заведены String DEFAULT ''
-- (заполнить позже join'ом). В отчёте валюта = BYN-консолидация (SPEC §10.7).
--
-- PARTITION BY toYYYYMM(month) — месяц = естественная единица перезаливки GLMF.
-- ORDER BY: company_id+month первыми (фильтр отчёта всегда по ЮЛ+период), затем
-- контрагент/счёт для свёртки, doc_id+num замыкают бизнес-ключ строки.

CREATE DATABASE IF NOT EXISTS finance;

CREATE TABLE IF NOT EXISTS finance.fact_glmf
(
    company_id              LowCardinality(String),    -- ИНН/УНП нашего ЮЛ
    counterparty_id         String,                    -- ИНН/УНП партнёра ('' = NULL)
    doc_id                  String,                    -- DocID 1С `{"#",...}` (= Premaster.DocID, ключ к dim_contract)
    num                     Int64,                     -- GLMF.Num — уникальный id строки

    date                    Date,                      -- дата проводки
    month                   Date,                      -- первое число месяца (GLMF.Month)

    dr_acc                  LowCardinality(String),    -- полный субсчёт дебета (напр. 62.1.1)
    cr_acc                  LowCardinality(String),    -- полный субсчёт кредита
    dr_acc_root             LowCardinality(String),    -- корень (до точки) для свёртки
    cr_acc_root             LowCardinality(String),

    code_pl                 LowCardinality(String),    -- CodePL
    group_pl                LowCardinality(String),    -- GroupPL ('ПРОДАЖИ' = выручка)
    ico                     UInt8,                     -- 1=ВГО

    country                 LowCardinality(String),    -- РБ/РФ/КЗ/УЗ (маппинг из BY/RU/KZ/UZ)

    amt_wovat_byn           Decimal(18, 2),            -- AmountWOVATBelRubFact (выручка/обороты без НДС)
    amt_withvat_byn         Decimal(18, 2),            -- AmountWithVATBelRubFact (ДЗ/КЗ-сальдо с НДС)
    amt_wovat_usd           String DEFAULT '',         -- placeholder (USD не материализован, см. шапку)
    amt_withvat_usd         String DEFAULT '',

    doc_name_1c             String,                    -- GLMF.DocName1C (для drill-down)
    operation_description   String,                    -- GLMF.OperationDescription

    date_of_load            Date,                      -- GLMF.DateOfLoad — ключ Replacing + watermark инкремента
    inserted_at             DateTime DEFAULT now()
)
ENGINE = ReplacingMergeTree(date_of_load)
PARTITION BY toYYYYMM(month)
ORDER BY (company_id, month, counterparty_id, dr_acc, cr_acc, doc_id, num)
SETTINGS index_granularity = 8192;
