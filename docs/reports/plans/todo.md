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
- ✅ **CHECKPOINT A (Q1) закрыт 2026-07-25** (см. `mp-source-investigation.md`): `ALL_view_МП` из книги = плоские таблицы `Budgeting.dbo.FormToLoadFact / FormToLoadPlan / FormToLoaTaktTarget` (+ `CodeCFO`), штрафы — `FinDWH.dbo.FINDWHACCESSGROUP`. Онлайн-источник подключён (`olap_mp.go`), `PLANS_MOCK=1` остаётся dev-путём.
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

## Новые ТЗ аналитика (var/tz, получены 2026-08-24) — план `Фазы 0–4`

Две скорректированные редакции требований; исходники — `var/tz/ТЗ_Маркетплейсы_тактический_план_форма.docx`
(v1.0 · 06.08.2026) и `var/tz/ТЗ_Розница_тактический_план_форма.docx` (v1.0 · 05.08.2026).
Полный план реализации — раздел «Реализация новых ТЗ» ниже и `SPEC.md` §28.

**Главное по МП:** инверсия направления расчёта — вводятся продажи 1046, %СПП, две
наценки и **доли статей прямых затрат** (новый реестр «Условия площадки»), суммы
расходной части считаются (§3.4 ТЗ). Плюс: статусы карточки, возвраты с обязательным
комментарием, версии-снапшоты, публикация в приёмники Budgeting, эффективный НДС
per-площадка, курс тактики из справочника, общие затраты по 7 группам PL, карточка
площадки, валидации МП-01..11 / W1..W8.

**Главное по Рознице (новая форма TPL-TO-RETAIL):** экземпляр = (страна, период),
4 страны, строка = магазин (до 375), ввод только месячных ячеек плана в нац. валюте,
индикаторы LFL/LFM/%вып./ожидание года, массовые операции (копирование сценария,
распределение «Методика Е.В.», индекс роста, ФОТ с cap 106%, аренда), sync магазинов
из `[001 CodeCFO]`, ABAC по РМ, валидации V-01..11 / W-01..06.

### [x] Фаза 0. Санация перед новыми ТЗ
- [x] `migrations/0030_plans_approval` — таблица `pl_approval` (её не было: `RecordApproval` писал в пустоту, ошибка глушилась) + поля ТЗ: `comment`, `target_stage`, `revoked`
- [x] Возврат этапа требует комментария; решения от целевого этапа и выше аннулируются (`RevokeApprovalsFrom`); `GET /api/plans/instances/{id}/approvals`
- [x] Каскад: override «Наценка %» / «Наценка от общей сс %» ведёт себестоимость (ТЗ §3.4), а не просто подставляется в вывод
- [x] Golden-тест каскада legacy (`mpform_golden_test.go`) — страховка на весь рефакторинг
- [x] Заморозка legacy-ветки `/api/plans/mp/{form,compute,copy,formula,export,import}` → 410 (`PLANS_LEGACY_MP_API=1` для аварийного отката); удалены `pages/plans/mp.vue`, `MpFactTable.vue`, мёртвый `segmentGroup()`

### [x] Фаза 1. Общая оболочка процесса (карточки форм)
- [x] `migrations/0031_plans_form_card` — `form_card` (статусы/шаг/версия/calc_mode/lock), `card_approval` (решения + revoked), `card_version` (снапшот значений+условий+курсов), `plans_form_route` (маршрут данными, шаг «Финансист» выключен по умолчанию)
- [x] `migrations/0032_plans_publish` — `publish_mapping` (ответы BI как данные) + `publish_log`
- [x] `migrations/0033_plans_view_preset` — пресеты представлений (Розница §4.6)
- [x] `migrations/0034_plans_vat_fx` — справочник `dir_vat` (эффективные ставки площадок) + помесячные курсы и KZT/UZS в `dir_fx_rate`
- [x] `card.go` — чистая статусная машина + 8 тестов (возврат только назад и только с комментарием, аннулирование, reopen с причиной, skip_if_same_user, блокировка после утверждения)
- [x] `card_store.go` / `card_service.go` / `card_handler.go` — переход одной транзакцией, лист согласования, версии, API `/api/plans/cards/*`
- [x] `publish.go` / `publish_store.go` — Publisher + dry-run + идемпотентность (DELETE+INSERT, у приёмников нет PK) + контроль «введено = записано» + белый список таблиц; 6 тестов
- [x] `rates.go` — RateBook: НДС и курсы из справочников с кэшем и фолбэком; 4 теста
- [x] `abac_scope.go` — ABAC по стране/ЮЛ/шагу (колонки `plans_user_scope` существовали с 0012, но не применялись); 6 тестов
- [x] `calendar_store.go` — календари из `plans_country_calendar` вместо `CalendarSeed()`
- [x] `form_registry.go` — реестр форм и их карточек (МП large/small, Розница BY/RU/KZ/UZ)
- [x] `.env.example`: `PLANS_PUBLISH_ENABLED`, `PLANS_PUBLISH_TARGETS`
- [ ] ⏳ Уведомления на события карточки (возврат/утверждение/ошибка публикации) — переиспользовать `task_notify.go`

### [x] Фаза 2. МП: условия площадки и инверсия расчёта
- [x] `migrations/0036_plans_mp_conditions` — `mp_conditions` + `mp_condition_item` (доли статей, допускаются отрицательные), `mp_common_cost` (7 групп статей PL), `mp_calc_log` (лог расчёта по ячейке), `plans_calc_owner` (владелец пары CodePL×CodeCFO, ТЗ §8.1)
- [x] `mpform_inverse.go` — каскад «условия → суммы» (ТЗ §3.4) + итоги формы по F274; **приёмочный тест на контрольной выборке Приложения Б воспроизводит Wildberries и итог по форме** (`mpform_inverse_test.go`, 6 тестов)
- [x] Итоговая «доля прямых затрат в обороте» считается по всем площадкам — дефект прототипа (27,92 % вместо 26,32 %) не воспроизводится
- [x] `mp_conditions.go` / `mp_conditions_service.go` / `mp_conditions_handler.go` — реестр условий: CRUD с версиями, копирование из прошлого периода, diff для согласующего с порогами обоснования, подсказки «фактическая доля прошлого месяца» (в реестр не переносятся, §3.1)
- [x] `mp_validate.go` — МП-01…МП-11 + МП-W1…МП-W8, включая контроль двойного счёта 51/52/54 и конфликт наименований кодов 52/54; 8 тестов
- [x] Ручное переопределение расчётной суммы не перезатирается автопересчётом (§7.2): в форме видно «расчёт даёт X, вручную Y»
- [x] `mpform_spec.go` — спека по режиму (`mpFormSpecFor`): в inverse %СПП/наценки/себестоимость read-only, статьи затрат — calc_editable; добавлены строки «PL от себестоимости общей» и «Доля прямых затрат в обороте»
- [x] Штрафы: поддержка источника `DWH.dbo.wb_dimensions_penalty` (ТЗ §6.2/§9.1) наряду с FINDWHACCESSGROUP
- [x] Валюта площадки RUB/KZT/UZS + эффективная ставка НДС per-площадка в форме
- [x] Фронт: `composables/useMpConditions.ts`, `composables/useMpCascade.ts` (второй режим каскада), `pages/plans/mp-conditions/[cardId].vue` (реестр условий с diff, подсказками и пересчётом)
- [ ] ⏳ Инверсия включается на боевой период только после подтверждения §12 п.11 (сейчас `calc_mode` карточки = legacy по умолчанию)

### [x] Фаза 3. Форма «Розница» (TPL-TO-RETAIL)
- [x] `migrations/0035_plans_retail` — `tp_instance` (карточка 1:1, страна/период/валюта/ЮЛ), `tp_row` (магазин + снапшот атрибутов + `row_version`, partial unique по непустому `klient_id`), `tp_value` (строка × метрика × месяц, `source`), `tp_value_version`, `tp_lfl_override`, `tp_period_param`, `tp_reg_manager_map`; справочники `dir_retail_store` и `dir_retail_group_map` («Магазины» ↔ «3.Магазины» — данными, не строковым сравнением)
- [x] `retail_calc.go` — индикаторы §4 по формулам DJ6/DK6/DL6; «ожидание года» берёт закрытые месяцы ТОЛЬКО из календаря; для «новый»/«ххх» LFL и % вып. не считаются (пусто, а не 0)
- [x] `retail_bulk.go` — массовые операции §5 с предпросмотром diff и областью применения: копирование сценария (с коэффициентом), распределение «Методика Е.В.», индекс роста (4 базы, переопределения по городу/LFL/типу/магазину), ФОТ с порогом 106 %, аренда (этап 1 + оборотная часть); ячейка `manual` не перезатирается
- [x] `retail_validate.go` — V-01…V-11 и W-01…W-06; V-05 (сверки = 0) блокирует отправку
- [x] `retail_summary.go` — разрезы §8 (LFL/до года/новые/закрыты + город/РМ/тип/ЮЛ/категория), 11 показателей в нац. валюте и сокращённые наборы BYN/USD, 4 контрольные сверки с расшифровкой
- [x] `retail_source.go` + `retail_mock.go` — источники §11 (001 CodeCFO, sales_and_COGG_from_FOX_offline_retail, VFORMTOLOADPLAN, plan_saler_st) + фикстура на 17 магазинов РБ с фактом 2025-01…2026-06
- [x] `retail_store/service/handler/card/io/sync/regmanager` — репозиторий, ABAC по РМ, HTTP, снапшот и публикация карточки, Excel-обмен, синхронизация справочника, админ-ручки соответствия «пользователь ↔ RegManager»
- [x] `tp_value.metric` (sales|payroll|rent) — осознанное отклонение от §10: формула ФОТ читает план продаж как ВХОД, поэтому ФОТ и продажи не могут делить одну ячейку
- [x] Пример файла для импорта: `GET /api/plans/retail/{cardId}/import-template` + кнопка «Пример импорта» в форме — колонки-ключи, колонки месяцев периода и подсказки строками-комментариями («#…», парсер их пропускает); строки среза идут со своими текущими значениями, поэтому загрузка примера без правок ничего не меняет
- [x] `.env.example`: восемь `PLANS_RETAIL_*`
- [ ] ⏳ «Сумма+самовывоз» как база сравнения (§6a) — не реализовано: «курс П.М» и «индекс самовывоза» в ТЗ не определены, читается только `Параметр='ПРОДАЖИ'`. **Ждёт BI.**
- [x] Сентинел «даты нет» в `[001 CodeCFO]`: отсутствие даты закрытия записано как `1900-01-01` (196 из 206 действующих магазинов РБ), а не пустым значением — V-09 читала его как «закрыт в 1900 году» и выбрасывала из формы ВЕСЬ справочник (прод: «0 из 0 магазинов»). Отсечка `retailDateMinYear` в `parseRetailDate` + нормализация на границе (источник, мок, чтение снапшота из `tp_row`)
- [ ] ⏳ Имена колонок факта (§11 п.2) — в ТЗ не указаны; **проба 10.10.6.15 показала, что дефолта `Summa` в `sales_and_COGG_from_FOX_offline_retail` НЕТ** (запрос падает «Недопустимое имя столбца»), то есть факт розницы на проде не читается вовсе. Кандидаты — `AmountWithVATBelRubFact` (BYN) / `AmountWithVATCurrencyFact`; одной правкой env не закрывается: в таблице рядом лежит `GroupPL='СЫРЬЕВАЯ СЕБЕСТОИМОСТЬ'`, продажи хранятся со знаком минус, а `Fact()` не фильтрует ни `GroupPL`, ни страну. **Подтвердить у BI набор колонок и фильтр.**
- [ ] ⏳ История ранее утверждённой тактики (§11 п.4) — `Checks.dbo.plan_saler_st` на 10.10.6.15 не существует (базы `Checks` нет; есть `Checks_olap`, `CheckOffice`). Ближайшее — `DWH.fact.plan_saler` (`KLIENT_ID, PYEAR, PMONTH, SUMMA`), но без `CFO` и `VERSION`, на которых построен запрос. Следствие: колонка «утв. тактика» пуста, `KLIENT_ID` в строках не заполняется. **Ждёт адрес таблицы от BI.**
- [ ] ⏳ Автосинхронизация RegManager из `DimEmployee` (§12 п.3) — доступ не подтверждён, соответствие заполняется вручную через админ-ручки
- [ ] ⏳ Сокращённый набор 6 показателей BYN/USD (§8) — в ТЗ не перечислен, выбраны денежные; **подтвердить у аналитика**
- [ ] ⏳ База порога 106 % для ФОТ (§5) и параметры «оборотных» магазинов (§12 п.13) — **ждут финблок**
- [ ] ⏳ Аренда этап 2 (фактические договоры) — вне скоупа §5

### [~] Фаза 4. Активация публикации — механизм готов, запись ждёт BI
- [x] Publisher + dry-run + `publish_log` + идемпотентность + контроль «введено = записано» + белый список таблиц (`PLANS_PUBLISH_TARGETS`)
- [x] Отчёт dry-run прямо перечисляет открытые вопросы §12 («ожидает подтверждения BI: какой Параметр… / агрегат vs детализация…») — это и есть инструмент, которым BI подтверждает решения
- [ ] ⏳ Заполнить `publish_mapping` ответами BI и включить `PLANS_PUBLISH_ENABLED=1`
- [ ] ⏳ Уникальный индекс приёмника по (Параметр, Страна, КодЦФО, КодPL, Дата) — запрос к BI; до него идемпотентность держится DELETE+INSERT в транзакции

### Сквозной прогон на живой БД (24.08.2026)
Собранный бинарь API против чистого Postgres 16 + Redis, `PLANS_MOCK=1`:
создание 6 карточек периода (МП large/small + розница ×4) · реестр условий с diff ·
инверсный пересчёт (доля прямых затрат в обороте 25,65 % — как в ТЗ) · маршрут
1.1 → Финансист → 1.2 → 1.3 → 1.4 с возвратом и обязательным комментарием ·
блокировка записи после утверждения · reopen с причиной · dry-run публикации с
перечнем вопросов к BI · форма розницы (17 магазинов) · «индекс роста» с
предпросмотром (30 ячеек изменится, 2 магазина «новый/ххх» пропущены).

Прогон нашёл четыре дефекта, которые не поймались бы юнит-тестами: изменяющий CTE
в `form_card` (SELECT не видел вставленную строку — карточки не создавались),
пересчёт игнорировал `calc_mode`, МП-11 дублировалось по площадкам, нестабильный
порядок листа согласования. Все исправлены.

### Проверка форм в браузере на поднятом контуре (24.08.2026)
Контур поднят с ИЗОЛИРОВАННЫМ именем проекта: `docker compose -p finance …`.
Без `-p` compose берёт имя проекта из каталога (`swarm`) и конфликтует с
MP-кабинетом — он останавливает чужой `swarm-postgres-1`. Makefile этого не
делает, поэтому до правки таргетов поднимать контур руками с `-p finance`.

Проверено в UI (Chrome DevTools): карточки периода на странице периода (6 форм:
МП large/small + розница ×4) · реестр условий площадок с diff «было → стало» и
бейджем «28 сверх порога — нужно обоснование» · пересчёт расходной части кнопкой
(итоги + замечания) · общие затраты по 7 группам с подытогами · панель процесса
(текущий/следующий шаг, отправка, лист согласования, версии) · отчёт публикации
dry-run с блоками «ожидает подтверждения BI» и «нет правила маппинга» · маршрут
форм с выключенным шагом «Финансист» · форма МП в режиме «расчёт от условий»
с долями под статьями · экран заданий · свод периода.

Через API на живых данных: ввод плана розницы (индикаторы пересчитались —
LFL 33,30 %, LFM 9,49 %, % вып. 83,27 %), 4 контрольные сверки = 0,
валидации V-01 блокируют незаполненную форму.

Найдено и исправлено при проверке:
- МП-11 блокировал отправку из-за ЛЮБОГО расхождения названия статьи со
  справочником (7 блокировок на стилистике). Теперь блокируют только коды 52/54,
  где расходится смысл статьи (ТЗ §8.1, §12 п.10), остальное — предупреждение.
- Шапка формы МП округляла эффективную ставку НДС до «20 %» вместо «20,36 %».

Известное поведение (не регресс): в консоли браузера hydration mismatch на всех
страницах кабинета — SSR не видит токен из localStorage. Воспроизводится и на
страницах, существовавших до этих работ.

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
