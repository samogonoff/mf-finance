# TODO — ВГО-отчёт на `Table_Fin_PL`

Источник: `SPEC.md` + `tasks/plan.md`. Отмечай `[x]` по мере выполнения.
Базовая проверка для всех Go-задач: `cd go && go build ./... && go vet ./...`.

---

## Фаза 0 — Фундамент (без смены поведения)

### [ ] T1. Справочник юрлиц: код → {ИНН, Name, Country}
**Файлы:** `go/internal/reports/debt/seed.go`, `model.go`
- Добавить поле `Code string` в `Entity` (`model.go`).
- Заполнить коды для 15 ЮЛ; **добавить новые** записи: `DR`/692221084, `DR2`/693335015,
  `GP`/190465888 (имя GP — уточнить, временно плейсхолдер).
- Helper `INNByCode(code string) (string, bool)` и/или `EntityByCode`.
- Не ломать `Entities()`, `OurINNs()`, `EntitiesLevel1()`.

**Acceptance:**
- Все 12 кодов из SPEC §4.2 резолвятся в корректный ИНН.
- Существующие функции возвращают прежние данные (новые поля не ломают JSON).

**Verify:** `cd go && go build ./... && go vet ./...`; точечный `go run` или временный
`_test.go` рядом, проверяющий `INNByCode("MF")=="690591512"`, `INNByCode("DR")` ок.

---

### [ ] T2. Config + env-ключи (дефолт пока не меняем)
**Файлы:** `go/internal/config/config.go`, `.env.example`
- Добавить `DebtFinPLTable` (`MSSQL_FINPL_TABLE`, default `Table_Fin_PL`),
  `DebtFinPLMinMonth` (`DEBT_FINPL_MIN_MONTH`, default `2025-01-01`).
- `DEBT_BACKEND` пока остаётся default `mssql` (флип — в T9).
- **`.env.example`** — описать обе переменные (что управляет, дефолт, пустое поведение)
  в **том же коммите** (правило проекта, [[feedback-env-example-must-document]]).

**Acceptance:** конфиг компилируется и читает env; `.env.example` содержит обе записи.
**Verify:** `cd go && go build ./...`; `grep -E 'MSSQL_FINPL_TABLE|DEBT_FINPL_MIN_MONTH' .env.example`.

---

## Фаза 1 — Чтение из Table_Fin_PL (revenue-срез)

### [ ] T3. ВГО-фильтр и period-helpers для finpl
**Файлы:** `go/internal/reports/debt/vgo_report_filter.go` (или новый `finpl_filter.go`)
- `vgoFinPLClause()` → `AND [ВГО] = 1` (без списка ИНН).
- Month-нормализация: `monthFloor(date_from, minMonth)`, `monthCeil(date_to)` —
  округление к границам месяца + клампинг нижней границы (2025-01).

**Acceptance:** хелперы покрыты точечным тестом (граничные месяцы, период < 2025-01).
**Verify:** `cd go && go test ./internal/reports/debt/ 2>/dev/null || go build ./...`
(раннера нет — допускается локальный `_test.go`).

---

### [ ] T4. `repo_finpl.go` — Report() выручки из Table_Fin_PL
**Файлы:** `go/internal/reports/debt/repo_finpl.go` (НОВЫЙ)
- `finPLRepo` реализует `PremasterRepo` (как `clickhouseRepo`): MSSQL-коннект к
  `Table_Fin_PL` (переиспользовать `NewPremasterRepo` DSN или отдельный конструктор).
- `Report()`: `SELECT ... WHERE [ВГО]=1 AND [Month] BETWEEN @from AND @to`,
  GROUP BY компания/контрагент/валюта; `Компания`→ИНН/Name через T1.
- Выручка из `Amount*` по продажным `GroupPL/CodePL`; мультивалюта (дизайн-решение §4.3 плана).
- `Drilldown()` — заглушка/делегирование (полноценно в T8).
- На этом срезе ДЗ/КЗ = 0 (добавит T7).

**Acceptance:** против непустой `Table_Fin_PL` (или фикстуры из T5) возвращает строки
выручки с корректными компанией/контрагентом/валютой.
**Verify:** `go build ./...`; ручной прогон через mock (T5) — см. CHECKPOINT A.

---

### [ ] T5. Mock-фикстуры finpl (снэпшот пуст)
**Файлы:** `go/internal/reports/debt/mocks.go`
- Фикстуры, отражающие месячную ОПУ-структуру (revenue-строки, 2025 г., мультивалюта,
  включая ВГО-пару с DR/Дримдом для проверки нового справочника).

**Acceptance:** `DEBT_MOCK=1` отдаёт осмысленные revenue-строки нового формата.
**Verify:** `go build ./...`; запрос `/report` под finance-admin сессией возвращает фикстуры.

---

### [ ] T6. Wiring: `DEBT_BACKEND=finpl` (opt-in)
**Файлы:** `go/cmd/api/main.go`
- В блоке выбора бэкенда добавить ветку `cfg.DebtBackend == "finpl"`:
  `finPLRepo` для Report, Premaster — для drilldown/fallback (по образцу `compositeRepo`).
- Логировать `debt: backend=finpl`.

**Acceptance:** `DEBT_BACKEND=finpl` стартует, `/report` идёт в Table_Fin_PL;
`DEBT_BACKEND=mssql` — прежнее поведение (без регрессий).
**Verify:** `cd swarm && make restart-go-api && make logs-go` → строка `backend=finpl`;
запрос `/api/reports/debt/report` отдаёт revenue-строки.

> **===== CHECKPOINT A =====**
> Revenue-путь из Table_Fin_PL работает (opt-in). Ревью формы строк, классификации
> выручки и мультивалюты на 1–2 месяцах. Согласовать перед merge ДЗ/КЗ.

---

## Фаза 2 — Premaster fallback (ДЗ/КЗ + договор + просрочка)

### [ ] T7. Merge: finpl-выручка ⋈ premaster ДЗ/КЗ  ⚠ РИСК §11.2
**Файлы:** `go/internal/reports/debt/repo_finpl.go`, дизайн-заметка в `docs/reports/debt/`
- Реализовать композицию: revenue-строки (Table_Fin_PL) + ДЗ/КЗ-сальдо, договор,
  срок/просрочку, PartnerINN/менеджер/канал (Premaster `Report`).
- **Зафиксировать правило слияния по гранулярности** (отдельные строки vs агрегат на
  пару Компания×Контрагент×Валюта) — см. CHECKPOINT B.
- Граничные: контрагент с ИНН=NULL (Летникова) не роняет; ненайденный код компании.

**Acceptance:** итоговый `/report` содержит и выручку (finpl), и ДЗ/КЗ-сальдо (premaster);
правило слияния задокументировано.
**Verify:** `go build ./...`; сравнить выдачу с `DEBT_BACKEND=mssql` (ДЗ/КЗ совпадают,
выручка берётся из finpl).

> **===== CHECKPOINT B =====**
> Ревью грануляр-merge (риск §11.2): отдельные строки vs агрегат, дата closing-сальдо.
> Согласовать с заказчиком до cutover.

---

## Фаза 3 — Drilldown и переключение дефолта

### [ ] T8. Drilldown «согласно PL» (месячная PL-детализация)
**Файлы:** `go/internal/reports/debt/repo_finpl.go`, `model.go` (при необходимости)
- `Drilldown()` из `Table_Fin_PL`: строки месяца — `DocName1C, OperationDescription,
  CodePL/GroupPL, Dr_Cr, Amount*`. Дневной premaster-drilldown в finpl не используется.
- `DocumentRow` переосмыслить как PL-строку месяца (§5.1 SPEC); не ломать JSON-контракт.

**Acceptance:** drilldown под finpl возвращает месячные PL-строки; `DEBT_BACKEND=mssql`
сохраняет дневной drilldown.
**Verify:** `go build ./...`; запрос `/drilldown` под finpl отдаёт месячную детализацию.

---

### [ ] T9. Cutover: дефолт `finpl` + filter-options + доки  ⚠ затрагивает prod
**Файлы:** `config.go`, `.env.example`, `go/cmd/api/main.go`, `seed.go`/`service.go`
(filter-options), `CLAUDE.md`, `docs/reports/debt/*`, `SPEC.md` (статус → landed)
- Сменить default `DEBT_BACKEND` → `finpl`; обновить комментарии config.
- `FilterOptions`: решить, расширять ли список юрлиц новыми ВГО-ЮЛ (DR/DR2/GP) —
  согласно решению заказчика.
- Обновить `.env.example` (новый дефолт) + `CLAUDE.md` (блок про источник debt) + доки.
- Чек-лист prod-готовности: `Table_Fin_PL` наполнена (`Update_Table_Fin_PL` по расписанию).

**Acceptance:** дефолтный старт использует finpl; докуменация и `.env.example` согласованы;
есть путь отката (`DEBT_BACKEND=mssql`).
**Verify:** `cd swarm && make restart-go-api && make logs-go` без явного env → `backend=finpl`.

> **===== CHECKPOINT C =====**
> Финальная сверка сумм ВГО с официальным PL-файлом (финотдел). Подтвердить prod-готовность
> и смену дефолта. Только после этого мёржить в master.

---

## Связанные памятки
- [[debt-table-fin-pl-source]] — что за витрина, справочник код→ИНН, ограничения.
- [[feedback-env-example-must-document]] — env обязана попасть в `.env.example`.
- [[infra-olap-mssql-tds-handshake]] — `10.10.6.15` EOF на handshake → ретраи.
- [[debt-docid-bridge]] — мост DocID Premaster↔Docs (для просрочки в fallback).
