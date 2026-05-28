-- 0009 — настройки инкрементального ETL (вкл/выкл, интервал, последняя ошибка).
-- Key-value таблица, читается goroutine'ой incremental-worker'а в go-api на каждом тике.
-- Меняется через PUT /api/admin/etl/debt/settings (роль ROLE_ADMIN).
--
-- Ключи:
--   incremental_enabled         — '1' | '0'   (вкл/выкл периодического pull)
--   incremental_interval_minutes — целое в минутах (default 10)
--   incremental_last_tick_at    — RFC3339 (когда воркер последний раз пробежался)
--   incremental_last_error      — текст последней ошибки (или пустая строка)

CREATE TABLE IF NOT EXISTS debt_etl_settings (
    key         TEXT PRIMARY KEY,
    value       TEXT NOT NULL,
    updated_at  TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

INSERT INTO debt_etl_settings (key, value) VALUES
    ('incremental_enabled',           '0'),
    ('incremental_interval_minutes',  '10'),
    ('incremental_last_tick_at',      ''),
    ('incremental_last_error',        '')
ON CONFLICT (key) DO NOTHING;
