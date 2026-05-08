#!/bin/sh
# Запускается postgres-cost при первом старте контейнера
# (volume cost_db_data ещё пустой). Применяет все *.up.sql из migrations/.
set -eu

for f in /docker-entrypoint-initdb.d/migrations/*.up.sql; do
    [ -e "$f" ] || continue
    echo "[cost-init-db] applying $f"
    psql -v ON_ERROR_STOP=1 --username "$POSTGRES_USER" --dbname "$POSTGRES_DB" -f "$f"
done
