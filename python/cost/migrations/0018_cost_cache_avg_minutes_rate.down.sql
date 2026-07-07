-- 0018 down — удаляем колонки AVG-полей
ALTER TABLE cost_data_cache
    DROP COLUMN IF EXISTS "Пошив, минуты",
    DROP COLUMN IF EXISTS "Раскрой, минуты",
    DROP COLUMN IF EXISTS "Курс на дату расчета";
