# Локальный ClickHouse для проверки формы расходов

Форма расходов (этап 1.5) читает затраты из ClickHouse (`finance.exp_costs`), а не из процедуры при каждом открытии. Чтобы проверить чтение и подстановку бюджета на реальном снимке локально:

1. Поднять ClickHouse (образ `clickhouse/clickhouse-server:24.3-alpine`, в проекте — контейнер `finance-ch-test`, HTTP-порт 18123; тестовые пользователь и пароль заданы в окружении контейнера).
2. Применить схему: `clickhouse/migrations/013_exp_costs.up.sql` (создаёт `finance.exp_costs` и журнал заливок).
3. Залить снимок из процедуры затрат (только чтение; около 4–5 минут, ~770 тыс. строк):
   `MODE=expcosts EXP_COSTS_FORCE=1 EXP_COSTS_PROC=ZATRATY_DLYA_OKSANY_BYSTRYE CLICKHOUSE_HTTP_URL=http://localhost:18123 CLICKHOUSE_USER=… CLICKHOUSE_PASSWORD=… MSSQL_PREMASTER_*=… MSSQL_PREMASTER_DB=Budgeting go run ./cmd/findebt-etl` (из каталога `go`).
4. Запустить живой тест (без `PLANS_TEST_CH_URL` он пропускается):
   `PLANS_TEST_CH_URL=http://localhost:18123 PLANS_TEST_CH_USER=… PLANS_TEST_CH_PASSWORD=… go test ./internal/plans -run TestZZLiveExpCH -v`

Тест читает розницу РБ, интернет-магазин и опт, проверяет состав (у опта ЦФО 35, 254, 303, 414 и страны BY/RU/UZ) и прогоняет синхронизацию формы: сколько ячеек бюджета подставилось в текущий и следующие месяцы.

Результат на снимке от 20.10.2026 (карточка за октябрь 2026): розница РБ — 240 369 строк источника, 11 122 ячейки бюджета; интернет-магазин — 1 793 строки, 65 ячеек; опт — 3 049 строк, 110 ячеек.