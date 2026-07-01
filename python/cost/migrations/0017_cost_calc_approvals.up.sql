-- 0017 — статус согласования расчётов ПЭО

CREATE TABLE IF NOT EXISTS cost_calc_approvals (
    id              BIGSERIAL PRIMARY KEY,
    model           TEXT NOT NULL,
    articul         TEXT NOT NULL,
    calc_sign       TEXT,
    plan_id         TEXT,
    status          TEXT NOT NULL DEFAULT 'pending'
                    CHECK (status IN ('pending', 'approved', 'rejected')),
    approved_by     TEXT,
    approved_at     TIMESTAMPTZ,
    comment         TEXT,
    created_at      TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at      TIMESTAMPTZ NOT NULL DEFAULT now(),
    UNIQUE (model, articul, calc_sign, plan_id)
);

CREATE INDEX IF NOT EXISTS idx_approvals_model ON cost_calc_approvals (model, articul);
CREATE INDEX IF NOT EXISTS idx_approvals_status ON cost_calc_approvals (status);
