-- Реестры формы этапа 1.5 (ТЗ §5, уточнения ответами 38, 40 и находкой Н3).
--
-- Реестров четыре: ФОТ, командировки, логистика по ЦФО 250 и ИТ-расходы. Строки
-- в них добавляет и удаляет пользователь (ТЗ §5.5), а итоги подставляются в
-- статьи табличной части, которые из-за этого перестают редактироваться.
--
-- Почему одна таблица строк на все четыре, а не четыре разные: у реестров общая
-- половина (двенадцать месяцев, итог, порядок, кто и когда правил) и разные
-- атрибуты. Атрибуты держим в JSONB, а то, по чему идёт ОТБОР и ПУБЛИКАЦИЯ —
-- код ЦФО, код статьи и два признака ИТ-реестра — обычными колонками: искать и
-- публиковать по JSONB нельзя.
--
-- Признаки ИТ-строки (ТЗ §5.4, §11) — именно колонки, а не текст в примечании:
-- в эталоне они записаны словами «ПЕРЕНОСИМ НА ЦФО 250» и «НЕ БЕРЁМ В РАСЧЁТ».

CREATE TABLE IF NOT EXISTS exp_reg_row (
    id          BIGSERIAL PRIMARY KEY,
    instance_id BIGINT NOT NULL REFERENCES exp_instance(id) ON DELETE CASCADE,
    -- payroll | travel | logistics | it
    kind        TEXT NOT NULL,
    sort_order  INT  NOT NULL DEFAULT 0,
    -- Отбор и публикация: код ЦФО и статья PL строки.
    code_cfo    INT  NOT NULL DEFAULT 0,
    code_pl     INT  NOT NULL DEFAULT 0,
    -- Признаки ИТ-реестра. move_to_250 публикует строку под ЦФО 250 вместо
    -- своего; excluded исключает её из итога и из публикации совсем.
    move_to_250 BOOLEAN NOT NULL DEFAULT FALSE,
    excluded    BOOLEAN NOT NULL DEFAULT FALSE,
    -- Атрибуты вида реестра: ЮЛ и сотрудник у ФОТ, страна/город/цель у
    -- командировок, наименование строки у ИТ.
    attrs       JSONB NOT NULL DEFAULT '{}',
    note        TEXT NOT NULL DEFAULT '',
    created_by  BIGINT REFERENCES users(id),
    created_at  TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at  TIMESTAMPTZ NOT NULL DEFAULT NOW()
);
CREATE INDEX IF NOT EXISTS exp_reg_row_inst_idx ON exp_reg_row (instance_id, kind, sort_order);

CREATE TABLE IF NOT EXISTS exp_reg_value (
    id     BIGSERIAL PRIMARY KEY,
    row_id BIGINT NOT NULL REFERENCES exp_reg_row(id) ON DELETE CASCADE,
    month  INT NOT NULL CHECK (month BETWEEN 1 AND 12),
    -- NULL = не заполнено, как и в сетке формы: у реестра ФОТ есть строки
    -- резерва с нулями по всем месяцам (ТЗ §5.1), и ноль там введён осознанно.
    amount NUMERIC(18,2),
    UNIQUE (row_id, month)
);
CREATE INDEX IF NOT EXISTS exp_reg_value_row_idx ON exp_reg_value (row_id);

-- Журнал удалённых строк: ТЗ §5.5 требует, чтобы удалённая строка попадала в
-- журнал ВМЕСТЕ СО СВОИМИ ЗНАЧЕНИЯМИ, иначе «куда делись 40 тысяч» не ответить.
CREATE TABLE IF NOT EXISTS exp_reg_deleted (
    id          BIGSERIAL PRIMARY KEY,
    instance_id BIGINT NOT NULL REFERENCES exp_instance(id) ON DELETE CASCADE,
    kind        TEXT NOT NULL,
    payload     JSONB NOT NULL,          -- строка целиком, включая месяцы
    deleted_by  BIGINT REFERENCES users(id),
    deleted_at  TIMESTAMPTZ NOT NULL DEFAULT NOW()
);
CREATE INDEX IF NOT EXISTS exp_reg_deleted_inst_idx ON exp_reg_deleted (instance_id, deleted_at DESC);
