-- Откат 0034 — колонка цвета убирается.
-- Данные восстановимы полным рефрешем кэша из источника, ничего уникального
-- в этой колонке не хранится.

ALTER TABLE cost_calc_version_rows
    DROP COLUMN IF EXISTS color;

ALTER TABLE cost_data_cache
    DROP COLUMN IF EXISTS color;
