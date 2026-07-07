-- 0024 — настоящие ДОЛЖНОСТИ (не роли в процессе): IT-Директор = Серяков и т.п.
-- Плоские (1 должность = 1 носитель). Замы носителя — из plans_deputy (глоб.+этапы).
-- Должность «покрывает» набор ЦФО (plans_cfo_position) → она их ТОП (живая связь:
-- сменил носителя — ТОП обновился у всех её ЦФО). ТЗ: ТОП заводится в справочнике ЦФО.

CREATE TABLE IF NOT EXISTS plans_job_position (
    id             BIGSERIAL PRIMARY KEY,
    title          TEXT NOT NULL,
    holder_user_id BIGINT REFERENCES users(id) ON DELETE SET NULL,
    description    TEXT NOT NULL DEFAULT '',
    sort_order     INT  NOT NULL DEFAULT 100,
    created_at     TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

-- Покрытие: какой ЦФО какой должностью ведётся (ТОП). code_cfo = внешний ключ
-- строки dir_cfo (external_id). Живая связь, переживает переимпорт dir_cfo.
CREATE TABLE IF NOT EXISTS plans_cfo_position (
    code_cfo    TEXT PRIMARY KEY,
    position_id BIGINT NOT NULL REFERENCES plans_job_position(id) ON DELETE CASCADE,
    updated_at  TIMESTAMPTZ NOT NULL DEFAULT NOW()
);
CREATE INDEX IF NOT EXISTS idx_plans_cfo_position_pos ON plans_cfo_position(position_id);

-- Примеры должностей (носители назначаются в UI). Преднаполнение для наглядности.
INSERT INTO plans_job_position (title, description, sort_order) VALUES
    ('IT-Директор',            'ТОП ИТ-подразделений', 10),
    ('Коммерческий директор',  'ТОП коммерческого блока', 20),
    ('Директор по рознице',    'ТОП розницы', 30),
    ('Директор по производству','ТОП производства', 40),
    ('Финансовый директор',    'ТОП финансового блока', 50)
ON CONFLICT DO NOTHING;
