-- 0027 — движок ПРОЦЕССА: задания на этапах. Задание = форма × ЦФО-срез + роль.
-- Шаблоны (конструктор) задают, какие задания генерятся на этапе и по какому
-- разрезу; экземпляры заданий — на конкретный период (pl_instance) с авто-ТОПом.
-- Делегирование 2-ступ. + владелец этапа двигает этап. ТЗ §«Согласование»/§ACL.

-- Владелец этапа (финансист) — кто проверяет все задания и двигает этап.
ALTER TABLE pl_stage_instance ADD COLUMN IF NOT EXISTS owner_user_id BIGINT REFERENCES users(id);

-- Шаблоны заданий этапа (конструктор). group_by: position (по ТОПу ЦФО) |
-- legal_entity (по ЮЛ, согласование) | none (одно задание на весь срез).
CREATE TABLE IF NOT EXISTS pl_task_template (
    id          BIGSERIAL PRIMARY KEY,
    stage_code  TEXT NOT NULL,
    form_code   TEXT NOT NULL,
    title       TEXT NOT NULL,
    cfo_filter  JSONB NOT NULL DEFAULT '{}',   -- entity_type/group_cfo1/group_cfo2/country/legal_entity
    task_role   TEXT NOT NULL DEFAULT 'fill',  -- fill | approve
    group_by    TEXT NOT NULL DEFAULT 'position',
    sort_order  INT  NOT NULL DEFAULT 100
);

-- Экземпляры заданий (на период pl_instance).
CREATE TABLE IF NOT EXISTS pl_task (
    id               BIGSERIAL PRIMARY KEY,
    pl_id            BIGINT NOT NULL REFERENCES pl_instance(id) ON DELETE CASCADE,
    stage_code       TEXT NOT NULL,
    template_id      BIGINT REFERENCES pl_task_template(id) ON DELETE SET NULL,
    form_code        TEXT NOT NULL,
    title            TEXT NOT NULL,
    cfo_codes        JSONB NOT NULL DEFAULT '[]',
    task_role        TEXT NOT NULL DEFAULT 'fill',
    position_id      BIGINT REFERENCES plans_job_position(id) ON DELETE SET NULL,
    legal_entity     TEXT NOT NULL DEFAULT '',
    assignee_user_id BIGINT REFERENCES users(id),
    delegate_user_id BIGINT REFERENCES users(id),
    status           TEXT NOT NULL DEFAULT 'pending',
    created_at       TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at       TIMESTAMPTZ NOT NULL DEFAULT NOW()
);
CREATE INDEX IF NOT EXISTS idx_pl_task_pl ON pl_task(pl_id, stage_code);
CREATE INDEX IF NOT EXISTS idx_pl_task_assignee ON pl_task(assignee_user_id);
CREATE INDEX IF NOT EXISTS idx_pl_task_delegate ON pl_task(delegate_user_id);

-- Шаблоны заданий по схеме ТЗ (приоритетные формы; разрез — по ТОПу ЦФО).
INSERT INTO pl_task_template (stage_code, form_code, title, cfo_filter, task_role, group_by, sort_order) VALUES
    ('1.1', 'TPL-TO-RETAIL',    'Товарооборот розницы',  '{"group_cfo1":"Розница"}',      'fill',    'position', 10),
    ('1.1', 'TPL-MP',           'Маркетплейсы large',    '{"segment":"large"}',           'fill',    'position', 20),
    ('1.1', 'TPL-MP',           'Маркетплейсы small',    '{"segment":"small"}',           'fill',    'position', 25),
    ('1.5', 'TPL-CFO-EXP',      'Затраты ЦП',            '{"entity_type":"отдел"}',       'fill',    'position', 30),
    ('2.1', 'TPL-PROD-MINUTES', 'Минуты и объёмы пр-ва', '{"group_cfo1":"Производство"}', 'fill',    'position', 40),
    ('2.3', 'TPL-CFO-EXP',      'Затраты ЦЗ',            '{"entity_type":"отдел"}',       'fill',    'position', 50),
    ('3',   'TPL-LE-APPROVE',   'Согласование бюджета ЮЛ','{}',                           'approve', 'legal_entity', 60)
ON CONFLICT DO NOTHING;
