-- 0072 — маршрут форм продаж по ТЗ доработок кабинета (15.10.2026, п. 4.1 и 6.3).
--
-- 0066 (по ФЗ 02.10 п. 17) выключила шаг 1.2 «Согласование руководителем» у обеих
-- форм и поставила финансового контролёра вторым шагом. Итоговое ТЗ возвращает
-- руководителя (Смолер О.В. — розница, Ворончук Е. / Левин — крупные / мелкие МП)
-- и оставляет контролёра между 1.2 и 1.3. По решению заказчика контролёр нужен
-- только рознице:
--
--   TPL-TO-RETAIL: 1.1 → 1.2 → fin → 1.3 → 1.4
--   TPL-MP:        1.1 → 1.2 → 1.3 → 1.4   (fin остаётся строкой маршрута,
--                                            выключен — включается одним флагом)
--
-- Порядок у обеих форм одинаковый, без совпадающих sort_order: при равных
-- значениях порядок шагов не определён (ORDER BY sort_order).
-- Названия, «ответственные», сроки и флаги самосогласования/публикации шагов не
-- трогаем — они из сида 0031/0066 и могли быть уточнены в админке.
UPDATE plans_form_route SET sort_order = 10, enabled = TRUE, updated_at = NOW()
 WHERE form_code IN ('TPL-MP', 'TPL-TO-RETAIL') AND step_code = '1.1';

UPDATE plans_form_route SET sort_order = 20, enabled = TRUE, updated_at = NOW()
 WHERE form_code IN ('TPL-MP', 'TPL-TO-RETAIL') AND step_code = '1.2';

UPDATE plans_form_route SET sort_order = 30, enabled = (form_code = 'TPL-TO-RETAIL'), updated_at = NOW()
 WHERE form_code IN ('TPL-MP', 'TPL-TO-RETAIL') AND step_code = 'fin';

UPDATE plans_form_route SET sort_order = 40, enabled = TRUE, updated_at = NOW()
 WHERE form_code IN ('TPL-MP', 'TPL-TO-RETAIL') AND step_code = '1.3';

UPDATE plans_form_route SET sort_order = 50, enabled = TRUE, updated_at = NOW()
 WHERE form_code IN ('TPL-MP', 'TPL-TO-RETAIL') AND step_code = '1.4';

-- Открытые карточки назад не переносим: розница, стоящая на fin, уже прошла
-- бы руководителя по новому маршруту и идёт дальше как есть; черновики и
-- возвращённые на 1.1 пойдут через 1.2 при следующей отправке.
--
-- У МП шага fin больше нет, и карточка на нём застряла бы («шаг не входит в
-- маршрут формы»). Решение на этом шаге переходит к руководителю — тому шагу,
-- который заменил контролёра в маршруте МП.
UPDATE form_card SET step_code = '1.2', updated_at = NOW()
 WHERE form_code = 'TPL-MP' AND step_code = 'fin'
   AND status IN ('on_approval', 'returned');

-- Решение выключенного шага не должно оставаться действующим на открытых
-- карточках МП (так же 0066 поступила с шагом 1.2).
UPDATE card_approval a SET revoked = TRUE,
    revoked_reason = 'шаг fin выключен для МП (маршрут 0072)'
  FROM form_card c
 WHERE a.card_id = c.id AND c.form_code = 'TPL-MP'
   AND c.status IN ('draft', 'returned', 'on_approval')
   AND a.step_code = 'fin' AND a.decision IN ('approve', 'auto_skipped') AND NOT a.revoked;
