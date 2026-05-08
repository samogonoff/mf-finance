# Раздел «Себестоимость» (Cost History)

Самодостаточный модуль кабинета: API, миграции и Nuxt-страница лежат
в этом каталоге. Разработчик раздела правит **только `python/cost/`**.

Бизнес-логика — портирование `var/original/` (Django + jQuery) на наш
стек: **FastAPI** + **Nuxt 3 layer**. Идентификация пользователя берётся
из основного контура (Go-API/`useAuth`) — отдельной таблицы пользователей
у раздела нет.

## Источники данных

| Источник | Где          | Что лежит                                  | Доступ  |
|----------|--------------|--------------------------------------------|---------|
| postgres-cost     | локальный контейнер | `cost_price_changes_audit` (лог изменений)        | RW      |
| MSSQL `Checks`    | srv-sql 10.10.6.107  | `CostHistory` (~13M строк)                        | RO      |
| MSSQL `Gpartner`  | srv-sql 10.10.6.107  | `s_price_level` (справочник уровней цен)          | RO      |
| MSSQL `FinSandBox`| srv-olap 10.10.6.15  | `CostHistory_Changes` (приёмник правок цен)       | RW      |

Креды задаются в `python/cost/.env` (см. `.env.example`).

## Структура

```
python/cost/
├── .env.example              # шаблон окружения раздела (postgres-cost + MSSQL/OLAP)
├── Dockerfile.dev            # python:3.12 + msodbcsql18 + pyodbc
├── requirements.txt
├── init-db.sh                # применение миграций при первом старте postgres-cost
├── app/                      # FastAPI
│   ├── main.py               #   точка входа, lifespan, CORS, префикс /api/cost
│   ├── db.py                 #   asyncpg-пул (postgres-cost) + pyodbc-коннекты
│   └── routes.py             #   эндпоинты раздела ← основное место правки
├── migrations/               # SQL-миграции локальной БД
│   └── 0001_cost_init.up.sql
└── nuxt-layer/               # Nuxt-layer, расширяет основной nuxt.config.ts
    ├── nuxt.config.ts
    ├── plugins/
    │   └── cost-bypass.client.ts   # стаб user в cost-only режиме
    └── pages/cost/
        └── index.vue         # страница раздела ← основное место правки UI
```

## Запуск

```bash
cp python/cost/.env.example python/cost/.env   # один раз; заполнить креды MSSQL/OLAP
cd swarm && make cost-up
```

После этого:

| Что               | URL                                   |
|-------------------|---------------------------------------|
| UI раздела        | http://localhost:8088/cost            |
| API              | http://localhost:8088/api/cost/filter-options |
| Healthcheck       | http://localhost:8091/healthz         |
| psql в свою БД    | `psql -h localhost -p 55433 -U cost cost` |

Все порты задаются в `python/cost/.env`.

## API

| Method | URL                              | Что делает                          |
|--------|----------------------------------|-------------------------------------|
| GET    | `/api/cost/filter-options`        | каскадная фильтрация (12 фильтров)   |
| POST   | `/api/cost/aggregated`            | агрегаты GROUP BY на CostHistory     |
| POST   | `/api/cost/details`               | детализация по модели/артикулу        |
| GET    | `/api/cost/price-levels`          | справочник уровней цен (Gpartner)     |
| POST   | `/api/cost/save-changes`          | сохранить одно изменение (OLAP+аудит) |
| POST   | `/api/cost/save-batch`            | bulk-save изменений                   |

Все save-эндпоинты ожидают заголовок `X-Username` (фронт ставит из
основного `useAuth`). Если заголовка нет — пишется `system`.

## Команды Makefile

| Команда              | Что делает                                |
|----------------------|-------------------------------------------|
| `make cost-up`       | поднять контур (build + up)              |
| `make cost-down`     | остановить контур                        |
| `make cost-logs`     | tail логов всех контейнеров              |
| `make cost-ps`       | статус контейнеров                       |
| `make cost-psql`     | psql в БД cost                           |
| `make cost-console-py` | shell в python-cost                    |
| `make cost-reset-db` | ⚠ снести том `cost_db_data` и поднять заново |

## Как раздел встраивается в полный контур

Главный `nuxt/nuxt.config.ts` расширяется этим layer-ом
(`extends: ["../python/cost/nuxt-layer"]`), поэтому в полном
`docker-compose.dev.yml` `/cost` тоже появится — но запросы на
`/api/cost/*` обслуживает только `python-cost`. Если разработчик хочет
видеть раздел в составе всего проекта, нужно либо запустить и
`docker-compose.cost.yml` параллельно, либо позже добавить `python-cost`
в основной compose с проксированием nginx-фасадом.
