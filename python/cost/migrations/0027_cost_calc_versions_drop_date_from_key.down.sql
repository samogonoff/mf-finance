DROP INDEX idx_ver_one_original;

ALTER TABLE cost_calc_versions
    DROP CONSTRAINT cost_calc_versions_model_articul_calc_sign_plan_id_version_key;
ALTER TABLE cost_calc_versions
    ADD CONSTRAINT cost_calc_versions_model_articul_calc_sign_plan_id_дата_key
        UNIQUE (model, articul, calc_sign, plan_id, "дата расчета", version);

CREATE UNIQUE INDEX idx_ver_one_original
    ON cost_calc_versions (model, articul, calc_sign, plan_id, "дата расчета")
    WHERE status = 'original';
