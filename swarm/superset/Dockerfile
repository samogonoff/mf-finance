# Apache Superset для Finance Cabinet.
#
# Базовый образ apache/superset НЕ содержит драйверов к нашим источникам —
# доставляем их здесь. Список синхронизирован с bootstrap_databases.py:
# что регистрируем как подключение, то и должно быть в этом файле.
#
#   clickhouse-connect  → ClickHouse finance (fact_glmf, fact_findebt, dim_contract)
#   psycopg2-binary     → Postgres finance и Postgres cost
#   pymssql             → MSSQL Premaster1C / OLAP (10.10.6.107, 10.10.6.15)
#   fastmcp             → MCP-сервис (`superset mcp run`), см. superset_config.py
FROM apache/superset:6.1.0

USER root

# ВАЖНО: ставим через docker/pip-install.sh (внутри `uv pip install`), а НЕ
# через `pip install`. Superset живёт в venv /app/.venv, созданном uv — своего
# pip там нет, поэтому `pip` под root резолвится в /usr/local/bin/pip и кладёт
# пакеты в СИСТЕМНЫЙ Python, куда Superset не смотрит. Симптом: образ собрался,
# а `superset db upgrade` падает с ModuleNotFoundError: No module named 'psycopg2'.
#
# VIRTUAL_ENV задаём явно, чтобы uv не зависел от того, из какого каталога
# запущен слой.
ENV VIRTUAL_ENV=/app/.venv

RUN /app/docker/pip-install.sh --no-cache \
      "clickhouse-connect>=0.8,<0.9" \
      "psycopg2-binary>=2.9" \
      "pymssql>=2.3" \
      "fastmcp>=2.0"

# Проверяем сразу на этапе сборки: тихо собравшийся образ без драйвера —
# худший из возможных исходов, ошибка всплывёт только в рантайме.
RUN /app/.venv/bin/python -c "\
import psycopg2, clickhouse_connect, pymssql, fastmcp; \
print('драйверы на месте: psycopg2', psycopg2.__version__.split()[0], \
      '| pymssql', pymssql.__version__, '| fastmcp', fastmcp.__version__)"

# Возвращаем непривилегированного пользователя базового образа.
USER superset
