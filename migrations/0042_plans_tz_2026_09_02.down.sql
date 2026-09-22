-- Откат 0042 — сценарии, состав розницы и строки ЮЛ с УНП.
DELETE FROM plans_directory_row
 WHERE directory_id IN (SELECT id FROM plans_directory WHERE code IN ('dir_scenario', 'dir_retail_scope'));
DELETE FROM plans_directory WHERE code IN ('dir_scenario', 'dir_retail_scope');
DELETE FROM plans_directory_row
 WHERE directory_id = (SELECT id FROM plans_directory WHERE code = 'dir_legal_entity')
   AND (external_id LIKE 'FICT-%' OR external_id IN
        ('BY-690719790', 'KZ-141240004842', 'RU-6950135110', 'UZ-305554644'));
