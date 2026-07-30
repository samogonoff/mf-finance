-- 0029 — MSSQL: add mp_price_rub column to FinSandBox.dbo.CostHistory_Changes
--
-- Запускать ТОЛЬКО на MSSQL сервере (10.10.6.15, база FinSandBox).
--
--   sqlcmd -S 10.10.6.15 -d FinSandBox -U user -P pass -i 0029_cost_history_changes_mp_price.up.sql

IF NOT EXISTS (SELECT 1 FROM sys.columns WHERE object_id = OBJECT_ID('[FinSandBox].[dbo].[CostHistory_Changes]') AND name = 'mp_price_rub')
    ALTER TABLE [FinSandBox].[dbo].[CostHistory_Changes] ADD mp_price_rub NUMERIC(18,2) NULL;
GO
