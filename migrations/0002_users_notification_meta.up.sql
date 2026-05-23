-- 0002 — мета-поля пользователя для системы уведомлений.
-- welcome_notification_sent выставляется один раз при первом успешном логине
-- (см. auth.Service.HandleB24Callback → notifications.Service.CreateWelcome).

ALTER TABLE users
    ADD COLUMN IF NOT EXISTS welcome_notification_sent BOOLEAN NOT NULL DEFAULT FALSE;
