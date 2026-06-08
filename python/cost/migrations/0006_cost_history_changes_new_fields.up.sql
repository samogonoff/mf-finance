-- 0006 — MSSQL: добавление 4 полей в FinSandBox.dbo.CostHistory_Changes
-- для отслеживания утверждения цен.
--
-- Запускать ТОЛЬКО на MSSQL сервере (10.10.6.15, база FinSandBox).
-- Для применения необходимо:
--   1. Подключиться к OLAP серверу
--   2. Выполнить этот скрипт в базе FinSandBox
--
-- Либо запустить через SQL файл миграции (если настроено):
--   sqlcmd -S 10.10.6.15 -d FinSandBox -U user -P pass -i 0006_cost_history_changes_new_fields.up.sql

IF NOT EXISTS (SELECT 1 FROM sys.columns WHERE object_id = OBJECT_ID('[FinSandBox].[dbo].[CostHistory_Changes]') AND name = 'calc_sign')
    ALTER TABLE [FinSandBox].[dbo].[CostHistory_Changes] ADD calc_sign NVARCHAR(50) NULL;
GO

IF NOT EXISTS (SELECT 1 FROM sys.columns WHERE object_id = OBJECT_ID('[FinSandBox].[dbo].[CostHistory_Changes]') AND name = 'plan_id')
    ALTER TABLE [FinSandBox].[dbo].[CostHistory_Changes] ADD plan_id NVARCHAR(50) NULL;
GO

IF NOT EXISTS (SELECT 1 FROM sys.columns WHERE object_id = OBJECT_ID('[FinSandBox].[dbo].[CostHistory_Changes]') AND name = 'approved_at')
    ALTER TABLE [FinSandBox].[dbo].[CostHistory_Changes] ADD approved_at DATETIME NULL;
GO

IF NOT EXISTS (SELECT 1 FROM sys.columns WHERE object_id = OBJECT_ID('[FinSandBox].[dbo].[CostHistory_Changes]') AND name = 'approved_by')
    ALTER TABLE [FinSandBox].[dbo].[CostHistory_Changes] ADD approved_by NVARCHAR(255) NULL;
GO
