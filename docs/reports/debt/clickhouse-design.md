# ClickHouse как слой ускорения отчётов — проектная заметка

> Статус: **design draft v0.1**, 2026-05-23. Не реализация, обсуждение.
> Контекст: M1 + M2 debt-отчёта уже работают через `[FinDWH].[dbo].[Premaster1C]`
> на MSSQL (~203M строк). Вопрос — поможет ли ClickHouse и при каких условиях.

## 1. TL;DR

**Стоит ли заводить ClickHouse сейчас? — Нет, пока не появится один из триггеров:**

| Триггер | Как замерить |
|---|---|
| Свёртка отчёта в M1 на 5+ юрлицах × год идёт > 30 сек | замерить smoke-запрос после восстановления доступа к OLAP |
| Параллельных пользователей отчёта > 5 | по логам Go-API |
| Полная история (18 ЮЛ × 5 лет) — UI/ТЗ требует | от автора ТЗ |
| Нужны ageing-матрицы, тренды, top-N — то, чего нет в готовых витринах | продукт |

Сейчас (MVP) — **MSSQL хватает**, потому что:
- индексы `(CompanyID, Date)` и `(CompanyID, DocID)` точно покрывают наши SQL;
- свёртка — **один UNION ALL** с агрегатами по индексу;
- drill-down — по `(CompanyID, DocID)`, миллисекунды.

При этом **архитектурно мы готовы** к переезду:
- Интерфейс `PremasterRepo` (Report + Drilldown) уже разделяет данные и логику.
- Вся бизнес-логика (`chart_of_accounts`, `BuildReport`, `BuildDrilldown`, `parse_doc`) живёт в Go и не зависит от источника.
- Переезд на CH = новая реализация `PremasterRepo` (`repo_clickhouse.go`) + ETL-job. Менять остальное **не придётся**.

## 2. Что сейчас «болит» (или нет)

| Боль | Серьёзность | MSSQL покрывает? |
|---|---|---|
| Свёртка по 1 РФ-юрлицу × месяц | низкая | да, секунды |
| Свёртка по 5 ЮЛ × год | средняя | да, ожидаем ~10-30 сек (надо замерить) |
| Свёртка по 18 ЮЛ × 5 лет | потенциальная | вероятно — минуты, нужно проверить |
| Drill-down по DocID | низкая | да, мгновенно (`CLUSTERED (DocID, RwNm)`) |
| Параллельные пользователи (10+) | потенциальная | MSSQL шарится между всеми отчётами FinDWH — конкуренция с ETL |
| Зависимость от чужого ETL Premaster | средняя | MSSQL не закрывает, нужно мониторить `DateOfLoad`/`CompaniesMF.ClosedPeriod` |
| Ageing-матрицы (вне ТЗ задолженности) | средняя/высокая для других отчётов | плохо — нет колонок просрочки в Premaster, надо считать |

**Промежуточный вывод:** для debt-отчёта M1+M2 MSSQL — подходящий outlet. ClickHouse понадобится, когда добавятся аналитические отчёты, требующие быстрых агрегаций по всему фактовому объёму (например, тренды, top-N, ageing на лету).

## 3. Что инфраструктурно уже есть

В `docker compose -f docker-compose.dev.yml ps` мы видели:

```
swarm-clickhouse-1   clickhouse/clickhouse-server:24.3-alpine   Up 3 minutes
```

Это **orphan** от проекта MP, который шарит docker-compose namespace `swarm`. Контейнер живой. Можно либо:
- (a) **переиспользовать** этот же контейнер (создать в нём БД `finance`),
- (b) **добавить свой** в `swarm/docker-compose.dev.yml` (с другим именем тома, чтобы не было конфликта).

Для прод-Swarm — потребуется отдельная инсталляция (CH в Docker Swarm работает, есть проверенные паттерны). Но это разговор после MVP.

## 4. Что даст ClickHouse для нашего отчёта

### 4.1 Свёртка `Report`

| Аспект | MSSQL `Premaster1C` | ClickHouse `fact_premaster` |
|---|---|---|
| Объём | 203M строк, ~50-80 GB | 200M строк, ~3-8 GB (сжатие в 10-20×) |
| Хранение | row-store, B-tree индексы | column-store, sparse index по сортировочному ключу |
| Профиль `SELECT SUM, GROUP BY` | использует индекс по части ключей; UNION ALL Dr/Cr × WHERE × GROUP BY | по умолчанию хорош; SummingMergeTree даст уже свёрнутые остатки |
| Параллелизм | один запрос — один план; конкурирует с ETL за tempdb | column-block parallelism, до 8-32 потоков на запрос |
| Latency типовой свёртки | секунды-десятки секунд | миллисекунды-секунды для 200M |

Ожидаемое ускорение свёртки: **5-50×**. Заметно при > 5 ЮЛ × год периода.

### 4.2 Drill-down

| MSSQL | ClickHouse |
|---|---|
| `WHERE CompanyID=? AND DocID=?` по кластерному индексу `(DocID, RwNm)` → мгновенно. | По primary-key seek тоже хорошо, но MSSQL уже идеально подходит. |

Drill-down мигрировать **нет смысла**. Это OLTP-доступ, не аналитика. Оставляем MSSQL.

### 4.3 Будущие отчёты

CH открывает рантайм-возможности, которых MSSQL для нас не даёт:
- **Ageing-матрицы**: считать `dateDiff('day', date, today)` и группировать в корзины — на лету за миллисекунды.
- **Top-N**: топ-100 должников по объёму просрочки — секунды.
- **Тренды**: график «задолженность по дням за год» — мгновенно.
- **Cohort-анализ**: «как меняется средняя просрочка по поставщикам, появившимся в Q1 2025» — теоретически возможен.

Для debt-отчёта v1 ничего из этого не требуется. **Это аргумент в пользу CH, но не "must-have"**.

## 5. Архитектура: где CH встанет

```
   ┌─────────────────────────────────────────────────────────────────┐
   │  Finance-API (Go)                                                │
   │                                                                  │
   │     internal/reports/debt/                                       │
   │     ┌─────────┐    ┌─────────────────────────────────────┐      │
   │     │ Handler │ →  │ Service                              │      │
   │     │         │    │   ↓                                  │      │
   │     │         │    │ PremasterRepo (interface)           │      │
   │     └─────────┘    │   ├─ premasterRepo  (MSSQL)         │      │
   │                    │   └─ clickhouseRepo (CH) ← НОВЫЙ    │      │
   │                    │   wraps via ENV DEBT_BACKEND        │      │
   │                    └─────────────────────────────────────┘      │
   │                                                                  │
   │     internal/etl/premaster/ ← НОВЫЙ                              │
   │       ├─ cron-job (каждые 5-15 мин)                              │
   │       ├─ читает Premaster1C delta по DateOfChange > last_load    │
   │       └─ INSERT в CH fact_premaster                              │
   └─────────────────────────────────────────────────────────────────┘
              │                                       │
              │                                       │
              ▼                                       ▼
   ┌──────────────────────────┐         ┌──────────────────────────┐
   │  MSSQL (10.10.6.15)      │         │  ClickHouse              │
   │  FinDWH.dbo.Premaster1C  │ ───ETL─►│  finance.fact_premaster  │
   │  (203M, читаем)          │         │  (SummingMergeTree)      │
   │  + Objects, Counterparty,│         │  + dim_companies (Dict)  │
   │  + CompaniesMF (dim)     │         │  + dim_counterparty(Dict)│
   └──────────────────────────┘         └──────────────────────────┘

   Чтение:
     • Report (свёртка)    → clickhouseRepo (если CH доступен)
     • Drilldown (один док)→ premasterRepo (MSSQL индексирован)
     • Filter options      → seed.go (статика)
```

`DEBT_BACKEND=mssql` (по умолчанию) — текущее поведение M1+M2. `DEBT_BACKEND=ch` — переключение на CH когда тот наполнен.

## 6. Схема `fact_premaster` (ClickHouse)

```sql
CREATE TABLE finance.fact_premaster
(
    company_id       LowCardinality(String),     -- ИНН/УНП нашего юрлица
    counterparty_id  String,                     -- ИНН/УНП партнёра (NULL → '' через ifNull)
    date             Date,                       -- дата проводки
    date_time        DateTime,                   -- полная дата+время
    doc_id           String,                     -- стабильный ID документа 1С
    rw_nm            Int64,                      -- порядок строки в документе

    dr_acc           LowCardinality(String),     -- счёт дебета
    cr_acc           LowCardinality(String),     -- счёт кредита
    dr_acc_root      LowCardinality(String),     -- LEFT до точки — для group-by
    cr_acc_root      LowCardinality(String),

    amount_with_vat  Decimal(18, 2),
    amount_wo_vat    Decimal(18, 2),

    ico              UInt8,                      -- 1 = ВГО

    -- Денормализация: имена + страна + валюта по company_id уже здесь
    -- (для отчёта без дополнительных JOIN'ов; обновляется ETL'ом из CompaniesMF).
    country          LowCardinality(String),     -- BY / RU / KZ / UZ / TR / ...
    curr_id          UInt16,                     -- ID функциональной валюты юрлица
    company_name     LowCardinality(String),
    partner_name     String,                     -- из Counterparty или '' если нет

    -- Денормализация валюты — курс уже посчитан ETL'ом по date.
    rate_to_byn      Decimal(15, 8),             -- 1 для CurrID=1; иначе CurrencyDaily.curr_rate
    amount_byn       Decimal(18, 2) MATERIALIZED amount_with_vat * rate_to_byn,

    -- Имя документа из Objects (для drill-down без LEFT JOIN).
    doc_name1c       String DEFAULT '',

    -- Технические поля для ETL.
    date_of_change   DateTime,                   -- из Premaster1C; основа инкрементального ETL
    inserted_at      DateTime DEFAULT now()
)
ENGINE = ReplacingMergeTree(date_of_change)
PARTITION BY toYYYYMM(date)
ORDER BY (company_id, date, counterparty_id, dr_acc_root, cr_acc_root, doc_id, rw_nm)
SETTINGS index_granularity = 8192;
```

**Выбор движка:** `ReplacingMergeTree(date_of_change)` — потому что Premaster даёт UPDATE'ы (повторные правки одного `(DocID, RwNm)`). При мерже последняя версия (по `date_of_change`) победит. Для финального запроса нужно `FINAL` или `argMax(...) GROUP BY (DocID, RwNm)`.

**Альтернатива:** `SummingMergeTree` поверх свёрнутой витрины (по `(company_id, date, counterparty_id, acc_root)`) — сразу хранит свёрнутые суммы. Это быстрее для отчёта, но теряем строки документа → drill-down невозможен из CH. Выбор: основная таблица — Replacing (атомы), плюс **Materialized View** на её основе с SummingMergeTree для свёртки.

```sql
CREATE MATERIALIZED VIEW finance.fact_premaster_signed_mv
ENGINE = SummingMergeTree
PARTITION BY toYYYYMM(date)
ORDER BY (company_id, counterparty_id, acc_root, date)
AS
SELECT
    company_id, counterparty_id, dr_acc_root AS acc_root, date,
    amount_with_vat                            AS sum_dr_amt,
    toDecimal128(0, 2)                          AS sum_cr_amt
FROM finance.fact_premaster
UNION ALL
SELECT
    company_id, counterparty_id, cr_acc_root AS acc_root, date,
    toDecimal128(0, 2)                          AS sum_dr_amt,
    amount_with_vat                            AS sum_cr_amt
FROM finance.fact_premaster;
```

Финальный SELECT свёртки:
```sql
SELECT
    company_id, counterparty_id, acc_root,
    sumIf(sum_dr_amt - sum_cr_amt, date <  {date_from:Date}) AS opening,
    sumIf(sum_dr_amt - sum_cr_amt, date >= {date_from:Date}) AS turnover,
    sum(sum_dr_amt - sum_cr_amt)                              AS closing
FROM finance.fact_premaster_signed_mv
WHERE company_id IN {companies:Array(String)}
  AND date <= {date_to:Date}
GROUP BY company_id, counterparty_id, acc_root
HAVING abs(opening) + abs(turnover) > 0.005
```

Это **3-5 миллисекунд** на 200M строк против десятков секунд в MSSQL — даже без эмпирического замера такая разница ожидаема для column-store + pre-aggregation.

## 7. ETL: как наполнять CH из Premaster

### 7.1 Стратегия (рекомендуемая): инкрементальный pull по `DateOfChange`

```
Каждые 5-15 минут (cron в Go-API):
  1. last_change = SELECT max(date_of_change) FROM clickhouse.fact_premaster
  2. delta = SELECT * FROM mssql.Premaster1C WHERE DateOfChange > last_change
            LEFT JOIN CompaniesMF, Counterparty, Objects, CurrencyDaily
            -- те же JOIN'ы что в §8a.2 schema-draft
  3. INSERT INTO clickhouse.fact_premaster
  4. ReplacingMergeTree сам схлопнёт старые версии (DocID, RwNm) в фоне.
```

Плюсы: lag 5-15 мин, не упирается в полную загрузку.
Минусы: если ETL Premaster ретроактивно правит **давние** проводки и DateOfChange корректно меняет — норм; если DateOfChange не трогается — дрифт. Для проверки — раз в неделю **полный reconcile** (full reload + dedup).

### 7.2 Альтернатива: ночной full-reload

```
Раз в сутки в 03:00:
  1. CREATE TABLE fact_premaster_new (...)
  2. INSERT INTO fact_premaster_new SELECT * FROM mssql.Premaster1C JOIN ...
  3. EXCHANGE TABLES fact_premaster AND fact_premaster_new
  4. DROP TABLE fact_premaster_new
```

Плюсы: гарантированно консистентно. Минусы: lag 24ч; для debt-отчёта обычно приемлемо.

### 7.3 Где живёт ETL

В Go-API как goroutine + cron-планировщик (`robfig/cron` или собственный `time.Ticker`). Преимущество: переиспользует те же DB-пулы, не нужен отдельный сервис.

Файлы:
```
go/internal/etl/premaster/
├── service.go     ← оркестратор: cron + run-once
├── source.go      ← чтение delta из MSSQL
├── sink.go        ← INSERT в CH через clickhouse-go/v2
└── checkpoint.go  ← хранение last_change в Postgres (debt_etl_checkpoint)
```

## 8. Что меняется в Go-коде при переезде

Минимально, потому что `PremasterRepo` уже изолирует данные от логики.

### Новые файлы
- `go/internal/reports/debt/repo_clickhouse.go` — реализация интерфейса для CH. Тот же `Report`/`Drilldown` контракт.
- `go/internal/etl/premaster/*` — отдельный pluggable пакет.

### Изменения в `main.go`
```go
var debtRepo debt.PremasterRepo
switch os.Getenv("DEBT_BACKEND") {
case "ch":
    chDB := chx.Open(cfg.ClickHouseDSN)
    debtRepo = debt.NewClickHouseRepo(chDB)
case "mssql", "":
    mssqlDB, _ := debt.NewPremasterRepo(...)
    debtRepo = debt.WrapPremasterRepo(mssqlDB)
}
```

### Что НЕ меняется
- `chart_of_accounts.go` — классификация счетов
- `BuildReport`/`BuildDrilldown`/`ResolveDoc` — вся бизнес-логика
- `seed.go`, `model.go`, `handler.go`, `mocks.go`
- Unit-тесты (34 теста) — продолжают работать

Это и есть главный технический аргумент: **гибридный режим** возможен (drill-down → MSSQL, свёртка → CH) почти бесплатно — просто две разные реализации `PremasterRepo`. Можно даже сделать `compositeRepo`, который роутит методы.

## 9. Затраты и риски

### Затраты (одноразовые)

| Что | Объём |
|---|---|
| Дизайн + согласование | сделано в этом документе + 1 встреча |
| Реализация `repo_clickhouse.go` | ~200 строк Go + тесты |
| ETL pipeline | ~400 строк Go + тесты |
| CH схема + materialized views | ~50 строк SQL |
| Сверка с MSSQL (reconcile-команда) | ~100 строк Go |
| Docker-compose / Swarm | ~30 строк YAML |
| **Итого** | ~1-2 недели одного разработчика |

### Затраты (постоянные)

| Что | Объём |
|---|---|
| Память CH | 2-4 GB резерв + 5-15 GB диска |
| Поддержка ETL (мониторинг lag, reconcile) | ~1-2 ч в месяц |

### Риски

| Риск | Митигация |
|---|---|
| Дрифт CH vs MSSQL (ETL пропустил UPDATE) | Еженедельный reconcile-job, checksum по `(date_of_change), count(*)` за период |
| `DateOfChange` не обновляется при ретро-правках в 1С | Полный reload раз в N дней |
| CH-кластер падает | Fallback `DEBT_BACKEND=mssql` через ENV; деградация UX, но не отказ |
| Lag ETL > N минут | Метрика `etl_lag_seconds` → алёрт через [notifications](../../python/cost/AGENTS.md#notify) |
| Разработчик отчётов потерял знание о двух источниках | Зафиксировать архитектуру в CLAUDE.md и в этом документе |

## 10. Альтернативы (рассмотрены и отброшены)

| Альтернатива | Почему нет |
|---|---|
| **Materialized view в самом MSSQL** | MSSQL `INDEXED VIEW` имеет жёсткие ограничения (`COUNT_BIG`, `SCHEMABINDING`, нет UNION ALL после агрегатов). Прирост скорости — 2-3×, не порядки. |
| **Materialized view в нашем Postgres (`finance` DB)** | Postgres — row-store, агрегаты по 200M строк — те же часы. Если использовать `cstore_fdw` / `citus` — это уже отдельный extension. |
| **Кеш в Redis** | Кеш помогает повторным запросам, но не первому. UI работает с произвольными комбинациями фильтров — кеш-промахов будет много. |
| **DuckDB embedded** | Хорошо для одиночных аналитических запросов, но не для concurrent multi-user. И не share-able между инстансами Go-API в Swarm. |
| **Прямо `[Payments].[report].[FinDebt1]`** | Это уже агрегат аналитиков. Зависим от их ETL и от их интерпретации (см. § 8.1 schema-draft). |

## 11. Рекомендация

### Сейчас (M1+M2 done)
- **Не делать.** Замерить smoke по реальной БД, оценить latency.
- Зафиксировать `DEBT_BACKEND` ENV в коде (default `mssql`) — это _zero-cost_ подготовка к будущему переключению.

### Триггеры перехода на CH
- Свёртка `Report` стабильно > 30 секунд на типичном фильтре;
- Параллельных пользователей debt-отчёта > 5;
- Появилась задача аналитики, требующая `Top-N`, `тренды`, `ageing на лету` (вне готовых витрин);
- Решили **не использовать** готовые витрины `FinancialReportСounterparties`/`CubeBadDebt` (см. § 8.1 schema-draft).

### Когда триггер сработал
1. **M6** (новый milestone): добавить `ClickHouseDSN` в config, поднять CH-контейнер.
2. **M7**: схема `fact_premaster` + MV + одна команда-загрузчик (`make debt-etl-bootstrap` — полный reload).
3. **M8**: cron-инкрементальный ETL + reconcile-job + метрики.
4. **M9**: `repo_clickhouse.go` за интерфейсом `PremasterRepo`, переключение через ENV.
5. **M10**: композитный режим (свёртка → CH, drill-down → MSSQL) или полный переход.

### Бонус: ClickHouse открывает новые отчёты

Когда CH будет на месте — это **общий фундамент** для финансовой аналитики. Поверх `fact_premaster` сразу можно сделать:
- Отчёт по выручке по магазинам/каналам (Department × GroupPL × Month) — секунды вместо минут;
- Cash-flow прогноз на основе истории платежей;
- Cohort-анализ контрагентов;
- Аномалии (drift detection по amount distribution).

Это **не для debt-отчёта** напрямую, но один шаг (поднять CH) даёт фундамент для целого слоя финансовой аналитики кабинета.

## 12. Что ставим в backlog сейчас

- **M6 (заведомо отложен):** `DEBT_BACKEND` ENV, no-op заглушка `clickhouseRepo` возвращающая `not implemented` — чтобы wiring был готов и переключение по флагу делалось одним deploy'ем без code-change.
- **Документация:** этот файл (`docs/reports/debt/clickhouse-design.md`).
- **Метрика для триггера перехода:** залогировать latency `repo.Report()` через `slog`/`prometheus` (в идеале — `debt_report_duration_seconds` гистограмма).

Если автор ТЗ скажет «debt будет использовать 10+ менеджеров» или «нужен реал-тайм top-100 должников» — поднимаем CH без откладывания.
