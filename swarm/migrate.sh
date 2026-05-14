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

echo "==> done"