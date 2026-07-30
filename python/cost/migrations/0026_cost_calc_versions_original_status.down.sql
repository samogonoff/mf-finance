DROP INDEX IF EXISTS idx_ver_one_original;

ALTER TABLE cost_calc_versions DROP CONSTRAINT cost_calc_versions_status_check;
ALTER TABLE cost_calc_versions ADD CONSTRAINT cost_calc_versions_status_check
    CHECK (status IN ('draft', 'pending', 'approved', 'rejected', 'archived'));
