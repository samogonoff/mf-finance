-- 0023 — структура компании (ответственность по направлениям). Дерево узлов
-- ВЫВОДИТСЯ из dir_cfo (направление group_cfo1 × страна country); ЦФО разносятся
-- автоматически по этим разрезам. Здесь храним только «кто ответственный за узел»
-- (+ опц. форма ввода и ручной override набора ЦФО). Замы берутся из plans_deputy
-- ответственного. «Не разнесено» = ЦФО в узлах без ответственного.
CREATE TABLE IF NOT EXISTS plans_org_responsible (
    group_cfo1          TEXT NOT NULL,                 -- направление (Розница/Маркетплейсы/…)
    country             TEXT NOT NULL DEFAULT '',      -- страна узла ('' = без страны)
    responsible_user_id BIGINT REFERENCES users(id) ON DELETE SET NULL,
    form_code           TEXT NOT NULL DEFAULT '',      -- TPL-* (форма ввода узла)
    code_cfo_override   JSONB,                         -- ручной набор ЦФО (NULL = авто по group×country)
    updated_at          TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    PRIMARY KEY (group_cfo1, country)
);

-- Предзаполняем формы по направлениям (ТЗ Приложение G) — для узлов розницы/МП/опт/ИМ.
INSERT INTO plans_org_responsible (group_cfo1, country, form_code)
SELECT DISTINCT payload_json->>'group_cfo1',
       COALESCE(payload_json->>'country',''),
       CASE payload_json->>'group_cfo1'
            WHEN 'Розница' THEN 'TPL-TO-RETAIL'
            WHEN 'Маркетплейсы' THEN 'TPL-MP'
            WHEN 'Маркетплейсы РФ' THEN 'TPL-MP'
            WHEN 'Производство' THEN 'TPL-PROD-MINUTES'
            WHEN 'Производство картонажное' THEN 'TPL-PROD-MINUTES'
            ELSE '' END
FROM plans_directory_row r JOIN plans_directory d ON d.id = r.directory_id
WHERE d.code = 'dir_cfo' AND COALESCE(payload_json->>'group_cfo1','') <> ''
ON CONFLICT DO NOTHING;

-- Вершина структуры: два учредителя — финальное утверждение всего (этап 4).
-- group_cfo1='Учредители'; слоты 'founder1'/'founder2' (эталон ТЗ: Сипарова С.Г., Сериков А.Г.).
INSERT INTO plans_org_responsible (group_cfo1, country, form_code) VALUES
    ('Учредители', 'founder1', 'итоговое утверждение (этап 4)'),
    ('Учредители', 'founder2', 'итоговое утверждение (этап 4)')
ON CONFLICT DO NOTHING;
