DELETE FROM publish_mapping WHERE form_code = 'TPL-CFO-EXP' AND target_table = 'Budgeting.dbo.VFORMTOLOADPLAN';
UPDATE publish_mapping SET target_currency = '' WHERE form_code = 'TPL-CFO-EXP';
