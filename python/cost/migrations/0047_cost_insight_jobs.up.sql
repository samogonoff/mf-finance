-- 0047 — асинхронный запуск углублённых исследований
--
-- Исследование (`/insights/ask`, app/insight_agent.py) идёт по шагам и штатно
-- занимает от 20 секунд до пары минут. Синхронный ответ этого не выдерживает:
-- у `location /api/cost/` в swarm/config/cost.conf не задан proxy_read_timeout,
-- то есть действует дефолтный минутный, и запрос на 84 секунды бэкенд досчитал,
-- а клиент получил 504 (02.09.2026). Подгонять длительность исследования под
-- таймаут прокси — лечить симптом, поэтому запуск разделён на два обращения:
-- POST стартует задачу и сразу отдаёт job_id, GET отдаёт статус и результат.
--
-- ПОЧЕМУ СОСТОЯНИЕ В БАЗЕ, А НЕ В ПАМЯТИ ПРОЦЕССА
-- ===============================================
-- Реплика у python-cost одна, но деплой идёт с `order: start-first`: на время
-- обновления работают ДВА контейнера, и опрос статуса может попасть в новый, где
-- задачи в памяти нет. То же после любого рестарта. Поэтому задача живёт в
-- строке таблицы, и любой контейнер отвечает про неё одинаково.
--
-- Колонки добавляются к cost_insight_questions (0046), а не в отдельную таблицу:
-- это тот же объект — исследование, — просто теперь у него есть жизненный цикл.
-- Строка создаётся сразу со status='running' и дописывается по мере работы;
-- прогресс обновляется после каждого шага, чтобы пользователь видел, что
-- происходит, а не пустой спиннер.

ALTER TABLE cost_insight_questions
    -- Публичный идентификатор для опроса. Отдельно от id: последовательный id
    -- позволял бы перебором читать чужие исследования.
    ADD COLUMN IF NOT EXISTS job_id     UUID,
    ADD COLUMN IF NOT EXISTS status     TEXT NOT NULL DEFAULT 'done',
    -- {"step": 3, "tool": "drill", "sql_calls": 2} — что агент делает сейчас.
    ADD COLUMN IF NOT EXISTS progress   JSONB NOT NULL DEFAULT '{}'::jsonb,
    ADD COLUMN IF NOT EXISTS updated_at TIMESTAMPTZ NOT NULL DEFAULT now();

-- Существующие записи (0046) — это завершённые синхронные исследования, поэтому
-- дефолт статуса именно 'done': иначе они выглядели бы как вечно висящие.
UPDATE cost_insight_questions SET job_id = gen_random_uuid() WHERE job_id IS NULL;

ALTER TABLE cost_insight_questions
    ALTER COLUMN job_id SET NOT NULL;

-- Опрос статуса идёт по job_id и должен быть точечным.
CREATE UNIQUE INDEX IF NOT EXISTS cost_insight_questions_job_idx
    ON cost_insight_questions (job_id);

-- Поиск зависших задач: «сколько сейчас выполняется» (ограничитель на
-- одновременные исследования) и уборка тех, чей процесс умер на середине.
CREATE INDEX IF NOT EXISTS cost_insight_questions_running_idx
    ON cost_insight_questions (updated_at DESC)
    WHERE status = 'running';

-- Кэш обязан отдавать только ЗАВЕРШЁННЫЕ исследования: индекс 0046 отбирал по
-- degraded=FALSE, а у только что стартовавшей задачи degraded тоже FALSE — без
-- условия по статусу кэш возвращал бы пустой результат работающей задачи.
DROP INDEX IF EXISTS cost_insight_questions_cache_idx;
CREATE INDEX IF NOT EXISTS cost_insight_questions_cache_idx
    ON cost_insight_questions (request_hash, created_at DESC)
    WHERE degraded = FALSE AND status = 'done';

COMMENT ON COLUMN cost_insight_questions.status IS
    'running — агент работает, done — завершено, failed — не удалось. Состояние в БД, а не в памяти: деплой идёт start-first, и опрос может попасть в другой контейнер.';
