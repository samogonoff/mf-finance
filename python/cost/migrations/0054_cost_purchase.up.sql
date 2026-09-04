-- 0054 — закупная готовая продукция: детальная себестоимость и журнал импорта
-- (пожелания № 6 и № 7, объединены заказчиком 03.09.2026)
--
-- Ассортимент, закупаемый у сторонних производителей, считается на КПСС и
-- ПФКСС. КПСС приходит из портала БМ (OLAP, база mfportal, таблицы bm_chl_*):
-- строки планов с типом производства «заказ готовой», у которых в
-- calculation_json есть цена размещения в USD и снимок ставок (курс, пошлина %,
-- транспорт %, сертификация $, тесты $). ПФКСС считается в приложении по
-- приходу (инвойс + накладные), см. следующий этап.
--
-- ГДЕ ЛЕЖАТ САМИ КАЛЬКУЛЯЦИИ
-- ==========================
-- Строки калькуляций импорт создаёт в cost_manual_calc (структура
-- cost_data_cache, читаются через cost_data_all вместе с кэшем) — так же, как
-- копии калькуляций с другим признаком (0043). В CostHistory этих моделей нет
-- вовсе (проверено 03.09.2026: 0 из 164), поэтому создавать их больше негде.
-- Импортированные строки отличаются source_calc_sign = 'ПОРТАЛ'.
--
-- ЗАЧЕМ ОТДЕЛЬНАЯ ТАБЛИЦА ДЕТАЛИЗАЦИИ
-- ===================================
-- Решение заказчика: в главную таблицу отдаётся только ИТОГ себестоимости —
-- в поле «Основные материалы», остальные статьи пустые. А раскладка (цена,
-- курс, логистика, таможня, сертификация) хранится здесь, одинаково для КПСС
-- (из портала) и ПФКСС (из инвойса), чтобы сравнивать план и факт по каждой
-- статье: «за счёт чего уложились в плановую себестоимость или нет».
-- Три статьи сверх цены: Логистика (= транспорт + СВХ), Таможня (= пошлина +
-- таможенный сбор), Сертификация (= сертификация + тесты) — так свёрнуты пять
-- строк накладных из Excel заказчика (Invoice_MF8693.xlsx).

CREATE TABLE IF NOT EXISTS cost_purchase_cost (
    id              BIGSERIAL PRIMARY KEY,
    -- Ключ калькуляции — как у статуса ПЭО: с заданием.
    model           TEXT        NOT NULL,
    articul         TEXT        NOT NULL,
    calc_sign       TEXT        NOT NULL,
    plan_id         TEXT        NOT NULL DEFAULT '',
    task_number     TEXT        NOT NULL DEFAULT '',
    -- Строка cost_manual_calc, в которой лежит сама калькуляция.
    manual_calc_id  BIGINT,
    -- portal — КПСС из mfportal; invoice — ПФКСС из модалки прихода.
    source          TEXT        NOT NULL CHECK (source IN ('portal', 'invoice')),
    portal_item_id  BIGINT,     -- bm_chl_checklist_items.id
    invoice_line_id BIGINT,     -- строка инвойса (этап 2)
    -- Валюта и курсы на момент расчёта. Все курсы — к БЕЛ. РУБЛЮ, НБ РБ на дату
    -- (решение заказчика 04.09.2026): цена и накладные сводятся к бел. рублю,
    -- итог пересчитывается обратно в доллары по usd_to_byn той же даты.
    currency        TEXT,                       -- валюта цены строки (USD, CNY, RUB…)
    unit_price_cur  NUMERIC(18, 4),             -- цена за единицу в валюте
    cur_to_byn      NUMERIC(18, 8),             -- 1 ед. валюты в бел. руб.
    usd_to_byn      NUMERIC(18, 8),             -- доллар → бел. руб. (обратный пересчёт итога)
    rate_date       DATE,                       -- дата курсов
    qty             NUMERIC(14, 3),             -- количество (задание / приход)
    -- Раскладка на единицу в долларах и в бел. рублях.
    price_usd       NUMERIC(14, 4), price_byn       NUMERIC(14, 4),
    logistics_usd   NUMERIC(14, 4), logistics_byn   NUMERIC(14, 4),
    customs_usd     NUMERIC(14, 4), customs_byn     NUMERIC(14, 4),
    cert_usd        NUMERIC(14, 4), cert_byn        NUMERIC(14, 4),
    total_usd       NUMERIC(14, 4), total_byn       NUMERIC(14, 4),
    -- Цены продажи, пришедшие вместе с расчётом (розница портала; опт — по уровню).
    retail_byn      NUMERIC(14, 2),
    wholesale_byn   NUMERIC(14, 2),
    -- Полный снимок входных данных: JSON портала или строка инвойса с долями.
    snapshot        JSONB       NOT NULL DEFAULT '{}'::jsonb,
    created_by      TEXT        NOT NULL,
    created_at      TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at      TIMESTAMPTZ NOT NULL DEFAULT now(),
    CONSTRAINT uq_purchase_cost_key UNIQUE (model, articul, calc_sign, plan_id, task_number)
);

CREATE INDEX IF NOT EXISTS ix_purchase_cost_pair ON cost_purchase_cost (model, articul);
CREATE INDEX IF NOT EXISTS ix_purchase_cost_portal_item ON cost_purchase_cost (portal_item_id);

-- Журнал запусков импорта из портала: что прочитали, что создали/обновили,
-- что пропустили и почему (нет расчёта в JSON, нет артикула/задания).
CREATE TABLE IF NOT EXISTS cost_purchase_import_run (
    id              BIGSERIAL PRIMARY KEY,
    started_at      TIMESTAMPTZ NOT NULL DEFAULT now(),
    finished_at     TIMESTAMPTZ,
    started_by      TEXT        NOT NULL,
    status          TEXT        NOT NULL DEFAULT 'running',  -- running | done | error
    plans           INT         NOT NULL DEFAULT 0,
    items           INT         NOT NULL DEFAULT 0,
    created_rows    INT         NOT NULL DEFAULT 0,
    updated_rows    INT         NOT NULL DEFAULT 0,
    skipped         INT         NOT NULL DEFAULT 0,
    skipped_reasons JSONB       NOT NULL DEFAULT '{}'::jsonb,
    error           TEXT
);
