-- Расходы формы 1.5 публикуются в ПЕСОЧНИЦУ (publish_sandbox в БД кабинета): правило BYN-пары включено.
-- Реальная запись невозможна по построению: приёмники FormToLoadPlan / VFORMTOLOADPLAN — только для песочницы,
-- боевой публикатор их отклоняет (publishSandboxOnlyTargets), а сами расходы у аналитиков лежат в
-- Budgeting.budget.Input — запись туда не согласована с ФЭО.
-- Второе правило (VFORMTOLOADPLAN, национальная валюта) остаётся выключенным: публикация пишет в один приёмник за раз.
UPDATE publish_mapping
   SET enabled = TRUE,
       note = 'Этап 1.5, BYN-пара. Только песочница (publish_sandbox): реальная запись расходов — в Budgeting.budget.Input, не согласована с ФЭО.',
       updated_at = NOW()
 WHERE form_code = 'TPL-CFO-EXP' AND block_type = 'exp' AND target_table = 'Budgeting.dbo.FormToLoadPlan';