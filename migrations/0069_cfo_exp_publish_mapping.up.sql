-- Publication mapping for the code-based CFO forms from ТЗ v2.
-- Disabled until BI confirms the dry-run; an enabled rule is required before
-- approved CFO values can be written to the tactical target table.
INSERT INTO publish_mapping (form_code, block_type, country, param_name, target_table, enabled, note)
SELECT 'TPL-CFO-EXP', 'cfo_exp', '', 'VFORMTOLOADTAKTTARGET', 'Budgeting.dbo.VFORMTOLOADTAKTTARGET', FALSE,
       'CFO forms 31/32/33/34/37/962; enable after BI dry-run and duplicate check.'
WHERE NOT EXISTS (
    SELECT 1 FROM publish_mapping
    WHERE form_code = 'TPL-CFO-EXP' AND block_type = 'cfo_exp' AND country = ''
);
