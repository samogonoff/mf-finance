-- Откат 0047: вернуть синхронный запуск исследований.
--
-- Журнал и кэш остаются (0046), теряется только жизненный цикл задачи. После
-- откатa `/insights/ask` обязан снова отвечать синхронно — иначе клиент будет
-- получать job_id, по которому нечего опрашивать.
--
-- Индекс кэша возвращаем к виду 0046: без колонки status условие по ней
-- невалидно.

DROP INDEX IF EXISTS cost_insight_questions_cache_idx;
DROP INDEX IF EXISTS cost_insight_questions_running_idx;
DROP INDEX IF EXISTS cost_insight_questions_job_idx;

ALTER TABLE cost_insight_questions
    DROP COLUMN IF EXISTS job_id,
    DROP COLUMN IF EXISTS status,
    DROP COLUMN IF EXISTS progress,
    DROP COLUMN IF EXISTS updated_at;

CREATE INDEX IF NOT EXISTS cost_insight_questions_cache_idx
    ON cost_insight_questions (request_hash, created_at DESC)
    WHERE degraded = FALSE;
