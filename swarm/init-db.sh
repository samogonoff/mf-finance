#!/bin/sh
# Инициализация ролей и применение миграций. Запускается postgres'ом
# при первом старте контейнера (docker-entrypoint-initdb.d).
#
# Что делаем:
#   1. analytics_ro — read-only роль для Python-контейнера
#   2. права: SELECT на public, ALL на схему analytics
#   3. применяем все *.up.sql из /docker-entrypoint-initdb.d/migrations
set -eu

psql -v ON_ERROR_STOP=1 --username "$POSTGRES_USER" --dbname "$POSTGRES_DB" <<-'EOSQL'
    DO $$
    BEGIN
        IF NOT EXISTS (SELECT FROM pg_catalog.pg_roles WHERE rolname = 'analytics_ro') THEN
            CREATE ROLE analytics_ro LOGIN PASSWORD 'analytics_ro';
        END IF;
    END
    $$;
EOSQL

for f in /docker-entrypoint-initdb.d/migrations/*.up.sql; do
    [ -e "$f" ] || continue
    echo "[init-db] applying $f"
    psql -v ON_ERROR_STOP=1 --username "$POSTGRES_USER" --dbname "$POSTGRES_DB" -f "$f"
done

psql -v ON_ERROR_STOP=1 --username "$POSTGRES_USER" --dbname "$POSTGRES_DB" <<-'EOSQL'
    GRANT USAGE ON SCHEMA public TO analytics_ro;
    GRANT SELECT ON ALL TABLES IN SCHEMA public TO analytics_ro;
    ALTER DEFAULT PRIVILEGES IN SCHEMA public GRANT SELECT ON TABLES TO analytics_ro;

    GRANT USAGE, CREATE ON SCHEMA analytics TO analytics_ro;
    GRANT ALL ON ALL TABLES IN SCHEMA analytics TO analytics_ro;
    ALTER DEFAULT PRIVILEGES IN SCHEMA analytics GRANT ALL ON TABLES TO analytics_ro;
EOSQL
