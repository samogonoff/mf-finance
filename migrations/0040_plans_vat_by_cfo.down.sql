-- Откат 0040 — возврат к ставкам сида 0034 (ТЗ МП §3.4).
DELETE FROM plans_directory_row
 WHERE directory_id = (SELECT id FROM plans_directory WHERE code = 'dir_vat')
   AND (payload_json->>'code_cfo')::int <> 0;

INSERT INTO plans_directory_row (directory_id, external_id, payload_json)
SELECT d.id, v.ext, v.payload FROM plans_directory d
CROSS JOIN (VALUES
    ('RU-335', '{"country": "RU", "code_cfo": 335, "name": "Wildberries — эффективная", "vat_rate": 0.2036, "source": "эффективная (ТЗ §3.4)", "note": ""}'::jsonb),
    ('RU-336', '{"country": "RU", "code_cfo": 336, "name": "Lamoda — эффективная", "vat_rate": 0.2036, "source": "эффективная (ТЗ §3.4)", "note": ""}'::jsonb),
    ('RU-337', '{"country": "RU", "code_cfo": 337, "name": "Ozon — эффективная", "vat_rate": 0.2036, "source": "эффективная (ТЗ §3.4)", "note": ""}'::jsonb),
    ('RU-954', '{"country": "RU", "code_cfo": 954, "name": "Yandex Market — эффективная", "vat_rate": 0.1662, "source": "эффективная (ТЗ §3.4)", "note": ""}'::jsonb)
) AS v(ext, payload)
WHERE d.code = 'dir_vat'
ON CONFLICT DO NOTHING;
