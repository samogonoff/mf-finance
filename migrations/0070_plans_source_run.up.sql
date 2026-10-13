-- Журнал прогонов источников модуля планов (ТЗ доработок кабинета, п. 1.1).
--
-- «Ввести проверку после выката: количество строк по каждой таблице-источнику
-- сравнивается с предыдущим успешным прогоном, отклонение больше порога — алерт».
-- mp_fact_snapshot_run хранит только ПОСЛЕДНИЙ прогон (UPSERT по периоду), и
-- сравнивать было не с чем. Здесь — история: каждая загрузка источника — строка.
--
-- healthy — прогон без ошибки и без падения строк сверх порога; по нему и идёт
-- сравнение. alerted — по прогону поднят алерт (переход «здоров → нет»), чтобы
-- повторные загрузки того же сломанного источника не спамили админов.
CREATE TABLE IF NOT EXISTS plans_source_run (
    id          BIGSERIAL PRIMARY KEY,
    source      TEXT        NOT NULL,             -- mp_snapshot:fact, retail:fact, …
    scope       TEXT        NOT NULL DEFAULT '',  -- период/сегмент/страна
    rows_total  INTEGER     NOT NULL DEFAULT 0,
    error_text  TEXT        NOT NULL DEFAULT '',
    healthy     BOOLEAN     NOT NULL DEFAULT TRUE,
    alerted     BOOLEAN     NOT NULL DEFAULT FALSE,
    prev_rows   INTEGER,                          -- строк в предыдущем здоровом прогоне
    created_at  TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX IF NOT EXISTS idx_plans_source_run_key
    ON plans_source_run (source, scope, created_at DESC);
