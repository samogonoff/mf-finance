-- 0049 — мультипаки: себестоимость пака = Σ вложенных калькуляций + упаковка
--
-- ЗАДАЧА
-- ======
-- В группе «Носки&Колготки» есть модель-артикулы, которые физически содержат
-- 3/5/7 пар, а продаются как ОДНА единица. Их себестоимость — сумма
-- себестоимостей входящих одиночек плюс материалы упаковки. Дальше пак живёт
-- обычной калькуляцией: цена, согласование ПЭО, запись в DWH.
--
-- В источнике ([Checks].[dbo].[CostHistory]) слот такой калькуляции ЕСТЬ, но
-- пустой. Пример на 02.09.2026: модель 430A-3839 / артикул B3-263430A
-- «НОСКИ ДЕТСКИЕ (3 пары)», признак ПКПСС — две строки типа «шт» (декоры) с
-- нулевой суммой, себестоимость 0.00. Заказчик подтвердил: мультипаков БЕЗ
-- слота в источнике не бывает — цена пишется обратно в источник по ключу через
-- процедуру, поэтому калькуляция без слота смысла не имеет. Отсюда: создавать
-- калькуляцию с нуля (как cost_manual_calc) здесь НЕ нужно, наполняется
-- существующий ключ.
--
-- Связь пак → одиночки из данных НЕ выводится: у пака артикул B3-263430A, у
-- одиночек — B2-124430A и подобные, общего корня нет. Состав задаётся руками.
--
-- ПОЧЕМУ ТРИ ТАБЛИЦЫ
-- ==================
-- cost_multipack       — шапка, ключ (Модель, Артикул). БЕЗ признака
--                        калькуляции и плана: заказчик решил 02.09.2026, что
--                        ключ состава — модель/артикул. Пак проходит этапы
--                        ПКПСС → КПСС → ПФКСС одним и тем же составом, меняются
--                        только суммы одиночек. Держать состав на (модель,
--                        артикул, признак, план) значило бы заводить его заново
--                        на каждом этапе.
-- cost_multipack_item  — состав: какие одиночки и по сколько штук.
-- cost_multipack_build — журнал сборок со СНАПШОТОМ сумм. Отдельно от состава
--                        именно потому, что состав общий для всех этапов, а
--                        суммы у каждого этапа свои: снапшот, лежащий на строке
--                        состава, затирался бы при сборке следующего этапа, и
--                        детект «в источнике пересчитали» врал бы.
--
-- ГДЕ ЛЕЖАТ САМИ СТРОКИ РАСЧЁТА
-- =============================
-- Здесь — только состав, не строки. Строки пака (свёртки одиночек + упаковка)
-- живут в версии калькуляции (cost_calc_versions / cost_calc_version_rows) и
-- попадают в кэш штатным путём — _apply_version_rows_to_cache. Это даёт
-- бесплатно: цену, согласование ПЭО, запись в DWH, блокировки и переживание
-- refresh кэша (_reapply_active_versions_to_cache переприменяет pending и
-- approved версии после каждого реимпорта).

CREATE TABLE IF NOT EXISTS cost_multipack (
    id          BIGSERIAL   PRIMARY KEY,
    model       TEXT        NOT NULL,
    articul     TEXT        NOT NULL,
    -- Сколько единиц в паке ПО ПАСПОРТУ (3/5/7). Информационное поле: реальное
    -- количество считается суммой qty по составу, и они могут расходиться —
    -- пак из 3 пар может собираться как 2 одиночки одной модели + 1 другой.
    -- Расхождение показываем в интерфейсе, но не запрещаем.
    pack_size   NUMERIC,
    note        TEXT,
    created_by  TEXT        NOT NULL,
    created_at  TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_by  TEXT,
    updated_at  TIMESTAMPTZ NOT NULL DEFAULT now(),
    UNIQUE (model, articul)
);

COMMENT ON TABLE cost_multipack IS
    'Мультипаки: модель-артикулы, продаваемые как одна единица, а физически содержащие несколько одиночек. Ключ (модель, артикул) — состав общий для всех этапов калькуляции.';

CREATE TABLE IF NOT EXISTS cost_multipack_item (
    id            BIGSERIAL PRIMARY KEY,
    multipack_id  BIGINT    NOT NULL REFERENCES cost_multipack(id) ON DELETE CASCADE,

    src_model     TEXT      NOT NULL,
    src_articul   TEXT      NOT NULL,
    -- Пусто = «следовать контексту пака»: собираем из одиночки с тем же
    -- признаком калькуляции и планом, что у самого пака. Так состав переносится
    -- между этапами без правки. Заполнено = пользователь зафиксировал
    -- конкретную калькуляцию одиночки (например, у одиночки нет ПФКСС, и берём
    -- КПСС) — тогда сборка берёт именно её и предупреждает о разнице признаков.
    src_calc_sign TEXT      NOT NULL DEFAULT '',
    src_plan_id   TEXT      NOT NULL DEFAULT '',

    qty           NUMERIC   NOT NULL CHECK (qty > 0),
    position      INTEGER   NOT NULL DEFAULT 0,
    created_at    TIMESTAMPTZ NOT NULL DEFAULT now(),

    -- Одна одиночка входит в пак один раз — количество задаётся qty, а не
    -- повтором строки: иначе «пересобрать» не смог бы сопоставить снапшот.
    UNIQUE (multipack_id, src_model, src_articul, src_calc_sign, src_plan_id)
);

CREATE INDEX IF NOT EXISTS idx_multipack_item_pack ON cost_multipack_item (multipack_id, position);
-- Обратный поиск «в какие паки входит эта одиночка»: нужен, чтобы при правке
-- калькуляции одиночки показать, что она влияет на N паков.
CREATE INDEX IF NOT EXISTS idx_multipack_item_src ON cost_multipack_item (src_model, src_articul);

COMMENT ON TABLE cost_multipack_item IS
    'Состав мультипака: какие калькуляции-одиночки и по сколько штук входят. Пустые src_calc_sign/src_plan_id означают «брать одиночку того же этапа и плана, что у пака».';

CREATE TABLE IF NOT EXISTS cost_multipack_build (
    id            BIGSERIAL   PRIMARY KEY,
    multipack_id  BIGINT      NOT NULL REFERENCES cost_multipack(id) ON DELETE CASCADE,

    -- Контекст сборки — конкретная калькуляция пака.
    calc_sign     TEXT        NOT NULL DEFAULT '',
    plan_id       TEXT        NOT NULL DEFAULT '',
    task_number   TEXT        NOT NULL DEFAULT '',

    -- Версия калькуляции, в которую легли собранные строки. NULL, если сборку
    -- посчитали, но версию ещё не сохранили (предпросмотр в редакторе).
    version_id    BIGINT      REFERENCES cost_calc_versions(id) ON DELETE SET NULL,

    -- Снапшот: по каждой позиции — какая калькуляция одиночки взята (модель,
    -- артикул, признак, план, дата расчёта, курс) и её суммы по статьям на
    -- момент сборки. По нему детектируется «в источнике пересчитали»:
    -- сравниваем с текущими суммами и показываем «было X → стало Y».
    items         JSONB       NOT NULL DEFAULT '[]'::jsonb,

    -- Итоги сборки, чтобы журнал читался без разбора JSONB.
    items_rub     NUMERIC,
    items_usd     NUMERIC,
    packaging_rub NUMERIC,
    packaging_usd NUMERIC,
    total_rub     NUMERIC,
    total_usd     NUMERIC,

    built_by      TEXT        NOT NULL,
    built_at      TIMESTAMPTZ NOT NULL DEFAULT now()
);

-- «Последняя сборка этого пака на этом этапе» — основной запрос детекта
-- устаревания, выполняется при каждом открытии редактора.
CREATE INDEX IF NOT EXISTS idx_multipack_build_ctx
    ON cost_multipack_build (multipack_id, calc_sign, plan_id, task_number, built_at DESC);

COMMENT ON TABLE cost_multipack_build IS
    'Журнал сборок мультипака со снапшотом сумм одиночек на момент сборки. Снапшот здесь, а не на строке состава, потому что состав общий для всех этапов, а суммы у каждого этапа свои.';

-- Право на ведение состава. По решению заказчика 02.09.2026 — калькулятор И
-- ПЭО (в отличие от cost:calc_sign_copy, где ПЭО исключён намеренно).
UPDATE cost_roles
   SET permissions = permissions || '["cost:multipack"]'::jsonb,
       updated_at = now()
 WHERE name IN ('Калькулятор', 'ПЭО', 'Full Admin')
   AND NOT permissions @> '["cost:multipack"]'::jsonb;
