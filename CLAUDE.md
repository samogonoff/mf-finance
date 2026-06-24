# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

## Что это

Finance Cabinet — изолированный кабинет финансиста, сибиринг MP-кабинета маркетплейсов. Своя БД, своё B24 OAuth-приложение, свой домен. Часть кода (auth, контракт `/api/auth/b24/callback`) портирована 1:1 из MP — см. упоминания `FINANCE_PORTING_GUIDE.md` в коде и `README.md`.

Внутри живут **два независимых контура** на одном репо:

1. **Основной контур** (`swarm/docker-compose.dev.yml`, `make up`) — Go-API + Nuxt + Postgres `finance` + Redis + python-analytics sandbox.
2. **Контур «Себестоимость»** (`swarm/docker-compose.cost.yml`, `make cost-up`) — отдельный FastAPI (`python-cost`) + отдельный Postgres `cost` + nginx-cost. Раздел встраивается в основной Nuxt через `extends: ["../python/cost/nuxt-layer"]`.

Контуры можно гонять параллельно или по-отдельности. У них РАЗНЫЕ БД и РАЗНЫЕ миграции.

## Команды

Все команды запускаются из `swarm/`:

```bash
make up               # поднять основной dev-контур (Go + Nuxt + PG + Redis + analytics)
make down             # остановить
make logs             # tail логов всех сервисов
make logs-go          # логи только go-api
make logs-nuxt        # логи только nuxt
make psql             # psql в БД finance
make reset-db         # ⚠ снести том db_data и поднять PG заново (миграции из migrations/ применятся init-db.sh)
make migrate          # повторно прогнать /docker-entrypoint-initdb.d/migrations/*.up.sql на существующем volume
make restart-<svc>    # перезапустить сервис: make restart-go-api / make restart-nuxt

# Раздел «Себестоимость» (изолированный контур)
make cost-up          # поднять postgres-cost + python-cost + nuxt-cost + nginx-cost
make cost-down
make cost-logs
make cost-psql        # psql в БД cost
make cost-reset-db    # ⚠ снести том cost_db_data

# Миграции вне контейнера PG (golang-migrate, нужен docker):
../swarm/migrate.dev.sh           # = up
../swarm/migrate.dev.sh down 1    # откатить одну версию в обеих БД
../swarm/migrate.dev.sh version
../swarm/migrate.dev.sh force 1   # форснуть версию (если БД «dirty»)
```

Эндпоинты после `make up`:
- Nuxt: http://finance.local (или `http://localhost:3001` без nginx)
- Go API: http://api.finance.local (или `http://localhost:8081` без nginx)
- Postgres: `psql -h localhost -p 55432 -U finance finance`
- Redis: `redis-cli -h localhost -p 63791`
- python-analytics: только из `data_net`, наружу не торчит

Эндпоинты `make cost-up`:
- UI: http://localhost:8088/cost
- FastAPI: http://localhost:8091
- Postgres-cost: `psql -h localhost -p 55433 -U cost cost`

Для прямых имён нужен `/etc/hosts`:
```
127.0.0.1   finance.local api.finance.local
```

### Hot-reload и работа в контейнерах

- Go-API использует `air` (`go/.air.toml`), бинарник — `./.tmp/api`, watch на `*.go`.
- Nuxt с Vite-HMR, `CHOKIDAR_USEPOLLING=true` (без него поллинг не работает в WSL).
- FastAPI `python-cost` запускается через `uvicorn --reload --reload-dir /var/www/cost/app` — изменения в `nuxt-layer/` его НЕ дёргают.
- Тестов в репозитории нет ни в одном слое; единичные тесты пиши рядом, общего раннера нет.

## Архитектура

### Bewegt-points (что важно понимать сразу)

**Две Postgres-БД, две независимые цепочки миграций.**
- `migrations/` — БД `finance` (главная, Go-API). Применяются `init-db.sh` при первом старте контейнера postgres ИЛИ через `make migrate` / `swarm/migrate.dev.sh` на ходу. Golang-migrate ведёт `schema_migrations` сам.
- `python/cost/migrations/` — БД `cost`. Применяются аналогично, своим `init-db.sh` контейнера postgres-cost.
- Прод-CI прогоняет обе через `swarm/migrations/Dockerfile` → `migrate.sh` (нужны `POSTGRES_URL` и `COST_DATABASE_URL` в env). Это отдельный job `migrations` между `publish` и `deploy`.

**Nuxt → Go-API ходит двумя URL'ами.**
- С клиента (браузер) — через `NUXT_PUBLIC_API_BASE` (по умолчанию `http://api.finance.local`), это публичный URL через nginx.
- С сервера (SSR/nitro-routes, в частности `server/api/auth/b24/callback.get.ts`) — через `NUXT_INTERNAL_API_BASE=http://go-api:8080`, по докер-сети, минуя nginx. Если запутаешь — auth callback не сработает.

**B24 OAuth-callback живёт в Nuxt, не в Go.** Nitro-route `server/api/auth/b24/callback.get.ts` принимает code от B24, дёргает токен, потом POST'ит в Go `/api/auth/b24/callback`. То есть `B24_CLIENT_SECRET` нужен Nuxt'у, а не Go-API. Поэтому в nginx-фасаде весь префикс `/api/` на `finance.local` уходит в Nuxt (а Go висит на отдельном хосте `api.finance.local`) — симметрично с MP. Не пытайся «исправить» это и завернуть `/api` в Go.

**Auth-токены живут в Redis, не в JWT.** Схема ключей фиксирована (см. `go/internal/auth/tokens.go`):
- `auth_token:{uuid}` → userID (TTL 1 ч)
- `refresh_token:{uuid}` → userID (TTL 30 дней)
- `user_tokens:{userId}` → SET всех токенов пользователя (для logout-all)

UUID-токены, не JWT. `POST /api/auth/refresh` есть в Finance (в MP его не было — баг).

**`python-analytics` изолирован.** Сидит во второй докер-сети `data_net` (приватная PG ↔ python). Конектится под ролью `analytics_ro` (read-only public + RW на схему `analytics`). Наружу его открывать только через белый список Go-прокси — это явно описано в `python/app/main.py` как требование, не как реализация.

**Cost-раздел нельзя править из соседних каталогов.** В `python/cost/AGENTS.md` написано: разработчик cost-раздела правит ТОЛЬКО `python/cost/`. Frontend cost'а живёт в `python/cost/nuxt-layer/`, не в `nuxt/`. Если задача про раздел «Себестоимость» — лезь только в `python/cost/`. Этот файл (AGENTS.md) — авторитетный источник по эндпоинтам, MSSQL/OLAP-источникам и mock-режиму (`COST_MOCK=1` — отдают данные из `app/mocks.py` без VPN к 10.10.6.107/15).

**Дизайн-система — единственный источник истины для стилей.** Все цвета/радиусы/гутеры — переменные из `nuxt/assets/styles/design-system.css`. Числа в `JetBrains Mono` + `tabular-nums`, hairline borders без теней, accent индиго `#4338ca` (light) / `#818cf8` (dark). Не хардкодь цвета в компонентах — кабинет позже получит брендинг чисто через токены.

**Иерархия ролей.** `go/internal/auth/roles.go` — единственный источник истины. Прямые роли в БД (`users.roles` JSONB) разворачиваются `ExpandRoles` на выдаче токена и в middleware. Иерархия:

```
ROLE_ADMIN        ⊇ ROLE_COST_ADMIN, ROLE_FINANCE_ADMIN
ROLE_COST_ADMIN   ⊇ ROLE_COST_USER
ROLE_USER         — всегда добавляется
```

В Nuxt `useScope.hasScope('admin'|'cost'|'finance'|'analytics')` сводится к `roles.includes(...)` — потому что `/api/auth/me` возвращает уже эффективный набор. Новый пользователь после B24-логина создаётся без прикладных ролей; админ присваивает их через `PUT /api/admin/users/{id}/roles` (валидация по белому списку `auth.Allowed`).

**Уведомления — один общий поток, опциональное B24-дублирование.**
- API: `GET /api/notifications`, `/unread-count`, `POST /{id}/read`, `/read-all`. Колокольчик в шапке (`nuxt/components/NotificationsBell.vue`) использует adaptive polling 30 с / 10 с при открытом drawer.
- Welcome-уведомление создаётся при первом логине через `auth.PostLoginHook` (см. `main.go`); идемпотентно по флагу `users.welcome_notification_sent`.
- B24-дублирование идёт через корпоративный mfportal `site_api` (тот же контракт, что у MP `SiteApiNotifyService`): POST JSON `{id: "<b24_id>", message: "Finance: <title>\n<message>"}` + Basic Auth. Канал включается, когда заданы ВСЕ три ENV: `SITE_API_NOTIFY_URL`, `SITE_API_NOTIFY_USER`, `SITE_API_NOTIFY_PASSWORD`. В `.env.example` пусто → автоматически «prod-only». Per-user отключение — чекбокс на `/account` (`PATCH /api/account/notification-settings`).
- Доставка — fire-and-forget goroutine из `notifications.Service.Create`, аудит в полях `b24_sent_at`, `b24_attempts`, `b24_last_error`. Retry-цикла нет; повторная отправка вручную пока не реализована.

**Cost → уведомления через внутренний канал.**
- Go-API: `POST /internal/notifications` за middleware `RequireInternalToken` (заголовок `X-Internal-Token` = ENV `INTERNAL_SERVICE_TOKEN`). Если ENV пуст — все `/internal/*` отвечают 503 (fail-closed).
- python-cost: `app/notify.py` шлёт через `httpx`; URL+токен из `FINANCE_INTERNAL_API` и `INTERNAL_SERVICE_TOKEN`. Вызывается из `app/routes.py` после `save-changes` и `save-batch`. Сетевые ошибки no-op'ятся (логируем — но не валим сохранение цен).

**Баг-трекер.**
- API: `POST /api/bugtracker/report` (multipart: `payload` JSON + `screenshots[]`), требует `ROLE_USER`. Дедупликация по `signature = SHA256(section|url|title|description)` за 24 часа — повторный submit возвращает существующий id.
- Скриншоты: max 8 × 5MB, типы `image/png|jpeg|webp|gif`. Хранятся в `${BUGTRACKER_UPLOADS_DIR}/{report_id}/{uuid}.{ext}` (named volume `bugtracker_uploads`). Раздаются через Go: `GET /uploads/bugtracker/{id}/{file}` (анти-traversal через `filepath.Clean` + prefix-check).
- Уведомление админам: `service.go` → fire-and-forget goroutine вызывает `notifications.Service.CreateForAllAdmins`. Заголовок «Новый баг-репорт: …», ссылка на `/admin/bugtracker`.
- Админка: `/admin/bugtracker` (список + метрики, MTTR `EXTRACT(EPOCH FROM resolved_at - created_at)`) и `/admin/bugtracker/sources` (словарь причин).
- FAB-кнопка «Сообщить о проблеме» — `BugReportFab.vue` подключён в `app.vue`, прячется на `/login` и в cost-only-режиме. `html2canvas` подключается **динамическим импортом** — не раздувает основной бандл.

### Слои

```
finance/
├── go/                     # Go 1.22, модуль github.com/company/finance-api
│   ├── cmd/api/            # main.go + cors.go (точка входа, маршруты)
│   └── internal/
│       ├── auth/           # OAuth-handler, токены (Redis), пользователь (PG)
│       ├── config/         # env-driven Config (HTTP_ADDR, POSTGRES_URL, REDIS_ADDR, CORS_ORIGINS)
│       ├── db/             # pgx pool
│       └── redisx/         # redis-клиент
├── nuxt/                   # Nuxt 3, JS-зависимости в package.json
│   ├── app.vue             # layout двухколоночный (sidebar 220px + content)
│   ├── pages/              # /, /operations, /reports, /counterparties, /analytics, /account, /login
│   ├── composables/        # useAuth, useTheme, useScope, useEntity, useOauthState
│   ├── middleware/scope-guard.ts  # глобальный гард: требует ROLE_FINANCE на клиенте
│   ├── server/api/auth/b24/callback.get.ts  # обмен code → token, проксирует в Go
│   ├── plugins/            # api-unauthorized.client.ts: 401 → /login
│   └── utils/format.ts     # money/pct/num/delta — единые форматтеры
├── python/
│   ├── app/                # python-analytics — заглушка stdlib HTTPServer, /healthz
│   └── cost/               # FastAPI раздел «Себестоимость» — см. python/cost/AGENTS.md
│       ├── app/{main,routes,db,mocks}.py
│       ├── nuxt-layer/     # фронт раздела, подключается в основной nuxt.config.ts
│       └── migrations/     # SQL миграции БД cost (своя цепочка)
├── migrations/             # SQL миграции БД finance
└── swarm/
    ├── Makefile            # точка входа для всех команд
    ├── docker-compose.dev.yml  # основной контур
    ├── docker-compose.cost.yml # cost-контур (отдельная БД, отдельные порты)
    ├── docker-compose.yml      # prod-stack (Swarm, заполняется CI sed'ом из %%CONFIG_NAME%%)
    ├── ci-finance.yml          # GitLab CI: publish → migrations → deploy
    ├── init-db.sh              # init для основной PG (роли + миграции)
    ├── migrate.sh / migrate.dev.sh  # CI / локальные миграции (golang-migrate)
    └── config/{default_dev.conf,cost.conf,nginx.conf}
```

### CI/CD

GitLab CI (`swarm/ci-finance.yml`):
- `publish` собирает 5 образов (go-api, nuxt, python-cost, redis, migrations) и пушит в `registry.markformelle.ru`. Триггерится push'ем в `stage` или `master`.
- `migrations` запускает образ `migrations` против обеих БД (`POSTGRES_URL`, `COST_DATABASE_URL`).
- `deploy` обновляет Swarm-stack через `docker stack deploy`, шаблонит `docker-compose.yml` через sed (`%%CONFIG_NAME%%`, `%%CONFIG2_NAME%%` → имена `docker config`'ов с env-файлами). После деплоя проверяет, что все сервисы поднялись на нужном SHA — иначе fail.
- `rollback_app_stage` / `rollback_app_prod` — ручные джобы, дёргают `docker service update --rollback`.

В компоузе `docker-compose.yml` строки с `%%CONFIG_NAME%%` — это плейсхолдеры, которые CI заменяет; для prod-стека не запускай этот файл локально как есть.

## Правила работы с env-переменными

**Любая новая переменная окружения, которую читает код (Go `os.Getenv`/`env(...)`,
Python `os.environ`, Nuxt `process.env.*` / `runtimeConfig`), ОБЯЗАНА появиться
в `.env.example` в том же коммите.** Каждая запись — с коротким комментарием:
что управляет, какой дефолт, что произойдёт, если оставить пустой.

Аналогично для cost-раздела — `python/cost/.env.example`.

Запрещено добавлять env «втихую» (только в `docker-compose.dev.yml` или `config.go`,
без описания в `.env.example`). Прод-конфиги собираются по этому файлу — пропуск
там = выкатим релиз без переменной, и сервис упадёт после деплоя
(как было с `MSSQL_PREMASTER_*` / `DEBT_MOCK` → «premaster repo not configured»).

## Часто встречающиеся ошибки

- **«Auth callback ломается локально»** → проверь `NUXT_INTERNAL_API_BASE=http://go-api:8080` в env Nuxt. Server-route bypasses nginx по докер-сети.
- **«502 на /api/cost/* в основном контуре»** → cost-стек не поднят. `default_dev.conf` явно отдаёт 502, чтобы это было видно. Подними `make cost-up`.
- **«Миграции не накатываются после `make up`»** → init-db.sh запускается только при ПЕРВОМ старте volume. На существующем — `make migrate` (повтор init-блока) или `swarm/migrate.dev.sh` (golang-migrate).
- **«Падает pyodbc к 10.10.6.x»** → выставь `COST_MOCK=1` в `python/cost/.env`. Будет ходить в моки из `app/mocks.py`.
- **«analytics_ro не видит таблицу»** → права через `ALTER DEFAULT PRIVILEGES` выдаются только на будущие таблицы. Для уже созданных — `GRANT SELECT ON ALL TABLES IN SCHEMA public TO analytics_ro;` в `init-db.sh`.
- **«python-cost не может создать уведомление»** → проверь, что `INTERNAL_SERVICE_TOKEN` одинаков у `go-api` и `python-cost` (`.env`), и что cost-стек запущен В ОДНОЙ сети с go-api (`finance_dev_network`, см. `docker-compose.cost.yml`). Если токен у go-api пуст — `/internal/*` возвращают 503.
- **«В Б24 не приходят уведомления в prod»** → причины по убыванию вероятности: (1) не задана хотя бы одна из `SITE_API_NOTIFY_URL/USER/PASSWORD`; (2) у юзера `notify_via_b24=false` (см. `/account`); (3) у юзера в БД `b24_id IS NULL` (не залогинился через B24 OAuth); (4) site_api отвечает `success=false` → смотри `notifications.b24_last_error` и `b24_attempts`.
- **«premaster repo not configured» на /api/reports/debt/***» → в env go-api не заданы `MSSQL_PREMASTER_SERVER/USER/PASSWORD` И `DEBT_MOCK` не равен `1`. Решение: либо `DEBT_MOCK=1` (фикстуры), либо все три `MSSQL_PREMASTER_*` (live на снэпшот `Premaster1C_20260514`). См. `.env.example` блок «Задолженность ВГО».
- **«В отчёте ВГО нет выручки (только ДЗ/КЗ)»** → `DEBT_BACKEND=finpl` (дефолт), но витрина `FinDWH.dbo.Table_Fin_PL` пуста — её наполняет job `Update_Table_Fin_PL` (ОПУ «по свежим алгоритмам»). Выручка берётся из неё (`GroupPL='ПРОДАЖИ'`, USD), ДЗ/КЗ/договор/просрочка — из Premaster (композиция `finplComposite`). Откат на старый источник — `DEBT_BACKEND=mssql`. Справочник код→ИНН и дизайн слияния: `docs/reports/debt/finpl-merge.md`, `SPEC.md`.
