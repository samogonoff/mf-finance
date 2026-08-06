-- 0030 — расширение точности числовых колонок под реальную точность источника.
--
-- Источник [Checks].[dbo].[CostHistory] хранит заметно больше знаков, чем
-- принимали наши колонки, поэтому часть себестоимости терялась ещё на загрузке
-- в кэш (не при отображении):
--
--   Норма                      источник numeric(38,8), было numeric(18,4)
--                              → 26% строк усекалось; фактически источник не
--                                использует больше 6 знаков (проверено: 0 строк
--                                за 3 месяца имеют >6), поэтому берём 6.
--   цена материала, руб./USD.  источник money(19,4) = ровно 4 знака, было (18,2)
--                              → 31% строк усекалось, из них ~7% (69k строк за
--                                3 месяца) обнулялись в 0.00 полностью: реальные
--                                цены вида 0.0005 превращались в ноль, и
--                                стоимость строки исчезала, а не «округлялась».
--   бакеты + Себестоимость     источник numeric(38,6)/money(19,4), было (18,2)
--                              → берём 6, чтобы суммировать без потерь и
--                                округлять до копеек только на отдаче.
--
-- Курс на дату расчета (4), Пошив/Раскрой минуты (2) и Ставка НДС (2) уже
-- совпадают с точностью источника — не трогаем.
-- Цены по уровню оставлены (18,2) осознанно: это утверждаемые цены, копейки достаточно.
--
-- ВАЖНО: расширение колонки НЕ восстанавливает уже усечённые данные — в них
-- останутся те же обрезанные числа. После этой миграции нужен ПОЛНЫЙ рефреш
-- cost_data_cache из источника (load_cost_data_to_cache без cutoff).
-- Строки уже сохранённых версий (cost_calc_version_rows) осознанно НЕ
-- пересчитываются: там могут быть ручные правки пользователей, восстановить их
-- из источника нельзя.
--
-- Смена scale у numeric требует перезаписи таблицы под ACCESS EXCLUSIVE.
-- Все колонки одной таблицы меняем одним ALTER TABLE — это один проход, а не N.

ALTER TABLE cost_data_cache
    ALTER COLUMN "Норма" TYPE numeric(18,6),
    ALTER COLUMN "цена материала, руб." TYPE numeric(18,4),
    ALTER COLUMN "цена материала, USD." TYPE numeric(18,4),
    ALTER COLUMN "Основные материалы, руб." TYPE numeric(18,6),
    ALTER COLUMN "Основные материалы, USD." TYPE numeric(18,6),
    ALTER COLUMN "Вспомогательные материалы, руб." TYPE numeric(18,6),
    ALTER COLUMN "Вспомогательные материалы, USD." TYPE numeric(18,6),
    ALTER COLUMN "Пошив, руб." TYPE numeric(18,6),
    ALTER COLUMN "Пошив, USD." TYPE numeric(18,6),
    ALTER COLUMN "Раскрой, руб." TYPE numeric(18,6),
    ALTER COLUMN "Раскрой, USD." TYPE numeric(18,6),
    ALTER COLUMN "Декоры, руб." TYPE numeric(18,6),
    ALTER COLUMN "Декоры, USD." TYPE numeric(18,6),
    ALTER COLUMN "Вязание, руб." TYPE numeric(18,6),
    ALTER COLUMN "Вязание, USD." TYPE numeric(18,6),
    ALTER COLUMN "Себестоимость, руб." TYPE numeric(18,6),
    ALTER COLUMN "Себестоимость, USD." TYPE numeric(18,6);

-- Зеркально для строк версий: иначе сохранение версии из кэша снова усечёт
-- значения, и «оригинал» перестанет совпадать с источником.
ALTER TABLE cost_calc_version_rows
    ALTER COLUMN "Норма" TYPE numeric(18,6),
    ALTER COLUMN "цена материала, руб." TYPE numeric(18,4),
    ALTER COLUMN "цена материала, USD." TYPE numeric(18,4),
    ALTER COLUMN "Основные материалы, руб." TYPE numeric(18,6),
    ALTER COLUMN "Основные материалы, USD." TYPE numeric(18,6),
    ALTER COLUMN "Вспомогательные материалы, руб." TYPE numeric(18,6),
    ALTER COLUMN "Вспомогательные материалы, USD." TYPE numeric(18,6),
    ALTER COLUMN "Пошив, руб." TYPE numeric(18,6),
    ALTER COLUMN "Пошив, USD." TYPE numeric(18,6),
    ALTER COLUMN "Раскрой, руб." TYPE numeric(18,6),
    ALTER COLUMN "Раскрой, USD." TYPE numeric(18,6),
    ALTER COLUMN "Декоры, руб." TYPE numeric(18,6),
    ALTER COLUMN "Декоры, USD." TYPE numeric(18,6),
    ALTER COLUMN "Вязание, руб." TYPE numeric(18,6),
    ALTER COLUMN "Вязание, USD." TYPE numeric(18,6),
    ALTER COLUMN "Себестоимость, руб." TYPE numeric(18,6),
    ALTER COLUMN "Себестоимость, USD." TYPE numeric(18,6);
