-- 0074 down — убрать стороннюю готовую продукцию из контура.
--
-- Дашборд маржи при откате вернётся к режиму без ГП (gp=own): в app/margin.py
-- таблица читается только при gp_mode = all|gp, а режим по умолчанию — all,
-- поэтому накатывать down нужно ВМЕСТЕ с откатом кода. Иначе дашборд упадёт на
-- «relation cost_ready_goods does not exist».
DROP TABLE IF EXISTS cost_ready_goods;
