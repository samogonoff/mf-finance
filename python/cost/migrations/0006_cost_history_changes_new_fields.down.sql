-- 0006 — откат: удаление 4 полей из FinSandBox.dbo.CostHistory_Changes

IF EXISTS (SELECT 1 FROM sys.columns WHERE object_id = OBJECT_ID('[FinSandBox].[dbo].[CostHistory_Changes]') AND name = 'calc_sign')
    ALTER TABLE [FinSandBox].[dbo].[CostHistory_Changes] DROP COLUMN calc_sign;
GO

IF EXISTS (SELECT 1 FROM sys.columns WHERE object_id = OBJECT_ID('[FinSandBox].[dbo].[CostHistory_Changes]') AND name = 'plan_id')
    ALTER TABLE [FinSandBox].[dbo].[CostHistory_Changes] DROP COLUMN plan_id;
GO

IF EXISTS (SELECT 1 FROM sys.columns WHERE object_id = OBJECT_ID('[FinSandBox].[dbo].[CostHistory_Changes]') AND name = 'approved_at')
    ALTER TABLE [FinSandBox].[dbo].[CostHistory_Changes] DROP COLUMN approved_at;
GO

IF EXISTS (SELECT 1 FROM sys.columns WHERE object_id = OBJECT_ID('[FinSandBox].[dbo].[CostHistory_Changes]') AND name = 'approved_by')
    ALTER TABLE [FinSandBox].[dbo].[CostHistory_Changes] DROP COLUMN approved_by;
GO
