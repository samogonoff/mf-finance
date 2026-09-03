-- Откат 0044 — справочники-подсказки формы МП.
DELETE FROM plans_directory_row
 WHERE directory_id IN (SELECT id FROM plans_directory WHERE code IN ('dir_mp_spp_hint', 'dir_mp_penalty_hint'));
DELETE FROM plans_directory WHERE code IN ('dir_mp_spp_hint', 'dir_mp_penalty_hint');
