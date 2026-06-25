-- 0016 — производственные календари р.д. по странам (Q3, SPEC §4.1, §7.3).
-- Глобальные настройки проекта; дата N-го р.д. этапа считается по календарю страны
-- ответственного. Переиспользуемо (табель и пр.). Движок пока берёт CalendarSeed
-- из кода; таблица — под редактирование/полные праздники (этап 2).

CREATE TABLE IF NOT EXISTS plans_country_calendar (
    id            BIGSERIAL PRIMARY KEY,
    country       TEXT NOT NULL,            -- BY|RU|KZ|UZ
    year          INT  NOT NULL,
    holidays      JSONB NOT NULL DEFAULT '[]',  -- ['2026-01-01',…]
    work_weekends JSONB NOT NULL DEFAULT '[]',  -- перенесённые рабочие выходные
    short_days    JSONB NOT NULL DEFAULT '[]',  -- [{"date":"2026-03-07","hours":7}]
    updated_by    BIGINT REFERENCES users(id),
    updated_at    TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    UNIQUE (country, year)
);

INSERT INTO plans_country_calendar (country, year, holidays) VALUES
    ('RU', 2026, '["2026-01-01","2026-01-02","2026-01-07"]'),
    ('BY', 2026, '["2026-01-01","2026-01-02","2026-01-07"]'),
    ('KZ', 2026, '["2026-01-01","2026-01-02","2026-01-07"]'),
    ('UZ', 2026, '["2026-01-01","2026-01-02"]')
ON CONFLICT (country, year) DO NOTHING;
