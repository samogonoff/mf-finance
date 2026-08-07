-- Откат 0030 — возврат к прежней (урезанной) точности.
--
-- ВНИМАНИЕ: откат ЛОССИ. Сужение scale у numeric округляет значения на месте,
-- восстановить их потом уже нельзя. В частности цены материалов вида 0.0005
-- снова станут 0.00 и стоимость соответствующих строк снова обнулится.
-- Применять только если действительно нужно вернуть прежнюю схему; после
-- откатa обязателен полный рефреш кэша из источника.

ALTER TABLE cost_calc_version_rows
    ALTER COLUMN "Норма" TYPE numeric(18,4),
    ALTER COLUMN "цена материала, руб." TYPE numeric(18,2),
    ALTER COLUMN "цена материала, USD." TYPE numeric(18,2),
    ALTER COLUMN "Основные материалы, руб." TYPE numeric(18,2),
    ALTER COLUMN "Основные материалы, USD." TYPE numeric(18,2),
    ALTER COLUMN "Вспомогательные материалы, руб." TYPE numeric(18,2),
    ALTER COLUMN "Вспомогательные материалы, USD." TYPE numeric(18,2),
    ALTER COLUMN "Пошив, руб." TYPE numeric(18,2),
    ALTER COLUMN "Пошив, USD." TYPE numeric(18,2),
    ALTER COLUMN "Раскрой, руб." TYPE numeric(18,2),
    ALTER COLUMN "Раскрой, USD." TYPE numeric(18,2),
    ALTER COLUMN "Декоры, руб." TYPE numeric(18,2),
    ALTER COLUMN "Декоры, USD." TYPE numeric(18,2),
    ALTER COLUMN "Вязание, руб." TYPE numeric(18,2),
    ALTER COLUMN "Вязание, USD." TYPE numeric(18,2),
    ALTER COLUMN "Себестоимость, руб." TYPE numeric(18,2),
    ALTER COLUMN "Себестоимость, USD." TYPE numeric(18,2);

ALTER TABLE cost_data_cache
    ALTER COLUMN "Норма" TYPE numeric(18,4),
    ALTER COLUMN "цена материала, руб." TYPE numeric(18,2),
    ALTER COLUMN "цена материала, USD." TYPE numeric(18,2),
    ALTER COLUMN "Основные материалы, руб." TYPE numeric(18,2),
    ALTER COLUMN "Основные материалы, USD." TYPE numeric(18,2),
    ALTER COLUMN "Вспомогательные материалы, руб." TYPE numeric(18,2),
    ALTER COLUMN "Вспомогательные материалы, USD." TYPE numeric(18,2),
    ALTER COLUMN "Пошив, руб." TYPE numeric(18,2),
    ALTER COLUMN "Пошив, USD." TYPE numeric(18,2),
    ALTER COLUMN "Раскрой, руб." TYPE numeric(18,2),
    ALTER COLUMN "Раскрой, USD." TYPE numeric(18,2),
    ALTER COLUMN "Декоры, руб." TYPE numeric(18,2),
    ALTER COLUMN "Декоры, USD." TYPE numeric(18,2),
    ALTER COLUMN "Вязание, руб." TYPE numeric(18,2),
    ALTER COLUMN "Вязание, USD." TYPE numeric(18,2),
    ALTER COLUMN "Себестоимость, руб." TYPE numeric(18,2),
    ALTER COLUMN "Себестоимость, USD." TYPE numeric(18,2);
