# Finance · Cost — AGENTS.md

> ⚠ **Scope constraint**: Работаем **только** в `python/cost/`. Редактирование любых файлов за пределами этой директории — строжайший запрет.

## Quick start

```bash
cp .env.example .env          # fill MSSQL/OLAP creds (или COST_MOCK=1)
# Isolated contour (cost-only):
cd swarm && make cost-up      # postgres-cost + python-cost + nuxt-cost + nginx-cost
make cost-logs                # tail all cost services
```

| Service | Cost contour URL | Direct port |
|---------|------------------|-------------|
| FastAPI | http://localhost:8091 | `:8091` |
| Nuxt (cost-only) | http://localhost:3002 | `:3002` |
| Nginx (cost) | http://localhost:8088 | `:8088` |
| Postgres-cost | `psql -h localhost -p 55433 -U cost cost` | `:55433` |

В основном дев-контуре (`cd swarm && make up`) эндпоинты `/api/cost/*` доступны через `api.finance.local/api/cost/` (nginx проксирует на `python-cost:8091`).

## Структура

```
python/cost/
├── app/
│   ├── __init__.py       # пустой
│   ├── main.py           # FastAPI app, lifespan (init/close pool), CORS, /healthz
│   ├── routes.py         # все эндпоинты раздела
│   ├── db.py             # asyncpg (postgres-cost) + pyodbc (MSSQL/OLAP)
│   └── mocks.py          # заглушки для COST_MOCK=1
├── migrations/
│   └── 0001_cost_init.{up,down}.sql
├── nuxt-layer/
│   ├── nuxt.config.ts
│   ├── pages/cost/index.vue   # SPA-страница «Себестоимость»
│   ├── components/CostMultiSelect.vue
│   ├── middleware/cost-redirect.global.ts
│   └── plugins/cost-bypass.ts
├── Dockerfile.dev         # python:3.12-slim-bookworm + msodbcsql18
├── requirements.txt       # fastapi, uvicorn, asyncpg, pyodbc, pydantic, python-dotenv
├── init-db.sh             # entrypoint для postgres-cost (применяет миграции)
├── .env.example           # шаблон с комментариями
├── .env                   # реальные креды (в gitignore)
└── AGENTS.md              # этот файл
```

## FastAPI endpoints (`app/`)

Все эндпоинты зарегистрированы с префиксом `COST_API_PREFIX` (по умолчанию `/api/cost`).

| Метод | Путь | Описание | Источник данных |
|-------|------|----------|-----------------|
| `GET` | `/filter-options` | Каскадные значения фильтров (DWH) | OLAP `[DWH].[dim].[groups]` |
| `POST` | `/load-data` | Сырые данные с пагинацией (limit/offset) | MSSQL `[Checks].[CostHistory]` |
| `POST` | `/aggregated` | GROUP BY с AVG/SUM по CostHistory | MSSQL `[Checks].[CostHistory]` |
| `POST` | `/details` | Детализация по модели с GROUP BY | MSSQL `[Checks].[CostHistory]` |
| `GET` | `/price-levels` | Справочник уровней цен | MSSQL `[Gpartner].[s_price_level]` |
| `POST` | `/save-changes` | Сохранить одно изменение цены | OLAP `[FinSandBox].[CostHistory_Changes]` + локальный аудит |
| `POST` | `/save-batch` | Массовое сохранение | OLAP `[FinSandBox].[CostHistory_Changes]` + локальный аудит |
| `GET` | `/healthz` | Health check | — |

### Детали эндпоинтов

#### `GET /filter-options`
- **Новый источник**: OLAP `[DWH].[dim].[groups]` (справочник групп), а не CostHistory.
- Фильтры: `brand_manager` (из `BRAND_FIO`), `level01`–`level05` (из `gr1-gr5`/`group1-group5`), `calc_sign` (статический).
- `level01`–`level05` возвращают `[{id, text}]` для Select2-совместимости.
- `brand_manager` и `calc_sign` — плоские `string[]`.
- `calc_sign` статический: `["ПКПСС", "КПСС", "ПФКСС", "ФКСС"]`.
- Убраны из фильтров: `country`, `family`, `season`, `model`, `articul`.
- Каскад: полный двусторонний между brand_manager и Level01-05.
- Параметры: query params (ключи фильтров, `id` значения для уровней).

#### `POST /load-data`
- Новый эндпоинт для сырых данных с пагинацией.
- Body: `{ date_from, date_to, brand_manager: [...], level01: [...], ..., limit, offset }`
- `limit` макс. 5000, по умолчанию 1000.
- Возвращает: `{ data: [...], count, total, offset, limit }`.

#### `POST /aggregated`
- GROUP BY: `Бренд-менеджер, Модель, Артикул, Признак калькуляции, дата расчета, Уровень цен, Страна пр-ва, Семья, Сезон`.
- AVG-поля: `Розничная цена по уровню, руб.`, `Отпускная цена по уровню, руб`, `Розничная цена по уровню, USD.`, `Отпускная цена по уровню, USD.`, `Пошив, руб.`, `Пошив, USD.`, `Раскрой, руб.`, `Раскрой, USD.`
- SUM-поля: `Основные материалы, руб.`, `Основные материалы, USD.`, `Вспомогательные материалы, руб.`, `Вспомогательные материалы, USD.`, `Декоры, руб.`, `Декоры, USD.`, `Себестоимость, руб.`, `Себестоимость, USD.`
- Фильтры: все 12 полей (level01-05, brand_manager, country, family, season, calc_sign, model, articul).

#### `POST /details`
- **Изменён**: принимает `model` (строка), не `model + articul`.
- Body: `{ model, date_from?, date_to?, calc_sign? }`
- Делает GROUP BY по `дата расчета, Признак калькуляции, Модель, Артикул, Наименование модели, Номер задания производства`.
- Серверный расчёт: `Себестоимость, руб.` (сумма статей), `Наценка, руб.`, `Наценка, %`, `Маржинальность, %`.
- Новые поля: `Номер задания производства`, `Вязание, руб.`, `Декор, руб.`.

#### `POST /save-changes` и `/save-batch`
- Пишут в OLAP `[FinSandBox].[dbo].[CostHistory_Changes]` и дублируют в локальный `cost_price_changes_audit` (postgres-cost).
- В mock-режиме — только локальный аудит.

## Базы данных (`app/db.py`)

### postgres-cost (asyncpg)
- Пул: `COST_DATABASE_URL`, min_size=1, max_size=8
- Инициализируется в `lifespan`, закрывается при shutdown
- Таблица: `cost_price_changes_audit` (аудит изменений)

### MSSQL Checks (pyodbc, read-only)
- `get_mssql_conn()` — чтение `[Checks].[CostHistory]` (~13M строк)
- `get_gpartner_conn()` — чтение `[Gpartner].[s_price_level]`

### MSSQL OLAP (pyodbc, write)
- `get_olap_conn()` — запись в `[FinSandBox].[CostHistory_Changes]`

### Драйвер
- `MSSQL_DRIVER` (по умолчанию `{ODBC Driver 18 for SQL Server}`)
- Все pyodbc-коннекты синхронные, FastAPI запускает их в threadpool
- Параметры: `TrustServerCertificate=yes; Encrypt=optional;`

### Источники данных (extern)

| Source | Host | DB | Доступ |
|--------|------|----|--------|
| MSSQL | `10.10.6.107` | `Checks` (CostHistory) | RO |
| MSSQL | `10.10.6.107` | `Gpartner` (price levels) | RO |
| MSSQL (OLAP) | `10.10.6.15` | `FinSandBox` (changes receiver) | RW |

## Mock-режим

`COST_MOCK=1` в `.env` — все эндпоинты отдают данные из `app/mocks.py` вместо MSSQL/OLAP.
- Фильтры: статический словарь `FILTER_OPTIONS` с 14 моделями и 15 артикулами
- Агрегаты: 136 сгенерированных строк
- Детали: 2 варианта на модель/артикул
- Уровни цен: 5 предопределённых уровней
- Сохранение: пишет только в локальный postgres-cost (аудит)

Нужен, когда у разработчика нет VPN до `10.10.6.107/15`.

## Nuxt-layer (`nuxt-layer/`)

Слой подключается через `extends: ['../python/cost/nuxt-layer']` в основном `nuxt.config.ts`.

### Страница `/cost` (`pages/cost/index.vue`)
Single-page приложение (template + script + scoped styles):
- **Шапка**: заголовок «Себестоимость», индикатор MOCK-режима
- **Фильтры**: 12 каскадных мультиселектов (CostMultiSelect), период дат, кнопки «Сбросить» / «Загрузить данные»
- **Action bar**: инфо о количестве записей, кнопки «Экспорт в Excel» и «Сохранить изменения»
- **Error banner**: показывается при ошибках API, с подсказкой про COST_MOCK=1
- **Таблица (агрегаты)**: 24 колонки + 🔍, пагинация по 50 строк, выбор уровня цен (dropdown)
- **Модальное окно детализации**: открывается по 🔍, с фильтрами дат/признака калькуляции, каскадными фильтрами колонок, таблицей 17 колонок, кнопкой «Открыть в новом окне»
- **Excel export**: генерирует HTML-таблицу → Blob → скачивание `.xls`
- **Ctrl+C**: копирует всю таблицу как TSV (если нет выделения)

### Хидеры запросов
- `apiBase` — пустая строка если `costOnly`, иначе `config.public.apiBase`

### Файлы слоя
- `nuxt.config.ts` — пустой (`export default defineNuxtConfig({})`)
- `CostMultiSelect.vue` — мультиселект компонент для фильтров
- `cost-redirect.global.ts` — middleware редиректа
- `cost-bypass.ts` — плагин для cost-only режима

## Миграции (`migrations/`)

- `0001_cost_init.up.sql` — создаёт таблицу `cost_price_changes_audit` (id BIGSERIAL, model, articul, price_level, retail_rub, wholesale_rub, username, changed_at)
- `init-db.sh` — запускается postgres-cost при первом старте: применяет все `*.up.sql` из `migrations/` через `psql`

## Dockerfile (`Dockerfile.dev`)

- Base: `python:3.12-slim-bookworm` (фиксирован, т.к. `-slim` уехал на trixie где строже keyring path)
- Устанавливает: `msodbcsql18` (ODBC для SQL Server), unixodbc, gcc/g++ (для сборки pyodbc)
- Копирует `requirements.txt`, устанавливает зависимости
- `CMD`: `uvicorn app.main:app --host 0.0.0.0 --port 8091 --reload --reload-dir /var/www/cost/app`
- `--reload-dir` сужен до `app/` — uvicorn не дёргается на изменения в `nuxt-layer/`

## Зависимости (`requirements.txt`)

```
fastapi==0.115.0
uvicorn[standard]==0.30.6
asyncpg==0.29.0
pydantic==2.9.2
pyodbc==5.1.0
python-dotenv==1.0.1
```

## Известные пробелы

- **Нет тестов.** Ни одного теста в `python/cost/`.
- **Нет линтинга/форматирования.** Ни ruff, ни black, ни mypy.
- **Нет схем Pydantic.** Роуты принимают `dict` вместо моделей — нет валидации на уровне типов.
- **sync DB вызовы в async FastAPI.** pyodbc синхронный, FastAPI запускает в threadpool — но при ошибках соединения нет таймаутов и retry-логики.
- **Ошибки соединения с MSSQL/OLAP.** Если сервер недоступен, endpoint падает с 500 без внятного сообщения.
- **Нет пула для pyodbc.** Каждый запрос открывает новый коннект к MSSQL/OLAP.
- **Жёстко зашитые SQL запросы.** Все в `routes.py`, нет query builder или ORM.
- **Excel export.** Генерирует HTML, не настоящий `.xlsx`.

