-- Откат 0035 — форма «Розница» (TPL-TO-RETAIL).
-- Порядок обратный созданию: сначала зависимые таблицы, потом tp_instance.
DROP TABLE IF EXISTS tp_reg_manager_map;
DROP TABLE IF EXISTS tp_period_param;
DROP TABLE IF EXISTS tp_lfl_override;
DROP TABLE IF EXISTS tp_value_version;
DROP TABLE IF EXISTS tp_value;
DROP TABLE IF EXISTS tp_row;
DROP TABLE IF EXISTS tp_instance;

-- Справочники розницы.
DELETE FROM plans_directory_row
 WHERE directory_id IN (SELECT id FROM plans_directory WHERE code IN ('dir_retail_store', 'dir_retail_group_map'));
DELETE FROM plans_directory WHERE code IN ('dir_retail_store', 'dir_retail_group_map');

-- Правила публикации KZ/UZ, добавленные этой миграцией (BY/RU остаются от 0032).
DELETE FROM publish_mapping
 WHERE form_code = 'TPL-TO-RETAIL' AND block_type = 'sales_plan' AND country IN ('KZ', 'UZ');
