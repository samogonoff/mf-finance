-- 0015 — MSSQL: add comment column to FinSandBox.dbo.CostHistory_Changes
-- Запускать ТОЛЬКО на MSSQL OLAP сервере (10.10.6.15, база FinSandBox).
IF NOT EXISTS (SELECT 1 FROM sys.columns
    WHERE object_id = OBJECT_ID('[FinSandBox].[dbo].[CostHistory_Changes]')
    AND name = 'comment')
    ALTER TABLE [FinSandBox].[dbo].[CostHistory_Changes] ADD comment NVARCHAR(1000) NULL;
GO
