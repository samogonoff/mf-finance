-- 0019 — должность-центричная ролевая модель: связь должность↔процесс (этапы),
-- заместители (глобально + в разрезе этапа) и метка отсутствия пользователя
-- (отпуск/увольнение). Метка позже наполняется из Bitrix24 (users.b24_id);
-- при срабатывании функционал отсутствующего подхватывает зам.

-- 1. Связь «должность ↔ этапы процесса» (many-to-many; ТЗ §«должность к процессам»).
CREATE TABLE IF NOT EXISTS plans_position_stage (
    position_code TEXT NOT NULL REFERENCES plans_position(code) ON DELETE CASCADE,
    stage_code    TEXT NOT NULL,           -- '1.1'…'4'
    PRIMARY KEY (position_code, stage_code)
);

-- Преднаполняем из default_stage_code должностей (где он задан).
INSERT INTO plans_position_stage (position_code, stage_code)
SELECT code, default_stage_code FROM plans_position WHERE default_stage_code <> ''
ON CONFLICT DO NOTHING;

-- 2. Заместители. stage_code='' → глобальный зам (на весь функционал);
--    непустой → зам только в разрезе конкретного этапа.
CREATE TABLE IF NOT EXISTS plans_deputy (
    id                BIGSERIAL PRIMARY KEY,
    principal_user_id BIGINT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    deputy_user_id    BIGINT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    stage_code        TEXT NOT NULL DEFAULT '',
    note              TEXT NOT NULL DEFAULT '',
    updated_by        BIGINT REFERENCES users(id),
    updated_at        TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    UNIQUE (principal_user_id, stage_code),
    CHECK (principal_user_id <> deputy_user_id)
);
CREATE INDEX IF NOT EXISTS idx_plans_deputy_principal ON plans_deputy(principal_user_id);
CREATE INDEX IF NOT EXISTS idx_plans_deputy_deputy ON plans_deputy(deputy_user_id);

-- 3. Метка отсутствия пользователя (глобальная, во всех интерфейсах).
--    absence_status: '' | 'vacation' | 'sick' | 'dismissed'. Источник — позже B24
--    (users.b24_id уже есть); absence_synced_at фиксирует момент последнего пуша.
ALTER TABLE users ADD COLUMN IF NOT EXISTS absence_status    TEXT NOT NULL DEFAULT '';
ALTER TABLE users ADD COLUMN IF NOT EXISTS absence_until     DATE;
ALTER TABLE users ADD COLUMN IF NOT EXISTS absence_synced_at TIMESTAMPTZ;
