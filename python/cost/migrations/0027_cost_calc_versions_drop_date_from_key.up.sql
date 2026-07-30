-- 0027 — версии калькуляции больше не привязаны к конкретной "дата расчета".
--
-- Источник (MSSQL CostHistory) регулярно пересчитывает одно и то же задание
-- (PLAN_ID), создавая НОВУЮ "дата расчета" при каждом пересчёте — старые не
-- удаляются. Раньше версия (черновик/pending/approved) была уникальна по
-- (model, articul, calc_sign, plan_id, "дата расчета", version), поэтому при
-- очередном пересчёте источника вся история согласования незаметно "отвязывалась"
-- от задания — редактор открывался как будто версий никогда не было.
--
-- Теперь версия принадлежит заданию (model, articul, calc_sign, plan_id) целиком;
-- "дата расчета" остаётся информационным полем (на основе какой даты создана
-- версия), но не входит в ключ. Применение версии к cost_data_cache
-- (_apply_version_rows_to_cache) всегда бьёт в АКТУАЛЬНУЮ (max) дату расчёта
-- для ключа на момент применения — см. db.py.

ALTER TABLE cost_calc_versions
    DROP CONSTRAINT cost_calc_versions_model_articul_calc_sign_plan_id_дата_key;
ALTER TABLE cost_calc_versions
    ADD CONSTRAINT cost_calc_versions_model_articul_calc_sign_plan_id_version_key
        UNIQUE (model, articul, calc_sign, plan_id, version);

DROP INDEX idx_ver_one_original;
CREATE UNIQUE INDEX idx_ver_one_original
    ON cost_calc_versions (model, articul, calc_sign, plan_id)
    WHERE status = 'original';
