-- 0027 down
DROP TABLE IF EXISTS pl_task;
DROP TABLE IF EXISTS pl_task_template;
ALTER TABLE pl_stage_instance DROP COLUMN IF EXISTS owner_user_id;
