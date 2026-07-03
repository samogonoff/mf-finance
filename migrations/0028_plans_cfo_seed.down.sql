-- 0028 down — снести дамп-строки справочников ЦФО (вернуть к пустому/код-сиду).
DELETE FROM plans_directory_row
WHERE directory_id IN (
    SELECT id FROM plans_directory
    WHERE code IN ('dir_cfo', 'dir_cfo_group', 'dir_cfo_subgroup', 'dir_cfo_type')
);
UPDATE plans_directory SET sync_status = 'seed'
WHERE code IN ('dir_cfo', 'dir_cfo_group', 'dir_cfo_subgroup', 'dir_cfo_type');
