-- Откат 0034 — справочник НДС и помесячные курсы.
DELETE FROM plans_directory_row WHERE directory_id = (SELECT id FROM plans_directory WHERE code = 'dir_vat');
DELETE FROM plans_directory WHERE code = 'dir_vat';
DELETE FROM plans_directory_row
 WHERE directory_id = (SELECT id FROM plans_directory WHERE code = 'dir_fx_rate')
   AND external_id IN ('BYN-0-0', 'RUB-0-0', 'USD-0-0', 'KZT-0-0', 'UZS-0-0');
