# Источник факта МП — разведка на сервере (CHECKPOINT A / Q1)

> Дата: 2026-06-25. Сервер: `10.10.6.15:1433` (тот же, что FinDWH/Premaster для ВГО).
> Метод: одноразовый probe через `go-mssqldb` с кредами `MSSQL_PREMASTER_*`.
> Цель: найти реальный объект под `Источник_МП` / `ALL_view_МП` из ТЗ.

## Что говорит ТЗ (дословно)

**Приложение D «Источники данных»:**
- `Источник_МП` → **OLAP Продажи + SQL Payments.Debt** → Факт МП.
- `Источник_МП_штрафы` → **Отчёт WB / Payments** → Штрафы МП.
- `olap mf_test Продажи` → **OLAP куб mf_test.Продажи** → факт продаж (этап 1.1).
- `olap Budgeting Budget` → **OLAP куб Budgeting, срез Budget** → факт/план статей PL.

**Документ «TPL-MP формы» §4.1 «Источник_МП»:** запрос OLAP **`ALL_view_МП`**, ключи
SUMIFS `month + code_cfo + scenario + CodePL`; колонки: `Scenario, Год, Номер месяца,
CodePL, CodeCFO, Group_МП_new (Маркетплейсы_large/small), Amount_BYN, Amount_RUB,
Amount_USD` (H/I/J).

## Что реально на сервере 10.10.6.15

**Базы (sys.databases):** `FinDWH`, **`Payments`** (отдельная БД!), `Budgeting`,
**`MP`**, `PIM_MP`, `MPmultiWH`, `Audit_MPstats`, `mfportal`, `onec_data` и ~50 др.
→ **Payments — отдельная база, НЕ таблица внутри FinDWH.**

| Объект | БД | Колонки / факт | Пригодность под `Источник_МП` |
|---|---|---|---|
| `dbo.ALL_view` | **Budgeting** | Scenario, Country, Month(date), CodePL, GroupPL, CodeCFO, GroupCFO1-3, CFO, **Amount, AmountBYN** | **СЛОМАНА** на снэпшоте (`ошибки привязки` — зависит от куба/linked). Нет `Group_МП_new`, нет Amount_RUB/USD |
| `dbo.sales_and_COGG_from_FOX_marketplaces` | **FinDWH** | DocID, CodeCFO, CodePL, Date, Month, AmountWOVAT/WithVAT(+Fact+BelRub), Budget/Strat-суммы, ICO | Запрашивается, данные есть (2026-05), **но агрегат по группе 250, не по площадкам 335/336/337/954**; CodePL 1006/2006 |
| `SALES_MARKETPLACES_FIRMA_RAZMER` и др. | **MP** | продажи МП (фирма/размер/склад) | операционные продажи, не бюджетный разрез |
| `marketplaces_spp_history` | **MP** | marketplace, date, item_id, **spp_value** | источник **% СПП** (блок формы) |
| `STOCKS_*MARKETPLACES*` | **MP** | остатки | не нужно для факта PL |

## Вывод

`Источник_МП` в ТЗ — **не готовая таблица, а Power-Query-сборка** (`ALL_view_МП`)
поверх куба «Продажи» + `Payments.Debt`, с добавленным разрезом `Group_МП_new`
(площадка→large/small) и пересчётом в 3 валюты (BYN/RUB/USD). **Готового объекта
ровно с этими колонками (по площадкам × CodePL × Scenario × Group_МП_new ×
Amount_BYN/RUB/USD) на сервере нет.** `Budgeting.dbo.ALL_view` концептуально ближе
всего, но (а) сломана на снэпшоте, (б) без МП-разреза и валют.

## Что нужно от аналитика/BI (для закрытия CHECKPOINT A)

Выбрать один путь:

- **(A) BI заводит вьюху** `ALL_view_МП` (именованный объект в Budgeting или FinDWH)
  с колонками как в §4.1 (Год/Номер месяца/CodePL/CodeCFO/Group_МП_new/Amount_BYN/
  RUB/USD), починив привязки. Тогда веб: `PLANS_MP_FACT_VIEW=<db>.dbo.ALL_view_МП`,
  `PLANS_MOCK=0` — и всё. **Предпочтительно** (минимум кода у нас).
- **(B) Воспроизводим сборку у себя**: SQL поверх куба «Продажи»/`MP` + `Payments`
  + маппинг площадка→`Group_МП_new` + курсы→3 валюты. Больше кода и связности, но
  без зависимости от BI-вьюхи.

До решения — рабочий путь `PLANS_MOCK=1` (фикстуры из прототипа `Маркетплейсы_large`);
форма деградирует до пустого факта, ввод тактики не блокируется.

## Технические заметки для коннектора (когда источник определят)

- `go/internal/plans/olap_mp.go` сейчас ждёт колонки `Год / Номер месяца / CodePL /
  CodeCFO / Group_МП_new / Amount_RUB` и `FROM [<view>]`. Под реальный объект надо:
  (1) поддержать **3-частное имя** (`db.dbo.view`) — текущая `FROM [%s]` ломает точки;
  (2) выровнять имена колонок по факту (`Month` date vs `Год/Номер месяца`; валюты).
- Штрафы (CodePL=66): отдельный источник (`Источник_МП_штрафы` / Payments / отчёт WB).
- % СПП: `MP.dbo.marketplaces_spp_history` (spp_value) — отдельный поток.
