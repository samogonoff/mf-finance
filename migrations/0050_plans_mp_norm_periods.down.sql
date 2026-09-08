-- Откат 0050: убрать справочник нормативов (значения 0048 не восстанавливаются —
-- их вернёт повторный накат 0048).
DELETE FROM plans_directory_row
 WHERE directory_id = (SELECT id FROM plans_directory WHERE code = 'dir_mp_norm');
DELETE FROM plans_directory WHERE code = 'dir_mp_norm';
