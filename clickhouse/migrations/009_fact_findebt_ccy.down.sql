-- 009 down — снести мультивалютную таблицу. Откат к 008 (одновалютная BYN)
-- потребует повторного прогона 008 + bootstrap. down этим раннером не применяется
-- (см. swarm/migrate-clickhouse.sh) — файл для ручного отката.
DROP TABLE IF EXISTS finance.fact_findebt_ccy;
