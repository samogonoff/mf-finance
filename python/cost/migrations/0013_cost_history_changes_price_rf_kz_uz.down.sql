-- 0013 — откат: удаление 3 полей price_rf, price_kz, price_uz из FinSandBox.dbo.CostHistory_Changes
--
-- Запускать ТОЛЬКО на MSSQL сервере (10.10.6.15, база FinSandBox).

IF EXISTS (SELECT 1 FROM sys.columns WHERE object_id = OBJECT_ID('[FinSandBox].[dbo].[CostHistory_Changes]') AND name = 'price_rf')
    ALTER TABLE [FinSandBox].[dbo].[CostHistory_Changes] DROP COLUMN price_rf;
GO

IF EXISTS (SELECT 1 FROM sys.columns WHERE object_id = OBJECT_ID('[FinSandBox].[dbo].[CostHistory_Changes]') AND name = 'price_kz')
    ALTER TABLE [FinSandBox].[dbo].[CostHistory_Changes] DROP COLUMN price_kz;
GO

IF EXISTS (SELECT 1 FROM sys.columns WHERE object_id = OBJECT_ID('[FinSandBox].[dbo].[CostHistory_Changes]') AND name = 'price_uz')
    ALTER TABLE [FinSandBox].[dbo].[CostHistory_Changes] DROP COLUMN price_uz;
GO
