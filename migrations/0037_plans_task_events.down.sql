-- Откат 0037 — журнал действий по заданию.
DROP TABLE IF EXISTS pl_task_event;
ALTER TABLE pl_task DROP COLUMN IF EXISTS delegate_note;
ALTER TABLE pl_task DROP COLUMN IF EXISTS delegated_at;
ALTER TABLE pl_task DROP COLUMN IF EXISTS delegated_by;
ALTER TABLE pl_task DROP COLUMN IF EXISTS due_at;
