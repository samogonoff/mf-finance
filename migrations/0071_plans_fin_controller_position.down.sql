-- Роль удаляется вместе с привязкой к этапу (ON DELETE CASCADE), но только пока
-- на неё никто не назначен: назначения — данные администратора, откат схемы не
-- должен молча лишать человека шага контролёра.
DELETE FROM plans_position p
 WHERE p.code = 'fin_controller'
   AND NOT EXISTS (SELECT 1 FROM plans_user_scope s WHERE s.role = p.code);
