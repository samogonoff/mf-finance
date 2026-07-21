# Runbook: отчёт «Задолженность ВГО» на источнике FinDebt

> Единственный источник отчёта — готовый расчётный слой FinDebt (`[Payments].
> [report].[FinDebt3]`), сверенный с 1С копейка-в-копейку
> (`findebt-verification.md`). Поток: **MSSQL FinDebt3 → CH `finance.fact_findebt`
> → отчёт**. Старые бэкенды (Premaster/GLMF/finpl) выпилены.

## Архитектура (что где)

| Слой | Где | Что |
|---|---|---|
| Источник | `[Payments].[report].[FinDebt3]` на OLAP 10.10.6.15 | суточный снэпшот ДЗ/КЗ на уровне документа/договора, со сроком/просрочкой |
| ETL | `cmd/findebt-etl` (бинарь `/findebt-etl` в образе go-api) | заливка FinDebt3 → CH |
| Витрина | CH `finance.fact_findebt` (миграция `clickhouse/migrations/008`) | документное зерно, ВГО, BYN |
| Отчёт | `DEBT_BACKEND=findebt` → `repo_findebt_ch.go` | группировка до договора; drill-down → документы |
| Инкремент | in-process воркер go-api (`FINDEBT_SYNC_INTERVAL`) | докатывает новые снэпшоты по расписанию |

## Prod: переменные окружения (go-api)

```
DEBT_MOCK=0
DEBT_BACKEND=findebt
MSSQL_PREMASTER_SERVER=10.10.6.15      # OLAP (тот же сервер, где Payments)
MSSQL_PREMASTER_PORT=1433
MSSQL_PREMASTER_DB=FinDWH
MSSQL_PREMASTER_USER=...
MSSQL_PREMASTER_PASSWORD=...
MSSQL_PAYMENTS_DB=Payments             # БД с вьюхами FinDebt (схема report)
MSSQL_FINDEBT_SCHEMA=report
MSSQL_FINDEBT1_TABLE=FinDebt1          # зарезервировано, extract использует FinDebt3
MSSQL_FINDEBT3_TABLE=FinDebt3
CLICKHOUSE_HTTP_URL=http://<ch-host>:8123
CLICKHOUSE_USER=finance
CLICKHOUSE_PASSWORD=...
FINDEBT_SYNC_INTERVAL=21600            # 6 ч (вьюхи суточные); 0 = воркер выключен
```

## Порядок выката

### 1. Миграции CH — автоматически в CI
Джоб `migrations` катит `clickhouse/migrations/008_fact_findebt.up.sql` через
`swarm/migrate-clickhouse.sh` (нужны `CLICKHOUSE_HTTP_URL/USER/PASSWORD`).
Идемпотентно (`CREATE ... IF NOT EXISTS`). Проверить после деплоя:

```sh
curl -s "$CLICKHOUSE_HTTP_URL/?user=$CLICKHOUSE_USER&password=$CLICKHOUSE_PASSWORD" \
  --data-binary "EXISTS finance.fact_findebt"        # → 1
```

> ⚠ Если на CH остались СТАРЫЕ таблицы (`fact_premaster`/`fact_glmf`/`dim_contract`/
> `etl_run_log`) от прежних бэкендов — они больше не наполняются, дропнуть вручную:
> `DROP TABLE IF EXISTS finance.fact_premaster` (и аналогично остальные).

### 2. Первичная заливка (bootstrap) — один раз, вручную
Найти запущенный таск go-api и выполнить bootstrap внутри него.

⚠ Креды (`MSSQL_PREMASTER_*`, `CLICKHOUSE_*`, …) приходят из `docker config`,
смонтированного как `/etc/api.env`, и сорсятся ТОЛЬКО в `entrypoint.sh` для PID 1.
`docker exec` стартует свежий процесс с базовым env образа — этих переменных там
НЕТ. Поэтому сорсим `/etc/api.env` сами, как это делает entrypoint:

```sh
TASK=$(docker service ps --no-trunc --filter desired-state=running \
  --format '{{.Name}}.{{.ID}}' finance_go-api | head -1)

# Полная история с 2021 (~2-3 мин; FinDebt3 читается с OLAP).
# MODE/FINDEBT_MIN_DATE идут через -e и переживают source (в api.env их нет).
docker exec -e MODE=bootstrap -e FINDEBT_MIN_DATE=2021-01-01 "$TASK" \
  sh -c 'set -a; . /etc/api.env; set +a; exec /findebt-etl'
```

Если уже зашёл внутрь контейнера (`docker exec -it "$TASK" sh`) — env там пуст,
грузим его сами прямо в shell, потом запускаем (бинарь в корне `/findebt-etl`):

```sh
set -a; . /etc/api.env; set +a; MODE=bootstrap FINDEBT_MIN_DATE=2021-01-01 /findebt-etl
```

Ожидаемый лог: `bootstrap-findebt DONE rows=<N>`.

### 3. Инкремент — автоматически воркером
При `FINDEBT_SYNC_INTERVAL>0` go-api сам докатывает новые снэпшоты (первый прогон
через минуту после старта, далее каждые N секунд). Воркер stateless: берёт
watermark (последний `snapshot_date`) из CH.

Ручной прогон при необходимости:
```sh
docker exec -e MODE=incremental "$TASK" /findebt-etl
```

## Проверка

```sh
# 1) объём и последний снэпшот
curl -s "$CH" --data-binary \
  "SELECT count(), max(snapshot_date) FROM finance.fact_findebt"

# 2) контрольная пара МФ→Формэль (должно быть ДЗ 866791.74 / КЗ 53196999.09 на 2026-05-31)
curl -s "$CH" --data-binary \
  "SELECT toString(sum(sum_d_byn)), toString(-sum(sum_k_byn)) \
   FROM finance.fact_findebt FINAL \
   WHERE snapshot_date='2026-05-31' AND company_id='690591512' AND counterparty_id='690719790'"
```

Затем открыть отчёт в кабинете (роль `ROLE_FINANCE_ADMIN`), проверить договоры и
колонки «Отсрочка/Дата оплаты/Просрочка».

## Траблшутинг

- **`findebt-ch: status ... CLICKHOUSE_HTTP_URL/USER not set`** при старте go-api →
  задать `CLICKHOUSE_*`. Без CH отчёт в live-режиме не поднимется.
- **Отчёт пуст, а CH не пуст** → проверить, что bootstrap залил нужный период
  (`min(snapshot_date)`), и что `DEBT_BACKEND=findebt` (не `findebt-live`).
- **Нужен фолбэк без CH** → `DEBT_BACKEND=findebt-live`: отчёт читает FinDebt-вьюхи
  из MSSQL напрямую (медленнее, зависит от живого OLAP; drill-down/договоры — как в
  свод-режиме, без документного разреза). Только на время инцидента с CH.
- **Просрочка/дата оплаты пусты у части договоров** → это данные источника
  (`Payments.Docs` срок заведён примерно у половины документов), не баг.
