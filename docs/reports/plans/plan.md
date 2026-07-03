# PLAN — Модуль «Тактические планы», MVP-вертикаль TPL-MP (Маркетплейсы)

> Источник требований: `docs/reports/plans/SPEC.md`. Режим: план, без правок кода.
> Дата: 2026-06-25. Ветка: `add-plan`.
> Положен рядом со спекой (а не в `tasks/`), т.к. корневые `tasks/plan.md` /
> `tasks/todo.md` заняты планом ВГО-отчёта (закоммичены, текущая работа ветки).
> Список задач — `docs/reports/plans/todo.md`.

---

## 1. Что строим (один абзац)

Раздел «Тактические планы» в основном контуре (Go-API + Nuxt + Postgres `finance`).
MVP — рабочая форма **TPL-MP (Маркетплейсы)** этапа 1.1: пользователь (внутренний
финансист или внешний менеджер площадки) открывает форму своего ABAC-среза (large
= группа 250 / small = 480), видит **read-only факт и стратегию** (онлайн из
OLAP/SQL `Источник_МП`, fallback `PLANS_MOCK`), **вводит тактику** по блокам
продаж/себестоимости/СПП по площадкам и месяцам, переключает валюту (BYN/RUB/USD),
оставляет обязательные комментарии при корректировке, импортирует/экспортирует
Excel. Каждое значение тактики ложится в `pl_metric`, полный снимок формы — в
`form_submission.json_payload`. Движок расчётов CALC, своды по ЮЛ и согласование
1.2–4 — **этап 2** (описаны в SPEC §11–12, в MVP не реализуются).

Базовые паттерны (эталон — `go/internal/reports/debt/`): роутер `http.NewServeMux`
(`mux.HandleFunc("METHOD /api/...", auth.RequireRole(...))`), pgx-репозиторий,
`writeJSON/writeErr`, Nuxt composable `$fetch(base + ...)` с Bearer, design-system
токены, `scope-guard` + `useScope`.

---

## 2. Граф зависимостей

```
VS0  Каркас: роли PLANS_* + scope + меню + пустая /plans + /api/plans/health
 │     (сквозная проводка auth→route→guard→меню→страница, без данных)
 ▼
VS1  Справочники dir_marketplace/dir_cfo(MP)/dir_pl_line  ──┐  (миграции 0010/0012 + загрузка из Excel)
 │     платформы 335/336/337/954, статьи code_pl            │
 ▼                                                          │
VS2  Факт-коннектор Источник_МП (online + PLANS_MOCK) ──────┤── CHECKPOINT A (суммы = прототип)
 │     GET /api/plans/mp/fact → read-only таблица на форме   │
 ▼                                                          │
VS3  Ядро записи: pl_instance + pl_metric + form GET/PUT ◄──┘  (миграция 0010)
 │     ввод тактики → pl_metric + form_submission (round-trip)
 ├───────────────┬──────────────────────────────┐
 ▼               ▼                               ▼
VS4 ABAC         VS5 Комментарии+корректировки   VS6 Импорт/экспорт Excel
 plans_user_scope  (COM-01/ADJ-02, обязательно)    (TPL-06, валидация ABAC)
 фильтр срезов     ── CHECKPOINT C (ABAC)           ── CHECKPOINT B (round-trip)
 ▼
VS7  Валюта (dir_fx_rate, переключатель) + включение сегмента small
       ── CHECKPOINT D (валюта = колонки H/I/J × курсы)
 ▼
=== РЕЛИЗ MVP ===
 ▼
ЭТАП 2 (вне этого плана): CALC-движок · workflow 1.2–4 · своды 1.6/2.4 (TPL-08) ·
       TPL-TO-RETAIL/CFO-EXP/WHOLESALE/IM/PROD-MINUTES/STRATEGY · аудит · cron-синк
```

Критический путь: **VS0 → VS1 → VS3 → VS4**. VS2 параллелится с VS1/VS3 (read-path
независим от write-path до момента отрисовки одной таблицы). VS5/VS6 зависят только
от VS3. VS7 — последним (нужны VS2 валютные суммы и VS3 запись).

---

## 3. Принцип нарезки

Каждый VS — **вертикальный срез** (миграция → Go repo/service/handler → маршрут →
Nuxt composable → компонент → страница), дающий наблюдаемый результат в браузере и
проверяемый `go test` + ручным smoke. Не делаем «сначала все миграции, потом весь
Go, потом весь фронт».

Инварианты для каждого VS (из SPEC §23 Boundaries):
- Новые env → сразу в `.env.example` с комментарием (тот же коммит).
- Стили только из design-system; числа `.col-num` (mono + tabular-nums).
- В UI — `name_cfo`, не `code_cfo`; ABAC проверяется на сервере, не только в UI.
- Не трогать корневой `SPEC.md`, `tasks/*` (ВГО), `python/cost/`.

---

## 4. Задачи (вертикальные срезы)

### VS0 — Каркас раздела (walking skeleton)
**Зависит от:** —
**Файлы:** `go/internal/auth/roles.go`; `go/internal/plans/handler.go` (заглушка
`Health`); `go/cmd/api/main.go` (роут); `nuxt/composables/useScope.ts` (scope
`plans`); `nuxt/middleware/scope-guard.ts` (ветка `/plans`); `nuxt/app.vue` (пункт
меню); `nuxt/pages/plans/index.vue` (заглушка дашборда); `.env.example` (блок
«Тактические планы»).
**Делаем:**
- В `roles.go`: `RolePlansAdmin = "ROLE_PLANS_ADMIN"`, `RolePlansUser =
  "ROLE_PLANS_USER"`; в `Allowed`; `hierarchy[RoleAdmin] += RolePlansAdmin`,
  `hierarchy[RolePlansAdmin] = {RolePlansUser}`.
- `useScope.ts`: scope `plans` → `["ROLE_ADMIN","ROLE_PLANS_ADMIN","ROLE_PLANS_USER"]`.
- `scope-guard.ts`: `path.startsWith("/plans")` → требует `hasScope("plans")`.
- `app.vue`: пункт «Тактические планы» (`to="/plans"`, иконка
  `lucide:clipboard-list`), виден при `hasScope('plans')`.
- Go: `GET /api/plans/health` → `auth.RequireRole(authSvc, auth.RolePlansUser, plansH.Health)` → `{"ok":true}`.
**Критерии приёмки:**
- Пользователь с `ROLE_PLANS_USER` видит пункт меню и открывает `/plans`; без роли —
  редирект на `/`.
- `GET /api/plans/health` → 200 для PLANS_USER, 403 без роли, 401 без токена.
**Проверка:**
```bash
cd go && go build ./... && go vet ./... && go test ./internal/auth/...
# роль через PUT /api/admin/users/{id}/roles (ROLE_PLANS_USER), затем:
curl -H "Authorization: Bearer <token>" http://api.finance.local/api/plans/health   # {"ok":true}
curl -H "Authorization: Bearer <token-без-роли>" .../api/plans/health               # 403
# UI: make up → залогиниться плановым юзером → виден пункт, /plans открывается
```

---

### VS1 — Справочники TPL-MP (`dir_marketplace`, `dir_cfo`, `dir_pl_line`) ✅
**Зависит от:** VS0
**Реализация (миграции дробятся по VS, нумерация инкрементальная):**
`migrations/0010_plans_directories.{up,down}.sql` (таблицы `plans_directory`,
`plans_directory_row` + метаданные справочников). **Ядро** (`pl_instance`,
`pl_metric`…) — отдельной миграцией `0011_plans_core` в VS3, НЕ здесь.
`go/internal/plans/seed_mp.go` (seed-данные площадок/статей + `SeedSource`);
`handler.go` (`GET /api/plans/directories`, `GET /api/plans/directories/{code}/rows`);
`go/cmd/api/main.go` (роуты за `RequireRole(PlansUser)`).
**Делаем:**
- Таблицы `plans_directory`, `plans_directory_row` (SPEC §7.3); строки MVP — из
  seed в коде (статичная НСИ), таблицы — под будущую синхронизацию/ABAC.
- Seed `dir_marketplace`: large 335/336/337/954, small 953/955/957/959/990/991/958/
  475/474/338/339 (+страна, сегмент, group code 250/480) — из SPEC §26-A.
- Seed `dir_pl_line` (блоки продаж/себестоимости 1046/1045/1022/1006/8006/2006/6006 +
  статьи затрат) и `dir_cfo` — MP-подмножество.
- API чтения справочников (реестр + строки по коду; 404 на неизвестный).
**Критерии приёмки:**
- `GET /api/plans/directories/dir_marketplace/rows` отдаёт ≥4 площадки large с
  `name_cfo`, `code_cfo`, `segment`, `country`.
- `dir_pl_line` содержит 1046/1045/1022/1006/8006 с наименованиями.
**Проверка:**
```bash
../swarm/migrate.dev.sh up && make migrate
cd go && go test ./internal/plans/...     # тест загрузчика/seed
curl .../api/plans/directories/dir_marketplace/rows | jq '.[] | select(.segment=="large")'
make psql -> SELECT code, sync_status FROM plans_directory;
```

---

### VS2 — Факт МП (онлайн `Источник_МП` + mock) → read-only на форме · **CHECKPOINT A**
**Зависит от:** VS0, VS1 (справочники для подписей строк)
**Файлы:** `go/internal/plans/sources/olap_mp.go` (online коннектор `ALL_view_МП`),
`sources/mock_mp.go` (фикстуры из чисел прототипа), `sources/source.go` (интерфейс +
выбор по `PLANS_MOCK`); `service.go` (`MpFact`); `handler.go` (`GET /api/plans/mp/fact`);
`nuxt/composables/usePlans.ts`; `nuxt/components/plans/MpFactTable.vue`;
`nuxt/pages/plans/mp.vue` (временная страница просмотра факта); `.env.example`
(`PLANS_MOCK`, `PLANS_OLAP_*`, `PLANS_SQL_PAYMENTS_DSN`).
**Делаем:**
- Интерфейс `MpFactSource.Fetch(year, month, segment) → []FactRow` (ключи
  SUMIFS: month+code_cfo+scenario+CodePL; суммы BYN/RUB/USD).
- `PLANS_MOCK=1` → `mock_mp.go` (контрольные числа из SPEC §25: WB/335/1046/2026-05
  = 357 034 569.85; СПП WB 2026-06 = 0.31).
- `GET /api/plans/mp/fact?year&month&segment` → факт по площадкам/блокам.
- Таблица на форме: строки = блоки×площадки, колонка факт (read-only, `.col-num`).
**Критерии приёмки:**
- При `PLANS_MOCK=1` `/api/plans/mp/fact?segment=large&month=5&year=2026` отдаёт WB
  1046 = 357034569.85 (или в выбранной валюте).
- UI рисует read-only факт; редактировать нельзя.
**Проверка:**
```bash
PLANS_MOCK=1 -> curl '.../api/plans/mp/fact?year=2026&month=5&segment=large' | jq
cd go && go test ./internal/plans/sources/...
# CHECKPOINT A: при live (PLANS_MOCK=0) суммы сверить с прототипом Маркетплейсы_large
#   (если расхождение — live в прод не включать, держать PLANS_MOCK=1)
```

---

### VS3 — Ядро записи: `pl_instance` + `pl_metric` + форма GET/PUT (round-trip)
**Зависит от:** VS1 (справочники), VS2 (факт для отрисовки колонок)
**Файлы:** `migrations/0010_plans_core` (если не создано в VS1 — таблицы
`pl_instance`, `pl_stage_instance`, `pl_metric`, `form_submission`); `go/internal/
plans/repo.go` (CRUD instance/metric/submission), `service.go` (`MpForm`,
`SaveMpForm`); `handler.go` (`GET/PUT /api/plans/mp/form`); `usePlanForm.ts`;
`nuxt/components/plans/MpForm.vue` (редактируемая матрица); `nuxt/pages/plans/[id]/
mp/[segment].vue`.
**Делаем:**
- Сборка матрицы формы (SPEC §13 контракт `GET /mp/form`): площадки × блоки ×
  месяцы (M−1/M/M+1) × сценарий; факт/стратегия read-only, тактика editable.
- `PUT /mp/form`: editable-ячейки → upsert в `pl_metric` (ключ SPEC §9.8); полный
  снимок → `form_submission.json_payload`.
- Создание/получение `pl_instance` на период (если нет — `POST /api/plans/instances`).
- Vue: editable input только в колонках «Тактика бюджет (таргеты)»; подсветка
  изменённых ячеек.
**Критерии приёмки:**
- Ввод тактики WB 1046 на M → `PUT` → перезагрузка `GET` возвращает то же значение;
  строка есть в `pl_metric`; снимок есть в `form_submission`.
- Факт/стратегия в UI недоступны для ввода.
**Проверка:**
```bash
cd go && go test ./internal/plans/...   # round-trip репо/сервиса
# UI: открыть /plans/<id>/mp/large, ввести значение, Сохранить, перезагрузить
make psql -> SELECT line_code, profit_center, amount FROM pl_metric WHERE pl_id=<id>;
make psql -> SELECT jsonb_array_length(json_payload->'rows') FROM form_submission ...;
```

---

### VS4 — ABAC-срез (`plans_user_scope`) · **CHECKPOINT C**
**Зависит от:** VS3
**Файлы:** `migrations/0012_plans_directories` (таблица `plans_user_scope`);
`go/internal/plans/abac.go` (резолв среза пользователя → allowed `code_cfo`/segment/
country); применение в `service.MpForm`/`SaveMpForm` (фильтр + отклонение чужих);
`handler.go` (админ-эндпоинт назначения среза `PUT /api/plans/scope/{user_id}`,
роль `ROLE_PLANS_ADMIN`).
**Делаем:**
- Резолв: роль + `plans_user_scope` → набор разрешённых площадок/сегмента/страны.
- `GET /mp/form` отдаёт только разрешённые строки; `PUT` отклоняет ячейки чужого
  среза (403/ошибка строки).
- В UI — только `name_cfo`.
**Критерии приёмки:**
- Пользователь сегмента small **не видит** large на API (пустой/403), не только в UI.
- Менеджер с площадкой 335 не может записать тактику по 337.
**Проверка:**
```bash
cd go && go test ./internal/plans/...     # negative ABAC-кейсы
curl -H "Bearer <small-user>" '.../api/plans/mp/form?segment=large'   # пусто/403
# CHECKPOINT C: матрица доступа SPEC §5.2/5.3 воспроизведена
```

---

### VS5 — Комментарии и корректировки (COM-01 / ADJ-02 обязательны)
**Зависит от:** VS3
**Файлы:** `migrations/0011_plans_workflow.{up,down}.sql` (`pl_comment`,
`pl_adjustment`); `go/internal/plans/repo.go`+`service.go` (comments/adjust);
`handler.go` (`GET/POST /api/plans/instances/{id}/comments`, `POST .../adjust`);
`usePlans.ts`; `nuxt/components/plans/CellComment.vue` (поповер на ячейке).
**Делаем:**
- При ручной корректировке (`is_manual=true`) — **обязательное** поле «Причина»
  (`pl_adjustment.reason`) + комментарий (`pl_comment`); хранить
  `original_calculated`/`adjusted_value` (ADJ-03).
- Статусы комментариев `open/resolved`; `@login` mentions.
**Критерии приёмки:**
- `PUT /mp/form` с `is_manual=true` без причины → 400; с причиной → сохраняет
  adjustment.
- Скорректированные ячейки визуально помечены (ADJ-04).
**Проверка:**
```bash
cd go && go test ./internal/plans/...
curl -X PUT .../mp/form -d '{...is_manual:true, без reason}'   # 400
make psql -> SELECT reason, adjusted_value FROM pl_adjustment;
```

---

### VS6 — Импорт/экспорт Excel TPL-MP (TPL-06) · **CHECKPOINT B**
**Зависит от:** VS3 (формат формы), VS4 (валидация ABAC)
**Файлы:** `go/internal/plans/importexport.go`; `handler.go`
(`GET /api/plans/mp/export`, `POST /api/plans/mp/import`); кнопки в
`nuxt/pages/plans/[id]/mp/[segment].vue` (Excel ↓ / Excel ↑).
**Делаем:**
- Экспорт: снимок формы → `.xlsx` с именами колонок прототипа (площадка, code_pl,
  месяцы, валюта). Решить движок: Go-xlsx-библиотека или zip+XML (зафиксировать в T).
- Импорт: только editable-колонки; обязательны `code_cfo + code_pl + month + reason`;
  неизвестный код → ошибка строки; чужой ABAC → отклонение файла.
**Критерии приёмки:**
- Экспорт → импорт обратно даёт идентичные значения тактики (round-trip).
- Импорт файла с чужим `code_cfo` (вне ABAC) отклоняется целиком.
**Проверка:**
```bash
cd go && go test ./internal/plans/...   # round-trip + валидация
# UI: Экспорт → правка одной ячейки → Импорт → значение применилось, остальные равны
# CHECKPOINT B: значения после round-trip == значения до
```

---

### VS7 — Валюта (`dir_fx_rate`) + включение сегмента small · **CHECKPOINT D**
**Зависит от:** VS2 (валютные суммы), VS3 (запись)
**Файлы:** `migrations/0012` (если `dir_fx_rate` не создан); `go/internal/plans/
currency.go` (пересчёт BYN/RUB/USD по `dir_fx_rate`); применение в `service.MpForm`;
переключатель в шапке `MpForm.vue`; включение `segment=small` (тот же шаблон, ABAC
группа 480).
**Делаем:**
- Хранить суммы в трёх валютах (как `Источник_МП` H/I/J); переключатель шапки
  выбирает отображение; курсы тактики из `dir_fx_rate` (помесячно, RUB/USD).
- Включить small как полноценный сегмент (площадки SPEC §26-A, страны RU/KZ/UZ).
**Критерии приёмки:**
- Переключение RUB/BYN/USD меняет числа согласованно с курсами `dir_fx_rate`.
- Форма small открывается для своего ABAC-среза (группа 480), площадки KZ/UZ видны.
**Проверка:**
```bash
cd go && go test ./internal/plans/...
# UI: переключить валюту → суммы пересчитались; открыть /plans/<id>/mp/small
# CHECKPOINT D: BYN/RUB/USD == колонки H/I/J источника × курсы
```

---

## 5. Чекпоинты между фазами

| Чекпоинт | После | Что сверяем | Действие при провале |
|---|---|---|---|
| **A** (факт) | VS2 | Суммы live-OLAP == прототип `Маркетплейсы_large` (WB/335/1046, СПП) | Держать `PLANS_MOCK=1`, live в прод не включать |
| **B** (round-trip) | VS6 | export→import даёт идентичные значения | Чинить парсер/маппинг колонок до релиза |
| **C** (ABAC) | VS4 | large↔small изоляция на API; чужая площадка не пишется | Блокер релиза (безопасность) |
| **D** (валюта) | VS7 | BYN/RUB/USD согласованы с H/I/J × курсы | Чинить `dir_fx_rate`/пересчёт |

Перед каждым коммитом (база для Go-задач):
```bash
cd go && go build ./... && go vet ./... && go test ./...
```
Миграции — парные `.up/.down`, нумерация после `0009`; накат
`../swarm/migrate.dev.sh up` (golang-migrate, обе БД) или `make migrate`.

---

## 6. Что НЕ входит в MVP (этап 2, см. SPEC §11–12, §24)

Движок CALC: формулы каскада наценки/маржи/долей + ФОТ/аренда/логистика; **гибрид
формул (D11)** — дефолт `calc_rule` (версионируемые) + per-срез override
`pl_formula_override` (с причиной), безопасный вычислитель выражений
(`go/internal/plans/calc/`), drill-down «в формулу» + лог расчёта (CALC-03).
Workflow 1.2–4 (согласование, зависимости WF-DEP, своды 1.6/2.4 TPL-08) с
**календарями р.д. по странам** (`plans_country_calendar`, глобальные настройки
проекта; дедлайн этапа — по стране ответственного). Шаблоны TPL-TO-RETAIL /
TPL-CFO-EXP / TPL-WHOLESALE / TPL-IM / TPL-PROD-MINUTES / TPL-STRATEGY; копирование
сценария TPL-09; конфигураторы процессов/шаблонов; полный аудит
(`PLANS_AUDIT_ENABLED`); cron-синхронизация справочников.

---

## 7. Открытые вопросы (не блокируют MVP, нужны к этапу 2)

- ✅Q1 — **закрыто:** креды OLAP = сервер **FinDWH** (переиспользуем `MSSQL_PREMASTER_*`).
  Уточнить только имя объекта `ALL_view_МП` в FinDWH (`cmd/mssql-probe`); до этого `PLANS_MOCK=1`.
- ✅Q2 — **закрыто:** авторизация = **B24 OAuth**. Под-вопрос: заведены ли внешние менеджеры в B24 (иначе локальные учётки).
- ✅Q3 — **закрыто:** календари р.д. **по странам** (BY/RU/KZ/UZ), ведутся вручную глобально
  (`plans_country_calendar`); дата этапа — по календарю страны ответственного. Реализация — этап 2 (workflow).
- 🔶Q4 — модель редактирования формул **закрыта (D11, гибрид:** дефолт `calc_rule` + per-срез override`)`;
  остаётся ❓Q4b — снять точные выражения каскада с автора прототипа (формулы в Excel скрыты).

---

## 8. Ревью

План на проверку человеком. Подтвердить:
1. Нарезку MVP (VS0–VS7) и критический путь VS0→VS1→VS3→VS4.
2. Размещение плана/спеки в `docs/reports/plans/` (а не в занятых `tasks/*`).
3. Стартовать с VS0 (каркас) — даёт сквозную проводку до первого кода данных.
