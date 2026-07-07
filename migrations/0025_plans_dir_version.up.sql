-- 0025 — версии справочников. Версия (plans_directory.version) бампается ТОЛЬКО
-- при реальных изменениях строк (diff>0), а не на каждом прогоне sync. История
-- версий (sync + ручные правки) — для отдельной вкладки «Версии» на карточке
-- справочника. ТЗ DIR-02 (версия, дата актуальности).
CREATE TABLE IF NOT EXISTS plans_dir_version (
    id           BIGSERIAL PRIMARY KEY,
    directory_id BIGINT NOT NULL REFERENCES plans_directory(id) ON DELETE CASCADE,
    version      INT NOT NULL,
    changed_at   TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    source       TEXT NOT NULL DEFAULT 'sync',  -- sync | manual
    changed_by   BIGINT REFERENCES users(id),   -- кто (для ручных правок)
    added        INT NOT NULL DEFAULT 0,
    changed      INT NOT NULL DEFAULT 0,
    removed      INT NOT NULL DEFAULT 0,
    summary      TEXT NOT NULL DEFAULT '',       -- человекочитаемое описание
    diff_sample  JSONB NOT NULL DEFAULT '[]'
);
CREATE INDEX IF NOT EXISTS idx_plans_dir_version ON plans_dir_version(directory_id, version DESC);
