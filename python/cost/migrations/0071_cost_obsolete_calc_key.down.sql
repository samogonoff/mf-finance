-- Откат 0071: ключ снова пара модель+артикул. Пометки конкретных калькуляций
-- (включая развёрнутые из старых пар) при этом теряются — перед откатом на
-- проде выгрузить cost_obsolete, если данные нужны.

ALTER TABLE cost_obsolete DROP CONSTRAINT IF EXISTS uq_obsolete_key;

DELETE FROM cost_obsolete
 WHERE NOT (calc_sign = '' AND plan_id = '' AND task_number = '');

ALTER TABLE cost_obsolete
    DROP COLUMN IF EXISTS calc_sign,
    DROP COLUMN IF EXISTS plan_id,
    DROP COLUMN IF EXISTS task_number;

ALTER TABLE cost_obsolete
    ADD CONSTRAINT uq_obsolete_key UNIQUE (model, articul);

DROP INDEX IF EXISTS ix_obsolete_active;
CREATE INDEX IF NOT EXISTS ix_obsolete_active
    ON cost_obsolete (model, articul) WHERE obsolete;
