-- 0055 — ПФКСС закупной готовой продукции по приходу (пожелание № 7, этап 2)
--
-- Фактическая себестоимость закупной продукции считается по инвойсу: цена
-- строки в валюте прихода плюс накладные расходы на партию, распределённые по
-- строкам пропорционально стоимости.
--
-- ВАЛЮТЫ И КУРСЫ (решение заказчика 04.09.2026)
-- ============================================
-- Приход и накладные приходят в разных валютах: юани, рос. рубли, евро,
-- тенге, узбекские сумы, бел. рубли. Поэтому:
--   · у инвойса выбирается ВАЛЮТА ПРИХОДА (валюта цены строк);
--   · у КАЖДОЙ статьи накладных — своя валюта и свой курс;
--   · все курсы берутся к БЕЛ. РУБЛЮ на дату прихода из НБ РБ
--     ([DWH].[dim].[valuta1].KURS_BANK; KURS — внутренний курс компании, не он);
--   · всё сводится к бел. рублю, распределение считается в бел. рублях;
--   · итоговая себестоимость пересчитывается обратно в доллары по курсу USD
--     к бел. рублю на ту же дату.
-- Первая версия (03.09) считала через доллар и держала три курса в шапке —
-- отброшена: она не давала задать валюту отдельной статье.
--
-- Накладные вынесены в отдельную таблицу, а не в колонки шапки: у каждой
-- статьи теперь три поля (сумма, валюта, курс), а пошлина задаётся суммой на
-- КАЖДЫЙ код ТН ВЭД (эталон — Excel заказчика Invoice_MF8693.xlsx, где куртки
-- и брюки имеют разные суммы пошлины по своим кодам).
--
-- Пять статей сворачиваются в три хранимые (решение заказчика 03.09.2026):
-- Логистика = транспорт + СВХ, Таможня = пошлина + таможенный сбор,
-- Сертификация — так же, как у КПСС из портала (0054), чтобы сравнивать план
-- и факт по статьям.
--
-- Инвойс живёт черновиком (draft), пока экономист правит строки и накладные;
-- «Применить» (applied) создаёт калькуляции ПФКСС в cost_manual_calc и записи
-- раскладки в cost_purchase_cost. Применённый инвойс не правится.

CREATE TABLE IF NOT EXISTS cost_purchase_invoice (
    id              BIGSERIAL PRIMARY KEY,
    number          TEXT        NOT NULL,
    invoice_date    DATE,
    arrival_date    DATE,                                  -- дата прихода: по ней берутся курсы
    contract        TEXT,
    supplier        TEXT,
    comment         TEXT,
    -- Валюта прихода (цены строк) и её курс к бел. рублю на дату прихода.
    currency        TEXT        NOT NULL DEFAULT 'USD',
    cur_rate        NUMERIC(18, 8) NOT NULL,               -- 1 ед. валюты в бел. руб.
    -- Курс доллара к бел. рублю на ту же дату: им итог пересчитывается в USD.
    usd_rate        NUMERIC(18, 8),
    rate_date       DATE,                                  -- дата курсов (обычно = arrival_date)
    rates_source    TEXT        NOT NULL DEFAULT 'nbrb' CHECK (rates_source IN ('nbrb', 'manual')),
    status          TEXT        NOT NULL DEFAULT 'draft' CHECK (status IN ('draft', 'applied')),
    totals          JSONB       NOT NULL DEFAULT '{}'::jsonb,
    created_by      TEXT        NOT NULL,
    created_at      TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_by      TEXT,
    updated_at      TIMESTAMPTZ NOT NULL DEFAULT now(),
    applied_by      TEXT,
    applied_at      TIMESTAMPTZ
);

-- Накладные расходы на партию: статья, сумма, её валюта и курс к бел. рублю.
-- kind: transport | svh | customs_fee | cert | duty. У duty заполняется hs_code
-- (код ТН ВЭД группы), у остальных он пустой.
CREATE TABLE IF NOT EXISTS cost_purchase_invoice_overhead (
    id              BIGSERIAL PRIMARY KEY,
    invoice_id      BIGINT      NOT NULL REFERENCES cost_purchase_invoice(id) ON DELETE CASCADE,
    kind            TEXT        NOT NULL CHECK (kind IN ('transport', 'svh', 'customs_fee', 'cert', 'duty')),
    hs_code         TEXT        NOT NULL DEFAULT '',
    amount          NUMERIC(18, 4) NOT NULL DEFAULT 0,
    currency        TEXT        NOT NULL DEFAULT 'BYN',
    rate            NUMERIC(18, 8) NOT NULL DEFAULT 1,     -- 1 ед. валюты в бел. руб.
    amount_byn      NUMERIC(18, 4) NOT NULL DEFAULT 0,     -- amount × rate, для сверки
    comment         TEXT,
    -- Одна статья может состоять из нескольких сумм в разных валютах
    -- (в эталоне транспорт — часть в рос. рублях, часть в долларах), поэтому
    -- уникальности по (invoice_id, kind, hs_code) нет.
    UNIQUE (invoice_id, kind, hs_code, currency, amount, id)
);

CREATE INDEX IF NOT EXISTS ix_purchase_overhead_invoice ON cost_purchase_invoice_overhead (invoice_id, kind);

CREATE TABLE IF NOT EXISTS cost_purchase_invoice_line (
    id              BIGSERIAL PRIMARY KEY,
    invoice_id      BIGINT      NOT NULL REFERENCES cost_purchase_invoice(id) ON DELETE CASCADE,
    line_no         INT         NOT NULL,
    -- Калькуляция, к которой относится приход (КПСС из портала); план и задание
    -- переходят в ПФКСС. Строка, добавленная руками без КПСС, — с пустым планом.
    model           TEXT        NOT NULL,
    articul         TEXT        NOT NULL,
    plan_id         TEXT        NOT NULL DEFAULT '',
    task_number     TEXT        NOT NULL DEFAULT '',
    name            TEXT,
    color           TEXT,
    hs_code         TEXT        NOT NULL DEFAULT '',        -- код ТН ВЭД (группа пошлины)
    qty             NUMERIC(14, 3) NOT NULL,
    unit_price_cur  NUMERIC(18, 4) NOT NULL,                -- цена в валюте прихода
    -- Результат распределения: на единицу в бел. рублях (основа) и в долларах.
    price_byn       NUMERIC(18, 4), sum_byn        NUMERIC(18, 2),
    logistics_byn   NUMERIC(18, 4), customs_byn    NUMERIC(18, 4),
    cert_byn        NUMERIC(18, 4), total_byn      NUMERIC(18, 4),
    total_usd       NUMERIC(18, 4),
    -- Что создано при применении.
    manual_calc_id  BIGINT,
    purchase_cost_id BIGINT,
    UNIQUE (invoice_id, line_no)
);

CREATE INDEX IF NOT EXISTS ix_purchase_invoice_line_key ON cost_purchase_invoice_line (model, articul, plan_id);
