-- 0017 — конфигурация маршрута (ответственные по этапам) + регистрация
-- справочников из Лисы/1С (read-only, синхронизируемые) рядом с manual-НСИ.
-- ТЗ §5 (ответственные), §7.1 (источники: lisa|1c|manual|calculated), §«UI справочников».

CREATE TABLE IF NOT EXISTS plans_route_config (
    stage_code  TEXT PRIMARY KEY,            -- '1.1'…'4'
    responsible TEXT NOT NULL DEFAULT '',     -- ответственные/согласующие (метки из схемы ТЗ)
    due_rd      INT,                          -- N-й р.д. (для справки/переопределения)
    updated_by  BIGINT REFERENCES users(id),
    updated_at  TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

-- Ответственные по схеме ТЗ (Приложение A, §4.2–4.4). Редактируются админом процессов.
INSERT INTO plans_route_config (stage_code, responsible, due_rd) VALUES
    ('1.1', 'Смолер, Ткачева, Галькевич, Бединская, Мурашко (МП large), Левин/Качановская (МП small), Пистоленко, Сипаров Ю.Г.', 2),
    ('1.2', 'Руководители ЦП: Смолер, Ворончук, Левин', 3),
    ('1.3', 'Дегтерева Е.В.', 3),
    ('1.4', 'Сипаров С.Г.', 4),
    ('1.5', 'Антипова О.В. + статьи (Маркетинг, IT, HR, Логистика)', 5),
    ('1.6', 'Антипова О.В.', 5),
    ('2.1', 'Планирование производства', NULL),
    ('2.2', 'Захарченко Н.М.', NULL),
    ('2.3', 'Сериков, Счастная, Сипаров В.Ю. (по странам)', 3),
    ('2.4', 'Антипова О.В. (зам. Осипович)', 4),
    ('3',   'Согласующие ЮЛ: Захарченко, Дегтерева, Мавлянов, Командиров, Левин, Сметанин, Акаева', 6),
    ('4',   'Сипарова С.Г., Сериков А.Г.', 6)
ON CONFLICT (stage_code) DO NOTHING;

-- Справочники из Лисы/1С (read-only, синхронизируемые) — регистрируем рядом с manual.
INSERT INTO plans_directory (code, source, sync_status) VALUES
    ('dir_lisa_prod_units', 'lisa', 'never'),   -- производственные подразделения/линии
    ('dir_lisa_norms',      'lisa', 'never'),   -- нормы времени (мин/ед.)
    ('dir_lisa_shipments',  'lisa', 'never'),   -- объёмы отгрузки
    ('dir_lisa_routes',     'lisa', 'never'),   -- маршруты доставки
    ('dir_lisa_tariffs',    'lisa', 'never'),   -- тарифы логистики
    ('dir_lease',           '1c',   'never'),   -- договоры аренды + условия
    ('dir_payroll_rate',    '1c',   'never'),   -- ставки ФОТ, коэффициенты, соцвзносы
    ('dir_headcount',       'lisa', 'never')    -- численность по ЦФО
ON CONFLICT (code) DO NOTHING;
