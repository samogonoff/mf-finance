-- CFO forms 31/32/33/34/37/962: manually edited supplemental registries.
-- One JSON row keeps sheet-specific attributes and nullable monthly inputs;
-- this table never writes the form grid or publishes a registry automatically.
CREATE TABLE IF NOT EXISTS cfo_exp_registry (
  id BIGSERIAL PRIMARY KEY,
  card_id BIGINT NOT NULL REFERENCES form_card(id) ON DELETE CASCADE,
  book_scope TEXT NOT NULL,
  sheet TEXT NOT NULL,
  sort_order INT NOT NULL DEFAULT 0,
  attrs JSONB NOT NULL DEFAULT '{}',
  body JSONB NOT NULL DEFAULT '{}',
  created_by BIGINT REFERENCES users(id),
  updated_by BIGINT REFERENCES users(id),
  created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
  updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);
CREATE INDEX IF NOT EXISTS cfo_exp_registry_sheet_idx ON cfo_exp_registry(card_id, book_scope, sheet, sort_order, id);
