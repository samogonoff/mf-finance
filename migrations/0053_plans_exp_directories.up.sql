-- Справочники этапа 1.5 по ответам аналитика 10.09.2026 (36 и 38).
--
-- 36. Справочник статей PL — ДВА уровня.
--   Первый («группа PL → статья → код PL») уже есть: dir_pl_line, наполняется из
--   Budgeting.dbo.CodePL. Согласования не требует, генерится выгрузкой.
--   Второй — «код PL → укрупнённая группа P&L» (ТЗ §12.2, четырнадцать подгрупп)
--   — заводится отдельным РЕДАКТИРУЕМЫМ полем pnl_group той же строки: это
--   решение НСИ, а не свойство источника, и правится без релиза.
--   Код PL без второго уровня — не ошибка: в своде он попадает в «Не
--   распределено» с подсветкой, публикацию не блокирует.
--
-- 38. Ставка соцвзносов НЕ нужна: реестр ФОТ содержит только «ФОТ чистый», а
--   налоги от выплат — самостоятельные статьи со своими суммами. Заводим только
--   суточные и норматив авиаперелёта.

-- ── Порядок групп PL (ТЗ §7.3): задаётся справочником, не алфавитом ──────────
INSERT INTO plans_directory (code, source, sync_status)
VALUES ('dir_pl_group', 'manual', 'seed')
ON CONFLICT (code) DO NOTHING;

DELETE FROM plans_directory_row
 WHERE directory_id = (SELECT id FROM plans_directory WHERE code = 'dir_pl_group');

INSERT INTO plans_directory_row (directory_id, external_id, payload_json)
SELECT d.id, v.ext, v.payload FROM plans_directory d
CROSS JOIN (VALUES
    ('retail-01',  '{"form_kind": "retail", "group_pl": "ВЫПЛАТЫ РАБОТНИКАМ", "sort_order": 10}'::jsonb),
    ('retail-02',  '{"form_kind": "retail", "group_pl": "ВСПОМОГАТЕЛЬНЫЕ РАСХОДЫ", "sort_order": 20}'::jsonb),
    ('retail-03',  '{"form_kind": "retail", "group_pl": "ПРОЧИЕ РАСХОДЫ", "sort_order": 30}'::jsonb),
    ('retail-04',  '{"form_kind": "retail", "group_pl": "ВЛОЖЕНИЯ", "sort_order": 40}'::jsonb),
    ('retail-05',  '{"form_kind": "retail", "group_pl": "АМОРТИЗАЦИЯ", "sort_order": 50}'::jsonb),
    ('retail-06',  '{"form_kind": "retail", "group_pl": "ОСНОВНЫЕ РАСХОДЫ", "sort_order": 60}'::jsonb),
    ('retail-07',  '{"form_kind": "retail", "group_pl": "ПРОЧИЕ ДОХОДЫ", "sort_order": 70}'::jsonb),
    ('retail-08',  '{"form_kind": "retail", "group_pl": "АРЕНДА", "sort_order": 80}'::jsonb),
    ('retail-09',  '{"form_kind": "retail", "group_pl": "НАЛОГИ", "sort_order": 90}'::jsonb),
    ('retail-10',  '{"form_kind": "retail", "group_pl": "БЕЗВОЗМЕЗДНАЯ ПОМОЩЬ", "sort_order": 100}'::jsonb),
    ('retail-11',  '{"form_kind": "retail", "group_pl": "РЕКЛАМА И МАРКЕТИНГ", "sort_order": 110}'::jsonb),
    ('retail-12',  '{"form_kind": "retail", "group_pl": "РЕЗУЛЬТАТЫ ИНВЕНТАРИЗАЦИИ", "sort_order": 120}'::jsonb),
    ('retail-13',  '{"form_kind": "retail", "group_pl": "ПРОЦЕНТЫ (Депозиты, вложения, ЦБ)", "sort_order": 130}'::jsonb),
    ('retail-14',  '{"form_kind": "retail", "group_pl": "ПРОЦЕНТЫ (займы, кредиты)", "sort_order": 140}'::jsonb),
    ('mp-01',      '{"form_kind": "mp", "group_pl": "ВЫПЛАТЫ РАБОТНИКАМ", "sort_order": 10}'::jsonb),
    ('mp-02',      '{"form_kind": "mp", "group_pl": "ВСПОМОГАТЕЛЬНЫЕ РАСХОДЫ", "sort_order": 20}'::jsonb),
    ('mp-03',      '{"form_kind": "mp", "group_pl": "ПРОЧИЕ РАСХОДЫ", "sort_order": 30}'::jsonb),
    ('mp-04',      '{"form_kind": "mp", "group_pl": "ВЛОЖЕНИЯ", "sort_order": 40}'::jsonb),
    ('mp-05',      '{"form_kind": "mp", "group_pl": "АМОРТИЗАЦИЯ", "sort_order": 50}'::jsonb),
    ('mp-06',      '{"form_kind": "mp", "group_pl": "ОСНОВНЫЕ РАСХОДЫ", "sort_order": 60}'::jsonb),
    ('mp-07',      '{"form_kind": "mp", "group_pl": "АРЕНДА", "sort_order": 70}'::jsonb),
    ('mp-08',      '{"form_kind": "mp", "group_pl": "РЕКЛАМА И МАРКЕТИНГ", "sort_order": 80}'::jsonb)
) AS v(ext, payload)
WHERE d.code = 'dir_pl_group';

-- ── Второй уровень: код PL → укрупнённая группа P&L (ТЗ §12.3) ───────────────
-- Заполняется по перечислению ТЗ. Коды, которых в перечислении нет, остаются без
-- значения — это и есть «Не распределено» в своде.
UPDATE plans_directory_row r
   SET payload_json = jsonb_set(r.payload_json, '{pnl_group}', to_jsonb(v.grp))
  FROM plans_directory d,
       (VALUES
            (24,'Заработная плата'),(26,'Заработная плата'),(78,'Заработная плата'),
            (79,'Заработная плата'),(80,'Заработная плата'),(81,'Заработная плата'),
            (86,'Заработная плата'),(87,'Заработная плата'),(88,'Заработная плата'),
            (89,'Заработная плата'),
            (54,'Аренда'),(55,'Аренда'),
            (50,'Логистические расходы'),(51,'Логистические расходы'),
            (52,'Логистические расходы'),(93,'Логистические расходы'),
            (48,'Расходы на ИТ'),(49,'Расходы на ИТ'),(85,'Расходы на ИТ'),
            (10,'Реклама и маркетинг'),(12,'Реклама и маркетинг'),(13,'Реклама и маркетинг'),
            (14,'Реклама и маркетинг'),(15,'Реклама и маркетинг'),(16,'Реклама и маркетинг'),
            (17,'Реклама и маркетинг'),(18,'Реклама и маркетинг'),(19,'Реклама и маркетинг'),
            (20,'Реклама и маркетинг'),(22,'Реклама и маркетинг'),(23,'Реклама и маркетинг'),
            (91,'Реклама и маркетинг'),
            (58,'Банковские расходы'),(83,'Банковские расходы'),
            (45,'Расходы на магазины'),(84,'Расходы на магазины'),
            (67,'Амортизация')
       ) AS v(code_pl, grp)
 WHERE d.id = r.directory_id
   AND d.code = 'dir_pl_line'
   AND r.payload_json->>'code_pl' = v.code_pl::text;

-- ── Суточные (ответ 38): страна, период, сумма, валюта ───────────────────────
INSERT INTO plans_directory (code, source, sync_status)
VALUES ('dir_per_diem', 'manual', 'seed')
ON CONFLICT (code) DO NOTHING;

DELETE FROM plans_directory_row
 WHERE directory_id = (SELECT id FROM plans_directory WHERE code = 'dir_per_diem');

INSERT INTO plans_directory_row (directory_id, external_id, payload_json)
SELECT d.id, v.ext, v.payload FROM plans_directory d
CROSS JOIN (VALUES
    ('RU-2026', '{"country": "RU", "amount": 40, "currency": "USD", "from_year": 2026, "note": "по эталону 2026"}'::jsonb),
    ('KZ-2026', '{"country": "KZ", "amount": 50, "currency": "USD", "from_year": 2026, "note": "по эталону 2026"}'::jsonb),
    ('UZ-2026', '{"country": "UZ", "amount": 45, "currency": "USD", "from_year": 2026, "note": "по эталону 2026"}'::jsonb)
) AS v(ext, payload)
WHERE d.code = 'dir_per_diem';

-- ── Норматив авиаперелёта в одну сторону из РБ (ответ 38) ────────────────────
-- Ключ — страна И ГОРОД: у Узбекистана Ташкент и Фергана стоят по-разному.
INSERT INTO plans_directory (code, source, sync_status)
VALUES ('dir_flight_norm', 'manual', 'seed')
ON CONFLICT (code) DO NOTHING;

DELETE FROM plans_directory_row
 WHERE directory_id = (SELECT id FROM plans_directory WHERE code = 'dir_flight_norm');

INSERT INTO plans_directory_row (directory_id, external_id, payload_json)
SELECT d.id, v.ext, v.payload FROM plans_directory d
CROSS JOIN (VALUES
    ('RU',         '{"country": "RU", "city": "", "amount": 300, "currency": "BYN", "note": "в одну сторону из РБ"}'::jsonb),
    ('KZ',         '{"country": "KZ", "city": "", "amount": 1000, "currency": "BYN", "note": "в одну сторону из РБ"}'::jsonb),
    ('UZ-Ташкент', '{"country": "UZ", "city": "Ташкент", "amount": 1000, "currency": "BYN", "note": "в одну сторону из РБ"}'::jsonb),
    ('UZ-Фергана', '{"country": "UZ", "city": "Фергана", "amount": 1300, "currency": "BYN", "note": "в одну сторону из РБ"}'::jsonb)
) AS v(ext, payload)
WHERE d.code = 'dir_flight_norm';
