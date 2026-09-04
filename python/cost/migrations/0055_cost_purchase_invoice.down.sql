-- Откат 0055: инвойсы прихода закупной продукции и их строки. Созданные при
-- применении калькуляции ПФКСС остаются в cost_manual_calc
-- (source_calc_sign = 'ПРИХОД') и раскладка в cost_purchase_cost (source =
-- 'invoice') — удалить отдельно, если нужно:
--   DELETE FROM cost_purchase_cost WHERE source = 'invoice';
--   DELETE FROM cost_manual_calc WHERE source_calc_sign = 'ПРИХОД';

DROP TABLE IF EXISTS cost_purchase_invoice_line;
DROP TABLE IF EXISTS cost_purchase_invoice_overhead;
DROP TABLE IF EXISTS cost_purchase_invoice;
