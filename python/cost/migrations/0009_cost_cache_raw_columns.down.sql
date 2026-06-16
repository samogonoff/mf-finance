-- 0009 down — откат: удаляем добавленные колонки.

ALTER TABLE cost_data_cache DROP COLUMN IF EXISTS "Материал/техоперация/декор(признак)";
ALTER TABLE cost_data_cache DROP COLUMN IF EXISTS "Наименование";
ALTER TABLE cost_data_cache DROP COLUMN IF EXISTS "артикул материала";
ALTER TABLE cost_data_cache DROP COLUMN IF EXISTS "свойство1";
ALTER TABLE cost_data_cache DROP COLUMN IF EXISTS "свойство2";
ALTER TABLE cost_data_cache DROP COLUMN IF EXISTS "свойство3";
ALTER TABLE cost_data_cache DROP COLUMN IF EXISTS "Норма";
ALTER TABLE cost_data_cache DROP COLUMN IF EXISTS "цена материала, руб.";
ALTER TABLE cost_data_cache DROP COLUMN IF EXISTS "цена материала, USD.";
