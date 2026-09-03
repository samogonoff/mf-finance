-- Откат 0048: вернуть исследования к одиночным вопросам без диалога.
--
-- Журнал и результаты остаются, теряется только связь раундов между собой.
-- После откатa `/insights/ask` обязан игнорировать thread_id — иначе фронт будет
-- присылать ветку, которую негде хранить, и каждый уточняющий вопрос снова
-- начнётся с нуля (но уже молча, без ошибки).

DROP INDEX IF EXISTS cost_insight_questions_thread_idx;

ALTER TABLE cost_insight_questions
    DROP COLUMN IF EXISTS thread_id,
    DROP COLUMN IF EXISTS seq;
