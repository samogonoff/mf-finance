-- 0004 — баг-трекер. Эталон: MP Version20260512120000CreateBugTracker.
-- Скриншоты хранятся на диске (volume bugtracker_uploads); в БД — только
-- массив путей (TEXT[]). Источники проблем — отдельный словарь, заполняется
-- админом при выставлении статуса resolved.

CREATE TABLE IF NOT EXISTS bug_report_sources (
    id          BIGSERIAL PRIMARY KEY,
    name        TEXT        NOT NULL UNIQUE,
    is_active   BOOLEAN     NOT NULL DEFAULT TRUE,
    created_at  TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE TABLE IF NOT EXISTS bug_reports (
    id                BIGSERIAL PRIMARY KEY,
    user_id           BIGINT       REFERENCES users(id) ON DELETE SET NULL,
    section           TEXT         NOT NULL,                       -- finance|cost|operations|reports|counterparties|analytics|account
    status            TEXT         NOT NULL DEFAULT 'new',         -- new|in_progress|resolved|duplicate|rejected
    type              TEXT         NOT NULL DEFAULT 'bug',         -- bug|data|ui|performance|other
    title             TEXT         NOT NULL,
    description       TEXT         NOT NULL DEFAULT '',
    steps             TEXT         NOT NULL DEFAULT '',
    signature         TEXT         NOT NULL,                       -- SHA256(section|url|title|description), для дедупа
    route             JSONB        NOT NULL DEFAULT '{}'::jsonb,   -- {path, fullPath, name, params, query}
    entity_ref        JSONB        NOT NULL DEFAULT '{}'::jsonb,   -- ссылка на доменную сущность ({type, id})
    context_snapshot  JSONB        NOT NULL DEFAULT '{}'::jsonb,   -- состояние фильтров/формы на момент репорта
    tech_context      JSONB        NOT NULL DEFAULT '{}'::jsonb,   -- {ua, platform, viewport, dpr, lang}
    console_logs      JSONB        NOT NULL DEFAULT '[]'::jsonb,
    network_errors    JSONB        NOT NULL DEFAULT '[]'::jsonb,
    js_errors         JSONB        NOT NULL DEFAULT '[]'::jsonb,
    screenshots       TEXT[]       NOT NULL DEFAULT '{}',
    admin_comment     TEXT         NOT NULL DEFAULT '',
    source_id         BIGINT       REFERENCES bug_report_sources(id) ON DELETE SET NULL,
    b24_task_id       TEXT,
    created_at        TIMESTAMPTZ  NOT NULL DEFAULT NOW(),
    updated_at        TIMESTAMPTZ  NOT NULL DEFAULT NOW(),
    resolved_at       TIMESTAMPTZ
);

CREATE INDEX IF NOT EXISTS idx_bug_reports_section_status ON bug_reports (section, status);
CREATE INDEX IF NOT EXISTS idx_bug_reports_user_created   ON bug_reports (user_id, created_at DESC);
CREATE INDEX IF NOT EXISTS idx_bug_reports_signature      ON bug_reports (signature);
CREATE INDEX IF NOT EXISTS idx_bug_reports_created        ON bug_reports (created_at DESC);
