UPDATE publish_mapping
   SET enabled = FALSE,
       note = 'Этап 1.5, BYN-пара. Выключено до сверки с BI: dry-run → песочница → запись.',
       updated_at = NOW()
 WHERE form_code = 'TPL-CFO-EXP' AND block_type = 'exp' AND target_table = 'Budgeting.dbo.FormToLoadPlan';