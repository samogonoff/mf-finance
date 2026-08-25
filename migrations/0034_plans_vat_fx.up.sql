-- 0034 — справочники «Ставка НДС» и помесячные курсы тактики.
--
-- НДС. ТЗ МП §3.4: эффективная ставка СВОЯ у каждой площадки и НЕ равна
-- законодательной (20,36 % у Wildberries/Lamoda/Ozon, 16,62 % у Yandex Market —
-- «вероятно, смесь товарных групп с разными ставками»), поэтому прошивать её в
-- формулу нельзя. В коде ставка была хардкодом vatByCountry() (KZ/UZ 12 %, иначе 20 %).
-- Теперь: справочник dir_vat со ставкой по (страна, код ЦФО), где code_cfo=0 —
-- страновой дефолт, а строка площадки его переопределяет.
--
-- Курсы. ТЗ МП §3.3: «курс тактики берётся из справочника курсов по месяцу и валюте;
-- применённый курс фиксируется вместе с утверждённой версией». Раньше dir_fx_rate
-- держал один курс на валюту (и расчёт вообще читал хардкод FxRateSeed).
-- Добавляем год/месяц (0 = дефолт «на любой месяц») и валюты KZT/UZS — без них
-- ввод в валюте площадки (ТЗ МП §3.3, МП-10) невозможен.

INSERT INTO plans_directory (code, source, sync_status)
VALUES ('dir_vat', 'manual', 'seed')
ON CONFLICT (code) DO NOTHING;

DELETE FROM plans_directory_row WHERE directory_id = (SELECT id FROM plans_directory WHERE code = 'dir_vat');
INSERT INTO plans_directory_row (directory_id, external_id, payload_json)
SELECT d.id, v.ext, v.payload FROM plans_directory d
CROSS JOIN (VALUES
    -- Страновые дефолты (законодательные ставки).
    ('BY-0',   '{"country": "BY", "code_cfo": 0, "name": "Беларусь — базовая", "vat_rate": 0.20, "source": "закон", "note": ""}'::jsonb),
    ('RU-0',   '{"country": "RU", "code_cfo": 0, "name": "Россия — базовая", "vat_rate": 0.20, "source": "закон", "note": ""}'::jsonb),
    ('KZ-0',   '{"country": "KZ", "code_cfo": 0, "name": "Казахстан — базовая", "vat_rate": 0.12, "source": "закон", "note": ""}'::jsonb),
    ('UZ-0',   '{"country": "UZ", "code_cfo": 0, "name": "Узбекистан — базовая", "vat_rate": 0.12, "source": "закон", "note": ""}'::jsonb),
    -- Эффективные ставки площадок МП (ТЗ §3.4, расчёт по значениям строк 11–20/26–35
    -- прототипа). Открытый вопрос §12 п.4: откуда ставка берётся на плановый период.
    ('RU-335', '{"country": "RU", "code_cfo": 335, "name": "Wildberries — эффективная", "vat_rate": 0.2036, "source": "эффективная (ТЗ §3.4)", "note": "уточняется у финблока (§12 п.4)"}'::jsonb),
    ('RU-336', '{"country": "RU", "code_cfo": 336, "name": "Lamoda — эффективная", "vat_rate": 0.2036, "source": "эффективная (ТЗ §3.4)", "note": "уточняется у финблока (§12 п.4)"}'::jsonb),
    ('RU-337', '{"country": "RU", "code_cfo": 337, "name": "Ozon — эффективная", "vat_rate": 0.2036, "source": "эффективная (ТЗ §3.4)", "note": "уточняется у финблока (§12 п.4)"}'::jsonb),
    ('RU-954', '{"country": "RU", "code_cfo": 954, "name": "Yandex Market — эффективная", "vat_rate": 0.1662, "source": "эффективная (ТЗ §3.4)", "note": "уточняется у финблока (§12 п.4)"}'::jsonb)
) AS v(ext, payload)
WHERE d.code = 'dir_vat';

-- Курсы: дефолтные (year=0, month=0) строки по валютам ввода/отображения.
INSERT INTO plans_directory (code, source, sync_status)
VALUES ('dir_fx_rate', 'manual', 'seed')
ON CONFLICT (code) DO NOTHING;

DELETE FROM plans_directory_row
 WHERE directory_id = (SELECT id FROM plans_directory WHERE code = 'dir_fx_rate')
   AND external_id IN ('BYN-0-0', 'RUB-0-0', 'USD-0-0', 'KZT-0-0', 'UZS-0-0');
INSERT INTO plans_directory_row (directory_id, external_id, payload_json)
SELECT d.id, v.ext, v.payload FROM plans_directory d
CROSS JOIN (VALUES
    ('BYN-0-0', '{"currency": "BYN", "year": 0, "month": 0, "rate_byn": 1.0,     "scenario": "Тактика бюджет (таргеты)"}'::jsonb),
    ('RUB-0-0', '{"currency": "RUB", "year": 0, "month": 0, "rate_byn": 0.0376,  "scenario": "Тактика бюджет (таргеты)"}'::jsonb),
    ('USD-0-0', '{"currency": "USD", "year": 0, "month": 0, "rate_byn": 3.2,     "scenario": "Тактика бюджет (таргеты)"}'::jsonb),
    ('KZT-0-0', '{"currency": "KZT", "year": 0, "month": 0, "rate_byn": 0.0068,  "scenario": "Тактика бюджет (таргеты)"}'::jsonb),
    ('UZS-0-0', '{"currency": "UZS", "year": 0, "month": 0, "rate_byn": 0.00026, "scenario": "Тактика бюджет (таргеты)"}'::jsonb)
) AS v(ext, payload)
WHERE d.code = 'dir_fx_rate';
