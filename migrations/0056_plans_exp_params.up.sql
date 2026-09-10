-- Параметры периода формы этапа 1.5 (ТЗ §6.1).
--
-- «Удельный вес и порог берутся из справочника, задаются на страну с
-- возможностью переопределения по магазину. Оба параметра выводятся в шапке
-- формы и доступны для изменения на период. Кто и когда изменил параметр —
-- сохраняется».
--
-- Поэтому параметр хранится НА ЭКЗЕМПЛЯР ФОРМЫ, а не в справочнике: справочник
-- задаёт значение по умолчанию, а период может его пережить. Без этого нельзя
-- воспроизвести утверждённые суммы: расчёт §6.1 зависит от порога, и меняющийся
-- справочник задним числом изменил бы уже согласованный план.

CREATE TABLE IF NOT EXISTS exp_param (
    id          BIGSERIAL PRIMARY KEY,
    instance_id BIGINT NOT NULL REFERENCES exp_instance(id) ON DELETE CASCADE,
    -- payroll_share (удельный вес) | payroll_cap (порог базы начисления)
    param_code  TEXT NOT NULL,
    -- country — значение на всю страну формы, cfo — переопределение по объекту.
    scope_kind  TEXT NOT NULL DEFAULT 'country',
    scope_value TEXT NOT NULL DEFAULT '',
    value       NUMERIC(18,6) NOT NULL,
    updated_by  BIGINT REFERENCES users(id),
    updated_at  TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    UNIQUE (instance_id, param_code, scope_kind, scope_value)
);
CREATE INDEX IF NOT EXISTS exp_param_inst_idx ON exp_param (instance_id, param_code);

-- Журнал правок параметров: ТЗ §6.1 требует хранить, кто и когда менял. Отдельная
-- таблица, потому что в самой строке остаётся только последнее значение.
CREATE TABLE IF NOT EXISTS exp_param_version (
    id          BIGSERIAL PRIMARY KEY,
    instance_id BIGINT NOT NULL REFERENCES exp_instance(id) ON DELETE CASCADE,
    param_code  TEXT NOT NULL,
    scope_kind  TEXT NOT NULL,
    scope_value TEXT NOT NULL,
    value_old   NUMERIC(18,6),
    value_new   NUMERIC(18,6),
    changed_by  BIGINT REFERENCES users(id),
    changed_at  TIMESTAMPTZ NOT NULL DEFAULT NOW()
);
CREATE INDEX IF NOT EXISTS exp_param_version_idx ON exp_param_version (instance_id, changed_at DESC);
