-- Откат 0054: детализация себестоимости закупной продукции и журнал импорта.
-- Сами импортированные калькуляции остаются в cost_manual_calc
-- (source_calc_sign = 'ПОРТАЛ') — при необходимости удалить их отдельно:
--   DELETE FROM cost_manual_calc WHERE source_calc_sign = 'ПОРТАЛ';

DROP TABLE IF EXISTS cost_purchase_import_run;
DROP TABLE IF EXISTS cost_purchase_cost;
