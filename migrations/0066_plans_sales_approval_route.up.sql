-- Sales cards follow fill -> financial controller -> Degtereva -> Siparova.
-- Existing cards waiting at the removed manager step enter the controller step.
UPDATE plans_form_route
   SET enabled = TRUE, step_name = 'Финансовый контролёр', responsible = 'Финансовый контролёр',
       sort_order = 20, skip_if_same_user = FALSE
 WHERE form_code IN ('TPL-MP', 'TPL-TO-RETAIL') AND step_code = 'fin';

UPDATE plans_form_route
   SET enabled = FALSE
 WHERE form_code IN ('TPL-MP', 'TPL-TO-RETAIL') AND step_code = '1.2';

UPDATE plans_form_route
   SET step_name = 'Согласование Дегтеревой Е.В.', responsible = 'Дегтерева Е.В.', sort_order = 30, enabled = TRUE
 WHERE form_code IN ('TPL-MP', 'TPL-TO-RETAIL') AND step_code = '1.3';

UPDATE plans_form_route
   SET step_name = 'Утверждение Сипаровой С.Г.', responsible = 'Сипарова С.Г.', sort_order = 40, enabled = TRUE
 WHERE form_code IN ('TPL-MP', 'TPL-TO-RETAIL') AND step_code = '1.4';

UPDATE form_card SET step_code = 'fin', updated_at = NOW()
 WHERE form_code IN ('TPL-MP', 'TPL-TO-RETAIL') AND step_code = '1.2'
   AND status IN ('on_approval', 'returned');

-- The removed manager step must not remain an active approval on open cards.
UPDATE card_approval a SET revoked = TRUE,
    revoked_reason = 'шаг 1.2 заменён финансовым контролёром (маршрут 0066)'
  FROM form_card c
 WHERE a.card_id = c.id AND c.form_code IN ('TPL-MP', 'TPL-TO-RETAIL')
   AND c.status IN ('draft', 'returned', 'on_approval')
   AND a.step_code = '1.2' AND a.decision IN ('approve', 'auto_skipped') AND NOT a.revoked;

-- New MP tactics use the share/markup cascade. Frozen approval snapshots keep
-- their original calculation mode until a deliberate reopen.
UPDATE form_card SET calc_mode = 'inverse', updated_at = NOW()
 WHERE form_code = 'TPL-MP' AND status IN ('draft', 'returned') AND calc_mode <> 'inverse';
