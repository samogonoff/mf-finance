-- 0042 — правки ТЗ 02.09.2026: сценарии, ЮЛ с УНП, состав розницы правилами.
--
-- Три справочника, каждый закрывает пункт финальных правок:
--
-- 1) dir_scenario (правка Розница §23). Сценариев ВОСЕМЬ, а не шесть, и форма
--    обязана понимать любое значение, которое встретит в приёмнике. Держим
--    справочником, а не константами в коде: сценарий приходит из данных.
--
-- 2) dir_legal_entity (правки Розница §15/§16, МП §19). План пишется на РЕАЛЬНОЕ
--    ЮЛ — УНП, а не на фиктивный код. Фиктивные коды (1 BY, 2 RU, 4 CN, 7 KZ,
--    8 UZ, 9 TR) остаются только для чтения исторических строк, поэтому у них
--    флаг historical_only: на запись их брать нельзя.
--
-- 3) dir_retail_scope (правка Розница §11). Состав розницы — ПРАВИЛО, а не
--    список магазинов: группа ЦФО «3.Магазины» плюс восемь ЦФО-отделов, минус
--    ЦФО 40 (интернет-магазин) и 722 (техническая запись переезда магазина в
--    Солигорске). Правило живёт строками, чтобы отдел добавлялся без релиза.

INSERT INTO plans_directory (code, source, sync_status) VALUES
    ('dir_scenario',     'manual', 'never'),
    ('dir_retail_scope', 'manual', 'never')
ON CONFLICT (code) DO NOTHING;

-- ── Сценарии (правка Розница §23) ───────────────────────────────────────────
DELETE FROM plans_directory_row
 WHERE directory_id = (SELECT id FROM plans_directory WHERE code = 'dir_scenario');

INSERT INTO plans_directory_row (directory_id, external_id, payload_json)
SELECT d.id, v.ext, v.payload FROM plans_directory d
CROSS JOIN (VALUES
    ('-1', '{"code": -1, "name": "Факт по начислениям",      "kind": "fact"}'::jsonb),
    ('0',  '{"code": 0,  "name": "Факт по платежам",         "kind": "fact"}'::jsonb),
    ('1',  '{"code": 1,  "name": "Стратегический бюджет",    "kind": "budget"}'::jsonb),
    ('2',  '{"code": 2,  "name": "Текущий бюджет",           "kind": "budget"}'::jsonb),
    ('3',  '{"code": 3,  "name": "Кризисный бюджет",         "kind": "budget"}'::jsonb),
    ('9',  '{"code": 9,  "name": "Ожид.факт по начислениям", "kind": "expected"}'::jsonb),
    ('10', '{"code": 10, "name": "Ожид.факт по платежам",    "kind": "expected"}'::jsonb),
    -- Сценарий тактики — тот, в котором работает наша форма.
    ('11', '{"code": 11, "name": "Тактика бюджет (таргеты)", "kind": "tactic"}'::jsonb)
) AS v(ext, payload)
WHERE d.code = 'dir_scenario';

-- ── ЮЛ с реальными УНП (правки Розница §15/§16, МП §19) ─────────────────────
-- Реальные ЮЛ контура: одно на страну, подтверждено запросом B1 и правками.
-- Уникального ограничения (directory_id, external_id) в plans_directory_row нет
-- (строки версионируются valid_from/valid_to), поэтому идемпотентность — своим
-- DELETE по ключам этой миграции, а не ON CONFLICT.
DELETE FROM plans_directory_row
 WHERE directory_id = (SELECT id FROM plans_directory WHERE code = 'dir_legal_entity')
   AND (external_id LIKE 'FICT-%' OR external_id IN
        ('BY-690719790', 'KZ-141240004842', 'RU-6950135110', 'UZ-305554644'));

INSERT INTO plans_directory_row (directory_id, external_id, payload_json)
SELECT d.id, v.ext, v.payload FROM plans_directory d
CROSS JOIN (VALUES
    ('BY-690719790',   '{"name": "ФОРМЭЛЬ ООО", "short": "F", "country": "BY", "unp": "690719790", "historical_only": false, "note": "план розницы РБ и маркетплейсов (ЦФО 250)"}'::jsonb),
    ('KZ-141240004842','{"name": "MFKaz", "short": "MFKaz", "country": "KZ", "unp": "141240004842", "historical_only": false, "note": "план розницы КЗ"}'::jsonb),
    ('RU-6950135110',  '{"name": "TDMF", "short": "TDMF", "country": "RU", "unp": "6950135110", "historical_only": false, "note": "план розницы РФ"}'::jsonb),
    ('UZ-305554644',   '{"name": "MFUz", "short": "MFUz", "country": "UZ", "unp": "305554644", "historical_only": false, "note": "план розницы УЗ"}'::jsonb),
    -- Фиктивные коды: только чтение исторических строк, на запись не брать.
    ('FICT-1', '{"name": "Фиктивный код BY", "country": "BY", "unp": "1", "historical_only": true, "note": "только чтение исторических строк"}'::jsonb),
    ('FICT-2', '{"name": "Фиктивный код RU", "country": "RU", "unp": "2", "historical_only": true, "note": "только чтение исторических строк"}'::jsonb),
    ('FICT-4', '{"name": "Фиктивный код CN", "country": "CN", "unp": "4", "historical_only": true, "note": "только чтение исторических строк"}'::jsonb),
    ('FICT-7', '{"name": "Фиктивный код KZ", "country": "KZ", "unp": "7", "historical_only": true, "note": "только чтение исторических строк"}'::jsonb),
    ('FICT-8', '{"name": "Фиктивный код UZ", "country": "UZ", "unp": "8", "historical_only": true, "note": "только чтение исторических строк"}'::jsonb),
    ('FICT-9', '{"name": "Фиктивный код TR", "country": "TR", "unp": "9", "historical_only": true, "note": "только чтение исторических строк"}'::jsonb)
) AS v(ext, payload)
WHERE d.code = 'dir_legal_entity';

-- ── Состав розницы правилами (правка Розница §11) ───────────────────────────
DELETE FROM plans_directory_row
 WHERE directory_id = (SELECT id FROM plans_directory WHERE code = 'dir_retail_scope');

INSERT INTO plans_directory_row (directory_id, external_id, payload_json)
SELECT d.id, v.ext, v.payload FROM plans_directory d
CROSS JOIN (VALUES
    -- Основа: группа ЦФО магазинов (в справочнике 'Магазины', в таблице плана
    -- '3.Магазины' — это РАЗНЫЕ значения одного и того же, см. ТЗ §6 п.1).
    ('group-3.Магазины', '{"kind": "include_group", "value": "3.Магазины", "note": "правило ALL_view: группа ЦФО магазинов"}'::jsonb),
    -- Восемь ЦФО-отделов, которые ALL_view относит к рознице.
    ('cfo-36',  '{"kind": "include_cfo", "value": "36",  "note": "ЦФО-отдел розницы (ALL_view)"}'::jsonb),
    ('cfo-102', '{"kind": "include_cfo", "value": "102", "note": "ЦФО-отдел розницы (ALL_view)"}'::jsonb),
    ('cfo-203', '{"kind": "include_cfo", "value": "203", "note": "ЦФО-отдел розницы (ALL_view)"}'::jsonb),
    ('cfo-205', '{"kind": "include_cfo", "value": "205", "note": "ЦФО-отдел розницы (ALL_view)"}'::jsonb),
    ('cfo-304', '{"kind": "include_cfo", "value": "304", "note": "ЦФО-отдел розницы (ALL_view)"}'::jsonb),
    ('cfo-305', '{"kind": "include_cfo", "value": "305", "note": "ЦФО-отдел розницы (ALL_view)"}'::jsonb),
    ('cfo-412', '{"kind": "include_cfo", "value": "412", "note": "ЦФО-отдел розницы (ALL_view)"}'::jsonb),
    ('cfo-435', '{"kind": "include_cfo", "value": "435", "note": "ЦФО-отдел розницы (ALL_view)"}'::jsonb),
    -- Исключения. Порядок применения: exclude сильнее include.
    ('excl-40',  '{"kind": "exclude_cfo", "value": "40",  "note": "интернет-магазин — не розница"}'::jsonb),
    ('excl-722', '{"kind": "exclude_cfo", "value": "722", "note": "техническая запись переезда магазина в Солигорске"}'::jsonb)
) AS v(ext, payload)
WHERE d.code = 'dir_retail_scope';
