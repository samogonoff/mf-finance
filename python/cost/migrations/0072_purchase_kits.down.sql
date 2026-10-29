-- Откат 0072 · комплекты прихода закупной продукции.
--
-- Созданные при применении калькуляции ПФКСС комплектов остаются:
--   cost_manual_calc      (source_calc_sign = 'ПРИХОД'),
--   cost_purchase_cost    (source = 'invoice'),
-- при необходимости удалить отдельно (см. 0055 down).

ALTER TABLE cost_purchase_invoice_line DROP COLUMN IF EXISTS kit_id;
DROP TABLE IF EXISTS cost_purchase_invoice_kit;
