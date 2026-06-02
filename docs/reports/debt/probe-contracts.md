# Probe: договоры в Premaster1C (задача 4.1, отчёт #5)

> Цель — установить, **в каком субконто** проводки лежит договор и **резолвится
> ли** он через `Objects`, плюс поискать срок отсрочки. Выяснить ДО реализации:
> гадать нельзя — выведем неверные данные.
>
> Запускать под VPN (OLAP `10.10.6.15`). Утилита: `cmd/mssql-probe`, режим
> `PROBE_SQL`. ENV: `OLAP_*` или `MSSQL_PREMASTER_*`. Длинные имена не обрезать —
> `PROBE_FULL_TEXT=1`. `WITH (NOLOCK)` обязателен.

## ✅ Результат Шага 1 (ПТИР, счета 60/76) — найдено

Для **кредиторских** проводок (60, 76):

| субконто | содержимое | пример |
|---|---|---|
| **`CrSubconto1`** | **договор** ← | «20/09 от 20.09.2021», «Дополнительное соглашение к Договору 20/09…», «Оферта (возмещение ДМС…)» |
| `CrSubconto2` | документ расчётов / контрагент | «Поступление (акт…) 00БС-000058…» |
| `CrSubconto3` | контрагент | «ООО Торговый Дом „Марк Формэль“» |
| `DrSubconto1` | статья встречного счёта (≈ предмет) | «Аренда помещений», «Аренда имущества», «ДМС» |

- Договор резолвится в `Objects.Name` и несёт **название** (номер + дата). Отдельного
  поля «предмет» нет — как предмет используем статью встречного субконто (`DrSubconto1`).
- **Срок отсрочки нигде не виден** → источника нет, это вопрос к автору ТЗ (см. §B3).

**Решение по выводу (согласовано):** две колонки — «Договор» (`CrSubconto1`) и
«Статья/предмет» (встречное субконто).

## ✅ Результат Шага 1b (ТД, счёт 62) — состав субконто ДРУГОЙ

Для **дебиторских** проводок (Dr `62.01` / Cr `90.01.1`):

| субконто | содержимое | пример |
|---|---|---|
| `DrSubconto1` | контрагент | «MARK FORMELLE IT, ООО», «ООО „Формэль“» |
| **`DrSubconto2`** | **договор** ← | «Договор поставки № 17/12 от 17.12.2025» |
| `DrSubconto3` | документ реализации | «Реализация (акт…) ТДБП-000001…» |
| `CrSubconto2` | номенклатура (≈ предмет) | «Швейная машина LK1900…», «Гольфы женские…» |
| `CrSubconto3` | группа номенклатуры | «Экспортные товары» |

## ⚠ Ключевой вывод: позиция субконто зависит от счёта

| счёт | договор | контрагент | предмет |
|---|---|---|---|
| 62 (ДЗ) | `DrSubconto2` | `DrSubconto1` | `CrSubconto2` (номенклатура) |
| 60 (КЗ) | `CrSubconto1` | `CrSubconto3` | `DrSubconto1` (статья) |
| 76 (КЗ) | `CrSubconto1` | `CrSubconto2` | `DrSubconto1` (статья) |

Фиксированную «колонку договора» задать **нельзя** — состав субконто разный даже
между 60 и 76. Надёжный путь для MVP — резолвить все субконто расчётной стороны и
определять договор **эвристикой по содержимому** имени (`Договор|Соглашен|Оферт|
Контракт|№|\d+/\d+|от ДД.ММ.ГГГГ`); контрагента мы и так знаем из `CounterpartyID`;
предмет — номенклатура/статья со встречной стороны. Точную карта-альтернативу
(вид субконто по счёту) можно взять из `DimSubkonto`/1С — вопрос к автору ТЗ.

## Шаг 2 (быстрый) — уникальные договоры: видно ли предмет/срок

Старый Шаг 2 (`DISTINCT` без периода) сканировал всю историю — оттого долгий.
Быстро: **узкий период** + **один `INNER JOIN`** на правильное субконто (договор
есть всегда → INNER, не LEFT). Дебиторка (62, договор = `DrSubconto2`):

```bash
PROBE_FULL_TEXT=1 PROBE_SQL="
SELECT DISTINCT TOP 30 o.[Name] AS contract
FROM [FinDWH].[dbo].[Premaster1C] p WITH (NOLOCK)
JOIN [FinDWH].[dbo].[Objects] o WITH (NOLOCK) ON o.ID = p.DrSubconto2
WHERE p.CompanyID='6950135110' AND p.ICO=1
  AND p.[Date] >= '2026-01-01' AND p.DrAcc LIKE '62%'
" go run ./cmd/mssql-probe
```

Кредиторка (60/76, договор = `CrSubconto1`):

```bash
PROBE_FULL_TEXT=1 PROBE_SQL="
SELECT DISTINCT TOP 30 o.[Name] AS contract
FROM [FinDWH].[dbo].[Premaster1C] p WITH (NOLOCK)
JOIN [FinDWH].[dbo].[Objects] o WITH (NOLOCK) ON o.ID = p.CrSubconto1
WHERE p.CompanyID='9731039708' AND p.ICO=1
  AND p.[Date] >= '2025-01-01' AND p.CrAcc LIKE '60%'
" go run ./cmd/mssql-probe
```

Если год всё равно долго — сузь до месяца (`'2026-04-01'..'2026-05-01'`) или гоняй
по месяцам в цикле. Из вывода и так уже видно: в `Name` есть **номер+дата**, но
**нет срока отсрочки** — подтверждаем, что срок берём не отсюда (вопрос к автору ТЗ).

## Полная картина по одному ЮЛ (ПТИР `9731039708`, для ТЕКС — заменить ИНН на `5031159833`)

**A. С какими нашими РФ/РБ ЮЛ есть расчёты (+ метка ICO):**

```bash
PROBE_SQL="
SELECT LTRIM(RTRIM(p.CounterpartyID)) AS cp_inn, COUNT(*) AS rows,
       MAX(p.ICO) AS ico, MIN(p.[Date]) AS first_dt, MAX(p.[Date]) AS last_dt
FROM [FinDWH].[dbo].[Premaster1C] p WITH (NOLOCK)
WHERE p.CompanyID='9731039708' AND p.[Date] >= '2024-01-01'
  AND LTRIM(RTRIM(p.CounterpartyID)) IN
      ('690591512','690719790','6950135110','5031159833','9909349268','695018688905')
GROUP BY LTRIM(RTRIM(p.CounterpartyID))
ORDER BY rows DESC
" go run ./cmd/mssql-probe
```

Если в выводе только `6950135110` — расчёты фактически только с ТД. `ico` покажет,
помечены ли строки как ВГО (ожидаем смесь 0/1 — корень #4: union-фильтр их добирает).

**B. Все контрагенты ЮЛ (топ по объёму) — масштаб + внешние:**

```bash
PROBE_SQL="
SELECT TOP 30 LTRIM(RTRIM(p.CounterpartyID)) AS cp_inn, COUNT(*) AS rows, MAX(p.ICO) AS ico
FROM [FinDWH].[dbo].[Premaster1C] p WITH (NOLOCK)
WHERE p.CompanyID='9731039708' AND p.[Date] >= '2024-01-01'
  AND LTRIM(RTRIM(p.CounterpartyID)) <> ''
GROUP BY LTRIM(RTRIM(p.CounterpartyID))
ORDER BY rows DESC
" go run ./cmd/mssql-probe
```

**C. Все названия договоров ЮЛ (кредиторка CrSubconto1 + дебиторка DrSubconto2):**

```bash
PROBE_FULL_TEXT=1 PROBE_SQL="
SELECT DISTINCT name FROM (
  SELECT o.[Name] AS name
  FROM [FinDWH].[dbo].[Premaster1C] p WITH (NOLOCK)
  JOIN [FinDWH].[dbo].[Objects] o WITH (NOLOCK) ON o.ID = p.CrSubconto1
  WHERE p.CompanyID='9731039708' AND p.[Date] >= '2023-01-01'
    AND (p.CrAcc LIKE '60%' OR p.CrAcc LIKE '76%')
  UNION
  SELECT o.[Name]
  FROM [FinDWH].[dbo].[Premaster1C] p WITH (NOLOCK)
  JOIN [FinDWH].[dbo].[Objects] o WITH (NOLOCK) ON o.ID = p.DrSubconto2
  WHERE p.CompanyID='9731039708' AND p.[Date] >= '2023-01-01' AND p.DrAcc LIKE '62%'
) t ORDER BY name
" go run ./cmd/mssql-probe
```

## Почему Шаг 1 был медленным и как забирать быстро

Тормозил **сам probe**: 6 `LEFT JOIN` к `Objects` (5.2 млн) поверх скана всей
истории ПТИР + несаргируемый `LEFT(Acc,2)`. Рычаги:

1. **Probe:** добавлять `AND p.[Date] >= 'YYYY-01-01'` (режет по индексу
   `(CompanyID, Date)`) и резолвить только нужное субконто (1 join вместо 6).
2. **Продакшн — отдельный «забор» договоров НЕ нужен.** Договор уже лежит как
   субконто в тех же строках. Добавляем **1 `LEFT JOIN Objects` на `CrSubconto1`**
   (и `DrSubconto1` для 62) в существующий ETL-запрос — как сейчас джойнится
   `DocID→Objects`. Договор приедет в ClickHouse одним проходом (задача 4.2a).
3. **Чанкинг bootstrap по `Date`** (по годам) — каждый под-запрос узкий по индексу
   `(CompanyID, Date)`, против таймаутов на полной истории. Это и есть «кусками».
