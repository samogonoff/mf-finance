#!/bin/sh
set -eu

if [ "$#" -eq 0 ]; then
  set -- up
fi

: "${POSTGRES_URL:?POSTGRES_URL is required}"
: "${COST_DATABASE_URL:?COST_DATABASE_URL is required}"

echo "==> migrate finance"
/usr/local/bin/migrate \
  -path=/migrations/finance \
  -database "$POSTGRES_URL" \
  "$@"

echo "==> migrate cost"
/usr/local/bin/migrate \
  -path=/migrations/cost \
  -database "$COST_DATABASE_URL" \
  "$@"

# ClickHouse — отдельный канал (HTTP-аплай, без golang-migrate/schema_migrations).
# Применяем только на 'up'; down/version/force этим путём не поддерживаются.
# Скрипт сам пропускается, если CLICKHOUSE_HTTP_URL пуст (CH-слой не настроен).
if [ "$1" = "up" ]; then
  echo "==> migrate clickhouse"
  /bin/sh /usr/local/bin/migrate-clickhouse.sh /migrations/clickhouse
fi

echo "==> done"