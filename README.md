# Finance Cabinet

Личный кабинет для финансистов рядом с MP-кабинетом маркетплейсов. Полностью изолирован: своя БД, своё B24 OAuth-приложение, свой домен. Переключение между кабинетами — кнопкой в шапке.

## Стек

| Слой | Технологии |
|---|---|
| Бэк | Go 1.22 — API, авторизация (Bitrix24 OAuth), refresh-токены, Bearer-middleware |
| Аналитика | Python 3.12 — изолированный sandbox (read-only PG-роль, отдельная сеть) |
| Фронт | Nuxt 3 — финансовый дашборд (full-width, sidebar, плотные таблицы, JetBrains Mono для цифр) |
| Хранилище | PostgreSQL 16, Redis 7 |

## Запуск (локально)

```bash
cp .env.example .env
# подставить B24_CLIENT_SECRET и NUXT_PUBLIC_B24_CLIENT_ID

cd swarm
make up        # docker compose -f docker-compose.dev.yml up -d --build
make logs      # хвост логов всех сервисов
make down      # остановить
make psql      # шелл в БД
make help      # список команд
```

В `/etc/hosts` добавьте:
```
127.0.0.1   finance.local api.finance.local
```

После старта:
- Nuxt:    <http://finance.local>          (или `http://localhost:3001` напрямую, без nginx)
- Go API:  <http://api.finance.local>      (или `http://localhost:8081`)
- Postgres: `psql -h localhost -p 55432 -U finance finance`
- Redis:   `redis-cli -h localhost -p 63791`
- Python sandbox: внутренний порт `python-analytics:8090` (наружу не торчит, ходит только в `data_net`)

## Структура

```
finance/
├── nuxt/                    # фронт — Nuxt 3
│   ├── app.vue              # двухколоночный layout (sidebar + content)
│   ├── assets/styles/       # дизайн-система: tokens, components, forms
│   ├── components/          # KpiTile, Sparkline, LoginCard
│   ├── composables/         # useAuth, useTheme, useScope, useOauthState
│   ├── middleware/          # scope-guard
│   ├── pages/               # /, /operations, /reports, /counterparties, ...
│   ├── plugins/             # api-unauthorized.client.ts (401 → /login)
│   ├── server/api/auth/b24/ # OAuth-callback (server route)
│   └── utils/format.ts      # money / pct / num / delta — все форматтеры
├── go/                      # API
│   ├── cmd/api/             # main + CORS
│   └── internal/auth/       # service, handler, tokens (Redis), user (PG)
├── python/                  # аналитика-sandbox (заглушка пока)
├── migrations/              # SQL-миграции (применяются init-db.sh при первом старте PG)
└── swarm/                   # docker-контур (как в MP)
    ├── docker-compose.dev.yml
    ├── Makefile
    ├── init-db.sh           # роли + миграции + GRANT'ы для analytics_ro
    ├── config/              # nginx.conf, default_dev.conf
    ├── nginx/Dockerfile.dev
    ├── nuxt/Dockerfile.dev
    ├── go-api/Dockerfile.dev
    └── python-analytics/Dockerfile.dev
```

## Дизайн-система

Финансовый кабинет нарочно строже и плотнее MP:

- **Числа в монотипе** (`JetBrains Mono`, `tabular-nums`) — все суммы, ИНН, даты, % выровнены по разрядам.
- **Hairline borders, без теней** — карточки = `1px solid var(--border)`, `border-radius: 6-8px`.
- **Узкий accent** — индиго `#4338ca` (light) / `#818cf8` (dark) только для активных состояний и CTA.
- **Семантика дельт** — пара зелёный/красный (`#0a7f3f` / `#b42318`), янтарный для предупреждений.
- **Density-режимы** — таблицы поддерживают `compact` / `normal` / `comfortable`.
- **Full-width layout** — никакого `max-width: 1440px` на основных страницах. Sidebar `220px` + контент на оставшуюся ширину; на ультрашироких мониторах увеличивается padding.
- **Sticky header + footer в таблицах** — для длинных списков операций.
- **Плотные KPI-сетки** — 6 колонок на десктопе, 3 на планшете, 2 на мобильном.

Токены: `nuxt/assets/styles/design-system.css`. Все компоненты обязаны использовать только переменные оттуда — это позволит позднее ввести брендинг без правок CSS.

## Авторизация

Скопирована 1:1 из MP с двумя отличиями:
1. Реализован `POST /api/auth/refresh` (в MP его не было — баг).
2. Свои роли: `ROLE_FINANCE`, `ROLE_FINANCE_ADMIN`, `ROLE_ANALYST`.

Контракт `/api/auth/b24/callback` — см. `FINANCE_PORTING_GUIDE.md` § 1.4.

Схема Redis (TTL):
- `auth_token:{uuid}` → userID — 1 час
- `refresh_token:{uuid}` → userID — 30 дней
- `user_tokens:{userId}` → SET всех токенов (для logout-all)

## Разделение со старым кабинетом

В шапке Finance — кнопка «← Маркетплейсы» (URL берётся из `NUXT_PUBLIC_MP_URL`). В MP-репо нужно симметрично добавить кнопку «Финансы →». Это отдельный маленький PR, делается уже в MP, не в этом репо.

## TODO

- [ ] Подключить настоящие эндпоинты (`/api/operations`, `/api/counterparties`, …) — сейчас mock в pages.
- [ ] Перенести из MP `ToastCenter`, `AppDialog`, `AppEmptyState` (адаптировать под новые токены).
- [ ] WebSocket для live-обновлений баланса/операций.
- [ ] Настоящие миграции таблиц `transactions`, `accounts`, `counterparties`, `categories`.
- [ ] Python: реальный аналитический endpoint, белый список через Go-прокси.
