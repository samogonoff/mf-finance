-- 0005 — дублирование уведомлений в Битрикс24.
-- Доставка включается, только если задан ENV B24_NOTIFY_WEBHOOK_URL
-- (в dev по умолчанию пусто; в prod webhook задаётся в env-файле сервиса).
-- Сам факт отправки и ошибки складываем в аудит-поля; retry-цикла на старте нет.

ALTER TABLE users
    ADD COLUMN IF NOT EXISTS notify_via_b24 BOOLEAN NOT NULL DEFAULT TRUE;

ALTER TABLE notifications
    ADD COLUMN IF NOT EXISTS b24_sent_at    TIMESTAMPTZ,
    ADD COLUMN IF NOT EXISTS b24_attempts   INTEGER NOT NULL DEFAULT 0,
    ADD COLUMN IF NOT EXISTS b24_last_error TEXT;
