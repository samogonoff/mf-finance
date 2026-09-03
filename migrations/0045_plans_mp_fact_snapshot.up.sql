-- 0045 — снапшот факта маркетплейсов (правки ТЗ 02.09.2026, часть 2 и МП §12).
--
-- Формулировка аналитика: данные за отчётный месяц готовятся не раньше 20-го
-- числа текущего, форма берёт факт за прошлый месяц, и «читать вживую нельзя —
-- в динамике данные плавают». Причина техническая: больше двухсот процедур в DWH,
-- FinDWH и MP перезаливают таблицы через TRUNCATE, поэтому один и тот же запрос
-- в разные минуты даёт разные суммы, а форма обязана показывать согласующему то
-- же, что видел заполняющий.
--
-- Решение ТЗ: отдельная таблица, в которую данные «питаются за период», и форма
-- читает её С ОТМЕТКОЙ «данные актуальны на …».
--
-- Почему снимок хранится в БД finance, а не кэшем в памяти: отметка актуальности
-- должна переживать перезапуск сервиса, иначе после деплоя форма молча покажет
-- другой факт.

CREATE TABLE IF NOT EXISTS mp_fact_snapshot (
    id            BIGSERIAL PRIMARY KEY,
    period_year   INT NOT NULL,
    period_month  INT NOT NULL,
    -- Слой: fact | strategy | takt_target. Один снимок обслуживает все колонки
    -- формы, иначе они разъедутся по времени сбора.
    layer         TEXT NOT NULL,
    segment       TEXT NOT NULL DEFAULT '',
    code_cfo      INT NOT NULL,
    name_cfo      TEXT NOT NULL DEFAULT '',
    code_pl       INT NOT NULL,
    currency      TEXT NOT NULL DEFAULT 'RUB',
    amount        NUMERIC(20,4) NOT NULL DEFAULT 0,
    UNIQUE (period_year, period_month, layer, segment, code_cfo, code_pl)
);

CREATE INDEX IF NOT EXISTS idx_mp_fact_snapshot_period
    ON mp_fact_snapshot (period_year, period_month, layer, segment);

-- Отметка «данные актуальны на …»: одна строка на (период, слой, сегмент).
-- Держим отдельно от данных, потому что отметка нужна и когда снимок ПУСТ —
-- «собрано, данных нет» и «не собрано» для формы разные состояния.
CREATE TABLE IF NOT EXISTS mp_fact_snapshot_run (
    period_year   INT NOT NULL,
    period_month  INT NOT NULL,
    layer         TEXT NOT NULL,
    segment       TEXT NOT NULL DEFAULT '',
    rows_total    INT NOT NULL DEFAULT 0,
    refreshed_at  TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    source_note   TEXT NOT NULL DEFAULT '',
    error_text    TEXT NOT NULL DEFAULT '',
    PRIMARY KEY (period_year, period_month, layer, segment)
);
