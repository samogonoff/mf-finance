-- 0020 down — удаляем колонки минут из cost_calc_version_rows
ALTER TABLE cost_calc_version_rows
    DROP COLUMN IF EXISTS "Пошив, минуты",
    DROP COLUMN IF EXISTS "Раскрой, минуты";
