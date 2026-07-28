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

### [x] VS2. Факт МП (online FinDWH + PLANS_MOCK) — `160ed09`
- [x] `plans/{fact.go,olap_mp.go,mock_mp.go}` (в пакете plans, не подпакет — иначе цикл импорта с MarketplaceSeed)
- [x] Online через FinDWH (переиспользуем `mssqlDB` ВГО / `MSSQL_PREMASTER_*`), `PLANS_MOCK=1` → фикстуры; nil MSSQL → fallback mock
- [x] `GET /api/plans/mp/fact?year&month&segment` за `RequireRole(PlansUser)`; `usePlans.ts`; `MpFactTable.vue`; `pages/plans/mp.vue`
- [x] `.env.example`: `PLANS_MOCK`, `PLANS_MP_FACT_VIEW` (заведены в VS0, config их читает)
- [x] Приёмка: mock → WB/335/1046/2026-05 = 357034569.85; UI read-only — покрыто тестами
- [x] `go test ./internal/plans/...` зелёные (TDD: RED→GREEN)
- [ ] ⏳ CHECKPOINT A: live-сверка сумм с прототипом — при поднятом FinDWH (`PLANS_MOCK=0`); до сверки держать mock
- [ ] ⏳ UI-smoke `/plans/mp` — при `make up` (фронт-typecheck/раннер локально недоступен)

## Фаза B — Write-path (ввод тактики)

### [x] VS3. Ядро записи: pl_instance + pl_metric + форма GET/PUT (round-trip) — `d108798`
- [x] Таблицы `pl_instance/pl_stage_instance/pl_metric/form_submission` (`0011_plans_core`)
- [x] `store.go` (MetricStore: pgx + in-memory для тестов) + `service.go` (сборка матрицы, upsert тактики, снимок) + `form.go` (чистые buildMpForm/metricsFromRequest)
- [x] `GET/PUT /api/plans/mp/form`; `POST /api/plans/instances` за `RequireRole(PlansUser)`
- [x] `usePlanForm.ts`; `components/plans/MpForm.vue`; `pages/plans/mp/[segment].vue` (route `[id]` отложен — форма по year/month/segment)
- [x] Приёмка: ввод→PUT→GET тот же; строка `pl_metric`; снимок `form_submission`; факт read-only — покрыто round-trip тестами (сервис + HTTP)
- [x] `go test ./internal/plans/...` зелёные (round-trip через in-memory store)
- [ ] ⏳ накат `0011` `make migrate` + UI-smoke ввода тактики — при `make up`

### [x] VS4. ABAC-срез (plans_user_scope) — `9051b80`
- [x] Таблица `plans_user_scope` (`0012`, user_id, role, stage_code, country, legal_entity, code_cfo[])
- [x] `abac.go`: Principal (+admin-обход), `applyScope`/`checkScopeRows` (чистые); `scope_store.go` (pgx); резолв в `MpForm`/`SaveMpForm`
- [x] `PUT /api/plans/scope/{user_id}` (роль `ROLE_PLANS_ADMIN`); principal из `auth.CurrentUser`+`HasRole`
- [x] Приёмка: small-юзер не видит large на API; площадка 335 не пишет в 337; `name_cfo` (из VS3) — покрыто тестами
- [x] CHECKPOINT C: изоляция разрезов на API воспроизведена (тесты large↔small, своя/чужая площадка)
- [ ] ⏳ seed реальных ответственных (Мурашко/Левин) — нужны их user_id (B24); назначаются через `PUT /api/plans/scope/{user_id}`. Полный конфигуратор — этап 2

### [x] VS5. Комментарии + корректировки (COM-01/ADJ-02) — `d915b07`
- [x] `migrations/0013_plans_workflow` (`pl_adjustment`, `pl_comment`) — 0012 занят scope
- [x] Обязательная причина при `is_manual` (ADJ-02); `pl_adjustment` хранит `adjusted_value/reason`; `FormRow.Manual` для подсветки (ADJ-04)
- [x] `GET/POST /api/plans/instances/{id}/comments`; корректировки пишутся в `SaveMpForm`; `CellComment.vue` + reason-поле
- [x] Приёмка: PUT с `is_manual` без причины → 400; скорректированные ячейки помечены — покрыто тестами
- [x] `go test ./internal/plans/...` зелёные (manual-без-причины, персист adjustment, comments add/list)
- [ ] ⏳ накат `0013` `make migrate` + UI-smoke — при `make up`

## Фаза C — Excel + валюта

### [x] VS6. Импорт/экспорт Excel TPL-MP — `dfe6601`
- [x] `plans/importexport.go` (минимальный xlsx на stdlib zip+XML, без зависимостей); `GET /api/plans/mp/export`, `POST /api/plans/mp/import`
- [x] Импорт: editable-колонки (amount|tactic); обязательны `code_cfo+code_pl+reason`; неизвестный код → ошибка строки; чужой ABAC → отклонение файла (через SaveMpForm)
- [x] Приёмка: export→readback сохраняет тактику; неизвестный code_pl/пустая причина → ошибка — покрыто тестами
- [x] `go test ./internal/plans/...` зелёные (grid round-trip + домен)
- [x] CHECKPOINT B: round-trip кодека без расхождений (значение сохраняется через xlsx)
- [ ] ⏳ UI-smoke export/import — при `make up`

### [x] VS7. Валюта (dir_fx_rate) + сегмент small — `730df13`
- [x] `dir_fx_rate` (seed BYN-база) в реестре; `plans/currency.go` пересчёт BYN/RUB/USD через BYN
- [x] Переключатель валюты на странице формы; `segment=small` (группа 480, RU/KZ/UZ) + demo-факт small
- [x] Приёмка: переключение валюты согласовано с курсами; small-форма показывает Kaspi/Uzmarket — покрыто тестами
- [x] `go test ./internal/plans/...` зелёные (конвертация + small)
- [x] CHECKPOINT D: пересчёт через курсы dir_fx_rate (помесячные курсы из источника — этап 2)
- [ ] ⏳ UI-smoke переключения валюты/small — при `make up`

### [x] T-DOC. Финализация документации
- [x] `.env.example`: `PLANS_MOCK`, `PLANS_MP_FACT_VIEW` (читаются config), `PLANS_MP_PENALTIES_VIEW`/`PLANS_AUDIT_ENABLED` (forward, этап 2)
- [x] SPEC статус MVP-вертикалей VS0–VS7; чекпоинты B/D — на mock-уровне, A/C — на проде
- [ ] ⏳ При `make up`: накат миграций 0010–0013, CHECKPOINT A (live FinDWH) + UI-smoke всех экранов

---

## Статус: VS0–VS12 готовы (MVP + CALC/D11 + workflow + аудит). Стенд поднят, миграции 0010–0016 применены, сквозной смоук пройден.

### Прогон на стенде 25.06 (✅ / находки)
- ✅ Миграции 0010–0016 применены (`docker exec swarm-postgres-1 psql … < migrations/*.up.sql`).
- ✅ Auth/роли (ROLE_PLANS_* через иерархию ROLE_ADMIN), справочники, форма round-trip в PG, ADJ-02 (400 без причины), CALC + override, workflow (lazy-init этапов по календарю, WF-DEP блокирует 1.6←2.4), аудит подключён (off по умолч.).
- 🔶 **CHECKPOINT A (Q1):** FinDWH доступен, но `ALL_view_МП` в подключённой БД нет → витрина МП = иной источник (куб Продажи+Payments). Нужно имя/расположение от аналитика. Форма деградирует до пустого факта (фикс закоммичен), `PLANS_MOCK=1` рабочий путь.
- ⏳ UI-smoke в браузере (`http://finance.local`/`:3001`) — за пользователем.

---

## Этап 2 — прогресс

### [x] VS8. CALC-движок: вычислитель + calc_rule + каскад — `f4937e7`
- [x] `eval.go` безопасный вычислитель (TDD); `calc.go` CalcRuleSeed/resolveFormulas/computeCascade
- [x] `migrations/0014_plans_calc` (calc_rule + pl_formula_override + seed формул)
- [x] `service.ComputeMp` + `GET /api/plans/mp/compute`; UI-превью каскада
- [x] Формулы провизорные (Q4b) — как данные, переопределяемы

### [x] VS9. pl_formula_override — per-срез переопределение формулы (D11) — `e076a47`
- [x] store: FormulaOverrides(plID)/UpsertOverride (pgx + mem)
- [x] `ComputeMp` мёржит override поверх seed; `PUT /api/plans/mp/formula` (причина + проверка компиляции)
- [x] Тесты: override меняет расчёт; обязательная причина; битая формула отклонена
- [x] UI: override-редактор на странице формы
- [ ] ⏳ накат `0014` + UI-smoke — при `make up`

### [x] VS10. Список экземпляров PL + дашборд + справочники — `6590b78`
- [x] `store.ListInstances` + `GET /api/plans/instances`; тест metric_count
- [x] Дашборд `/plans` (KPI, список PL, создание периода, deep-link); экран справочников `/plans/directories`
- [x] Форма читает `?year&month`
- [ ] ⏳ UI-smoke — при `make up`

### [x] VS11. Аудит (AUD-01..05, PLANS_AUDIT_ENABLED) — `16a3d5c`
- [x] `0015_plans_audit`; `audit.go` (Auditor: pgx + no-op); запись на мутациях; `GET /api/plans/audit` + CSV
- [x] config `PlansAuditEnabled`; UI `/plans/audit` (фильтры, CSV); ссылка для plans-админа
- [x] Тесты: запись save_form; no-op выключен
- [ ] ⏳ накат `0015` + UI-smoke — при `make up`

### [x] VS13. Свод по ЮЛ × каналам (TPL-08) — `4829228`
- [x] dir_marketplace: маппинг площадка→ЮЛ (провизорный, Q-ЮЛ); `store.MetricsAll`
- [x] `service.Svod` (агрегат 1046 по ЮЛ×канал); `GET /api/plans/mp/svod`; панель на карточке PL
- [x] Тест + смоук на стенде (TD Mark Formelle 999000)

### [x] VS14. UX-ревизия: свод периода, валюта, таргет, статусы (2026-07-25)
Основание: `docs/reports/plans/ux-redesign.md` (обратная связь заказчика + финансов),
источники подтверждены probe'ом — `mp-source-investigation.md` §ОБНОВЛЕНИЕ 2026-07-25.
- [x] `PLANS_MP_TAKT_TABLE` → `MpFactSource.MpTaktTarget` (FormToLoaTaktTarget) + mock-таргет
- [x] `mp_layers.go`: факт / факт пр. года / стратегия / таргет одним слоем для формы и свода
- [x] Валюта — параметр (`MpFormData(…, currency)`, `SaveMpForm(…, currency)`), хранение в RUB, проценты не конвертируются
- [x] `GET /api/plans/instances/{id}/board` + `MpBoard.vue`: свод периода с фильтрами (валюта/сегмент/ЮЛ/страна/площадка), детализация по МП в строках
- [x] `usePlanStatus.ts` — единый словарь состояний; `PlanStatusBar.vue` — состояние периода (этап, дедлайн, просрочки, готовность)
- [x] `/plans` → рабочий стол «Сейчас от вас ждут»
- [x] Ввод процентов в процентах; кнопка «Таргет→тактика»
- [x] Уведомления по заданиям (`task_notify.go`): назначение/делегирование/возврат/сдача
- [x] `go build/vet/test` (в т.ч. `-race`) зелёные; `nuxt build` проходит
- [ ] ⏳ UI-smoke на поднятом стенде — за пользователем
- [ ] ⏳ Строка 8006: в ФАКТЕ её нет с 2023-12 (есть в Plan/TaktTarget) — решение за финансами
- [ ] ⏳ Матрица ответственных ЦФО × этап, split задания по площадкам, срок+комментарий при делегировании

### [x] VS15. Объединение форм МП large+small в одну (2026-07-25)
- [x] `migrations/0029_plans_mp_single_form.{up,down}.sql` — один шаблон «Маркетплейсы» (`segment:"all"`)
- [x] `matchingCfo`: `segment="all"` → все площадки dir_marketplace
- [x] Мультисегментная форма: `mpLayerSet`/`forCfo`, `MpFormPlatform.segment/legal_entity`
- [x] `SaveMpForm` пишет `pl_metric.segment` по площадке (не по заданию)
- [x] UI формы: фильтры Маркет / Сегмент / ЮЛ, группировка колонок, «Итого по фильтру»
- [x] «· без ТОПа» → «· исполнитель не назначен»
- [ ] ⏳ После наката 0029 — «Генерировать задания» на карточке периода

### Дальше (вне текущего прохода)
Прочие шаблоны (ТО-розница/ЦФО-затраты/опт/ИМ/производство/стратегия) ·
cron-синхронизация справочников · уведомления на события workflow ·
**CHECKPOINT A** — источник МП (см. `mp-source-investigation.md`, решение BI).

## Этап 2 (полный объём — не начинать без отдельного плана)
CALC-движок (гибрид формул D11: `calc_rule` + `pl_formula_override` + безопасный
вычислитель, drill-down «в формулу») · workflow 1.2–4 + календари р.д. по странам
(`plans_country_calendar`, глоб. настройки; дедлайн по стране ответственного) ·
своды 1.6/2.4 (TPL-08) · TPL-TO-RETAIL/CFO-EXP/WHOLESALE/IM/PROD-MINUTES/STRATEGY ·
TPL-09 копирование · конфигураторы · аудит (`PLANS_AUDIT_ENABLED`) · cron-синк
справочников.
