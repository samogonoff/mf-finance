# План доработки отчёта «Задолженность ВГО»: пересчёт на Doc_Date + свёртка начало/оборот/конец

> Статус: **черновик на согласование** (аналитик + разработка). Основан на внутреннем
> документе аналитика «Логика формирования отчёта ВГО» (2026-07). Решение по объёму:
> реализуем **полную новую методику** (не только пересчёт валют).
>
> Связанные доки: [findebt-runbook.md](findebt-runbook.md),
> [findebt-verification.md](findebt-verification.md), [tz-requirements.md](tz-requirements.md),
> [payments-source-map.md](payments-source-map.md), [open-questions.md](open-questions.md).

> **Статус реализации (обновлено — блокер снят аналитиком).**
> Аналитик разрешил блокер: `FinDebt3` ↔ `Debt_arh` **связывать не нужно и нельзя**
> (`Doc_Number` не уникален). `FinDebt1` — только whitelist пар `(UNPOrg, Acc)`;
> суммы/документы — из `Debt_arh`/`Wholesales_arh` по `DocID`; агрегация по
> `(UNPOrg, Acc[, контрагент])`. Полная методика: `vgo-doc-date-methodology-v2.md`
> нет — см. §12 ниже, обновлённый под метод аналитика.
> - ✅ **`DEBT_BACKEND=findebt-docdate`** — РЕАЛИЗОВАН как метод аналитика (суммы из
>   `Debt_arh`/`Wholesales_arh`, пересчёт на дату документа). Второй поток рядом с
>   `findebt` (дефолт), для сверки. См. §12.
> - 🟡 **Открытые §9-риски** заложены с дефолтами/флажками: обороты только
>   `Wholesales_arh(DrAcc)` (контрольное равенство может не сойтись — нужен
>   `Payment_arh`/`CrAcc`); контрагент опционален (`MSSQL_DEBT_ARH_CPARTY_COL`);
>   `Debt_arh_свернутая`/RUR 643vs860/знак Дт-Кт/канон. курс USD — на сверку.

## 0. TL;DR

Новая методика меняет **два** принципа текущего findebt-контура:

1. **Валюта считается на дату документа (`Doc_Date`), а не на дату снэпшота.** Сейчас
   BYN/USD приходят готовыми «линзами» из `FinDebt3`, пересчитанными по курсу на
   отчётную дату → сумма в USD «плавает» от снэпшота к снэпшоту для неизменного
   документа. Цель — считать BYN/USD самим: `сумма_в_валюте_договора × курс(Doc_Date)`.
2. **Начало/оборот/конец периода — из реальных источников, а не разница снэпшотов.**
   Сейчас оборот = `closing − opening` двух снэпшотов `FinDebt3`. Цель:
   - сальдо на начало/конец — из `Debt_arh` (дедуп по `DocID`, «первое появление»);
   - оборот — из реальных движений `Wholesales_arh`/`Payment_arh`;
   - контрольное соотношение `(Дт нач − Кт нач) + (Дт об − Кт об) = (Дт кон − Кт кон)`.

Плюс: обработка «Корректировки долга» отдельной строкой (§7), выручка `Реализация%`,
статические справочники счетов/баз.

⚠ **Это разворот принципа «числа чужого ETL, свою свёртку не считаем».** Текущий
findebt намеренно доверяет `FinDebt3` (сверен с 1С копейка-в-копейку). Новая методика
вводит **свою свёртку** из сырых `Debt_arh` + движений. Поэтому новый контур
включается **под гейтом сверки** (§9) и не становится дефолтом, пока числа не сойдутся
с 1С — ровно как findebt в своё время.

## 1. Что уже есть в коде (не переписываем зря)

| Требование документа | Текущая реализация | Вывод |
|---|---|---|
| Период, колонки «Начало / Обороты / Конец», выручка | UI `DebtTable.vue`, поля `DebtRow` (`go/internal/reports/debt/model.go:58-91`) | структура готова, меняем **наполнение** |
| Пересчёт BYN/USD | 3 линзы `CUR_FILTER` из `FinDebt3`, пересчёт на дату снэпшота | заменяем на пересчёт по `Doc_Date` |
| Обороты | `close_dt − open_dt` (`repo_findebt_ch.go:73-110`) | заменяем на движения |
| Сальдо нач/кон из `Debt_arh` дедуп `DocID` | нет; дедуп по бизнес-ключу `FinDebt3` (ReplacingMergeTree) | новый источник |
| `CurrencyDaily`, `valuta` (NAIM→KOD), ASOF | нет нигде в Go | новый ETL + таблицы CH |
| «Корректировка долга» (DrAcc=CrAcc) | не обрабатывается | новая логика |
| Выручка `Реализация%` (`revenue_period`) | поле есть, но = 0 в findebt-пути | заполняем |

Итого фронт и модель ответа менять почти не нужно — вся работа в **ETL и запросах CH**.

## 2. Источники (сводно)

| Объект (MSSQL) | Роль | Куда в CH |
|---|---|---|
| `[Payments].[report].[FinDebt3]`, `CUR_FILTER='В валюте договора'` | факт документа в исходной валюте (база, срок, описание, договор) | `fact_findebt_ccy` (уже есть) |
| `[Payments].[report].[FinDebt1]` | отбор активных пар контрагент/счёт по каналу (`DebtClass`) | только фильтр |
| `[SRV-SQL].Gpartner.dbo.valuta` | справочник валют NAIM→KOD (с паддинг-пробелами → `RTRIM`) | `dim_valuta` (новая) |
| `[SRV-SQL].Checks.dbo.CurrencyDaily` | ежедневные курсы к BYN (`KOD, date, curr_rate`) | `currency_daily` (новая) |
| `Payments.dbo.Debt_arh` / `Debt_arh_свернутая` | суточный снэпшот остатка, дедуп по `DocID` → сальдо нач/кон | `fact_debt_arh` (новая) |
| `Payments.dbo.Wholesales_arh` / `Payment_arh` | реальные движения (отгрузки/оплаты/корректировки) → обороты | `fact_debt_moves` (новая) |

**⚠ Открытый вопрос по композиции источников** (в согласование): документ берёт
базу документа из `FinDebt3`, но сальдо нач/кон — из `Debt_arh`. `FinDebt3` уже
производен от `Debt_arh`. Нужно зафиксировать с аналитиком: (а) начало/конец берём
из `Debt_arh` дедуп-DocID, а `FinDebt3` даёт только обогащение (договор/срок/описание);
(б) ключ связи `Debt_arh.DocID` ↔ `FinDebt3` (по DocID нет — см. [debt-docid-bridge],
матч Premaster↔Docs шёл по договору, не по документу). **Без явного ключа связи сальдо
из Debt_arh и обогащение из FinDebt3 не соединяются построчно** — это блокер Фазы B.

## 3. Принцип пересчёта валюты (Doc_Date)

Все три показателя — на **одну** дату `Doc_Date`:
- **Валюта договора** — `SUM_D − SUM_K` из `FinDebt3` без изменений.
- **BYN** = `Сумма × Курс(валюта_договора → BYN, на Doc_Date)`. Если документ уже в BYN — курс = 1.
- **USD** = `BYN / Курс(USD[KOD=840] → BYN, на Doc_Date)`. Кросс всегда через BYN.

Ловушки (§4 документа):
- `Debt_arh.Date` = `datetime 23:59:59`, `CurrencyDaily.date` = чистый `DATE` → сравнивать в `CONVERT(DATE, …)` с обеих сторон.
- `FinDebt3.Currency` (ветка «В валюте договора») — текстовый **NAIM** (`RUR.`), не **KOD** → маппинг через `valuta` с `RTRIM`.
- Нет курса на дату (выходной) → `ISNULL(...,1)` в SQL Server молча ставит 1 (ошибка без ошибки). В CH решаем **ASOF JOIN** (последний известный курс ≤ Doc_Date). `coalesce(rate,1)` допустим только для документов, уже в BYN.
- Linked-server джойн к курсам построчно = удалённый nested loop, десятки минут → справочники выгружаем **целиком один раз**, ASOF выполняем локально в CH.

## 4. Целевые таблицы ClickHouse (миграция `010`)

Схема `clickhouse/migrations/010_debt_currency_and_moves.up.sql` (накат
`swarm/migrate-clickhouse.sh`, идемпотентно, по одному statement — см.
[infra-clickhouse-http-multistatement]).

```sql
-- Справочник валют NAIM -> KOD
CREATE TABLE IF NOT EXISTS finance.dim_valuta (
    kod   Int32,
    naim  String                 -- уже RTRIM на этапе ETL
) ENGINE = ReplacingMergeTree ORDER BY (naim);

-- Курсы к BYN, ORDER BY (kod, date) обязателен для ASOF JOIN
CREATE TABLE IF NOT EXISTS finance.currency_daily (
    kod        Int32,
    date       Date,
    curr_rate  Decimal(18, 6)
) ENGINE = ReplacingMergeTree ORDER BY (kod, date);

-- Снэпшоты Debt_arh, дедуп «первое появление DocID» -> сальдо нач/кон
CREATE TABLE IF NOT EXISTS finance.fact_debt_arh (...);   -- поля уточнить по Debt_arh

-- Реальные движения (отгрузки/оплаты/корректировки) -> обороты
CREATE TABLE IF NOT EXISTS finance.fact_debt_moves (
    move_date     Date,          -- дата проводки (уникальна на движение)
    doc_id        String,
    company_id    LowCardinality(String),
    counterparty_id String,
    acc           LowCardinality(String),
    document_type String,        -- 'Реализация...', 'Корректировка долга...'
    dr_acc        String,        -- для детекта DrAcc=CrAcc
    cr_acc        String,
    src_kod       Int32,         -- валюта движения -> для ASOF FX
    amount        Decimal(18,2),
    ...
) ENGINE = ReplacingMergeTree ORDER BY (...);
```

Существующая `fact_findebt_ccy` (миграция 009) остаётся — база документов/срока/описания.

## 5. ETL (Go)

Все экстракторы — рядом с существующими в `go/internal/etl/`, оркестрация через
`cmd/findebt-etl` (добавить `MODE`).

1. **`extract_currency.go`** (новый):
   - `valuta` — полная разовая выгрузка `SELECT KOD, RTRIM(NAIM) FROM [SRV-SQL].Gpartner.dbo.valuta` → `dim_valuta`.
   - `CurrencyDaily` — полная + инкрементальная по `date` (watermark `max(date)` из CH) → `currency_daily`.
   - Доступ через linked server 4-частными именами с текущего OLAP-коннекта (`MSSQL_PREMASTER_*`). **Проверить, что `[SRV-SQL]` виден** — иначе нужен отдельный коннект (Фаза 0).
2. **`extract_debt_arh.go`** (новый): снэпшоты `Debt_arh` с дедупом по `DocID`
   («первое появление»), `CONVERT(DATE, …)` на датах → `fact_debt_arh`.
3. **`extract_debt_moves.go`** (новый): `Wholesales_arh` + `Payment_arh`, включая
   `DocumentType`, `DrAcc`, `CrAcc` (для корректировок §7) → `fact_debt_moves`.
4. **Воркеры**: расширить `worker_findebt.go` или добавить тикеры под новые источники
   (интервалы из ENV, `<=0` = выкл).
5. **`.env.example`** — новые ENV в **том же коммите** (иначе прод упадёт после
   деплоя, см. [feedback-env-example-must-document]): имена linked-server БД/таблиц
   (`MSSQL_VALUTA_FQN`, `MSSQL_CURRENCY_DAILY_FQN`, `MSSQL_DEBT_ARH_FQN`,
   `MSSQL_WHOLESALES_ARH_FQN`, `MSSQL_PAYMENT_ARH_FQN`), интервалы синхронизации.

## 6. Отчётный запрос (пересчёт + свёртка)

Правки в `go/internal/reports/debt/repo_findebt_ch.go`.

**Пересчёт валют (заменяет линзы BYN/USD):**
- В `findebtReportQuery` (сейчас `repo_findebt_ch.go:73-110`) вместо выбора готовой
  линзы `cur_filter` — брать «В валюте договора» и умножать на курс через
  **ASOF LEFT JOIN**:
  ```sql
  ... f
  ASOF LEFT JOIN finance.currency_daily rSrc ON rSrc.kod = f.src_kod AND rSrc.date <= f.doc_date
  ASOF LEFT JOIN finance.currency_daily rUsd ON rUsd.kod = 840      AND rUsd.date <= f.doc_date
  -- Saldo_BYN = Saldo_Contract * coalesce(rSrc.curr_rate, 1)
  -- Saldo_USD = Saldo_BYN / nullIf(rUsd.curr_rate, 0)
  ```
- `src_kod` — маппинг `NAIM→KOD`: денормализовать в факт при ETL (join к `dim_valuta`
  с `RTRIM`) **или** join в запросе. Предпочтительно в ETL (ASOF-джойн проще).
- Линза `lens` (`lens.go`) остаётся API-контрактом (валюта договора / BYN / USD), но
  BYN/USD теперь **вычисляемые**, а не отдельные строки `cur_filter`. Это упрощает и
  дедуп: `cur_filter` можно перестать тащить ×3 (ревизия миграции 009 — опционально,
  во второй итерации).

**Свёртка начало/оборот/конец (заменяет разницу снэпшотов):**
- Сальдо нач/кон — из `fact_debt_arh` (дедуп-DocID, «до начала» / «до-на конец»).
- Обороты — из `fact_debt_moves` внутри `[from, to]`.
- Заполнить `revenue_period` = обороты `document_type LIKE 'Реализация%'`.
- Добавить лог/проверку **контрольного соотношения** на строку (§9).

Модель ответа `DebtRow` (`model.go:58-91`) и поля `opening_*/turnover_*/closing_*/
revenue_period` уже есть — правим только их источник, не контракт API.

## 7. Корректировка долга (§8 документа)

- Детект: `document_type LIKE 'Корректировка долга%'` **и** `dr_acc = cr_acc`.
- Учитывать как **самостоятельное движение** в обороте на дату своей проводки
  (наравне с отгрузками/оплатами), **не** замена суммы в валюте договора.
- Пересчёт BYN — по курсу на **дату корректировки** (её `Doc_Date`), не на дату
  первичного договора (это разные документы/даты/валюты).
- В отчёте — **отдельная строка**: тот же Компания/Партнёр/Счёт, но другой
  Договор/Дата/Валюта. Не схлопывать с исходным договором.
- Если корректировка до начала периода — входит в сальдо на начало наравне с прочими.
- В выручку **не** включается.

## 8. Справочники и выгрузка (§5, §9 документа)

- Статические списки в коде (не отдельный SQL-объект): «Наименование базы»/«Компания»
  по `UNPOrg`, справочник счетов БУ (счёт = первые 2 цифры `Acc`, субсчёт = `Acc`
  целиком). Свериться с текущими `accountNameFor`/`account_name`/`subaccount_name`.
- Целевая структура выгрузки — лист «шаблон выгрузки данных» приложения к ТЗ
  (19 колонок, см. [tz-requirements.md](tz-requirements.md)).
- Оживить кнопки Excel/Печать (сейчас декоративные, `nuxt/pages/reports/index.vue`).

## 9. Гейт сверки (обязателен до дефолта)

Новый контур включается флагом (напр. `DEBT_BACKEND=findebt-moves` или
`DEBT_METHOD=doc-date`), дефолт остаётся `findebt`, пока не сойдётся:
1. **Контрольная пара** МФ→Формэль: ДЗ `866 791.74` / КЗ `53 196 999.09` на
   `2026-05-31` (как в [findebt-runbook.md](findebt-runbook.md)).
2. **Контрольное соотношение** по каждой строке:
   `(Дт нач − Кт нач) + (Дт об − Кт об) = (Дт кон − Кт кон)`. Рассинхрон = пропущен
   источник движений (не учтён `Payment_arh` / `Debt_arh_свернутая`) → лог + метрика.
3. Точечная сверка пересчёта USD на `Doc_Date` против 1С на нескольких документах.

## 10. Порядок работ (по зависимостям)

1. **Фаза 0 — согласование** (§2 открытый вопрос композиции источников; ключ связи
   `Debt_arh`↔`FinDebt3`; доступность `[SRV-SQL]`; справочники баз/счетов; периодичность).
2. **Пересчёт валют на Doc_Date**: миграция `dim_valuta`+`currency_daily`,
   `extract_currency.go`, ASOF в `repo_findebt_ch.go`. Самодостаточно, чинит §1.
3. **Свёртка из Debt_arh + движений**: `fact_debt_arh`/`fact_debt_moves`,
   `extract_debt_arh.go`/`extract_debt_moves.go`, замена свёртки в отчёте, выручка.
4. **Корректировки долга** (§7) — поверх движений.
5. **Справочники + выгрузка** (§8), гейт сверки (§9), затем переключение дефолта.
6. Обновить [findebt-runbook.md](findebt-runbook.md) (заодно исправить расхождение
   имени `fact_findebt` → `fact_findebt_ccy`).

## 12. Реализовано: поток `findebt-docdate` = метод аналитика

Второй бэкенд рядом с `findebt` (дефолт), переключается через `DEBT_BACKEND`. Полностью
по методике аналитика (§4-§7): суммы из сырых `Debt_arh`/`Wholesales_arh` (не FinDebt3),
пересчёт BYN/USD на дату документа через ASOF, агрегация по `(UNPOrg, Acc[, контрагент])`.

**Что добавлено:**
- CH-миграции: `010_debt_currency` (`dim_valuta`, `currency_daily`) +
  `011_debt_arh_facts` (`debt_facts` из `Debt_arh` дедуп по DocID; `turnover_facts`
  из `Wholesales_arh`).
- ETL: `extract_currency.go` (+`worker_currency.go`, `MODE=currency`) и
  `extract_debtarh.go` (+`worker_debtarh.go`, `MODE=debtarh`). Whitelist `(UNPOrg,Acc)`
  из `FinDebt1` на последнюю дату канала ВГО; полный reload; контрагент опционален
  (`MSSQL_DEBT_ARH_CPARTY_COL`, валидируется как идентификатор).
- Бэкенд `repo_findebt_docdate_ch.go`: два запроса (сальдо из `debt_facts` порогом
  по `DocDate`; обороты+выручка из `turnover_facts`), слияние по `(company,cparty,acc)`
  в Go. Пересчёт `docDateConvExpr` (ASOF `currency_daily`). Drill-down по `DocID` со
  знаком и флагом «на начало / внутри периода».
- Env (`.env.example`+`config.go`): `MSSQL_VALUTA_FQN`, `MSSQL_CURRENCY_DAILY_FQN`,
  `CURRENCY_SYNC_INTERVAL`, `MSSQL_DEBT_ARH_FQN`, `MSSQL_WHOLESALES_ARH_FQN`,
  `MSSQL_DEBT_ARH_CPARTY_COL`, `DEBTARH_SYNC_INTERVAL`.

**Расчёт (проверено на CH 24.3, эталон RUR 1221002.01@2024-07-01 → USD≈14153):**
- сальдо на начало = `SUM(amt_t)` где `DocDate < from`; на конец = где `DocDate <= to`;
  знак: `amt_t >= 0` → ДЗ, `< 0` → КЗ по модулю;
- обороты = `turnover_facts` за `[from,to]`; выручка = `DocType LIKE 'Реализац%'`
  (корректировки долга в выручку НЕ входят — проверено: `turn_dz` включает
  корректировку, `revenue` — нет);
- `Amount_Target = Amount × курс(вал→BYN) / (target=BYN?1:курс(target→BYN))` на дату
  документа через ASOF.

**CH-ловушки (найдены и обойдены):**
- ASOF требует **колоночного** equi-join: код USD — колонкой `usd_kod=toInt32(840)`,
  не константой `rUsd.kod=840` (иначе «needs at least one equi-join column»).
- Пустой `sumIf` по USD-линзе (Nullable из-за деления) → `NULL`; обёрнуто `ifNull(…,0)`.
- Шэдоуинг алиаса: `toString(doc_date) AS doc_date` затеняет Date-колонку — `is_opening`
  вынесен в подзапрос.

**Как запустить и сверить:**
1. Накатить CH-миграции 010+011.
2. Залить курсы (`MODE=currency`) и факты (`MODE=debtarh`) — как bootstrap findebt
   ([findebt-runbook.md](findebt-runbook.md)). Проверить `count()` в `currency_daily`,
   `debt_facts`, `turnover_facts`.
3. Проверить `[SRV-SQL]` доступен (иначе подменить FQN курсов); проверить, есть ли в
   `Debt_arh` УНП контрагента → задать `MSSQL_DEBT_ARH_CPARTY_COL`.
4. Прогнать отчёт под `findebt` и `findebt-docdate` на одних фильтрах; сверить
   контрольную пару МФ→Формэль и контрольное равенство
   `(Дт нач−Кт нач)+(Дт об−Кт об)=(Дт кон−Кт кон)`.

**Открытые §9-риски (на сверку, не блокируют):** обороты только `Wholesales_arh(DrAcc)`
— если есть `Payment_arh`/`CrAcc`-движения, контрольное равенство разойдётся, добавить;
`Debt_arh_свернутая` для старых периодов (UNION); RUR 643 vs 860; знак Дт/Кт; канон.
курс USD (BYN-мост vs прямой ЦБ РФ, расхождение ~5%); «закрытые до периода» документы
в `closing` (порог по `DocDate <= to` не отсекает погашенные — сверить с бухгалтерией).

## 11. Риски

- **Linked-server**: построчный джойн к курсам = десятки минут → материализация
  справочников целиком, ASOF локально. OLAP флапает на больших сканах
  ([infra-olap-mssql-tds-handshake]) — фильтровать по индексируемым колонкам.
- **Двойная популяция**: `FinDebt3` (база) vs `Debt_arh` (сальдо) — разные наборы
  документов, ключ связи по `DocID` исторически не матчился ([debt-docid-bridge]).
  **Главный блокер Фазы 3** — решить в Фазе 0.
- **Разворот принципа доверия FinDebt3** — своя свёртка требует полной сверки с 1С
  (гейт §9), иначе риск чисел мимо 1С.
- **Корректировка ≈, но ≠ курсовой пересчёт** (§8, расхождение 3-5%) — вести отдельной
  строкой, не «чинить» в курс.
