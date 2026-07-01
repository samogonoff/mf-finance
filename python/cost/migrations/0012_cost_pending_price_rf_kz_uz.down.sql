-- 0012 down — remove 3 price columns from cost_price_pending
ALTER TABLE cost_price_pending
    DROP COLUMN IF EXISTS "Цена РФ",
    DROP COLUMN IF EXISTS "Цена КЗ",
    DROP COLUMN IF EXISTS "Цена УЗ";
