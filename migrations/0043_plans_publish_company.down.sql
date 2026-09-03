-- Откат 0043 — колонка «Компания» в песочнице и включение записи плана.
ALTER TABLE publish_sandbox DROP COLUMN IF EXISTS company;

UPDATE publish_mapping SET enabled = FALSE
 WHERE form_code IN ('TPL-MP', 'TPL-TO-RETAIL');
