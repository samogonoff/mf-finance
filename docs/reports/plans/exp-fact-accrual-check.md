# Факт по начислениям для формы расходов: сверка с платёжным фактом

Откуда идея: контур «Бюджетирование» (`finance-projects/budget`) копирует факт прямо из `FinDWH.dbo.Table_Fin_PL`
(`budget.usp_RefreshFact`: сценарий «факт», «Для отчета» ∈ PL / BS_20 / BS_25; ЦФО `35BK`, `250BK` и пустые коды
отбрасываются; статьи продаж хранятся с обратным знаком). Эта копия сверена его авторами с представлением
`Budgeting.dbo.FactAndBudgetPlusCompany` (144 672 ячейки за 2022–2026 совпали) — то есть это **факт по начислениям**.

Процедура затрат `ZATRATY_DLYA_OKSANY_BYSTRYE` «Факта по начислениям» не отдаёт — только «Факт по платежам», и форма
расходов подставляет его вместо факта (баннер «Факт построен по платежам»). У нас та же `Table_Fin_PL` уже реплицируется
в ClickHouse (`finance.findwh_table_fin_pl`, воркер `FINDWH_SYNC_INTERVAL`, раз в день).

## Что сделано

`PLANS_EXP_FACT_FROM_FINPL=1` (по умолчанию 0): при чтении затрат из ClickHouse «Факт по начислениям» собирается запросом
к `finance.findwh_table_fin_pl` (`go/internal/plans/exp_ch_source.go`, `expAccrualFactSQL`) вместо платёжного факта.

- Отбор: сценарий «факт», «Для отчета» ∈ PL / BS_20 / BS_25, числовой код ЦФО, группа ЦФО вида формы (с номером и без
  него), страна, годы, месяцы с 2024 года, без служебных кодов PL 1 / 50 / 93 и без строк продаж / себестоимости / маржи /
  отгрузок. Опт — суммы в BYN, остальные виды — в валюте страны.
- Каталог статей (группа, название, спецдоступ) берётся из снимка затрат `finance.exp_costs` по коду PL; статья вне
  каталога в форму не попадает (P&L не знает «спецдоступа», и без каталога закрытая статья могла бы показаться всем).
- Проекта в P&L нет — факт суммируется по всем проектам. Плановые сценарии процедуры отбираются по проекту «Нет проектов!»,
  поэтому факт может быть **больше** того, что дал бы тот же срез процедуры (суммы по проектам) — это нужно проверить.
- Формы отделов (ЦФО-формы со своими сценариями) не затронуты. Реплика пуста / недоступна / факта за срез нет — работает
  платёжный факт, как раньше. Если начисления есть только за часть месяцев (в реплике они появляются после закрытия месяца),
  месяцы без начислений **дополняются платёжным фактом**: ячейки помечены источником «платежи», колонки «прошлый / текущий
  месяц» не пустеют, на экране подпись «факт — по платежам, где нет начислений». Месяц считается «с начислениями», если за
  него есть хотя бы одна ячейка начислений в срезе (страна × вид формы × год); месяц с частичными начислениями платёжным
  фактом не дополняется.

## Сверка перед включением на проде

Обе таблицы лежат в одном ClickHouse, поэтому сверка — один запрос. Розница РБ, помесячно: платёжный факт процедуры против
начислений P&L (расходные статьи, без продаж; суммы в валюте страны):

```sql
SELECT p.m AS month, round(p.pay) AS pay_proc, round(a.acc) AS accrual_finpl, round(a.acc - p.pay) AS diff
FROM (
    SELECT toStartOfMonth(month) AS m, sum(amount) AS pay
    FROM finance.exp_costs
    WHERE scenario = 'Факт по платежам' AND country = 'BY' AND group_cfo1 = '3.Магазины'
      AND (project_name = '' OR project_name = 'Нет проектов!')
      AND code_pl NOT IN (1, 50, 93)
      AND positionCaseInsensitiveUTF8(group_pl, 'ПРОДАЖИ') = 0 AND positionCaseInsensitiveUTF8(group_pl, 'СЕБЕСТОИМОСТЬ') = 0
      AND positionCaseInsensitiveUTF8(group_pl, 'МАРЖА') = 0 AND positionCaseInsensitiveUTF8(group_pl, 'ОТГРУЗК') = 0
      AND month >= '2025-01-01'
    GROUP BY m
) AS p
LEFT JOIN (
    SELECT toStartOfMonth(f.month) AS m, sum(f.amount) AS acc
    FROM finance.findwh_table_fin_pl AS f
    INNER JOIN (SELECT code_pl, any(group_pl) AS gp FROM finance.exp_costs GROUP BY code_pl) AS d
            ON d.code_pl = toInt32OrZero(f.code_pl)
    WHERE f.scenario = 'факт' AND f.for_report IN ('PL', 'BS_20', 'BS_25') AND f.country = 'BY'
      AND replaceRegexpOne(f.group_cfo1, '^[0-9]+\\.\\s*', '') = 'Магазины' AND toInt32OrZero(f.code_cfo) > 0
      AND toInt32OrZero(f.code_pl) NOT IN (1, 50, 93)
      AND positionCaseInsensitiveUTF8(d.gp, 'ПРОДАЖИ') = 0 AND positionCaseInsensitiveUTF8(d.gp, 'СЕБЕСТОИМОСТЬ') = 0
      AND positionCaseInsensitiveUTF8(d.gp, 'МАРЖА') = 0 AND positionCaseInsensitiveUTF8(d.gp, 'ОТГРУЗК') = 0
      AND f.month >= '2025-01-01'
    GROUP BY m
) AS a USING (m)
ORDER BY m;
```

Что смотреть:

1. Начисления и платежи не обязаны совпадать, но должны быть одного порядка; провал в 2–3 раза в нескольких месяцах —
   повод остановиться и спросить аналитика.
2. Помесячные начисления за закрытые месяцы 2025 сверить с колонкой «Факт» Excel «PL 2025-2026» по нескольким статьям
   (ЗП, аренда, реклама) для РБ — это эталон формы.
3. Тот же запрос для опта (`group_cfo3 IN ('ОПТ РБ','ОПТ РФ','ОПТ УЗ')`, колонка `amount_byn`) и ИМ.
4. Факт 2026 после марта для опта: платёжный факт его не содержал (вопрос аналитикам); в P&L он должен быть.

После сверки: `PLANS_EXP_FACT_FROM_FINPL=1`, перезапуск go-api; в форме расходов баннер «факт по платежам» пропадает, а кнопка
«Обновить из источника» перечитывает и снимок затрат, и начисления.

## Локальная проверка запроса

В локальном ClickHouse реплика P&L пустая (заливка требует доступа к FinDWH: `MODE=findwh`, см. `docs/bi/findwh-etl.md`).
Запрос проверен на синтетических строках: оба написания группы («Магазины», «3.Магазины») суммируются, сценарий «бюджет»,
«Для отчета» вне PL / BS_20 / BS_25 и ЦФО `35BK` отбрасываются.

## Откуда какие расходы (ответ аналитика от 22.10.2026)

> «Расходы для розницы, ИМ и опта — таблица Table_Fin_PL в базе FinDWH; для маркетплейсов затраты по сценарию факт идут из
> источника ZATRATY_DLYA_OKSANY_BYSTRYE (Budgeting)».

| Вид формы | Факт | Стратегия / тактика (бюджет) |
|---|---|---|
| Розница, интернет-магазин, опт | `Table_Fin_PL` (реплика `finance.findwh_table_fin_pl`) — флаг `PLANS_EXP_FACT_FROM_FINPL` | `Table_Fin_PL` — флаг `PLANS_EXP_PLAN_FROM_FINPL` |
| Маркетплейсы | процедура затрат (снимок `finance.exp_costs`) | процедура затрат |

Оба флага по умолчанию выключены (расходы идут из процедуры, как раньше) и действуют только для розницы, ИМ и опта;
маркетплейсы всегда читаются из процедуры. Названия сценариев в `Table_Fin_PL` задаются `PLANS_EXP_FINPL_STRATEGY` /
`PLANS_EXP_FINPL_TACTIC` (списки через запятую, регистр не важен; значения по умолчанию и диагностика — в разделе «Какие
сценарии реально есть в Table_Fin_PL» ниже) — если у аналитиков они называются иначе, меняется переменная, а не код.
Сценарий заменяется только если в реплике за срез есть данные.

## Сверка плана (перед включением `PLANS_EXP_PLAN_FROM_FINPL`)

Тактика процедуры («Тактика бюджет (таргеты)») против сценария «тактика» P&L, помесячно, розница РБ (расходные статьи, без
продаж; суммы в валюте страны). Для стратегии замените сценарии на «Стратегический бюджет» и «бюджет».

```sql
SELECT p.m AS month, round(p.proc) AS procedure_tactic, round(a.fin) AS finpl_tactic, round(a.fin - p.proc) AS diff
FROM (
    SELECT toStartOfMonth(month) AS m, sum(amount) AS proc
    FROM finance.exp_costs
    WHERE scenario = 'Тактика бюджет (таргеты)' AND country = 'BY' AND group_cfo1 = '3.Магазины'
      AND (project_name = '' OR project_name = 'Нет проектов!')
      AND code_pl NOT IN (1, 50, 93)
      AND positionCaseInsensitiveUTF8(group_pl, 'ПРОДАЖИ') = 0 AND positionCaseInsensitiveUTF8(group_pl, 'СЕБЕСТОИМОСТЬ') = 0
      AND positionCaseInsensitiveUTF8(group_pl, 'МАРЖА') = 0 AND positionCaseInsensitiveUTF8(group_pl, 'ОТГРУЗК') = 0
      AND toYear(month) = 2026
    GROUP BY m
) AS p
LEFT JOIN (
    SELECT toStartOfMonth(f.month) AS m, sum(f.amount) AS fin
    FROM finance.findwh_table_fin_pl AS f
    INNER JOIN (SELECT code_pl, any(group_pl) AS gp FROM finance.exp_costs GROUP BY code_pl) AS d
            ON d.code_pl = toInt32OrZero(f.code_pl)
    WHERE lowerUTF8(f.scenario) = lowerUTF8('тактика') AND f.for_report IN ('PL', 'BS_20', 'BS_25') AND f.country = 'BY'
      AND replaceRegexpOne(f.group_cfo1, '^[0-9]+\\.\\s*', '') = 'Магазины' AND toInt32OrZero(f.code_cfo) > 0
      AND toInt32OrZero(f.code_pl) NOT IN (1, 50, 93)
      AND positionCaseInsensitiveUTF8(d.gp, 'ПРОДАЖИ') = 0 AND positionCaseInsensitiveUTF8(d.gp, 'СЕБЕСТОИМОСТЬ') = 0
      AND positionCaseInsensitiveUTF8(d.gp, 'МАРЖА') = 0 AND positionCaseInsensitiveUTF8(d.gp, 'ОТГРУЗК') = 0
      AND toYear(f.month) = 2026
    GROUP BY m
) AS a USING (m)
ORDER BY m;
```

Что смотреть: суммы по месяцам должны совпадать или расходиться объяснимо (в P&L нет проекта — суммируются все проекты;
фильтр «Для отчета» взят от факта и для планового сценария может быть иным). Любое заметное расхождение — вопрос аналитикам:
как в `Table_Fin_PL` называются сценарии и какие значения «Для отчета» у бюджета и тактики.

## Какие сценарии реально есть в Table_Fin_PL

Названия сценариев бюджета и тактики в `Table_Fin_PL` не подтверждены: в таблице сценарий — текстовая колонка (известны «факт» и
«НовСтрат»), а в процедуре затрат сценарии называются по справочнику `Budgeting.dbo.Scenario` («Стратегический бюджет»,
«Тактика бюджет (таргеты)»). Поэтому по умолчанию принимается любое из двух написаний — «бюджет, Стратегический бюджет» и
«тактика, Тактика бюджет (таргеты)» (список через запятую в `PLANS_EXP_FINPL_STRATEGY` / `PLANS_EXP_FINPL_TACTIC`, регистр не важен).

Что лежит на самом деле, показывает диагностика для администратора процессов: `GET /api/plans/exp-source/scenarios` — сценарии
реплики P&L (с «Для отчета», числом строк и месяцами), сценарии снимка процедуры и список настроенных названий, которых в реплике
нет (`missing`). То же запросом в ClickHouse:

```sql
SELECT scenario, for_report, count() AS rows, min(month) AS from_month, max(month) AS to_month
  FROM finance.findwh_table_fin_pl GROUP BY scenario, for_report ORDER BY scenario, for_report;
```

Если сценарии лежат иначе (например, коды в отдельной таблице), достаточно задать их названия в переменных; если в реплике бюджет и
тактика вообще отсутствуют, флаг `PLANS_EXP_PLAN_FROM_FINPL` ничего не заменит (сценарий заменяется, только если в реплике за срез
есть данные), и вопрос уходит аналитикам.
