# SPEC — Модуль «Тактические планы» (Profit & Loss), MVP-вертикаль «Маркетплейсы»

> Статус: **MVP реализован (VS0–VS7, backend+frontend, тесты зелёные).**
> Осталось: накат миграций 0010–0013 и прогон на поднятом стеке (CHECKPOINT A —
> live FinDWH). Прогресс и детали — `docs/reports/plans/todo.md`, `plan.md`.
> Дата: 2026-06-25. Ветка: `add-plan`.
> Источник истины по требованиям: `var/plan/Техническое задание … веб интерфейсе (2).docx`
> (далее «ТЗ»), `var/plan/TPL-MP_ формы … large/small.docx` (далее «ТЗ-MP»),
> Excel-прототипы `var/plan/Тактический план_large_МП_2026_июнь.xlsx` (лист
> `Маркетплейсы_large`), `…_small_…xlsb` (лист `Маркетплейсы_small`),
> `var/plan/Справочник ЦФО и ЦЗ.xlsx`, `var/plan/РБ ВСЕ ЦФО (1).xlsx`.
> Эта спека **не трогает** корневой `SPEC.md` (он про ВГО-отчёт) и кладётся по
> конвенции репозитория `docs/reports/<module>/`.

> **Решения заказчика (зафиксированы 2026-06-25):**
> 1. Охват — **весь модуль** тактических планов; форма «Маркетплейсы» (TPL-MP) —
>    детально проработанный **первый вертикальный срез MVP**.
> 2. Архитектура — **основной контур** репозитория: Go-API
>    (`github.com/company/finance-api`) + Nuxt 3 + Postgres `finance`; новые
>    миграции в `migrations/`; раздел встраивается в левое меню «Финансы и
>    экономика». Изолированный контур (как «Себестоимость») **не** используем.
> 3. Источник факта/стратегии — **онлайн-интеграция OLAP/SQL сразу** (OLAP
>    Budgeting, SQL Payments.Debt, Lisa). Mock-фикстуры остаются как
>    документированный fallback (`PLANS_MOCK=1`), по умолчанию выключен.

---

## Содержание

1. Objective (цель и пользователи)
2. Decisions (реестр решений)
3. Глоссарий
4. Бизнес-процесс, календарь, потоки
5. Роли, участники, ABAC
6. Архитектура размещения в репозитории
7. Модель данных (БД `finance`, миграции, DDL)
8. Справочники (`dir_*`): источники, поля, синхронизация
9. Шаблоны форм: общая модель + **TPL-MP в деталях**
10. Интеграции и источники данных (OLAP / SQL / Lisa)
11. Движок расчётов (формулы)
12. Workflow согласования (этапы 1.1–4, переходы, зависимости)
13. API-контракты
14. UI: экраны, навигация, дизайн-система
15. Импорт/экспорт Excel
16. Уведомления (Bitrix24)
17. Аудит / история изменений
18. Безопасность и НФТ
19. Commands (команды разработки)
20. Project structure (структура файлов)
21. Code style (стиль кода)
22. Testing strategy (стратегия проверки)
23. Boundaries (always / ask-first / never)
24. План реализации (задачи T1…Tn, MVP → этап 2)
25. Checkpoints (сверка чисел)
26. Приложения (площадки, реестр CodePL, схемы источников)
27. References

---

## 1. Objective (цель и пользователи)

### 1.1. Назначение

Отдельный веб-раздел внутри Finance Cabinet для **подготовки, согласования и
ведения тактических планов прибылей и убытков (P&L / PL)** с участием нескольких
подразделений. Заменяет ручную работу в тяжёлых Excel-файлах (своды + запросы в
БД), сохраняя их структуру и формулы, но добавляя версионность, маршрут
согласования, ABAC, комментарии и аудит.

Система объединяет: справочные данные (Лиса/1С/OLAP); ввод и корректировку по
шаблонам форм; расчёт статей по алгоритмам; маршрут согласования по цепочке;
комментарии и аудит.

### 1.2. Цели (из ТЗ §«Цели»)

- Сократить время подготовки тактического PL за счёт автоматического расчёта
  ключевых статей.
- Единый источник правды по версиям плана и корректировкам на каждом этапе.
- Прозрачно фиксировать согласования, замечания и ответственность.
- Разграничить доступ по ролям и объектам (подразделение, период, этап).

### 1.3. Пользователи

| Категория | Кто | Что делает |
|---|---|---|
| Финансисты (внутренние) | ФЭО, координатор бюджета (Антипова О.В.), согласующие | Ввод, своды по ЮЛ/каналам, согласование, корректировки |
| Владельцы ЦП/ЦЗ | Ответственные за разрез (см. §5) | Ввод бюджета своего разреза на этапе заполнения |
| **Внешние сотрудники** | Менеджеры площадок МП (Мурашко — large; Левин/Качановская — small) и т.п. | Видят **только свой ABAC-срез** (по наименованию подразделения, не по коду), вводят тактику, оставляют комментарии |
| Администраторы | Админ НСИ, админ процессов | Справочники, маршруты, шаблоны, SLA |
| Аудитор | Опционально | Просмотр истории |

Ключевое требование UX (ТЗ ACL-06): пользователь видит **`name_cfo`
(наименование подразделения/площадки)**, не `code_cfo`; чужие подразделения не
видит. «Максимально продуманный интерфейс и стилистика текущего сайта» —
обязателен дизайн-систем токены (`nuxt/assets/styles/design-system.css`), без
хардкода цветов.

### 1.4. MVP-вертикаль (первый релиз)

Форма **TPL-MP (Маркетплейсы), сегмент large** — полностью рабочая: справочники
`dir_cfo`, `dir_pl_line`, `dir_marketplace`, `dir_scenario`, `dir_fx_rate`; ввод
тактики; подтягивание факта/стратегии онлайн; комментарии; ABAC; импорт/экспорт
Excel. Сегмент **small** — та же форма, другой ABAC-фильтр (группа 480). Движок
расчётов CALC и полный workflow 1.1–4 — этап 2 (но в спеке описаны полностью).

---

## 2. Decisions (реестр решений)

| # | Вопрос | Решение | Статус | Основание |
|---|---|---|---|---|
| D1 | Охват спеки | Весь модуль; TPL-MP — детальный MVP-срез | ✅ | заказчик 25.06 |
| D2 | Размещение | Основной контур Go+Nuxt+`finance` | ✅ | заказчик 25.06 |
| D3 | Источник факта в MVP | Онлайн OLAP/SQL; `PLANS_MOCK` fallback off | ✅ | заказчик 25.06 |
| D4 | Хранение значений | Editable-ячейки → `pl_metric`; полный снимок формы → `form_submission.json_payload` | ✅ | ТЗ §7.2.1, ТЗ-MP §5 |
| D5 | Новые роли | `ROLE_PLANS_ADMIN ⊇ ROLE_PLANS_USER`; `ROLE_ADMIN ⊇ ROLE_PLANS_ADMIN`; ABAC-срез сверху | ✅ | конвенция `auth/roles.go` |
| D6 | Валюта | Хранить суммы в 3 валютах (BYN/RUB/USD), как `Источник_МП`; переключатель в шапке; пересчёт через `dir_fx_rate` | ✅ | прототип large, кол. H/I/J |
| D7 | Период формы | Отчётный месяц M + горизонт (M−1, M, M+1) + конструктор на 12 мес 2026 | ✅ | прототип large, кол. F/G/AR..BD |
| D8 | Сегмент small (xlsb) | Не парсим бинарь; small = тот же шаблон TPL-MP, площадки и группа 480 из ТЗ-MP | ✅ | ТЗ-MP §1, §2.2 |
| D9 | Расчёты в MVP | Числовой ввод + read-only факт; формулы каскада (наценка, маржа, доли) — этап 2 (CALC) | 🔶 | ТЗ §7.7, MVP без CALC |
| D10 | Сводный шаблон 1.6/2.4 | Описан, но реализация — этап 2 | 🔶 | ТЗ TPL-08 |
| D11 | Редактирование формул каскада | **Гибрид:** дефолт в `calc_rule` (версионируемые, правит админ процессов) + per-срез override формулы финансистом (с причиной), приоритет override > calc_rule; нужен безопасный вычислитель выражений | ✅ | заказчик 25.06 |

Легенда: ✅ принято · 🔶 принято с фазированием · ❓ открыто.

**Открытые вопросы:**
- 🔶 Q1 (креды) — **частично:** креды/сервер взяты как у ВГО (`MSSQL_PREMASTER_*`).
  **Смоук на стенде 25.06:** подключение к MSSQL живое, но объекта **`ALL_view_МП`
  в подключённой БД (Premaster-снэпшот) НЕТ** (`mssql: Недопустимое имя объекта`).
  Витрина МП (`Источник_МП`/`ALL_view_МП`) — иной OLAP-источник (куб Продажи +
  Payments), вероятно другой сервер/БД. **Нужно от аналитика/ИТ: где живёт витрина
  МП и её точное имя.** До уточнения — `PLANS_MOCK=1` (форма уже деградирует до
  пустого факта, ввод тактики не блокируется). Это и есть незакрытый **CHECKPOINT A**.
- ✅ Q2 (auth) — **закрыто 25.06:** авторизация = **B24 OAuth** (как у внутренних).
  Под-вопрос Q2-b: заведены ли внешние менеджеры площадок в B24; если нет —
  локальные учётки (не блокирует архитектуру ролей, см. §5.1).
- ✅ Q3 (календарь) — **закрыто 25.06:** **производственные календари по странам**
  (РФ/РБ/КЗ/УЗ) — разные. Этап привязывается к стране **по ответственному**: дата
  р.д. этапа считается по календарю страны его исполнителя. Ведём вручную в
  глобальных настройках проекта: на год по каждой стране — праздники, рабочие
  выходные, сокращённые дни. Переиспользуемо (табель и пр.). См. §4.1, §7.3
  (`plans_country_calendar`).
- 🔶 Q4 — модель редактирования формул **закрыта (D11, гибрид)**; остаётся ❓Q4b —
  снять **точные выражения** каскада с автора прототипа large (наценка %, маржа
  розничная, доля прямых затрат): формулы в Excel скрыты, есть только значения. §11.

---

## 3. Глоссарий (из ТЗ §«Термины»)

| Термин | Значение |
|---|---|
| PL / P&L | Отчёт о прибылях и убытках (тактический план на период) |
| Лиса | Корпоративная система — источник операционных/производственных данных |
| 1С | ERP — финансовые, кадровые, договорные и часть справочных данных |
| OLAP Budgeting | Куб бюджетирования; срезы `Budget`, измерение `Scenario` |
| Экземпляр процесса (`pl_instance`) | Конкретный тактический план на период (месяц) в маршруте |
| Версия показателей | Снимок значений статей PL на момент сохранения/перехода этапа |
| Корректировка | Ручное изменение рассчитанного/импортированного значения с **обязательным основанием** |
| ЦП | Центр прибыли (розница, **МП**, опт, IM) |
| ЦЗ | Центр затрат (IT, HR, логистика, маркетинг…) |
| ЮЛ | Юридическое лицо (Mark Formelle, Formelle, MF Kazakhstan, MF IT, MF Tex, TD Mark Formelle, MF Shanghai…) |
| Канал | Retail, **Marketplaces**, Wholesale, IM |
| р.д. | Рабочий день отчётного месяца (2–6 р.д. по схеме) |
| СПП | Скидка постоянного покупателя (скидка площадки) |
| ABAC | Атрибутивное ограничение доступа (по ЦФО/магазину/этапу/периоду/стране/ЮЛ) |
| `code_cfo` / `name_cfo` | Код / наименование ЦФО (подразделения/площадки) |
| `code_pl` | Код статьи PL (строка/блок метрики) |
| Сегмент (large/small) | Нарезка одного шаблона TPL-MP: large=группа 250, small=группа 480 |

---

## 4. Бизнес-процесс, календарь, потоки

Тактический PL формируется **двумя параллельными потоками**, сходящимися в
финальном согласовании по ЮЛ и каналам.

### 4.1. Календарь (отчётный месяц M)

| Срок | Этапы | Содержание |
|---|---|---|
| 25-е число M−1 | 2.1, 2.2 | Производство: ввод минут/объёмов; согласование планов пр-ва |
| 2-й р.д. M | 1.1 | Заполнение бюджетов продаж всех ЦП (**здесь живёт TPL-MP**) |
| 3-й р.д. | 1.2, 1.3, 2.3 | Согласование руководителями ЦП; директор направления; обновление ЦЗ |
| 4-й р.д. | 1.4, 2.4 | Финал потока продаж; формирование бюджетов по ЮЛ (пр-во/ЦЗ) |
| 5-й р.д. | 1.5, 1.6 | Обновление расходов ЦП; своды по ЮЛ и каналам (продажи) |
| 6-й р.д. | 3, 4 | Согласование бюджетов ЮЛ; итоговое утверждение по ЮЛ и каналам |

Система должна: вычислять даты 2–6 р.д. по производственному календарю;
показывать на дашборде просрочки по этапу/разрезу; блокировать переход при
нарушении SLA (настраиваемо: предупреждение / жёсткий блок).

**Календари — по странам (Q3 ✅).** У РФ/РБ/КЗ/УЗ производственные календари
разные, поэтому дата N-го р.д. для этапа считается по календарю **страны
ответственного** этого этапа (страна берётся из его `plans_user_scope.country`).
Если на этапе несколько исполнителей из разных стран — дедлайн считается по
календарю каждого (своя дата на свою задачу). Календари ведутся вручную в
**глобальных настройках проекта**: на год по каждой стране — праздники, рабочие
выходные (перенесённые рабочие дни), сокращённые дни. Данные переиспользуемы
(табель, прочие расчёты от р.д.). Таблица — `plans_country_calendar` (§7.3).

### 4.2. Поток 1 — бюджеты продаж ЦП

| Этап | Наименование | Ответственные (эталон) | Действия |
|---|---|---|---|
| 1.1 | Заполнение бюджетов продаж ЦП | Смолер, Ткачева, Галькевич, Бединская, **Мурашко (МП large)**, **Левин/Качановская (МП small)**, Левин С., Пистоленко, Сипаров Ю.Г. | Ввод плана продаж; авто-ФОТ розницы от продаж, аренда из 1С; корректировки |
| 1.2 | Согласование руководителем подразделения | Розница РБ/КЗ/УЗ — Смолер; Крупные МП — Ворончук; Мелкие МП — Левин | Просмотр, комментарии, согласовать/вернуть на 1.1 |
| 1.3 | Согласование директором направления | Дегтерева Е.В. | Согласование сводного потока продаж |
| 1.4 | Финальное согласование потока продаж | Сипаров С.Г. | Закрытие редактирования 1.1–1.3 для цикла |
| 1.5 | Обновление расходов в бюджетах ЦП | Антипова (зам. Осипович); Маркетинг (Ворончук), IT (Сериков), HR (Счастная), Логистика (Сипаров В.Ю.); по странам | Косвенные расходы ЦП; логистика от объёмов (Лиса) |
| 1.6 | Формирование бюджетов по ЮЛ и каналам | Антипова О.В. | Свод ЮЛ × канал. **Зависимость: после 2.4** |

### 4.3. Поток 2 — производство и ЦЗ

| Этап | Наименование | Ответственные | Действия |
|---|---|---|---|
| 2.1 | Минуты и объёмы пр-ва | Планирование пр-ва (все страны) | Импорт из Лисы / ручной ввод; ФОТ пр-ва от минут |
| 2.2 | Согласование планов пр-ва | Захарченко | Согласовать/вернуть на 2.1 |
| 2.3 | Обновление бюджетов ЦЗ | IT (Сериков), HR (Счастная), Логистика (Сипаров В.Ю.); РФ/РБ/УЗ | Расходы ЦЗ по странам; пересчёт логистики |
| 2.4 | Формирование бюджетов по ЮЛ | Антипова (зам. Осипович) | Свод по ЮЛ: Mark Formelle, MF IT, MF Tex. Выход для 1.6 |

### 4.4. Финал (6 р.д.)

- **Этап 3** — согласование бюджетов ЮЛ; параллельные задачи, у каждого ЮЛ свой
  согласующий:

  | ЮЛ | Согласующий |
  |---|---|
  | Mark Formelle | Захарченко Н.М. |
  | Formelle | Дегтерева Е.В. |
  | MF IT | Мавлянов Д.А. |
  | MF Kazakhstan | Командиров М.В. |
  | TD Mark Formelle | Левин |
  | MF Tex | Сметанин П.С. |
  | MF Shanghai | Акаева |

- **Этап 4** — итоговое утверждение по ЮЛ и каналам: Сипарова С.Г., Сериков А.Г.
  (финальная блокировка периода).

### 4.5. Зависимости (ТЗ §5.5)

| Правило | Описание |
|---|---|
| WF-DEP-01 | Этап 1.6 доступен только после `completed` этапа 2.4 (и 1.1–1.5) |
| WF-DEP-02 | Этап 3 — после завершения 1.6 |
| WF-DEP-03 | Потоки 1.1–1.4 и 2.1–2.4 — параллельно в своих сроках |
| WF-DEP-04 | На 1.5/2.3 — параллельная работа по статьям (Маркетинг/IT/HR/Логистика) и странам |

### 4.6. Жизненный цикл экземпляра PL

`draft` → `in_progress` (подстатусы по `stage_id`) → `returned` (возврат на этап)
· `waiting_dependency` (ждёт другой поток, напр. 1.6 ждёт 2.4) → `approved` (этап
4) → `archived` (период закрыт).

Правила переходов: вперёд — заполнены обязательные поля шаблона / действие
«Согласовать» у всех назначенных (или кворум); возврат — целевой этап +
обязательный комментарий + уведомление; при переходе — снимок версии показателей;
корректировка на этапах 3–4 только с правом `edit_after_submission`.

---

## 5. Роли, участники, ABAC

### 5.1. Двухслойная модель ролей (ТЗ §«Участники и роли»)

ТЗ задаёт роли **двумя слоями**, и система обязана воспроизвести оба. ТЗ прямо:
*«роли задаются конфигуратором и привязываются к этапам, ЦП/ЦЗ, стране и ЮЛ. Имена
ответственных из схемы — эталон для первичного наполнения справочника
"Пользователь ↔ роль ↔ объект"»* → **боевая привязка ответственных настраивается
вручную**, имена из схемы (Мурашко, Левин…) — лишь seed.

**Слой 1 — глобальные auth-роли (RBAC, B24 OAuth).** Грубый гейт доступа к
модулю; кладутся в `users.roles` через существующий `PUT
/api/admin/users/{id}/roles`. Расширяем `go/internal/auth/roles.go` (D5) **всего
двумя** ролями — не плодим 12 функциональных в `auth.Allowed`:

```
ROLE_ADMIN         ⊇ ROLE_PLANS_ADMIN (+ существующие COST/FINANCE)
ROLE_PLANS_ADMIN   ⊇ ROLE_PLANS_USER        // конфигуратор: НСИ, маршруты, шаблоны, SLA, назначение ответственных
ROLE_PLANS_USER    — участник процесса (ввод/согласование своего среза)
ROLE_USER          — всегда добавляется
```

**Слой 2 — функциональная роль × объект (ABAC, данные).** 12 системных ролей ТЗ
живут **не как auth-константы, а как значения `role` в таблице
`plans_user_scope`** (= справочник ТЗ §7.1.7/§7.1.12 «Пользователь ↔ роль ↔
объект», поля `user_id, role, stage_code, country, legal_entity, code_cfo[]`).
Причина: роли по сути **пообъектные** (один человек — разная роль на разных
ЦФО/ЮЛ/этапах; напр. Захарченко = согласующий производства И согласующий ЮЛ Mark
Formelle), что auth-роль выразить не может. Настраивает **«Администратор
процессов»** через конфигуратор; первичное наполнение — из схемы.

Перечень функциональных ролей (значения `plans_user_scope.role`):

| Функциональная роль | Этапы | Привязка к объекту |
|---|---|---|
| `filler` Ответственный за заполнение ЦП/ЦЗ | 1.1, 1.5, 2.1, 2.3 | `code_cfo[]` / статья / страна |
| `dept_head` Руководитель подразделения | 1.2 | ЦП |
| `direction_head` Директор направления | 1.3 | направление продаж |
| `sales_final` Финальный согласующий потока продаж | 1.4 | весь поток |
| `budget_coordinator` Координатор бюджета | 1.5, 1.6, 2.4 | своды ЮЛ/каналы |
| `prod_planning` Производственное планирование | 2.1 | производство/страна |
| `prod_approver` Согласующий производства | 2.2 | производство |
| `le_approver` Согласующий бюджета ЮЛ | 3 | `legal_entity` |
| `final_approver` Итоговый утверждающий | 4 | период |
| `nsi_admin` Администратор НСИ | — | справочники |
| `process_admin` Администратор процессов | — | маршруты/SLA/назначения |
| `auditor` Аудитор | — | история (опц.) |

Поверх — прикладные разрешения (ТЗ ACL-04): `view_pl`, `edit_metrics`,
`edit_template`, `approve`, `manage_directories`, `view_audit`, `admin`,
`edit_after_submission` (для 3–4). ABAC-срез из `plans_user_scope`: этап × ЦП/ЦЗ ×
страна × ЮЛ × набор `code_cfo`.

**Внешние сотрудники** (менеджеры площадок): auth — `ROLE_PLANS_USER` через B24
OAuth (Q2 ✅); доступ — узкий ABAC-срез (`filler`, свои `code_cfo`, сегмент). Если
внешних нет в B24 — нужны локальные учётки (под-вопрос §2 Q2-b, не блокирует
архитектуру ролей).

**MVP:** вручную сидим только заполнителей МП (`filler`: Мурашко→large/250,
Левин/Качановская→small/480) через `PUT /api/plans/scope/{user_id}`. Полный
конфигуратор всех 12 ролей и слой согласования — этап 2.

### 5.2. Матрица доступа по этапам (ТЗ §«Матрица»)

| Этап | Заполнение | Согласование | Корректировка |
|---|---|---|---|
| 1.1 | Владелец ЦП | — | Владелец ЦП |
| 1.2 | — | Руководитель ЦП | — |
| 1.3 | — | Дегтерева Е.В. | — |
| 1.4 | — | Сипаров С.Г. | — |
| 1.5 | Ответственные по статье/стране | — | Антипова + владельцы статей |
| 1.6 | Антипова О.В. | — | Антипова (после 2.4) |
| 2.1 | Планирование пр-ва | — | Планирование пр-ва |
| 2.2 | — | Захарченко | — |
| 2.3 | IT/HR/Логистика по странам | — | По своему ЦЗ |
| 2.4 | Антипова (зам.) | — | Антипова |
| 3 | — | Согласующий ЮЛ | По `edit_after_submission` |
| 4 | — | Сипарова, Сериков | — |

### 5.3. Ответственные за TPL-MP (этап 1.1, канал Marketplaces)

| Сегмент | code_cfo группы | Ответственный | Площадки |
|---|---|---|---|
| Large | 250 | Мурашко | WB(335), Lamoda(336), Ozon(337), Yandex(954) |
| Small | 480 | Левин / Качановская | Детский мир(953), Золотое яблоко(955), …ТЕКС(957/959/990/991/958), Kaspi(338, KZ), WB/Ozon KZ(475/474), Uzmarket(339, UZ) |

ABAC: пользователь сегмента видит только свои `code_cfo`; в UI — `name_cfo`
(название площадки).

---

## 6. Архитектура размещения в репозитории

Основной контур (D2). Следуем существующим паттернам (см. отчёт `reports/debt`).

```
finance/
├── migrations/                       # +0010_plans_*.up/down.sql … (БД finance)
├── go/
│   ├── cmd/api/main.go               # +регистрация маршрутов /api/plans/*
│   └── internal/
│       ├── auth/roles.go             # +ROLE_PLANS_ADMIN / ROLE_PLANS_USER
│       └── plans/                    # НОВЫЙ пакет модуля
│           ├── handler.go            # HTTP-хендлеры (http.NewServeMux паттерн)
│           ├── service.go            # бизнес-логика, переходы, снимки версий
│           ├── repo.go               # pgx-репозиторий (instances, metrics, submissions)
│           ├── directories.go        # реестр dir_*, синхронизация
│           ├── abac.go               # фильтр среза пользователя
│           ├── importexport.go       # Excel import/export (TPL-06)
│           ├── calc/                 # движок расчётов (этап 2)
│           └── sources/              # online-коннекторы OLAP/SQL/Lisa (+mock)
├── nuxt/
│   ├── app.vue                       # +пункт меню «Тактические планы»
│   ├── pages/plans/                  # /plans, /plans/[id], /plans/mp/...
│   ├── composables/usePlans.ts       # клиент API ($fetch + NUXT_PUBLIC_API_BASE)
│   ├── composables/usePlanForm.ts    # модель формы TPL-MP
│   └── components/plans/             # PlanTable, MpForm, ApprovalPanel, …
└── docs/reports/plans/SPEC.md        # этот файл
```

Принципы из CLAUDE.md, которые НЕЛЬЗЯ нарушать:
- Любая новая env-переменная (Go `os.Getenv`/`config`, Nuxt `runtimeConfig`,
  Python) **обязана** появиться в `.env.example` в том же коммите.
- С браузера API через `NUXT_PUBLIC_API_BASE`; SSR-роуты — через
  `NUXT_INTERNAL_API_BASE` (докер-сеть). `/api/*` на `finance.local` → Nuxt; Go на
  `api.finance.local`. Не заворачивать `/api` в Go.
- Auth-токены — UUID в Redis, не JWT. Middleware ролей — `auth.RequireRole`.

---

## 7. Модель данных (БД `finance`, миграции, DDL)

Новая цепочка миграций `migrations/0010_plans_*` … (snake_case, `BIGSERIAL`,
`TIMESTAMPTZ`, `JSONB` — как `0008/0009_debt_*`). Префикс таблиц — `plans_` / `pl_`
/ `dir_`.

### 7.1. Ядро процесса

```sql
-- 0010_plans_core.up.sql
CREATE TABLE pl_instance (
  id            BIGSERIAL PRIMARY KEY,
  period_year   INT  NOT NULL,
  period_month  INT  NOT NULL,                 -- отчётный месяц M
  status        TEXT NOT NULL DEFAULT 'draft', -- draft|in_progress|returned|waiting_dependency|approved|archived
  calendar_id   BIGINT,                        -- производственный календарь р.д.
  template_ver  INT,                           -- зафиксированная версия шаблона (TPL-04)
  created_by    BIGINT REFERENCES users(id),
  created_at    TIMESTAMPTZ NOT NULL DEFAULT now(),
  UNIQUE (period_year, period_month)
);

CREATE TABLE pl_stage_instance (
  id          BIGSERIAL PRIMARY KEY,
  pl_id       BIGINT NOT NULL REFERENCES pl_instance(id) ON DELETE CASCADE,
  stage_code  TEXT NOT NULL,                   -- '1.1'…'4'
  track       TEXT NOT NULL,                   -- 'sales'|'production'|'final'
  status      TEXT NOT NULL DEFAULT 'pending', -- pending|in_progress|completed|returned|blocked
  assignees   JSONB NOT NULL DEFAULT '[]',     -- [user_id…]
  due_at      TIMESTAMPTZ,
  depends_on  JSONB NOT NULL DEFAULT '[]',     -- ['2.4', …]
  UNIQUE (pl_id, stage_code)
);

CREATE TABLE pl_metric (
  id              BIGSERIAL PRIMARY KEY,
  pl_id           BIGINT NOT NULL REFERENCES pl_instance(id) ON DELETE CASCADE,
  template_code   TEXT NOT NULL,               -- 'TPL-MP'
  segment         TEXT,                        -- 'large'|'small' (для TPL-MP)
  line_code       INT,                         -- code_pl (1046, 1045, 1022, …)
  block_type      TEXT,                        -- sales_manager_price|spp|shipments|…
  profit_center   INT,                         -- code_cfo (площадка)
  cost_center     INT,
  country         TEXT,                        -- BY|RU|KZ|UZ
  legal_entity    TEXT,
  channel         TEXT,                        -- 'Marketplaces'…
  scenario        TEXT NOT NULL,               -- 'Тактика бюджет (таргеты)'
  period_year     INT NOT NULL,
  period_month    INT NOT NULL,                -- месяц значения (M-1, M, M+1, …)
  currency        TEXT NOT NULL,               -- BYN|RUB|USD
  amount          NUMERIC(20,4),               -- введённая тактика (plan_tactic)
  amount_fact     NUMERIC(20,4),               -- read-only факт (OLAP/SQL)
  amount_strategy NUMERIC(20,4),               -- read-only стратегия/план
  amount_calc     NUMERIC(20,4),               -- движок CALC (этап 2)
  is_manual       BOOLEAN NOT NULL DEFAULT false,
  UNIQUE (pl_id, template_code, segment, line_code, block_type,
          profit_center, scenario, period_year, period_month, currency)
);
CREATE INDEX idx_pl_metric_pl ON pl_metric(pl_id);
CREATE INDEX idx_pl_metric_lookup ON pl_metric(template_code, segment, period_year, period_month);

CREATE TABLE form_submission (
  id           BIGSERIAL PRIMARY KEY,
  pl_id        BIGINT NOT NULL REFERENCES pl_instance(id) ON DELETE CASCADE,
  stage_id     BIGINT REFERENCES pl_stage_instance(id),
  template_code TEXT NOT NULL,
  json_payload JSONB NOT NULL,                 -- полный снимок таблицы (см. §9.6)
  submitted_by BIGINT REFERENCES users(id),
  submitted_at TIMESTAMPTZ NOT NULL DEFAULT now()
);
```

### 7.2. Корректировки, комментарии, согласование, аудит

```sql
-- 0011_plans_workflow.up.sql
CREATE TABLE pl_adjustment (             -- ADJ-01..03
  id                 BIGSERIAL PRIMARY KEY,
  metric_id          BIGINT NOT NULL REFERENCES pl_metric(id) ON DELETE CASCADE,
  original_calculated NUMERIC(20,4),
  adjusted_value     NUMERIC(20,4) NOT NULL,
  reason             TEXT NOT NULL,            -- обязательное основание
  adjusted_by        BIGINT REFERENCES users(id),
  adjusted_at        TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE TABLE pl_comment (                -- COM-01..05
  id         BIGSERIAL PRIMARY KEY,
  pl_id      BIGINT NOT NULL REFERENCES pl_instance(id) ON DELETE CASCADE,
  stage_id   BIGINT REFERENCES pl_stage_instance(id),
  metric_ref TEXT,                            -- ссылка на ячейку/строку
  body       TEXT NOT NULL,
  mentions   JSONB NOT NULL DEFAULT '[]',     -- @login
  status     TEXT NOT NULL DEFAULT 'open',    -- open|resolved
  author_id  BIGINT REFERENCES users(id),
  created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE TABLE pl_approval (               -- WF-05
  id        BIGSERIAL PRIMARY KEY,
  pl_id     BIGINT NOT NULL REFERENCES pl_instance(id) ON DELETE CASCADE,
  stage_id  BIGINT REFERENCES pl_stage_instance(id),
  legal_entity TEXT,                          -- для этапа 3 — по ЮЛ
  user_id   BIGINT REFERENCES users(id),
  decision  TEXT NOT NULL,                    -- approve|return|delegate
  comment   TEXT,
  decided_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE TABLE plans_audit_event (         -- AUD-01..05 (флаг audit.enabled)
  id            BIGSERIAL PRIMARY KEY,
  ts            TIMESTAMPTZ NOT NULL DEFAULT now(),
  user_id       BIGINT,
  action        TEXT NOT NULL,
  entity_type   TEXT,
  entity_id     BIGINT,
  before        JSONB,
  after         JSONB,
  ip            TEXT,
  correlation_id TEXT
);
```

### 7.3. Справочники и ABAC

```sql
-- 0012_plans_directories.up.sql
CREATE TABLE plans_directory (            -- DIR-01..05
  id          BIGSERIAL PRIMARY KEY,
  code        TEXT UNIQUE NOT NULL,       -- 'dir_cfo','dir_pl_line','dir_marketplace',…
  source      TEXT NOT NULL,             -- lisa|1c|olap|manual|calculated
  sync_status TEXT NOT NULL DEFAULT 'never', -- ok|error|stale|never
  synced_at   TIMESTAMPTZ,
  last_error  TEXT
);

CREATE TABLE plans_directory_row (
  id           BIGSERIAL PRIMARY KEY,
  directory_id BIGINT NOT NULL REFERENCES plans_directory(id) ON DELETE CASCADE,
  external_id  TEXT,                      -- lisa_id / olap-ключ
  payload_json JSONB NOT NULL,
  valid_from   DATE,
  valid_to     DATE
);
CREATE INDEX idx_plans_dir_row ON plans_directory_row(directory_id);

CREATE TABLE plans_user_scope (           -- 7.1.7 «Пользователь ↔ роль ↔ объект»
  id         BIGSERIAL PRIMARY KEY,
  user_id    BIGINT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
  role       TEXT NOT NULL,               -- прикладная роль внутри процесса
  stage_code TEXT,                        -- ограничение по этапу (nullable=все)
  country    TEXT,
  legal_entity TEXT,
  code_cfo   JSONB NOT NULL DEFAULT '[]', -- список разрешённых ЦФО (или статья)
  UNIQUE (user_id, role, stage_code, country, legal_entity)
);

-- WF-04 (Q3): производственные календари ПО СТРАНАМ, глобально на проект, по году.
-- Дата N-го р.д. этапа считается по календарю страны ответственного
-- (plans_user_scope.country). Переиспользуемо (табель и пр.).
CREATE TABLE plans_country_calendar (
  id          BIGSERIAL PRIMARY KEY,
  country     TEXT NOT NULL,              -- BY|RU|KZ|UZ
  year        INT  NOT NULL,
  holidays    JSONB NOT NULL DEFAULT '[]',  -- ['2026-01-01',…] нерабочие праздники
  work_weekends JSONB NOT NULL DEFAULT '[]',-- перенесённые рабочие выходные (рабочие сб/вс)
  short_days  JSONB NOT NULL DEFAULT '[]',  -- сокращённые дни [{"date":"2026-03-07","hours":7}]
  updated_by  BIGINT REFERENCES users(id),
  updated_at  TIMESTAMPTZ NOT NULL DEFAULT now(),
  UNIQUE (country, year)
);
-- Функция «N-й рабочий день месяца M для страны C» = вычисляется из этого
-- календаря (выходные сб/вс − holidays + work_weekends). Кэш дат этапов цикла —
-- опционально в pl_stage_instance.due_at при создании экземпляра.

CREATE TABLE calc_rule (                  -- CALC-01 (этап 2) — дефолтные формулы каскада
  id          BIGSERIAL PRIMARY KEY,
  code        TEXT NOT NULL,              -- 'markup','retail_margin','sales_platform_price',…
  template_code TEXT NOT NULL,            -- 'TPL-MP'
  block_type  TEXT,                       -- к какому блоку относится формула
  formula_expr TEXT NOT NULL,             -- выражение: 'sales_net - cogs', '1046/(1+vat)', …
  version     INT NOT NULL,
  valid_from  DATE,
  UNIQUE (code, template_code, version)
);

CREATE TABLE pl_formula_override (        -- D11: per-срез override формулы финансистом
  id          BIGSERIAL PRIMARY KEY,
  pl_id       BIGINT NOT NULL REFERENCES pl_instance(id) ON DELETE CASCADE,
  scope_code_cfo INT,                     -- площадка/ЦФО, на которую распространяется (nullable=весь срез)
  block_type  TEXT NOT NULL,
  line_code   INT,
  formula_expr TEXT NOT NULL,             -- переопределённое выражение
  reason      TEXT NOT NULL,              -- обязательное основание (как ADJ-02)
  version     INT NOT NULL DEFAULT 1,
  author_id   BIGINT REFERENCES users(id),
  created_at  TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE INDEX idx_pl_formula_override_pl ON pl_formula_override(pl_id);
```

Разрешение формулы движком (D11): **override (`pl_formula_override` для этого
экземпляра/среза/ячейки) → иначе `calc_rule` нужной версии**. Override наследует
ABAC-срез автора (финансист правит формулу только в своём разрезе). Требуется
безопасный вычислитель выражений (whitelisted операции, без произвольного кода).

`directory_row.payload_json` — гибкий, чтобы не плодить таблицы под каждый
справочник; конкретные поля см. §8.

---

## 8. Справочники (`dir_*`)

| Код | Источник (онлайн / первичка) | Ключевые поля payload | Этапы |
|---|---|---|---|
| `dir_cfo` | 1С/OLAP; первичка «Справочник ЦФО и ЦЗ» (листы «ЦФО маг РБ/РФ/KZ/UZ», «пр-ва, отделы», «маркетинг», PQ «Все коды») | `code_cfo, name_cfo, group_cfo1, group_cfo2, country, entity_type, lisa_id, trade_area_m2, lfl_status, director_id, responsible_id` | все |
| `dir_pl_line` | «Справочник ЦФО и ЦЗ» → «Расходы Code PL» (+ «Переменные»/«Постоянные») | `code_pl, expense_name, group_pl, cost_type` | 1.5, 2.3, **MP** |
| `dir_marketplace` | «Тактический план_*_МП» → «Списки_для_фильтров», «Источник_МП» | `mp_code, mp_name, code_cfo, code_pl(1046), segment, country` | **1.1 MP** |
| `dir_scenario` | OLAP Budgeting, изм. Scenario | `Факт по начислениям, Факт по платежам, Стратегический бюджет, Тактика бюджет (таргеты)` | все |
| `dir_fx_rate` | «Списки_для_фильтров» (Month/курс тактика/Валюта) / ТО «курсы» | `month, rate, currency, scenario` | 1.6, 3, **MP** |
| `dir_store_to` | «ТО 2025-2026+» / SQL Gpartner | `store_name, code_cfo, lisa_id, lfl, store_type, country, city, manager, trade_area` | 1.1 (TO) |
| `dir_cfo_mapping` | «ТО» → «Справка_ID лисы_001Code CFO» | `code_fox, code_cfo, lisa_id, country, group_cfo2` | 1.1 |
| `dir_lease`, `dir_payroll_rate`, `dir_headcount`, `dir_projects`, `dir_marketing_map` | 1С / Lisa / хранилище | см. ТЗ §7.1 | этап 2 |

**Правила сценариев по этапам (ТЗ 7.1.11):** на 1.1/1.5 editable только «Тактика
бюджет (таргеты)»; «Факт» и «Стратегический бюджет» — read-only. Модуль стратегии
(этап 2) — editable «Стратегический бюджет»; копирование в тактику — TPL-09.

**Синхронизация (DIR-01..05):** реестр с источником; версия/дата актуальности;
cron + ручной запуск админом; при ошибке — статус `error` + текст + повтор с
экспоненциальной задержкой; справочники в расчётах блокируют пересчёт при `stale`
(настраиваемо). UI справочников: список с фильтром по источнику/статусу, просмотр
записей, журнал синхронизации; ручное редактирование только для `manual`.

---

## 9. Шаблоны форм: общая модель + TPL-MP в деталях

### 9.1. Общая модель (ТЗ TPL-01..08)

- TPL-01 — шаблон привязан к этапу и/или роли.
- TPL-02 — элементы: группы, строки PL, числовые поля, lookup-справочники,
  read-only вычисляемые.
- TPL-03 — валидации: обязательность, min/max, зависимость полей.
- TPL-04 — версионирование шаблона; экземпляр PL фиксирует версию при создании.
- TPL-05 — табличный ввод (много строк).
- TPL-06 — экспорт/импорт Excel (обяз. для 1.1, 2.1, 1.5).
- TPL-07 — отдельные шаблоны на ЦП/ЦЗ × страна.
- TPL-08 — свод 1.6/2.4: ЮЛ × канал, read-only агрегаты + корректируемые ячейки.
- TPL-09 — копирование утверждённого сценария в новый период.
- TPL-10 — вложенная подформа/файл расчёта для статей с формулами.

Прочие шаблоны (этап 2, описаны в ТЗ §7.2): TPL-TO-RETAIL (1.1, ТО розница),
TPL-CFO-EXP (1.5/2.3, РБ ВСЕ ЦФО), TPL-WHOLESALE, TPL-IM, TPL-PROD-MINUTES (2.1),
TPL-STRATEGY. В этой спеке детально раскрыт **TPL-MP**; остальные — каркасом (§24).

### 9.2. TPL-MP — общая схема

Large и small — **две нарезки одного шаблона** TPL-MP (этап 1.1, канал
Marketplaces) по сегменту. Матрица формы:

> **площадка (code_cfo) × блок метрики (code_pl/block_type) × месяц × сценарий × валюта**

Разделение в источнике: поле `Group_МП_new` = `Маркетплейсы_large`(группа 250) /
`Маркетплейсы_small`(группа 480).

### 9.3. Шапка формы

| Поле | Поле в системе | Редактируемость | Источник |
|---|---|---|---|
| Планируемый месяц | `year`, `month` | выбор | пользователь |
| Валюта (переключатель) | `currency_mode` | выбор | RUB / BYN / USD |
| Сценарий | `scenario` | фиксирован | «Тактика бюджет (таргеты)» |
| Страна | `country` | read-only / фильтр | `dir_cfo`, ABAC |

### 9.4. Площадки (строки-детализация)

**Large (группа 250):** Wildberries(335), Lamoda(336), Ozon(337), Yandex
Market/«ООО Яндекс.Маркет»(954).

**Small (группа 480):** Детский мир(953,RU), Золотое яблоко(955,RU), Yandex Market
ТЕКС(957,RU), Детский мир ТЕКС(959,RU), Wildberries ТЕКС(990,RU), Ozon
ТЕКС(991,RU), Lamoda ТЕКС(958,RU), Wildberries KZ(475,KZ), Ozon KZ(474,KZ),
Kaspi(338,KZ), Uzmarket(339,UZ).

### 9.5. Блоки метрик (строки формы, прототип `Маркетплейсы_large`)

Каждый блок — строка-итог по `code_pl` + 4 строки детализации по площадке. Порядок
сверху вниз (с точными `code_pl` из прототипа):

| # | block_type | code_pl | Наименование | Ввод/расчёт |
|---|---|---|---|---|
| 1 | `sales_manager_price` | **1046** | ПРОДАЖИ по ценам менеджера с НДС | ввод (тактика) |
| 2 | `sales_manager_price_net` | **1045** | ПРОДАЖИ по ценам менеджера без НДС | расчёт от 1046/НДС |
| 3 | `spp` | — | % СПП (скидка) | ввод |
| 4 | `sales_platform_price` | **1022** | ПРОДАЖИ по цене площадки с НДС | расчёт/ввод |
| 5 | `sales_platform_price_net` | **1006** | ПРОДАЖИ по цене площадки без НДС | расчёт |
| 6 | `shipments` | **8006** | Себестоимость по отпускным ценам | ввод |
| 7 | `markup` | — | Наценка, % | расчёт |
| 8 | `discounts` | — | Скидки и уценки по всем площадкам МП (Скидка BYN / Уценка BYN) | ввод |
| 9 | `retail_margin` | — | Маржа розничная (+ Маржинальность розничная, %) | расчёт |
| 10 | `cogs_total` | **2006/6006** | Себестоимость общая (Cost of goods) осн+пошив | ввод/источник |
| 11 | `markup_total` | — | Наценка от общей сс, % | расчёт |
| 12 | `gross_margin` | — | Маржа (gross) + Маржа (gross), % | расчёт |
| 13 | `direct_costs` | (см. ниже) | Прямые затраты по площадкам (по каждой площадке) | ввод/источник |
| 14 | `pl_by_platform` | — | PL по площадкам (от прямых затрат) + % | расчёт |
| 15 | `pl_by_cogs` | — | PL (сумма) от себестоимости общей + % | расчёт |
| 16 | `common_costs` | **250** | Общие затраты по МП (кроме прямых) | ввод/источник |
| 17 | `pl_total` | — | PL (сумма) / PL (%) | расчёт |
| 18 | `direct_share` | — | Доля прямых затрат в обороте (по площадке) | расчёт |

**Прямые затраты по площадкам (блок 13)** — повторяется для каждой площадки
(WB/Lamoda/Ozon/YM), статьи (с `code_pl`) и для каждой — «Доля в выручке по ценам
менеджера, %»: Комиссия площадки; Агентское вознаграждение(64); Грузоперевозки
экспорт(51); Транспортная логистика(52); Складская логистика(54); РЕКЛАМА И
МАРКЕТИНГ(13); Реклама-соц.сети(15); Расходы на упаковку/пакеты(45); Эквайринг(58);
Расходы на IT обслуживание ПО(48); Штрафы(66); Прочие удержания и компенсации(66).

**Общие затраты (блок 16, группа 250)** — стандартные группы PL с `code_pl` из
`dir_pl_line`: ВЫПЛАТЫ РАБОТНИКАМ(24,26,27,28,29,30,31,32,78,79,80,81,86,87,88,89),
РЕКЛАМА И МАРКЕТИНГ(10,12,13,14,15,16,17,18,19,22,23,91), ПРОЧИЕ РАСХОДЫ
(58,60,61,62,63,64,65,83,66), ОСНОВНЫЕ РАСХОДЫ(25,33,35,36,37,38,39,40,41,42,45,84,92),
ВСПОМОГАТЕЛЬНЫЕ(21,46,47,48,49,50,51,52,53,85,93), АРЕНДА(54,55), АМОРТИЗАЦИЯ(67).

### 9.6. Колонки (метрики по периодам, прототип large)

| Группа колонок | Поле в системе | Редактируемость | Источник |
|---|---|---|---|
| Прошлый месяц / Факт (M−1) | `amount_fact[month−1]` | read-only | OLAP, лист «Источник_МП» |
| **Тактика бюджет (таргеты) — M** | `plan_tactic[M]` | **editable** | ввод |
| **Тактика бюджет (таргеты) — M+1** | `plan_tactic[M+1]` | **editable** | ввод |
| Стратегия / План | `plan_strategy` | read-only | OLAP, копирование TPL-09 |
| Факт 2025 помесячно (Янв…Дек+ИТОГ) | `fact[2025,m]` | read-only | «Источник_МП» |
| Факт 2026 помесячно (+ИТОГ) | `fact[2026,m]` | read-only | OLAP |
| Прогноз 1 пг 2026 (факт 4+тактика 2) | расчёт | read-only | форма |
| План 1 пг / Отклонение (руб., %) | расчёт | read-only | форма |
| **Тактика бюджет — конструктор 12 мес 2026** | `plan_tactic[2026,m]` | **editable** | ввод |
| План 12 мес 2026 | `plan_strategy[2026,m]` | read-only | OLAP |

Правило этапа 1.1: редактируется **только** сценарий «Тактика бюджет (таргеты)»;
факт и стратегия — read-only.

### 9.7. form_submission.json_payload (TPL-MP)

```json
{
  "template_code": "TPL-MP",
  "segment": "large",
  "period": {"year": 2026, "month": 6},
  "header": {"currency_mode": "RUB", "scenario": "Тактика бюджет (таргеты)"},
  "rows": [
    {"code_cfo": 335, "mp_platform": "Wildberries", "code_pl": 1046,
     "block_type": "sales_manager_price", "amount": 457380000.0,
     "comment": "корректировка на акцию", "is_manual": true}
  ]
}
```

### 9.8. Куда сохраняется ввод

- Editable-ячейки → `pl_metric` (`line_code=code_pl`, `profit_center=code_cfo`,
  `block_type`, `scenario`, `period`, `currency`, `amount`, `is_manual`).
- Полный снимок → `form_submission.json_payload`.
- Комментарий при ручной корректировке — **обязателен** (COM-01, ADJ-02) →
  `pl_comment` + `pl_adjustment.reason`.
- Ключ записи (ТЗ-MP §5.2): `mp_platform + code_cfo + code_pl + block_type +
  scenario + year + month + currency_mode`.

---

## 10. Интеграции и источники данных (онлайн)

D3: online сразу; `PLANS_MOCK=1` — fallback на фикстуры (по умолчанию off, как
`DEBT_MOCK`). Коннекторы в `go/internal/plans/sources/`.

**Креды (Q1 ✅):** OLAP-источник МП живёт на том же сервере **FinDWH**, что и
ВГО-отчёт. Переиспользуем существующие `MSSQL_PREMASTER_*` (тот же `*sql.DB`, что
уже открывается в `main.go` для debt) — **отдельные `PLANS_OLAP_*` не вводим**.
К уточнению — лишь имя объекта в FinDWH (`ALL_view_МП`, `Источник_МП_штрафы`);
снять `cmd/mssql-probe`. До уточнения — `PLANS_MOCK=1`.

| Запрос/подключение | Источник | Прототип | Назначение |
|---|---|---|---|
| `ALL_view_МП` | OLAP куб Продажи + SQL Payments.Debt | «Источник_МП» | факт МП (выручка, продажи, СПП, себестоимость) |
| Источник_МП_штрафы | Отчёт WB / Payments | «Источник_МП_штрафы» | штрафы (CodePL=66) |
| olap Budgeting Budget | OLAP куб Budgeting, срез Budget | РБ ВСЕ ЦФО | факт/план статей PL (1.5, 2.3) |
| olap mf_test Продажи | OLAP куб mf_test.Продажи | ТО | факт продаж (1.1) |
| statistics | SQL Gpartner (s_klient, stat_klient, abrvtr) | ТО | магазины, LFL, площадь |
| Курсы тактика | «Списки_для_фильтров» / ТО «курсы» | — | `dir_fx_rate`, пересчёт валюты |

### 10.1. Схема листа «Источник_МП» (read-only факт МП)

Запрос OLAP `ALL_view_МП`. Колонки:
`дата для сцепки | Scenario(План/Тактика бюджет) | Год | Номер месяца | CodePL |
CodeCFO | Group_МП_new(Маркетплейсы_large/small) | Amount_BYN(H) | Amount_RUB(I) |
Amount_USD(J)`. Агрегация SUMIFS по ключам **month + code_cfo + scenario +
CodePL(block_type)**; в трёх валютах (H/I/J).

Дополнительные `code_pl` в источнике (13, 24, 26, 46, 48, 5006, 6006, 7006, 9006…)
— вспомогательные статьи внутри блоков продаж и СПП.

### 10.2. Схема листа «Источник_МП_штрафы»

`Страна | Месяц | CodePL(66) | CodeCFO | GroupCFO1 | CFO | Сумма без
НДС_нацвалюта | _BYN | _USD | _RUR | Наименование(тип штрафа)`.

### 10.3. «Списки_для_фильтров» → справочники UI

`GroupPL_для_затрат`, `GroupCFO3_для_затрат`, `Code_канала_для_маржи` (06 =
Маркет-плэйсы), `Code_для_вязания_полотна` (связь с полотном Intercom), курсы
валют (`Month | курс тактика | Валюта(RUB/USD) | Ф/П`).

Маршрутизация валют: суммы хранятся в BYN/RUB/USD; переключатель шапки выбирает
отображение; курсы тактики — из `dir_fx_rate` (помесячно, RUB и USD).

---

## 11. Движок расчётов (формулы) — этап 2

Расчёты по версии правил (`calc_rule`), привязанной к экземпляру PL; результат:
`amount_calc`, источник формулы, входы (для аудита). MVP — числовой ввод без
автоформул; каскад наценки/маржи/долей считается на этапе 2.

**Модель редактирования формул (D11 — гибрид).** Формула хранится и выводится в
UI (drill-down «в формулу», экран #4):
- **Дефолт** — версионируемые правила `calc_rule.formula_expr` (правит «Администратор
  процессов» в конфигураторе шаблонов; версия привязана к периоду, CALC-01).
- **Override** — финансист может переопределить формулу для своей ячейки/строки
  **в своём ABAC-срезе** → `pl_formula_override` (с обязательной причиной, как
  ADJ-02; версионируется).
- **Разрешение при расчёте:** `pl_formula_override` (срез/ячейка) → иначе
  `calc_rule`. Движок — **безопасный вычислитель выражений** (`go/internal/plans/
  calc/`): whitelisted операции (+ − × ÷, MAX/MIN, ссылки на `block_type`/входы),
  без eval произвольного кода. Лог расчёта (CALC-03): входы → применённая формула
  (calc_rule vN или override) → результат. Переопределение ЗНАЧЕНИЯ (не формулы) —
  отдельный механизм `pl_adjustment` (ADJ); формула и значение независимы.

### 11.1. Каскад TPL-MP (восстановить точные формулы по ячейкам — ❓Q4)

Из структуры прототипа выводятся зависимости (значения формул в Excel скрыты,
подтвердить у автора):
- `1045 (без НДС) = 1046 / (1 + НДС_страны)`; аналогично `1006 = 1022 / (1+НДС)`.
- `1022 (по цене площадки с НДС) = 1046 × (1 − %СПП)` (СПП — скидка площадки).
- `Наценка, % = (продажи_без_НДС − себестоимость) / себестоимость`.
- `Маржа розничная = продажи_без_НДС − себестоимость − скидки/уценки`.
- `Маржа (gross) = продажи_без_НДС − себестоимость_общая(COGS)`.
- `Прямые затраты(статья) = выручка_по_ценам_МД × доля_в_выручке_%`.
- `PL по площадке = маржа − Σ прямые затраты`.
- `PL (сумма) = Σ PL по площадкам − общие затраты(группа 250)`.
- `Доля прямых затрат в обороте = Σ прямые / оборот`.

### 11.2. Прочие алгоритмы (ТЗ §«Алгоритмы») — этап 2

- **ФОТ производства от минут:** `FOT_prod = minutes_plan × rate_prod × coef_prod`;
  `× (1 + k_soc)`; распределение по маппингу «подразделение → статья PL».
- **ФОТ розницы от продаж:** A) `revenue × pct_payroll`; B) `hours × rate × coef`;
  C) `MAX(revenue×pct_min, hours×rate)`.
- **Аренда от договоров:** fixed `area × rate × index_factor`; percent
  `revenue(store) × revenue_share`; mixed `fixed + variable`.
- **Логистика от отгрузок:** `Σ ship_volume × unit_price(route, tier)`;
  `MAX(trip_min, calc)` + топливная надбавка; распределение по каналам.
- **Свод PL:** `PL_line = Σ allocated_metrics (+ корректировки)`; контроль
  сходимости и отклонения от прошлой тактики > X%.

Требования CALC-01..05: версионирование формул; полный/инкрементальный пересчёт;
лог расчёта; ручная корректировка не перезаписывается автопересчётом без явного
сброса; кнопка «Пересчитать» с предпросмотром diff.

---

## 12. Workflow согласования (этапы 1.1–4)

Маршрут фиксирован схемой (этапы 1.1–1.6, 2.1–2.4, 3, 4); конфигуратор меняет
назначенных, SLA, кворум — но не порядок зависимостей (§4.5).

| ID | Требование |
|---|---|
| WF-01 | Два трека (`sales`, `production`) + финал 3–4 |
| WF-02 | Назначение по матрице: этап × ЦП/ЦЗ × страна × ЮЛ |
| WF-03 | Действия: Сохранить, Отправить на согласование, Согласовать, Вернуть на этап N, Делегировать |
| WF-04 | SLA = календарь (25-е, 2–6 р.д.); напоминания за 1 р.д. до дедлайна |
| WF-05 | Лист согласования по экземпляру и по каждому ЮЛ (этап 3) |
| WF-06 | 1.2 — параллельное согласование по ЦП |
| WF-07 | 1.5/2.3 — параллельные подзадачи по статьям и странам |
| WF-08 | 3 — параллельно по ЮЛ (7+); переход к 4 — когда все ЮЛ `approved` |
| WF-09 | 1.6 показывает статус 2.4 и блокирует свод при незавершённом пр-ве |

MVP реализует жизненный цикл `pl_instance`/`pl_stage_instance` и действия WF-03 для
этапа 1.1 (TPL-MP); полная цепочка 1.2–4 — этап 2.

---

## 13. API-контракты

Префикс `/api/plans/*`, паттерн `http.NewServeMux` + `auth.RequireRole`. Формат
ответов — JSON (`writeJSON`/`writeErr`, как в `reports/debt`).

```
# Справочники
GET    /api/plans/directories                 # список dir_* + статус синхронизации
GET    /api/plans/directories/{code}/rows      # записи справочника (фильтр source/status)
POST   /api/plans/directories/{code}/sync      # ручная синхронизация (PLANS_ADMIN)

# Экземпляры PL
GET    /api/plans/instances                    # фильтры: year, month, status, channel, country, segment
POST   /api/plans/instances                    # создать цикл на период (PLANS_ADMIN)
GET    /api/plans/instances/{id}               # карточка: треки, своды, статусы этапов

# Форма TPL-MP
GET    /api/plans/mp/form                       # ?year&month&segment&currency — собранная матрица (факт+стратегия+тактика), ABAC-срез
PUT    /api/plans/mp/form                       # сохранить editable-ячейки (plan_tactic) + снимок form_submission
GET    /api/plans/mp/fact                        # ?year&month&segment — read-only факт из OLAP/SQL (Источник_МП)
GET    /api/plans/mp/penalties                   # штрафы (CodePL=66)

# Корректировки / комментарии / согласование
POST   /api/plans/instances/{id}/adjust          # ADJ-01..03 (reason обязателен)
GET    /api/plans/instances/{id}/comments
POST   /api/plans/instances/{id}/comments         # COM-01..05
POST   /api/plans/instances/{id}/stages/{code}/action  # WF-03: submit|approve|return|delegate

# Импорт/экспорт (TPL-06)
GET    /api/plans/mp/export                       # .xlsx снимок формы
POST   /api/plans/mp/import                       # загрузка editable-колонок (валидация ABAC/code_cfo/code_pl/reason)

# Аудит (AUD)
GET    /api/plans/audit                           # ?user&period&pl_id (view_audit)
```

Контракт `GET /api/plans/mp/form` (ответ, укрупнённо):
```json
{
  "header": {"year":2026,"month":6,"segment":"large","currency":"RUB",
             "scenario":"Тактика бюджет (таргеты)"},
  "platforms": [{"code_cfo":335,"name":"Wildberries","country":"RU"}, …],
  "blocks": [{"block_type":"sales_manager_price","code_pl":1046,
              "name":"ПРОДАЖИ по ценам менеджера с НДС","editable":true,
              "rows":[{"code_cfo":335,
                       "fact":{"2026-05":357034569.85},
                       "strategy":{"2026-06":457380000.0},
                       "tactic":{"2026-06":457380000.0,"2026-07":457380000.0}}]}],
  "abac":{"allowed_code_cfo":[335,336,337,954]}
}
```

---

## 14. UI: экраны, навигация, дизайн-система

### 14.1. Навигация (ТЗ §7.3)

Левое меню «Финансы и экономика» (`nuxt/app.vue`, `<aside class="app-sidebar">`):
- **«Тактические планы»** — формы ввода, согласование, карточка PL.
- «Управленческая отчётность» — read-only своды (этап 2).
- «Годовая стратегия» — стратегические сценарии и копирование в тактику (этап 2).

Пункт меню виден при `useScope.hasScope('plans')` (роль `ROLE_PLANS_*`).

### 14.2. Экраны (ТЗ §«Карта»)

| № | Экран | Маршрут | Описание |
|---|---|---|---|
| 1 | Дашборд | `/plans` | задачи по этапу/р.д., потоки 1 и 2, просрочки, блок 1.6 до 2.4 |
| 2 | Список PL | `/plans/list` | фильтры: период, статус, ЦП/ЦЗ, ЮЛ, канал, страна |
| 3 | Карточка PL | `/plans/[id]` | вкладки: трек продаж, трек пр-ва, свод ЮЛ/каналы, согласование, комментарии, история |
| 4 | **Редактор TPL-MP** | `/plans/[id]/mp/[segment]` | таблица площадка×блок×месяц, подсветка корректировок, drill-down |
| 5 | Справочники | `/plans/directories` | список + карточка + журнал синхронизации |
| 6 | Конфигуратор процессов | `/plans/admin/workflow` | этапы, маршруты, роли (этап 2) |
| 7 | Конфигуратор шаблонов | `/plans/admin/templates` | конструктор полей (этап 2) |
| 8 | Журнал аудита | `/plans/audit` | при `audit.enabled` |

### 14.3. Дизайн-система (обязательно)

Только токены `nuxt/assets/styles/design-system.css`. Числа — `var(--font-mono)`
(JetBrains Mono) + `font-variant-numeric: tabular-nums`, выравнивание вправо
(класс `.col-num`). Hairline borders без теней. Акцент индиго `--accent: #4338ca`.
Дельты: `--pos:#0a7f3f` / `--neg:#b42318`. Первая колонка (площадка/статья) —
`position: sticky` (`.col-sticky`). Большие таблицы — виртуализация/пагинация (НФТ:
открытие карточки ≤3 с при до 10 000 ячеек). **Цвета не хардкодить.**

Цветовая гамма раздела (ТЗ §7.3): синий/голубой/серый (корпоративная отчётность) —
ложится на текущие токены.

---

## 15. Импорт/экспорт Excel (TPL-06)

- **Экспорт:** текущий снимок формы → `.xlsx` с теми же именами колонок, что в
  прототипе (площадка, code_pl, месяцы, валюта).
- **Импорт:** только editable-колонки («Тактика бюджет» / `plan_tactic`);
  обязательны `code_cfo + code_pl + month + reason` (ADJ-02).
- **Валидация:** неизвестный `code_cfo/code_pl` → ошибка строки; чужой ABAC-срез →
  отклонение всего файла.
- MVP: импорт/экспорт для TPL-MP (large). Реализация — `importexport.go` (без
  внешних бинарей; xlsx как zip+XML, как в инструментах разведки этой спеки, либо
  Go-библиотека xlsx — выбрать в T-IMP).

---

## 16. Уведомления (Bitrix24)

Переиспользуем существующий канал Finance (`notifications.Service`, CLAUDE.md
§«Уведомления»): возврат на этап, назначение задачи, нарушение SLA (за 1 р.д.).
B24-дублирование — через `site_api` при заданных `SITE_API_NOTIFY_*`. Per-user
отключение — чекбокс `/account`. Доставка fire-and-forget; аудит в полях
`b24_sent_at/attempts/last_error`. Новых ENV не вводим — канал общий.

---

## 17. Аудит / история (AUD-01..05)

Флаг `PLANS_AUDIT_ENABLED` (по умолчанию off в dev, on в prod). События: вход,
создание PL, сохранение формы, корректировка, пересчёт, смена этапа,
согласование/отклонение, комментарий, синхронизация справочника. Запись:
`ts, user_id, action, entity_type, entity_id, before, after, ip, correlation_id`.
UI: фильтр по пользователю/периоду/экземпляру; хронология; экспорт CSV; срок
хранения (политика, напр. 3 года).

---

## 18. Безопасность и НФТ

| Категория | Требование |
|---|---|
| Производительность | Карточка PL ≤3 с при ≤10 000 ячеек (виртуализация/пагинация) |
| Доступность | 99% в рабочие часы (SLA уточнить) |
| Безопасность | HTTPS; секреты интеграций шифруются; журнал доступа; ABAC отклоняет чужие срезы на API, не только в UI |
| Резервное копирование | Ежедневно, RPO ≤24 ч |
| Совместимость | Chrome/Edge последние 2 версии |
| Локализация | UI на русском, числа — формат РФ |
| Аутентификация | **B24 OAuth** (как Finance) — внутренние и внешние (Q2 ✅); если внешних нет в B24 — локальные учётки (Q2-b) |

---

## 19. Commands (команды разработки)

Все из `swarm/` (CLAUDE.md):

```bash
make up                 # поднять основной dev-контур (Go + Nuxt + PG + Redis)
make down
make logs-go            # логи go-api
make logs-nuxt
make psql               # psql в БД finance
make migrate            # повторно прогнать миграции на существующем volume
make reset-db           # ⚠ снести том db_data и поднять PG заново
make restart-go-api     # перезапуск go-api (перечитывает env через Makefile)
make restart-nuxt
../swarm/migrate.dev.sh           # golang-migrate up (обе БД)
../swarm/migrate.dev.sh down 1    # откатить версию
../swarm/migrate.dev.sh version
../swarm/migrate.dev.sh force N   # форснуть версию при dirty
```

Эндпоинты: Nuxt `http://finance.local` (или `:3001`); Go `http://api.finance.local`
(или `:8081`); PG `psql -h localhost -p 55432 -U finance finance`.

Тестов-раннера в репо нет (CLAUDE.md): единичные тесты пишем рядом (`*_test.go`),
запуск `go test ./internal/plans/...` внутри контейнера go-api.

---

## 20. Project structure (структура файлов) — см. §6

Новые файлы: `migrations/0010..0012_plans_*.{up,down}.sql`; пакет
`go/internal/plans/*`; роли в `go/internal/auth/roles.go`; маршруты в
`go/cmd/api/main.go`; Nuxt `pages/plans/*`, `composables/usePlans.ts`,
`composables/usePlanForm.ts`, `components/plans/*`; пункт меню в `app.vue`;
`.env.example` — блок «Тактические планы»; документация
`docs/reports/plans/*`.

Блок `.env.example` (черновик, финализировать в T-ENV; CLAUDE.md — env
обязательна в том же коммите):
```bash
# === Модуль «Тактические планы» ===
PLANS_MOCK=0                       # =1 → фикстуры вместо OLAP/SQL (fallback), по умолчанию live
PLANS_AUDIT_ENABLED=0              # =1 → писать plans_audit_event (AUD)
# Источник факта/стратегии МП — тот же сервер FinDWH, что у ВГО-отчёта:
#   переиспользуем MSSQL_PREMASTER_* (см. блок «Задолженность ВГО»).
#   Отдельные PLANS_OLAP_* НЕ вводим (Q1 ✅, SPEC §10).
PLANS_MP_FACT_VIEW=ALL_view_МП     # имя вьюхи/таблицы факта МП в FinDWH (уточнить cmd/mssql-probe)
PLANS_MP_PENALTIES_VIEW=           # источник штрафов (CodePL=66) в FinDWH
# Производственные календари р.д. — по странам (BY/RU/KZ/UZ), ведутся вручную в БД
# (plans_country_calendar), глобально на проект. Env-флаг не нужен (Q3 ✅).
```

---

## 21. Code style (стиль кода)

- **Go:** пакет `plans` по слоям `handler/service/repo` (как `reports/debt`);
  pgx pool из `db`; роуты `mux.HandleFunc("METHOD /api/plans/…", auth.RequireRole(...))`;
  JSON через `writeJSON/writeErr`; ошибки оборачивать `fmt.Errorf("...: %w", err)`;
  никаких глобалов, зависимости через конструктор `New...(pool, deps)`.
- **SQL:** snake_case, `BIGSERIAL`, `TIMESTAMPTZ`, `JSONB` для гибких payload;
  индексы на колонках фильтрации; `ON DELETE CASCADE` от `pl_instance`. Оборачивать
  NULL-колонки источников в `ISNULL/COALESCE` (memory: NULL роняет scan).
- **Nuxt/TS:** composables возвращают типизированные `$fetch`-функции с
  `authHeader()`; `base = useRuntimeConfig().public.apiBase`; страницы —
  `definePageMeta({ middleware: 'scope-guard' })`; форматтеры — `nuxt/utils/format.ts`
  (money/pct/num/delta). Числа в таблицах — `.col-num`.
- **Стили:** только токены design-system; первая колонка sticky; без теней.
- **Именование шаблонов/блоков** — как в прототипе (`TPL-MP`, `block_type`,
  `code_pl`), чтобы импорт/экспорт совпадал с Excel.

---

## 22. Testing strategy (стратегия проверки)

Раннера нет — точечные проверки:
1. **Go unit** (`internal/plans/*_test.go`): сборка матрицы формы из строк
   `pl_metric`; ABAC-фильтр (чужой `code_cfo` не попадает); валидатор импорта
   (неизвестный code_pl → ошибка строки; чужой срез → отклонение файла); пересчёт
   валют через `dir_fx_rate`.
2. **Контракт API**: snapshot JSON `GET /api/plans/mp/form` на фикстуре large
   (площадки 335/336/337/954, блоки 1046/1045/1022/1006/8006), сверка с прототипом.
3. **Сверка чисел (CHECKPOINT)**: суммы факта из онлайн-OLAP против значений
   прототипа `Маркетплейсы_large` (например WB 335, 1046, май-2026 = 357 034 569.85
   BYN-экв.) — §25.
4. **UI smoke** (`/run`, `verify`): открыть `/plans/[id]/mp/large`, ввести тактику,
   сохранить, проверить запись в `pl_metric` и снимок в `form_submission`; проверить
   обязательность комментария при корректировке; экспорт→импорт round-trip.
5. **ABAC negative**: пользователь сегмента small не видит large на API (403/пустой
   срез), не только в UI.

---

## 23. Boundaries (always / ask-first / never)

**Always (всегда делать):**
- Любую новую env-переменную сразу заносить в `.env.example` с комментарием
  (CLAUDE.md, memory `feedback-env-example-must-document`).
- Брать стили только из design-system; числа — mono+tabular-nums.
- В UI показывать `name_cfo`, не `code_cfo` (ACL-06); ABAC проверять на сервере.
- Хранить editable → `pl_metric` + полный снимок → `form_submission`.
- Снимок версии показателей при переходе этапа; обязательное основание у
  корректировки (ADJ-02) и комментарий (COM-01).
- Отвечать пользователю и вести документацию на русском (memory
  `feedback-respond-in-russian`).
- Новые миграции — парная `.up/.down`, нумерация по порядку (после `0009`).

**Ask-first (согласовать до реализации):**
- Точные коннекты/креды OLAP/SQL (❓Q1) и схема аутентификации внешних (❓Q2).
- Точные формулы каскада TPL-MP и прочих расчётов (❓Q4) — снять у автора прототипа
  до включения CALC.
- Источник производственного календаря р.д. (❓Q3).
- Любое расширение `auth.Allowed` (новые роли) — согласовать с владельцем auth.
- Детальный wireframe форм — по согласованию с Дмитрием после этапа 1 (ТЗ §7.7).

**Never (не делать):**
- Не перезаписывать корневой `SPEC.md` (это ВГО-отчёт) и не трогать `python/cost/`
  (чужой контур, `AGENTS.md`).
- Не заворачивать `/api` на `finance.local` в Go (ломает auth-контракт).
- Не парсить бинарный `.xlsb` (small) — small = тот же TPL-MP, площадки/группа 480
  из ТЗ-MP.
- Не хардкодить цвета/числа форматирования в компонентах.
- Не давать пользователю видеть/править чужой ABAC-срез.
- Не включать live-OLAP в прод без сверки чисел (CHECKPOINT, §25) — до сверки
  `PLANS_MOCK` управляемо.

---

## 24. План реализации (задачи)

### Этап 1 — MVP (форма TPL-MP large+small)

| T | Задача | Файлы | Готово, когда |
|---|---|---|---|
| T1 | Миграции ядра + справочников + ABAC | `migrations/0010..0012_plans_*` | `make migrate` проходит; таблицы есть |
| T2 | Роли `ROLE_PLANS_ADMIN/USER` | `auth/roles.go` | в `Allowed`+иерархии; `RequireRole` работает |
| T3 | Справочники `dir_cfo/pl_line/marketplace/scenario/fx_rate` + первичная загрузка из Excel-прототипов | `plans/directories.go` | строки в `plans_directory_row`; API `/directories` |
| T4 | Онлайн-коннекторы OLAP/SQL `Источник_МП`(+штрафы) + `PLANS_MOCK` | `plans/sources/*` | `/api/plans/mp/fact` отдаёт факт; mock-fallback |
| T5 | API формы: GET/PUT `/api/plans/mp/form`, сохранение в `pl_metric`+`form_submission` | `plans/handler.go,service.go,repo.go` | round-trip ввода тактики |
| T6 | ABAC-фильтр сегмента/площадок | `plans/abac.go` | negative-тест проходит |
| T7 | UI редактор TPL-MP (таблица площадка×блок×месяц, валюта, подсветка) | `pages/plans/[id]/mp/[segment].vue`, `components/plans/MpForm.vue`, `usePlanForm.ts` | ввод+сохранение из браузера; design-system |
| T8 | Комментарии + корректировки (обязательное основание) | `plans/*`, `components/plans/*` | COM-01/ADJ-02 enforced |
| T9 | Импорт/экспорт Excel TPL-MP | `plans/importexport.go` | round-trip xlsx; валидация ABAC |
| T10 | Пункт меню + дашборд-заглушка + scope-guard | `app.vue`, `pages/plans/index.vue` | раздел виден для `ROLE_PLANS_*` |
| T-ENV | `.env.example` блок + документация | `.env.example`, `docs/reports/plans/` | все env описаны |

### Этап 2 (после MVP)

Движок CALC (формулы каскада + ФОТ/аренда/логистика); workflow 1.2–4 (согласование,
зависимости WF-DEP, своды 1.6/2.4 TPL-08); шаблоны TPL-TO-RETAIL, TPL-CFO-EXP,
TPL-WHOLESALE, TPL-IM, TPL-PROD-MINUTES, TPL-STRATEGY; копирование сценария TPL-09;
конфигураторы процессов/шаблонов; полный аудит; онлайн-синхронизация справочников
по cron.

---

## 25. Checkpoints (сверка чисел перед прод)

- **CHK-A (факт):** суммы `GET /api/plans/mp/fact` из онлайн-OLAP совпадают со
  значениями прототипа `Маркетплейсы_large` (контрольные точки: WB/335/1046/2026-05
  = 357 034 569.85; Ozon/337/1046/ИТОГ-2026 = 948 763 124.08; СПП WB
  2026-06 = 0.31). Расхождение → не включать live в прод.
- **CHK-B (round-trip):** ввод тактики → `pl_metric` → снимок `form_submission` →
  экспорт xlsx → импорт обратно даёт идентичные значения.
- **CHK-C (ABAC):** пользователь large не видит small и наоборот (API-уровень).
- **CHK-D (валюта):** переключатель RUB/BYN/USD даёт суммы, согласованные с
  колонками H/I/J источника и курсами `dir_fx_rate`.

До прохождения CHK-A/B — `PLANS_MOCK=1` управляемо (как `DEBT_MOCK` для ВГО).

---

## 26. Приложения

### A. Площадки large/small (code_cfo → площадка → страна)

Large(250): 335 Wildberries · 336 Lamoda · 337 Ozon · 954 Yandex Market.
Small(480): 953 Детский мир(RU) · 955 Золотое яблоко(RU) · 957 Yandex Market
ТЕКС(RU) · 959 Детский мир ТЕКС(RU) · 990 Wildberries ТЕКС(RU) · 991 Ozon
ТЕКС(RU) · 958 Lamoda ТЕКС(RU) · 475 Wildberries KZ(KZ) · 474 Ozon KZ(KZ) · 338
Kaspi(KZ) · 339 Uzmarket(UZ).

### B. Реестр CodePL блоков продаж/себестоимости TPL-MP

1046 ПРОДАЖИ по ценам менеджера с НДС · 1045 …без НДС · 1022 ПРОДАЖИ по цене
площадки с НДС · 1006 …без НДС · 8006 Себестоимость по отпускным ценам ·
2006/6006 Себестоимость общая (Cost of goods) · 66 Штрафы/Прочие удержания · 250
группа «Общие затраты по МП». Прямые затраты: 64 Агентское · 51 Грузоперевозки
экспорт · 52 Транспортная логистика · 54 Складская логистика · 13 Реклама ·
15 Реклама-соц.сети · 45 Упаковка · 58 Эквайринг · 48 IT-ПО. Полный реестр статей
PL — `dir_pl_line` («Расходы Code PL», коды 10–98).

### C. Соответствие этапов и прототипов (ТЗ §7.4)

| Этап | Шаблон | Прототип Excel | Этап разр. | Ответственный |
|---|---|---|---|---|
| 1.1 | **TPL-MP** | Тактический план_*_МП | **1 (MVP)** | Мурашко / Левин |
| 1.1 | TPL-TO-RETAIL | ТО → Отчет_ТО_общий_new | 1 (MVP) | Смолер, Ткачева… |
| 1.5 | TPL-CFO-EXP | РБ ВСЕ ЦФО | 1 (MVP) | Антипова + статьи |
| 1.1 | TPL-WHOLESALE / TPL-IM | ТО → ИМ_ОПТ | 2 | Пистоленко / Сипаров Ю.Г. |
| 2.1 | TPL-PROD-MINUTES | Lisa | 2 | Планирование пр-ва |
| стратегия | TPL-STRATEGY | ТО → Стратегия 2026 | 2 | ФЭБ |

### D. Источники по приложению D ТЗ — см. §10.

---

## 27. References

- ТЗ: `var/plan/Техническое задание на реализацию процесса формирования и
  согласования тактических планов в веб интерфейсе (2).docx`.
- ТЗ-MP: `var/plan/TPL-MP_ формы «Тактический план_large_МП» и «…_small_МП».docx`.
- Прототипы: `var/plan/Тактический план_large_МП_2026_июнь.xlsx`
  (`Маркетплейсы_large`, `Источник_МП`, `Источник_МП_штрафы`,
  `Списки_для_фильтров`); `…_small_…xlsb`; `var/plan/Справочник ЦФО и ЦЗ.xlsx`;
  `var/plan/РБ ВСЕ ЦФО (1).xlsx`.
- Матрица ответственных (Google Sheets):
  https://docs.google.com/spreadsheets/d/1boK04_JWWBEQHOvTXKlglAMVH1AVn_uevUbDjIYnROg/edit
- Конвенции репо: `CLAUDE.md`; корневой `SPEC.md` (ВГО, **не трогать**);
  `go/internal/reports/debt/*` (эталон пакета); `go/internal/auth/roles.go`;
  `nuxt/assets/styles/design-system.css`; `migrations/0008..0009_debt_*`.
- Транскрипт согласования форм 24.06.2026 (ТЗ Приложение F).
- **Скорректированные ТЗ 2026-08** (см. §28): `var/tz/ТЗ_Маркетплейсы_тактический_план_форма.docx`
  (v1.0 · 06.08.2026), `var/tz/ТЗ_Розница_тактический_план_форма.docx`
  (v1.0 · 05.08.2026). Контрольные выборки приёмки — Приложение Б каждого ТЗ.

---

## 28. Скорректированные ТЗ 2026-08 (МП v2 + Розница)

Аналитик передал две новые редакции требований (`var/tz/`, 24.08.2026):
`ТЗ_Маркетплейсы_тактический_план_форма.docx` (v1.0 · 06.08.2026) и
`ТЗ_Розница_тактический_план_форма.docx` (v1.0 · 05.08.2026). Разделы ниже
фиксируют, чем они меняют §§9–14 этой спеки. Полный план работ — `todo.md`,
раздел «Новые ТЗ аналитика».

### 28.1. Инверсия направления расчёта (главное изменение МП)

Было (первая редакция и прототип): из источника приходят СУММЫ, а доли, наценки
и %СПП вычисляются как производные. Стало (ТЗ §3.1, требование звонка
06.08.2026): вводятся продажи по ценам менеджера с НДС, %СПП, две наценки и
УДЕЛЬНЫЕ ВЕСА статей прямых затрат, а вся расходная часть считается.

Формулы (ТЗ §3.4, проверены на контрольной выборке Приложения Б — тесты
`mpform_inverse_test.go`):

```
S_мд      = S_мд_ндс / (1 + НДС_площадки)
S_пл      = S_мд × (1 − %СПП)
СС_отп    = S_пл / (1 + Наценка)
СС_общ    = S_пл / (1 + Наценка_от_общей_сс)
Комиссия  = S_мд × %СПП
Статья    = S_мд × доля_статьи
ПЗ        = Комиссия + Σ статей
PL(площ.) = Маржа − (ПЗ − Комиссия)     ← комиссия не вычитается дважды
PL(форма) = S_пл − СС_отп − Общие − ПЗ + Σ Комиссий      (формула файла F274)
Доля ПЗ в обороте (итог) = (Σ ПЗ − Σ Комиссий) / Σ S_пл  ← по ВСЕМ площадкам
```

Направление — режим карточки (`form_card.calc_mode`: `legacy` | `inverse`), а не
глобальный переключатель: закрытые периоды считаются тем алгоритмом, которым были
утверждены, а включение инверсии на новый период — конфигурация с мгновенным
откатом (открытый вопрос ТЗ §12 п.11 формально не подтверждён).

**Дефект прототипа, который НЕ переносится:** итоговая «Доля прямых затрат в
обороте» в файле вычитает комиссии только Wildberries и Ozon (27,92 % вместо
26,32 %). В вебе итог считается по всем площадкам — отдельный пункт приёмки.

### 28.2. Реестр «Условия площадки»

Новая сущность (ТЗ §6.1): `mp_conditions` + `mp_condition_item` (миграция 0036).
Одна запись = (площадка, период): %СПП, наценка, наценка от общей сс, ставка НДС,
доли статей прямых затрат (могут быть отрицательными — компенсации), версия,
обоснование изменения. Копируется из предыдущего периода одним действием; diff
показывает согласующему, какие доли изменились и на сколько; отклонение сверх
порога требует обоснования. Фактические доли прошлых периодов остаются
производными и в реестр НЕ переносятся — только подсказка при заполнении.

### 28.3. Справочные величины вместо хардкода

- **НДС.** Эффективная ставка своя у каждой площадки и не равна законодательной
  (20,36 % у WB/Lamoda/Ozon, 16,62 % у Yandex Market). Справочник `dir_vat`
  (миграция 0034), ставка по (страна, код ЦФО); строка площадки перебивает
  страновой дефолт. Прошивать ставку в формулу запрещено (ТЗ §3.4).
- **Курсы.** Помесячные курсы в `dir_fx_rate` (год/месяц, 0 = дефолт) + валюты
  площадок KZT/UZS. Применённый курс фиксируется в версии карточки, чтобы
  утверждённые суммы не «уезжали» при следующем обновлении справочника (§3.3).
- Оба справочника читает `RateBook` (`rates.go`) с кэшем и фолбэком на seed:
  недоступность справочника не должна ронять форму.

### 28.4. Общая оболочка процесса (обе формы)

`form_card` + `card_approval` + `card_version` + `plans_form_route`
(миграция 0031). Единица маршрута — ФОРМА, а не этап периода: ТЗ МП §2.2 требует
независимости крупных и мелких МП, ТЗ Розница §2.0 — независимости четырёх стран.
На один `pl_instance` приходится несколько карточек (МП large/small,
Розница BY/RU/KZ/UZ).

- Статусы: `draft → on_approval → approved → published | publish_failed`,
  `returned`, `archived`. Метки ТЗ (`on_approval_1.2`, `returned_to_1.1`)
  собираются из пары статус+шаг.
- Возврат: только назад, с целевым шагом и ОБЯЗАТЕЛЬНЫМ комментарием; решения от
  целевого шага и выше помечаются `revoked` (не удаляются).
- Версия-снапшот пишется при каждом переходе: значения + условия + курсы + НДС.
- После утверждения период закрыт на запись ДЛЯ ВСЕХ; изменение — только через
  `reopen` с причиной (аудит).
- Маршрут — данные: шаг «Финансист» между 1.2 и 1.3 включается флагом `enabled`
  без правки кода; `skip_if_same_user` закрывает вырожденное самосогласование
  розницы РБ (заполняющий и согласующий — одно лицо).

### 28.5. Публикация во внешний контур

`publish_mapping` + `publish_log` (миграция 0032). Приёмники семейства
`FormToLoa*/VFORMTOLOAD*`: девять колонок, без первичного ключа, без колонок
сценария и ЮЛ. Отсюда: сценарий задаётся выбором таблицы; идемпотентность делает
сервис (DELETE по логическому ключу Параметр+Страна+КодЦФО+КодPL+Дата + INSERT в
одной транзакции); после записи — контроль «Σ введено − Σ прочитано = 0».

Открытые вопросы BI (§12: какой «Параметр» писать, агрегат по группе 250/480 или
детализация по площадкам, кто заполняет BYN-пару) вынесены В ДАННЫЕ — строки
`publish_mapping`. Пока строки выключены, доступен только режим dry-run: он
считает, что ушло бы, и сверяет с приёмником. Этим отчётом BI и подтверждает
решения. Запись включается `PLANS_PUBLISH_ENABLED=1`, таблицы-приёмники — белым
списком `PLANS_PUBLISH_TARGETS` (имя таблицы приходит из данных маппинга).

### 28.6. Валидации МП

Блокирующие МП-01…МП-11 и предупреждающие МП-W1…МП-W8 — `mp_validate.go`,
чистые функции с тестами. Отдельно стоят два контроля:
- **МП-08 / МП-W8 (двойной счёт).** Подтверждено на данных: ЦФО 250 стоит в форме
  ЦЗ 32 со статьями 51 и 52, и те же статьи считаются долей в форме МП. Владелец
  расчёта пары (CodePL, CodeCFO) ведётся в `plans_calc_owner`; по рекомендации
  ТЗ §8.1 владелец статей 51/52/54 для ЦФО маркетплейсов — форма МП, у
  не-владельца строка read-only. Решение финблока (§12 п.6) меняет строку
  справочника, не код.
- **МП-11 (конфликт наименований).** Код 54 в справочнике «Расходы Code PL» —
  «Аренда помещений (стоянки)», в форме МП — «Складская логистика»; код 52 —
  «Грузоперевозки развоз и перемещение» против «Транспортная логистика».
  Расхождение с справочником по коду блокирует отправку до решения НСИ (§12 п.10).

### 28.7. Форма «Розница» (TPL-TO-RETAIL)

Экземпляр = (страна, период), 4 независимые карточки; одно реальное ЮЛ на страну
(BY→F, RU→TDMF, KZ→MFKaz, UZ→MFUz — подтверждено запросом B1). Строка = магазин
(ключ CodeCFO, KLIENT_ID опционален). Руками вводятся только месячные ячейки
плана в национальной валюте + комментарий + LFL-переопределение (финансист);
остальные 16 колонок — вывод. Индикаторы (нац. валюта): % вып., LFL тактич.,
LFM тактич., % к стратегии, итого, ожидание года (факт закрытых месяцев из
календаря + тактика остальных). Массовые операции — с предпросмотром diff и
областью применения; результат редактируемый, ячейка `manual` не перезатирается.
Детали и таблицы — `todo.md` и код `retail_*.go`.

Расчётные величины §5 после ответов финблока (27.08.2026, см. §28.9): индекс
роста утверждается на уровне СТРАНЫ (переопределения по городу/LFL/типу/магазину
остались уточнением и сопровождаются предупреждением, если странового значения
нет); ФОТ считается от продаж БЕЗ НДС фондом страны с детализацией по магазинам
в пропорции среднего факта за последний закрытый квартал; аренда на этапе 1 —
только перенос факта предыдущего месяца.

Одно осознанное отклонение от §10 ТЗ: `tp_value.metric` (`sales|payroll|rent`).
Формула ФОТ (§5) читает план продаж как ВХОД (`MIN(План_продаж; …)`), поэтому ФОТ
и продажи не могут занимать одну ячейку, хотя §10 перечисляет `payroll_share` и
`rent_carryover` среди значений `source`. Ввод (§3) и все V-проверки относятся к
`metric='sales'`.

### 28.8. Что осталось за ответами аналитика/BI

Нумерация §12 у двух ТЗ своя — в таблице указано, какого.

| Блокирует | Вопрос |
|---|---|
| включение inverse на боевой период | МП §12 п.11 — подтверждение инверсии; п.5 — кто утверждает условия |
| МП-08/МП-11 | МП §12 п.6 (владелец 51/52), п.10 (конфликт кодов 52/54) |
| источник ставки НДС | МП §12 п.4 — откуда берётся эффективная ставка на плановый период |
| курс тактики | МП §12 п.12 — какой объект Budgeting держит курс сценария «Тактика» |
| публикация МП (запись) | МП §12 п.1/2 — «Параметр» для МП, агрегат 250/480 vs детализация по площадкам |
| ABAC розницы | Розница §12 п.3 — доменный логин в `DimEmployee` для связки с RegManager |
| факт и история розницы | Розница §12 п.1 (эталон «Выручка с НДС», имена колонок факта), §11 п.4 — адрес таблицы истории плана |
| формы РФ/КЗ/УЗ | Розница §12 п.15 — разработке переданы только формы по РБ |
| все интеграции | смена скомпрометированного SQL-пароля, сервисная учётка (оба ТЗ) |
| идемпотентность приёмника | Розница §12 п.2 (остаток) — уникальный индекс по Параметр+Страна+КодЦФО+КодPL+Дата на стороне BI; до него держим DELETE+INSERT в транзакции |

Закрыто ответами финблока 27.08.2026 — Розница §12 п.2, 4, 6, 7, 8, 11, 12, 13
(§28.9).

### 28.9. Ответы финблока по Розница §12 (27.08.2026)

Отвечали Теплухин Д.Н. (процесс, публикация) и Осипович О.И. (методика расчётов).
Пять пунктов сняты как некритичные для старта, три уточнены по существу.

| № | Ответ | Что сделано |
|---|---|---|
| 2 | BYN-пару приёмника заполняет ETL на стороне BI | приложение пишет только нац. валюту (`VFORMTOLOADTAKTTARGET`); правил с `target_currency='BYN'` нет. Остаток вопроса — уникальный индекс приёмника (у BI) |
| 4 | шаг «Финансист» подтверждён, «просто переход делаем» | миграция 0039: шаг включён и переставлен МЕЖДУ 1.2 и 1.3, как в §2.1 (в сиде 0031 он стоял до 1.2) |
| 6 | исключение `CodePL NOT IN ('50','93','01')` — пока не критично | из скоупа исключено, запрос расходов не меняем |
| 7 | «да — публиковать» | правила публикации плана продаж розницы включены (миграция 0039) под параметром `ПРОДАЖИ`; база сравнения «Сумма+самовывоз» (§6a) снята со скоупа вместе с неопределёнными «индексом самовывоза» и «курсом П.М» |
| 8 | формат метки VERSION закрываем сами | `RetailPlanVersionLabel`: `TAKT-<год>-<месяц>-v<NNN>` (`TAKT-2026-07-v003`); лексикографический порядок = хронологический, поэтому `MAX([VERSION])` в `PlanHistory` возвращает последнюю итерацию |
| 11 | индекс роста задаётся на уровне страны для Розницы; для МП — по каждой крупной площадке и сводный; для опта — по странам | розница: страновой уровень — норма, переопределения остались уточнением, при отсутствии странового значения массовая операция предупреждает. Требование к МП/опту зафиксировано для их форм |
| 12 | верхняя граница ФОТ — 106 % от плана продаж на текущий месяц; удельный вес применяется к Продажам БЕЗ НДС текущего периода; вес устанавливается по каждой стране Розницы и детализируется по магазинам в пропорции ср. факта за последний квартал | `bulkPayroll` переписан (см. ниже) |
| 13 | аренда — «пока факт предыдущего месяца»; позже перестроить расчёт от условий договора там, где есть привязка | оборотная часть по умолчанию ВЫКЛЮЧЕНА (этап 1 = перенос факта), включается явным `rent_turnover=true` как переходный режим |

**ФОТ (п. 12) — итоговый расчёт на месяц:**

```
База  = MIN(Продажи_без_НДС(магазин); Порог × База_ограничения_без_НДС)
Фонд  = Σ уд.вес(магазин) × База(магазин)
Доля  = ср.факт_квартала(магазин) / Σ ср.факт_квартала
ФОТ   = Фонд × Доля
```

Три решения внутри, которые стоит знать:

- **Продажи без НДС.** Форма вводит выручку С НДС (§11), поэтому база делится на
  `(1 + ставка)`; ставка — страновая из `dir_vat` через `RateBook`, при
  недоступном справочнике работают seed-значения.
- **Порог стоит на БАЗЕ НАЧИСЛЕНИЯ, а не на сумме ФОТ** — как в формуле §5
  `MIN(План_продаж; База × Порог)`. С базой ограничения по умолчанию (план
  текущего месяца) порог не срабатывает: он предназначен для случаев, когда база
  начисления не совпадает с планом (режимы совместимости — стратегия, факт
  прошлого года, утверждённая тактика; закрытые периоды считаются тем
  алгоритмом, которым были утверждены). Порог на сумме ФОТ был бы либо мёртвым
  (сравнение выплат с оборотом), либо — с множителем удельного веса — зажал бы
  распределение в полосу ±6 % и обнулил бы саму «пропорцию ср. факта».
- **Квартал — только закрытые месяцы** из календаря (тот же инвариант, что у
  «ожидания года», §4.4). Магазины без факта за квартал (новые точки) в фонд и
  распределение не входят: им ФОТ считается напрямую от базы, ячейка несёт
  пометку. Сумма распределённого ФОТ по стране равна фонду — это проверено
  тестом.

