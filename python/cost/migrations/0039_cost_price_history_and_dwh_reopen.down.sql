-- Откат 0039.
--
-- Вместе с таблицами теряется локальная история цен: в OLAP
-- (FinSandBox.dbo.CostHistory_Changes) записи остаются, поэтому восстановить
-- срез можно повторным бэкфиллом после нового наката.

DROP TABLE IF EXISTS cost_dwh_reopen;
DROP TABLE IF EXISTS cost_price_history;
