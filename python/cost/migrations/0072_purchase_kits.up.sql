-- 0072 · Комплекты в приходе закупной продукции (Б24 668119, Луценко Т.А.)
--
-- План закупной содержит комплекты: задание в Лисе выдаётся на КОМПЛЕКТ, а в
-- таможенной декларации стоят СОСТАВЛЯЮЩИЕ — отдельные строки со своими
-- артикулами, ценами и кодами ТН ВЭД; саму декларацию комплект не помечает.
-- Решение заказчика 29.10.2026: ПФКСС на составляющие НЕ создаётся — только на
-- комплект; количество комплекта вводится вручную; группировку строк под
-- комплект делает оператор в окне прихода (docs/purchase_kits.md).
--
-- cost_purchase_invoice_kit — комплект инвойса: модель/артикул комплекта,
-- план и задание (автозаполняются из КПСС-основы комплекта, правятся вручную),
-- количество вручную. После применения сюда пишутся id созданной ПФКСС
-- (manual_calc_id) и её раскладки (purchase_cost_id); у строк-составляющих
-- manual_calc_id/purchase_cost_id остаются NULL.
-- cost_purchase_invoice_line.kit_id — привязка строки-составляющей к комплекту:
-- строка входит максимум в один комплект, комплект без строк применить нельзя.
-- Уникальность защищает от двух комплектов с одним ключом ПФКСС в инвойсе —
-- они дрались бы за одну калькуляцию.

CREATE TABLE IF NOT EXISTS cost_purchase_invoice_kit (
    id               BIGSERIAL PRIMARY KEY,
    invoice_id       BIGINT      NOT NULL REFERENCES cost_purchase_invoice (id) ON DELETE CASCADE,
    line_no          INTEGER     NOT NULL DEFAULT 0,
    model            TEXT        NOT NULL DEFAULT '',
    articul          TEXT        NOT NULL DEFAULT '',
    plan_id          TEXT        NOT NULL DEFAULT '',
    task_number      TEXT        NOT NULL DEFAULT '',
    name             TEXT        NOT NULL DEFAULT '',
    qty              NUMERIC(14, 3) NOT NULL,
    manual_calc_id   BIGINT,
    purchase_cost_id BIGINT,
    created_at       TIMESTAMPTZ NOT NULL DEFAULT now(),
    UNIQUE (invoice_id, line_no)
);

CREATE INDEX IF NOT EXISTS ix_purchase_invoice_kit_invoice
    ON cost_purchase_invoice_kit (invoice_id);

ALTER TABLE cost_purchase_invoice_line
    ADD COLUMN IF NOT EXISTS kit_id BIGINT REFERENCES cost_purchase_invoice_kit (id) ON DELETE SET NULL;

CREATE INDEX IF NOT EXISTS ix_purchase_invoice_line_kit
    ON cost_purchase_invoice_line (kit_id);
