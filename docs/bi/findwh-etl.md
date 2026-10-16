# SPEC — реплика FinDWH → ClickHouse для аналитиков (GLMF, Table_Fin_PL, ExRates)

> Статус: реализовано (MR !142): миграция `clickhouse/migrations/012_findwh`,
> код `go/internal/etl/findwh*.go`, воркер go-api (`FINDWH_SYNC_INTERVAL`),
> CLI `cmd/findebt-etl MODE=findwh`. Эксплуатация — §9.
> Зачем: аналитикам нужен быстрый доступ к факту/плану FinDWH из Superset,
> не нагружая OLAP-сервер `10.10.6.15` и не завися от VPN.

## 1. Решения заказчика

| Вопрос | Решение |
|---|---|
| Тексты `TransDescription` / `OperationDescription` | **нужны** — переносим все колонки GLMF |
| Глубина истории GLMF | **с 01.01.2025**, раньше не нужно |
| Свежесть Table_Fin_PL | **раз в день** (30-минутный цикл блоков 4–7 не повторяем) |
| ExRates | ежедневно, все страновые таблицы |

## 2. Источники (замер 13–15.10.2026)

Все — `[FinDWH].[dbo]` на OLAP `10.10.6.15`.

| Таблица | Строк | Размер MSSQL | Наполнение | Ключ строки |
|---|---|---|---|---|
| `ExRates`, `ExRatesRU/KZ/UZ/TR` (`ExRatesBY` пуста) | ~3,9 тыс. каждая | 0,2 МБ | `spExRates*`: TRUNCATE+INSERT из `[srv-sql].Gpartner.dbo.valuta1`, **только USD** (`VALUTA_ID=3`) | нет (`DATA`,`VALUTA_ID`) |
| `GLMF` | 233 млн всего, **~117 млн с 2025** | 117 ГБ (row compression) | `GLMF_exec`: **полная пересборка** из Premaster1C в `GLMF_load` + `ALTER TABLE … SWITCH` | **нет**: `Num` — identity, новый после каждой пересборки |
| `Table_Fin_PL` | 2,9 млн | 2,8 ГБ | `Update_Table_Fin_PL`: TRUNCATE+INSERT в транзакции; `…_Refresh`: DELETE+INSERT блоков 4–7 | нет (куча, индекс только `BlockNum`) |

### Расписание (SQL Agent, по истории `msdb.dbo.sysjobhistory`)

```
06:00  job «FinDWH fullbackup»
       ├ step 1  spExRates*                      ~3 мин
       ├ step 2  GLMF_exec                       ~1 ч 50 мин  → GLMF готова ≈ 07:55
       ├ step 3  start «FinDWH Update_Table_Fin_PL»
       │           └ Update_Table_Fin_PL         ~35 мин      → Table_Fin_PL готова ≈ 08:30
       └ steps 4–7 …, full backup                             → ≈ 09:45
09:30–23:30  «Update_Table_Fin_PL_Refresh» каждые 30 мин (блоки 4–7), 0,5–11 мин
21:33  «FinDWH rebuild» (обслуживание индексов)
```

**Окно чтения для ETL: старт не раньше 09:00** (GLMF и Table_Fin_PL готовы,
бэкап закончен), закончить до 06:00. Точку готовности проверяем по истории
джоба, а не по часам: последний успешный `step_id=2` джоба `FinDWH fullbackup`
за сегодня.

### GLMF — что важно для методики

- Структура строки: 48 колонок (см. `INFORMATION_SCHEMA.COLUMNS`), в т.ч.
  `DocID` (1С-ссылка), `Date` (datetime), `Month`, счета, CodePL/GroupPL/CodeCFO,
  суммы в валюте и BYN, `ICO`, `DateOfLoad`, 8 id субконто.
- Кластерный индекс `(Company, DrAcc, CrAcc, Date)`. Seek возможен **только по
  `Company`** (код: MF, TDMF, MFT, MFUz, MFKaz, F, MFC, MFTex, PTIR, DR, MFZ, BR),
  дата внутри компании — уже скан.
- Ретро-изменения: за сутки перезаливается 0,5–2,8 млн строк в 2–52 парах
  «компания×месяц», задним числом до 2022 г.; внутри одной пары до 44 разных
  `DateOfLoad` — Premaster перезаливает кусками, не целыми периодами.
- Классификация (`[002 CodePL]`, `[001 CodeCFO]`, маппинги BK) применяется
  заново ко всей истории при каждой пересборке.
- Встречаются строки с будущими датами (MFKaz: 2026-11, 2026-12).

**Следствие:** инкремент по `DateOfLoad` невозможен — он не видит удалений,
сторно и переклассификации, а ключа для upsert нет.

## 3. Целевая схема ClickHouse

Таблицы **`finance.findwh_*`** — сырая реплика «как в источнике», без бизнес-логики,
рядом с отчётными витринами ВГО. Аналитикам — read-only роль на эти таблицы (§9).

Отдельная БД `findwh` была в первой версии, но прод-пользователь CH имеет права
только на `finance.*`: `CREATE DATABASE findwh` падал с `ACCESS_DENIED`, и CI-джоб
`migrations` ронял деплой. Префикс в общей БД не требует от админов ничего.

| Таблица CH | Движок / партиции | ORDER BY |
|---|---|---|
| `finance.findwh_glmf` | `MergeTree`, `PARTITION BY (company, toYYYYMM(date))` | `(company, date, dr_acc, cr_acc)` |
| `finance.findwh_glmf_day_fp` | `ReplacingMergeTree(checked_at)` — отпечатки дней | `(company, date)` |
| `finance.findwh_table_fin_pl` | `MergeTree`, без партиций | `(scenario, month, country, code_pl)` |
| `finance.findwh_ex_rates` | `MergeTree`, колонка `country` ('', RU, KZ, UZ, TR) | `(country, valuta_id, data)` |
| `finance.findwh_etl_runs` | журнал прогонов (таблица, строк, длительность, статус) | `(table, started_at)` |

- Колонки GLMF — все 48, snake_case, `LowCardinality(String)` для справочных
  (счета, коды, группы, страна, ответственные), `Decimal(18,2)` для сумм,
  `Float64` для `ExRate`, `DateTime` для `date`. Плюс `month`, `date_of_load`,
  `loaded_at`. Кириллические колонки Table_Fin_PL (`Компания`, `Корр-ка`, `ВГО`,
  `Блок`, `Метод.нюансы`, `КлючСвязи`, `Для отчета…`) — транслитерируются,
  соответствие фиксируется в миграции комментариями.
- ~12 компаний × ~24 месяца ≈ 300 партиций GLMF (+12 в год) — в норме для CH.

## 4. Методика загрузки

### 4.1 ExRates и Table_Fin_PL — полный снимок

1. Залить в `finance.findwh_<t>__new` (CREATE AS `<t>`), сверить `count()` и `sum(...)` с источником.
2. `EXCHANGE TABLES finance.findwh_<t>__new AND finance.findwh_<t>` — атомарная подмена,
   аналитики не видят пустую/полузалитую таблицу. Старую версию дропнуть.

Table_Fin_PL — 2,9 млн строк, ExRates — ~20 тыс.; раз в день после 09:00.

### 4.2 GLMF — сверка по отпечаткам дней + пересборка партиций в CH

```
MSSQL (на сервере, ~5 мин скана)               ClickHouse
──────────────────────────────────            ─────────────────────────────
GROUP BY Company, CAST(Date AS date)   ─┐
  COUNT_BIG(*), SUM(суммы),              │  ~8 тыс. строк по сети
  CHECKSUM_AGG(BINARY_CHECKSUM(...))     ├──► сравнить с glmf_day_fp
WHERE Date >= '20250101'                ─┘        │
                                                  ▼ «грязные» дни (company, date)
SELECT * … WHERE Company=@c                  для каждой затронутой партиции
  AND Date >= @d AND Date < @d+1      ───►   (company, месяц):
  (только грязные дни)                         staging ← старые строки партиции
                                                         кроме грязных дней
                                               staging ← свежие строки грязных дней
                                               REPLACE PARTITION … FROM staging
                                             glmf_day_fp ← новые отпечатки
```

1. **Отпечаток дня** считается целиком на стороне MSSQL: по сети уходит ~8 тыс.
   строк (12 компаний × ~650 дней). Замер: вся история MFKaz (~15 млн строк) —
   20 с; весь GLMF — порядка 5 мин скана на сервере.
   В `BINARY_CHECKSUM` входят: `DocID, DrAcc, CrAcc, Date, CodePL, CodeCFO,
   GroupPL, Expense, CounterpartyID, AmountWithVATBelRubFact,
   AmountWOVATBelRubFact, ICO` (+ остальные классификационные поля по итогам
   проверки стабильности, §7.1). `Num` и `DateOfLoad` — **не** входят.
2. **Грязный день** = отпечаток отличается, или день есть только с одной стороны
   (пропал в источнике → удаляем в CH; новый → догружаем).
3. **Замена атомарна на уровне партиции**: партиция собирается в staging из
   уже лежащих в CH чистых дней (локальная копия, дёшево) и свежих строк грязных
   дней, затем `ALTER TABLE … REPLACE PARTITION`. По сети едут только грязные дни.
4. Ожидаемый ежедневный объём по сети — 1–3 млн строк (по распределению
   `DateOfLoad`). Если грязных дней > N % (глобальная смена маппинга) — это
   фактически полная перезагрузка; ETL всё равно её делает, но пишет warn.
5. **Начальная загрузка** — тем же механизмом (в CH пусто → все дни грязные),
   по компаниям, начиная с мелких; прерванная загрузка продолжается сама.

### 4.3 Где живёт код

- `findwh.go` — колонки, потоковая запись в CH, журнал `etl_runs`, готовность
  источника, `RunFinDWHDaily`;
- `findwh_snapshot.go` — Table_Fin_PL и ExRates (снимок + `EXCHANGE TABLES`);
- `findwh_glmf.go` — отпечатки, грязные дни, пачки, сборка секций, сверка;
- `findwh_worker.go` — воркер go-api, свой MSSQL-пул на 2 соединения (поток
  GLMF держит соединение часами и не должен занимать общий OLAP-пул на 4).

Воркер тикает раз в `FINDWH_SYNC_INTERVAL`; на каждом тике для каждой таблицы:
есть успешный прогон за сегодняшнюю дату сервера MSSQL → пропуск; шаг SQL Agent
за сегодня ещё не прошёл → пропуск; иначе загрузка и строка в `etl_runs`.
Упавшая таблица повторится на следующем тике.

## 5. Объёмы и сроки

| | GLMF (с 2025) | Table_Fin_PL | ExRates |
|---|---|---|---|
| Строк | ~117 млн, +~6 млн/мес | 2,9 млн | ~20 тыс. |
| Размер в CH | **~4 ГБ** (замер: 33 Б/строку со всеми текстами, сжатие ×9,4) | < 0,2 ГБ | — |
| Начальная загрузка из WSL (VPN, RTT 48 мс) | ~17 ч (1 900 строк/с все колонки) | ~25 мин | сек. |
| Начальная загрузка с прода | **нужен замер** (ожидаем в разы быстрее) | минуты | сек. |
| Ежедневно | ~5 мин скан отпечатков + 1–3 млн строк | 2,9 млн строк | сек. |

Замер сжатия — по 927 тыс. реальных строк MF (январь–апрель 2026) в CH 24.3.
Узкое место чтения — сеть, не сервер: без текстов скорость ×2,8 (5 200 строк/с).

## 6. Грабли (проверены)

- **Формат дат.** Логин работает с `DATEFORMAT dmy`: литерал `'2026-09-01'` для
  `datetime` = **9 января**. Даты — только параметрами или в формате `'YYYYMMDD'`.
  (Для колонок типа `date` ISO читается верно — `DateOfLoad`, `Month`.)
- **NULL'ы.** Почти все колонки GLMF nullable; `ICO` — `bit` (кастить в
  `TINYINT`). Сканировать в `sql.Null*` или оборачивать в `ISNULL` (опыт `a3b732e`).
- **Без `ORDER BY`** в extract'е и только seek по `Company` — иначе сортировка
  233 млн строк в tempdb, загрузка «висит» (опыт `3f0fd77`).
- **Таймаут чтения драйвера.** В `go-mssqldb` `connection timeout` действует и
  как дедлайн на чтение: агрегат, молчащий дольше таймаута, рвётся `i/o timeout`.
  В ETL-DSN не задавать (дефолт 0) — пробник с `connection timeout=30` на этом падает.
- **OLAP-рукопожатие TDS** периодически не отвечает (EOF / i/o timeout) — ретраи
  с паузой на уровне шага.
- **Ошибка прошлого дизайна** (`004_fact_glmf`): `ReplacingMergeTree` с `num` в
  ключе. `Num` меняется при каждой пересборке → дедуп не срабатывает, дубли
  копятся. Ключ строки из источника не строить.
- **Не читать 06:00–09:00**: во время `SWITCH` чтение с `NOLOCK` может получить
  ошибку или неконсистентный набор.

## 7. Открытые вопросы

1. **Стабильность отпечатка.** Снять отпечатки за сегодня и сравнить после
   ночной пересборки: у не тронутых дней они должны совпасть. Если поля
   `BINARY_CHECKSUM` недетерминированы (float `ExRate`, порядок) — убрать их.
   Снимок MFKaz от 15.10 сохранён для сверки 16.10.
2. **Учётка только на чтение** для ETL (сейчас `.env` подключается как `sa`):
   `db_datareader` на FinDWH + `SELECT` на `msdb.dbo.sysjobhistory/sysjobs`.
3. **Замер скорости с прода** (из контейнера go-api) — от него зависит, гонять
   начальную загрузку ночью одним прогоном или частями.
4. **ExRates vs `finance.currency_daily`**: у нас уже есть все валюты из
   `CurrencyDaily`; ExRates — только USD из другого источника (`valuta1`).
   Уточнить у аналитиков, нужны ли оба.
5. **Отпечатки на стороне DWH**: предложить владельцу FinDWH писать таблицу
   отпечатков в конце `GLMF_exec` — тогда наш ежедневный 5-минутный скан не нужен.
6. **Доступ Superset** к `findwh` и роль аналитиков в CH.

## 8. Задачи (сделано в MR !142, кроме 7–8)

1. CH-миграция `012_findwh.up.sql`: 5 таблиц `finance.findwh_*` (§3).
2. `etl/findwh_snapshot.go`: полный снимок через `EXCHANGE TABLES` (ExRates, Table_Fin_PL) + тесты маппинга колонок.
3. `etl/findwh_glmf_fp.go`: отпечатки дней (MSSQL) + diff с `glmf_day_fp` + тесты diff'а.
4. `etl/findwh_glmf_load.go`: перегрузка грязных дней + сборка партиции в staging + `REPLACE PARTITION`.
5. Проверка готовности источника по `msdb` + воркер с расписанием + `MODE=findwh` в `cmd/findebt-etl`.
6. ENV (`FINDWH_SYNC_ENABLED`, `FINDWH_GLMF_FROM=20250101`, `FINDWH_READY_AFTER=09:00`, …) — в `.env.example`.
7. Начальная загрузка на проде, сверка `count/sum` по компании×месяцу с MSSQL.
8. Датасеты в Superset + роль аналитиков.

## 9. Эксплуатация

**Включить на проде:** накатить CH-миграции (CI-джоб `migrations` делает сам),
выдать учётке MSSQL права из §7.2, в env go-api `FINDWH_SYNC_INTERVAL=1800`.
Первый тик после ~08:30 сам сделает начальную загрузку GLMF (пустая реплика =
все дни грязные), прогресс — в логе `findwh: glmf <компания> [месяцы]`.

**Руками / начальная загрузка отдельно от go-api:**

```bash
docker exec -e MODE=findwh finance-go-api-1 /findebt-etl          # как воркер
docker exec -e MODE=findwh -e FINDWH_FORCE=1 -e FINDWH_TABLES=table_fin_pl finance-go-api-1 /findebt-etl
docker exec -e MODE=findwh -e FINDWH_FORCE=1 -e FINDWH_GLMF_FULL=1 -e FINDWH_TABLES=glmf finance-go-api-1 /findebt-etl
```

(на проде креды в `/etc/api.env` — сорсить, см. память про `docker exec`).
Не запускать CLI параллельно с воркером: оба пишут в `finance.findwh_glmf_stage`.

**Статус в кабинете:** `/admin/etl` «Загрузки данных» (только `ROLE_ADMIN`, ручка
`GET /api/admin/etl/findwh`, код `go/internal/etl/findwh_status.go`). По каждой
таблице: состояние (загружено за сегодня / грузится / ждём DWH / в очереди /
DWH опаздывает после 12:00 при `FINDWH_READY_CHECK=1` / ошибка / устарело ≥2 дн. /
ClickHouse недоступен / источник не проверен), дата данных, последний прогон и
обычная длительность, объём и диапазон дат реплики, готовность источника; для
GLMF — сверка «строк в реплике = Σ по отпечаткам». Плюс действующие `FINDWH_*`
(неразобранное значение помечается), состояние воркера (что грузит сейчас — из
памяти go-api, CLI-прогоны видны только в журнале) и последние 40 прогонов.

Страница не трогает пул воркера: у неё свой пул MSSQL на одно соединение с
`dial timeout=5` / `connection timeout=15` (открывается, если заданы
`MSSQL_PREMASTER_*`, даже при выключенном воркере). Ответ укладывается в 20 с:
ветки MSSQL (10 с на всё) и CH (5 с на запрос) идут параллельно, ошибки
источников показываются в отчёте, а не валят его.

**Свежесть данных** (для дашборда/алерта):

```sql
SELECT table_name, max(source_date) AS loaded_for, argMax(status, started_at) AS last_status
FROM finance.findwh_etl_runs GROUP BY table_name
```

**Роль аналитиков** (руками, под пользователем с правом управления доступом —
в миграцию не вынесено, у мигратора прав может не быть):

```sql
CREATE ROLE IF NOT EXISTS findwh_reader;
GRANT SELECT ON finance.findwh_glmf TO findwh_reader;
GRANT SELECT ON finance.findwh_table_fin_pl TO findwh_reader;
GRANT SELECT ON finance.findwh_ex_rates TO findwh_reader;
GRANT SELECT ON finance.findwh_etl_runs TO findwh_reader;
GRANT findwh_reader TO <пользователь Superset/аналитика>;
```

**Сверка с источником** (раз после начальной загрузки, §8.7): по компании×месяцу
`count()` и `sum(amount_with_vat_byn_fact)` в `finance.findwh_glmf` против того же
в MSSQL (`GROUP BY Company, Month`, `Date >= '20250101'`).

**Если секция не сходится** (`stage не сходится с источником` в логе): секции не
трогаются, отпечатки не пишутся — на следующем тике попытка повторится. Если
расхождение стабильное (реплику правили руками), перегрузить:
`FINDWH_FORCE=1 FINDWH_GLMF_FULL=1 FINDWH_TABLES=glmf`.
