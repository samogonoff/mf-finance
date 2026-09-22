UPDATE plans_form_route SET publish_on_approve = FALSE WHERE form_code = 'TPL-CFO-EXP' AND step_code = '1.4';
DELETE FROM publish_mapping WHERE form_code = 'TPL-CFO-EXP';
