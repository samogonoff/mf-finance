-- 002 — журнал завершённых ETL-запусков Premaster1C → fact_premaster.
-- Append-only: одна строка пишется при завершении каждого run'а (успех или ошибка).
-- Текущий «в процессе» запуск живёт в PG (debt_etl_checkpoint), не здесь.
--
-- Зачем CH: журнал растёт линейно от количества заборов (~ десятки/сотни в сутки
-- при инкременте каждые 5-15 минут), за год — тысячи строк. Аналитика «средняя
-- длительность забора», «error rate по ЮЛ», «throughput по часам» — это
-- column-store sweet spot.

CREATE TABLE IF NOT EXISTS finance.etl_run_log
(
    run_id          UUID DEFAULT generateUUIDv4(),
    company_id      LowCardinality(String),
    phase           LowCardinality(String),     -- 'bootstrap' | 'incremental'
    triggered_by    LowCardinality(String),     -- 'manual' (docker exec) | 'admin-ui' | 'cron'
    started_at      DateTime,
    finished_at     DateTime,
    duration_sec    Float32,
    rows_loaded     UInt64,                     -- сколько строк зашло в этом run'е
    last_change_at  Nullable(DateTime),         -- для incremental: верхняя граница обработанного DateOfChange
    error_text      String DEFAULT ''           -- '' если успех
)
ENGINE = MergeTree
PARTITION BY toYYYYMM(started_at)
ORDER BY (started_at, company_id)
SETTINGS index_granularity = 8192;
