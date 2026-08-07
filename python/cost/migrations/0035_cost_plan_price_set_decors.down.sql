-- Откат 0035 — декоры убираются из наборов цен.
--
-- ВНИМАНИЕ: строки декоров удаляются безвозвратно, иначе прежний уникальный
-- ключ (без row_kind) на них не наложится. Уже применённые к cost_data_cache
-- суммы декоров откат не отменяет — для возврата к данным источника нужен
-- полный рефреш кэша.

DELETE FROM cost_plan_price_set_rows WHERE row_kind = 'decor';

ALTER TABLE cost_plan_price_set_rows
    DROP CONSTRAINT IF EXISTS cost_plan_price_set_rows_kind_check;

ALTER TABLE cost_plan_price_set_rows
    DROP CONSTRAINT IF EXISTS cost_plan_price_set_rows_kind_key;

ALTER TABLE cost_plan_price_set_rows
    ADD CONSTRAINT "cost_plan_price_set_rows_set_id_Наименование_а_key"
        UNIQUE (set_id, "Наименование", "артикул материала",
                "свойство1", "свойство2", "свойство3");

ALTER TABLE cost_plan_price_set_rows
    DROP COLUMN IF EXISTS row_kind;
