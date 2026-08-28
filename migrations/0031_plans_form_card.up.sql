-- 0031 — «карточка формы»: общая оболочка процесса для ВСЕХ форм комплекта.
-- Новые ТЗ (var/tz, 2026-08) требуют от каждой формы одинакового жизненного цикла:
-- статусы draft→on_approval→approved→published, возврат на любой предыдущий шаг с
-- обязательным комментарием, аннулирование согласований (revoked), нумерованные
-- версии-снапшоты со значениями/условиями/курсом, reopen с причиной,
-- конфигурируемый маршрут (в т.ч. включаемый шаг «Финансист»).
--
-- Почему отдельный слой, а не pl_stage_instance: единица маршрута — ФОРМА, а не
-- этап периода. ТЗ МП §2.2: «крупные и мелкие МП — независимые подзадачи одного
-- периода: возврат по мелким не блокирует крупные». ТЗ Розница §2.0: «экземпляр
-- формы = (страна, период)… возврат по одной стране не должен блокировать
-- остальные». Значит на один pl_instance приходится несколько карточек:
-- МП large / МП small / Розница BY / RU / KZ / UZ.
--
-- pl_instance остаётся контейнером периода (календарь, 12 этапов, доска) — его
-- UNIQUE(period_year, period_month) не трогаем.

CREATE TABLE IF NOT EXISTS form_card (
    id              BIGSERIAL PRIMARY KEY,
    pl_id           BIGINT NOT NULL REFERENCES pl_instance(id) ON DELETE CASCADE,
    form_code       TEXT NOT NULL,                    -- TPL-MP | TPL-TO-RETAIL | …
    scope_key       TEXT NOT NULL DEFAULT '',         -- 'large'/'small' (МП) | 'BY'/'RU'/'KZ'/'UZ' (розница)
    title           TEXT NOT NULL DEFAULT '',
    country         TEXT NOT NULL DEFAULT '',
    legal_entity    TEXT NOT NULL DEFAULT '',         -- одно реальное ЮЛ (розница §4.2.1)
    currency        TEXT NOT NULL DEFAULT '',         -- валюта ВВОДА (нац. валюта формы)
    -- Направление расчёта: legacy = «суммы → доли» (действующая форма МП),
    -- inverse = «доли/наценки → суммы» (ТЗ МП §3.1). Режим на карточке, чтобы
    -- закрытые периоды считались тем алгоритмом, которым были утверждены.
    calc_mode       TEXT NOT NULL DEFAULT 'legacy',
    -- status: draft | on_approval | returned | approved | published | publish_failed | archived.
    -- Составные метки ТЗ (on_approval_1_2 / returned_to_1_1) собираются из
    -- status + step_code в коде — в БД хранится нормализованная пара.
    status          TEXT NOT NULL DEFAULT 'draft',
    step_code       TEXT NOT NULL DEFAULT '1.1',      -- текущий шаг маршрута
    current_version INT  NOT NULL DEFAULT 0,
    fx_snapshot     JSONB NOT NULL DEFAULT '{}',      -- применённые курсы (фиксируются при утверждении)
    locked          BOOLEAN NOT NULL DEFAULT FALSE,   -- запись значений закрыта (approved/published)
    due_at          TIMESTAMPTZ,
    created_by      BIGINT REFERENCES users(id),
    created_at      TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at      TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    UNIQUE (pl_id, form_code, scope_key)
);
CREATE INDEX IF NOT EXISTS idx_form_card_pl ON form_card(pl_id);
CREATE INDEX IF NOT EXISTS idx_form_card_status ON form_card(form_code, status);

-- Лист согласования карточки: решения по шагам маршрута. Комментарий обязателен
-- при возврате (проверка в коде); аннулированные решения не удаляются.
CREATE TABLE IF NOT EXISTS card_approval (
    id             BIGSERIAL PRIMARY KEY,
    card_id        BIGINT NOT NULL REFERENCES form_card(id) ON DELETE CASCADE,
    step_code      TEXT NOT NULL,
    user_id        BIGINT REFERENCES users(id),
    decision       TEXT NOT NULL,                    -- submit|approve|return|reopen|auto_skipped|publish|publish_failed
    target_step    TEXT NOT NULL DEFAULT '',
    comment        TEXT NOT NULL DEFAULT '',
    version_no     INT  NOT NULL DEFAULT 0,
    revoked        BOOLEAN NOT NULL DEFAULT FALSE,
    revoked_reason TEXT NOT NULL DEFAULT '',
    decided_at     TIMESTAMPTZ NOT NULL DEFAULT NOW()
);
CREATE INDEX IF NOT EXISTS idx_card_approval_card ON card_approval(card_id, decided_at);

-- Версия-снапшот: пишется при КАЖДОМ переходе (вперёд и назад). payload содержит
-- значения формы + применённые условия площадок + курсы (ТЗ МП §2.3, §7.2).
CREATE TABLE IF NOT EXISTS card_version (
    id          BIGSERIAL PRIMARY KEY,
    card_id     BIGINT NOT NULL REFERENCES form_card(id) ON DELETE CASCADE,
    version_no  INT  NOT NULL,
    step_from   TEXT NOT NULL DEFAULT '',
    step_to     TEXT NOT NULL DEFAULT '',
    action      TEXT NOT NULL DEFAULT '',
    reason      TEXT NOT NULL DEFAULT '',
    payload     JSONB NOT NULL DEFAULT '{}',
    created_by  BIGINT REFERENCES users(id),
    created_at  TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    UNIQUE (card_id, version_no)
);

-- Маршрут формы: шаги настраиваются данными, а не кодом. Шаг «Финансист» между
-- 1.2 и 1.3 (реальная практика, встреча 10.07.2026) включается флагом enabled
-- без правки кода — ТЗ МП §2.1, Розница §2.1.
CREATE TABLE IF NOT EXISTS plans_form_route (
    id                 BIGSERIAL PRIMARY KEY,
    form_code          TEXT NOT NULL,
    step_code          TEXT NOT NULL,
    step_name          TEXT NOT NULL,
    sort_order         INT  NOT NULL,
    kind               TEXT NOT NULL DEFAULT 'approve',  -- fill|approve|final
    responsible        TEXT NOT NULL DEFAULT '',          -- метка ответственных (совместимо с plans_route_config)
    position_code      TEXT NOT NULL DEFAULT '',          -- должность процесса (plans_position)
    due_rd             INT,                               -- срок: N-й рабочий день
    enabled            BOOLEAN NOT NULL DEFAULT TRUE,
    skip_if_same_user  BOOLEAN NOT NULL DEFAULT FALSE,    -- самосогласование (Розница §2.1)
    publish_on_approve BOOLEAN NOT NULL DEFAULT FALSE,    -- триггер публикации (этап 1.4)
    updated_by         BIGINT REFERENCES users(id),
    updated_at         TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    UNIQUE (form_code, step_code)
);

-- Сид маршрута обеих форм: 1.1 → [Финансист, выключен] → 1.2 → 1.3 → 1.4(публикация).
INSERT INTO plans_form_route
    (form_code, step_code, step_name, sort_order, kind, responsible, due_rd, enabled, skip_if_same_user, publish_on_approve)
VALUES
    ('TPL-MP',        '1.1', 'Заполнение бюджета продаж',       10, 'fill',    'Мурашко Ф. (крупные), Левин / Качановская (мелкие)', 2, TRUE,  FALSE, FALSE),
    ('TPL-MP',        'fin', 'Финансовый контролёр',            20, 'approve', 'финансист',                                          3, FALSE, FALSE, FALSE),
    ('TPL-MP',        '1.2', 'Согласование руководителем',      30, 'approve', 'Ворончук Е. (крупные), Левин (мелкие)',              3, TRUE,  FALSE, FALSE),
    ('TPL-MP',        '1.3', 'Директор направления',            40, 'approve', 'Дегтерева Е.В.',                                     3, TRUE,  FALSE, FALSE),
    ('TPL-MP',        '1.4', 'Утверждение',                     50, 'final',   'Сипарова С.Г.',                                      4, TRUE,  FALSE, TRUE),
    ('TPL-TO-RETAIL', '1.1', 'Заполнение плана продаж',         10, 'fill',    'Смолер О.В. (РМ/уполномоченный)',                    2, TRUE,  FALSE, FALSE),
    ('TPL-TO-RETAIL', 'fin', 'Финансовый контролёр',            20, 'approve', 'финансист',                                          3, FALSE, FALSE, FALSE),
    ('TPL-TO-RETAIL', '1.2', 'Согласование руководителем',      30, 'approve', 'Смолер О.В.',                                        3, TRUE,  TRUE,  FALSE),
    ('TPL-TO-RETAIL', '1.3', 'Директор направления',            40, 'approve', 'Дегтерева Е.В.',                                     3, TRUE,  FALSE, FALSE),
    ('TPL-TO-RETAIL', '1.4', 'Утверждение',                     50, 'final',   'Сипарова С.Г.',                                      4, TRUE,  FALSE, TRUE)
ON CONFLICT (form_code, step_code) DO NOTHING;
