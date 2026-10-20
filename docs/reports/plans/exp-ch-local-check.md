# Локальный ClickHouse для проверки формы расходов

Форма расходов (этап 1.5) читает затраты из ClickHouse (`finance.exp_costs`), а не из процедуры при каждом открытии. Чтобы проверить чтение и подстановку бюджета на реальном снимке локально:

1. Поднять ClickHouse (тестовые пользователь и пароль выберите сами, ниже `<пароль>`):

   ```bash
   docker run -d --name finance-ch-test -p 18123:8123 \
     -e CLICKHOUSE_USER=finance -e CLICKHOUSE_PASSWORD=<пароль> \
     clickhouse/clickhouse-server:24.3-alpine
   ```

2. Применить схему (создаёт `finance.exp_costs` и журнал заливок):

   ```bash
   docker exec -i finance-ch-test clickhouse-client --user finance --password <пароль> --multiquery \
     < clickhouse/migrations/013_exp_costs.up.sql
   ```

3. Залить снимок из процедуры затрат (только чтение; около 4–5 минут, ~770 тыс. строк). Из каталога `go`, переменные окружения:
   `MODE=expcosts`, `EXP_COSTS_FORCE=1`, `EXP_COSTS_PROC=ZATRATY_DLYA_OKSANY_BYSTRYE`,
   `CLICKHOUSE_HTTP_URL=http://localhost:18123`, `CLICKHOUSE_USER=finance`, `CLICKHOUSE_PASSWORD=<пароль>`,
   `MSSQL_PREMASTER_SERVER`, `MSSQL_PREMASTER_USER`, `MSSQL_PREMASTER_PASSWORD`, `MSSQL_PREMASTER_DB=Budgeting`, `MSSQL_ENCRYPT=false`; затем

   ```bash
   go run ./cmd/findebt-etl
   ```

4. Запустить живой тест (без `PLANS_TEST_CH_URL` он пропускается):

   ```bash
   PLANS_TEST_CH_URL=http://localhost:18123 PLANS_TEST_CH_USER=finance PLANS_TEST_CH_PASSWORD=<пароль> \
     go test ./internal/plans -run TestZZLiveExpCH -v
   ```

Тест читает розницу РБ, интернет-магазин и опт, проверяет состав (у опта ЦФО 35, 254, 303, 414 и страны BY/RU/UZ) и прогоняет синхронизацию формы: сколько ячеек бюджета подставилось в текущий и следующие месяцы. Запасной источник в тесте — всегда ошибка, а источник чтения проверяется по следу (`ExpSourceKindCH`): при недоступном ClickHouse, пустой таблице или ошибке запроса тест падает с понятной причиной, а не читает фикстуры.

Результат на снимке от 20.10.2026 (карточка за октябрь 2026, до отсечения строк продаж из форм): розница РБ — 240 369 строк источника, 11 122 ячейки бюджета; интернет-магазин — 1 793 строки, 65 ячеек; опт — 3 049 строк, 110 ячеек.
