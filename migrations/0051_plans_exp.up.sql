-- Этап 1.5 «Расходы центров прибыли» (ТЗ docs/reports/plans/tz-stage-1.5-2026-09-09.md,
-- план docs/reports/plans/stage-1.5-plan.md).
--
-- Почему своя сетка, а не tp_row/tp_value формы продаж:
--   - строка 1.5 — это ПАРА «ЦФО × статья PL», а tp_row имеет UNIQUE (instance_id,
--     code_cfo), то есть одна строка на объект. Переиспользовать нельзя;
--   - период формы — ГОД (двенадцать месяцев плана и двенадцать факта, ТЗ §1.3),
--     а tp_value хранит одно значение на (строка × месяц) внутри месячной карточки;
--   - пусто и ноль в 1.5 РАЗНЫЕ (ТЗ §7.8, валидация E-11), поэтому amount
--     nullable — в tp_value он NOT NULL DEFAULT 0, и это поведение копировать нельзя.

CREATE TABLE IF NOT EXISTS exp_instance (
    id           BIGSERIAL PRIMARY KEY,
    card_id      BIGINT NOT NULL REFERENCES form_card(id) ON DELETE CASCADE,
    -- retail (форма 1.5-Р) | mp (форма 1.5-МП). Отбор строк из источника разный
    -- (ТЗ §2.1), а оболочка процесса и сетка — общие.
    form_kind    TEXT NOT NULL,
    country      TEXT NOT NULL,
    plan_year    INT  NOT NULL,
    currency     TEXT NOT NULL,                    -- валюта ВВОДА, нац. (ТЗ §1.3)
    legal_entity TEXT NOT NULL DEFAULT '',
    created_by   BIGINT REFERENCES users(id),
    created_at   TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at   TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    UNIQUE (card_id)
);

CREATE TABLE IF NOT EXISTS exp_row (
    id           BIGSERIAL PRIMARY KEY,
    instance_id  BIGINT NOT NULL REFERENCES exp_instance(id) ON DELETE CASCADE,
    code_cfo     INT  NOT NULL,
    code_pl      INT  NOT NULL,
    -- Атрибуты строки: часть приходит из источника затрат (ТЗ §2.1), часть
    -- дотягивается из справочника ЦФО (ТЗ §2.4) — город, тип магазина, РМ, ЮЛ.
    cfo             TEXT NOT NULL DEFAULT '',
    group_cfo1      TEXT NOT NULL DEFAULT '',
    group_cfo2      TEXT NOT NULL DEFAULT '',      -- город (ТЗ §7.1)
    group_pl        TEXT NOT NULL DEFAULT '',
    expense         TEXT NOT NULL DEFAULT '',
    legal_entity    TEXT NOT NULL DEFAULT '',
    country_company TEXT NOT NULL DEFAULT '',
    store_type      TEXT NOT NULL DEFAULT '',
    reg_manager     TEXT NOT NULL DEFAULT '',
    attrs           JSONB NOT NULL DEFAULT '{}',
    -- Комментарий строки: обязателен при сработавшем предупреждении E-18 (ТЗ §7.8).
    comment         TEXT NOT NULL DEFAULT '',
    -- Растёт при пересинхронизации атрибутов из справочника — видно, что строка
    -- «догнала» новую версию НСИ.
    row_version     INT  NOT NULL DEFAULT 1,
    created_at      TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at      TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    UNIQUE (instance_id, code_cfo, code_pl)
);
CREATE INDEX IF NOT EXISTS exp_row_instance_idx  ON exp_row (instance_id);
CREATE INDEX IF NOT EXISTS exp_row_group_pl_idx  ON exp_row (instance_id, group_pl);

CREATE TABLE IF NOT EXISTS exp_value (
    id         BIGSERIAL PRIMARY KEY,
    row_id     BIGINT NOT NULL REFERENCES exp_row(id) ON DELETE CASCADE,
    -- plan — ввод пользователя, fact — read-only снимок источника (ТЗ §2.5).
    scenario   TEXT NOT NULL DEFAULT 'plan',
    month      INT  NOT NULL CHECK (month BETWEEN 1 AND 12),
    -- NULL = ячейка не заполнена. Ноль вводится явно и означает «расхода нет»
    -- (ТЗ §7.8, E-11). Отличие от tp_value.amount NOT NULL DEFAULT 0 — намеренное.
    amount     NUMERIC(18,2),
    source     TEXT NOT NULL DEFAULT 'manual',
    note       TEXT NOT NULL DEFAULT '',
    updated_by BIGINT REFERENCES users(id),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    UNIQUE (row_id, scenario, month)
);
CREATE INDEX IF NOT EXISTS exp_value_row_idx ON exp_value (row_id, scenario);

-- Журнал правок (ТЗ §1.2: кто, когда, прежнее значение, новое, номер версии).
-- Доступен согласующему и наблюдателю, поэтому это таблица, а не лог в stdout.
CREATE TABLE IF NOT EXISTS exp_value_version (
    id         BIGSERIAL PRIMARY KEY,
    row_id     BIGINT NOT NULL REFERENCES exp_row(id) ON DELETE CASCADE,
    scenario   TEXT NOT NULL DEFAULT 'plan',
    month      INT  NOT NULL,
    amount_old NUMERIC(18,2),                      -- NULL = ячейка была пустой
    amount_new NUMERIC(18,2),
    card_version INT NOT NULL DEFAULT 0,
    changed_by BIGINT REFERENCES users(id),
    changed_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);
CREATE INDEX IF NOT EXISTS exp_value_version_row_idx ON exp_value_version (row_id, changed_at DESC);

-- Маршрут формы 1.5. Шаги и их порядок — те же, что у форм комплекта (0031):
-- маршрут это ДАННЫЕ, шаг «Финансист» включается флагом без правки кода.
--
-- publish_on_approve = FALSE намеренно: пока не решено, что делать с общими
-- затратами МП (mp_common_cost) и с ФОТ/арендой розницы, которые по ТЗ §1
-- переезжают в 1.5, две формы будут писать одни статьи PL в один приёмник.
-- До ответа аналитика (вопросы В-1/В-2 плана) публикация 1.5 — только dry-run.
INSERT INTO plans_form_route
    (form_code, step_code, step_name, sort_order, kind, responsible, due_rd, enabled, skip_if_same_user, publish_on_approve)
VALUES
    ('TPL-CFO-EXP', '1.1', 'Заполнение расходов ЦП',    10, 'fill',    'ответственный за ЦФО',      5, TRUE,  FALSE, FALSE),
    ('TPL-CFO-EXP', 'fin', 'Финансовый контролёр',      20, 'approve', 'финансист',                 5, FALSE, FALSE, FALSE),
    ('TPL-CFO-EXP', '1.2', 'Согласование руководителем',30, 'approve', 'руководитель направления',  6, TRUE,  TRUE,  FALSE),
    ('TPL-CFO-EXP', '1.4', 'Утверждение',               40, 'final',   'Сипарова С.Г.',             6, TRUE,  FALSE, FALSE)
ON CONFLICT (form_code, step_code) DO NOTHING;
