-- 0032 — публикация утверждённого плана во внешний контур (Budgeting).
-- ТЗ МП §9.3 / Розница §7.2: при утверждении на 1.4 значения транзакционно и
-- идемпотентно уходят в приёмники семейства FormToLoad*. Приёмники без первичного
-- ключа: логическая уникальность — Параметр + Страна + КодЦФО + КодPL + Дата.
--
-- Открытые вопросы BI (§12: какой «Параметр» писать, агрегат по группе 250/480 или
-- детализация по площадкам, кто заполняет BYN-пару) вынесены В ДАННЫЕ — таблица
-- publish_mapping. Ответы аналитика заполняются строками, без релиза кода.
-- До ответов маппинг сидируется выключенным (enabled=FALSE) и работает только
-- режим dry-run: сервис считает, что и куда ушло бы, и сверяет с приёмником.

CREATE TABLE IF NOT EXISTS publish_mapping (
    id            BIGSERIAL PRIMARY KEY,
    form_code     TEXT NOT NULL,
    block_type    TEXT NOT NULL DEFAULT '',      -- строка формы (пусто = все)
    country       TEXT NOT NULL DEFAULT '',      -- пусто = любая страна
    param_name    TEXT NOT NULL,                 -- 'ПРОДАЖИ' | 'ПРОДАЖИ с НДС с самовыв_' | …
    code_pl       INT,                           -- КодPL приёмника (несёт показатель И канал/страну)
    target_table  TEXT NOT NULL,                 -- Budgeting.dbo.VFORMTOLOADTAKTTARGET (нац.) / FormToLoaTaktTarget (BYN)
    target_currency TEXT NOT NULL DEFAULT '',    -- '' = нац. валюта карточки, 'BYN' = BYN-пара
    aggregate     BOOLEAN NOT NULL DEFAULT FALSE,-- писать агрегат по группе вместо площадок
    aggregate_cfo INT,                           -- 250 (large) / 480 (small) при aggregate
    enabled       BOOLEAN NOT NULL DEFAULT FALSE,
    note          TEXT NOT NULL DEFAULT '',
    updated_by    BIGINT REFERENCES users(id),
    updated_at    TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    UNIQUE (form_code, block_type, country, param_name, target_table, target_currency)
);

-- Журнал публикаций: и dry-run, и запись. diff = Σ введённое − Σ прочитанное из
-- приёмника (контроль записи ТЗ МП §9.3 «= 0 по каждой группе и площадке»).
CREATE TABLE IF NOT EXISTS publish_log (
    id              BIGSERIAL PRIMARY KEY,
    card_id         BIGINT REFERENCES form_card(id) ON DELETE CASCADE,
    version_no      INT  NOT NULL DEFAULT 0,
    target          TEXT NOT NULL DEFAULT '',
    mode            TEXT NOT NULL DEFAULT 'dry_run', -- dry_run|write
    idempotency_key TEXT NOT NULL DEFAULT '',        -- card:version:target — повтор не удваивает
    rows_total      INT  NOT NULL DEFAULT 0,
    rows_written    INT  NOT NULL DEFAULT 0,
    sum_input       NUMERIC(20,4) NOT NULL DEFAULT 0,
    sum_target      NUMERIC(20,4) NOT NULL DEFAULT 0,
    diff            NUMERIC(20,4) NOT NULL DEFAULT 0,
    status          TEXT NOT NULL DEFAULT 'ok',      -- ok|failed|blocked
    error_text      TEXT NOT NULL DEFAULT '',
    report          JSONB NOT NULL DEFAULT '{}',     -- построчный отчёт dry-run
    started_at      TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    finished_at     TIMESTAMPTZ,
    created_by      BIGINT REFERENCES users(id)
);
CREATE INDEX IF NOT EXISTS idx_publish_log_card ON publish_log(card_id, started_at DESC);

-- Заготовки маппинга (выключены до ответов BI, §12). Значения code_pl взяты из
-- ответа A4: магазин ЦФО 100 (BY) → ПРОДАЖИ = 1001; ЦФО 105 (RU) → 2011.
-- ВНИМАНИЕ: код несёт показатель И канал/страну, поэтому финальный набор берётся
-- из справочника Budgeting.dbo.CodePL, а не выводится по шаблону.
INSERT INTO publish_mapping
    (form_code, block_type, country, param_name, code_pl, target_table, target_currency, aggregate, aggregate_cfo, enabled, note)
VALUES
    ('TPL-TO-RETAIL', 'sales_plan', 'BY', 'ПРОДАЖИ', 1001, 'Budgeting.dbo.VFORMTOLOADTAKTTARGET', '', FALSE, NULL, FALSE,
     'ожидает подтверждения BI (§12 п.2, п.7): нужен ли параметр «с самовыв_», кто пишет BYN-пару'),
    ('TPL-TO-RETAIL', 'sales_plan', 'RU', 'ПРОДАЖИ', 2011, 'Budgeting.dbo.VFORMTOLOADTAKTTARGET', '', FALSE, NULL, FALSE,
     'код по срезу A4 (ЦФО 105 RU); подтвердить по справочнику CodePL'),
    ('TPL-MP', 'sales_manager_price', '', 'ПРОДАЖИ с НДС_СПП', NULL, 'Budgeting.dbo.VFORMTOLOADTAKTTARGET', '', FALSE, NULL, FALSE,
     'ожидает решения §12 п.1: какой Параметр для МП (ОТГРУЗКИ С НДС / ПРОДАЖИ с НДС с самовыв_ / ПРОДАЖИ с НДС_СПП / ПРОДАЖИ_СПП)'),
    ('TPL-MP', 'sales_platform_price', '', 'ПРОДАЖИ_СПП', NULL, 'Budgeting.dbo.VFORMTOLOADTAKTTARGET', '', FALSE, NULL, FALSE,
     'ожидает решения §12 п.2: детализация по площадкам или агрегат 250/480')
ON CONFLICT (form_code, block_type, country, param_name, target_table, target_currency) DO NOTHING;
