-- Откат 0038: снять NOT NULL и DEFAULT с task_number.
--
-- Пустые строки обратно в NULL НЕ превращаем: с NULLS DISTINCT это вернуло бы
-- поведение, из-за которого ON CONFLICT не срабатывал. Удалённые на шаге 1
-- дубли откатом тоже не восстановить — они были следствием того же дефекта.

ALTER TABLE cost_calc_approvals ALTER COLUMN task_number DROP NOT NULL;
ALTER TABLE cost_calc_approvals ALTER COLUMN task_number DROP DEFAULT;
