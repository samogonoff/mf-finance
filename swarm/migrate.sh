#!/bin/sh
# swarm/migrate.sh — накат миграций обеих Postgres-БД (finance + cost) и ClickHouse.
# Запускается CI-джобом `migrations` (см. swarm/ci-finance.yml) из образа
# migrations-*, ENTRYPOINT которого указывает сюда.
#
# Почему тут есть preflight (см. функции ниже) — история проблемы:
#   1. golang-migrate на время наката держит СЕССИОННЫЙ advisory-lock
#      (`SELECT pg_advisory_lock($1)`), чтобы два мигратора не гонялись.
#   2. Раньше DSN шёл без таймаутов: миграция с `ALTER TABLE ... ADD CONSTRAINT`
#      вставала в очередь за ACCESS EXCLUSIVE и висела часами, потому что
#      python-cost держал открытые сессии на той же таблице. Job убивали по
#      таймауту — но `docker run` убивает CLI-клиента, а САМ КОНТЕЙНЕР остаётся
#      жив, продолжает ждать блокировку и держит advisory-lock.
#   3. После добавления lock_timeout (54e840d) вечное зависание превратилось в
#      быстрое падение — но уже на самом захвате advisory-lock, который всё ещё
#      удерживает осиротевшая сессия из п.2:
#         error: try lock failed in line 0: SELECT pg_advisory_lock($1)
#         (details: pq: canceling statement due to lock timeout)
#      lock_timeout действует и на advisory-локи, поэтому симптом именно такой.
#
# Отсюда preflight перед `up`:
#   - unlock: показать держателей advisory-локов и прибить осиротевшие
#     (в БД finance/cost advisory-локи берёт ТОЛЬКО golang-migrate — приложения
#     ими не пользуются, так что любой держатель старше порога = зомби);
#   - heal:   снять флаг dirty (force на version-1), чтобы `up` переприменил
#     упавшую миграцию. Заменяет прибитый гвоздями `force-cost 13` в CI.
#
# Команды:
#   up | down N | version | ...   — прокидываются в golang-migrate для ОБЕИХ БД
#   force-cost <v> / force-finance <v>  — ручной force одной БД
#   unlock-cost / unlock-finance / unlock  — ручное снятие осиротевших локов
#   doctor                        — диагностика без изменений (версия, dirty, локи)
set -eu

if [ "$#" -eq 0 ]; then
  set -- up
fi

: "${POSTGRES_URL:?POSTGRES_URL is required}"
: "${COST_DATABASE_URL:?COST_DATABASE_URL is required}"

# --- Настройки (все с дефолтами, описаны в .env.example) ----------------------
# lock_timeout — сколько ждать блокировку (таблицы ИЛИ advisory), прежде чем
# упасть. Защита от вечного зависания на ALTER TABLE.
LOCK_TIMEOUT="${MIGRATE_LOCK_TIMEOUT:-10s}"
# statement_timeout — потолок на один statement. Не путать с lock_timeout:
# это про длительность самого DDL (CREATE INDEX на большой таблице и т.п.).
STATEMENT_TIMEOUT="${MIGRATE_STATEMENT_TIMEOUT:-300s}"
# Возраст (сек) держателя advisory-лока, после которого он считается зомби.
UNLOCK_IDLE_SECONDS="${MIGRATE_UNLOCK_IDLE_SECONDS:-60}"
# 1 — снимать dirty автоматически (force version-1 + переприменить), 0 — нет.
AUTO_HEAL="${MIGRATE_AUTO_HEAL:-1}"
# 1 — прибивать зомби-держателей advisory-локов, 0 — только показывать.
AUTO_UNLOCK="${MIGRATE_AUTO_UNLOCK:-1}"
# Путь к бинарю golang-migrate (переопределяется только в тестах/локально).
MIGRATE_BIN="${MIGRATE_BIN:-/usr/local/bin/migrate}"

# --- DSN ----------------------------------------------------------------------
# Таймауты и application_name дописываются только в DSN для golang-migrate
# (lib/pq умеет произвольные GUC в query-string). psql получает СЫРОЙ URL:
# libpq такие параметры не понимает и отвалится с "invalid URI query parameter".
append_migrate_params() {
  _url="$1"
  _app="$2"
  _sep='?'
  case "$_url" in
    *"?"*) _sep='&' ;;   # already has query params
  esac
  printf '%s%slock_timeout=%s&statement_timeout=%s&application_name=%s' \
    "$_url" "$_sep" "$LOCK_TIMEOUT" "$STATEMENT_TIMEOUT" "$_app"
}

MIGRATE_POSTGRES_URL="$(append_migrate_params "$POSTGRES_URL" "finance-migrate")"
MIGRATE_COST_URL="$(append_migrate_params "$COST_DATABASE_URL" "cost-migrate")"

# --- psql-хелперы -------------------------------------------------------------
have_psql() { command -v psql >/dev/null 2>&1; }

# psql_q <raw-dsn> <sql> — одна колонка на строку, без заголовков.
psql_q() {
  PGCONNECT_TIMEOUT="${PGCONNECT_TIMEOUT:-10}" \
    psql "$1" -X -q -A -t -F '|' -v ON_ERROR_STOP=1 -c "$2"
}

SQL_LOCK_HOLDERS="
SELECT format('pid=%s state=%s age=%s app=%s wait=%s query=%s',
              a.pid,
              a.state,
              date_trunc('second', now() - a.state_change),
              coalesce(nullif(a.application_name, ''), '-'),
              coalesce(a.wait_event_type || ':' || a.wait_event, '-'),
              left(regexp_replace(coalesce(a.query, ''), '[[:space:]]+', ' ', 'g'), 80))
FROM pg_stat_activity a
WHERE a.datname = current_database()
  AND a.pid <> pg_backend_pid()
  AND EXISTS (SELECT 1 FROM pg_locks l
              WHERE l.pid = a.pid AND l.locktype = 'advisory' AND l.granted)
ORDER BY a.state_change"

SQL_VERSION="
SELECT CASE
         WHEN to_regclass('schema_migrations') IS NULL THEN ''
         ELSE (SELECT version || ' ' || dirty FROM schema_migrations LIMIT 1)
       END"

# show_locks <raw-dsn> <label> — печатает держателей advisory-локов (0 = их нет).
show_locks() {
  _dsn="$1"; _label="$2"
  if ! _holders="$(psql_q "$_dsn" "$SQL_LOCK_HOLDERS" 2>&1)"; then
    echo "==> [$_label] preflight: psql недоступен/не подключился, пропускаю"
    echo "    $_holders"
    return 1
  fi
  if [ -z "$_holders" ]; then
    echo "==> [$_label] advisory-локов нет"
    return 2   # нечего снимать — unlock_db на этом останавливается
  fi
  echo "==> [$_label] держатели advisory-локов:"
  echo "$_holders" | sed 's/^/    /'
  return 0
}

# unlock_db <raw-dsn> <label> — прибить держателей advisory-локов старше порога.
unlock_db() {
  _dsn="$1"; _label="$2"
  show_locks "$_dsn" "$_label" || return 0

  if [ "$AUTO_UNLOCK" != "1" ]; then
    echo "    MIGRATE_AUTO_UNLOCK=0 — не трогаю (снять вручную: unlock-$_label)"
    return 0
  fi

  _killed="$(psql_q "$_dsn" "
WITH victims AS (
  SELECT a.pid, a.state, date_trunc('second', now() - a.state_change) AS age
  FROM pg_stat_activity a
  WHERE a.datname = current_database()
    AND a.pid <> pg_backend_pid()
    AND a.state_change < now() - make_interval(secs => ${UNLOCK_IDLE_SECONDS})
    AND EXISTS (SELECT 1 FROM pg_locks l
                WHERE l.pid = a.pid AND l.locktype = 'advisory' AND l.granted)
)
SELECT format('pid=%s state=%s age=%s terminated=%s',
              v.pid, v.state, v.age, pg_terminate_backend(v.pid))
FROM victims v" 2>&1)" || {
    echo "    ⚠ не удалось снять локи: $_killed"
    return 0
  }

  if [ -n "$_killed" ]; then
    echo "    ⚠ прибиты осиротевшие сессии (старше ${UNLOCK_IDLE_SECONDS}s):"
    echo "$_killed" | sed 's/^/      /'
  else
    echo "    держатели моложе ${UNLOCK_IDLE_SECONDS}s — не трогаю (возможен параллельный мигратор)"
  fi
}

# heal_dirty <raw-dsn> <migrate-dsn> <path> <label>
# Если schema_migrations.dirty — форсим version-1, чтобы `up` переприменил
# упавшую миграцию (миграции идемпотентны: IF NOT EXISTS / DROP IF EXISTS).
heal_dirty() {
  _dsn="$1"; _mdsn="$2"; _path="$3"; _label="$4"

  if ! _row="$(psql_q "$_dsn" "$SQL_VERSION" 2>/dev/null)"; then
    return 0
  fi
  [ -n "$_row" ] || return 0

  _version="${_row% *}"
  _dirty="${_row#* }"
  echo "==> [$_label] schema_migrations: version=$_version dirty=$_dirty"

  # psql печатает boolean как 't'/'f' в бинарном формате и как 'true'/'false'
  # при конкатенации в text (наш случай) — принимаем оба.
  case "$_dirty" in
    t|true) ;;
    *) return 0 ;;
  esac

  if [ "$AUTO_HEAL" != "1" ]; then
    echo "    ⚠ БД dirty, но MIGRATE_AUTO_HEAL=0 — чиню вручную: force-$_label <version>"
    return 0
  fi

  case "$_version" in
    ''|*[!0-9]*) echo "    ⚠ непонятная version='$_version', heal пропущен"; return 0 ;;
  esac

  _target=$((_version - 1))
  if [ "$_target" -lt 0 ]; then
    _target=0
  fi
  echo "    ⚠ dirty → force $_target, миграция $_version будет переприменена"
  "$MIGRATE_BIN" -path="$_path" -database "$_mdsn" force "$_target"
}

preflight() {
  _dsn="$1"; _mdsn="$2"; _path="$3"; _label="$4"
  if ! have_psql; then
    echo "==> [$_label] preflight пропущен: в образе нет psql"
    return 0
  fi
  unlock_db "$_dsn" "$_label"
  heal_dirty "$_dsn" "$_mdsn" "$_path" "$_label"
}

# --- Одиночные команды --------------------------------------------------------
case "${1:-}" in
  force-cost)
    "$MIGRATE_BIN" \
      -path=/migrations/cost \
      -database "$MIGRATE_COST_URL" \
      force "${2:?usage: $0 force-cost <version>}"
    exit $?
    ;;
  force-finance)
    "$MIGRATE_BIN" \
      -path=/migrations/finance \
      -database "$MIGRATE_POSTGRES_URL" \
      force "${2:?usage: $0 force-finance <version>}"
    exit $?
    ;;
  unlock-cost)
    have_psql || { echo "psql не найден в образе"; exit 1; }
    unlock_db "$COST_DATABASE_URL" cost
    exit 0
    ;;
  unlock-finance)
    have_psql || { echo "psql не найден в образе"; exit 1; }
    unlock_db "$POSTGRES_URL" finance
    exit 0
    ;;
  unlock)
    have_psql || { echo "psql не найден в образе"; exit 1; }
    unlock_db "$POSTGRES_URL" finance
    unlock_db "$COST_DATABASE_URL" cost
    exit 0
    ;;
  doctor)
    have_psql || { echo "psql не найден в образе"; exit 1; }
    AUTO_UNLOCK=0
    for _pair in "finance|$POSTGRES_URL" "cost|$COST_DATABASE_URL"; do
      _label="${_pair%%|*}"; _dsn="${_pair#*|}"
      psql_q "$_dsn" "$SQL_VERSION" >/dev/null 2>&1 \
        && echo "==> [$_label] schema_migrations: $(psql_q "$_dsn" "$SQL_VERSION")" \
        || echo "==> [$_label] нет доступа к БД"
      show_locks "$_dsn" "$_label" || true
    done
    exit 0
    ;;
esac

# --- Основной путь ------------------------------------------------------------
if [ "$1" = "up" ]; then
  preflight "$POSTGRES_URL" "$MIGRATE_POSTGRES_URL" /migrations/finance finance
  preflight "$COST_DATABASE_URL" "$MIGRATE_COST_URL" /migrations/cost cost
fi

echo "==> migrate finance"
"$MIGRATE_BIN" \
  -path=/migrations/finance \
  -database "$MIGRATE_POSTGRES_URL" \
  "$@"

echo "==> migrate cost"
"$MIGRATE_BIN" \
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
