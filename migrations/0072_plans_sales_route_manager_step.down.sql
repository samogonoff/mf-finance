-- Возврат к маршруту 0066: 1.1 → fin → 1.3 → 1.4 у обеих форм, 1.2 выключен.
UPDATE card_approval SET revoked = FALSE, revoked_reason = ''
 WHERE revoked_reason = 'шаг fin выключен для МП (маршрут 0072)';

UPDATE plans_form_route SET sort_order = 10, enabled = TRUE, updated_at = NOW()
 WHERE form_code IN ('TPL-MP', 'TPL-TO-RETAIL') AND step_code = '1.1';
UPDATE plans_form_route SET sort_order = 20, enabled = TRUE, updated_at = NOW()
 WHERE form_code IN ('TPL-MP', 'TPL-TO-RETAIL') AND step_code = 'fin';
UPDATE plans_form_route SET sort_order = 30, enabled = FALSE, updated_at = NOW()
 WHERE form_code IN ('TPL-MP', 'TPL-TO-RETAIL') AND step_code = '1.2';
UPDATE plans_form_route SET sort_order = 30, enabled = TRUE, updated_at = NOW()
 WHERE form_code IN ('TPL-MP', 'TPL-TO-RETAIL') AND step_code = '1.3';
UPDATE plans_form_route SET sort_order = 40, enabled = TRUE, updated_at = NOW()
 WHERE form_code IN ('TPL-MP', 'TPL-TO-RETAIL') AND step_code = '1.4';

-- Карточки на выключаемом шаге 1.2 переходят к контролёру, как в 0066, и с той
-- же пометкой аннулированных решений — откат 0066 их восстановит.
UPDATE form_card SET step_code = 'fin', updated_at = NOW()
 WHERE form_code IN ('TPL-MP', 'TPL-TO-RETAIL') AND step_code = '1.2'
   AND status IN ('on_approval', 'returned');

UPDATE card_approval a SET revoked = TRUE,
    revoked_reason = 'шаг 1.2 заменён финансовым контролёром (маршрут 0066)'
  FROM form_card c
 WHERE a.card_id = c.id AND c.form_code IN ('TPL-MP', 'TPL-TO-RETAIL')
   AND c.status IN ('draft', 'returned', 'on_approval')
   AND a.step_code = '1.2' AND a.decision IN ('approve', 'auto_skipped') AND NOT a.revoked;
