-- 0053 — согласование цен по постановлению № 713 (пожелание № 8)
--
-- В Беларуси действует регулирование цен (постановление Совмина № 713). При
-- выпуске новинки или повышении цены на регулируемый ассортимент нужно
-- согласование государственных органов (исполкома). Заказчик (03.09.2026)
-- попросил поля по калькуляции:
--   · требуется согласование (да/нет);
--   · вид: повышение цены / новинка;
--   · артикул аналога — из справочника Gpartner S_MODELI, с поиском;
--   · розничная и оптовая цена аналога — подтягиваются по тем же правилам, что
--     цены артикулов в главной таблице (последняя калькуляция → утверждённая
--     цена в DWH → плановая из S_MODELI);
--   · решение исполкома: согласовано / не согласовано.
--
-- ГРАНУЛЯРНОСТЬ
-- =============
-- Ключ — модель + артикул, без признака калькуляции, плана и задания (решение
-- заказчика 03.09.2026: «карточка должна быть привязана вообще к
-- модели-артикулу»). Исполком согласует цену изделия, а не конкретную
-- калькуляцию: одно решение действует на все планы и этапы этой пары. Это
-- крупнее, чем ключ цены (4 поля) и статуса ПЭО (5 полей) — не «уточнять».
--
-- Цены аналога хранятся снимком на момент выбора вместе с источником: решение
-- исполкома принималось при этих цифрах, и если цена аналога потом поменяется,
-- в карточке останется то, что показывали в исполком. Кнопка «Обновить цены»
-- перечитывает их явно.

CREATE TABLE IF NOT EXISTS cost_reg713 (
    id                 BIGSERIAL PRIMARY KEY,
    model              TEXT        NOT NULL,
    articul            TEXT        NOT NULL,
    required           BOOLEAN     NOT NULL DEFAULT FALSE,
    -- Когда изделие отметили «требуется согласование» (последний раз): дата
    -- создания отметки для отчёта. Снятие галочки поле не чистит — видно, что
    -- отмечали; повторная установка переписывает.
    required_at        TIMESTAMPTZ,
    -- price_increase — повышение цены; novelty — новинка. NULL — не выбрано.
    kind               TEXT        CHECK (kind IN ('price_increase', 'novelty')),
    analog_model       TEXT,
    analog_articul     TEXT,
    analog_name        TEXT,
    analog_retail      NUMERIC(14, 2),
    analog_wholesale   NUMERIC(14, 2),
    -- Откуда взяты цены аналога: dwh (утверждённая цена), calc (последняя
    -- калькуляция в CostHistory), gpartner (плановая из S_MODELI), manual.
    analog_price_source TEXT,
    analog_price_at    TIMESTAMPTZ,
    -- Решение исполкома. NULL — ещё не рассмотрено.
    decision           TEXT        CHECK (decision IN ('approved', 'rejected')),
    decision_at        TIMESTAMPTZ,
    decision_doc       TEXT,       -- номер/дата письма исполкома, если есть
    comment            TEXT,
    updated_by         TEXT        NOT NULL,
    created_at         TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at         TIMESTAMPTZ NOT NULL DEFAULT now(),
    CONSTRAINT uq_reg713_key UNIQUE (model, articul)
);

CREATE INDEX IF NOT EXISTS ix_reg713_required ON cost_reg713 (required) WHERE required;

-- История изменений карточки: кто, когда, что было и что стало. Решение
-- исполкома — юридически значимый факт, его переписывание должно быть видно.
CREATE TABLE IF NOT EXISTS cost_reg713_log (
    id          BIGSERIAL PRIMARY KEY,
    reg713_id   BIGINT      NOT NULL REFERENCES cost_reg713(id) ON DELETE CASCADE,
    changed_by  TEXT        NOT NULL,
    changed_at  TIMESTAMPTZ NOT NULL DEFAULT now(),
    before      JSONB,
    after       JSONB       NOT NULL
);

-- Право вести карточку 713 — всем рабочим ролям раздела: бренд-менеджер ставит
-- цену, ПЭО ведёт согласование с исполкомом, калькулятор первым видит новинку
-- (решение заказчика 03.09.2026: «калькулятору право нужно, и БМ, и ПЭО»),
-- плюс Full Admin. Без права остаётся только «Просмотр».
UPDATE cost_roles
   SET permissions = permissions || '["cost:reg713"]'::jsonb,
       updated_at = now()
 WHERE name IN ('Бренд-менеджер', 'ПЭО', 'Калькулятор', 'Full Admin')
   AND NOT permissions @> '["cost:reg713"]'::jsonb;
