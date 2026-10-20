-- Возврат кодов и обоснований (тексты из 0032 и 0035): значение и примечание должны откатываться вместе.
UPDATE publish_mapping
   SET code_pl = 2011,
       note = 'код по срезу A4 (ЦФО 105 RU); подтвердить по справочнику CodePL',
       updated_at = NOW()
 WHERE form_code = 'TPL-TO-RETAIL' AND block_type = 'sales_plan' AND country = 'RU' AND param_name = 'ПРОДАЖИ';

UPDATE publish_mapping
   SET code_pl = NULL,
       note = 'ТЗ Розница §7.2/§12: КодPL по срезу KZ подтверждает BI',
       updated_at = NOW()
 WHERE form_code = 'TPL-TO-RETAIL' AND block_type = 'sales_plan' AND country = 'KZ' AND param_name = 'ПРОДАЖИ';

UPDATE publish_mapping
   SET code_pl = NULL,
       note = 'ТЗ Розница §7.2/§12: КодPL по срезу UZ подтверждает BI',
       updated_at = NOW()
 WHERE form_code = 'TPL-TO-RETAIL' AND block_type = 'sales_plan' AND country = 'UZ' AND param_name = 'ПРОДАЖИ';
