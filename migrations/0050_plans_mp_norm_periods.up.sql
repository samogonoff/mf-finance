-- 0050 — нормативы удержаний площадок отдельным справочником, с периодом.
--
-- Правка 08.09.2026 п. 8: норматив зависит от периода. У Wildberries 43 %
-- действовали январь–февраль 2026, с марта — 42,8 %. Одним значением на площадку
-- (как в миграции 0048) июньские суммы не воспроизводятся: доля агентского
-- выводится остатком норматива, и 0,2 п.п. разницы уезжают во все статьи.
--
-- Норматив покрывает РОВНО шесть долей: комиссия площадки, агентское
-- вознаграждение, транспортная логистика 52, складская логистика 54, штрафы 66 и
-- прочие удержания 66. Экспорт 51, реклама 13, соцсети 15, упаковка 45,
-- эквайринг 58 и IT 48 в него не входят — это наши расходы, а не удержания.
--
-- Значения заданы к выручке ПО ЦЕНАМ МЕНЕДЖЕРА без НДС, как в договоре с
-- площадкой и в прототипе. База формы другая (цена площадки без НДС, решение
-- звонка 07.09.2026), поэтому перед применением норматив делится на (1 − %СПП);
-- суммы от смены базы не меняются — это закреплено приёмочным тестом.
--
-- Поле norm_share в dir_marketplace (0048) больше не читается: оно осталось
-- как след и снимается этой миграцией, чтобы не было двух источников правды.

INSERT INTO plans_directory (code, source, sync_status)
VALUES ('dir_mp_norm', 'manual', 'seed')
ON CONFLICT (code) DO NOTHING;

DELETE FROM plans_directory_row
 WHERE directory_id = (SELECT id FROM plans_directory WHERE code = 'dir_mp_norm');

INSERT INTO plans_directory_row (directory_id, external_id, payload_json)
SELECT d.id, v.ext, v.payload FROM plans_directory d
CROSS JOIN (VALUES
    ('335-2026-01', '{"code_cfo": 335, "name": "Wildberries", "norm": 0.430, "from_year": 2026, "from_month": 1, "to_year": 2026, "to_month": 2, "note": "январь–февраль 2026"}'::jsonb),
    ('335-2026-03', '{"code_cfo": 335, "name": "Wildberries", "norm": 0.428, "from_year": 2026, "from_month": 3, "to_year": 0, "to_month": 0, "note": "с марта 2026"}'::jsonb),
    ('336',         '{"code_cfo": 336, "name": "Lamoda", "norm": 0.340, "from_year": 0, "from_month": 0, "to_year": 0, "to_month": 0, "note": "весь 2026"}'::jsonb),
    ('337',         '{"code_cfo": 337, "name": "Ozon", "norm": 0.350, "from_year": 0, "from_month": 0, "to_year": 0, "to_month": 0, "note": "весь 2026"}'::jsonb),
    ('954',         '{"code_cfo": 954, "name": "Yandex Market", "norm": 0.320, "from_year": 0, "from_month": 0, "to_year": 0, "to_month": 0, "note": "весь 2026"}'::jsonb)
) AS v(ext, payload)
WHERE d.code = 'dir_mp_norm';

-- Единственный источник правды — dir_mp_norm.
UPDATE plans_directory_row r
   SET payload_json = r.payload_json - 'norm_share'
  FROM plans_directory d
 WHERE d.id = r.directory_id AND d.code = 'dir_marketplace';
