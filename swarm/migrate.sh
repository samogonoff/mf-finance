#!/usr/bin/env bash
# swarm/migrate.sh — накатывает миграции finance + cost одноразовым контейнером
# golang-migrate. Идемпотентно: безопасно запускать на каждом деплое.
#
# Использование:
#   ./swarm/migrate.sh                # = up (накатить все pending)
#   ./swarm/migrate.sh up
#   ./swarm/migrate.sh down 1         # откатить одну версию в каждой БД
#   ./swarm/migrate.sh version        # показать текущую версию
#   ./swarm/migrate.sh force 1        # форснуть версию (если БД "dirty")
#
# DSN берутся из ENV: POSTGRES_URL (finance) и COST_DATABASE_URL (cost).
# Если не заданы — пробуем подхватить из ../.env рядом с swarm/.
#
# Дополнительно (опционально):
#   MIGRATE_NETWORK   — docker-сеть для запуска (default: host).
#                       Для overlay-сети swarm: пометь её --attachable и передай
#                       сюда имя, например finance-prod_network.
#   MIGRATE_IMAGE     — кастомный образ (default: migrate/migrate:v4.17.0).
#
# Учёт применённых миграций golang-migrate ведёт сам — таблица
# `schema_migrations` создаётся в каждой БД автоматически и хранит номер
# последней применённой версии + флаг dirty (см. README.md, раздел Миграции).
#
# Exit codes:
#   0 — все миграции применены (или нечего применять)
#   1 — ошибка (несоответствие DSN, docker недоступен, миграция упала)

set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "$0")" && pwd)"
ROOT_DIR="$(cd "$SCRIPT_DIR/.." && pwd)"

# Подхватить DSN из .env (на уровень выше swarm/), если ENV пустой.
if [[ -z "${POSTGRES_URL:-}" || -z "${COST_DATABASE_URL:-}" ]]; then
  if [[ -f "$ROOT_DIR/.env" ]]; then
    set -a
    # shellcheck disable=SC1091
    . "$ROOT_DIR/.env"
    set +a
  fi
fi

: "${POSTGRES_URL:?POSTGRES_URL не задан (или нет в ../.env)}"
: "${COST_DATABASE_URL:?COST_DATABASE_URL не задан (или нет в ../.env)}"

MIGRATE_IMAGE="${MIGRATE_IMAGE:-migrate/migrate:v4.17.0}"
MIGRATE_NETWORK="${MIGRATE_NETWORK:-host}"

# Если по умолчанию передали ничего — это up.
if [[ $# -eq 0 ]]; then
  set -- up
fi

run_migrate() {
  local label="$1"
  local migrations_dir="$2"
  local dsn="$3"
  shift 3

  if [[ ! -d "$migrations_dir" ]]; then
    echo "[$label] каталог миграций не найден: $migrations_dir" >&2
    return 1
  fi

  echo "==> [$label] $migrations_dir"
  docker run --rm \
    --network "$MIGRATE_NETWORK" \
    -v "$migrations_dir":/migrations:ro \
    "$MIGRATE_IMAGE" \
    -path=/migrations \
    -database "$dsn" \
    "$@"
}

run_migrate "finance" "$ROOT_DIR/migrations"             "$POSTGRES_URL"      "$@"
run_migrate "cost"    "$ROOT_DIR/python/cost/migrations" "$COST_DATABASE_URL" "$@"

echo "==> done"
