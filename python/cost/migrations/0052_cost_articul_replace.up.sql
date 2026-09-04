-- 0052 — отметка «нужна замена артикула» и рассылка о ней (пожелание № 4)
--
-- Инициатор — Апанасёнок О. (ПЭО): в таблице нужно поле, где экономист ставит
-- отметку, что по калькуляции нужна замена артикула, после чего письмо об этом
-- автоматически уходит следующим операторам. Адресаты зависят от ассортимента:
--   · ЧНИ (носки и Orodoro)            — Кочеткова Т., Король Н.;
--   · всё остальное                     — Дащинская А.А.
-- Критерий ЧНИ — верхний уровень номенклатуры: trim("Level 01") IN
-- ('Носки&Колготки', 'Orodoro'). Слова «ЧНИ» в номенклатуре нет, по тексту его
-- не найти; строки с пустым Level 01 (их ~33 тыс.) уходят в «остальное».
--
-- ГРАНУЛЯРНОСТЬ
-- =============
-- Ключ отметки — тот же, что у статуса ПЭО (cost_calc_approvals): модель,
-- артикул, признак калькуляции, план и номер задания. Отметка ставится в строке
-- таблицы, а строка — это задание; замена артикула у разных заданий одного плана
-- может понадобиться по-разному. Пустое задание хранится как '' (не NULL), чтобы
-- UNIQUE работал как ключ, а не плодил дубли (урок миграции 0023).
--
-- АДРЕСАТЫ — В ТАБЛИЦЕ, А НЕ В КОДЕ
-- ================================
-- Люди меняются чаще, чем код деплоится. Справочник редактируется SQL'ем или
-- будущей админкой; сегмент 'chni' / 'other' выбирается по критерию выше.
-- Сама доставка настраивается переменными COST_SMTP_* (см. .env.example): без
-- них отметка ставится, а письмо не уходит — это видно в интерфейсе.

CREATE TABLE IF NOT EXISTS cost_articul_replace (
    id           BIGSERIAL PRIMARY KEY,
    model        TEXT        NOT NULL,
    articul      TEXT        NOT NULL,
    calc_sign    TEXT        NOT NULL DEFAULT '',
    plan_id      TEXT        NOT NULL DEFAULT '',
    task_number  TEXT        NOT NULL DEFAULT '',
    needed       BOOLEAN     NOT NULL DEFAULT TRUE,
    comment      TEXT,
    -- Снимок контекста строки на момент отметки — для письма и истории:
    -- наименование, Level 01, страна, бренд-менеджер. Источник может измениться,
    -- письмо должно описывать то, что видел экономист.
    context      JSONB       NOT NULL DEFAULT '{}'::jsonb,
    set_by       TEXT        NOT NULL,
    set_at       TIMESTAMPTZ NOT NULL DEFAULT now(),
    -- Итог последней рассылки по этой отметке. NULL в обоих полях — письмо ещё
    -- не отправлялось (например, отметку сняли).
    notified_at  TIMESTAMPTZ,
    notify_error TEXT,
    created_at   TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at   TIMESTAMPTZ NOT NULL DEFAULT now(),
    CONSTRAINT uq_articul_replace_key UNIQUE (model, articul, calc_sign, plan_id, task_number)
);

CREATE INDEX IF NOT EXISTS ix_articul_replace_needed
    ON cost_articul_replace (needed) WHERE needed;

-- Журнал уведомлений: кому, по какому каналу, что и чем закончилось. Отдельно
-- от отметки, потому что по одной отметке рассылок может быть несколько (сняли
-- и поставили снова), а каналов — два: почта и Битрикс.
CREATE TABLE IF NOT EXISTS cost_articul_replace_mail (
    id           BIGSERIAL PRIMARY KEY,
    replace_id   BIGINT      NOT NULL REFERENCES cost_articul_replace(id) ON DELETE CASCADE,
    segment      TEXT        NOT NULL,
    channel      TEXT        NOT NULL DEFAULT 'email',  -- email | bitrix
    recipients   TEXT[]      NOT NULL,                   -- e-mail'ы либо b24_id как текст
    subject      TEXT        NOT NULL,
    status       TEXT        NOT NULL,          -- sent | skipped | error
    error        TEXT,
    sent_at      TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE TABLE IF NOT EXISTS cost_articul_replace_recipients (
    id         BIGSERIAL PRIMARY KEY,
    segment    TEXT    NOT NULL CHECK (segment IN ('chni', 'other')),
    email      TEXT    NOT NULL,
    name       TEXT    NOT NULL DEFAULT '',
    -- ID пользователя в корпоративном Битриксе. Если задан — кроме письма
    -- уходит сообщение в Битрикс напрямую через портал (site_api, тот же
    -- контракт, что у Go-дубля уведомлений кабинета). Через кабинет слать
    -- нельзя: эти люди в Finance Cabinet не заходят, номера пользователя у них
    -- там не появится (уточнение заказчика 03.09.2026). Пусто — только письмо.
    b24_id     BIGINT,
    active     BOOLEAN NOT NULL DEFAULT TRUE,
    CONSTRAINT uq_articul_replace_recipient UNIQUE (segment, email)
);

-- ID в Битриксе получены от заказчика 03.09.2026: Кочеткова — 80367,
-- Король — 107612, Дащинская — 683.
INSERT INTO cost_articul_replace_recipients (segment, email, name, b24_id) VALUES
    ('chni',  't.kochetkova@markformelle.by',        'Кочеткова Татьяна', 80367),
    ('chni',  'n.korol@markformelle.by',             'Король Наталья',    107612),
    ('other', 'angela.daschinskaya@markformelle.by', 'Дащинская А.А.',    683)
ON CONFLICT (segment, email) DO UPDATE
    SET b24_id = COALESCE(cost_articul_replace_recipients.b24_id, EXCLUDED.b24_id);

-- Право ставить отметку. Решение о замене артикула принимает БРЕНД-МЕНЕДЖЕР
-- (уточнение заказчика 03.09.2026: «галочка нужна в первую очередь БМ»), поэтому
-- роль «Бренд-менеджер» — основной носитель права. ПЭО (инициатор пожелания) и
-- калькулятор, который ведёт калькуляции, оставлены тоже: они видят
-- необходимость замены со своей стороны процесса.
UPDATE cost_roles
   SET permissions = permissions || '["cost:articul_replace"]'::jsonb,
       updated_at = now()
 WHERE name IN ('Бренд-менеджер', 'Калькулятор', 'ПЭО', 'Full Admin')
   AND NOT permissions @> '["cost:articul_replace"]'::jsonb;
