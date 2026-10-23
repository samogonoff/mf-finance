-- A retail CFO 40 row was loaded before the internet-shop source correction.
-- Mark rows only in cards currently at the fill step. A return to an
-- intermediate approval step keeps the amounts submitted earlier. Never
-- rewrite a card already submitted for approval: its saved totals and a
-- possible retry of publication must agree.
-- The marker persists when an editable card later enters approval.
UPDATE exp_row r
   SET attrs = jsonb_set(r.attrs, '{retail_cfo40_reassigned}', 'true'::jsonb),
       updated_at = NOW()
  FROM exp_instance i
  JOIN form_card c ON c.id = i.card_id
 WHERE r.instance_id = i.id
   AND i.form_kind = 'retail'
   AND r.code_cfo = 40
   AND c.status IN ('draft', 'returned')
   AND NOT c.locked
   AND EXISTS (
       SELECT 1 FROM plans_form_route route
        WHERE route.form_code = c.form_code
          AND route.step_code = c.step_code
          AND route.kind = 'fill'
          AND route.enabled
   );
