-- 0011 — ядро процесса «Тактические планы» (docs/reports/plans/SPEC.md §7.1).
-- Экземпляр PL на период, его этапы, метрики (значения по площадке/статье/месяцу)
-- и снимок формы. VS3: запись тактики формы TPL-MP (round-trip).

CREATE TABLE IF NOT EXISTS pl_instance (
    id            BIGSERIAL PRIMARY KEY,
    period_year   INT  NOT NULL,
    period_month  INT  NOT NULL,                 -- отчётный месяц M
    status        TEXT NOT NULL DEFAULT 'draft', -- draft|in_progress|returned|waiting_dependency|approved|archived
    template_ver  INT,
    created_by    BIGINT REFERENCES users(id),
    created_at    TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    UNIQUE (period_year, period_month)
);

CREATE TABLE IF NOT EXISTS pl_stage_instance (
    id          BIGSERIAL PRIMARY KEY,
    pl_id       BIGINT NOT NULL REFERENCES pl_instance(id) ON DELETE CASCADE,
    stage_code  TEXT NOT NULL,                   -- '1.1'…'4'
    track       TEXT NOT NULL,                   -- 'sales'|'production'|'final'
    status      TEXT NOT NULL DEFAULT 'pending', -- pending|in_progress|completed|returned|blocked
    assignees   JSONB NOT NULL DEFAULT '[]',
    due_at      TIMESTAMPTZ,
    depends_on  JSONB NOT NULL DEFAULT '[]',
    UNIQUE (pl_id, stage_code)
);

CREATE TABLE IF NOT EXISTS pl_metric (
    id              BIGSERIAL PRIMARY KEY,
    pl_id           BIGINT NOT NULL REFERENCES pl_instance(id) ON DELETE CASCADE,
    template_code   TEXT NOT NULL DEFAULT 'TPL-MP',
    segment         TEXT NOT NULL DEFAULT '',     -- 'large'|'small'
    line_code       INT  NOT NULL,                -- code_pl
    block_type      TEXT NOT NULL DEFAULT '',     -- sales_manager_price|shipments|…
    profit_center   INT  NOT NULL DEFAULT 0,      -- code_cfo (площадка)
    cost_center     INT,
    country         TEXT NOT NULL DEFAULT '',
    legal_entity    TEXT,
    channel         TEXT,
    scenario        TEXT NOT NULL,                -- 'Тактика бюджет (таргеты)'
    period_year     INT  NOT NULL,
    period_month    INT  NOT NULL,                -- месяц значения (M, M+1, …)
    currency        TEXT NOT NULL,                -- BYN|RUB|USD
    amount          NUMERIC(20,4),                -- введённая тактика
    amount_fact     NUMERIC(20,4),
    amount_strategy NUMERIC(20,4),
    amount_calc     NUMERIC(20,4),
    is_manual       BOOLEAN NOT NULL DEFAULT FALSE,
    UNIQUE (pl_id, template_code, segment, line_code, block_type,
            profit_center, scenario, period_year, period_month, currency)
);
CREATE INDEX IF NOT EXISTS idx_pl_metric_pl ON pl_metric(pl_id);
CREATE INDEX IF NOT EXISTS idx_pl_metric_lookup
    ON pl_metric(template_code, segment, period_year, period_month);

CREATE TABLE IF NOT EXISTS form_submission (
    id            BIGSERIAL PRIMARY KEY,
    pl_id         BIGINT NOT NULL REFERENCES pl_instance(id) ON DELETE CASCADE,
    stage_id      BIGINT REFERENCES pl_stage_instance(id),
    template_code TEXT NOT NULL DEFAULT 'TPL-MP',
    json_payload  JSONB NOT NULL,
    submitted_by  BIGINT REFERENCES users(id),
    submitted_at  TIMESTAMPTZ NOT NULL DEFAULT NOW()
);
CREATE INDEX IF NOT EXISTS idx_form_submission_pl ON form_submission(pl_id);
