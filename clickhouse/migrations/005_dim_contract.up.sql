-- 005 — dim_contract: поток 2 (договоры). doc_id → название договора.
-- GLMF не несёт договор (субконто не материализуется), поэтому отдельный поток
-- из субконто Premaster1C(+History) ⋈ Objects. Связь с fact_glmf по doc_id
-- (идентичны, проверено). Грануляр. — 1 договор на DocID (проверено).
-- См. SPEC §4, docs/reports/debt/{prod-verification,probe-contracts}.md.
--
-- Маленькая таблица (одна строка на документ), грузится отдельным потоком —
-- дыры в Premaster (договор) не ломают числа fact_glmf (LEFT JOIN → 'без договора').
-- ReplacingMergeTree по inserted_at: повторный bootstrap обновляет имя по doc_id.

CREATE DATABASE IF NOT EXISTS finance;

CREATE TABLE IF NOT EXISTS finance.dim_contract
(
    doc_id        String,                    -- = fact_glmf.doc_id (1С-ссылка `{"#",...}`)
    contract_ref  String,                    -- сырая 1С-ссылка субконто-договора
    contract_name String,                    -- Objects.Name (отфильтровано эвристикой)
    account_kind  LowCardinality(String),    -- '62' | '60' | '76' (по какому счёту резолвлен)
    inserted_at   DateTime DEFAULT now()
)
ENGINE = ReplacingMergeTree(inserted_at)
ORDER BY doc_id
SETTINGS index_granularity = 8192;
