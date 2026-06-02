# Probe: договоры в Premaster1C (задача 4.1, отчёт #5)

> Цель — установить, **в каком субконто** проводки лежит договор и **резолвится
> ли** он в читаемое имя/предмет через `Objects`, плюс поискать срок отсрочки.
> Всё это нужно выяснить ДО реализации вывода договоров в коде: гадать нельзя —
> выведем неверные данные (принцип «лучше пустой экран, чем неверная цифра»).
>
> Запускать под VPN с доступом к OLAP `10.10.6.15`. Утилита: `cmd/mssql-probe`,
> режим `PROBE_SQL`. Все запросы стартуют с `CompanyID='9731039708'` (ПТИР) —
> узко по индексу `(CompanyID, Date)`, без fullscan. `WITH (NOLOCK)` обязателен.

ENV (один из наборов): `OLAP_HOST/OLAP_PORT/OLAP_USER/OLAP_PASSWORD/OLAP_DATABASE`
или `MSSQL_PREMASTER_SERVER/_PORT/_USER/_PASSWORD/_DB`. Длинные имена объектов:
`PROBE_FULL_TEXT=1`, чтобы не обрезались на 80 символах.

## Шаг 0 — какие субконто-колонки есть в Premaster1C

```bash
PROBE_TABLE=dbo.Premaster1C go run ./cmd/mssql-probe | grep -i subconto
```

Ожидаем `DrSubconto1..4`, `CrSubconto1..4` (см. schema-draft.md §3.6). Если
индексов меньше — в запросах ниже убери отсутствующие.

## Шаг 1 — резолв всех субконто на реальных ВГО-проводках ПТИР

Берём расчётные счета (62/60/76) и резолвим каждое субконто через `Objects`.
Цель — увидеть, в какой колонке стоит **договор** (а не контрагент/статья/счёт).

```bash
PROBE_FULL_TEXT=1 PROBE_SQL="
SELECT TOP 25
  LEFT(p.DrAcc,2) AS dr2, LEFT(p.CrAcc,2) AS cr2,
  od1.[Name] AS DrSub1, od2.[Name] AS DrSub2, od3.[Name] AS DrSub3,
  oc1.[Name] AS CrSub1, oc2.[Name] AS CrSub2, oc3.[Name] AS CrSub3,
  p.Mapping
FROM [FinDWH].[dbo].[Premaster1C] p WITH (NOLOCK)
LEFT JOIN [FinDWH].[dbo].[Objects] od1 WITH (NOLOCK) ON od1.ID = p.DrSubconto1
LEFT JOIN [FinDWH].[dbo].[Objects] od2 WITH (NOLOCK) ON od2.ID = p.DrSubconto2
LEFT JOIN [FinDWH].[dbo].[Objects] od3 WITH (NOLOCK) ON od3.ID = p.DrSubconto3
LEFT JOIN [FinDWH].[dbo].[Objects] oc1 WITH (NOLOCK) ON oc1.ID = p.CrSubconto1
LEFT JOIN [FinDWH].[dbo].[Objects] oc2 WITH (NOLOCK) ON oc2.ID = p.CrSubconto2
LEFT JOIN [FinDWH].[dbo].[Objects] oc3 WITH (NOLOCK) ON oc3.ID = p.CrSubconto3
WHERE p.CompanyID='9731039708' AND p.ICO=1
  AND (LEFT(p.DrAcc,2) IN ('62','60','76') OR LEFT(p.CrAcc,2) IN ('62','60','76'))
" go run ./cmd/mssql-probe
```

**Что искать в выводе:** в одной из колонок `*Sub2/*Sub3` будет имя вида
«Договор № … от …» / «Договор поставки …» — это и есть наш источник `Contract`.
Колонка с ИНН/именем контрагента — это субконто «Контрагенты», не договор.
Запомни: какой именно индекс (Dr/Cr, 1/2/3) держит договор для счетов 62/60/76.

## Шаг 2 — что внутри имени договора (название + предмет?) и срок оплаты

Когда из Шага 1 известна колонка-договор (пусть `CrSubconto2`), посмотреть
уникальные значения и понять, есть ли в имени **предмет**, и нет ли отдельного
объекта со сроком оплаты:

```bash
PROBE_FULL_TEXT=1 PROBE_SQL="
SELECT DISTINCT TOP 30 o.[Name] AS contract_name
FROM [FinDWH].[dbo].[Premaster1C] p WITH (NOLOCK)
JOIN [FinDWH].[dbo].[Objects] o WITH (NOLOCK) ON o.ID = p.CrSubconto2
WHERE p.CompanyID='9731039708' AND p.ICO=1 AND LEFT(p.CrAcc,2)='62'
" go run ./cmd/mssql-probe
```

Если в `Name` виден только номер/дата договора, но НЕ предмет/срок — значит
предмет и срок отсрочки лежат в самой 1С (реквизиты справочника договоров),
которой в FinDWH нет. Тогда срок отсрочки — **открытый вопрос к автору ТЗ**
(вариант из open-questions.md §B3: либо отдельный справочник, либо корзины
просрочки из `CubeBadDebt`).

## Что прислать обратно

1. Из Шага 1 — какая колонка субконто = договор для счетов 62/60/76 (Dr или Cr, индекс).
2. Из Шага 2 — пример 3–5 имён договоров: видно ли название/предмет, есть ли срок.
3. Найдено ли где-либо поле срока оплаты/отсрочки (или подтверждение, что его нет).

С п.1–2 я реализую вывод названия+предмета договора (задачи 4.2/4.3). П.3
определит судьбу даты отсрочки.
