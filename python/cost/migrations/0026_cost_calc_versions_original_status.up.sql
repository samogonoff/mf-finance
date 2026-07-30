-- 0026 — статус 'original' для cost_calc_versions: неизменяемый снимок данных
-- на момент первого реального сохранения калькуляции (см. db._ensure_original_version).
-- Никогда не редактируется в месте — только читается через отдельный путь
-- "Исходные данные" в редакторе; в общий список версий (list_versions) не входит.

ALTER TABLE cost_calc_versions DROP CONSTRAINT cost_calc_versions_status_check;
ALTER TABLE cost_calc_versions ADD CONSTRAINT cost_calc_versions_status_check
    CHECK (status IN ('original', 'draft', 'pending', 'approved', 'rejected', 'archived'));

-- Не более одного 'original' на калькуляцию (защита от гонки при одновременном
-- первом сохранении двумя пользователями — второй INSERT просто не пройдёт).
CREATE UNIQUE INDEX IF NOT EXISTS idx_ver_one_original
    ON cost_calc_versions (model, articul, calc_sign, plan_id, "дата расчета")
    WHERE status = 'original';
