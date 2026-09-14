-- Курсы валют на 2026 год (ответ И-9 от 11.09.2026).
--
-- Константу 3,5 из реестра командировок убрали ещё в предыдущем релизе — курс
-- берётся из справочника. Здесь заводятся начальные значения: они взяты из
-- курсовой таблицы самих эталонных книг (колонки 95–96 листов МП_KZ и МП_UZ) и
-- внутренне согласованы — 3,2 ÷ 515 и 3,2 ÷ 13 000 сходятся до знака.
--
-- ВНИМАНИЕ. Ответ 27 (10.09.2026) называл плановый курс доллара 3,5, а курсовая
-- таблица файла даёт 3,2. Числа реестра командировок (600 + 1 050 + 280 = 1 930)
-- получены по 3,5. Расхождение вынесено в вопрос 46 — до ответа справочник несёт
-- значения ФАЙЛА, потому что именно он назван источником начальных значений.
--
-- Автоматического источника у этих курсов нет: справочник ведёт финблок руками
-- (решение раздела «Принятые решения» того же ответа).

INSERT INTO plans_directory (code, source, sync_status)
VALUES ('dir_fx_rate', 'manual', 'seed')
ON CONFLICT (code) DO NOTHING;

-- Курсы к белорусскому рублю на каждый месяц 2026 года. Помесячно — потому что
-- форма фиксирует применённый курс в версии карточки: одна строка на год не
-- позволила бы поправить курс с середины года, не тронув уже утверждённое.
INSERT INTO plans_directory_row (directory_id, external_id, payload_json)
SELECT d.id,
       v.cur || '-2026-' || LPAD(m::text, 2, '0'),
       jsonb_build_object('currency', v.cur, 'year', 2026, 'month', m,
                          'rate_byn', v.rate, 'note', 'курсовая таблица эталона 2026')
  FROM plans_directory d
 CROSS JOIN (VALUES
        ('USD', 3.2),
        ('RUB', 0.0376),
        ('KZT', 0.006213592233009709),
        ('UZS', 0.00024615384615384614)
     ) AS v(cur, rate)
 CROSS JOIN generate_series(1, 12) AS m
 WHERE d.code = 'dir_fx_rate'
   AND NOT EXISTS (
        SELECT 1 FROM plans_directory_row r
         WHERE r.directory_id = d.id
           AND r.payload_json->>'currency' = v.cur
           AND (r.payload_json->>'year')::int = 2026
           AND (r.payload_json->>'month')::int = m);

-- Пара «валюта за доллар»: так курсы заданы в файле и так их правит финблок.
-- Хранится отдельным справочником, чтобы не смешивать с курсом к BYN: у них
-- разные знаменатели, и попытка держать их в одной колонке уже приводила к
-- ошибкам в других формах.
INSERT INTO plans_directory (code, source, sync_status)
VALUES ('dir_fx_per_usd', 'manual', 'seed')
ON CONFLICT (code) DO NOTHING;

DELETE FROM plans_directory_row
 WHERE directory_id = (SELECT id FROM plans_directory WHERE code = 'dir_fx_per_usd');

INSERT INTO plans_directory_row (directory_id, external_id, payload_json)
SELECT d.id, v.cur || '-2026', v.payload FROM plans_directory d
CROSS JOIN (VALUES
    ('KZT', '{"currency": "KZT", "year": 2026, "per_usd": 515, "note": "тенге за доллар; 3,2 ÷ 515 = курс к BYN"}'::jsonb),
    ('UZS', '{"currency": "UZS", "year": 2026, "per_usd": 13000, "note": "сум за доллар; 3,2 ÷ 13 000 = курс к BYN"}'::jsonb)
) AS v(cur, payload)
WHERE d.code = 'dir_fx_per_usd';
