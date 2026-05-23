ALTER TABLE notifications
    DROP COLUMN IF EXISTS b24_last_error,
    DROP COLUMN IF EXISTS b24_attempts,
    DROP COLUMN IF EXISTS b24_sent_at;

ALTER TABLE users
    DROP COLUMN IF EXISTS notify_via_b24;
