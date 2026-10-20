-- Возврат кодов и обоснований к состоянию до 0074: коды — как в 0032/0035 (RU 2011, KZ/UZ не заданы),
-- примечание — текст, который последней выставила 0043 для стран кроме BY.
UPDATE publish_mapping
   SET code_pl = CASE WHEN country = 'RU' THEN 2011 ELSE NULL END,
       note = 'ожидает КодPL от BI: финальные правки 02.09.2026 кодов для этой страны не называют, а КодPL несёт показатель И канал/страну',
       updated_at = NOW()
 WHERE form_code = 'TPL-TO-RETAIL' AND block_type = 'sales_plan'
   AND country IN ('RU', 'KZ', 'UZ') AND param_name = 'ПРОДАЖИ';
