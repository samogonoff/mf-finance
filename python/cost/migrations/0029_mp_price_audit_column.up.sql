-- 0029 — add mp_price_rub (Цена для МП, рос. руб.) to cost_price_changes_audit.
-- Значение считается заново в момент apply_pending_changes() (курс/константы
-- МП — актуальные на момент утверждения, см. обсуждение с пользователем), эта
-- колонка — локальный аудиторский снимок того, что было отправлено в OLAP.
-- Реальная колонка в FinSandBox.dbo.CostHistory_Changes — см.
-- mssql/0029_cost_history_changes_mp_price.up.sql (запускать отдельно на MSSQL).

ALTER TABLE cost_price_changes_audit
    ADD COLUMN IF NOT EXISTS mp_price_rub NUMERIC(18,2);
