# SPEC — ВГО-отчёт на GLMF → ClickHouse (два потока)

> Статус: на согласование. Дата: 2026-06-24. Ветка: `add-action`.
> Заменяет направление finpl/Table_Fin_PL (тот SPEC — в git, commit `4c9ece3`;
> finpl-код остаётся opt-in, см. решение #G). Источник истины по фактам разведки:
> `docs/reports/debt/prod-verification.md`, `finpl-merge.md`, memory
> `debt-table-fin-pl-source`.

---

## 1. Objective (цель)

Сделать отчёт «Задолженность ВГО» на **каноническом полном источнике GLMF**,
синхронизированном в **ClickHouse двумя независимыми потоками**, чтобы отчёт работал
быстро и показывал каноничную классификацию + **названия договоров**:

- **`fact_glmf`** ← поток из `vGLMFAddUSD` (= GLMF + USD): числа, классификация
  (`GroupPL/CodePL`), ИНН (`CompanyID/CounterpartyID`), счета, `ICO`, `DocID`,
  дневная дата. Инкремент по `DateOfLoad`.
- **`dim_contract`** ← отдельный поток из субконто `Premaster1C`(+`History`): `doc_id →
  contract_ref, contract_name` (через `Objects`). Грануляр. — **1 договор на `DocID`**.
- Отчёт через `DEBT_BACKEND=ch`: выручка (`group_pl='ПРОДАЖИ'`), ДЗ/КЗ-сальдо
  (62/60/76/1210/3310…), `LEFT JOIN/dictGet dim_contract` по `doc_id`.

**Пользователи:** финотдел (роль `ROLE_FINANCE_ADMIN`), отчёт `/api/reports/debt/*`.

---

## 2. Контекст: факты разведки OLAP (почему именно так)

| Источник | Полнота | Класс-ция | ИНН | Договор | Грануляр. |
|---|---|---|---|---|---|
| **GLMF** (`vGLMFAddUSD`) | ✅ полный, свежий (загрузка ежедн.) | ✅ `GroupPL` | ✅ | ❌ нет | дневная + `DocID` |
| Premaster1C | ⚠ неполон по 2025 (MF·01 = 101 vs GLMF 1.1М) | через join | ✅ | ✅ субконто | дневная |
| Table_Fin_PL | ⚠ ОПУ, без ID, месяц | ✅ +ручные правки | ❌ | ❌ | месяц |

- **`GLMF = Premaster1C + классификация`** (`GLMF_exec`: TRUNCATE+INSERT ⋈ `[002 CodePL]`,
  `CompaniesMF`, `Objects`). USD добавляет view `vGLMFAddUSD`.
- **Договор живёт только в субконто Premaster** (62→`DrSubconto2`, 60/76→`CrSubconto1`)
  → `Objects.ID = subconto → Objects.Name`. `DimSubkonto` — это статьи расходов, НЕ карта
  договора.
- **Ключ связи `DocID` идентичен** в GLMF и Premaster (проверено). **1 договор на `DocID`**
  (проверено: 60/76 — 22 дока × 1 договор; 62 — max 1).
- Слепой резолв даёт мусор → нужна **эвристика-фильтр** имени
  (`Договор|Соглашен|Оферт|Контракт|№|\d+/\d+|от ДД.ММ.ГГГГ`; отсечь
  `00БС|ТДБП|Оказание|Реализаци|Поступлени`).

---

## 2bis. Требования постановщика (ТЗ + приложение + картинки)

Распарсено из `var/fin/ТЗ_задолженность ВГО.docx` (+3 встроенные картинки) и
`Приложение_к_ТЗ_задолженность_ВГО.xlsx` (4 листа). Полный разбор:
**`docs/reports/debt/tz-requirements.md`**. Это авторитет по UI/формулам.

**Колонки отчёта (лист «вид»):** Счёт БУ, Наименование счёта, **Субсчёт БУ** (напр.
`60.01`), Наименование субсчёта, Компания, Компания партнёр, Договор, Отсрочка по
договору, Дата оплаты по договору, **Валюта**, [На начало: ДЗ/КЗ], [Обороты: ДЗ/КЗ],
[На конец: ДЗ/КЗ], [Выручка: за период/за последний месяц], Задолженность в днях.
(«Счёт БУ/Наименование» — только при группировке по счёту.)

**Группировка (4 уровня):** Компания–Партнёр → Счёт БУ → Договор → Документ операции.

**Выручка — ТОЧНЫЕ корреспонденции по странам (эталон ТЗ):**
РБ `Дт 62.1 Кт 90.1.1`; РФ `Дт 62 Кт 90.01` + `Дт 76.09 Кт 90.01`;
КЗ `Дт 1210 Кт 6010`; УЗ `Дт 4015 Кт 9010`. Только по контрагентам ГК.
«За последний месяц» — та же логика за последний календарный месяц от даты формирования.

**Срок/просрочка:** Отсрочка = «Срок оплаты» договора в 1С (image3: «в течение 30 дней»
→ `Payments.Docs.Delay`). Дата оплаты = дата операции + отсрочка (только на «Документ»).
Задолженность в днях = от (операция + срок) до отчётной даты.

**Что меняет дизайн (учтено в решениях ниже):** валюта — построчное измерение
(BYN/RUB/USD/EUR/CNY), субсчёт-уровень (полный `DrAcc/CrAcc`), выручка по точным Дт/Кт.

## 3. Решения (согласовано)

| # | Вопрос | Решение |
|---|---|---|
| A | Fact-таблица | **новая `fact_glmf`**; `fact_premaster` — для отката, потом ретайрим |
| B | Источник extract | **`vGLMFAddUSD`** (GLMF + USD), инкремент по `DateOfLoad` |
| C | Суммы | **все 6**: WOVAT/WithVAT × BelRub(BYN)/Currency(нац)/USD |
| C2 | **Валюта — построчное измерение** (ТЗ) | строка на (счёт×компания×партнёр×ВАЛЮТА); валюты BYN/RUB/USD/EUR/CNY. ⚠ источник валюты в данных не найден (gate §10.7) |
| C3 | **Уровень — субсчёт** (ТЗ) | полный `DrAcc/CrAcc` (60.01), не только корень |
| D | Выручка | по ТОЧНЫМ Дт/Кт ТЗ (РБ 62.1/90.1.1; РФ 62/90.01+76.09/90.01; КЗ 1210/6010; УЗ 4015/9010); `group_pl='ПРОДАЖИ'` — прокси, если сходится (gate §10.1) |
| E | ДЗ/КЗ-сальдо | signed-сальдо по корням 62/60/76/1210/3310 (как в текущем CH/MSSQL) |
| F | `dim_contract` охват | **62 + 60/76 + КЗ/УЗ**, +`Premaster1CHistory` для старых периодов |
| G | finpl-код | **оставить opt-in**; развиваем GLMF→ch; дефолт пока `mssql`, на ch — после сверки |
| H | Договор-грануляр. | **`doc_id → contract`** (1:1), без `rw_nm` |

---

## 4. Архитектура CH (два потока)

```
vGLMFAddUSD ──extract (инкремент DateOfLoad)──►  finance.fact_glmf
   ReplacingMergeTree(date_of_load)
   company_id, counterparty_id, doc_id, num, date, month,
   dr_acc, cr_acc, dr_acc_root, cr_acc_root, code_pl, group_pl, ico, country,
   amt_wovat_byn, amt_withvat_byn, amt_wovat_cur, amt_withvat_cur,
   amt_wovat_usd, amt_withvat_usd, doc_name_1c, operation_description

Premaster1C(+History) субконто ──extract (DISTINCT по DocID)──►  finance.dim_contract
   ReplacingMergeTree   doc_id (PK) → contract_ref, contract_name, account_kind
   (62→DrSubconto2, 60/76→CrSubconto1, КЗ/УЗ→TBD; Objects.Name; эвристика-фильтр)

repo_clickhouse (DEBT_BACKEND=ch):
   SELECT … FROM fact_glmf
   LEFT JOIN dim_contract USING(doc_id)   -- или dictGet('dim_contract', …, doc_id)
   WHERE ico=1 AND … (выручка group_pl='ПРОДАЖИ' | ДЗ/КЗ по acc_root)
```

- **Развязка потоков:** дыры в Premaster (договор) не ломают числа GLMF — нет договора →
  строка «без договора», числа целы.
- **`dim_contract` крошечная** (1 строка на документ), грузится отдельно, дёшево.
- В CH тяжёлый строковый join `DocID`↔субконто **не нужен в рантайме** — `dim_contract`
  собирается ETL'ом заранее; в отчёте — быстрый lookup.

---

## 5. Маппинг `DebtRow` → источник (provenance)

| Поле | Источник |
|---|---|
| Country, Company/CompanyINN, Partner/PartnerINN | `fact_glmf` (ИНН напрямую, без fuzzy) |
| Субсчёт (DrAcc/CrAcc), Наименование | `fact_glmf` (полный счёт, не корень) |
| RevenuePeriod / RevenueLastMonth | `fact_glmf` WOVAT по ТОЧНЫМ Дт/Кт ТЗ (§2bis), сверка с `group_pl='ПРОДАЖИ'` |
| Opening/Turnover/Closing ДЗ/КЗ | `fact_glmf` signed-сальдо WithVAT по 62/60/76/1210/3310 |
| **Валюта** (построчно) | ⚠ источник в данных НЕ найден — gate §10.7 (валюта договора / субконто / ExRate?) |
| Суммы (BYN/нац/USD) | `fact_glmf` |
| Contract / ContractRef | `dim_contract` по `doc_id` |
| PaymentTerm (Отсрочка) / Дата оплаты / Задолженность в днях | `Payments.Docs.Delay` (срок оплаты 1С, image3); дата оплаты = операция+отсрочка (только «Документ») |
| Manager / Channel | `Counterparty1C` (опц., как сейчас) |

---

## 6. Commands

```bash
# из swarm/
make up                      # Go + Nuxt + PG + Redis + clickhouse
make restart-go-api          # перечитать env
make logs-go

# ClickHouse миграции (по одному statement'у — HTTP CH не берёт multi-statement)
swarm/migrate-clickhouse.sh  # применить clickhouse/migrations/*.up.sql

# ETL (admin-эндпоинты + воркер, как сейчас для fact_premaster)
#   bootstrap fact_glmf  / incremental по DateOfLoad
#   bootstrap dim_contract (отдельный поток)

# Отчёт через новый источник
DEBT_BACKEND=ch              # читает fact_glmf + dim_contract

# Разведка OLAP (одноразовый probe, удалять после)
#   cmd/mssql-probe (PROBE_SQL=...) или временный go/cmd/<tmp>; ретраи на handshake
```

---

## 7. Project structure (что добавляем/трогаем)

```
clickhouse/migrations/
├── 004_fact_glmf.up.sql            (НОВ) таблица fact_glmf
├── 005_dim_contract.up.sql         (НОВ) таблица dim_contract (+ dictionary опц.)
go/internal/etl/
├── extract_glmf.go                 (НОВ) SELECT из vGLMFAddUSD (все суммы, class.)
├── extract_contract.go             (НОВ) DISTINCT DocID→субконто→Objects (+эвристика)
├── bootstrap.go / incremental.go   (~)   ветки для fact_glmf (watermark=DateOfLoad)
├── ch.go                           (~)   INSERT в fact_glmf / dim_contract
go/internal/reports/debt/
├── repo_clickhouse.go              (~)   SELECT из fact_glmf + JOIN dim_contract;
│                                          выручка group_pl='ПРОДАЖИ', ДЗ/КЗ по acc_root
├── chart_of_accounts.go            (~)   корни ДЗ/КЗ для КЗ/УЗ (1210/3310/6000)
go/internal/config/config.go        (~)   env для fact_glmf/источника (см. §9)
go/cmd/api/main.go                  (~)   wiring ch-бэкенда на fact_glmf
.env.example                        (~)   новые env тем же коммитом
docs/reports/debt/                  (~)   обновить clickhouse-design.md, prod-verification.md
```

Frontend (`nuxt/`) — не трогаем (контракт `DebtRow` стабилен).

---

## 8. Testing strategy

- **Раннер есть** (`go test ./...`); тесты пишем рядом (TDD: extract-маппинг, эвристика
  договора, signed-сальдо, классификация выручки).
- **Юнит**: чистые функции (эвристика-фильтр имён, acc-root, разворот сальдо, маппинг строк
  CH→`DebtRow`) — без БД.
- **Контракт**: ответ `ReportResponse/DebtRow` неизменен → UI без правок.
- **Сверка на проде** (gate, см. §10): суммы выручки/ДЗ/КЗ на 1–2 ЮЛ × месяц против
  офиц. ОПУ/оборотки; покрытие договоров по счетам.
- **ETL-идемпотентность**: повторный incremental не дублирует (ReplacingMergeTree + FINAL/argMax).

---

## 9. Code style / boundaries

### Always
- **Новая env → в `.env.example` тем же коммитом** (правило проекта). 
- Источник — за интерфейсом `PremasterRepo`; `BuildReport` не дублировать.
- Идентификаторы из env — через валидацию (анти-инъекция), именованные параметры.
- CH-миграции — **по одному statement'у** через `swarm/migrate-clickhouse.sh` (HTTP CH
  не берёт multi-statement).
- Договор резолвим по карте субконто-по-счёту + **эвристика-фильтр** (не слепой резолв).
- Диалог/комментарии — на русском.

### Ask first
- Позиция субконто для **КЗ/УЗ** счетов (1210/3310/6000) — установить разведкой до кода.
- Переключение дефолта `DEBT_BACKEND` на `ch` — только после сверки (gate §10).
- Ретайр `fact_premaster` — после подтверждения, что `fact_glmf` полнее/корректнее.

### Never
- Не флипать prod-дефолт на ch до сверки сумм.
- Не коммитить временные probe-утилиты.
- Не тащить договор и числа принудительно в одну таблицу/один join в рантайме MSSQL
  (тяжёлый строковый join по 209М → таймаут; для этого и CH с двумя потоками).
- Не трогать cost-раздел (`python/cost/`).

### Новые env (черновик, финал — в `.env.example`)
```
MSSQL_GLMF_VIEW=vGLMFAddUSD          # источник fact_glmf (GLMF + USD)
MSSQL_GLMF_TABLE=GLMF                # база (для dim/диагностики)
ETL_GLMF_WATERMARK_COL=DateOfLoad    # колонка инкремента
# dim_contract источник — Premaster1C(+History) уже покрыт MSSQL_PREMASTER_*
```

---

## 10. Gate / открытые вопросы (до merge в master)

1. **Сверка GLMF-выручки** с офиц. ОПУ/Table_Fin_PL по ЮЛ×месяц (дельта = ручные Блоки;
   <1–2% → CodePL-классификации достаточно). Старт-числа: MF·2025-01 Формэль ≈14.1М BYN.
2. **КЗ/УЗ договор**: позиция субконто (1210/3310/6000) — разведка (DimSubkonto не помог).
3. **ICO-покрытие КЗ/УЗ**: помечены ли внутригрупповые КЗ/УЗ `ICO=1` (на снэпшоте дало 0).
4. **`DateOfLoad` как watermark**: дневная грануляр. версии в ReplacingMergeTree — проверить,
   что ретроактивные правки GLMF подхватываются (полный re-bootstrap как страховка).
5. **Срок/просрочка КЗ/УЗ** в `Payments.Docs`.
6. **Имя ЮЛ `GP`** (190465888) — уточнить.
7. **⚠ Валюта задолженности (построчно, ТЗ) — КОНКРЕТНАЯ валюта НЕ материализована в FinDWH.**
   Разведка (2026-06-24) установила:
   - ✅ Справочник валют: `[SRV-SQL].Checks.dbo.CurrencyDaily` (`curr_id→KOD` ISO:
     840=USD, 643=RUB, 978=EUR, 156=CNY, 398=KZT, 860=UZS) + `CurrencyRates`.
   - ✅ Функц. валюта ЮЛ: `CompaniesMF.CurrID` (BY=1, RU=2, KZ=7, UZ=8…).
   - ⚠ Валютный признак — в субсчёте (`62.21`, `60.21/31/32` = валютные; `62.01/62.Р` =
     функц.), но НЕ конкретная валюта.
   - ❌ `GLMF/Premaster.AmountWOVATCurrency` хранится в **функц. валюте компании**, не в
     валюте долга: у ТД `ExRate≈0.037` (RUB→BYN) для ВСЕХ строк → транзакционная валюта
     нормализована прочь.
   **Вывод:** построчную «валюту долга» (USD/EUR/CNY) из GLMF/Premaster получить нельзя.
   Варианты: (A) попросить аналитика добавить транзакционную валюту в GLMF из 1С-источника
   (рекоменд.); (B) тянуть валюту из источных 1С-баз (тяжело, кросс-база); (C) показывать
   только «валютный/функц.» по субсчёту (деградация vs ТЗ). Решение — к постановщику/аналитику.
8. **Выручка по точным Дт/Кт ТЗ vs `GroupPL='ПРОДАЖИ'`** — совпадают ли множества
   проводок (РБ 62.1/90.1.1; РФ 62/90.01+76.09/90.01; КЗ 1210/6010; УЗ 4015/9010).

Полный чек-лист и SQL-пробы: `docs/reports/debt/prod-verification.md`.
Требования постановщика (колонки/формулы/картинки): `docs/reports/debt/tz-requirements.md`.
