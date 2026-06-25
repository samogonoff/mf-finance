-- 0014 — движок CALC (D11, SPEC §7.3, §11): формулы каскада как ДАННЫЕ.
-- calc_rule — дефолтные версионируемые формулы (правит «Администратор процессов»).
-- pl_formula_override — per-срез переопределение формулы финансистом (с причиной).
-- Приоритет при расчёте: override → calc_rule. Точные выражения — провизорные (Q4b).

CREATE TABLE IF NOT EXISTS calc_rule (
    id            BIGSERIAL PRIMARY KEY,
    code          TEXT NOT NULL,                 -- 'sales_net','gross_margin','markup_pct',…
    template_code TEXT NOT NULL DEFAULT 'TPL-MP',
    block_type    TEXT NOT NULL DEFAULT '',
    formula_expr  TEXT NOT NULL,
    version       INT  NOT NULL DEFAULT 1,
    valid_from    DATE,
    UNIQUE (code, template_code, version)
);

CREATE TABLE IF NOT EXISTS pl_formula_override (
    id             BIGSERIAL PRIMARY KEY,
    pl_id          BIGINT NOT NULL REFERENCES pl_instance(id) ON DELETE CASCADE,
    scope_code_cfo INT,                           -- площадка/ЦФО (NULL = весь срез)
    code           TEXT NOT NULL,
    block_type     TEXT NOT NULL DEFAULT '',
    formula_expr   TEXT NOT NULL,
    reason         TEXT NOT NULL,                 -- обязательное основание (как ADJ-02)
    version        INT  NOT NULL DEFAULT 1,
    author_id      BIGINT REFERENCES users(id),
    created_at     TIMESTAMPTZ NOT NULL DEFAULT NOW()
);
CREATE INDEX IF NOT EXISTS idx_pl_formula_override_pl ON pl_formula_override(pl_id);

-- Провизорные дефолтные формулы каскада TPL-MP (Q4b — уточнить у автора прототипа).
INSERT INTO calc_rule (code, template_code, formula_expr) VALUES
    ('sales_net',    'TPL-MP', 'sales / (1 + vat)'),
    ('gross_margin', 'TPL-MP', 'sales - cost'),
    ('markup_pct',   'TPL-MP', '(sales - cost) / cost')
ON CONFLICT (code, template_code, version) DO NOTHING;
