# TODO — Модуль «Тактические планы», MVP TPL-MP

Источник: `docs/reports/plans/SPEC.md` + `docs/reports/plans/plan.md`. Отмечай `[x]`.
База для Go-задач (перед каждым коммитом): `cd go && go build ./... && go vet ./... && go test ./...`.
Миграции: `../swarm/migrate.dev.sh up` (golang-migrate, обе БД) или `make migrate`. Парные `.up/.down`, нумерация после `0009`.
Env: любая новая переменная — сразу в `.env.example` (тот же коммит).

> Положено рядом со спекой: `tasks/plan.md` / `tasks/todo.md` заняты ВГО-отчётом.

---

## Фаза 0 — Каркас

### [x] VS0. Каркас раздела (роли + scope + меню + /plans + health) — `cae4fc4`
- [x] `auth/roles.go`: `RolePlansAdmin`, `RolePlansUser`; в `Allowed`; иерархия `ROLE_ADMIN ⊇ ROLE_PLANS_ADMIN ⊇ ROLE_PLANS_USER`
- [x] `useScope.ts`: scope `plans`; `scope-guard.ts`: ветка `/plans` → `hasScope('plans')`
- [x] `app.vue`: пункт меню «Тактические планы» (виден при `hasScope('plans')`)
- [x] `go/internal/plans/handler.go`: `GET /api/plans/health` за `RequireRole(PlansUser)`
- [x] `pages/plans/index.vue`: заглушка дашборда
- [x] `.env.example`: блок «Тактические планы»
- [x] Приёмка: меню видно плановому юзеру; `/plans` открывается; health 200/403/401
- [x] `go test ./internal/auth/... ./internal/plans/...` — зелёные (TDD: RED→GREEN)

## Фаза A — Read-path (факт)

### [x] VS1. Справочники dir_marketplace / dir_cfo(MP) / dir_pl_line — `7157aa6`
- [x] `migrations/0010_plans_directories.{up,down}.sql` (ядро → 0011 в VS3)
- [x] `plans/seed_mp.go` (`SeedSource`): площадки large(335/336/337/954) + small(953…339); блоки PL 1046/1045/1022/1006/8006 + статьи затрат
- [x] `GET /api/plans/directories`, `/{code}/rows` (404 на неизвестный) за `RequireRole(PlansUser)`
- [x] Приёмка: `dir_marketplace/rows` ≥4 large с `name_cfo/code_cfo/segment/country`; `dir_pl_line` содержит 1046/1045/1022/1006/8006 — покрыто тестами
- [x] `go test ./internal/plans/...` зелёные (TDD: RED→GREEN)
- [ ] ⏳ накат миграции `make migrate` + psql-сверка — при поднятом стеке (PG сейчас down)

### [ ] VS2. Факт МП (online FinDWH + PLANS_MOCK) — **CHECKPOINT A**
- [ ] `plans/sources/{source.go,olap_mp.go,mock_mp.go}` (ключи SUMIFS month+code_cfo+scenario+CodePL; BYN/RUB/USD)
- [ ] Online через креды FinDWH (переиспользуем `MSSQL_PREMASTER_*`/`PLANS_OLAP_*`), `PLANS_MOCK=1` → фикстуры
- [ ] `GET /api/plans/mp/fact?year&month&segment`; `usePlans.ts`; `components/plans/MpFactTable.vue`
- [ ] `.env.example`: `PLANS_MOCK`, OLAP-креды (FinDWH)
- [ ] Приёмка: mock → WB/335/1046/2026-05 = 357034569.85; UI рисует read-only факт
- [ ] CHECKPOINT A: live-суммы == прототип (иначе держать `PLANS_MOCK=1`)

## Фаза B — Write-path (ввод тактики)

### [ ] VS3. Ядро записи: pl_instance + pl_metric + форма GET/PUT (round-trip)
- [ ] Таблицы `pl_instance/pl_stage_instance/pl_metric/form_submission` (0010)
- [ ] `plans/repo.go`+`service.go`: сборка матрицы формы, upsert тактики, снимок
- [ ] `GET/PUT /api/plans/mp/form`; `POST /api/plans/instances`
- [ ] `usePlanForm.ts`; `components/plans/MpForm.vue`; `pages/plans/[id]/mp/[segment].vue`
- [ ] Приёмка: ввод→PUT→GET тот же; строка в `pl_metric`; снимок в `form_submission`; факт/стратегия read-only
- [ ] `go test ./internal/plans/...`

### [ ] VS4. ABAC-срез (plans_user_scope) — **CHECKPOINT C**
- [ ] Таблица `plans_user_scope` (user_id, role, stage_code, country, legal_entity, code_cfo[])
- [ ] `plans/abac.go`: резолв среза; фильтр в `MpForm`/`SaveMpForm`; отклонение чужих ячеек
- [ ] `PUT /api/plans/scope/{user_id}` (роль `ROLE_PLANS_ADMIN`) + seed ответственных (Мурашко large / Левин small)
- [ ] Приёмка: small-юзер не видит large на API; площадка 335 не пишет в 337; в UI — `name_cfo`
- [ ] CHECKPOINT C: матрица доступа SPEC §5 воспроизведена

### [ ] VS5. Комментарии + корректировки (COM-01/ADJ-02)
- [ ] `migrations/0011_plans_workflow` (`pl_comment`, `pl_adjustment`)
- [ ] Обязательная причина при `is_manual`; хранить `original_calculated/adjusted_value`
- [ ] `GET/POST .../comments`, `POST .../adjust`; `components/plans/CellComment.vue`
- [ ] Приёмка: PUT с `is_manual` без причины → 400; скорректированные ячейки помечены
- [ ] `go test ./internal/plans/...`

## Фаза C — Excel + валюта

### [ ] VS6. Импорт/экспорт Excel TPL-MP — **CHECKPOINT B**
- [ ] `plans/importexport.go`; `GET /api/plans/mp/export`, `POST /api/plans/mp/import`
- [ ] Импорт: только editable; обязательны `code_cfo+code_pl+month+reason`; чужой ABAC → отклонение файла
- [ ] Приёмка: export→import идентичные значения; чужой `code_cfo` отклонён
- [ ] CHECKPOINT B: round-trip без расхождений

### [ ] VS7. Валюта (dir_fx_rate) + сегмент small — **CHECKPOINT D**
- [ ] `dir_fx_rate` (month/rate/currency); `plans/currency.go` пересчёт BYN/RUB/USD
- [ ] Переключатель валюты в `MpForm.vue`; включить `segment=small` (группа 480, RU/KZ/UZ)
- [ ] Приёмка: переключение валюты согласовано с курсами; форма small открывается для своего среза
- [ ] CHECKPOINT D: BYN/RUB/USD == H/I/J × курсы

### [ ] T-DOC. Финализация документации
- [ ] Сверить `.env.example` со всеми введёнными переменными
- [ ] Обновить SPEC статусы T/VS; зафиксировать результаты чекпоинтов A–D

---

## Этап 2 (вне MVP, не начинать без отдельного плана)
CALC-движок (гибрид формул D11: `calc_rule` + `pl_formula_override` + безопасный
вычислитель, drill-down «в формулу») · workflow 1.2–4 + календари р.д. по странам
(`plans_country_calendar`, глоб. настройки; дедлайн по стране ответственного) ·
своды 1.6/2.4 (TPL-08) · TPL-TO-RETAIL/CFO-EXP/WHOLESALE/IM/PROD-MINUTES/STRATEGY ·
TPL-09 копирование · конфигураторы · аудит (`PLANS_AUDIT_ENABLED`) · cron-синк
справочников.
