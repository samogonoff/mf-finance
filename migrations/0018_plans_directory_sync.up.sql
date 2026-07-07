-- 0018 — синхронизация справочников Лиса/1С + кэш + журнал + каталог должностей.
-- ТЗ §«Справочники: Лиса и 1С» (DIR-01…05), §«UI справочников», §«Разграничение
-- прав» (две группы ролей: системные + назначаемые должности).

-- 1. Метаданные справочника: версия, диапазон действия (DIR-02), конфиг кэша/stale
--    (DIR-05), состояние backoff-повтора (DIR-04).
ALTER TABLE plans_directory ADD COLUMN IF NOT EXISTS version             INT NOT NULL DEFAULT 0;
ALTER TABLE plans_directory ADD COLUMN IF NOT EXISTS valid_from          DATE;
ALTER TABLE plans_directory ADD COLUMN IF NOT EXISTS valid_to            DATE;
ALTER TABLE plans_directory ADD COLUMN IF NOT EXISTS cache_ttl_seconds   INT  NOT NULL DEFAULT 3600;  -- TTL Redis-кэша строк
ALTER TABLE plans_directory ADD COLUMN IF NOT EXISTS stale_after_seconds INT  NOT NULL DEFAULT 86400; -- после — sync_status=stale
ALTER TABLE plans_directory ADD COLUMN IF NOT EXISTS retry_count         INT  NOT NULL DEFAULT 0;
ALTER TABLE plans_directory ADD COLUMN IF NOT EXISTS next_retry_at       TIMESTAMPTZ;
ALTER TABLE plans_directory ADD COLUMN IF NOT EXISTS last_sync_added     INT  NOT NULL DEFAULT 0;
ALTER TABLE plans_directory ADD COLUMN IF NOT EXISTS last_sync_changed   INT  NOT NULL DEFAULT 0;
ALTER TABLE plans_directory ADD COLUMN IF NOT EXISTS last_sync_removed   INT  NOT NULL DEFAULT 0;

-- 2. Журнал синхронизаций (DIR-03/04 + UI «журнал синхронизации»).
CREATE TABLE IF NOT EXISTS plans_dir_sync_log (
    id            BIGSERIAL PRIMARY KEY,
    directory_id  BIGINT NOT NULL REFERENCES plans_directory(id) ON DELETE CASCADE,
    started_at    TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    finished_at   TIMESTAMPTZ,
    status        TEXT NOT NULL DEFAULT 'running',  -- running|ok|error
    triggered_by  TEXT NOT NULL DEFAULT 'cron',     -- cron|manual:<user_id>
    rows_in       INT  NOT NULL DEFAULT 0,
    rows_added    INT  NOT NULL DEFAULT 0,
    rows_changed  INT  NOT NULL DEFAULT 0,
    rows_removed  INT  NOT NULL DEFAULT 0,
    duration_ms   INT  NOT NULL DEFAULT 0,
    error         TEXT NOT NULL DEFAULT '',
    diff_sample   JSONB NOT NULL DEFAULT '[]'       -- до N примеров изменений (added/changed/removed)
);
CREATE INDEX IF NOT EXISTS idx_plans_dir_sync_log_dir ON plans_dir_sync_log(directory_id, started_at DESC);

-- 3. Каталог назначаемых должностей (редактируемый администратором ТП).
--    Системные роли (Администратор, Администратор ТП, Участник) живут в auth/roles.go
--    и сюда НЕ входят — здесь только бизнес-должности процесса из схемы ТЗ.
CREATE TABLE IF NOT EXISTS plans_position (
    code               TEXT PRIMARY KEY,           -- filler|dept_head|director|…
    name               TEXT NOT NULL,              -- человекочитаемое имя должности
    description        TEXT NOT NULL DEFAULT '',
    default_stage_code TEXT NOT NULL DEFAULT '',   -- этап схемы по умолчанию ('1.1'…'4')
    track              TEXT NOT NULL DEFAULT '',   -- sales|production|final|nsi
    sort_order         INT  NOT NULL DEFAULT 100,
    created_at         TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

-- Должности из ТЗ §«Системные роли» + матрицы §ACL (преднаполнение; редактируемо).
INSERT INTO plans_position (code, name, description, default_stage_code, track, sort_order) VALUES
    ('filler',         'Ответственный за заполнение ЦП/ЦЗ', 'Ввод и корректировка показателей своего разреза (этап 1.1/2.1/2.3)', '1.1', 'sales',      10),
    ('dept_head',      'Руководитель подразделения',        'Согласование этапа 1.2 по своему ЦП',                                '1.2', 'sales',      20),
    ('director',       'Директор направления',              'Согласование сводного потока продаж (этап 1.3)',                     '1.3', 'sales',      30),
    ('sales_final',    'Финальный согласующий продаж',      'Закрытие редактирования потока продаж (этап 1.4)',                   '1.4', 'sales',      40),
    ('coordinator',    'Координатор бюджета',               'Обновление расходов, свод по ЮЛ/каналам (этапы 1.5, 1.6, 2.4)',      '1.5', 'sales',      50),
    ('prod_planner',   'Планирование производства',         'Минуты и объёмы производства (этапы 2.1–2.2)',                       '2.1', 'production', 60),
    ('prod_approver',  'Согласующий производства',          'Согласование планов производства (этап 2.2)',                        '2.2', 'production', 70),
    ('le_approver',    'Согласующий бюджета ЮЛ',            'Согласование бюджета своего ЮЛ (этап 3)',                            '3',   'final',      80),
    ('final_approver', 'Итоговый утверждающий',             'Итоговое утверждение планов по ЮЛ и каналам (этап 4)',               '4',   'final',      90),
    ('auditor',        'Аудитор',                           'Просмотр истории изменений (read-only)',                             '',    'nsi',       100)
ON CONFLICT (code) DO NOTHING;

-- 4. Регистрируем недостающие справочники-источники под новые провайдеры sync.
--    (dir_store_to / dir_lfl из Лисы; dir_fx_rate переводим в источник lisa).
INSERT INTO plans_directory (code, source, sync_status) VALUES
    ('dir_store_to', 'lisa', 'never'),
    ('dir_lfl',      'lisa', 'never')
ON CONFLICT (code) DO NOTHING;
