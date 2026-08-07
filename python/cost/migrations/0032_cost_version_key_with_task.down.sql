-- Откат 0032 — номер задания убирается из ключа версии.
--
-- ВНИМАНИЕ: откат может УПАСТЬ на дубликатах, и это ожидаемо. Убирая поле из
-- ключа, мы сливаем группы: если после 0032 успели создать версии с одинаковым
-- номером version под разными заданиями одного (model, articul, calc_sign,
-- plan_id), они станут конфликтующими. Ровно на этом упала миграция 0027 в CI.
-- Перед откатом такие версии нужно перенумеровать или удалить вручную —
-- автоматически решить, какая из них «главная», нельзя.
--
-- Найти конфликты заранее:
--   SELECT model, articul, calc_sign, plan_id, version, count(*)
--   FROM cost_calc_versions
--   GROUP BY 1,2,3,4,5 HAVING count(*) > 1;

DROP INDEX idx_ver_one_original;
CREATE UNIQUE INDEX idx_ver_one_original
    ON cost_calc_versions (model, articul, calc_sign, plan_id)
    WHERE status = 'original';

ALTER TABLE cost_calc_versions
    DROP CONSTRAINT cost_calc_versions_key_task_version_key;
ALTER TABLE cost_calc_versions
    ADD CONSTRAINT cost_calc_versions_model_articul_calc_sign_plan_id_version_key
        UNIQUE (model, articul, calc_sign, plan_id, version);

ALTER TABLE cost_calc_versions
    DROP COLUMN IF EXISTS task_number;
