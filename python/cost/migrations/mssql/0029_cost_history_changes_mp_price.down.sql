-- 0029 down — MSSQL: откат добавления mp_price_rub

IF EXISTS (SELECT 1 FROM sys.columns WHERE object_id = OBJECT_ID('[FinSandBox].[dbo].[CostHistory_Changes]') AND name = 'mp_price_rub')
    ALTER TABLE [FinSandBox].[dbo].[CostHistory_Changes] DROP COLUMN mp_price_rub;
GO
