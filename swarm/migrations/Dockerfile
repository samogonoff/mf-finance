ARG tag

FROM registry.markformelle.ru/services/docker-registry/finance-migrations-${tag}

COPY migrations /migrations/finance
COPY python/cost/migrations /migrations/cost
COPY swarm/migrate.sh /usr/local/bin/migrate.sh

RUN chmod +x /usr/local/bin/migrate.sh

ENTRYPOINT ["/bin/sh", "/usr/local/bin/migrate.sh"]
CMD ["up"]