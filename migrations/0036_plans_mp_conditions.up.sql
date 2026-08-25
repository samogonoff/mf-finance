-- 0036 — реестр «Условия площадки» и общие затраты МП.
--
-- Главное изменение скорректированного ТЗ (§3.1, §6.1): инверсия направления
-- расчёта. Сегодня из источника приходят СУММЫ, а доли/наценки/%СПП вычисляются
-- как производные. Требование звонка 06.08.2026 — наоборот: «должен быть шаблон
-- для ввода удельных весов затрат по каждому направлению, и когда согласовываются
-- продажи, автоматически рассчитывается вся расходная часть». Источником истины
-- становится реестр условий площадки, а суммы — производными от него.
--
-- Почему отдельная таблица, а не plans_directory: условия версионируются ПО
-- ПЕРИОДУ, участвуют в снапшоте версии карточки и в diff для согласующего
-- («согласующий видит, какие доли изменились и на сколько»), требуют обоснования
-- при отклонении сверх порога. Справочник строк такого не умеет.

CREATE TABLE IF NOT EXISTS mp_conditions (
    id                     BIGSERIAL PRIMARY KEY,
    pl_id                  BIGINT NOT NULL REFERENCES pl_instance(id) ON DELETE CASCADE,
    code_cfo               INT  NOT NULL,                 -- площадка (335/336/337/954/…)
    period_year            INT  NOT NULL,
    period_month           INT  NOT NULL,
    -- %СПП: «внутренняя история площадки», но в рамках оговорённых весов (ТЗ §6.1).
    spp_pct                NUMERIC(12,6) NOT NULL DEFAULT 0,
    -- Наценка к отпускным ценам и к общей себестоимости (Cost of goods).
    markup_pct             NUMERIC(12,6) NOT NULL DEFAULT 0,
    markup_total_pct       NUMERIC(12,6) NOT NULL DEFAULT 0,
    -- Эффективная ставка НДС площадки. NULL → берётся из справочника dir_vat
    -- (0034): у площадки она не законодательная (20,36 % / 16,62 %, ТЗ §3.4).
    vat_rate               NUMERIC(12,6),
    currency               TEXT NOT NULL DEFAULT '',       -- валюта площадки (RUB/KZT/UZS)
    valid_from             DATE,
    valid_to               DATE,
    version                INT  NOT NULL DEFAULT 1,
    -- Обоснование обязательно, когда доля/СПП отклонились от прошлого периода
    -- сверх порога (проверка в коде, ТЗ §6.1 и МП-W1/W2).
    change_reason          TEXT NOT NULL DEFAULT '',
    status                 TEXT NOT NULL DEFAULT 'draft',  -- draft|approved
    author_id              BIGINT REFERENCES users(id),
    created_at             TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at             TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    UNIQUE (pl_id, code_cfo, period_year, period_month)
);
CREATE INDEX IF NOT EXISTS idx_mp_conditions_pl ON mp_conditions(pl_id, period_year, period_month);

-- Доли статей прямых затрат: отдельной строкой на статью, потому что набор
-- статей у площадок РАЗНЫЙ (у Lamoda и Yandex Market есть 48, у Wildberries и
-- Ozon её нет), а доля может быть ОТРИЦАТЕЛЬНОЙ (компенсации, ТЗ §4.2).
CREATE TABLE IF NOT EXISTS mp_condition_item (
    id            BIGSERIAL PRIMARY KEY,
    conditions_id BIGINT NOT NULL REFERENCES mp_conditions(id) ON DELETE CASCADE,
    block_type    TEXT NOT NULL,                  -- cost_agent, cost_freight, …
    code_pl       INT  NOT NULL DEFAULT 0,
    share_pct     NUMERIC(12,6) NOT NULL DEFAULT 0,
    UNIQUE (conditions_id, block_type)
);

-- Общие затраты по МП (кроме прямых): вводятся по обычным статьям PL в 7 группах
-- и НЕ считаются от продаж (ТЗ §4.3). Одно значение на статью и месяц по всему
-- сегменту — привязки к площадке нет.
CREATE TABLE IF NOT EXISTS mp_common_cost (
    id           BIGSERIAL PRIMARY KEY,
    pl_id        BIGINT NOT NULL REFERENCES pl_instance(id) ON DELETE CASCADE,
    segment      TEXT NOT NULL DEFAULT '',        -- large|small|'' (весь блок МП)
    code_pl      INT  NOT NULL,
    period_year  INT  NOT NULL,
    period_month INT  NOT NULL,
    currency     TEXT NOT NULL DEFAULT 'RUB',
    amount       NUMERIC(20,4) NOT NULL DEFAULT 0,
    source       TEXT NOT NULL DEFAULT 'manual',  -- manual|from_prev|from_strategy
    updated_by   BIGINT REFERENCES users(id),
    updated_at   TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    UNIQUE (pl_id, segment, code_pl, period_year, period_month, currency)
);

-- Лог расчёта по ячейке: входы, формула, версия условий, результат (ТЗ §7.2
-- «лог расчёта по каждой вычисленной ячейке»). Нужен для разбора «почему такая
-- сумма» и для доказательства, что закрытый период не пересчитывался.
CREATE TABLE IF NOT EXISTS mp_calc_log (
    id            BIGSERIAL PRIMARY KEY,
    pl_id         BIGINT NOT NULL REFERENCES pl_instance(id) ON DELETE CASCADE,
    code_cfo      INT  NOT NULL,
    block_type    TEXT NOT NULL,
    period_year   INT  NOT NULL,
    period_month  INT  NOT NULL,
    formula       TEXT NOT NULL DEFAULT '',
    inputs        JSONB NOT NULL DEFAULT '{}',
    result        NUMERIC(20,4),
    conditions_version INT NOT NULL DEFAULT 0,
    calc_mode     TEXT NOT NULL DEFAULT 'inverse',
    created_at    TIMESTAMPTZ NOT NULL DEFAULT NOW()
);
CREATE INDEX IF NOT EXISTS idx_mp_calc_log_cell
    ON mp_calc_log(pl_id, code_cfo, block_type, period_year, period_month);

-- Реестр владельцев расчёта пары (CodePL, CodeCFO) → форма-владелец.
-- ТЗ §8.1: двойной счёт подтверждён на данных — ЦФО 250 стоит в форме ЦЗ 32 со
-- статьями 51 и 52, и те же статьи считаются долей в форме МП. Владелец
-- (рекомендация ТЗ — форма МП) держит расчёт, у не-владельца строка read-only
-- с подписью «источник: форма …». Контроль МП-08 не даёт одной комбинации
-- (ЦФО, статья, месяц, ЮЛ) прийти из двух форм.
CREATE TABLE IF NOT EXISTS plans_calc_owner (
    id          BIGSERIAL PRIMARY KEY,
    code_pl     INT  NOT NULL,
    code_cfo    INT  NOT NULL,
    owner_form  TEXT NOT NULL,                   -- TPL-MP | TPL-CFO-EXP(ЦЗ 32) | …
    valid_from  DATE NOT NULL DEFAULT CURRENT_DATE,
    valid_to    DATE,
    note        TEXT NOT NULL DEFAULT '',
    updated_by  BIGINT REFERENCES users(id),
    updated_at  TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    UNIQUE (code_pl, code_cfo, valid_from)
);

-- Сид владельцев по рекомендации ТЗ §8.1: логистические статьи 51/52/54 по ЦФО
-- маркетплейсов считает форма МП (доля согласуется с площадкой единым
-- переговорным пакетом вместе с СПП и комиссией — разрывать пакет нельзя).
-- Решение финблока по §12 п.6 может это изменить — тогда правится строка, не код.
INSERT INTO plans_calc_owner (code_pl, code_cfo, owner_form, note)
SELECT pl, cfo, 'TPL-MP', 'рекомендация ТЗ §8.1; ЦЗ 32 исключает ЦФО 250/480 по этим статьям'
  FROM (VALUES (51), (52), (54)) AS p(pl)
 CROSS JOIN (VALUES (250), (480), (335), (336), (337), (954)) AS c(cfo)
ON CONFLICT (code_pl, code_cfo, valid_from) DO NOTHING;
