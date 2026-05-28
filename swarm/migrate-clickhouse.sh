#!/bin/sh
# swarm/migrate-clickhouse.sh — накат ClickHouse-миграций через HTTP-интерфейс,
# по одному statement'у на запрос.
#
# Почему по одному statement'у, а не всем файлом:
#   - HTTP-интерфейс CH отвергает несколько statement'ов в одном теле
#     ("Multi-statements are not allowed", SYNTAX_ERROR);
#   - параметр ?multiquery=1 невалиден над HTTP (UNKNOWN_SETTING).
# Поэтому каждый *.up.sql режется на отдельные statement'ы по ';'.
#
# Идемпотентно: миграции написаны через CREATE ... IF NOT EXISTS, повторный
# прогон на каждом деплое безопасен. Версионирования (schema_migrations) здесь
# НЕТ — в отличие от Postgres, которым правит golang-migrate. down-миграции
# этим скриптом не применяются.
#
# Ограничения сплиттера (наши файлы им удовлетворяют): '--'-комментарии
# срезаются перед split'ом (в них бывает ';'), поэтому НЕ используйте ';'
# внутри строковых литералов и блочные комментарии /* ... */ в CH-миграциях.
#
# ENV:
#   CLICKHOUSE_HTTP_URL  — http(s)://host:port, без пути и query. Пусто →
#                          CH-слой не сконфигурирован, шаг пропускается (exit 0).
#                          Под --network host (CI) должен быть реальный
#                          host:port, доступный с раннера, а НЕ docker-DNS.
#   CLICKHOUSE_USER      — пользователь (default: finance)
#   CLICKHOUSE_PASSWORD  — пароль (default: finance)
#
# Аргумент: каталог с *.up.sql (default: /migrations/clickhouse).
set -eu

MIGRATIONS_DIR="${1:-/migrations/clickhouse}"

URL="${CLICKHOUSE_HTTP_URL:-}"
if [ -z "$URL" ]; then
  echo "==> [clickhouse] CLICKHOUSE_HTTP_URL не задан — пропускаю CH-миграции"
  exit 0
fi
URL="${URL%/}"
CH_USER="${CLICKHOUSE_USER:-finance}"
CH_PASS="${CLICKHOUSE_PASSWORD:-finance}"

if [ ! -d "$MIGRATIONS_DIR" ]; then
  echo "[clickhouse] каталог миграций не найден: $MIGRATIONS_DIR" >&2
  exit 1
fi

echo "==> [clickhouse] $MIGRATIONS_DIR -> $URL"

tmpd="$(mktemp -d)"
trap 'rm -rf "$tmpd"' EXIT

found=0
for f in "$MIGRATIONS_DIR"/*.up.sql; do
  [ -e "$f" ] || continue
  found=1
  echo "    -> $(basename "$f")"

  rm -f "$tmpd"/*.sql 2>/dev/null || true
  # 1) срезаем '--'-комментарии (в них встречается ';', ломающий split)
  # 2) режем на statement'ы по ';'
  # 3) каждый непустой statement пишем в отдельный файл NNNN.sql
  awk -v d="$tmpd" '
    { sub(/--.*/, ""); buf = buf $0 "\n" }
    END {
      n = 0
      m = split(buf, parts, ";")
      for (i = 1; i <= m; i++) {
        s = parts[i]
        gsub(/^[ \t\r\n]+/, "", s); gsub(/[ \t\r\n]+$/, "", s)
        if (s != "") {
          n++
          fn = sprintf("%s/%04d.sql", d, n)
          print s > fn
          close(fn)
        }
      }
    }
  ' "$f"

  for s in "$tmpd"/*.sql; do
    [ -e "$s" ] || continue
    if ! curl -sS -f \
          --user "$CH_USER:$CH_PASS" \
          --data-binary @"$s" \
          "$URL/"; then
      echo "[clickhouse] FAILED на statement $(basename "$s") из $(basename "$f")" >&2
      exit 1
    fi
  done
done

if [ "$found" -eq 0 ]; then
  echo "[clickhouse] нет *.up.sql в $MIGRATIONS_DIR" >&2
  exit 1
fi

echo "==> [clickhouse] done"
