-- 0047 — курсы по СЦЕНАРИЯМ: таблица «курс тактика» прототипа и плановый курс.
--
-- Найдено 07.09.2026: кабинет пересчитывал все колонки двумя константами —
-- 0,0376 (RUB) и 3,2 (USD). По маркетплейсам эти числа верные: ровно их даёт
-- таблица «курс тактика» файла «Тактический план_large_МП_2026_июнь.xlsx»
-- (лист Списки_для_фильтров, колонки N:Q, сценарий «Тактика бюджет (таргеты)»),
-- где с 06.2025 курс заморожен. Но держались они константами, а не данными.
--
-- По рознице те же числа НЕВЕРНЫ: в файле «ТО 2025-2026+ (3).xlsx» 3,2 и
-- 3,2/0,0376 стоят в колонке U под подписью «план 26», а тактика пересчитывается
-- курсом V2 = `Май_26!W2`, то есть курсом ФАКТА ПРЕДЫДУЩЕГО МЕСЯЦА (для июня
-- 2026 — 2,7806). Разделить их можно, только когда сценарий — измерение
-- справочника; код выбирает строку по сценарию (rates.go, pickFx).
--
-- Заполняем то, что подтверждено файлом:
--   * tactic — помесячные курсы 2025 года и заморозка с 06.2025 по 12.2025;
--   * plan   — плановый курс «план 26» (3,2 и 0,0376), им пересчитывается
--              стратегия.
--
-- Строк на 2026 год в таблице файла НЕТ — продлевать её или перейти на
-- Budgeting.dbo.ExRateFactTarget решает заказчик (вопрос 27,
-- docs/reports/plans/open-questions.md). До ответа за 2026 работает общая строка
-- справочника, то есть прежнее поведение, и это видно в данных, а не в коде.
--
-- Идемпотентно: DELETE по ключам этой миграции + INSERT.

DELETE FROM plans_directory_row
 WHERE directory_id = (SELECT id FROM plans_directory WHERE code = 'dir_fx_rate')
   AND external_id LIKE ANY (ARRAY['TACTIC-%', 'PLAN-%']);

INSERT INTO plans_directory_row (directory_id, external_id, payload_json)
SELECT d.id, v.ext, v.payload FROM plans_directory d
CROSS JOIN (VALUES
    -- Тактика 2025, помесячно (файл МП, N:Q). Январь–май — свои курсы,
    -- с июня по декабрь таблица заморожена.
    ('TACTIC-RUB-2025-1',  '{"currency": "RUB", "year": 2025, "month": 1,  "rate_byn": 0.033681,  "scenario": "tactic"}'::jsonb),
    ('TACTIC-RUB-2025-2',  '{"currency": "RUB", "year": 2025, "month": 2,  "rate_byn": 0.033905,  "scenario": "tactic"}'::jsonb),
    ('TACTIC-RUB-2025-3',  '{"currency": "RUB", "year": 2025, "month": 3,  "rate_byn": 0.0350787, "scenario": "tactic"}'::jsonb),
    ('TACTIC-RUB-2025-4',  '{"currency": "RUB", "year": 2025, "month": 4,  "rate_byn": 0.0362469, "scenario": "tactic"}'::jsonb),
    ('TACTIC-RUB-2025-5',  '{"currency": "RUB", "year": 2025, "month": 5,  "rate_byn": 0.0365634, "scenario": "tactic"}'::jsonb),
    ('TACTIC-RUB-2025-6',  '{"currency": "RUB", "year": 2025, "month": 6,  "rate_byn": 0.0376,    "scenario": "tactic"}'::jsonb),
    ('TACTIC-RUB-2025-7',  '{"currency": "RUB", "year": 2025, "month": 7,  "rate_byn": 0.0376,    "scenario": "tactic"}'::jsonb),
    ('TACTIC-RUB-2025-8',  '{"currency": "RUB", "year": 2025, "month": 8,  "rate_byn": 0.0376,    "scenario": "tactic"}'::jsonb),
    ('TACTIC-RUB-2025-9',  '{"currency": "RUB", "year": 2025, "month": 9,  "rate_byn": 0.0376,    "scenario": "tactic"}'::jsonb),
    ('TACTIC-RUB-2025-10', '{"currency": "RUB", "year": 2025, "month": 10, "rate_byn": 0.0376,    "scenario": "tactic"}'::jsonb),
    ('TACTIC-RUB-2025-11', '{"currency": "RUB", "year": 2025, "month": 11, "rate_byn": 0.0376,    "scenario": "tactic"}'::jsonb),
    ('TACTIC-RUB-2025-12', '{"currency": "RUB", "year": 2025, "month": 12, "rate_byn": 0.0376,    "scenario": "tactic"}'::jsonb),
    ('TACTIC-USD-2025-1',  '{"currency": "USD", "year": 2025, "month": 1,  "rate_byn": 3.4651,    "scenario": "tactic"}'::jsonb),
    ('TACTIC-USD-2025-2',  '{"currency": "USD", "year": 2025, "month": 2,  "rate_byn": 3.4579,    "scenario": "tactic"}'::jsonb),
    ('TACTIC-USD-2025-3',  '{"currency": "USD", "year": 2025, "month": 3,  "rate_byn": 3.305,     "scenario": "tactic"}'::jsonb),
    ('TACTIC-USD-2025-4',  '{"currency": "USD", "year": 2025, "month": 4,  "rate_byn": 3.1625,    "scenario": "tactic"}'::jsonb),
    ('TACTIC-USD-2025-5',  '{"currency": "USD", "year": 2025, "month": 5,  "rate_byn": 3.0971,    "scenario": "tactic"}'::jsonb),
    ('TACTIC-USD-2025-6',  '{"currency": "USD", "year": 2025, "month": 6,  "rate_byn": 3.2,       "scenario": "tactic"}'::jsonb),
    ('TACTIC-USD-2025-7',  '{"currency": "USD", "year": 2025, "month": 7,  "rate_byn": 3.2,       "scenario": "tactic"}'::jsonb),
    ('TACTIC-USD-2025-8',  '{"currency": "USD", "year": 2025, "month": 8,  "rate_byn": 3.2,       "scenario": "tactic"}'::jsonb),
    ('TACTIC-USD-2025-9',  '{"currency": "USD", "year": 2025, "month": 9,  "rate_byn": 3.2,       "scenario": "tactic"}'::jsonb),
    ('TACTIC-USD-2025-10', '{"currency": "USD", "year": 2025, "month": 10, "rate_byn": 3.2,       "scenario": "tactic"}'::jsonb),
    ('TACTIC-USD-2025-11', '{"currency": "USD", "year": 2025, "month": 11, "rate_byn": 3.2,       "scenario": "tactic"}'::jsonb),
    ('TACTIC-USD-2025-12', '{"currency": "USD", "year": 2025, "month": 12, "rate_byn": 3.2,       "scenario": "tactic"}'::jsonb),
    -- Плановый курс «план 26» (файл ТО, колонка U): им пересчитывается стратегия.
    ('PLAN-RUB-0-0', '{"currency": "RUB", "year": 0, "month": 0, "rate_byn": 0.0376, "scenario": "plan"}'::jsonb),
    ('PLAN-USD-0-0', '{"currency": "USD", "year": 0, "month": 0, "rate_byn": 3.2,    "scenario": "plan"}'::jsonb)
) AS v(ext, payload)
WHERE d.code = 'dir_fx_rate';
