-- 0026 — типизация должностей: founder | director | top (3 блока в UI).
-- Учредители — финал (этап 4); директора направлений; ТОПы (покрывают ЦФО).
-- Финансисты — это директора-держатели этапов (kind=director). ТЗ §«Участники».
ALTER TABLE plans_job_position ADD COLUMN IF NOT EXISTS kind TEXT NOT NULL DEFAULT 'director';
UPDATE plans_job_position SET kind='top'      WHERE title='IT-Директор';
UPDATE plans_job_position SET kind='director' WHERE title IN ('Финансовый директор','Коммерческий директор','Директор по рознице','Директор по производству');

INSERT INTO plans_job_position (title, description, kind, sort_order) VALUES
    ('Координатор бюджета', 'Финансист — держатель этапов свода (эталон ТЗ: Антипова О.В.)', 'director', 55),
    ('Учредитель', 'Финальное утверждение, этап 4 (эталон ТЗ: Сипарова С.Г.)', 'founder', 5),
    ('Учредитель', 'Финальное утверждение, этап 4 (эталон ТЗ: Сериков А.Г.)', 'founder', 6)
ON CONFLICT DO NOTHING;
