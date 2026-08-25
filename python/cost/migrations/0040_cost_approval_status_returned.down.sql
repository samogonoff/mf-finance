-- Откат 0040: вернуть CHECK без статуса 'returned'.
--
-- Существующие записи с этим статусом сначала переводим в 'rejected' — иначе
-- constraint не создастся. Различие «возврат против отклонения» при откате
-- теряется, восстановить его нечем.

UPDATE cost_calc_approvals SET status = 'rejected' WHERE status = 'returned';

ALTER TABLE cost_calc_approvals DROP CONSTRAINT IF EXISTS cost_calc_approvals_status_check;
ALTER TABLE cost_calc_approvals ADD CONSTRAINT cost_calc_approvals_status_check
    CHECK (status IN ('pending', 'approved', 'rejected'));
