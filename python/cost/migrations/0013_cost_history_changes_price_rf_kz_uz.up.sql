-- 0013 — MSSQL: добавление 3 полей (price_rf, price_kz, price_uz) в FinSandBox.dbo.CostHistory_Changes
-- для хранения цен РФ, КЗ, УЗ из s_price_level (PRICE_TYPE4/5/6).
--
-- Запускать ТОЛЬКО на MSSQL сервере (10.10.6.15, база FinSandBox).
-- Для применения необходимо:
--   1. Подключиться к OLAP серверу
--   2. Выполнить этот скрипт в базе FinSandBox
--
-- Либо запустить через SQL файл миграции (если настроено):
--   sqlcmd -S 10.10.6.15 -d FinSandBox -U user -P pass -i 0013_cost_history_changes_price_rf_kz_uz.up.sql

IF NOT EXISTS (SELECT 1 FROM sys.columns WHERE object_id = OBJECT_ID('[FinSandBox].[dbo].[CostHistory_Changes]') AND name = 'price_rf')
    ALTER TABLE [FinSandBox].[dbo].[CostHistory_Changes] ADD price_rf NUMERIC(18, 2) NULL;
GO

IF NOT EXISTS (SELECT 1 FROM sys.columns WHERE object_id = OBJECT_ID('[FinSandBox].[dbo].[CostHistory_Changes]') AND name = 'price_kz')
    ALTER TABLE [FinSandBox].[dbo].[CostHistory_Changes] ADD price_kz NUMERIC(18, 2) NULL;
GO

IF NOT EXISTS (SELECT 1 FROM sys.columns WHERE object_id = OBJECT_ID('[FinSandBox].[dbo].[CostHistory_Changes]') AND name = 'price_uz')
    ALTER TABLE [FinSandBox].[dbo].[CostHistory_Changes] ADD price_uz NUMERIC(18, 2) NULL;
GO
