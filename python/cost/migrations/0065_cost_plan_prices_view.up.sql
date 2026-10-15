-- 0065 — Бренд-менеджер видит «Цены материалов по плану» в режиме просмотра.
--
-- Задача 663794: БМ просит показать стоимость полотен на весь план — сейчас
-- модалка закрыта правом cost:edit_materials (Калькулятор/ПЭО/админ). Новое
-- право cost:plan_prices_view открывает ТОЛЬКО чтение (GET /plan-materials,
-- GET /plan-price-sets*): мутации наборов по-прежнему требуют edit_materials,
-- а фронт прячет кнопки правки. Выдаём роли «Бренд-менеджер» и её копии
-- «Бренд-менеджер ТЕКС» (миграция 0060); «Просмотр» право не получает.

UPDATE cost_roles
   SET permissions = permissions || '["cost:plan_prices_view"]'::jsonb,
       updated_at = now()
 WHERE name IN ('Бренд-менеджер', 'Бренд-менеджер ТЕКС')
   AND NOT permissions @> '["cost:plan_prices_view"]'::jsonb;
