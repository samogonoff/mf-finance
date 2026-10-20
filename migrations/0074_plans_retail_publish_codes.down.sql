UPDATE publish_mapping SET code_pl = 2011, updated_at = NOW()
 WHERE form_code = 'TPL-TO-RETAIL' AND block_type = 'sales_plan' AND country = 'RU' AND param_name = 'ПРОДАЖИ';
UPDATE publish_mapping SET code_pl = NULL, updated_at = NOW()
 WHERE form_code = 'TPL-TO-RETAIL' AND block_type = 'sales_plan' AND country IN ('KZ', 'UZ') AND param_name = 'ПРОДАЖИ';