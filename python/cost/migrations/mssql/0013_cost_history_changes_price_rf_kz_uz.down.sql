-- 0013 down — MSSQL: откат добавления price_rf, price_kz, price_uz

IF EXISTS (SELECT 1 FROM sys.columns WHERE object_id = OBJECT_ID('[FinSandBox].[dbo].[CostHistory_Changes]') AND name = 'price_rf')
    ALTER TABLE [FinSandBox].[dbo].[CostHistory_Changes] DROP COLUMN price_rf;
GO

IF EXISTS (SELECT 1 FROM sys.columns WHERE object_id = OBJECT_ID('[FinSandBox].[dbo].[CostHistory_Changes]') AND name = 'price_kz')
    ALTER TABLE [FinSandBox].[dbo].[CostHistory_Changes] DROP COLUMN price_kz;
GO

IF EXISTS (SELECT 1 FROM sys.columns WHERE object_id = OBJECT_ID('[FinSandBox].[dbo].[CostHistory_Changes]') AND name = 'price_uz')
    ALTER TABLE [FinSandBox].[dbo].[CostHistory_Changes] DROP COLUMN price_uz;
GO
