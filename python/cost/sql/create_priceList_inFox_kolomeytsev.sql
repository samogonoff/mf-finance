USE [test_Gpartner]
GO
/****** Object:  StoredProcedure [dbo].[create_priceList_inFox]    Script Date: 05.08.2026 16:49:20 ******/
SET ANSI_NULLS ON
GO
SET QUOTED_IDENTIFIER ON
GO
-- =============================================
-- Author:		Коломейцев А.С.
-- Create date: 05.08.2026
-- Description:	Процедура на создание прейскурантов в Лисе
-- EXEC [dbo].[create_priceList_inFox] @JSON_IN = '[{"plan_id":"1000","price_type":3,"calc_sign":"КПСС","author_name":"Иванов И.И.","prices1":[{"model":"411220","articul":"26-5956П-5","wholesale_rub":2280.00,"plan_price":0,"price_level_id":64},{"model":"411221","articul":"22/3322П-5","wholesale_rub":3500.00,"plan_price":10000}]},{"plan_id":"1001","price_type":1,"calc_sign":"","author_name":"Петров П.П.","prices1":[{"model":"411222","articul":"22/3294П-0","wholesale_rub":1500.00,"plan_price":1200,"price_level_id":64}]},{"plan_id":"1002","price_type":3,"calc_sign":"КПСС","author_name":"Федоров П.П.","prices1":[{"model":"1040PA-1414","articul":"251040PAN","wholesale_rub":1533.00,"plan_price":180}]}]'
-- =============================================
ALTER PROCEDURE [dbo].[create_priceList_inFox]
	@JSON_IN NVARCHAR(MAX)
	,@JSON_OUT NVARCHAR(MAX) = '' OUTPUT
AS
BEGIN
	DECLARE @NOMER NUMERIC(10,0)
	DECLARE @PRICE_ID NUMERIC(8,0)
	DECLARE @CURRENT_YEAR INT = YEAR(GETDATE())
	DECLARE @ERROR_COUNT INT = 0
	DECLARE @SUCCESS_COUNT INT = 0
	DECLARE @ERROR_MESSAGES NVARCHAR(MAX) = ''
	DECLARE @TOTAL_RECORDS INT = 0
	DECLARE @PLAN_ID NVARCHAR(50)
	DECLARE @CALC_SIGN NVARCHAR(50)
	DECLARE @PRICE_TYPE NVARCHAR(50)
	DECLARE @AUTHOR_NAME NVARCHAR(100)
	DECLARE @PRIM NVARCHAR(MAX)
	DECLARE @PLAN_PRICE_UPDATED INT = 0
	DECLARE @PRICE_LEVEL_UPDATED INT = 0

	SET NOCOUNT ON;
	
	-- Проверка на валидность JSON
	IF ISJSON(@JSON_IN) = 0
	BEGIN
		SET @JSON_OUT = '{"NO_VALID":2,"MESS":"Строка параметров не является строкой формата JSON"}'
		RETURN
	END
	
	-- Проверяем, что на входе массив
	IF LEFT(LTRIM(@JSON_IN), 1) != '['
	BEGIN
		SET @JSON_OUT = '{"NO_VALID":2,"MESS":"Ожидается JSON-массив"}'
		RETURN
	END

	BEGIN TRY
		-- Временная таблица для хранения данных из JSON
		CREATE TABLE #JSON_DATA (
			ROW_NUM INT IDENTITY(1,1),
			PARENT_ROW_NUM INT,
			MODEL NVARCHAR(50),
			ARTICUL NVARCHAR(50),
			PLAN_ID NVARCHAR(50),
			WHOLESALE_RUB DECIMAL(18,2),
			PLAN_PRICE DECIMAL(18,2),
			CALC_SIGN NVARCHAR(50),
			PRICE_TYPE NVARCHAR(50),
			AUTHOR_NAME NVARCHAR(100),
			PRICE_LEVEL_ID INT,
			MODEL_ID NUMERIC(8,0),
			PRIM NVARCHAR(MAX),
			CENA NUMERIC(11,2),
			CENA_TIP NUMERIC(1,0),
			PRICE_ID NUMERIC(8,0),
			NOMER NUMERIC(10,0)
		)

		-- Создаем временную таблицу для родительских объектов
		CREATE TABLE #PARENT_DATA (
			PARENT_ROW_NUM INT IDENTITY(1,1),
			PLAN_ID NVARCHAR(50),
			CALC_SIGN NVARCHAR(50),
			PRICE_TYPE NVARCHAR(50),
			AUTHOR_NAME NVARCHAR(100)
		)

		-- Парсим основной массив (родительские объекты)
		INSERT INTO #PARENT_DATA (PLAN_ID, CALC_SIGN, PRICE_TYPE, AUTHOR_NAME)
		SELECT 
			plan_id,
			calc_sign,
			price_type,
			author_name
		FROM OPENJSON(@JSON_IN)
		WITH (
			plan_id NVARCHAR(50) '$.plan_id',
			calc_sign NVARCHAR(50) '$.calc_sign',
			price_type NVARCHAR(50) '$.price_type',
			author_name NVARCHAR(100) '$.author_name'
		)

		-- Парсим массив prices1 из каждого родительского объекта
		INSERT INTO #JSON_DATA (
			PARENT_ROW_NUM,
			MODEL, 
			ARTICUL, 
			PLAN_ID, 
			WHOLESALE_RUB,
			PLAN_PRICE,
			CALC_SIGN, 
			PRICE_TYPE, 
			AUTHOR_NAME,
			PRICE_LEVEL_ID
		)
		SELECT 
			P.PARENT_ROW_NUM,
			Prices.model,
			Prices.articul,
			P.PLAN_ID,
			Prices.wholesale_rub,
			ISNULL(Prices.plan_price, 0),
			P.CALC_SIGN,
			P.PRICE_TYPE,
			P.AUTHOR_NAME,
			ISNULL(Prices.price_level_id, 0)
		FROM #PARENT_DATA P
		CROSS APPLY OPENJSON(
			(
				SELECT value 
				FROM OPENJSON(@JSON_IN) 
				WHERE [key] = P.PARENT_ROW_NUM - 1
			),
			'$.prices1'
		) WITH (
			model NVARCHAR(50) '$.model',
			articul NVARCHAR(50) '$.articul',
			wholesale_rub DECIMAL(18,2) '$.wholesale_rub',
			plan_price DECIMAL(18,2) '$.plan_price',
			price_level_id INT '$.price_level_id'
		) AS Prices

		-- Записи по MODEL_ID
		CREATE TABLE #EXPANDED_DATA (
			ROW_NUM INT IDENTITY(1,1),
			PARENT_ROW_NUM INT,
			MODEL NVARCHAR(50),
			ARTICUL NVARCHAR(50),
			PLAN_ID NVARCHAR(50),
			WHOLESALE_RUB DECIMAL(18,2),
			PLAN_PRICE DECIMAL(18,2),
			CALC_SIGN NVARCHAR(50),
			PRICE_TYPE NVARCHAR(50),
			AUTHOR_NAME NVARCHAR(100),
			PRICE_LEVEL_ID INT,
			MODEL_ID NUMERIC(8,0),
			PRIM NVARCHAR(MAX),
			CENA NUMERIC(11,2),
			CENA_TIP NUMERIC(1,0),
			PRICE_ID NUMERIC(8,0),
			NOMER NUMERIC(10,0)
		)

		-- Разворачиваем записи: для каждой комбинации MODEL+ART ищем все MODEL_ID
		INSERT INTO #EXPANDED_DATA (
			PARENT_ROW_NUM,
			MODEL, 
			ARTICUL, 
			PLAN_ID, 
			WHOLESALE_RUB,
			PLAN_PRICE,
			CALC_SIGN, 
			PRICE_TYPE, 
			AUTHOR_NAME,
			PRICE_LEVEL_ID,
			MODEL_ID,
			PRIM,
			CENA,
			CENA_TIP
		)
		SELECT 
			JD.PARENT_ROW_NUM,
			JD.MODEL,
			JD.ARTICUL,
			JD.PLAN_ID,
			JD.WHOLESALE_RUB,
			JD.PLAN_PRICE,
			JD.CALC_SIGN,
			JD.PRICE_TYPE,
			JD.AUTHOR_NAME,
			JD.PRICE_LEVEL_ID,
			S.ITEM_ID AS MODEL_ID,
			'PLAN_ID: ' + JD.PLAN_ID + ' AUTHOR: ' + JD.AUTHOR_NAME AS PRIM,
			CAST(JD.WHOLESALE_RUB AS NUMERIC(11,2)) AS CENA,
			CAST(JD.PRICE_TYPE AS NUMERIC(1,0)) AS CENA_TIP
		FROM #JSON_DATA JD
		INNER JOIN s_modeli S 
			ON S.MODEL = JD.MODEL COLLATE Cyrillic_General_CI_AS 
			AND S.ART = JD.ARTICUL COLLATE Cyrillic_General_CI_AS

		-- Проверяем, все ли модели найдены
		IF EXISTS (SELECT 1 FROM #EXPANDED_DATA WHERE MODEL_ID IS NULL)
		BEGIN
			SELECT @ERROR_MESSAGES = STRING_AGG(
				CONCAT('Модель ', MODEL, ' (арт. ', ARTICUL, ') не найдена'), 
				'; '
			) FROM (
				SELECT DISTINCT MODEL, ARTICUL 
				FROM #EXPANDED_DATA 
				WHERE MODEL_ID IS NULL
			) AS T
			
			SET @JSON_OUT = '{"NO_VALID":1,"MESS":"' + @ERROR_MESSAGES + '"}'
			DROP TABLE #JSON_DATA
			DROP TABLE #PARENT_DATA
			DROP TABLE #EXPANDED_DATA
			RETURN
		END

		-- Создаем таблицу для результатов по каждому plan_id
		CREATE TABLE #PLAN_RESULTS (
			PLAN_ID NVARCHAR(50),
			NOMER NUMERIC(10,0),
			PRICE_ID NUMERIC(8,0),
			RECORDS_COUNT INT
		)

		-- Таблица для хранения обновлений PLAN_PRICE
		CREATE TABLE #PLAN_PRICE_UPDATES (
			MODEL_ID NUMERIC(8,0),
			OLD_PLAN_PRICE DECIMAL(18,2),
			NEW_PLAN_PRICE DECIMAL(18,2),
			MODEL NVARCHAR(50),
			ARTICUL NVARCHAR(50)
		)

		-- Таблица для хранения обновлений PRICE_LEVEL_ID
		CREATE TABLE #PRICE_LEVEL_UPDATES (
			MODEL_ID NUMERIC(8,0),
			OLD_PRICE_LEVEL_ID INT,
			NEW_PRICE_LEVEL_ID INT,
			MODEL NVARCHAR(50),
			ARTICUL NVARCHAR(50)
		)

		-- Курсор для перебора родительских объектов (plan_id)
		DECLARE PARENT_CURSOR CURSOR LOCAL FAST_FORWARD FOR
			SELECT 
				PARENT_ROW_NUM,
				PLAN_ID,
				CALC_SIGN,
				PRICE_TYPE,
				AUTHOR_NAME
			FROM #PARENT_DATA
			ORDER BY PARENT_ROW_NUM

		DECLARE @PARENT_ROW_NUM INT
		
		OPEN PARENT_CURSOR
		FETCH NEXT FROM PARENT_CURSOR INTO @PARENT_ROW_NUM, @PLAN_ID, @CALC_SIGN, @PRICE_TYPE, @AUTHOR_NAME

		WHILE @@FETCH_STATUS = 0
		BEGIN
			-- Создаем новый NOMER для каждого plan_id
			EXEC @NOMER = [punictabl] 'prices', 1, '', 999999999, @CURRENT_YEAR
			
			-- Создаем новый PRICE_ID для текущего plan_id
			EXEC @PRICE_ID = [punictabl] 'price_id', 1, NULL, 99999999
			
			-- Формируем PRIM для текущего plan_id
			SET @PRIM = 'PLAN_ID: ' + @PLAN_ID + ' AUTHOR: ' + @AUTHOR_NAME

			-- 1. Вставляем запись в таблицу prices для текущего plan_id
			INSERT INTO [dbo].[prices] (
				NOMER,
				ITEM_ID,
				USERVRKV,
				PRIM,
				DATEVRKV,
				CORRECTION,
				DATA,
				STATUS,
				CENA_TIP,
				NACH_VVOD,
				REGION_ID,
				ISCONFIRM
			) VALUES (
				CAST(@NOMER AS CHAR(10)),
				@PRICE_ID,
				@AUTHOR_NAME,
				@PRIM,
				GETDATE(),
				0,
				GETDATE(),
				3,
				CAST(@PRICE_TYPE AS NUMERIC(1,0)),
				0,
				0,
				0
			)

			-- Обновляем PRICE_ID и NOMER для всех записей текущего plan_id в развернутой таблице
			UPDATE #EXPANDED_DATA 
			SET 
				PRICE_ID = @PRICE_ID,
				NOMER = @NOMER
			WHERE PARENT_ROW_NUM = @PARENT_ROW_NUM

			-- Получаем количество записей для текущего plan_id
			DECLARE @RECORDS_COUNT INT = (SELECT COUNT(*) FROM #EXPANDED_DATA WHERE PARENT_ROW_NUM = @PARENT_ROW_NUM)

			-- Сохраняем результат
			INSERT INTO #PLAN_RESULTS (PLAN_ID, NOMER, PRICE_ID, RECORDS_COUNT)
			VALUES (@PLAN_ID, @NOMER, @PRICE_ID, @RECORDS_COUNT)

			-- 2. Вставляем все записи из prices1 для текущего plan_id (развернутые)
			INSERT INTO [dbo].[prices1] (
				MODEL_ID,
				OLD_CENA,
				CENA,
				PRICE_ID,
				NPRICE,
				PROPER_ID
			)
			SELECT 
				MODEL_ID,
				0,
				CENA,
				PRICE_ID,
				'',
				0
			FROM #EXPANDED_DATA
			WHERE PARENT_ROW_NUM = @PARENT_ROW_NUM

			SET @SUCCESS_COUNT = @SUCCESS_COUNT + @RECORDS_COUNT

			FETCH NEXT FROM PARENT_CURSOR INTO @PARENT_ROW_NUM, @PLAN_ID, @CALC_SIGN, @PRICE_TYPE, @AUTHOR_NAME
		END

		CLOSE PARENT_CURSOR
		DEALLOCATE PARENT_CURSOR

		-- 3. Обновление PLAN_PRICE В s_modeli 
		
		-- Сохраняем информацию о том, какие PLAN_PRICE будут обновлены
		INSERT INTO #PLAN_PRICE_UPDATES (
			MODEL_ID,
			OLD_PLAN_PRICE,
			NEW_PLAN_PRICE,
			MODEL,
			ARTICUL
		)
		SELECT 
			S.ITEM_ID,
			S.PLAN_PRICE AS OLD_PLAN_PRICE,
			E.PLAN_PRICE AS NEW_PLAN_PRICE,
			S.MODEL,
			S.ART
		FROM #EXPANDED_DATA E
		INNER JOIN s_modeli S ON S.ITEM_ID = E.MODEL_ID
		WHERE E.PLAN_PRICE > 0 
			AND (S.PLAN_PRICE IS NULL OR E.PLAN_PRICE > S.PLAN_PRICE)
		GROUP BY S.ITEM_ID, S.PLAN_PRICE, E.PLAN_PRICE, S.MODEL, S.ART

		-- Обновляем PLAN_PRICE в s_modeli
		UPDATE S
		SET 
			S.PLAN_PRICE = E.PLAN_PRICE,
			S.USERVRKV = @AUTHOR_NAME,
			S.DATEVRKV = GETDATE()
		FROM s_modeli S
		INNER JOIN #EXPANDED_DATA E ON S.ITEM_ID = E.MODEL_ID
		WHERE E.PLAN_PRICE > 0 
			AND E.PRICE_TYPE = '3'  -- ТОЛЬКО ДЛЯ price_type = 3
			AND (S.PLAN_PRICE IS NULL OR E.PLAN_PRICE > S.PLAN_PRICE)

		SET @PLAN_PRICE_UPDATED = @@ROWCOUNT

		-- 4. Обновление PRICE_LEVEL_ID В s_modeli
		
		-- Сохраняем информацию о том, какие PRICE_LEVEL_ID будут обновлены
		INSERT INTO #PRICE_LEVEL_UPDATES (
			MODEL_ID,
			OLD_PRICE_LEVEL_ID,
			NEW_PRICE_LEVEL_ID,
			MODEL,
			ARTICUL
		)
		SELECT 
			S.ITEM_ID,
			S.PRICE_LEVEL_ID AS OLD_PRICE_LEVEL_ID,
			E.PRICE_LEVEL_ID AS NEW_PRICE_LEVEL_ID,
			S.MODEL,
			S.ART
		FROM #EXPANDED_DATA E
		INNER JOIN s_modeli S ON S.ITEM_ID = E.MODEL_ID
		WHERE E.PRICE_TYPE = '1'  
			AND E.PRICE_LEVEL_ID > 0
			AND (S.PRICE_LEVEL_ID IS NULL OR S.PRICE_LEVEL_ID = 0)
		GROUP BY S.ITEM_ID, S.PRICE_LEVEL_ID, E.PRICE_LEVEL_ID, S.MODEL, S.ART

		-- Обновляем PRICE_LEVEL_ID в s_modeli
		UPDATE S
		SET 
			S.PRICE_LEVEL_ID = E.PRICE_LEVEL_ID,
			S.USERVRKV = @AUTHOR_NAME,
			S.DATEVRKV = GETDATE()
		FROM s_modeli S
		INNER JOIN #EXPANDED_DATA E ON S.ITEM_ID = E.MODEL_ID
		WHERE E.PRICE_TYPE = '1'  
			AND E.PRICE_LEVEL_ID > 0
			AND (S.PRICE_LEVEL_ID IS NULL OR S.PRICE_LEVEL_ID = 0)

		SET @PRICE_LEVEL_UPDATED = @@ROWCOUNT

		-- 5. Формирование JSON-ответа
		
		SET @JSON_OUT = '{
			"NO_VALID": 0,
			"MESS": "Все записи успешно добавлены",
			"TOTAL_RECORDS": ' + CAST(@SUCCESS_COUNT AS VARCHAR) + ',
			"TOTAL_DOCUMENTS": ' + CAST((SELECT COUNT(*) FROM #PLAN_RESULTS) AS VARCHAR) + ',
			"PLAN_PRICE_UPDATED": ' + CAST(@PLAN_PRICE_UPDATED AS VARCHAR) + ',
			"PRICE_LEVEL_UPDATED": ' + CAST(@PRICE_LEVEL_UPDATED AS VARCHAR) + ',
			"DETAILS": [
				' + (
					SELECT 
						'{"PLAN_ID": "' + PLAN_ID + 
						'", "NOMER": ' + CAST(NOMER AS VARCHAR) + 
						', "PRICE_ID": ' + CAST(PRICE_ID AS VARCHAR) + 
						', "RECORDS_COUNT": ' + CAST(RECORDS_COUNT AS VARCHAR) +
						'}'
					FROM #PLAN_RESULTS
					FOR XML PATH(''), TYPE
				).value('.', 'NVARCHAR(MAX)') + '
			],
			"PLAN_PRICE_DETAILS": [
				' + (
					SELECT 
						'{"MODEL_ID": ' + CAST(MODEL_ID AS VARCHAR) + 
						', "MODEL": "' + MODEL + 
						'", "ARTICUL": "' + ARTICUL + 
						'", "OLD_PLAN_PRICE": ' + ISNULL(CAST(OLD_PLAN_PRICE AS VARCHAR), 'null') + 
						', "NEW_PLAN_PRICE": ' + CAST(NEW_PLAN_PRICE AS VARCHAR) +
						'}'
					FROM #PLAN_PRICE_UPDATES
					FOR XML PATH(''), TYPE
				).value('.', 'NVARCHAR(MAX)') + '
			],
			"PRICE_LEVEL_DETAILS": [
				' + (
					SELECT 
						'{"MODEL_ID": ' + CAST(MODEL_ID AS VARCHAR) + 
						', "MODEL": "' + MODEL + 
						'", "ARTICUL": "' + ARTICUL + 
						'", "OLD_PRICE_LEVEL_ID": ' + ISNULL(CAST(OLD_PRICE_LEVEL_ID AS VARCHAR), 'null') + 
						', "NEW_PRICE_LEVEL_ID": ' + CAST(NEW_PRICE_LEVEL_ID AS VARCHAR) +
						'}'
					FROM #PRICE_LEVEL_UPDATES
					FOR XML PATH(''), TYPE
				).value('.', 'NVARCHAR(MAX)') + '
			]
		}'

		-- Очищаем временные таблицы
		DROP TABLE #JSON_DATA
		DROP TABLE #PARENT_DATA
		DROP TABLE #EXPANDED_DATA
		DROP TABLE #PLAN_RESULTS
		DROP TABLE #PLAN_PRICE_UPDATES
		DROP TABLE #PRICE_LEVEL_UPDATES

	END TRY
	BEGIN CATCH
		DECLARE @ERROR_NUMBER INT = ERROR_NUMBER()
		DECLARE @ERROR_MESSAGE NVARCHAR(4000) = ERROR_MESSAGE()
		DECLARE @ERROR_LINE INT = ERROR_LINE()

		-- Очищаем временные таблицы при ошибке
		IF OBJECT_ID('tempdb..#JSON_DATA') IS NOT NULL DROP TABLE #JSON_DATA
		IF OBJECT_ID('tempdb..#PARENT_DATA') IS NOT NULL DROP TABLE #PARENT_DATA
		IF OBJECT_ID('tempdb..#EXPANDED_DATA') IS NOT NULL DROP TABLE #EXPANDED_DATA
		IF OBJECT_ID('tempdb..#PLAN_RESULTS') IS NOT NULL DROP TABLE #PLAN_RESULTS
		IF OBJECT_ID('tempdb..#PLAN_PRICE_UPDATES') IS NOT NULL DROP TABLE #PLAN_PRICE_UPDATES
		IF OBJECT_ID('tempdb..#PRICE_LEVEL_UPDATES') IS NOT NULL DROP TABLE #PRICE_LEVEL_UPDATES

		SET @JSON_OUT = '{
			"NO_VALID": 1,
			"MESS": "Критическая ошибка №' + CAST(@ERROR_NUMBER AS VARCHAR) + 
			': ' + REPLACE(@ERROR_MESSAGE, '"', '\"') + 
			' (строка ' + CAST(@ERROR_LINE AS VARCHAR) + ')"
		}'
	END CATCH

	PRINT @JSON_OUT
END
