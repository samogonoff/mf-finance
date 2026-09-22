-- Откат 0047: убрать сценарные строки курсов (общие строки 0034 остаются).
DELETE FROM plans_directory_row
 WHERE directory_id = (SELECT id FROM plans_directory WHERE code = 'dir_fx_rate')
   AND external_id LIKE ANY (ARRAY['TACTIC-%', 'PLAN-%']);
