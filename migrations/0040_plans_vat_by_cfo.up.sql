-- 0040 — эффективные ставки НДС по площадкам: ответ аналитика от 28.08.2026.
--
-- Было (сид 0034, из ТЗ МП §3.4): 20,36 % у WB/Lamoda/Ozon и 16,62 % у Yandex
-- Market. Не подтвердилось. Ставка привязана к КОДУ ЦФО и считается в контуре
-- [ALL] парой кодов: 1 − КодPL 1006 / КодPL 1022, где 1006 — «ПРОДАЖИ /
-- Маркет-плэйсы» (без НДС), 1022 — «ПРОДАЖИ с НДС с самовыв_ / Маркет-плэйсы».
-- Из представления факта МП ставку получить нельзя: AmountWithVATCurrencyFact и
-- AmountWOVATCurrencyFact там совпадают до копейки, эффективная ставка выходит 0.
--
-- Закономерность: основной российский контур ≈ 19,6 %, контур ТЕКС ≈ 18,7 %,
-- Казахстан ровно 16 %, Узбекистан ровно 12 %. Внутри одного ЦФО ставка ещё и
-- отличается от товара к товару (DWH.dbo.ITEM_NDS_WB по nmId) — значения ниже
-- средневзвешенные, поэтому они и живут в справочнике, а не в коде.
--
-- Открытым остаётся регламент на плановый период (последний закрытый месяц /
-- среднее за 12 месяцев / задаёт финблок) — вопрос 14, docs/reports/plans/open-questions.md.

DELETE FROM plans_directory_row
 WHERE directory_id = (SELECT id FROM plans_directory WHERE code = 'dir_vat')
   AND (payload_json->>'code_cfo')::int <> 0;

INSERT INTO plans_directory_row (directory_id, external_id, payload_json)
SELECT d.id, v.ext, v.payload FROM plans_directory d
CROSS JOIN (VALUES
    -- Основной российский контур.
    ('RU-335', '{"country": "RU", "code_cfo": 335, "name": "Wildberries", "vat_rate": 0.1960, "source": "1 − КодPL 1006/1022 (ответ 28.08.2026)", "note": ""}'::jsonb),
    ('RU-336', '{"country": "RU", "code_cfo": 336, "name": "Lamoda", "vat_rate": 0.1965, "source": "1 − КодPL 1006/1022 (ответ 28.08.2026)", "note": ""}'::jsonb),
    ('RU-337', '{"country": "RU", "code_cfo": 337, "name": "Ozon", "vat_rate": 0.1960, "source": "1 − КодPL 1006/1022 (ответ 28.08.2026)", "note": ""}'::jsonb),
    -- Контур ТЕКС.
    ('RU-953', '{"country": "RU", "code_cfo": 953, "name": "Детский мир", "vat_rate": 0.1872, "source": "1 − КодPL 1006/1022 (ответ 28.08.2026)", "note": ""}'::jsonb),
    ('RU-954', '{"country": "RU", "code_cfo": 954, "name": "Yandex Market", "vat_rate": 0.1871, "source": "1 − КодPL 1006/1022 (ответ 28.08.2026)", "note": ""}'::jsonb),
    ('RU-955', '{"country": "RU", "code_cfo": 955, "name": "Золотое яблоко", "vat_rate": 0.1868, "source": "1 − КодPL 1006/1022 (ответ 28.08.2026)", "note": ""}'::jsonb),
    ('RU-957', '{"country": "RU", "code_cfo": 957, "name": "Yandex Market ТЕКС", "vat_rate": 0.1870, "source": "1 − КодPL 1006/1022 (ответ 28.08.2026)", "note": ""}'::jsonb),
    ('RU-958', '{"country": "RU", "code_cfo": 958, "name": "Lamoda ТЕКС", "vat_rate": 0.1869, "source": "1 − КодPL 1006/1022 (ответ 28.08.2026)", "note": ""}'::jsonb),
    ('RU-990', '{"country": "RU", "code_cfo": 990, "name": "Wildberries ТЕКС", "vat_rate": 0.1868, "source": "1 − КодPL 1006/1022 (ответ 28.08.2026)", "note": ""}'::jsonb),
    ('RU-991', '{"country": "RU", "code_cfo": 991, "name": "Ozon ТЕКС", "vat_rate": 0.1868, "source": "1 − КодPL 1006/1022 (ответ 28.08.2026)", "note": ""}'::jsonb),
    -- Казахстан и Узбекистан — ровные ставки.
    ('KZ-338', '{"country": "KZ", "code_cfo": 338, "name": "Kaspi", "vat_rate": 0.1600, "source": "1 − КодPL 1006/1022 (ответ 28.08.2026)", "note": ""}'::jsonb),
    ('KZ-474', '{"country": "KZ", "code_cfo": 474, "name": "Ozon KZ", "vat_rate": 0.1600, "source": "1 − КодPL 1006/1022 (ответ 28.08.2026)", "note": ""}'::jsonb),
    ('KZ-475', '{"country": "KZ", "code_cfo": 475, "name": "Wildberries KZ", "vat_rate": 0.1600, "source": "1 − КодPL 1006/1022 (ответ 28.08.2026)", "note": ""}'::jsonb),
    ('UZ-339', '{"country": "UZ", "code_cfo": 339, "name": "Uzmarket", "vat_rate": 0.1200, "source": "1 − КодPL 1006/1022 (ответ 28.08.2026)", "note": ""}'::jsonb)
) AS v(ext, payload)
WHERE d.code = 'dir_vat'
ON CONFLICT DO NOTHING;
