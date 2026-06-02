-- 0008 — чекпоинт ETL Premaster1C → ClickHouse fact_premaster.
-- Хранит позицию keyset-итерации (last_doc_id, last_rw_nm) для bootstrap'а
-- и last_change_at для инкремента. Идемпотентен: при рестарте Go-API ETL
-- подхватит работу с последней успешно загруженной точки, без перечитывания.
--
-- company_id используется для scoped-bootstrap (тестовая заливка одного ЮЛ).
-- Для полной заливки и инкремента — company_id = ''.

CREATE TABLE IF NOT EXISTS debt_etl_checkpoint (
    source          TEXT        NOT NULL,          -- 'premaster' (расширим под objects/counterparty позже)
    phase           TEXT        NOT NULL,          -- 'bootstrap' | 'incremental'
    company_id      TEXT        NOT NULL DEFAULT '',
    last_doc_id     TEXT,                          -- keyset cursor — последний обработанный DocID
    last_rw_nm      BIGINT,                        -- ... и его RwNm
    last_change_at  TIMESTAMPTZ,                   -- для incremental — max(DateOfChange) последней пачки
    rows_loaded     BIGINT      NOT NULL DEFAULT 0,
    started_at      TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at      TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    finished_at     TIMESTAMPTZ,
    error_text      TEXT,
    PRIMARY KEY (source, phase, company_id)
);
