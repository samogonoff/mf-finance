# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

## Что это

Finance Cabinet — изолированный кабинет финансиста, сибиринг MP-кабинета маркетплейсов. Своя БД, своё B24 OAuth-приложение, свой домен. Часть кода (auth, контракт `/api/auth/b24/callback`) портирована 1:1 из MP — см. упоминания `FINANCE_PORTING_GUIDE.md` в коде и `README.md`.

Внутри живут **два независимых контура** на одном репо:

1. **Основной контур** (`swarm/docker-compose.dev.yml`, `make up`) — Go-API + Nuxt + Postgres `finance` + Redis + python-analytics sandbox.
2. **Контур «Себестоимость»** (`swarm/docker-compose.cost.yml`, `make cost-up`) — отдельный FastAPI (`python-cost`) + отдельный Postgres `cost` + nginx-cost. Раздел встраивается в основной Nuxt через `extends: ["../python/cost/nuxt-layer"]`.

Контуры можно гонять параллельно или по-отдельности. У них РАЗНЫЕ БД и РАЗНЫЕ миграции.

## Команды

Все команды запускаются из `swarm/`.

**Контур поднимается под именем проекта `finance` (`docker compose -p finance …`).**
Без `-p` compose берёт имя проекта из каталога — `swarm`, — а такой же каталог есть
у соседних кабинетов на машине разработчика: подъём finance останавливает и
пересоздаёт ЧУЖИЕ `swarm-postgres-1` / `swarm-redis-1`. Makefile это делает сам;
если запускаете compose руками — не забудьте `-p finance`. Контейнеры называются
`finance-postgres-1`, `finance-go-api-1`, `finance-nuxt-1` и т.д.


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

# Go — вне докера (нужен только go 1.25 локально), обязательно перед коммитом Go-задачи:
cd go && go build ./... && go vet ./... && go test ./...
cd go && go test ./internal/plans/...        # один пакет
cd go && go test ./internal/plans/ -run TestName -v  # один тест
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
- **Go: раннер есть.** Перед коммитом любой Go-задачи: `cd go && go build ./... && go vet ./... && go test ./...` (конвенция, см. `tasks/todo.md`). Тесты лежат рядом с кодом (`*_test.go`, ~20 файлов) — заметно в `internal/plans/` (расчёты, ABAC, календарь, workflow) и `internal/etl/` (SQL-маппинг extract'ов); используют in-memory store/фикстуры, живой БД не требуют.
- Nuxt и Python (`python/app`, `python/cost`) — тестов нет, общего раннера нет, линтеров (eslint/ruff/golangci-lint) не настроено.

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

**Модуль «Тактические планы» (`go/internal/plans/`) — самый крупный узел кода, не путать с cost-разделом.** P&L-планирование по ЦФО/площадкам с ABAC-срезами (`abac.go`, `abac_scope.go`, `plans_user_scope` — применяются страна/ЮЛ/шаг, а не только code_cfo), справочниками-директориями (`directories_*`, версионируемые, синхронизация через `sync.go`/`cfo_import.go`), календарём периодов (`calendar.go` + `calendar_store.go` — календари читаются из `plans_country_calendar`, не из кода), workflow согласования (`workflow.go`, `workflow_task.go`) и формами. Источник истины по требованиям и статусу — `docs/reports/plans/{SPEC,plan,todo}.md` (отдельная от корневого `SPEC.md`, который про ВГО-отчёт); скорректированные ТЗ 2026-08 разобраны в `SPEC.md` §28. Факт/стратегия читаются из OLAP/SQL онлайн, с mock-фолбэком `PLANS_MOCK=1`.

**Карточка формы — общая оболочка процесса всех форм комплекта.** `form_card` (+`card_approval`, `card_version`, `plans_form_route`; миграция 0031, код `card*.go`). Единица маршрута — ФОРМА, а не этап периода: крупные и мелкие МП, как и четыре страны розницы, живут независимо (возврат одной не блокирует другие), поэтому на один `pl_instance` приходится несколько карточек. Статусы `draft → on_approval → approved → published|publish_failed`, `returned`, `archived`; наружу отдаются метками ТЗ (`on_approval_1.2`, `returned_to_1.1`) из пары статус+шаг. Возврат — только назад, только с комментарием, согласования от целевого шага и выше помечаются `revoked` (не удаляются). При каждом переходе пишется версия-снапшот (значения + условия + курсы + НДС). После утверждения период закрыт на запись для всех, изменение — через `reopen` с причиной. Маршрут — данные: шаг «Финансист» включается флагом `enabled` без правки кода, `skip_if_same_user` закрывает вырожденное самосогласование.

**Форма МП: инверсия направления расчёта.** Действующий каскад `computeMpPlatform` («суммы → доли») остался, рядом появился `computeMpPlatformInverse` («условия → суммы», ТЗ §3.4) — вводятся продажи, %СПП, две наценки и доли статей, а расходная часть считается. Направление выбирается ПОЛЕМ КАРТОЧКИ `calc_mode` (`legacy`|`inverse`), а не глобально: закрытые периоды считаются тем алгоритмом, которым были утверждены. Источник истины расходной части — реестр «Условия площадки» (`mp_conditions`/`mp_condition_item`, миграция 0036, код `mp_conditions*.go`): копируется из прошлого периода одним действием, diff показывает согласующему изменения долей, отклонение сверх порога требует обоснования. Ручное переопределение расчётной суммы не перезатирается пересчётом (в форме видно «расчёт даёт X, вручную Y»). Приёмка — контрольная выборка ТЗ (Приложение Б, июнь-2026): `mpform_inverse_test.go`, любое расхождение = регресс. Клиентское зеркало обоих каскадов — `nuxt/composables/useMpCascade.ts`, правится СИНХРОННО с Go.

**НДС и курсы — из справочников, не из кода.** `RateBook` (`rates.go`) читает `dir_vat` (эффективная ставка площадки: 20,36 % у WB/Lamoda/Ozon, 16,62 % у Yandex Market — не законодательные 20 %) и `dir_fx_rate` (помесячные курсы + KZT/UZS для ввода в валюте площадки). Применённый курс фиксируется в версии карточки, иначе утверждённые суммы «уезжают» при следующем обновлении справочника. Хардкоды `vatByCountry`/`FxRateSeed` остались только фолбэком на случай недоступного справочника.

**Публикация плана во внешний контур.** `publish_mapping` + `publish_log` (миграция 0032, код `publish*.go`). Приёмники `Budgeting.dbo.FormToLoa*/VFORMTOLOAD*` — девять колонок, БЕЗ первичного ключа и без колонок сценария/ЮЛ: сценарий задаётся выбором таблицы, а идемпотентность делает сервис (DELETE по логическому ключу Параметр+Страна+КодЦФО+КодPL+Дата + INSERT в одной транзакции) с последующим контролем «Σ введено − Σ записано = 0». Открытые вопросы BI (какой «Параметр», агрегат 250/480 или детализация, кто пишет BYN-пару) вынесены в данные `publish_mapping`; пока правила выключены, работает только `dry_run` — отчёт, которым BI и подтверждает решения. Запись включает `PLANS_PUBLISH_ENABLED=1`, таблицы — белым списком `PLANS_PUBLISH_TARGETS` (имя приёмника приходит из данных, поэтому произвольная таблица недопустима).

**Первая генерация формы МП заморожена.** `/api/plans/mp/{form,compute,copy,formula,export,import}` отдают 410 Gone (`PLANS_LEGACY_MP_API=1` — аварийный откат). Актуальная форма — `/api/plans/tasks/{taskId}/mp-form`. Не подключайте к новым формам `calc.go`/`eval.go`/`pl_formula_override` — это движок легаси-ветки.

**Cost → уведомления через внутренний канал.**
- Go-API: `POST /internal/notifications` за middleware `RequireInternalToken` (заголовок `X-Internal-Token` = ENV `INTERNAL_SERVICE_TOKEN`). Если ENV пуст — все `/internal/*` отвечают 503 (fail-closed).
- python-cost: `app/notify.py` шлёт через `httpx`; URL+токен из `FINANCE_INTERNAL_API` и `INTERNAL_SERVICE_TOKEN`. Вызывается из `app/routes.py` после `save-changes` и `save-batch`. Сетевые ошибки no-op'ятся (логируем — но не валим сохранение цен).

**Баг-трекер.**
- API: `POST /api/bugtracker/report` (multipart: `payload` JSON + `screenshots[]`), требует `ROLE_USER`. Дедупликация по `signature = SHA256(section|url|title|description)` за 24 часа — повторный submit возвращает существующий id.
- Скриншоты: max 8 × 5MB, типы `image/png|jpeg|webp|gif`. Хранятся в `${BUGTRACKER_UPLOADS_DIR}/{report_id}/{uuid}.{ext}` (named volume `bugtracker_uploads`). Раздаются через Go: `GET /uploads/bugtracker/{id}/{file}` (анти-traversal через `filepath.Clean` + prefix-check).
- Уведомление админам: `service.go` → fire-and-forget goroutine вызывает `notifications.Service.CreateForAllAdmins`. Заголовок «Новый баг-репорт: …», ссылка на `/admin/bugtracker`.
- Админка: `/admin/bugtracker` (список + метрики, MTTR `EXTRACT(EPOCH FROM resolved_at - created_at)`) и `/admin/bugtracker/sources` (словарь причин).
- FAB-кнопка «Сообщить о проблеме» — `BugReportFab.vue` подключён в `app.vue`, прячется на `/login` и в cost-only-режиме. `html2canvas` подключается **динамическим импортом** — не раздувает основной бандл.

**Логи — JSON в stdout + опциональная трансляция в ELK (Logstash).** Схема портирована 1:1 из кабинета NCI и покрывает ОБА контура.
- Формат общий для всех рантаймов: одна JSON-запись на строку (кодек `json_lines`), поля `time/level/msg/service/host` + свои (`request_id`, `route`, `status`, `duration_ms`, `user_id`/`user`, `error`). `service` различает источник: `finance-api` (Go), `finance-nuxt` (Nitro-роуты), `finance-cost` (FastAPI раздела «Себестоимость»), `finance-analytics` (песочница).
- Реализации: `go/internal/logship/` (io.Writer вторым в `io.MultiWriter` рядом со stdout; slog настраивается в `go/cmd/api/logging.go`), `nuxt/server/utils/{logstash,logger}.ts`, `python/cost/app/logship.py`, `python/app/logship.py`. Два python-модуля — намеренные копии, а не общий пакет: контуры собираются в разные образы, `python-cost` не видит `python/`, и наоборот. Оба на чистом stdlib.
- **Инвариант:** отправка асинхронная и НИКОГДА не блокирует горячий путь. Недоступный Logstash не тормозит запросы и не роняет stdout — строки просто отбрасываются, счётчик потерь раз в минуту уходит в лог (`logship: строки лога не доставлены в Logstash`; рост = канал в ELK ослеп, это алерт).
- **stdout остаётся базовым каналом всегда** — его собирает docker. ELK только дублирует.
- Включается заданием `LOGSTASH_HOST` (+ `LOGSTASH_PORT`, дефолт 5044). Пусто → трансляция выключена. Переменные читают ОБА рантайма основного контура из корневого `.env` **без префикса `NUXT_`**: nitro-код смотрит в `process.env` напрямую, а не через `runtimeConfig` (иначе понадобился бы `NUXT_LOGSTASH_HOST` и переменная перестала бы быть общей с Go). Контур «Себестоимость» изолирован — те же имена задаются отдельно в `python/cost/.env`.
- Go-API логирует каждый запрос (`withLogging` в `cmd/api/logging.go`) и проставляет `X-Request-Id` (свой или пришедший от прокси); `user_id` доносится до access-лога через `auth.AuthObserver` — читать пользователя из контекста снаружи нельзя, `RequireBearer` кладёт его в клон запроса. Разбросанные по коду `log.Printf` перехвачены bridge'ом в slog, переписывать их не нужно.
- python-analytics: свой access-лог в `Handler._access_log` (`python/app/main.py`), текстовый лог `BaseHTTPServer` подавлен, чтобы запрос не дублировался; сервис живёт только в dev-контуре (в прод-стек он не входит).
- python-cost: свой access-лог-middleware в `app/main.py` (он же прокидывает `X-Request-Id`), INFO-строки `uvicorn.access` погашены, чтобы запрос не уезжал в ELK дважды.
- Проверка канала и тестовые строки для настройки индекса ELK: `cd swarm && make logship-test` (шлёт по образцу каждого сервиса с `marker=logship-connectivity-test`).

### Слои

```
finance/
├── go/                     # Go 1.25, модуль github.com/company/finance-api
│   ├── cmd/
│   │   ├── api/            # main.go + cors.go + logging.go (slog/JSON, access-лог) — точка входа
│   │   ├── cfo-import/     # разовый импорт справочника ЦФО/ЦЗ (plans)
│   │   ├── findebt-etl/    # standalone-раннер ETL по задолженности (fact_findebt*)
│   │   ├── logship-test/   # проверка канала логов в Logstash (make logship-test)
│   │   ├── mssql-probe/    # разведочные SQL-пробы к MSSQL (PROBE_SQL=...), не коммитить логику, только утилита
│   │   └── vgoprobe/       # разведочный проб к vGLMFAddUSD
│   └── internal/
│       ├── auth/           # OAuth-handler, токены (Redis), пользователь (PG), роли (roles.go)
│       ├── bugtracker/     # POST /api/bugtracker/report — дедуп, скриншоты, admin-метрики
│       ├── config/         # env-driven Config (HTTP_ADDR, POSTGRES_URL, REDIS_ADDR, CORS_ORIGINS, …)
│       ├── db/             # pgx pool
│       ├── etl/            # extract/bootstrap/incremental для ClickHouse-фактов (findebt, currency, debtarh, glmf)
│       ├── internalapi/    # POST /internal/* за RequireInternalToken (канал python-cost → notifications)
│       ├── logship/        # асинхронная трансляция slog-логов в Logstash (ELK)
│       ├── notifications/  # общий поток уведомлений + опциональное B24-дублирование
│       ├── plans/          # модуль «Тактические планы» (P&L) — см. Bewegt-point ниже и docs/reports/plans/SPEC.md
│       ├── redisx/         # redis-клиент
│       ├── reports/debt/   # отчёт «Задолженность ВГО» (mssql/clickhouse/finpl backends) — см. корневой SPEC.md
│       └── users/          # admin-управление ролями пользователей
├── nuxt/                   # Nuxt 3, JS-зависимости в package.json
│   ├── app.vue             # layout двухколоночный (sidebar 220px + content)
│   ├── pages/              # /, /operations, /reports, /counterparties, /analytics, /account, /login
│   ├── composables/        # useAuth, useTheme, useScope, useEntity, useOauthState
│   ├── middleware/scope-guard.ts  # глобальный гард: требует ROLE_FINANCE на клиенте
│   ├── server/api/auth/b24/callback.get.ts  # обмен code → token, проксирует в Go
│   ├── plugins/            # api-unauthorized.client.ts: 401 → /login
│   └── utils/format.ts     # money/pct/num/delta — единые форматтеры
├── python/
│   ├── app/                # python-analytics — заглушка stdlib HTTPServer, /healthz + logship.py (ELK)
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

- **«`make up` уронил соседний кабинет / `Conflict. The container name "/swarm-postgres-1" is already in use`»** → compose поднят без явного имени проекта: имя бралось из каталога `swarm`, которое совпадает с другими кабинетами на машине. Makefile уже использует `-p finance`; если запускали `docker compose` руками — добавьте `-p finance`. Пострадавшие чужие контейнеры поднимаются обратно `docker start swarm-postgres-1 swarm-redis-1`.
- **«Auth callback ломается локально»** → проверь `NUXT_INTERNAL_API_BASE=http://go-api:8080` в env Nuxt. Server-route bypasses nginx по докер-сети.
- **«502 на /api/cost/* в основном контуре»** → cost-стек не поднят. `default_dev.conf` явно отдаёт 502, чтобы это было видно. Подними `make cost-up`.
- **«Миграции не накатываются после `make up`»** → init-db.sh запускается только при ПЕРВОМ старте volume. На существующем — `make migrate` (повтор init-блока) или `swarm/migrate.dev.sh` (golang-migrate).
- **«CI-джоб `migrations` падает с `try lock failed ... SELECT pg_advisory_lock($1)` / `canceling statement due to lock timeout`»** → advisory-lock мигратора держит осиротевшая сессия от предыдущего прогона. Типовая цепочка: миграция встала в очередь за ACCESS EXCLUSIVE (сессии python-cost на той же таблице) → job убит по таймауту → но `docker run` убивает только CLI, а сам контейнер живёт дальше и держит lock. Лечится само: `swarm/migrate.sh` перед `up` делает preflight (показывает держателей advisory-локов и прибивает тех, кто старше `MIGRATE_UNLOCK_IDLE_SECONDS`, дефолт 60 с), а CI перед накатом сносит зомби-контейнеры от образа `migrations-*`. Руками: `docker run --rm --network host --env-file .env $MIGRATE_IMAGE doctor` (диагностика без изменений) и `... unlock-cost` / `unlock-finance`. Ручки: `MIGRATE_AUTO_UNLOCK=0` отключает автоснятие, `MIGRATE_LOCK_TIMEOUT` / `MIGRATE_STATEMENT_TIMEOUT` — таймауты в DSN мигратора (см. `.env.example`).
- **«Миграция падает с `canceling statement due to lock timeout` на `ALTER TABLE cost_data_cache`»** → это уже НЕ advisory-lock, а ACCESS EXCLUSIVE: таблицу держат живые сессии python-cost (запросы к кэшу, refresh на 960K строк), и `ALTER TABLE` не дожидается своей очереди за `MIGRATE_LOCK_TIMEOUT`. `migrate.sh` повторяет `up` (`MIGRATE_UP_RETRIES`, дефолт 3), и **после первой неудачи** прибивает держателей таблиц старше `MIGRATE_BLOCKER_AGE_SECONDS` (60 с), затем лечит dirty и повторяет. На первой попытке блокировщиков не трогает — нормальный деплой в них не упирается. Выключается через `MIGRATE_KILL_BLOCKERS=0` (тогда накат будет падать, пока приложение держит таблицу; альтернатива — на время наката погасить python-cost).
- **«БД cost dirty / `force-cost 13` в CI»** → прибитого гвоздями force'а больше нет. `migrate.sh` сам читает `schema_migrations`, и при `dirty=true` форсит `version-1`, после чего `up` переприменяет упавшую миграцию (все наши миграции идемпотентны: `IF NOT EXISTS` / `DROP CONSTRAINT IF EXISTS`). Отключается через `MIGRATE_AUTO_HEAL=0`.
- **«Миграция cost падает с `syntax error at or near "IF"`»** → в цепочку Postgres попал T-SQL-скрипт для `FinSandBox` (MSSQL). Такие миграции живут в `python/cost/migrations/mssql/`, а в основной цепочке остаётся no-op `SELECT 1;` — так сделано для 0006, 0013, 0015.
- **«Логи не появляются в ELK»** → (1) `LOGSTASH_HOST` пуст — трансляция выключена по дизайну, в логе на старте нет строки `logship: трансляция логов в Logstash включена`; (2) для контура «Себестоимость» переменную надо задать ОТДЕЛЬНО в `python/cost/.env` — корневой `.env` он не читает; (3) в Nuxt имя переменной без префикса (`LOGSTASH_HOST`, не `NUXT_LOGSTASH_HOST`); (4) сам канал проверяется `cd swarm && make logship-test`; (5) если строки шлются, но не доезжают — смотри в stdout периодический warn `logship: строки лога не доставлены в Logstash` (там `dropped_total`).
- **«В Superset все визуализации с `DB engine Error: Error -2 connecting to redis-superset:6379. Name or service not known`»** → не поднят контейнер Redis BI-контура. Имя не резолвится, потому что контейнера нет — Superset при этом работает, ломаются только запросы данных (кэш и results backend у него в Redis). Типовая причина: перезапуск демона Docker поднимает контейнеры с restart-политикой и не трогает те, у кого её нет. Проверить: `docker ps --filter name=redis-superset`; поднять: `cd swarm && make superset-up`. Симптом обманчив тем, что раздел «Себестоимость» продолжает работать — он этот Redis не использует. Политика `restart: on-failure` у `redis-superset` проставлена (17.08.2026), но у `swarm-redis-1`, `nginx`, `nuxt` основного контура её по-прежнему нет: после перезапуска демона Go-API поднимется без Redis, и авторизация ляжет тем же образом (токены живут в Redis).
- **«Падает pyodbc к 10.10.6.x»** → выставь `COST_MOCK=1` в `python/cost/.env`. Будет ходить в моки из `app/mocks.py`.
- **«analytics_ro не видит таблицу»** → права через `ALTER DEFAULT PRIVILEGES` выдаются только на будущие таблицы. Для уже созданных — `GRANT SELECT ON ALL TABLES IN SCHEMA public TO analytics_ro;` в `init-db.sh`.
- **«python-cost не может создать уведомление»** → проверь, что `INTERNAL_SERVICE_TOKEN` одинаков у `go-api` и `python-cost` (`.env`), и что cost-стек запущен В ОДНОЙ сети с go-api (`finance_dev_network`, см. `docker-compose.cost.yml`). Если токен у go-api пуст — `/internal/*` возвращают 503.
- **«В Б24 не приходят уведомления в prod»** → причины по убыванию вероятности: (1) не задана хотя бы одна из `SITE_API_NOTIFY_URL/USER/PASSWORD`; (2) у юзера `notify_via_b24=false` (см. `/account`); (3) у юзера в БД `b24_id IS NULL` (не залогинился через B24 OAuth); (4) site_api отвечает `success=false` → смотри `notifications.b24_last_error` и `b24_attempts`.
- **«premaster repo not configured» на /api/reports/debt/***» → в env go-api не заданы `MSSQL_PREMASTER_SERVER/USER/PASSWORD` И `DEBT_MOCK` не равен `1`. Решение: либо `DEBT_MOCK=1` (фикстуры), либо все три `MSSQL_PREMASTER_*` (live на снэпшот `Premaster1C_20260514`). См. `.env.example` блок «Задолженность ВГО».
- **«ВГО-отчёт через GLMF/ClickHouse (новый источник)»** → `DEBT_BACKEND=ch` + `DEBT_CH_SOURCE=glmf` читает `finance.fact_glmf` (выручка по точным Дт/Кт ТЗ + ДЗ/КЗ-сальдо до субсчёта) + `finance.dim_contract` (договоры из субконто Premaster, join по `doc_id`). Источник полнее Premaster1C, с каноничной классификацией. Залить: CH-миграции `swarm/migrate-clickhouse.sh` (004/005), затем bootstrap по источнику: `POST /api/admin/etl/debt/bootstrap {company_id, source: "glmf"|"contract"}` или CLI `cmd/debt-bootstrap` с `BOOTSTRAP_SOURCE=glmf|contract`. Инкремент fact_glmf — по `DateOfLoad` (воркер при `DEBT_CH_SOURCE=glmf`). Дефолт пока `mssql` (glmf — opt-in до сверки чисел, SPEC §10). Архитектура: `SPEC.md`, `docs/reports/debt/{tz-requirements,prod-verification}.md`.
- **«В отчёте ВГО нет выручки (только ДЗ/КЗ)»** → включён `DEBT_BACKEND=finpl`, но витрина `FinDWH.dbo.Table_Fin_PL` пуста — её наполняет job `Update_Table_Fin_PL` (ОПУ «по свежим алгоритмам»). finpl реализован, но **не дефолт** (дефолт `mssql` = Premaster, до сверки сумм на наполненной витрине). Под finpl выручка берётся из Table_Fin_PL (`GroupPL='ПРОДАЖИ'`, USD), ДЗ/КЗ/договор/просрочка — из Premaster (композиция `finplComposite`). Справочник код→ИНН и дизайн слияния: `docs/reports/debt/finpl-merge.md`, `SPEC.md`.
