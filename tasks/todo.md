# TODO — ВГО-отчёт: GLMF → ClickHouse (два потока)

Источник: `SPEC.md` + `tasks/plan.md`. Отмечай `[x]` по мере выполнения.
База для всех Go-задач: `cd go && go build ./... && go vet ./... && go test ./...`.
CH-миграции: `swarm/migrate-clickhouse.sh` (по одному statement'у).
Разведка OLAP: `cmd/mssql-probe` (`PROBE_SQL=...`) или временный `go/cmd/<tmp>` (удалять).

> Прежний finpl/Table_Fin_PL-план — в git (commit `8e21daf`). Этот заменяет его.

---

## Фаза A — Поток GLMF → CH (один ЮЛ)

### [x] T1. ✅ 35793c5 CH-таблица `fact_glmf` + extract + bootstrap (1 ЮЛ)
**Файлы:** `clickhouse/migrations/004_fact_glmf.{up,down}.sql`,
`go/internal/etl/extract_glmf.go`, `bootstrap.go` (ветка fact_glmf), `ch.go` (при необходимости)
- Миграция `fact_glmf` (ReplacingMergeTree(date_of_load), PARTITION toYYYYMM(month),
  ORDER BY company_id, month, counterparty_id, dr_acc, cr_acc, doc_id, num):
  company_id, counterparty_id, doc_id, num, date, month, dr_acc, cr_acc, dr_acc_root,
  cr_acc_root, code_pl, group_pl, ico, country, amt_wovat_byn, amt_withvat_byn,
  amt_wovat_usd, amt_withvat_usd, doc_name_1c, operation_description, date_of_load.
- `extract_glmf.go`: SELECT из `[FinDWH].[dbo].[vGLMFAddUSD]` (все поля выше), ВГО-фильтр.
- `bootstrap`: залить один ЮЛ (напр. TDMF `6950135110`) в `fact_glmf`, идемпотентно.

**Acceptance:** `fact_glmf` содержит строки ЮЛ; `count` и `sum(amt_wovat_byn)` за месяц
совпадают с прямым запросом к GLMF.
**Verify:** `swarm/migrate-clickhouse.sh`; bootstrap (admin/cmd); CH `SELECT count(),
sum(amt_wovat_byn) FROM finance.fact_glmf WHERE company_id='6950135110' AND month='2026-01-01'`
≈ GLMF тот же срез.

> **===== CHECKPOINT A =====** Данные одного ЮЛ в `fact_glmf` сходятся с GLMF.

---

## Фаза B — Отчёт из `fact_glmf`

### [ ] T2. repo_clickhouse — выручка (точные Дт/Кт)
**Файлы:** `go/internal/reports/debt/repo_clickhouse.go`, `chart_of_accounts.go`
- Выручка по корреспонденциям ТЗ (per country): РБ `Дт 62.1 Кт 90.1.1`; РФ `Дт 62 Кт 90.01`
  + `Дт 76.09 Кт 90.01`; КЗ `Дт 1210 Кт 6010`; УЗ `Дт 4015 Кт 9010`. WOVAT, только ВГО.
- RevenueLastMonth — последний календарный месяц периода.
- Источник — `fact_glmf` (не fact_premaster).

**Acceptance:** `/report` (ch на fact_glmf) отдаёт выручку по парам для залитого ЮЛ.
**Verify:** `go test`; сравнить выручку пары с прямым GLMF-запросом (по Дт/Кт и по `group_pl`).

### [ ] T3. repo_clickhouse — ДЗ/КЗ-сальдо (субсчёт)
**Файлы:** `repo_clickhouse.go`, `chart_of_accounts.go`
- signed-сальдо (opening/turnover/closing) по 62/60/76/1210/3310 на **уровне субсчёта**
  (полный `dr_acc/cr_acc`), WithVAT. Разворот в DZ/KZ через `ClassifyAccount` (учесть КЗ/УЗ счета).
- Субсчёт + наименование в `DebtRow` (поля Subaccount/SubaccountName).

**Acceptance:** `/report` содержит выручку + ДЗ/КЗ-сальдо по субсчетам залитого ЮЛ.
**Verify:** `go test`; сальдо сверить с оборотно-сальдовой по ЮЛ (gate).

> **===== CHECKPOINT B =====** Сверка ЧИСЕЛ (выручка точные Дт/Кт vs group_pl; ДЗ/КЗ-сальдо)
> на 1–2 ЮЛ × месяц против GLMF и офиц. ОПУ/оборотки. Главный gate качества.

---

## Фаза C — Договоры (второй поток)

### [ ] T4. CH-таблица `dim_contract` + extract + bootstrap
**Файлы:** `clickhouse/migrations/005_dim_contract.{up,down}.sql`,
`go/internal/etl/extract_contract.go`, `bootstrap.go` (ветка dim_contract)
- Миграция `dim_contract` (ReplacingMergeTree, ORDER BY doc_id):
  doc_id, contract_ref, contract_name, account_kind (62/60/76).
- `extract_contract.go`: `SELECT DISTINCT DocID, <субконто по счёту>, Objects.Name`
  из `Premaster1C`(+`Premaster1CHistory`); 62→`DrSubconto2`, 60/76→`CrSubconto1`;
  **эвристика-фильтр** имени (Договор|Соглашен|Оферт|Контракт|№|\d+/\d+|от ДД.ММ.ГГГГ;
  отсечь 00БС|ТДБП|Оказание|Реализаци|Поступлени).
- bootstrap `dim_contract` отдельным потоком.

**Acceptance:** `dim_contract` заполнена; 1 договор на `doc_id`; имена чистые (эвристика).
**Verify:** CH `SELECT count(), uniq(doc_id) FROM finance.dim_contract`; выборка имён глазами.

### [ ] T5. repo_clickhouse — JOIN `dim_contract` + drilldown
**Файлы:** `repo_clickhouse.go`
- `LEFT JOIN finance.dim_contract USING(doc_id)` (или `dictGet`) → `contract_name/ref` в DebtRow.
- Drilldown («Документ операции») — из `fact_glmf` по doc_id + договор.

**Acceptance:** `/report` показывает названия договоров; нет договора → «без договора».
**Verify:** `go test`; запрос `/report`/`/drilldown` с договорами для залитого ЮЛ.

> **===== CHECKPOINT C =====** Покрытие договоров по 62/60/76 (%); чистота эвристики.

---

## Фаза D — Масштаб + инкремент + wiring

### [ ] T6. Инкремент (watermark `DateOfLoad`) + все ЮЛ + admin
**Файлы:** `incremental.go`, `bootstrap.go`, `CountryByINN` (extract/bootstrap), admin-эндпоинты
- Инкремент `fact_glmf` по `DateOfLoad > last` (вместо DateOfChange); чекпоинты
  `source='glmf'`. Периодический re-bootstrap dim_contract.
- Расширить `CountryByINN` на КЗ/УЗ (+ DR/DR2/GP при необходимости).
- Bootstrap всех 15+ ЮЛ; admin-управление (как для fact_premaster).

**Acceptance:** все ЮЛ налиты; инкремент тянет дельту по DateOfLoad без дублей (Replacing).
**Verify:** `make logs-go`; CH counts по всем company_id; повторный тик не растит дубли (FINAL).

### [ ] T7. config/env + wiring `DEBT_BACKEND=ch`→`fact_glmf`
**Файлы:** `config.go`, `cmd/api/main.go`, `.env.example`
- ch-бэкенд читает `fact_glmf` (+dim_contract). Env: `MSSQL_GLMF_VIEW=vGLMFAddUSD`,
  watermark-настройки. **Все новые env — в `.env.example` тем же коммитом.**
- Дефолт `DEBT_BACKEND` пока `mssql` (флип — T9).

**Acceptance:** `DEBT_BACKEND=ch` отдаёт полный отчёт из fact_glmf+dim_contract; `mssql` — без регрессий.
**Verify:** `make restart-go-api && make logs-go`; запрос `/report`.

---

## Фаза E — КЗ/УЗ договоры + cutover

### [ ] T8. Разведка субконто КЗ/УЗ → договоры КЗ/УЗ
**Файлы:** `extract_contract.go`, `docs/reports/debt/probe-contracts.md`
- Разведка: какое субконто = договор для КЗ (1210/3310/6000) и УЗ-счетов (gate prod-verification §C).
- Расширить `extract_contract` на КЗ/УЗ; обновить карту субконто.

**Acceptance:** договоры резолвятся и для КЗ/УЗ (где есть); карта субконто задокументирована.
**Verify:** CH покрытие договоров по КЗ/УЗ-счетам.

### [ ] T9. Cutover: дефолт `ch` + ретайр `fact_premaster` + docs  ⚠ прод
**Файлы:** `config.go`, `.env.example`, `CLAUDE.md`, `docs/reports/debt/*`, `SPEC.md`
- Сменить дефолт `DEBT_BACKEND` → `ch` (на `fact_glmf`). Ретайр `fact_premaster`
  (после подтверждения). Обновить доки/CLAUDE.md/SPEC статус.
- Чек-лист prod-готовности: `fact_glmf` налит по всем ЮЛ, инкремент идёт.

**Acceptance:** дефолт — ch на fact_glmf; откат `mssql` доступен; доки согласованы.
**Verify:** `make restart-go-api` без env → `backend=ch`; полный `/report`.

> **===== CHECKPOINT D =====** Полная сверка всех ЮЛ + prod-готовность перед merge в master.

---

## Связанные памятки / доки
- `SPEC.md`, `docs/reports/debt/{tz-requirements,prod-verification,finpl-merge,clickhouse-design}.md`
- [[debt-table-fin-pl-source]] — источники, GLMF, валюта (вне скоупа).
- [[feedback-env-example-must-document]] — env в `.env.example` тем же коммитом.
- [[infra-clickhouse-http-multistatement]] — CH-миграции по одному statement'у.
- [[infra-olap-mssql-tds-handshake]] — `10.10.6.15` EOF на handshake → ретраи.
- [[debt-docid-bridge]] — мост DocID Premaster↔Docs (для срока/просрочки).
