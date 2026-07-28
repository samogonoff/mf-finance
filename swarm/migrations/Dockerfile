ARG tag

FROM registry.markformelle.ru/services/docker-registry/finance-migrations-${tag}

COPY migrations /migrations/finance
COPY python/cost/migrations /migrations/cost
COPY clickhouse/migrations /migrations/clickhouse
COPY swarm/migrate.sh /usr/local/bin/migrate.sh
COPY swarm/migrate-clickhouse.sh /usr/local/bin/migrate-clickhouse.sh

# curl нужен для HTTP-аплая CH-миграций (см. migrate-clickhouse.sh).
# postgresql-client (psql) — для preflight в migrate.sh: снятие осиротевших
# advisory-локов и чтение schema_migrations перед накатом.
# Базовый образ — alpine (официальный migrate/migrate).
RUN apk add --no-cache curl postgresql-client \
  && chmod +x /usr/local/bin/migrate.sh /usr/local/bin/migrate-clickhouse.sh

ENTRYPOINT ["/bin/sh", "/usr/local/bin/migrate.sh"]
CMD ["up"]