-- 0015 — MSSQL: add comment column to FinSandBox.dbo.CostHistory_Changes
--
-- Запускать ТОЛЬКО на MSSQL сервере (10.10.6.15, база FinSandBox).
--
--   sqlcmd -S 10.10.6.15 -d FinSandBox -U user -P pass -i 0015_cost_history_changes_comment.up.sql

IF NOT EXISTS (SELECT 1 FROM sys.columns WHERE object_id = OBJECT_ID('[FinSandBox].[dbo].[CostHistory_Changes]') AND name = 'comment')
    ALTER TABLE [FinSandBox].[dbo].[CostHistory_Changes] ADD comment NVARCHAR(MAX) NULL;
GO
