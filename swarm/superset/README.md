# Superset — BI-контур Finance Cabinet

Третий изолированный контур репозитория, рядом с основным (`make up`) и
«Себестоимостью» (`make cost-up`). Своя метабаза, свой Redis, свои порты.

```bash
cd swarm
make superset-up              # поднять (первый раз ~5-10 мин: сборка образа)
make superset-assets-import   # накатить подключения и дашборды из git
```

- UI — http://localhost:8093 (`admin` / `SUPERSET_ADMIN_PASSWORD` из `.env`)
- MCP — http://localhost:5008/mcp
- Метабаза — `psql -h localhost -p 55434 -U superset superset`

Сервисы контура: `postgres-superset` (метабаза), `redis-superset` (кэш, брокер),
`superset-init` (one-shot миграции + админ), `superset` (UI),
`superset-worker` (Celery, async-запросы), `superset-mcp`.

**Про воркер.** У всех подключений `allow_run_async: true` — источники медленные
(ClickHouse-факты растут, MSSQL это живой корпоративный OLAP), синхронный запрос
рвётся о `SUPERSET_WEBSERVER_TIMEOUT`. Поэтому `superset-worker` и
`RESULTS_BACKEND` обязательны: без них SQL Lab отвечает 500 «Results backend is
not configured» на любой запрос. Логи — `make superset-logs-worker`.

**Правка `superset_config.py` требует рестарта:** файл смонтирован в контейнер,
но процесс читает его один раз на старте. `docker compose ... up -d` контейнер не
пересоздаёт (определение сервиса не изменилось) — нужен
`make superset-down && make superset-up` либо явный `restart`.

Контур живёт в отдельном compose-проекте `swarm-superset`, а не в общем `swarm`.
Иначе `make down-remove` в основном контуре снёс бы его контейнеры как orphan-ов.

## Как разрабатывать так, чтобы правки уехали в git и на прод

Это главный вопрос по Superset, и ответ неочевиден: **дашборды хранятся в
метабазе Postgres, а не в файлах.** Настроил в UI — изменения существуют
только на твоей машине. Ни `git status`, ни CI о них не узнают.

Поэтому метабаза здесь считается *производной* от git-дерева
`swarm/superset/assets/`, а не источником истины.

```
                  ┌─────────────────────────────────────────┐
                  │  swarm/superset/assets/  (git, YAML)    │
                  │  databases/ datasets/ charts/ dashboards│
                  └───────┬────────────────────────▲────────┘
     make superset-        │                        │   make superset-
     assets-import        │                        │   assets-export
                  ┌───────▼────────────┐   ┌───────┴────────────┐
                  │ dev: localhost:8093│   │ dev: localhost:8093│
                  └────────────────────┘   └────────────────────┘
                          │
                    git push → CI
                          │
                  ┌───────▼────────────┐
                  │ prod: тот же import│
                  └────────────────────┘
```

### Цикл разработки

1. Правишь дашборд/чарт/датасет руками в UI — как обычно.
2. Выгружаешь в git-дерево:
   ```bash
   cd swarm && make superset-assets-export
   ```
3. Смотришь, что получилось. YAML читаемый, diff осмысленный:
   ```bash
   git -C .. diff -- swarm/superset/assets/
   ```
4. Стейджишь **поимённо** (в рабочем дереве обычно висят чужие изменения,
   `git add -A` опасен) и коммитишь.
5. Прод забирает то же дерево тем же импортом — см. «Прод» ниже.

Обратное направление тоже работает: `make superset-assets-import` накатывает
git-состояние в твой инстанс. Полезно, чтобы проверить чужой MR или откатить
собственные эксперименты (`git checkout` + import).

Хочешь начать с чистого листа — `make superset-reset-db` сносит метабазу;
git-ассеты при этом целы, восстанавливаются импортом.

### Что синхронизируется, а что нет

| Каталог | Направление | Комментарий |
|---|---|---|
| `assets/databases/` | git → Superset | git авторитетнее. Экспорт их **не трогает** |
| `assets/datasets/` | ↔ | |
| `assets/charts/` | ↔ | |
| `assets/dashboards/` | ↔ | |
| `assets/queries/` | ↔ | сохранённые запросы SQL Lab |
| Пользователи, роли, права | не синхронизируется | заводятся на каждом инстансе отдельно |

Экспорт **удаляет** из git те YAML, которых больше нет в инстансе — иначе
удалённый в UI чарт вернулся бы на прод следующим импортом.

### Почему `databases/` только в одну сторону

Экспорт отдаёт `sqlalchemy_uri` с замаскированным паролем (`XXXXXXXXXX`) —
записав это в git, мы бы затёрли наши `${VAR}`-плейсхолдеры мусором. Поэтому
подключения описаны в git руками, а экспорт их пропускает.

Если подключение создали **в UI**, в git его нет. `make superset-assets-export`
предупредит об этом в stderr — тогда опиши его YAML'ом руками.

### UUID — единственная связь между dev и prod

Импорт сопоставляет объекты **по `uuid`**, а не по имени или id. В
`assets/databases/*.yaml` uuid зашиты вручную и меняться не должны: датасеты
ссылаются на подключение именно по нему. Сменишь uuid — на проде появится
второе подключение, а датасеты останутся висеть на старом и отвалятся.

### Секреты не в git

В YAML лежат `${SUPERSET_DB_*_URI}`, а не строки соединения. Подстановка идёт
из окружения в момент импорта, во временный zip в памяти — отрендеренный YAML
на диск не попадает. Список переменных — в `.env.example`, раздел «Superset».

Флаг `SUPERSET_ASSETS_STRICT`: при `1` незаполненный плейсхолдер **роняет**
импорт. В dev держим `0` (нет VPN до MSSQL — это норма), в CI/prod
обязательно `1`, иначе выкатим дашборд без источника.

### Почему API, а не `superset import-directory`

CLI-команда `import-directory` внутри вызывает `ImportExamplesCommand`, который
транспилирует SQL виртуальных датасетов под целевой диалект — то есть тихо
переписывает наши ClickHouse-запросы. Скрипт `assets_sync.py` ходит в
`/api/v1/assets/{export,import}/` (`ImportAssetsCommand`): без транспиляции,
сопоставление строго по uuid.

## Русский интерфейс

Отдельный языковой пакет ставить не нужно — переводы лежат в самом Superset. Но
**официальный образ поставляет их только в исходниках**: в
`superset/translations/ru/LC_MESSAGES/` есть `messages.po`, а скомпилированных
файлов нет ни одного. Поэтому «из коробки» интерфейс английский, сколько бы ни
выставляли `BABEL_DEFAULT_LOCALE` и `LANGUAGES`.

Слоёв перевода два, и работают они независимо:

| Слой | Файл | Кто читает |
|---|---|---|
| Бэкенд | `messages.mo` | Flask-Babel |
| Фронтенд | `messages.json` (Jed 1.x) | React, через `translations/utils.py::get_language_pack` |

Основная часть интерфейса — React, так что без `.json` перевода почти не видно;
при отсутствии файла `get_language_pack` молча логирует ошибку и отдаёт
английский пакет.

Оба файла собираются при сборке образа скриптом `build_translations.py` из
`.po`. Локали задаются build-arg'ом `SUPERSET_LOCALES` (по умолчанию `ru`):

```bash
docker compose -f docker-compose.superset.yml build \
  --build-arg SUPERSET_LOCALES="ru uk"
```

**Почему не `pybabel compile`.** Он падает на 13 строках русского каталога с
несовместимыми плейсхолдерами — дефекты апстримного перевода (например, в
переводе появился `%(error)s`, которого нет в оригинале). Игнорировать нельзя:
битый плейсхолдер даёт исключение при подстановке, то есть падающую страницу
вместо текста. Скрипт эти строки отбраковывает и печатает их число — если после
обновления Superset оно вырастет, это будет видно в логе сборки.

Итог на 6.1.0: **переведено 4503 строки из 4584**, отбраковано 13.
Непереведённое остаётся английским — в основном свежие строки и технические
термины (`SQL Lab` так и остаётся `SQL Lab`).

Переключатель языка есть в меню пользователя; список — в `LANGUAGES` в
`superset_config.py`, дефолт задаётся `BABEL_DEFAULT_LOCALE`.

Названия наших дашбордов, чартов и колонок к этому отношения не имеют — они
наши и уже на русском.

## Источники данных

Через `host.docker.internal`, а не по докер-сети — так контур поднимается
независимо от `make up` / `make cost-up`, и та же схема конфигурации работает
в prod, где БД на других хостах.

| Подключение | Драйвер | dev-адрес |
|---|---|---|
| MF ClickHouse (finance) | `clickhouse-connect` | `:58123` |
| MF Postgres (finance) | `psycopg2` | `:55432` |
| MF Postgres (cost) | `psycopg2` | `:55433` |
| MF MSSQL (OLAP/Premaster) | `pymssql` | 10.10.6.15 / .107, **нужен VPN** |

Данные видны только у тех контуров, что запущены. Superset поднимется и без
них — подключения просто не ответят.

Везде `allow_dml: false`, `allow_ctas/cvas: false`: Superset читатель, не писатель.

## MCP-сервер

MCP встроен в Apache Superset начиная с 5.0 (модуль `superset.mcp_service`,
требует `fastmcp` — стоит в `Dockerfile`). Сторонний сервер не нужен.
Отдельный контейнер `superset-mcp` запускает `superset mcp run`, эндпоинт
`http://localhost:5008/mcp` прописан в `../../.mcp.json`.

Инструменты (27): `list_dashboards`, `get_chart_data`, `execute_sql`,
`generate_chart`, `generate_dashboard`, `create_virtual_dataset`,
`query_dataset`, `get_schema` и т.д. Под них написаны скиллы
`preset-io/agent-skills`:

```bash
claude plugin marketplace add preset-io/agent-skills
claude plugin install preset-mcp-skills@preset-agent-skills
claude plugin install preset-api-skills@preset-agent-skills
```

`preset-cli-skills` не нужен — он про CLI `sup` и Preset Cloud.
Проектная специфика (наши источники, правила) — `.claude/skills/superset-mf/`.

**Безопасность.** В dev `MCP_AUTH_ENABLED=0`, все вызовы идут от имени
`MCP_DEV_USERNAME=admin`. Допустимо только на localhost: порт 5008 без
авторизации = права админа Superset над всеми подключёнными БД для любого, кто
до него дотянулся. В prod обязательно `MCP_AUTH_ENABLED=1` плюс `MCP_JWT_SECRET`
(HS256) либо `MCP_JWKS_URI` (RS256). `MCP_RBAC_ENABLED` не выключать никогда.

Изменения в дашбордах, сделанные агентом через MCP, живут в метабазе — их
нужно так же выгрузить `make superset-assets-export`, иначе они не уедут.

## Прод

**Прод сознательно отложен** (решение от 11–12.08.2026). Сначала отрабатываем
аналитику на dev: собираем витрины и дашборды, набиваем руку, определяем
метрики. К проду возвращаемся, когда будет что выкатывать, а не заглушка.

Из этого НЕ следует, что можно не коммитить: экспортировать в git надо с
первого дня, иначе `make superset-reset-db` сотрёт наработанное. Тренировочные
дашборды — такой же артефакт, как код.

Ниже — схема на будущее. В `ci-finance.yml` она НЕ добавлена.

Что нужно:

1. Образ `superset` в job `publish` (собирается из `swarm/superset/Dockerfile`).
2. Сервис `superset` + `superset-mcp` в `docker-compose.yml` (prod-stack Swarm),
   с `%%CONFIG_NAME%%`-конфигом по образцу остальных сервисов.
3. Отдельный job **после** `deploy` — импорт ассетов:
   ```yaml
   superset_assets:
     stage: deploy
     needs: [deploy]
     script:
       - docker exec $(docker ps -q -f name=superset) \
           python /app/assets_sync.py import
     variables:
       SUPERSET_ASSETS_STRICT: "1"
   ```
4. Прод-переменные в `docker config`: `SUPERSET_SECRET_KEY`,
   `SUPERSET_ADMIN_PASSWORD`, все `SUPERSET_DB_*_URI`, `MCP_AUTH_ENABLED=1`,
   `MCP_JWT_SECRET`. Метабаза — отдельный Postgres, **не** `finance`.

Открытые вопросы к обсуждению: нужен ли Superset на проде вообще или это
внутренний инструмент аналитика; если нужен — под каким доменом и как
связывается аутентификация с B24 OAuth остального кабинета.
