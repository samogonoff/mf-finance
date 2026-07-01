IF EXISTS (SELECT 1 FROM sys.columns
    WHERE object_id = OBJECT_ID('[FinSandBox].[dbo].[CostHistory_Changes]')
    AND name = 'comment')
    ALTER TABLE [FinSandBox].[dbo].[CostHistory_Changes] DROP COLUMN comment;
GO
