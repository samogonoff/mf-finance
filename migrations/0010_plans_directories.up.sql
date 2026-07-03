-- 0010 — справочники модуля «Тактические планы» (docs/reports/plans/SPEC.md §7.3).
-- Первый срез VS1: реестр справочников + строки. Ядро процесса (pl_instance,
-- pl_metric, form_submission) приходит отдельной миграцией в VS3.
--
-- Для MVP строки dir_marketplace/dir_pl_line/dir_cfo отдаются API из seed-данных
-- в коде (go/internal/plans/seed_mp.go); таблицы созданы под будущую
-- синхронизацию из 1С/OLAP и ABAC-джойны. Метаданные справочников засеяны здесь.

CREATE TABLE IF NOT EXISTS plans_directory (
    id          BIGSERIAL PRIMARY KEY,
    code        TEXT UNIQUE NOT NULL,                 -- 'dir_marketplace','dir_pl_line','dir_cfo',…
    source      TEXT NOT NULL,                        -- lisa|1c|olap|manual|calculated
    sync_status TEXT NOT NULL DEFAULT 'never',        -- ok|error|stale|never|seed
    synced_at   TIMESTAMPTZ,
    last_error  TEXT
);

CREATE TABLE IF NOT EXISTS plans_directory_row (
    id           BIGSERIAL PRIMARY KEY,
    directory_id BIGINT NOT NULL REFERENCES plans_directory(id) ON DELETE CASCADE,
    external_id  TEXT,                                -- lisa_id / olap-ключ
    payload_json JSONB NOT NULL,
    valid_from   DATE,
    valid_to     DATE
);
CREATE INDEX IF NOT EXISTS idx_plans_dir_row ON plans_directory_row(directory_id);

-- Метаданные справочников MVP (источник истины по строкам — seed в коде).
INSERT INTO plans_directory (code, source, sync_status) VALUES
    ('dir_marketplace', 'manual', 'seed'),
    ('dir_pl_line',     'manual', 'seed'),
    ('dir_cfo',         'manual', 'seed')
ON CONFLICT (code) DO NOTHING;
