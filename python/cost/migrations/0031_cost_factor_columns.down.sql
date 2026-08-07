-- Откат 0031. Без коэффициента _recalc_cost_buckets вернётся к схеме
-- «бакет = Норма × цена» и снова начнёт раздувать себестоимость на строках,
-- где источник считал бакет иначе (см. комментарий в up-миграции).

ALTER TABLE cost_calc_version_rows
    DROP COLUMN IF EXISTS cost_factor_rub,
    DROP COLUMN IF EXISTS cost_factor_usd;

ALTER TABLE cost_data_cache
    DROP COLUMN IF EXISTS cost_factor_rub,
    DROP COLUMN IF EXISTS cost_factor_usd;
