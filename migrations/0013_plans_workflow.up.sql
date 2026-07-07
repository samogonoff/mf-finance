-- 0013 — корректировки и комментарии «Тактических планов» (SPEC §7.2, ADJ/COM).
-- pl_adjustment: аудит ручной корректировки (обязательное основание, ADJ-02/03).
-- pl_comment: комментарии к экземпляру PL / ячейке (COM-01).

CREATE TABLE IF NOT EXISTS pl_adjustment (
    id                  BIGSERIAL PRIMARY KEY,
    pl_id               BIGINT NOT NULL REFERENCES pl_instance(id) ON DELETE CASCADE,
    profit_center       INT  NOT NULL DEFAULT 0,    -- code_cfo (площадка)
    line_code           INT  NOT NULL,              -- code_pl
    block_type          TEXT NOT NULL DEFAULT '',
    period_year         INT  NOT NULL,
    period_month        INT  NOT NULL,
    currency            TEXT NOT NULL DEFAULT 'RUB',
    original_calculated NUMERIC(20,4),              -- значение до корректировки (факт/расчёт)
    adjusted_value      NUMERIC(20,4) NOT NULL,
    reason              TEXT NOT NULL,              -- обязательное основание (ADJ-02)
    adjusted_by         BIGINT REFERENCES users(id),
    adjusted_at         TIMESTAMPTZ NOT NULL DEFAULT NOW()
);
CREATE INDEX IF NOT EXISTS idx_pl_adjustment_pl ON pl_adjustment(pl_id);

CREATE TABLE IF NOT EXISTS pl_comment (
    id         BIGSERIAL PRIMARY KEY,
    pl_id      BIGINT NOT NULL REFERENCES pl_instance(id) ON DELETE CASCADE,
    metric_ref TEXT NOT NULL DEFAULT '',            -- ссылка на ячейку/строку (code_cfo:code_pl)
    body       TEXT NOT NULL,
    status     TEXT NOT NULL DEFAULT 'open',        -- open|resolved
    author_id  BIGINT REFERENCES users(id),
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);
CREATE INDEX IF NOT EXISTS idx_pl_comment_pl ON pl_comment(pl_id);
