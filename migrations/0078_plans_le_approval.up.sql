-- Согласование бюджетов ЮЛ (этап 3) и итоговое утверждение (этап 4): настраиваемые согласующие.
--
-- Этап 3 — по каждому юридическому лицу свой список согласующих, этап 4 — один список на период (legal_entity = '').
-- Политика списка: require_all = TRUE — согласие каждого, FALSE — достаточно одного. Решения хранятся отдельно от листа
-- согласования карточек; снятые (возврат, корректировка итога) остаются в журнале с причиной.
CREATE TABLE IF NOT EXISTS pl_final_approver (
    stage_code   TEXT        NOT NULL CHECK (stage_code IN ('3', '4')),
    legal_entity TEXT        NOT NULL DEFAULT '',
    user_id      BIGINT      NOT NULL,
    created_by   BIGINT,
    created_at   TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    PRIMARY KEY (stage_code, legal_entity, user_id)
);

CREATE TABLE IF NOT EXISTS pl_final_policy (
    stage_code   TEXT        NOT NULL CHECK (stage_code IN ('3', '4')),
    legal_entity TEXT        NOT NULL DEFAULT '',
    require_all  BOOLEAN     NOT NULL DEFAULT TRUE,
    updated_by   BIGINT,
    updated_at   TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    PRIMARY KEY (stage_code, legal_entity)
);

CREATE TABLE IF NOT EXISTS pl_final_decision (
    id            BIGSERIAL PRIMARY KEY,
    pl_id         BIGINT      NOT NULL REFERENCES pl_instance(id) ON DELETE CASCADE,
    stage_code    TEXT        NOT NULL CHECK (stage_code IN ('3', '4')),
    legal_entity  TEXT        NOT NULL DEFAULT '',
    user_id       BIGINT      NOT NULL,
    decision      TEXT        NOT NULL CHECK (decision IN ('approve', 'return')),
    comment       TEXT        NOT NULL DEFAULT '',
    created_at    TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    revoked       BOOLEAN     NOT NULL DEFAULT FALSE,
    revoked_at    TIMESTAMPTZ,
    revoke_reason TEXT        NOT NULL DEFAULT ''
);

CREATE INDEX IF NOT EXISTS pl_final_decision_pl_idx ON pl_final_decision (pl_id, stage_code) WHERE NOT revoked;

-- Задание «Согласование бюджета ЮЛ» (этап 3) заменено решениями согласующих на экране «Бюджет по ЮЛ»: у задания не было ни
-- формы, ни данных. Шаблон убираем, чтобы «Генерировать задания» не создавал пустые задания; уже созданные остаются.
DELETE FROM pl_task_template WHERE form_code = 'TPL-LE-APPROVE';
