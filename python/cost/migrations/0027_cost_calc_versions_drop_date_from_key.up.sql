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

-- Дедупликация перед сужением ключа: старый ключ включал "дата расчета", поэтому
-- в проде могли накопиться строки с одинаковым (model, articul, calc_sign, plan_id, version),
-- но разными датами расчёта — новый ключ их не пропустит. Не полагаемся на ручной
-- разбор конкретных найденных случаев: перенумеровываем ВСЕ такие конфликты сразу,
-- оставляя в каждой группе-дубликате версию с самой свежей "дата расчета" как есть,
-- а остальные сдвигаем за текущий максимум версии по заданию (status='original'
-- не трогаем — её версия обязана остаться 0, см. idx_ver_one_original).
WITH dupes AS (
    SELECT id, model, articul, calc_sign, plan_id,
           row_number() OVER (
               PARTITION BY model, articul, calc_sign, plan_id, version
               ORDER BY "дата расчета" DESC NULLS LAST, id DESC
           ) AS rn
    FROM cost_calc_versions
    WHERE status <> 'original'
),
to_renumber AS (
    SELECT id, model, articul, calc_sign, plan_id,
           row_number() OVER (
               PARTITION BY model, articul, calc_sign, plan_id
               ORDER BY id
           ) AS offset
    FROM dupes
    WHERE rn > 1
),
task_max AS (
    SELECT model, articul, calc_sign, plan_id, MAX(version) AS max_version
    FROM cost_calc_versions
    GROUP BY model, articul, calc_sign, plan_id
)
UPDATE cost_calc_versions v
SET version = tm.max_version + tr.offset
FROM to_renumber tr
JOIN task_max tm USING (model, articul, calc_sign, plan_id)
WHERE v.id = tr.id;

ALTER TABLE cost_calc_versions
    DROP CONSTRAINT cost_calc_versions_model_articul_calc_sign_plan_id_дата_key;
ALTER TABLE cost_calc_versions
    ADD CONSTRAINT cost_calc_versions_model_articul_calc_sign_plan_id_version_key
        UNIQUE (model, articul, calc_sign, plan_id, version);

DROP INDEX idx_ver_one_original;
CREATE UNIQUE INDEX idx_ver_one_original
    ON cost_calc_versions (model, articul, calc_sign, plan_id)
    WHERE status = 'original';
