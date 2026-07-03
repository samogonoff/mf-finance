-- 0020 — единый справочник стран (ТЗ-запрос: все коды/сокращения в одном месте,
-- сведение через него, а не хардкоды). Наполняется провайдером countryProvider
-- из Лисы s_country (или каноном при LISA_MOCK).
INSERT INTO plans_directory (code, source, sync_status) VALUES
    ('dir_country', 'lisa', 'never')
ON CONFLICT (code) DO NOTHING;
