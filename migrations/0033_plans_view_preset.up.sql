-- 0033 — пресеты представлений формы (состав/порядок колонок, группировка,
-- уровень детализации). ТЗ Розница §4.2/§4.6: «состав видимых колонок настраивается
-- пользователем и запоминается (пресеты представлений)», «последнее использованное
-- представление запоминается за пользователем и восстанавливается при следующем входе».
-- Обязательный минимум (код ЦФО, город, название магазина) пресетом не скрывается —
-- это правило кода, а не данных.

CREATE TABLE IF NOT EXISTS plans_view_preset (
    id         BIGSERIAL PRIMARY KEY,
    user_id    BIGINT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    form_code  TEXT NOT NULL,
    name       TEXT NOT NULL,                     -- '' = «последнее использованное»
    is_default BOOLEAN NOT NULL DEFAULT FALSE,
    payload    JSONB NOT NULL DEFAULT '{}',       -- {columns:[], group_by, detail, filters}
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    UNIQUE (user_id, form_code, name)
);
CREATE INDEX IF NOT EXISTS idx_view_preset_user ON plans_view_preset(user_id, form_code);
