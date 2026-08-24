-- 0035 — форма «Тактический план продаж, Розница» (TPL-TO-RETAIL), модель данных ТЗ §10.
--
-- Почему отдельный слой таблиц, а не pl_metric (как у МП): у розницы единица ввода —
-- (МАГАЗИН × МЕСЯЦ) на весь плановый период, а не (ЦФО × статья) одного месяца.
-- ТЗ §3: «ввод — только месячные ячейки плана продаж»; ТЗ §4.4 «Ожидание года»
-- требует читать все 12 месяцев года сразу. В pl_metric такой доступ означал бы
-- 375 магазинов × 12 месяцев строк со статьёй-заглушкой и без снапшота атрибутов
-- магазина, который ТЗ §2 требует фиксировать при создании экземпляра.
--
-- Оболочка процесса (статусы, согласования, версии, публикация, комментарии,
-- аудит) НЕ дублируется: она в form_card/card_approval/card_version (0031, 0032).
-- tp_instance привязан к карточке 1:1 — карточка и есть «экземпляр = (страна, период)».

-- Экземпляр формы: страна + период (ТЗ §1). Одно реальное ЮЛ на страну (§1, V-08),
-- своя национальная валюта (§1, V-11) — денормализованы из карточки, чтобы
-- утверждённый экземпляр не «поехал» при правке реестра форм.
CREATE TABLE IF NOT EXISTS tp_instance (
    id           BIGSERIAL PRIMARY KEY,
    card_id      BIGINT NOT NULL REFERENCES form_card(id) ON DELETE CASCADE,
    country      TEXT NOT NULL,
    period_year  INT  NOT NULL,
    period_month INT  NOT NULL,
    currency     TEXT NOT NULL,                    -- нац. валюта ввода (V-11)
    legal_entity TEXT NOT NULL,                    -- единственное ЮЛ страны (V-08)
    created_by   BIGINT REFERENCES users(id),
    created_at   TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at   TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    UNIQUE (card_id)
);
CREATE INDEX IF NOT EXISTS idx_tp_instance_period ON tp_instance(country, period_year, period_month);

-- Строка формы = магазин + СНАПШОТ его атрибутов из справочника (ТЗ §2:
-- «при создании экземпляра — снапшот атрибутов в строку»). Снапшот нужен, чтобы
-- утверждённый план читался теми атрибутами, при которых его согласовали:
-- LFL-статус, РМ и категория магазина в справочнике меняются между периодами.
--
-- Ключ строки — code_cfo (V-04, обязателен и уникален внутри экземпляра).
-- klient_id ОПЦИОНАЛЕН: у 10 новых магазинов РБ его ещё нет (в источнике статус
-- «ххх»), поэтому NOT NULL DEFAULT '' вместо NULL — так уникальность непустых
-- значений (V-04) выражается партиальным индексом, а не хитрым NULL-семантикой.
CREATE TABLE IF NOT EXISTS tp_row (
    id             BIGSERIAL PRIMARY KEY,
    instance_id    BIGINT NOT NULL REFERENCES tp_instance(id) ON DELETE CASCADE,
    code_cfo       INT  NOT NULL,
    klient_id      TEXT NOT NULL DEFAULT '',
    -- Видимые атрибуты (ТЗ §2): название, город (GroupCFO2), LFL-статус, тип, категория, РМ.
    name_cfo       TEXT NOT NULL DEFAULT '',
    city           TEXT NOT NULL DEFAULT '',
    lfl_status     TEXT NOT NULL DEFAULT '',
    store_type     TEXT NOT NULL DEFAULT '',
    category       TEXT NOT NULL DEFAULT '',
    reg_manager    TEXT NOT NULL DEFAULT '',
    -- Скрытые атрибуты (ТЗ §2): метраж, даты открытия/закрытия, ЮЛ, KLIENT_ID,
    -- CodeFOX, менеджер, стадия, PLAnalyticCFO1.
    legal_entity   TEXT NOT NULL DEFAULT '',        -- CompanyMF
    ploschad       NUMERIC(14,2) NOT NULL DEFAULT 0,
    date_open      DATE,
    date_close     DATE,
    stage_of_store TEXT NOT NULL DEFAULT '',
    code_fox       TEXT NOT NULL DEFAULT '',
    manager        TEXT NOT NULL DEFAULT '',
    pl_analytic    TEXT NOT NULL DEFAULT '',        -- PLAnalyticCFO1
    attrs          JSONB NOT NULL DEFAULT '{}',     -- остальные колонки источника «как есть»
    -- Комментарий к строке (ТЗ §3) — единственное текстовое поле ввода. Обязателен
    -- при сработавшем предупреждении (V-07).
    comment        TEXT NOT NULL DEFAULT '',
    -- row_version растёт при каждой пересинхронизации атрибутов из справочника:
    -- по нему видно, что строка «догнала» новую версию НСИ (ТЗ §10).
    row_version    INT  NOT NULL DEFAULT 1,
    created_at     TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at     TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    UNIQUE (instance_id, code_cfo)
);
CREATE INDEX IF NOT EXISTS idx_tp_row_instance ON tp_row(instance_id);
-- V-04: KLIENT_ID, если заполнен, тоже уникален внутри экземпляра.
CREATE UNIQUE INDEX IF NOT EXISTS idx_tp_row_klient
    ON tp_row(instance_id, klient_id) WHERE klient_id <> '';

-- Ячейка плана: строка × месяц (ТЗ §3, §10). amount NULL НЕ используется —
-- «пусто ≠ 0» (V-01) выражается ОТСУТСТВИЕМ строки: для нуля нужен явный ввод,
-- то есть строка с amount=0. Так V-01 проверяется наличием записи, а не сравнением
-- с нулём, и «пусто» невозможно спутать с «явный ноль».
CREATE TABLE IF NOT EXISTS tp_value (
    id           BIGSERIAL PRIMARY KEY,
    row_id       BIGINT NOT NULL REFERENCES tp_row(id) ON DELETE CASCADE,
    period_year  INT  NOT NULL,
    period_month INT  NOT NULL,
    -- metric: sales | payroll | rent. ОТСТУПЛЕНИЕ от модели ТЗ §10, где tp_value —
    -- одно число на (строка × месяц). Причина: §5 требует массовых операций «ФОТ от
    -- продаж» и «Аренда», а их формулы читают ПЛАН ПРОДАЖ как вход
    -- (ФОТ = MIN(План_продаж; База × Порог) × Уд.вес). В одной ячейке продажи и ФОТ
    -- сосуществовать не могут, а значения source='payroll_share'/'rent_carryover'
    -- перечислены в §10 как допустимые — значит ФОТ и аренда всё-таки хранятся.
    -- Ввод пользователя (§3) и все валидации V-01..V-11 относятся к metric='sales';
    -- payroll/rent — производные статьи, считаемые массовой операцией.
    metric       TEXT NOT NULL DEFAULT 'sales',
    amount       NUMERIC(18,2) NOT NULL DEFAULT 0,
    -- source (ТЗ §5): manual | from_strategy | from_prev_period | index_applied |
    -- payroll_share | rent_carryover | distributed | import.
    -- Ячейка с source='manual' НЕ перезатирается повторным запуском массовой
    -- операции — сброс защиты только явным действием.
    source       TEXT NOT NULL DEFAULT 'manual',
    -- Пояснение массовой операции: «ФОТ ограничен порогом 106 %» (ТЗ §5, ФОТ),
    -- «только факт предыдущего периода» (ТЗ §5, Аренда).
    note         TEXT NOT NULL DEFAULT '',
    updated_by   BIGINT REFERENCES users(id),
    updated_at   TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    UNIQUE (row_id, metric, period_year, period_month)
);
CREATE INDEX IF NOT EXISTS idx_tp_value_row ON tp_value(row_id, period_year, period_month);

-- История значений ячейки. Общая оболочка версионирует форму ЦЕЛИКОМ (card_version),
-- но ТЗ §10 требует ещё и построчной истории: «кто и когда поменял эту ячейку» —
-- без неё нельзя объяснить расхождение между двумя утверждёнными версиями.
CREATE TABLE IF NOT EXISTS tp_value_version (
    id           BIGSERIAL PRIMARY KEY,
    row_id       BIGINT NOT NULL REFERENCES tp_row(id) ON DELETE CASCADE,
    period_year  INT  NOT NULL,
    period_month INT  NOT NULL,
    metric       TEXT NOT NULL DEFAULT 'sales',
    amount_old   NUMERIC(18,2),                    -- NULL = ячейка была пустой
    amount_new   NUMERIC(18,2),
    source_old   TEXT NOT NULL DEFAULT '',
    source_new   TEXT NOT NULL DEFAULT '',
    op           TEXT NOT NULL DEFAULT '',         -- ручная правка или код массовой операции
    changed_by   BIGINT REFERENCES users(id),
    changed_at   TIMESTAMPTZ NOT NULL DEFAULT NOW()
);
CREATE INDEX IF NOT EXISTS idx_tp_value_version_row ON tp_value_version(row_id, changed_at);

-- Переопределение LFL-статуса на период (ТЗ §3): доступно ТОЛЬКО финансисту,
-- требует причины и НЕ перезатирает справочник — поэтому отдельная таблица, а не
-- UPDATE tp_row.lfl_status: в строке остаётся статус из НСИ, здесь — решение
-- финансиста на этот период.
CREATE TABLE IF NOT EXISTS tp_lfl_override (
    id          BIGSERIAL PRIMARY KEY,
    instance_id BIGINT NOT NULL REFERENCES tp_instance(id) ON DELETE CASCADE,
    code_cfo    INT  NOT NULL,
    lfl_status  TEXT NOT NULL,
    reason      TEXT NOT NULL,                     -- обязательна (ТЗ §3)
    set_by      BIGINT REFERENCES users(id),
    set_at      TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    UNIQUE (instance_id, code_cfo)
);

-- Параметры периода (ТЗ §5, §10): индекс роста продаж, удельный вес и порог ФОТ,
-- ставки аренды, пороги предупреждений. В ФОРМУЛЕ этих чисел нет — они данные,
-- потому что ТЗ §5 разрешает переопределение «по городу / LFL-статусу / типу
-- магазина / отдельному магазину» с приоритетом от частного к общему.
--
-- scope_kind: country | city | lfl | store_type | store (scope_value — значение
-- измерения; для country — код страны). Приоритет разбора: store → store_type →
-- lfl → city → country.
CREATE TABLE IF NOT EXISTS tp_period_param (
    id          BIGSERIAL PRIMARY KEY,
    instance_id BIGINT NOT NULL REFERENCES tp_instance(id) ON DELETE CASCADE,
    scope_kind  TEXT NOT NULL,
    scope_value TEXT NOT NULL DEFAULT '',
    -- param_code: sales_index | payroll_share | payroll_cap | rent_share |
    -- rent_turnover_threshold | rent_rate | lfl_warn | lfm_warn | strategy_warn.
    param_code  TEXT NOT NULL,
    param_value NUMERIC(18,6) NOT NULL DEFAULT 0,
    note        TEXT NOT NULL DEFAULT '',
    set_by      BIGINT REFERENCES users(id),       -- «кто задал» (ТЗ §5)
    set_at      TIMESTAMPTZ NOT NULL DEFAULT NOW(),-- «когда» (ТЗ §5)
    UNIQUE (instance_id, param_code, scope_kind, scope_value)
);
CREATE INDEX IF NOT EXISTS idx_tp_period_param_inst ON tp_period_param(instance_id, param_code);

-- Соответствие «пользователь ↔ RegManager» (ТЗ §9). Справочник сотрудников
-- FinDWH.dbo.DimEmployee, из которого это соответствие могло бы приезжать
-- автоматически, НЕ подтверждён по доступу — поэтому таблица живёт в приложении
-- и заполняется руками (или синхронизацией, когда доступ дадут).
-- Строки с RegManager 'Closed'/'n/a' в источнике — закрытые точки, они в
-- соответствие не попадают (проверка в коде, ТЗ §9).
CREATE TABLE IF NOT EXISTS tp_reg_manager_map (
    id          BIGSERIAL PRIMARY KEY,
    user_id     BIGINT REFERENCES users(id) ON DELETE CASCADE,
    login       TEXT NOT NULL DEFAULT '',          -- доменный логин, если пользователь ещё не заходил
    reg_manager TEXT NOT NULL,                     -- значение [001 CodeCFO].RegManager
    note        TEXT NOT NULL DEFAULT '',
    updated_by  BIGINT REFERENCES users(id),
    updated_at  TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    UNIQUE (reg_manager, login)
);
CREATE INDEX IF NOT EXISTS idx_tp_reg_manager_user ON tp_reg_manager_map(user_id);

-- Справочник магазинов розницы (ТЗ §11): источник FinDWH.dbo.[001 CodeCFO]
-- WHERE GroupCFO1='Магазины'. Отдельный справочник, а не dir_cfo, потому что
-- dir_cfo не несёт LfLStatus/TypeOfStore/Category/RegManager/DateOpen/DateClose —
-- атрибутов, на которых держатся §2 (снапшот), §5 (переопределения индекса),
-- §6 (пороги по LFL-статусам) и §9 (права РМ). Синхронизация — общим движком
-- sync.go (версионирование строк, раз в сутки + кнопка).
INSERT INTO plans_directory (code, source, sync_status)
VALUES ('dir_retail_store', 'olap', 'seed')
ON CONFLICT (code) DO NOTHING;

-- Маппинг публикации формы «Розница» (ТЗ §11 предупреждение о ГруппыЦФО1):
-- в справочнике источника GroupCFO1='Магазины', а в таблице плана — '3.Магазины'.
-- Сопоставление СПРАВОЧНИКОМ, а не строковым сравнением в коде.
INSERT INTO plans_directory (code, source, sync_status)
VALUES ('dir_retail_group_map', 'manual', 'seed')
ON CONFLICT (code) DO NOTHING;

DELETE FROM plans_directory_row
 WHERE directory_id = (SELECT id FROM plans_directory WHERE code = 'dir_retail_group_map');
INSERT INTO plans_directory_row (directory_id, external_id, payload_json)
SELECT d.id, v.ext, v.payload FROM plans_directory d
CROSS JOIN (VALUES
    ('stores', '{"source_group": "Магазины", "plan_group": "3.Магазины", "note": "ТЗ Розница §11: сопоставление справочника и таблицы плана"}'::jsonb)
) AS v(ext, payload)
WHERE d.code = 'dir_retail_group_map';

-- Маппинг публикации розницы: 0032 засеял только BY и RU. Форм розницы четыре
-- (ТЗ §1), поэтому добавляем KZ и UZ — иначе dry-run по этим странам молча даёт
-- «нет строк для публикации» вместо честного «правило ждёт BI».
-- block_type='sales_plan' и приёмник — как в 0032; КодPL NULL: его подтверждает BI
-- (ТЗ §12). Строки ВЫКЛЮЧЕНЫ (enabled=FALSE) — до подтверждения только dry-run.
INSERT INTO publish_mapping
    (form_code, block_type, country, param_name, code_pl, target_table, target_currency, aggregate, aggregate_cfo, enabled, note)
VALUES
    ('TPL-TO-RETAIL', 'sales_plan', 'KZ', 'ПРОДАЖИ', NULL, 'Budgeting.dbo.VFORMTOLOADTAKTTARGET', '', FALSE, NULL, FALSE,
     'ТЗ Розница §7.2/§12: КодPL по срезу KZ подтверждает BI'),
    ('TPL-TO-RETAIL', 'sales_plan', 'UZ', 'ПРОДАЖИ', NULL, 'Budgeting.dbo.VFORMTOLOADTAKTTARGET', '', FALSE, NULL, FALSE,
     'ТЗ Розница §7.2/§12: КодPL по срезу UZ подтверждает BI')
ON CONFLICT (form_code, block_type, country, param_name, target_table, target_currency) DO NOTHING;
