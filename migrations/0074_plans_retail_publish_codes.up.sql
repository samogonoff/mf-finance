-- Коды PL продаж розницы в правилах публикации: по данным приёмника Budgeting.dbo.VFORMTOLOADTAKTTARGET
-- (Параметр «ПРОДАЖИ», магазины группы «Магазины», проверено 20.10.2026): BY 1001, RU 1011, KZ 1012, UZ 1013.
-- Раньше у RU стоял 2011 (код себестоимости — продажи уехали бы не в ту статью), у KZ и UZ код не был задан.
UPDATE publish_mapping
   SET code_pl = 1011,
       note = 'КодPL 1011 — продажи магазинов РФ в приёмнике (проверено выборкой 20.10.2026; было 2011 — код себестоимости)',
       updated_at = NOW()
 WHERE form_code = 'TPL-TO-RETAIL' AND block_type = 'sales_plan' AND country = 'RU' AND param_name = 'ПРОДАЖИ';

UPDATE publish_mapping
   SET code_pl = 1012,
       note = 'КодPL 1012 — продажи магазинов Казахстана в приёмнике (проверено выборкой 20.10.2026: 47 ЦФО)',
       updated_at = NOW()
 WHERE form_code = 'TPL-TO-RETAIL' AND block_type = 'sales_plan' AND country = 'KZ' AND param_name = 'ПРОДАЖИ';

UPDATE publish_mapping
   SET code_pl = 1013,
       note = 'КодPL 1013 — продажи магазинов Узбекистана в приёмнике (проверено выборкой 20.10.2026: 9 ЦФО)',
       updated_at = NOW()
 WHERE form_code = 'TPL-TO-RETAIL' AND block_type = 'sales_plan' AND country = 'UZ' AND param_name = 'ПРОДАЖИ';