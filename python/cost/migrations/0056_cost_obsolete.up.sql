-- 0056 — статус «Неактуальная модель/артикул» (просьба заказчика 09.09.2026)
--
-- Калькулятор или ПЭО отмечают, что модель/артикул больше не актуальны: строки
-- в таблице подсвечиваются серым, в фильтре «Статус согласования» появляется
-- отдельное значение, в колонке «Этап» — ⚫. Это не этап согласования и не
-- отметка по калькуляции, а состояние самого изделия, поэтому ключ — модель +
-- артикул (как у карточки 713), а не ключ калькуляции с планом и заданием:
-- неактуальный артикул неактуален во всех своих калькуляциях.
--
-- Статус снимаемый: те же роли могут вернуть изделие в работу. История — в
-- set_by/set_at последнего изменения; отдельного журнала нет, задача его не
-- требует.

CREATE TABLE IF NOT EXISTS cost_obsolete (
    id          BIGSERIAL PRIMARY KEY,
    model       TEXT        NOT NULL,
    articul     TEXT        NOT NULL,
    obsolete    BOOLEAN     NOT NULL DEFAULT TRUE,
    comment     TEXT,
    set_by      TEXT        NOT NULL,
    set_at      TIMESTAMPTZ NOT NULL DEFAULT now(),
    created_at  TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at  TIMESTAMPTZ NOT NULL DEFAULT now(),
    CONSTRAINT uq_obsolete_key UNIQUE (model, articul)
);

CREATE INDEX IF NOT EXISTS ix_obsolete_active
    ON cost_obsolete (model, articul) WHERE obsolete;

-- Право ставить и снимать статус — у калькулятора и ПЭО (слова заказчика
-- 09.09.2026: «проставляется калькулятором и ПЭО»); Full Admin — как везде.
-- Бренд-менеджер права не получает.
UPDATE cost_roles
   SET permissions = permissions || '["cost:obsolete"]'::jsonb,
       updated_at = now()
 WHERE name IN ('Калькулятор', 'ПЭО', 'Full Admin')
   AND NOT permissions @> '["cost:obsolete"]'::jsonb;
