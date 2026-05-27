# Отчёт «Задолженность» — предварительная схема данных

> Статус: **draft v0.5**, обновлено 2026-05-23 после чтения ETL-процедур.
> **Принципиальная установка:** считаем отчёт **своими формулами на сырых
> проводках `Premaster1C`**, не опираясь на готовые DWH-витрины. Все свёртки
> ДЗ/КЗ/выручки/оборотов — на нашей стороне, по плану счетов из ТЗ.
> Готовые витрины описаны отдельно (§ 8) как **открытый вопрос** — нужно
> явно решить с автором ТЗ, можно/нужно ли их использовать.

## 1. Источник данных

**Главная таблица:** `[FinDWH].[dbo].[Premaster1C]` на OLAP-сервере `10.10.6.15`.
- Объём: ≈ 203 млн строк.
- Период `Date`: 2021-01-02 … 2029-04-30 (будущие — плановые/амортизационные).
- Индексы:
  - `CLUSTERED (DocID, RwNm)` — drill-down по документу.
  - `NONCLUSTERED (CompanyID, Date)` — **главный** для отчёта.
  - `NONCLUSTERED (CompanyID, Date, DocSeen)` — фильтр «увиденных».
  - `NONCLUSTERED (CompanyID, DocID)` — все строки документа в компании.
- `WITH (NOLOCK)` **обязательно** — под постоянной нагрузкой ETL.

**Связанные:**
- `Premaster1CHistory` (≈820M) — журнал версий, для ретроверсии (v3, не для MVP).
- `Premaster1C_old`, `Premaster1CHistory_old_data` — архивы, не использовать.
- `Premaster1C_20260514` — частный снэпшот одного дня, не репрезентативен.

## 2. Природа данных

Одна строка `Premaster1C` = одна проводка из 1С с маппингом на управленческий учёт. Документ → N строк (`RwNm` — номер строки внутри документа, `DocID` — стабильный ключ документа).

В таблице сосуществуют **разные планы счетов** — каждая страна свой:
- РФ: `60.01`, `62.01`, `90.01.1`, `44.01` (с подсубсчётами через точку)
- РБ: вариант ПБУ РБ
- КЗ/УЗ/прочие: 4-значный (`9120`, `4090`, `2010`, `9430`)

Поэтому **маппинг «счёт = ДЗ / КЗ / Выручка» должен зависеть от страны юрлица**. Источник классификации — ТЗ-приложение «счета БУ» (хардкод в Go).

## 3. Семантика колонок

### 3.1 Идентификация

| Колонка | Тип | Использование |
|---|---|---|
| `CompanyID` | varchar(100) | ИНН/УНП/БИН нашего юрлица. → `CompanyINN`. Длина намекает на страну, но определять страну надо через `CompaniesMF`. |
| `CounterpartyID` | varchar(100) | ИНН/УНП партнёра плоской строкой (РФ-партнёр = 10-значный ИНН). NULL для внутренних операций (курсовые, переоценки, начисление налогов). → `PartnerINN`. NULL отбрасываем при группировке по партнёру. |
| `DocID` | varchar(85), кластеризован | Внутренний 1С-ID формата `{"#",<type-guid>,<bytes>:<value-guid>}`. Ключ для drill-down. |
| `RwNm` | bigint | Порядковый номер строки в документе. |
| `BaseDocID` | binary | Внутренний 1С-ID базового документа. Для отчёта debt не нужен. |
| `ICO` | tinyint | 1 = ВГО (внутригрупповая), 0 = не ВГО. Используется фильтром «только ВГО». |
| `DocSeen` | tinyint | Флаг «увиденный документ» — поведение неизвестно, в v1 игнорируем. |

### 3.2 Время

| Колонка | Что значит | Использование |
|---|---|---|
| `Date` | дата документа (часто `23:59:59` для закрытий) | **главный фильтр периода** |
| `DateOfLoad` | дата загрузки строки в Premaster | для «как видели на дату X» |
| `DateOfChange` | дата последнего изменения | аудит, не для v1 |

### 3.3 Счета

| Колонка | Тип | Использование |
|---|---|---|
| `DrAcc` | varchar(20) | счёт дебета: `60.01`, `62.01`, `90.01.1`, `9120` |
| `CrAcc` | varchar(20) | счёт кредита |

Свёртка для отчёта:
- `Account` = до первой точки (`62.01` → `62`)
- `Subaccount` = после первой точки (`62.01` → `01`)

### 3.4 Суммы

| Колонка | Тип | Семантика |
|---|---|---|
| `AmountWithVATCurrency` | numeric(18,2) | сумма проводки в валюте, **с НДС**. Минус = сторно/уменьшение. Для свёрток ДЗ/КЗ. |
| `AmountWOVATCurrency` | numeric(18,2) | то же **без НДС**. Для свёрток выручки. |

**Валюта в самих колонках не хранится** — название `*Currency` означает «в валюте проводки», т.е. в функциональной валюте юрлица или в валюте валютного счёта. Для валютных проводок (счёт `52`, `1030`) валюта зашита в **субконто** валютного счёта (GUID 1С).

### 3.5 Текстовые поля

| Поле | Формат | Использование |
|---|---|---|
| `TransDescription` | Свободный текст. Для входящих: `«<услуга> по вх.д. <номер> от <ДД.ММ.ГГГГ>»`. Для исходящих — generic `«Реализация товаров»` | Источник `DocNumber`/`DocDate` для входящих. Regex: `по\s+вх\.д\.\s+(\S+)\s+от\s+(\d{2}\.\d{2}\.\d{4})` |
| `OperationDescription` | Короткий код (`«41 продажа CIS-139294»`), часто пустой | Не нужно для debt |
| **`Mapping`** | Шаблонизирован: `«<тип документа> <номер> от <ДД.ММ.ГГГГ ЧЧ:ММ:СС> (документ), <статья> (субконто)»` ИЛИ `«<имя ЦФО> (проводка), <статья> (субконто)»` | **Источник `DocKind`+`DocNumber`+`DocDate` для исходящих**. Regex: `^([^()]+?)\s+(\S+)\s+от\s+(\d{2}\.\d{2}\.\d{4}(?:\s+\d{1,2}:\d{2}:\d{2})?)\s+\(документ\)` |
| `CorrectorName`, `WhatChanged`, `Performer1C` | Аудит / исполнитель | Не нужно для debt |

### 3.6 Управленческий учёт (не для debt)

`CodeCFO`, `CodePL`, `ObjectCFO`, `ObjectPL`, `MappingCFO`, `MappingPL`, `DrSubconto1..4`, `CrSubconto1..4` — мап на ЦФО и P&L. Для отчёта задолженности **не используются**.

Субконто хранят GUIDы 1С. Resolver «GUID → имя» в `[FinDWH]` не найден (см. § 8 — открытый вопрос). Для drill-down debt-отчёта это **не блокер**, потому что номер и тип документа есть в `Mapping`.

## 4. Маппинг → `debt.DebtRow` с формулами свёртки

### 4.1 Маппинг полей

```
DebtRow.CompanyINN      ← Premaster.CompanyID
DebtRow.Company         ← CompaniesMF.CompanyMFName1C[CompanyID]
DebtRow.Country         ← CompaniesMF.Country[CompanyID]

DebtRow.PartnerINN      ← Premaster.CounterpartyID  (NULL → строка отбрасывается)
DebtRow.Partner         ← Counterparty.Counterparty[CounterpartyID]  (fallback = PartnerINN)

DebtRow.Account         ← substring(DrAcc/CrAcc до 1-й точки)
DebtRow.AccountName     ← lookup_chart_of_accounts(Country, Account)
DebtRow.Subaccount      ← substring(после 1-й точки)
DebtRow.SubaccountName  ← lookup_chart_of_accounts(Country, Account, Subaccount)

DebtRow.Currency        ← v2 (resolver GUID-субконто валютного счёта; в v1 — функциональная валюта юрлица из CompaniesMF.CurrID)
DebtRow.Contract        ← v2 (resolver GUID-субконто договора + AgreementsBK)
DebtRow.PaymentTermDays ← v2 (AgreementsBK)
```

### 4.2 Свёртки (Opening/Turnover/Closing)

Для каждой страны в Go захардкожен `accountKinds(country) → {dz, kz, revenue}` из ТЗ:
- РФ: `dz = {62.01, 62.02, 76.05, 76.09}`, `kz = {60.01, 60.02, 76.06, 76.10}`, `revenue = {90.01.1, 90.01}`
- РБ: TBD из ТЗ
- КЗ: `dz = {1210, 1280}`, `kz = {3310, 3390}`, `revenue = {6010}` (предположение по 4-значным кодам, сверить с ТЗ)
- УЗ/ТР/прочие: TBD из ТЗ

**Opening DZ** (входящее сальдо дебиторской) =
```sql
  Σ AmountWithVATCurrency  WHERE Date < @DateFrom AND DrAcc IN @dz
− Σ AmountWithVATCurrency  WHERE Date < @DateFrom AND CrAcc IN @dz
```
**Turnover DZ** (обороты за период) =
```sql
  Σ AmountWithVATCurrency  WHERE Date BETWEEN @DateFrom AND @DateTo AND DrAcc IN @dz
− Σ AmountWithVATCurrency  WHERE Date BETWEEN @DateFrom AND @DateTo AND CrAcc IN @dz
```
**Closing DZ** = `OpeningDZ + TurnoverDZ`.

**Opening / Turnover / Closing KZ** — зеркально:
```sql
  Σ AmountWithVATCurrency  WHERE … AND CrAcc IN @kz
− Σ AmountWithVATCurrency  WHERE … AND DrAcc IN @kz
```

**RevenuePeriod** (выручка за период) =
```sql
−Σ AmountWOVATCurrency  WHERE Date BETWEEN @DateFrom AND @DateTo AND CrAcc IN @revenue
```
Минус — потому что в выручных проводках `Cr 90.x` сумма знаком «−», возвращаем положительную сумму выручки.

**RevenueLastMonth** — то же, но для последнего календарного месяца внутри `[DateFrom; DateTo]`.

### 4.3 Группировка результата

Группировать в SQL по `(CompanyID, CounterpartyID, account_root, subaccount, currency)`. Currency в v1 берётся из `CompaniesMF.CurrID` → строка валюты (см. § 8.3 — нужен справочник CurrID→Currency).

ВГО-фильтр (опционально по выбору пользователя) = `WHERE Premaster.ICO = 1`.

### 4.4 Архитектурный шаблон запроса

Одним SQL c CTE через UNION ALL — это разумно для свёртки Dr/Cr-частей через индекс `(CompanyID, Date)`. Black-box набросок (детали SQL в `repo_premaster.go`):

```sql
WITH src AS (
    SELECT CompanyID, CounterpartyID, DrAcc AS acc, AmountWithVATCurrency AS amt, +1 AS sign_dr, Date
    FROM [FinDWH].[dbo].[Premaster1C] WITH (NOLOCK)
    WHERE CompanyID IN @companies AND Date <= @DateTo
    UNION ALL
    SELECT CompanyID, CounterpartyID, CrAcc, AmountWithVATCurrency, -1, Date
    FROM [FinDWH].[dbo].[Premaster1C] WITH (NOLOCK)
    WHERE CompanyID IN @companies AND Date <= @DateTo
)
SELECT CompanyID, CounterpartyID,
       LEFT(acc, …) AS account_root,
       SUM(CASE WHEN Date < @DateFrom THEN amt * sign_dr ELSE 0 END) AS opening_signed,
       SUM(CASE WHEN Date BETWEEN @DateFrom AND @DateTo THEN amt * sign_dr ELSE 0 END) AS turnover_signed,
       SUM(amt * sign_dr) AS closing_signed
FROM src
GROUP BY CompanyID, CounterpartyID, LEFT(acc, …)
```

Это даёт raw-сальдо со знаком. Разворачивать в `OpeningDZ` vs `OpeningKZ` на стороне Go: по `account_root + country + accountKinds(country)`.

**Performance:** для 5-10 компаний за год через индекс `(CompanyID, Date)` — оценка десятки секунд (UNION ALL × 2). Кэшировать результат на стороне Go (`debt_saved_filters` уже есть).

## 5. Drill-down `Premaster1C` → `debt.DocumentRow`

Запрос для конкретной (Company, Counterparty, Account, Period):

```sql
SELECT Date, DocID, DrAcc, CrAcc, AmountWithVATCurrency, Mapping, TransDescription
FROM [FinDWH].[dbo].[Premaster1C] WITH (NOLOCK)
WHERE CompanyID = @company
  AND CounterpartyID = @partner
  AND (DrAcc IN @accts OR CrAcc IN @accts)
  AND Date BETWEEN @from AND @to
ORDER BY Date, DocID, RwNm
```

Группировка в Go по `DocID`:

```
DocumentRow.DocDate    ← min(Date)
DocumentRow.DocNumber  ← parse(Mapping) для исходящих → group 2 regex
                        иначе parse(TransDescription) для входящих
                        иначе DocID (fallback)
DocumentRow.DocKind    ← parse(Mapping).type → group 1 regex
DocumentRow.DZChange   ← Σ AmountWithVATCurrency × sign по строкам где DrAcc ∈ @dz минус CrAcc ∈ @dz
DocumentRow.KZChange   ← зеркально
DocumentRow.PaymentDueDate ← v2 (нужен AgreementsBK по контракту)
DocumentRow.OverdueDays    ← v2 (расчёт из PaymentDueDate + reportDate)
```

## 6. Справочники, нужные нашей реализации

Все ниже — независимо от наличия готовых витрин; для нашей свёртки нужны они сами.

| Источник | Размер | Что берём |
|---|---|---|
| `[FinDWH].[dbo].[CompaniesMF]` | 18 строк | `UNP → {CompanyMFName1C, Country, CurrID, ClosedPeriod}`. Можно загрузить в кэш приложения; обновлять раз в день. |
| `[FinDWH].[dbo].[Counterparty]` | 10 237 строк | `CounterpartyID → Counterparty (имя)`. Аналогично кэш. Грязные ключи `""`, `"-"`, `"0"` отфильтровать. |
| `[FinDWH].[dbo].[Counterparty1C]` | 10 420 строк | Дополнительно: `Channel`, `Manager`. УНП РБ-контрагентов с **ведущим пробелом** — TRIM обязателен. |
| `[FinDWH].[dbo].[Counterparty1CINNRU/KZ/MFUz]` | 1942/391/841 | Опционально — точечный lookup по стране, быстрее общего `Counterparty`. |
| `[FinDWH].[dbo].[Counterparty1CEmptyRU/KZ/MFUz]` | 185/6/158 | Контрагенты без ИНН (физлица, торговые точки). В debt-отчёте — отдельно или показывать как «Без идентификатора». |
| **ТЗ-приложение** «счета БУ» | статика | Классификация `country + account → {dz, kz, revenue}`. **Хардкод в Go** (`go/internal/reports/debt/chart_of_accounts.go`). |
| `[FinDWH].[dbo].[ICO]` | 2 строки | Словарь bool→текст. Используем только для проверки контракта; фильтр — по колонке `Premaster.ICO`. |

## 7. Производительность

- Используем индекс `(CompanyID, Date)`. Все WHERE начинаются с фильтра `CompanyID IN @companies`.
- `WITH (NOLOCK)` на всех запросах.
- **Не делать**: `MIN/MAX(Date)`, `COUNT(*)`, `GROUP BY` без `WHERE` — full scan 200M.
- **Размер периода**: для года по 5-10 компаниям — приемлемо. Для многих лет — стоит делать материализованную копию в нашем Postgres (отдельный stretch goal).
- **Drill-down**: индекс `(CompanyID, DocID)` — мгновенно.

## 8. Открытые вопросы

### 8.1 ⚠ Главный вопрос: что **не устроило** в готовых витринах FinDWH?

В соседних таблицах FinDWH уже лежат **готовые срезы**, на первый взгляд закрывающие большую часть отчёта. Перед тем как писать свёртки руками — нужно явно решить с автором ТЗ, **почему он их не использует**.

#### `[FinDWH].[dbo].[FinancialReportСounterparties]` — 288 073 строки

Структура:

| Колонка | Что |
|---|---|
| `INN` | наш ИНН/УНП |
| `Period` | месяц (1-е число) |
| `AccountNumber` | счёт со субсчётом |
| `OpeningDebit / OpeningCredit / TurnoverDebit / TurnoverCredit / ClosingDebit / ClosingCredit` | сальдо/обороты по Dr и Cr |
| `Currency` | валюта явно |
| `Counterparty / CounterpartyINN / CounterpartyCode / CGroup` | контрагент + группа |

Покрывает: `OpeningDZ/KZ`, `TurnoverDZ/KZ`, `ClosingDZ/KZ`, `Currency`, `Partner/INN` — практически плоский `DebtRow`. Не покрывает: drill-down, классификация счетов ДЗ/КЗ, ВГО-фильтр (нет колонки ICO).

**Возможные причины «не использовать»** (нужно подтвердить):
- Гранулярность только месячная — нет произвольных диапазонов в днях.
- Алгоритм свёртки аналитиков может отличаться от ТЗ (например, какие счета относить к ДЗ/КЗ, как считать ВГО-исключения, как учитывать НДС).
- Зависимость от ETL: если он сломается — отчёт замолчит; самостоятельная свёртка из Premaster контролируется нами.
- Расхождения с 1С на отчётную дату; ТЗ может требовать сверять с исходником сделок.
- Аналитики обновляют по своему расписанию; для оперативного отчёта нужно «прямо сейчас».

#### `[FinDWH].[dbo].[CubeBadDebt]` — 5 654 594 строки

Готовый ageing с корзинами `SUM30/SUM60/SUM90/SUM999` (просрочка 0-30/30-60/60-90/90+), `DEBT_FILTER` ∈ {Дебиторская, Кредиторская, Отображать платежи и отгр.}, `Currency`, `Channel`, `Manager`. Покрывает то, чего нет в `DebtRow` модели вообще — ageing-матрицу.

**Возможные причины «не использовать»:**
- Те же что выше (гранулярность, контроль, расхождения).
- Может не покрывать все наши страны.
- Может быть заточен под одну конкретную бизнес-задачу.

#### Решение

Эти витрины задокументированы, **в код не включены**. Когда автор ТЗ скажет «можно/нужно использовать» — переключимся (или дублируем как «быстрый путь» с указанием расхождений со свёрткой из Premaster).

### 8.2 Договоры и сроки оплаты — **источник не найден**

`AgreementsBK` (77 строк) — это **workflow согласования договоров** (типа Bitrix24-СЭД), а не справочник. Колонки: `AgreementType`, `AgreementName`, `INN`, `CounterPartyName`, `AgreementDate`, `AgreementUsloviya` (заглушка-формуляр), `Author`, `Ispolnitel`. **Нет** `PaymentTermDays`, нет связи с конкретной проводкой Premaster.

Срок оплаты, скорее всего, лежит в самой 1С (в Subconto документа или в справочнике договоров 1С). Для нашего отчёта `PaymentDueDate`/`OverdueDays` либо:
- (a) брать из `CubeBadDebt` (ageing-корзины) — но это уровень контрагента, не документа;
- (b) парсить из текста `TransDescription`/`OperationDescription` (часто фиксированно «отсрочка 30 дней»);
- (c) исключать из v1 и v2 — добавлять в v3 после согласования с автором ТЗ.

### 8.3 Валюта и курсовая нормализация — **закрыто** (после чтения ETL)

#### Полная цепочка валют (из текстов `GLMF_exec`, `spExRates`)

| Что | Источник |
|---|---|
| Числовой ID валюты юрлица | `[FinDWH].[dbo].[CompaniesMF].CurrID` |
| Дневной курс «иностранной валюты → BYN» | **`[SRV-SQL].[Checks].[dbo].[CurrencyDaily]`** (linked server, БД `Checks`) — колонки: `date`, `curr_id`, `curr_rate` |
| Справочник валют `curr_id → код валюты` | **`[srv-sql].[Gpartner].[dbo].[valuta1]`** (linked server, БД `Gpartner`) |
| `CurrID = 1` | **BYN** (для него `IIF(B.CurrID=1, 1, ...)` — курс=1) |
| `CurrID = 3` | **USD** (из комментария в `spExRates`: «Это код долларов в таблице [srv-sql].[Gpartner].[dbo].[valuta1]») |
| `CurrID = 4, 7, 8, 9` | надо посмотреть `valuta1` (вероятно RUB, KZT, UZS, TRY соответствующих странам) |

Формула пересчёта (как у ETL):

```
ExRate = IIF(CurrID = 1, 1, CurrencyDaily.curr_rate)  -- курс на дату проводки
AmountBYN = AmountCurrency * ExRate
```

Для Finance-отчёта: либо считать сами в Go (join с `CurrencyDaily`), либо взять готовое `GLMF.ExRate`/`*BelRubFact` (но с учётом ограничения индекса GLMF, см. § 8.8).

#### `[FinDWH].[dbo].[ExRates]` — 3 795 строк, **частный случай**

Хранит **только USD** (`spExRates` insert'ит только `VALUTA_ID = '3'`). Не используем — берём напрямую `CurrencyDaily`.

`ExRatesRU/KZ/UZ/TR` — аналогично, скорее всего узкоспециализированные. `ExRatesBY` — 0 (BYN сам себе функциональный).

### 8.4 Resolver GUID-объектов 1С → имя — **закрыто**

**`[FinDWH].[dbo].[Objects]`** — 5 249 852 строк, два столбца: `ID` (GUID 1С формата `{"#",...}`) и `Name` (читаемое имя). Используется в `GLMF_exec` для резолва `DocID → DocName1C`.

Покрывает **все** GUIDы 1С: документы (`DocID`), контрагенты, договоры, валютные счета (примеры: `«основной»`, `«Текущий в дол.США Дабрабыт»`, `«Текущий в Евро Дабрабыт»`).

**Использование для debt:**
- `DocName1C` для drill-down → `LEFT JOIN Objects ON DocID` вместо парсинга `Mapping`.
- **Валюта проводки через субконто валютного счёта 52/1030**: `LEFT JOIN Objects ON Subconto1 = Objects.ID`, по `Name` определять валюту (regex `«в дол.США|USD»`, `«в Евро|EUR»`, ...). Хрупко, но проще чем resolver субконто отдельно.

`DimSubkonto` (75 строк) к Objects-resolver-у отношения не имеет — это маппинг типовых проводок, не resolver. **§ 8.4 закрыт.**

### 8.5 Сверка плана счетов с ТЗ — **открытый вопрос**

Из ТЗ-приложения «счета БУ» — собрать таблицу `country × account → {dz | kz | revenue | other}`. Это **должно быть в приложении к ТЗ**, не угадывается из данных.

### 8.6 Гранулярность периода — **открытый вопрос**

Свёртка по `Premaster1C` даёт произвольный диапазон (день/неделя/месяц/год). Если бизнес требует только месяц — это упрощение, не ограничение.

### 8.7 ВГО — определение — **открытый вопрос**

«Задолженность ВГО» = **только** проводки с `ICO=1`, или общая задолженность + ВГО как один из срезов? Влияет на дефолт фильтра.

### 8.8 Прочие таблицы — обследовано

#### `[FinDWH].[dbo].[GLMF]` — 203 230 641 строк (≈ Premaster1C)

**Обогащённый Premaster:** все колонки Premaster + `CounterpartyName` (резолвлено), `Country`, `GroupCFO1/2`, `Department`, `GroupPL`, `Expense`, `Month`, `Company`, `ExRate`, `AmountWithVATBelRubFact`, `AmountWOVATBelRubFact`, `StratBudgetAmountWOVAT*`, `BudgetAmountWOVAT*`, `DbAnalytics1`, `PLDescrBK`, `ICO`, `DateOfLoad`, `Num`.

**Индексы:** только `CLUSTERED (Company, DrAcc, CrAcc, Date)`. **Не подходит для нашего отчёта**, потому что у нас фильтр стартует с `CompanyID` (УНП, не `Company` — короткое имя). Поиск по голове кластера не поддержан → fullscan.

**Использовать**:
- ❌ не основа для debt-отчёта (медленнее Premaster)
- ✅ для будущих P&L и управленческих отчётов (Department × GroupPL × Month) — там фильтр совпадает с головой индекса
- ✅ как контрольная сверка наших цифр (точечные запросы с указанием `Company`)

#### `[FinDWH].[dbo].[GLMFSvernuto]` — 716 789 строк

Свёрнутая версия `GLMF` (вероятно по `Document × Month × Department × GroupPL`). Структура та же. Для debt не подходит (нет CounterpartyID-уровня).

#### `[FinDWH].[dbo].[CubeFinDWHShort]` — 434 373 строк

P&L куб (Страна × Месяц × КодЦФО × КодСтатьи × Department × ICO) с фактами и бюджетом, плюс пересчёт в USD (`AmountWOVATUSDFact`). **Не для debt** — про P&L. Полезно знать, что **USD-пересчёт уже есть** на агрегатном уровне.

#### `[FinDWH].[dbo].[FinancialReport]` — 36 231 строк

«FinancialReportСounterparties» **без разбивки по контрагенту**: `INN + Period + AccountNumber + Opening/Turnover/Closing × Debit/Credit`. Это оборотно-сальдовая ведомость по счетам компаний (без партнёров). **Не для debt** (нужна разбивка по контрагенту).

#### `[FinDWH].[dbo].[BUNetFinResCompaniesMF]` — 141 строка

`CompanyMFNameFinDWH + ReportingPeriod + NetFinResult` — финальный P&L по компаниям по месяцам. **Не для debt**.

## 8a. Инсайты из ETL-процедур (как делают аналитики)

После чтения `sp_BadDebtCube`, `GLMF_exec`, `spExRates` стало ясно где у аналитиков что лежит.

### 8a.1 Происхождение `CubeBadDebt`

`sp_BadDebtCube` — тонкая обёртка:

```sql
INSERT INTO CubeBadDebt
SELECT report.*, counterparty.*, companymf.CompanyMFNameFinDWH, ...
FROM [Payments].[report].[FinDebt1] AS report
LEFT JOIN Counterparty1C ON report.UNP = ... (с outer apply от задвоений)
LEFT JOIN CompaniesMF ON report.UNPOrg = CompaniesMF.UNP
```

**Истинный источник ageing — в БД `Payments`**, в таблице `[Payments].[report].[FinDebt1]`. Если хотим понять «как считается просрочка» — копаем процедуры в `Payments`.

### 8a.2 Как делается `GLMF` из `Premaster1C` — готовый шаблон join'ов для нас

```sql
INSERT INTO GLMF
SELECT
    A.* /* все колонки Premaster1C */,
    ISNULL(G.Counterparty,'Error')      AS CounterpartyName,  -- из Counterparty
    IIF(B.CurrID=1, 1, ISNULL(C.curr_rate, 0)) AS ExRate,     -- курс из CurrencyDaily
    AmountWithVAT * ExRate              AS AmountWithVATBelRubFact,
    ISNULL(B.Country,'Error')           AS Country,           -- из CompaniesMF
    D.GroupCFO1, D.GroupCFO2, D.CFO AS Department,            -- из [001 CodeCFO]
    E.GroupPL, E.Expense,                                     -- из [002 CodePL]
    F.[Name]                            AS DocName1C,         -- из Objects
    H.Mapping                           AS PLDescrBK          -- из [001 Mapping PL by BK]
FROM Premaster1C A
LEFT JOIN CompaniesMF B  ON A.CompanyID = B.UNP
LEFT JOIN #curr C  /* копия [SRV-SQL].Checks.dbo.CurrencyDaily */
                   ON B.CurrID = C.curr_id AND CAST(A.Date AS DATE) = C.date
LEFT JOIN [001 CodeCFO] D ON A.CodeCFO = D.CodeCFO
LEFT JOIN [002 CodePL]  E ON A.CodePL  = E.CodePL
LEFT JOIN Objects        F ON A.DocID    = F.ID
LEFT JOIN Counterparty   G ON A.CounterpartyID = G.CounterpartyID
LEFT JOIN [001 Mapping PL by BK] H ON ... (3-уровневый fallback)
WHERE ...

-- + insert корректировок из [002 List of adj-ts]
-- + insert корректировок из [Adj001SamovyvozInternetShop]
```

**Шаблон для нашего отчёта** (упрощённо):

```sql
SELECT A.CompanyID, A.CounterpartyID, A.DrAcc, A.CrAcc,
       A.AmountWithVATCurrency, A.Date, A.DocID, A.ICO,
       B.Country, B.CurrID, B.CompanyMFName1C AS company_name,
       G.Counterparty AS partner_name,
       ISNULL(F.[Name], '') AS doc_name1c,
       IIF(B.CurrID=1, 1, ISNULL(C.curr_rate, 0)) AS rate_to_byn
FROM   [FinDWH].[dbo].[Premaster1C] A WITH (NOLOCK)
LEFT JOIN [FinDWH].[dbo].[CompaniesMF] B ON A.CompanyID = B.UNP
LEFT JOIN [SRV-SQL].[Checks].[dbo].[CurrencyDaily] C
       ON B.CurrID = C.curr_id AND CAST(A.Date AS DATE) = C.date
LEFT JOIN [FinDWH].[dbo].[Counterparty] G ON A.CounterpartyID = G.CounterpartyID
LEFT JOIN [FinDWH].[dbo].[Objects] F     ON A.DocID = F.ID
WHERE A.CompanyID IN @companies AND A.Date <= @DateTo
```

С этим запросом в Go уже есть **имена, страна, валютный курс, DocName** — без отдельных lookup-ов и без хранения справочников в кэше.

### 8a.3 Природа adj-корректировок — для debt **не нужны**

Структуру `[002 List of adj-ts]` и `[Adj001SamovyvozInternetShop]` посмотрели на пробе. Обе таблицы — **P&L-корректировки**, не балансовые. У них:
- `DrAcc = 'n/a'`, `CrAcc = 'n/a'` — реальных счетов нет;
- `CounterpartyID = 'n/a'` — контрагента нет;
- `DbAnalytics1 = 'Adj'`/`'Adj001'`;
- `PLDescrBK = 'RevenueDirect'`/`'COGS'` — это статьи P&L.

Это **корректировки управленческого учёта** (производство в Турции, реклассы самовывоза интернет-магазина), которые меняют **только P&L-агрегаты** (`GroupPL=ПРОДАЖИ`/`СЕБЕСТОИМОСТЬ`, `Country`, `Department`). Балансовых счетов **60/62/76 они не касаются**, поэтому **в свёртке ДЗ/КЗ их игнорируем по построению**. Никакого выбора между «учитывать/нет» делать не нужно.

`insert_002 List of adj-ts` — простой ETL: BK загружает корректировки JSON-ом через `OPENJSON`. Никакой бизнес-логики, просто валидация типов.

`spAdj001SamovyvozInternetShop` (98KB) и `spAdj006PayrollReclass` (16KB) — генерируют свои adj-таблицы по сложной логике (рекласс себестоимости / ФОТ между подразделениями). Это тема для P&L-отчёта, не для debt.

### 8a.4 БД `[Payments]` — отдельный мир, целиком про задолженность

Это **специализированная БД учёта расчётов с контрагентами**, отдельная от `FinDWH`. Главное:

**Архив фактов:**
- `dbo.Debt_arh` — **77.3M строк**, ежедневный архив задолженности с пред-посчитанной просрочкой `Sum`, `Sum30`, `Sum60`, `Sum90`, `Sum999`.
- `dbo.Debt_arh_свернутая` — 4.4M, свёрнутая часть архива (давние периоды).
- `dbo.Debt_arh_26.04.2023`, `dbo.Debt_arh11082023` — снэпшоты на конкретные даты.
- `dbo.Debt` — 70K, оперативная задолженность сегодня.
- `dbo.Payment` (73K) / `Payment_arh` (478K) — платежи + архив.
- `dbo.Wholesales` (145K) / `Wholesales_arh` (4M) — оптовые отгрузки + архив.
- `dbo.Docs` (3M) — справочник документов.

**Справочники:**
- `dbo.UNPOrg` — **15 наших юрлиц** (vs 18 в `CompaniesMF` — есть расхождение).
- `dbo.UNP` — **2 220 контрагентов** с колонкой `CONTRAGENT` (имя).
- `dbo.Contragents` — 162.
- `dbo.VIP` — 7.

**Отчётные слои:**
- `report.FinDebt1` (5.6M), `FinDebt2` (766K), `FinDebt3` (6.5M) — три формата готового отчёта по долгу. `FinDebt1` — наш-куб-источник для `CubeBadDebt`.
- `report.FinDebt_Date_Updt` — последняя дата обновления.

**Главная процедура — `dbo.fin_debt_report_exec`** (61KB, обновлена 2024-09-20). Делает `report.FinDebt1` через:
1. Подбор отчётных дат (текущий период + EOMs прошлых месяцев) из `[srv-sql].Gpartner.dbo.s_calendar`.
2. Сбор актуального менеджера на пару (UNP, UNPOrg) по `ROW_NUMBER OVER (PARTITION BY ... ORDER BY MX_DATE DESC)`.
3. `#DEBT_T` = `Debt_arh_свернутая ∪ Debt_arh`, поделено на `Дебиторская` (`Sum >= 0`) и `Кредиторская` (`Sum < 0`).
4. `#PAYMENTS_T` = `Payment_arh` со специальными правилами по UNPOrg:
   - Обычные: `K_Payment` если `DrAcc LIKE '5%' OR '9%'`; `D_Payment` если `DrAcc LIKE '6%' OR '7%'`.
   - КЗ (`UNPOrg LIKE '141240004842%'`): другие счета (`1030`, `1010`).
   - УЗ (`UNPOrg = '305554644'`): счета `52`, `51`, `4310/4315/4330`, `6310/6315`.
5. `#WHOLESALES_T` = `Wholesales_arh`.
6. Дальше (не дочитан) — финальный INSERT в `report.FinDebt1`.

#### Природа `Sum30/60/90/999`

Это **уже посчитанные корзины просрочки** в `Debt_arh`. Сама арифметика просрочки делается **не здесь** — где-то ещё (вероятно ETL, который заливает `Debt_arh` из 1С). Чтобы понять формулу — нужно идти в источник `Debt_arh` (возможно отдельный SSIS-пакет или джоб 1С).

#### Главные **причины не использовать Payments-витрину** (кандидаты)

1. **Зависимость от ETL `Debt_arh`**: сломается ETL — отчёт замолчит. Самостоятельная свёртка из Premaster — независима.
2. **15 юрлиц** в `UNPOrg` vs **18** в `CompaniesMF` — не все наши юрлица покрыты витриной.
3. **Захардкоженные правила** для КЗ/УЗ в процедуре — новая страна / изменение плана счетов потребуют правки прод-процедуры.
4. **Каналы/менеджеры** проставляются эвристически (`#MESSES`) — могут быть пустыми/устаревшими.
5. **Период данных** ограничен `Debt_arh_свернутая ∪ Debt_arh` — не охватывает всю историю.
6. **Спецзнание про знак** `Sum`: «`Sum >= 0` → Дебиторская, `Sum < 0` → Кредиторская» — это конвенция Payments, не универсальная. Понимать в чём её смысл и как соотносится с ТЗ — отдельный разговор.

Это **сильные причины** идти своей свёрткой по `Premaster1C` — пункт § 8.1 (вопрос автору ТЗ) теперь имеет конкретные аргументы. Решение — за автором ТЗ.

### 8a.5 Итог по корректировкам и Payments

| Источник | Использовать в нашем отчёте? | Почему |
|---|---|---|
| `[002 List of adj-ts]` | **Нет** | P&L-корректировки, балансовых счетов не касаются |
| `[Adj001SamovyvozInternetShop]` | **Нет** | то же |
| `[Payments].[report].[FinDebt1]` | **Опционально (под вопросом автора ТЗ)** | готовый отчёт, но привязан к ETL `Debt_arh` и охватывает 15/18 юрлиц |
| `[Payments].[dbo].[fin_debt_report_exec]` | **Нет, только как референс** | главная процедура аналитиков — изучать её для понимания их логики, не дублировать |
| `[Payments].[dbo].[Debt_arh]` | **Нет, источник недоверия** | внешний ETL посчитал нам корзины — формула неизвестна |
| `[FinDWH].[dbo].[Premaster1C]` | **Да** | сырые проводки, контролируем сами |
| `[FinDWH].[dbo].[Objects]` | **Да** | resolver GUID → имя документа |
| `[FinDWH].[dbo].[CompaniesMF]` | **Да** | 18 наших юрлиц + страна + CurrID |
| `[FinDWH].[dbo].[Counterparty]` | **Да** | имена контрагентов |
| `[SRV-SQL].Checks.dbo.CurrencyDaily` | **Да** | курсы валют |
| `[srv-sql].Gpartner.dbo.valuta1` / `valuta` | **Да** | `CurrID → код USD/EUR/...` |

## 9. Архитектурный набросок реализации

```
go/internal/reports/debt/
├── repo_premaster.go      ← Реализовать: 2 SQL (UNION ALL Dr/Cr) для свёртки, 1 SQL для drill-down. WITH (NOLOCK).
├── chart_of_accounts.go   ← НОВЫЙ. Хардкод из ТЗ: country → {dz, kz, revenue} счетов.
├── dim_companies.go       ← НОВЫЙ. Lookup CompaniesMF (cache 1×день).
├── dim_counterparty.go    ← НОВЫЙ. Lookup Counterparty + TRIM ведущих пробелов, грязные ключи отфильтровать.
├── service.go             ← Уже есть. Подключить parsing Mapping (regex), агрегации опять же.
├── handler.go             ← Уже есть. Расширить параметрами OnlyICO, Currency, Country.
└── parse_doc.go           ← НОВЫЙ. Regex для Mapping и TransDescription, плюс fallback на DocID.
```

**Из реализации намеренно НЕ берём** готовые витрины (§8.1). Если в обсуждении с автором ТЗ они окажутся приемлемыми — добавим как альтернативный путь, не заменяя свёртку из Premaster.

## 10. Финальный план реализации

### 10.1 Что блокирует и что нет (после итерации 2)

| Готово к коду | Заблокировано пользовательским решением |
|---|---|
| Свёртка Opening/Turnover/Closing × DZ/KZ по Premaster — формулы и индексы | План счетов ДЗ/КЗ/Revenue по странам (`§ 8.5` — нужно ТЗ-приложение) |
| Drill-down по `DocID` + `Objects` (имя документа без regex) | Контракт/срок оплаты (`§ 8.2` — источник не найден, нужно решение) |
| Lookup юрлиц и контрагентов — есть напрямую в SQL JOIN | Что считать ВГО (`§ 8.7` — флаг или дефолт фильтра) |
| ВГО-фильтр по колонке `Premaster.ICO` | Решение про готовые витрины (`§ 8.1`) — для сверки или замены |
| **Валюта и пересчёт в BYN** — через `[SRV-SQL].Checks.dbo.CurrencyDaily` + `CompaniesMF.CurrID` | |
| **Resolver GUID 1С** — через `Objects` | |

### 10.2 Очерёдность реализации (revised)

Один обогащённый SQL (см. § 8a.2) даёт сразу имена, страну, валюту и курс — реализация **компактнее**, чем в v0.4.

**M1 — Skeleton + одна страна (РФ), без drill-down**
1. `chart_of_accounts.go` — РФ-счета из ТЗ (60.01/02, 62.01/02, 76.05/06/09/10, 90.01.1). Остальные — TODO.
2. `repo_premaster.go::FetchRows` — **один SQL** с join'ами на `CompaniesMF`, `CurrencyDaily`, `Counterparty`, `Objects` (как § 8a.2), затем сама свёртка `Dr/Cr UNION ALL` поверх. Кэширование `CompaniesMF` (18 строк) в памяти — опционально.
3. `service.go::BuildReport` — разворот `signed_sum → OpeningDZ/KZ` по `accountKinds(country, account)`.
4. `handler.go` — подключить вместо mock.
5. Smoke: апрель 2026, `CompanyID=6950135110`; сверить значения с ручной выборкой через `mssql-probe`.

**M2 — Drill-down**
6. `repo_premaster.go::FetchDocuments(CompanyID, CounterpartyID, accounts, dateFrom, dateTo)` — выборка проводок с `LEFT JOIN Objects` для `DocName1C`.
7. `service.go::BuildDrilldown` — группировка по `DocID`, `DocumentRow.DocKind/DocNumber` берётся ИЗ `Objects.Name` (без regex). Если `Objects.Name` пуст — fallback на `parse_doc.go` (regex по `Mapping`/`TransDescription`).
8. `handler.go::Drilldown`.
9. Smoke: открыть документ.

**M3 — Многострановость + ВГО**
10. Заполнить `chart_of_accounts.go` для всех 9 стран из ТЗ-приложения.
11. Параметр `OnlyICO bool` → `WHERE A.ICO = 1`.
12. Тесты на 2-значный (РФ) и 4-значный (КЗ) планы.

**M4 — Валюта в DebtRow + Revenue**
13. **Resolver валют:** разовая загрузка справочника `[srv-sql].[Gpartner].[dbo].[valuta1]` (`CurrID → код USD/RUB/...`) в кэш Go при старте. Маппинг `CurrID → string`.
14. `DebtRow.Currency` ← `CompaniesMF.CurrID` через resolver.
15. (Опционально) `DebtRow.AmountBYN` — пересчёт через `rate_to_byn` из основного SQL.
16. `RevenuePeriod`/`RevenueLastMonth` — те же SQL с фильтром по revenue-счетам.

**M5 — Ageing + Contract + сверка с витринами**
17. Подсасывать `CubeBadDebt` для ageing-корзин (`SUM30/60/90/999`) → расширить `DebtRow.Ageing*`.
18. (после ответа автора ТЗ по § 8.2) `Contract` + `PaymentTermDays` → `PaymentDueDate/OverdueDays`. Возможно через `[Payments].[report].[FinDebt1]` или прямой запрос к 1С.
19. Утилита сверки: наш свод vs `FinancialReportСounterparties` — diff-репорт.

### 10.3 Что нужно получить ОТ автора ТЗ до старта M1

Сократилось до **3 пунктов** (валюта и resolver GUID уже найдены):

1. **План счетов по странам** (приложение к ТЗ «счета БУ»).
2. **Определение ВГО**: «только ВГО» (default ON) или один из срезов в UI (default OFF).
3. **Гранулярность периода** — день / месяц / произвольно. От этого зависит UX фильтра.

Опционально:
4. **Учитывать ли ручные корректировки** аналитиков (`[002 List of adj-ts]`, `[Adj001SamovyvozInternetShop]`) — см. § 8a.3.
5. **Сверка с готовыми витринами** (§ 8.1) — для понимания расхождений (M5 task #19).

### 10.4 Производительность и риски

- Один свёрточный запрос Premaster: 5-10 компаний × 1 год → оценка ~10-30 секунд через индекс `(CompanyID, Date)`. Для UI — асинхронная загрузка с прогрессом или фоновый рендер с кешем (`debt_saved_filters` уже есть в Postgres).
- Drill-down: моментально (мс).
- При увеличении до 18 компаний × 5 лет — может стать минутами; стоит подумать о ночной материализации в наш Postgres (отдельный stretch goal в M5+).
- ETL Premaster может ломаться — наш отчёт это унаследует. В UI показать `CompaniesMF.ClosedPeriod` (последний закрытый месяц) как индикатор актуальности.
