-- 0039 — история цен для BI + переоткрытие калькуляции после записи в DWH
--
-- Две связанные вещи, решение заказчика 25.08.2026.
--
-- 1. `cost_price_history` — append-история установленных цен.
--
-- Полная история цен сейчас существует ТОЛЬКО в OLAP
-- (FinSandBox.dbo.CostHistory_Changes): запись туда идёт INSERT'ом, а чтение
-- берёт последнюю строку на калькуляцию через ROW_NUMBER. Локальная копия
-- `cost_price_changes_audit` историей не является — `sync_audit_from_olap`
-- перезаписывает её только записями rn = 1, то есть по одной на ключ.
-- Витрина `cost_calc_mv` и датасеты Superset смотрят на `cost_data_cache` и
-- истории цен не содержат вовсе.
--
-- Поэтому дашборд «история изменения цен» строить не на чем. Эта таблица —
-- локальный append-лог: та же строка, что уходит в DWH, но без затирания.
-- Superset к postgres-cost уже подключён, так что новых коннектов и доступов к
-- OLAP для BI не потребуется. `cost_price_changes_audit` намеренно не трогаем:
-- на нём висят блокировки и значок «записано в DWH», там нужна ровно одна
-- актуальная запись на ключ.
--
-- 2. `cost_dwh_reopen` — разрешение снова править калькуляцию, уже уехавшую в DWH.
--
-- После записи в DWH строка блокируется (`_has_audit`), и второй цикл
-- «правка → согласование ПЭО → установка цен» пройти нельзя. Отмену записи
-- делать НЕ будем: удаление строк уничтожает основание, по которому цена
-- действовала, и всё равно не откатывает прейскурант в учётной системе. Вместо
-- отмены — переоткрытие: блокировка снимается, а новая установка цен добавляет
-- в DWH ещё одну строку. В учётной системе более новый прейскурант перекрывает
-- старый (подтверждено заказчиком), поэтому отдельного сторно не требуется.
--
-- Разрешение самоистекающее: оно действует, пока `reopened_at` новее последней
-- записи цен по этому ключу. Как только цены установлены заново, блокировка
-- возвращается сама — вычищать таблицу руками не нужно.

CREATE TABLE IF NOT EXISTS cost_price_history (
    id              BIGSERIAL PRIMARY KEY,
    model           TEXT NOT NULL,
    articul         TEXT NOT NULL,
    calc_sign       TEXT NOT NULL DEFAULT '',
    plan_id         TEXT NOT NULL DEFAULT '',
    task_number     TEXT NOT NULL DEFAULT '',
    price_level     TEXT,
    retail_rub      NUMERIC(18, 4),
    wholesale_rub   NUMERIC(18, 4),
    price_rf        NUMERIC(18, 4),
    price_kz        NUMERIC(18, 4),
    price_uz        NUMERIC(18, 4),
    mp_price_rub    NUMERIC(18, 4),
    cost_rub        NUMERIC(18, 4),
    cost_usd        NUMERIC(18, 4),
    comment         TEXT,
    -- Кто ввёл цену и кто утвердил — как в CostHistory_Changes.
    changed_by      TEXT,
    approved_by     TEXT,
    approved_at     TIMESTAMPTZ NOT NULL DEFAULT now(),
    -- Признак того, что запись появилась после переоткрытия: по нему на дашборде
    -- видно исправления, а не только первичную установку цен.
    is_correction   BOOLEAN NOT NULL DEFAULT FALSE,
    -- Откуда пришла строка: 'app' — записана этим приложением при установке
    -- цен, 'olap_backfill' — разовый перенос ранее существовавших записей из
    -- CostHistory_Changes, чтобы у дашборда была предыстория.
    source          TEXT NOT NULL DEFAULT 'app',
    created_at      TIMESTAMPTZ NOT NULL DEFAULT now()
);

-- Основной разрез дашборда — динамика по калькуляции.
CREATE INDEX IF NOT EXISTS cost_price_history_key_idx
    ON cost_price_history (model, articul, calc_sign, plan_id, approved_at DESC);
CREATE INDEX IF NOT EXISTS cost_price_history_approved_at_idx
    ON cost_price_history (approved_at DESC);
CREATE INDEX IF NOT EXISTS cost_price_history_plan_idx
    ON cost_price_history (plan_id);

-- Защита от повторного бэкфилла: одна и та же запись OLAP не должна приехать
-- дважды. Для строк приложения ограничение не мешает — у них разный approved_at.
CREATE UNIQUE INDEX IF NOT EXISTS cost_price_history_backfill_uniq
    ON cost_price_history (model, articul, calc_sign, plan_id, approved_at, source);


CREATE TABLE IF NOT EXISTS cost_dwh_reopen (
    id              BIGSERIAL PRIMARY KEY,
    model           TEXT NOT NULL,
    articul         TEXT NOT NULL,
    calc_sign       TEXT NOT NULL DEFAULT '',
    plan_id         TEXT NOT NULL DEFAULT '',
    reopened_at     TIMESTAMPTZ NOT NULL DEFAULT now(),
    reopened_by     TEXT NOT NULL,
    -- Причина обязательна: операция нежелательная, и через месяц никто не
    -- вспомнит, почему цену переставляли.
    reason          TEXT NOT NULL,
    -- Кто и когда отозвал разрешение, не дожидаясь новой установки цен.
    revoked_at      TIMESTAMPTZ,
    revoked_by      TEXT
);

-- Ключ ищется на каждой отдаче главной таблицы — без индекса это seq scan на
-- каждый запрос.
CREATE INDEX IF NOT EXISTS cost_dwh_reopen_key_idx
    ON cost_dwh_reopen (model, articul, calc_sign, plan_id, reopened_at DESC);
