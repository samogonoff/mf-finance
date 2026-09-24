-- 0059 — ценообразование группы компаний (постановка заказчика 23.09.2026).
--
-- Товар внутри группы проходит цепочку компаний: каждая продаёт следующей со
-- своей корректировкой к цене (+ наценка / − скидка), последняя — внешнему
-- рынку. Заказчику нужен финрез такой цепочки по ассортименту главной таблицы:
-- итог группы, доход каждого звена, проваливание Level 01 → … → артикул.
--
-- Решения заказчика 23.09.2026, которые определили схему:
--   · компании группы — редактируемый справочник (шесть на старте, см. seed),
--     а не константы в коде: состав группы меняется;
--   · цепочка привязана к АССОРТИМЕНТУ — хранит набор фильтров главной таблицы
--     (scope, JSONB). Пересечение ассортимента разных цепочек допускается —
--     подсвечивать, не запрещать, поэтому ограничений на scope в базе нет;
--   · режим расчёта — на цепочке: cascade (% от цены предыдущего звена) или
--     base (все % от базовой отпускной);
--   · без расходов звеньев и без НДС — только цены; себестоимость производителя
--     берётся из главной таблицы, здесь не хранится;
--   · валюта отгрузки — на звене и не обязана совпадать с валютой страны
--     компании; всё считается в BYN, валюта — дополнительный показ по курсу НБ РБ;
--   · для будущей выгрузки в региональные 1С у звена хранится эффективный % к
--     базовой отпускной (eff_pct) независимо от режима — 1С не должна знать
--     про режимы и пересчитывать цепочку;
--   · право cost:group_pricing — только у Full Admin, остальным роли выдаёт
--     администратор через страницу ролей.
--
-- Триггеров нет: updated_at/updated_by ставит код при сохранении.

CREATE TABLE IF NOT EXISTS cost_group_company (
    id          BIGSERIAL PRIMARY KEY,
    name        TEXT        NOT NULL,
    country     TEXT        NOT NULL DEFAULT 'BY',
    currency    TEXT        NOT NULL DEFAULT 'BYN',
    comment     TEXT,
    is_active   BOOLEAN     NOT NULL DEFAULT TRUE,
    sort_order  INTEGER     NOT NULL DEFAULT 0,
    created_by  TEXT        NOT NULL DEFAULT '',
    created_at  TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_by  TEXT        NOT NULL DEFAULT '',
    updated_at  TIMESTAMPTZ NOT NULL DEFAULT now(),
    CONSTRAINT uq_group_company_name UNIQUE (name)
);

COMMENT ON COLUMN cost_group_company.country IS
    'Код страны компании (BY/RU/KZ/UZ…), свободный текст — справочника стран в разделе нет';
COMMENT ON COLUMN cost_group_company.currency IS
    'Валюта отгрузки по умолчанию для новых звеньев с этой компанией; код из purchase.CURRENCIES. На звене может быть изменена';
COMMENT ON COLUMN cost_group_company.is_active IS
    'Неактивная компания не предлагается в редакторе звеньев, но в существующих цепочках остаётся';

-- Шесть компаний группы, названных заказчиком 23.09.2026. Повторный накат и
-- переименованные заказчиком строки не трогаем: конфликт по имени — DO NOTHING.
INSERT INTO cost_group_company (name, country, currency, sort_order, created_by, updated_by) VALUES
    ('Марк Формэль',      'BY', 'BYN', 10, 'migration', 'migration'),
    ('Формэль',           'BY', 'BYN', 20, 'migration', 'migration'),
    ('ТД Марк Формэль',   'BY', 'BYN', 30, 'migration', 'migration'),
    ('Марк Формэль Текс', 'BY', 'BYN', 40, 'migration', 'migration'),
    ('Марк Формэль КЗ',   'KZ', 'KZT', 50, 'migration', 'migration'),
    ('Марк Формэль IT',   'BY', 'BYN', 60, 'migration', 'migration')
ON CONFLICT ON CONSTRAINT uq_group_company_name DO NOTHING;

CREATE TABLE IF NOT EXISTS cost_group_chain (
    id          BIGSERIAL PRIMARY KEY,
    name        TEXT        NOT NULL,
    mode        TEXT        NOT NULL DEFAULT 'cascade',
    weight      TEXT        NOT NULL DEFAULT 'unit',
    scope       JSONB       NOT NULL DEFAULT '{}'::jsonb,
    rate_date   DATE,
    status      TEXT        NOT NULL DEFAULT 'active',
    comment     TEXT,
    created_by  TEXT        NOT NULL DEFAULT '',
    created_at  TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_by  TEXT        NOT NULL DEFAULT '',
    updated_at  TIMESTAMPTZ NOT NULL DEFAULT now(),
    CONSTRAINT uq_group_chain_name UNIQUE (name),
    CONSTRAINT ck_group_chain_mode   CHECK (mode IN ('cascade', 'base')),
    CONSTRAINT ck_group_chain_weight CHECK (weight IN ('unit', 'volume')),
    CONSTRAINT ck_group_chain_status CHECK (status IN ('active', 'archived'))
);

COMMENT ON COLUMN cost_group_chain.mode IS
    'Режим расчёта: cascade — % звена к цене предыдущего звена; base — все % к базовой отпускной. Решение заказчика 23.09.2026: переключатель на цепочке';
COMMENT ON COLUMN cost_group_chain.weight IS
    'Вес строки в агрегатах: unit — 1 калькуляция = 1 единица; volume — «выпуск шт» из главной таблицы (есть в основном у ФКСС)';
COMMENT ON COLUMN cost_group_chain.scope IS
    'Фильтры ассортимента главной таблицы (ключи как в POST /aggregated: level01..level05, brand_manager, country, season, calc_sign, model, articul, plan_id — списки текстом; date_from/date_to по «дата расчета»; latest_only). Пустой объект = весь ассортимент. Пересечения scope разных цепочек допускаются';
COMMENT ON COLUMN cost_group_chain.rate_date IS
    'Дата курса НБ РБ для показа сумм звеньев в валюте отгрузки; NULL = на день запроса';

CREATE TABLE IF NOT EXISTS cost_group_chain_link (
    id          BIGSERIAL PRIMARY KEY,
    chain_id    BIGINT      NOT NULL REFERENCES cost_group_chain(id) ON DELETE CASCADE,
    seq         INTEGER     NOT NULL,
    company_id  BIGINT      NOT NULL REFERENCES cost_group_company(id) ON DELETE RESTRICT,
    adjust_pct  NUMERIC(9,4)  NOT NULL DEFAULT 0,
    currency    TEXT        NOT NULL DEFAULT 'BYN',
    eff_pct     NUMERIC(14,6) NOT NULL DEFAULT 0,
    CONSTRAINT uq_group_chain_link_seq UNIQUE (chain_id, seq),
    CONSTRAINT ck_group_chain_link_adjust CHECK (adjust_pct > -100)
);

COMMENT ON COLUMN cost_group_chain_link.seq IS
    'Порядок звена в цепочке, 1..n; звено seq=1 покупает у производителя по себестоимости, последнее продаёт внешнему рынку';
COMMENT ON COLUMN cost_group_chain_link.adjust_pct IS
    'Корректировка цены продажи этого звена, %: + наценка, − скидка. К чему применяется — определяет cost_group_chain.mode. Скидка 100 % и глубже обнулила бы цену — запрещена';
COMMENT ON COLUMN cost_group_chain_link.currency IS
    'Валюта отгрузки звена (код из purchase.CURRENCIES); суммы звена дополнительно показываются в ней по курсу НБ РБ. Может отличаться от валюты страны компании';
COMMENT ON COLUMN cost_group_chain_link.eff_pct IS
    'Эффективный % цены продажи звена к БАЗОВОЙ отпускной главной таблицы — независимо от режима; считает код при сохранении цепочки. Для выгрузки в региональные 1С (заказчик 23.09.2026)';

CREATE INDEX IF NOT EXISTS ix_group_chain_link_company ON cost_group_chain_link (company_id);

-- Витрина для внешней выгрузки (1С читает нашу базу напрямую): цепочки × звенья
-- с именем компании и eff_pct. Статус отдаём, а не фильтруем: потребитель сам
-- решает, нужны ли ему архивные.
CREATE OR REPLACE VIEW cost_group_pricing_export AS
SELECT c.id AS chain_id, c.name AS chain_name, c.mode, c.scope, c.status,
       l.seq, l.company_id, k.name AS company_name, k.country, l.currency,
       l.adjust_pct, l.eff_pct, c.updated_at
  FROM cost_group_chain c
  JOIN cost_group_chain_link l ON l.chain_id = c.id
  JOIN cost_group_company k ON k.id = l.company_id;

-- Право — только Full Admin (слова заказчика 23.09.2026). Другим ролям его при
-- необходимости выдаёт администратор через страницу ролей.
UPDATE cost_roles
   SET permissions = permissions || '["cost:group_pricing"]'::jsonb,
       updated_at = now()
 WHERE name IN ('Full Admin')
   AND NOT permissions @> '["cost:group_pricing"]'::jsonb;
