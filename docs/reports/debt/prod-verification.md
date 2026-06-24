# Что сверить на проде (ВГО-отчёт: выбор источника + договоры)

> Зачем: на dev-снэпшоте `10.10.6.15` часть фактов нельзя установить надёжно —
> `Table_Fin_PL` пуста, `Premaster1C` отдаёт противоречивые counts (101 vs 73017
> по одному срезу — признак перезаливки), КЗ/УЗ ICO-проводки не нашлись. Ниже —
> что подтвердить на ПРОДЕ под VPN до фиксации архитектуры. Утилита: `cmd/mssql-probe`
> (`PROBE_SQL=...`), `WITH (NOLOCK)`.
>
> Контекст и находки: `SPEC.md`, `docs/reports/debt/{finpl-merge,probe-contracts}.md`,
> memory `debt-table-fin-pl-source`. Цепочка: Premaster1C → GLMF_exec → GLMF (+класс.)
> → vGLMFAddUSD (+USD) → Update_Table_Fin_PL (+ручные правки, P&L, без ID).

## A. Какая таблица ПОЛНАЯ и стабильная (Premaster1C vs GLMF) — развилка ETL
GLMF строится из Premaster (`GLMF_exec`: TRUNCATE+INSERT). На проде они должны
сходиться по объёму; на снэпшоте Premaster выглядел недогруженным.

```sql
-- counts по одному ЮЛ/месяцу: Premaster vs GLMF (должны совпасть ±)
SELECT 'premaster' t, COUNT(*) c FROM FinDWH.dbo.Premaster1C WITH(NOLOCK)
  WHERE LTRIM(RTRIM(CompanyID))='690591512' AND [Date]>='2025-01-01' AND [Date]<'2025-02-01'
UNION ALL
SELECT 'glmf', COUNT(*) FROM FinDWH.dbo.GLMF WITH(NOLOCK)
  WHERE CompanyID='690591512' AND [Month]='2025-01-01';
-- свежесть обеих
SELECT 'premaster' t, MAX(DateOfLoad) FROM FinDWH.dbo.Premaster1C WITH(NOLOCK)
UNION ALL SELECT 'glmf', MAX(DateOfLoad) FROM FinDWH.dbo.GLMF WITH(NOLOCK);
```
**Решение:** если Premaster полный и свежий → ETL из Premaster (есть договор+класс.).
Если Premaster отстаёт/нестабилен, а GLMF свежий → ETL из GLMF + договор подмешать
join'ом Premaster-субконто по `DocID`/`Num`.

## B. Каноничная выручка сходится с офиц. ОПУ (дельта = ручные Блоки)
Классификация выручки = `[002 CodePL].GroupPL='ПРОДАЖИ'` по `CodePL` (Premaster и GLMF
оба несут `CodePL`). Сверить с официальным PL-файлом / Table_Fin_PL.

```sql
-- выручка ВГО по контрагенту (GLMF, BYN). Старт-числа (снэпшот, MF 2025-01):
--   Формэль ≈ 14.1М, MFKaz ≈ 1.2М, MFUz ≈ 0.2М (в BYN, знак Cr → abs)
SELECT CounterpartyID, MAX(CounterpartyName) nm, SUM(AmountWOVATBelRubFact) rev
FROM FinDWH.dbo.GLMF WITH(NOLOCK)
WHERE CompanyID='690591512' AND [Month]='2025-01-01' AND ICO=1 AND GroupPL=N'ПРОДАЖИ'
GROUP BY CounterpartyID ORDER BY ABS(SUM(AmountWOVATBelRubFact)) DESC;
```
**Критерий:** дельта с Table_Fin_PL/офиц. PL по выручке < 1–2% → берём CodePL-классификацию
(ручные Блоки на продажи почти не влияют — они про ФОТ/налоги). Иначе — разобрать,
какие Блоки трогают продажи, и решить, тянуть ли Table_Fin_PL для финальных сумм.

## C. Договоры: позиция субконто и покрытие (по счетам)
`DimSubkonto` НЕ помогает (это статьи расходов). Карта субконто (probe-contracts.md):
62→`DrSubconto2`, 60/76→`CrSubconto1`. КЗ/УЗ — не установлено. Резолв через `Objects.ID`.

```sql
-- 62 (ДЗ): ожидаем ~100% договоров
SELECT TOP 30 o.[Name] FROM FinDWH.dbo.Premaster1C p WITH(NOLOCK)
JOIN FinDWH.dbo.Objects o WITH(NOLOCK) ON o.ID=p.DrSubconto2
WHERE p.CompanyID='6950135110' AND p.ICO=1 AND p.DrAcc LIKE '62%'
  AND p.[Date]>='2025-01-01' AND p.[Date]<'2025-02-01';
-- 60/76 (КЗ): CrSubconto1 + нужна эвристика-фильтр (мусор: 00БС/ТДБП/Оказание/Реализаци)
SELECT TOP 30 o.[Name] FROM FinDWH.dbo.Premaster1C p WITH(NOLOCK)
JOIN FinDWH.dbo.Objects o WITH(NOLOCK) ON o.ID=p.CrSubconto1
WHERE p.CompanyID='9731039708' AND p.ICO=1 AND p.CrAcc LIKE '60%'
  AND p.[Date]>='2025-01-01' AND p.[Date]<'2025-02-01';
-- КЗ/УЗ (1210/3310/6000): какое субконто = договор? резолвить Dr/CrSubconto1..4 и смотреть
SELECT TOP 30 p.DrAcc,p.CrAcc, o1.[Name] s1,o2.[Name] s2,o3.[Name] s3,o4.[Name] s4
FROM FinDWH.dbo.Premaster1C p WITH(NOLOCK)
LEFT JOIN FinDWH.dbo.Objects o1 ON o1.ID=p.DrSubconto1
LEFT JOIN FinDWH.dbo.Objects o2 ON o2.ID=p.DrSubconto2
LEFT JOIN FinDWH.dbo.Objects o3 ON o3.ID=p.DrSubconto3
LEFT JOIN FinDWH.dbo.Objects o4 ON o4.ID=p.DrSubconto4
WHERE p.CompanyID='141240004842' AND p.DrAcc LIKE '1210%'
  AND p.[Date]>='2025-01-01' AND p.[Date]<'2025-02-01';
```
**Сверить:** доля строк с резолвленным договором по 62 / 60 / 76 / КЗ-УЗ; где договор
для КЗ/УЗ (позиция субконто); работает ли эвристика-фильтр (% чистых имён).

## D. ВГО-флаг и матчинг по ИНН
На снэпшоте КЗ 1210 с `ICO=1` дал 0 строк — проверить, помечены ли КЗ/УЗ ВГО-проводки.

```sql
-- помечены ли внутригрупповые КЗ/УЗ как ICO=1?
SELECT p.ICO, COUNT(*) c FROM FinDWH.dbo.Premaster1C p WITH(NOLOCK)
WHERE p.CompanyID='141240004842'
  AND LTRIM(RTRIM(p.CounterpartyID)) IN ('690591512','6950135110','305554644','690719790')
  AND p.[Date]>='2025-01-01' AND p.[Date]<'2025-02-01'
GROUP BY p.ICO;
```
**Сверить:** покрывает ли `ICO=1` все внутригрупповые пары (иначе union-страховка по
списку наших ИНН обязательна); чистота `CounterpartyID`/`CompanyID` (padding → нужен TRIM).

## E. ДЗ/КЗ-сальдо
Signed-сальдо (Σ Dr − Σ Cr) по корням 62/60/76/1210/3310 на дату — сверить с
оборотно-сальдовой 1С по 1–2 ЮЛ за месяц (входящее/оборот/исходящее).

## F. Срок/просрочка (Payments.Docs)
Мост по договору-GUID (`idrrefSQLToGUID(subconto)` = `Docs.ID`) сейчас закрывает РБ/РФ.
Проверить, есть ли срок/`Delay` для КЗ/УЗ договоров в `Payments.dbo.Docs`.

## Итог-развилки для решения
1. **Источник ETL:** Premaster (если полный) vs GLMF (если Premaster нестабилен) — пункт A.
2. **Финальные суммы выручки:** CodePL-классификация (быстро, без правок) vs Table_Fin_PL
   (с ручными Блоками) — пункт B.
3. **Договоры КЗ/УЗ:** позиция субконто — пункт C (эмпирически, DimSubkonto не помог).
