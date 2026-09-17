-- 0057 down — один снимок источника на ключ, как в 0032.
-- Лишние снимки удаляются (строки — каскадом); версии, сделанные от них,
-- теряют основание (ON DELETE SET NULL) — это информационное поле.

DELETE FROM cost_calc_versions
 WHERE status = 'original' AND snapshot_no IS NOT NULL AND snapshot_no <> 1;

DROP INDEX IF EXISTS idx_ver_source_version;
DROP INDEX IF EXISTS idx_ver_snapshot_no;
CREATE UNIQUE INDEX IF NOT EXISTS idx_ver_one_original
    ON cost_calc_versions (model, articul, calc_sign, plan_id, task_number)
    WHERE status = 'original';

DROP INDEX IF EXISTS idx_ver_key_task_version;
ALTER TABLE cost_calc_versions
    ADD CONSTRAINT cost_calc_versions_key_task_version_key
        UNIQUE (model, articul, calc_sign, plan_id, task_number, version);

ALTER TABLE cost_calc_versions
    DROP COLUMN IF EXISTS source_version_id,
    DROP COLUMN IF EXISTS source_hash,
    DROP COLUMN IF EXISTS snapshot_no;
