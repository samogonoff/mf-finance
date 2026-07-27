#!/bin/sh
set -eu

if [ "$#" -eq 0 ]; then
  set -- up
fi

: "${POSTGRES_URL:?POSTGRES_URL is required}"
: "${COST_DATABASE_URL:?COST_DATABASE_URL is required}"

# Single-database force commands (to fix dirty state without affecting other DBs).
case "${1:-}" in
  force-cost)
    /usr/local/bin/migrate \
      -path=/migrations/cost \
      -database "$COST_DATABASE_URL" \
      force "${2:?usage: $0 force-cost <version>}"
    exit $?
    ;;
  force-finance)
    /usr/local/bin/migrate \
      -path=/migrations/finance \
      -database "$POSTGRES_URL" \
      force "${2:?usage: $0 force-finance <version>}"
    exit $?
    ;;
esac

# Append lock_timeout & statement_timeout to DSN so migrations fail fast instead
# of waiting indefinitely for ACCESS EXCLUSIVE locks held by running app sessions.
append_timeout_params() {
  _url="$1"
  _sep='?'
  case "$_url" in
    *"?"*) _sep='&' ;;   # already has query params
  esac
  printf '%s%slock_timeout=10s&statement_timeout=30s' "$_url" "$_sep"
}

MIGRATE_POSTGRES_URL="$(append_timeout_params "$POSTGRES_URL")"
MIGRATE_COST_URL="$(append_timeout_params "$COST_DATABASE_URL")"

echo "==> migrate finance"
/usr/local/bin/migrate \
  -path=/migrations/finance \
  -database "$MIGRATE_POSTGRES_URL" \
  "$@"

echo "==> migrate cost"
/usr/local/bin/migrate \
  -path=/migrations/cost \
  -database "$MIGRATE_COST_URL" \
  "$@"

# ClickHouse — отдельный канал (HTTP-аплай, без golang-migrate/schema_migrations).
# Применяем только на 'up'; down/version/force этим путём не поддерживаются.
# Скрипт сам пропускается, если CLICKHOUSE_HTTP_URL пуст (CH-слой не настроен).
if [ "$1" = "up" ]; then
  echo "==> migrate clickhouse"
  /bin/sh /usr/local/bin/migrate-clickhouse.sh /migrations/clickhouse
fi

echo "==> done"