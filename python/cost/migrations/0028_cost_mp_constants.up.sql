-- 0028 — история констант для расчёта «Цена для МП, рос. руб.»
-- (наценка МП, % расходов МП, скидка СПП). Append-only лог: новое значение
-- добавляется отдельной строкой, не перезаписывает предыдущую — применяется
-- всегда самая свежая по (effective_date, created_at).

CREATE TABLE IF NOT EXISTS cost_mp_constants (
    id               SERIAL PRIMARY KEY,
    effective_date   DATE NOT NULL DEFAULT CURRENT_DATE,
    markup_mp        NUMERIC(8,4) NOT NULL,
    expense_pct_mp   NUMERIC(8,4) NOT NULL,
    spp_discount     NUMERIC(8,4) NOT NULL,
    created_by       TEXT NOT NULL,
    created_at       TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX IF NOT EXISTS idx_mp_constants_date
    ON cost_mp_constants (effective_date DESC, created_at DESC);
