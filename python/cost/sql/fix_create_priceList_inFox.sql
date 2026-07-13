-- =============================================
-- Fix: create_priceList_inFox (nested JSON format)
-- Date:  2026-07-13
--
-- Проблема: старая процедура принимала плоский массив и создавала
-- один прейскурант на весь массив. Нужно создавать отдельный
-- прейскурант на каждый документ из внешнего массива.
--
-- Формат @JSON_IN (вложенный — header + prices1[]):
--
--   [
--     {
--       "plan_id": "9272",
--       "price_type": 3,
--       "calc_sign": "КПСС",
--       "author_name": "Иванов И.И.",
--       "prices1": [
--         {"model":"411220","articul":"26-5956П-5","wholesale_rub":2280.00},
--         {"model":"411221","articul":"27-1234А-2","wholesale_rub":3500.00}
--       ]
--     },
--     {
--       "plan_id": "",
--       "price_type": 1,
--       "calc_sign": "ПФКСС",
--       "author_name": "Иванов И.И.",
--       "prices1": [
--         {"model":"411222","articul":"28-7890Б-3","wholesale_rub":1234.00}
--       ]
--     }
--   ]
--
--   EXEC [dbo].[create_priceList_inFox] @JSON_IN = @json, @JSON_OUT = @out OUTPUT
--   PRINT @out
-- =============================================
ALTER PROCEDURE [dbo].[create_priceList_inFox]
	@JSON_IN VARCHAR(MAX)
	,@JSON_OUT VARCHAR(MAX) = '' OUTPUT
AS
BEGIN
	SET NOCOUNT ON;

	IF ISJSON(@JSON_IN) = 0
	BEGIN
		SET @JSON_OUT = '{"no_valid":2,"mess":"Строка параметров не является строкой формата JSON"}'
		RETURN
	END

	BEGIN TRY
		DECLARE @CURRENT_YEAR INT = YEAR(GETDATE())

		-- Временные таблицы (копия структуры без данных)
		SELECT * INTO #PR  FROM [dbo].[prices]  WHERE 1 <> 1
		SELECT * INTO #PR1 FROM [dbo].[prices1] WHERE 1 <> 1

		-- Курсор по внешнему массиву документов
		DECLARE doc_cursor CURSOR FOR
		SELECT plan_id, price_type, calc_sign, author_name, prices1_json
		FROM OPENJSON(@JSON_IN)
		WITH (
			plan_id       NVARCHAR(50)   '$.plan_id',
			price_type    NVARCHAR(50)   '$.price_type',
			calc_sign     NVARCHAR(50)   '$.calc_sign',
			author_name   NVARCHAR(100)  '$.author_name',
			prices1_json  NVARCHAR(MAX)  '$.prices1' AS JSON
		)

		DECLARE @plan_id      NVARCHAR(50)
		DECLARE @price_type   NVARCHAR(50)
		DECLARE @calc_sign    NVARCHAR(50)
		DECLARE @author_name  NVARCHAR(100)
		DECLARE @prices1_json NVARCHAR(MAX)

		DECLARE @NOMER    NUMERIC(10,0)
		DECLARE @PRICE_ID NUMERIC(8,0)

		OPEN doc_cursor
		FETCH NEXT FROM doc_cursor INTO @plan_id, @price_type, @calc_sign, @author_name, @prices1_json

		WHILE @@FETCH_STATUS = 0
		BEGIN
			-- Свой NOMER и PRICE_ID на каждый документ-прейскурант
			EXEC @NOMER    = [punictabl] 'prices',   1, '',       999999999, @CURRENT_YEAR
			EXEC @PRICE_ID = [punictabl] 'price_id', 1, null,     99999999

			-- Заголовок прейскуранта
			INSERT INTO #PR (NOMER, ITEM_ID, USERVRKV, PRIM, DATEVRKV, CORRECTION, DATA, STATUS, CENA_TIP, NACH_VVOD, REGION_ID, ISCONFIRM)
			VALUES (
				CAST(@NOMER AS CHAR(10)),
				@PRICE_ID,
				@author_name,
				'PLAN_ID: ' + ISNULL(@plan_id, '') + ' CENA_TIP: ' + ISNULL(@calc_sign, ''),
				GETDATE(), 0, GETDATE(), 0, CAST(@price_type AS NUMERIC(1,0)), 0, 0, 0
			)

			-- Строки прейскуранта из prices1 (для пустого массива ничего не вставится)
			INSERT INTO #PR1 ([MODEL_ID], [OLD_CENA], [CENA], [PRICE_ID], [NPRICE], [PROPER_ID])
			SELECT
				(SELECT ITEM_ID FROM s_modeli S WHERE S.MODEL = item.model AND S.ART = item.articul),
				0,
				CAST(item.wholesale_rub AS NUMERIC(11,2)),
				@PRICE_ID,
				'',
				0
			FROM OPENJSON(@prices1_json)
			WITH (
				model         NVARCHAR(50)   '$.model',
				articul       NVARCHAR(50)   '$.articul',
				wholesale_rub DECIMAL(18,2)  '$.wholesale_rub'
			) AS item

			FETCH NEXT FROM doc_cursor INTO @plan_id, @price_type, @calc_sign, @author_name, @prices1_json
		END

		CLOSE doc_cursor
		DEALLOCATE doc_cursor

		-- Запись в реальные таблицы
		INSERT INTO [dbo].[prices]  SELECT * FROM #PR
		INSERT INTO [dbo].[prices1] SELECT * FROM #PR1

		SET @JSON_OUT = '{"no_valid":0,"mess":"Запись добавлена в БД"}'

		DROP TABLE #PR
		DROP TABLE #PR1
	END TRY
	BEGIN CATCH
		DECLARE @ERROR_NUMBER   INT = ERROR_NUMBER()
		DECLARE @ERROR_MESSAGE  NVARCHAR(4000) = ERROR_MESSAGE()
		DECLARE @ERROR_SEVERITY INT = ERROR_SEVERITY()
		DECLARE @ERROR_STATE    INT = ERROR_STATE()
		DECLARE @ERROR_LINE     INT = ERROR_LINE()

		SET @JSON_OUT = '{"no_valid":1,"mess":" ОШИБКА №'
			+ CAST(@ERROR_NUMBER AS VARCHAR) + ', '
			+ REPLACE(@ERROR_MESSAGE, '"', '\"') + '", '
			+ CAST(@ERROR_LINE AS VARCHAR) + '"}'
	END CATCH
END
