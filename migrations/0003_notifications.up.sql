-- 0003 — in-app уведомления (один общий поток).
-- Упрощённый вариант MP-таблицы notifications: без stream-разделения,
-- без B24/Telegram-полей доставки. См. план переноса (Слой 1).

CREATE TABLE IF NOT EXISTS notifications (
    id          BIGSERIAL PRIMARY KEY,
    user_id     BIGINT       NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    title       TEXT         NOT NULL,
    message     TEXT         NOT NULL DEFAULT '',
    type        TEXT         NOT NULL DEFAULT 'info',   -- info | success | warning | error
    is_read     BOOLEAN      NOT NULL DEFAULT FALSE,
    created_at  TIMESTAMPTZ  NOT NULL DEFAULT NOW(),
    read_at     TIMESTAMPTZ,
    data        JSONB        NOT NULL DEFAULT '{}'::jsonb,
    object_type TEXT
);

CREATE INDEX IF NOT EXISTS idx_notifications_user_read    ON notifications (user_id, is_read);
CREATE INDEX IF NOT EXISTS idx_notifications_user_created ON notifications (user_id, created_at DESC);
