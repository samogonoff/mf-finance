# PLAN — ВГО-отчёт: GLMF → ClickHouse (два потока)

> Источник требований: `SPEC.md`. Разбор ТЗ: `docs/reports/debt/tz-requirements.md`.
> Факты разведки: `docs/reports/debt/prod-verification.md`. Режим: план, без правок кода.
> Дата: 2026-06-24. Ветка: `add-action`. (Заменяет finpl-план — он в git, commit `8e21daf`.)

---

## 1. Что строим (в одном абзаце)

Отчёт «Задолженность ВГО» переходит на **GLMF** (полный, свежий, классифицированный
GL с ИНН/DocID), материализованный в **ClickHouse двумя независимыми потоками**:
`fact_glmf` (числа/классификация, инкремент по `DateOfLoad`) + `dim_contract`
(`doc_id → договор` из субконто Premaster/History). Отчёт через `DEBT_BACKEND=ch`:
выручка по точным Дт/Кт ТЗ, ДЗ/КЗ-сальдо до субсчёта, `LEFT JOIN dim_contract` по
`doc_id`. Переиспользуем существующий ETL-каркас (`internal/etl/`, `fact_premaster`).
Валюта — **вне скоупа** (BYN-консолидация, §SPEC 10.7).

---

## 2. Граф зависимостей

```
T1  CH-миграция fact_glmf + extract_glmf + bootstrap(1 ЮЛ)        ── CHECKPOINT A
 │      (поток GLMF→CH, числа лежат, counts/sums = источник)
 ├───────────────┬───────────────────────────────────────┐
 ▼               ▼                                         ▼
T2 repo_ch       T4 CH-миграция dim_contract +            T6 incremental
 выручка          extract_contract(62/60/76+эвр.)+          (watermark DateOfLoad)
 (точные Дт/Кт)    bootstrap                                 + CountryByINN(KZ/UZ)
 │               │                                          + bootstrap всех ЮЛ
 ▼               ▼                                          + admin
T3 repo_ch       T5 repo_ch LEFT JOIN dim_contract
 ДЗ/КЗ-сальдо      + drilldown
 (субсчёт)        │
 │   CHECKPOINT B │   CHECKPOINT C
 └───────┬────────┘
         ▼
        T7 config/env + wiring DEBT_BACKEND=ch→fact_glmf
         ▼
        T8 разведка субконто КЗ/УЗ → extend extract_contract (договоры КЗ/УЗ)
         ▼
        T9 cutover: дефолт ch + ретайр fact_premaster + docs   ── CHECKPOINT D
```

**Критический путь:** T1 → T2 → T3 → T5 → T7 → T9.
T4 параллелится после T1; T6 после T1; T8 после T4.

---

## 3. Нарезка — вертикальные срезы

Каждая фаза даёт **демонстрируемый сквозной результат**:
- **T1** — данные ОДНОГО ЮЛ реально лежат в `fact_glmf` (можно сверить с GLMF) ещё до отчёта.
- **T2/T3** — `/report` (на `fact_glmf`) отдаёт выручку, затем ДЗ/КЗ — законченные срезы.
- **T4/T5** — добавляют второй поток (договоры) поверх рабочего отчёта.
- **T6/T7** — масштаб (все ЮЛ, инкремент) + переключение источника.
- На каждом чекпоинте — откат `DEBT_BACKEND=mssql` возвращает текущее поведение (до T9).

---

## 4. Ключевые проектные решения (фиксируем в коде/доках)

1. **Источник extract** — `vGLMFAddUSD` (GLMF + USD). Watermark инкремента — `DateOfLoad`
   (в GLMF нет `DateOfChange`); engine `ReplacingMergeTree(date_of_load)`. Риск дневной
   грануляр. версии (§6) — страховка полным re-bootstrap.
2. **Суммы** — BYN-консолидация (`AmountWOVATBelRubFact`/`AmountWithVATBelRubFact`) + USD
   (`…USDFact`). Выручка — WOVAT, ДЗ/КЗ-сальдо — WithVAT.
3. **Выручка** — точные Дт/Кт ТЗ по странам (РБ 62.1/90.1.1; РФ 62/90.01 + 76.09/90.01;
   КЗ 1210/6010; УЗ 4015/9010). Сверить с `group_pl='ПРОДАЖИ'` (CHECKPOINT B).
4. **ДЗ/КЗ** — signed-сальдо до **субсчёта** (полный `DrAcc/CrAcc`, не только корень).
5. **Договор** — `dim_contract(doc_id PK → contract_ref, contract_name, account_kind)`;
   62→`DrSubconto2`, 60/76→`CrSubconto1`, Objects.Name, эвристика-фильтр; КЗ/УЗ — T8.
   Из `Premaster1C` + `Premaster1CHistory` (последний — для периодов, где Premaster тонкий).
6. **ВГО** — `ico=1` + union-страховка по списку наших ИНН (как сейчас в `vgoFilter`).

---

## 5. Чекпоинты (человеческое ревью)

- **CHECKPOINT A** (после T1): данные ОДНОГО ЮЛ в `fact_glmf` — `count`/`sum(BYN)` совпадают
  с прямым запросом к GLMF за период. Подтвердить extract до отчёта.
- **CHECKPOINT B** (после T3): **сверка чисел** — выручка (точные Дт/Кт vs `group_pl`) и
  ДЗ/КЗ-сальдо на 1–2 ЮЛ × месяц против GLMF и офиц. ОПУ/оборотки. Главный gate качества.
- **CHECKPOINT C** (после T5): **покрытие договоров** — % строк с резолвленным договором по
  62/60/76; эвристика-фильтр (доля чистых имён). Согласовать до масштаба.
- **CHECKPOINT D** (после T9): полная сверка всех ЮЛ + prod-готовность (`fact_glmf` налит,
  инкремент идёт) перед сменой дефолта на ch и merge в master.

---

## 6. Риски и развязки

| Риск | Митигиация |
|---|---|
| Watermark `DateOfLoad` — дневная грануляр. версии (ретро-правки в тот же день) | `ReplacingMergeTree(date_of_load)` + периодический полный re-bootstrap; контроль в CHECKPOINT D |
| Выручка по точным Дт/Кт ≠ `group_pl='ПРОДАЖИ'` | сверка обоих в CHECKPOINT B; эталон — Дт/Кт ТЗ |
| Субконто КЗ/УЗ для договоров не установлено | T8 (разведка) отдельно; до неё КЗ/УЗ «без договора», числа целы |
| Premaster1C неполон по старым периодам (договор) | `dim_contract` тянет и из `Premaster1CHistory`; дыры → «без договора» |
| ICO=1 не покрывает КЗ/УЗ-пары | union-страховка по нашим ИНН (gate prod-verification §D) |
| Смена дефолта ломает прод при ненал. `fact_glmf` | T9 последняя, чек-лист «налито+инкремент идёт»; откат `DEBT_BACKEND=mssql` |
| `GLMF.AmountWOVATCurrency` — функц. валюта (не долга) | используем BYN (`…BelRubFact`); валюта вне скоупа |

---

## 7. Вне скоупа

- **Построчная валюта** (транзакционная) — нет в FinDWH (§SPEC 10.7), исключена.
- Frontend (`nuxt/`) — контракт `DebtRow` сохраняем; правки (субсчёт-колонка) — мелко, после T5.
- finpl/Table_Fin_PL-код — остаётся opt-in, не трогаем/не удаляем.
- Cost-раздел (`python/cost/`) — изолирован.
