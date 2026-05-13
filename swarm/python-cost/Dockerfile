ARG tag

FROM registry.markformelle.ru/services/docker-registry/finance-python-${tag}

WORKDIR /var/www/cost

COPY python/cost/requirements.txt .

RUN pip install --upgrade pip && \
    pip install --no-cache-dir -r requirements.txt

COPY python/cost/. .
COPY swarm/entrypoint.sh /entrypoint.sh

RUN chmod +x /entrypoint.sh

ENTRYPOINT ["/entrypoint.sh"]
