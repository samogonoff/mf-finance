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

# ── Русский интерфейс ────────────────────────────────────────────────────────
#
# Официальный образ поставляет переводы ТОЛЬКО в исходниках: в
# superset/translations/<locale>/LC_MESSAGES/ лежит messages.po, а
# скомпилированных файлов нет ни одного (проверено на 6.1.0: во всём каталоге
# ни .mo, ни .json). Поэтому интерфейс остаётся английским, сколько бы ни
# выставляли BABEL_DEFAULT_LOCALE и LANGUAGES в superset_config.py.
#
# Слоёв перевода два, и нужны ОБА:
#   messages.mo   — бэкенд, Flask-Babel;
#   messages.json — фронтенд, см. superset/translations/utils.py:get_language_pack.
#                   Файла нет → функция молча логирует ошибку и отдаёт английский.
# Основная часть интерфейса на React, так что без .json перевода почти не видно.
# `pybabel compile` здесь не годится: он падает на 13 строках русского каталога
# с несовместимыми плейсхолдерами (дефекты апстримного перевода). Наш скрипт эти
# строки отбраковывает — иначе битый плейсхолдер уронил бы страницу в рантайме —
# и печатает их число, чтобы регресс в апстриме был заметен.
ARG SUPERSET_LOCALES="ru"

COPY superset/build_translations.py /app/build_translations.py

RUN /app/.venv/bin/python /app/build_translations.py \
      /app/superset/translations ${SUPERSET_LOCALES}

# Проверяем, что оба слоя на месте и пакет реально читается тем же кодом,
# которым его читает Superset. Иначе образ соберётся, а интерфейс останется
# английским — молча, как и было до этого.
RUN /app/.venv/bin/python -c "\
import sys; sys.path.insert(0, '/app'); \
from superset.translations.utils import get_language_pack; \
p = get_language_pack('ru')['locale_data']['superset']; \
assert p['Dashboards'][1] and p['Dashboards'][1] != 'Dashboards', p.get('Dashboards'); \
print('перевод ru:', len(p), 'строк, Dashboards =', p['Dashboards'][1])"

# Возвращаем непривилегированного пользователя базового образа.
USER superset
