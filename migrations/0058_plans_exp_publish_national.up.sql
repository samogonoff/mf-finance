-- Публикация формы 1.5 в национальной валюте (ответ 4.2 от 11.09.2026).
--
-- Приёмник разделяется колонкой «Параметр», значение 'ЗАТРАТЫ' подтверждено —
-- заготовка оказалась верной. Добавляется вторая цель: VFORMTOLOADPLAN несёт
-- национальную валюту и читается наравне с FormToLoadPlan (в самой книге эталона
-- лежат запросы к обеим).
--
-- Правило выключено, как и первое: включаем после dry-run с нулём дублей по
-- ЧЕТЫРЁМ наборам — реестр общих затрат МП, статьи 51/52 логистики, ИТ с
-- признаком переноса и код 66 (ответ 4.1).
--
-- ВАЖНО для BI (ответ И-5): уникальный индекс приёмника обязан включать колонку
-- «Параметр». Без неё публикация затрат затрёт строки продаж по той же паре
-- «ЦФО + статья» — они лежат в одной таблице и различаются только Параметром.
-- Наш DELETE уже идёт по ключу Параметр+Страна+КодЦФО+КодPL+Дата.

INSERT INTO publish_mapping (form_code, block_type, country, param_name, target_table, target_currency, enabled, note)
SELECT 'TPL-CFO-EXP', 'exp', '', 'ЗАТРАТЫ', 'Budgeting.dbo.VFORMTOLOADPLAN', '', FALSE,
       'Этап 1.5, национальная валюта (ответ 4.2). Включать вместе с основным правилом после нулевого dry-run.'
 WHERE NOT EXISTS (
        SELECT 1 FROM publish_mapping
         WHERE form_code = 'TPL-CFO-EXP' AND target_table = 'Budgeting.dbo.VFORMTOLOADPLAN');

-- Основное правило пишет BYN-пару: в FormToLoadPlan числа приведены к рублям.
UPDATE publish_mapping
   SET target_currency = 'BYN',
       note = 'Этап 1.5, BYN-пара. Выключено до сверки с BI: dry-run → песочница → запись.'
 WHERE form_code = 'TPL-CFO-EXP' AND target_table = 'Budgeting.dbo.FormToLoadPlan';
