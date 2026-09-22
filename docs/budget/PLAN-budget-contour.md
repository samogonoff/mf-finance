# План: раздел «Бюджетирование» — обособленный контур (аналог `/adv/` в MP)

Статус: **проект** (Ф1 частично и Ф5 уже выполнены — см. отметки). Дата: 18.09.2026.

Что делаем: подключаем к Finance Cabinet раздел **«Бюджетирование»** как
**обособленный контур** — отдельный репозиторий, свой стек Python (FastAPI +
статика), свои права доступа. Ровно тот паттерн, по которому в MP работает
раздел «Реклама» (`/adv/`, репо `analytics-p`).

Референсы (проверены перед составлением плана):
`mp/DEVOPS-adv-integration.md`, `mp/SPEC-service-access-contract.md`,
`mp/SHELL-CONTRACT.md`, `mp/swarm/config/default{,_dev}.conf` (блоки `/adv`),
репо `analytics-p` (`app/{config,auth,main}.py`, `frontend/`, `swarm/`).

---

## 1. Зафиксированные решения

| Вопрос | Решение |
|---|---|
| Название и адрес | **«Бюджетирование»**, подпуть `/budget/` на домене кабинета |
| Ключ раздела | `budget`; роли `ROLE_BUDGET` (доступ) и `ROLE_BUDGET_ADMIN` (админ раздела); апстрим `budget-app:8080`; репозиторий `finance-budget` |
| Содержание | **Слот-каркас под аналитика**: пустой, но полностью рабочий контур (маршрут + авторизация + шапка + демо-вкладка «живые данные»). Наполнение приходит из его репозитория |
| Фронтенд | **Статика внутри FastAPI**, как в `/adv`: html/js/css без сборщика, uvicorn отдаёт `index.html`, подставляя `<base href>`. Репо автономен, Node не нужен |
| Данные | **Только чтение витрин**: ClickHouse / OLAP (MSSQL) на чтение + кэш с TTL на своей стороне. Своей БД и миграций у контура нет |
| Переключение разделов | Селектор в шапке кабинета, как в MP (`SHELL-CONTRACT.md` §2.1). Переключатель юрлица из шапки убран |

Единый реестр сервисов (в MP — `config/app_services.php`) пока не заводим:
первый контур подключаем явными правками, реестр в Go (`GET /api/app/header`)
делаем, когда появится второй контур — иначе абстракция без второго потребителя.

---

## 2. Целевая архитектура

```
браузер ──► nginx (finance.local)
              ├─ /                 → nuxt:3000              [кабинет]
              ├─ /api/             → nuxt:3000              [b24-callback, как сейчас]
              ├─ /budget/          → budget-app:8080        [статика раздела, БЕЗ гейта]
              └─ /budget/api/      → budget-app:8080        [API раздела, ЗА гейтом]
                     ▲
        auth_request │ internal location /_budget_auth
                     └─► go-api:8080  GET /api/auth/check?role=ROLE_BUDGET
                             200 → X-Auth-User-Id / -Email / -Name / -Roles
                             401 не аутентифицирован · 403 нет роли

api.finance.local  → go-api:8080    [как сейчас; сюда же уходит refresh токена]
```

Принципы (из `SPEC-service-access-contract.md`):

* **Finance — единственный провайдер идентичности.** Контур не проверяет токен
  сам и наружу порт не публикует; доверяет ТОЛЬКО заголовкам `X-Auth-*`, которые
  ставит наш nginx, перетирая всё присланное клиентом.
* **Ровно две базовые роли на раздел.** Всё, что тоньше, — внутри контура по
  ключу `X-Auth-User-Id`; ролями Finance не разрастаемся.
* **Статика не гейтится, API гейтится.** Токен лежит в `localStorage` и доступен
  только JS раздела, который дёргает API с `Authorization: Bearer`.

### 2.1 Отличия Finance от MP, которые учтены в плане

1. **Гейта нет — его надо создать.** В MP `auth_request` идёт в Symfony
   (`/api/app/auth-check`). В Finance нужен новый эндпоинт в Go; зато подзапрос
   будет обычным `proxy_pass` на `go-api:8080`, а не fastcgi.
2. **`/api/` на `finance.local` уходит в Nuxt, а не в Go** (b24-callback).
   Поэтому `/_budget_auth` проксируем на `go-api:8080` напрямую по докер-сети,
   не трогая существующую маршрутизацию.
3. **Прод-фасада nginx в репозитории нет.** Стек публикует порты
   (`nuxt:8088`, `go-api:8089`, `python-cost:8091`), фасад живёт вне репо →
   прод-гейт `/budget/` — **задача DevOps** (Ф7) и главный внешний блокер.
4. **Модель доступа cost повторять нельзя.** `python-cost` берёт пользователя из
   заголовка `X-Cost-User`, который шлёт сам браузер (`app/middleware.py`):
   подделывается тривиально. Новый контур — только через `auth_request`.
5. **Токен и origin.** Раздел обязан открываться с того же origin, что кабинет
   (`http://finance.local/budget/`). На `localhost:3001` (Nuxt мимо nginx)
   `/budget/` не существует, а токена с другого origin не видно — будет 401.

---

## 3. Фазы

### Ф1. Контракт доступа в ядре (Go) — 1–1,5 дня · **роли сделаны**

* ✅ **Сделано:** `go/internal/auth/roles.go` — константы `RoleBudgetUser`
  (`ROLE_BUDGET`) и `RoleBudgetAdmin` (`ROLE_BUDGET_ADMIN`), иерархия
  `ROLE_ADMIN ⊇ ROLE_BUDGET_ADMIN ⊇ ROLE_BUDGET`, обе роли в `Allowed`
  (иначе админ не выдаст их через `PUT /api/admin/users/{id}/roles`).
  Тесты в `roles_test.go`, включая инвариант «глобальный админ достаёт до
  `ROLE_BUDGET`» — в MP это было багом: гейт пропускал, контур отдавал 403.
* ✅ **Сделано:** эндпоинт **`GET /api/auth/check?role=ROLE_BUDGET`**
  (`internal/auth/handler.go`, тесты в `check_test.go`, контракт и конфиг
  фасада — `docs/budget/gate.md`). Неизвестная роль отдаёт `403`, а не `500`:
  наружу nginx пропускает как есть только 401 и 403. Требования, по которым
  он сделан:
  * Bearer невалиден/отсутствует → `401`; роли нет → `403`; иначе `200`;
  * заголовки ответа: `X-Auth-User-Id`, `X-Auth-Email`, `X-Auth-Name`
    (**urlencoded** — в заголовке только ASCII, а имена кириллические),
    `X-Auth-Roles` (CSV **развёрнутых** ролей);
  * `?role=` валидируется по `Allowed` — произвольную строку не принимаем;
  * тесты: 401/403/200, разворачивание иерархии, urlencode, неизвестная роль.

### Ф2. Маршрут и гейт в dev-контуре — 0,5 дня

Файлы: `swarm/config/default_dev.conf`, `swarm/docker-compose.dev.yml`,
`swarm/Makefile`, `.env.example`.

Пункт 1 (nginx) ✅ сделан — см. `swarm/config/default_dev.conf` и
`docs/budget/gate.md`; конфиг прогнан на стенде с заглушками. Пункты 2–4
(сервис `budget-app` в dev-compose, цели Makefile, переменные) остаются.

1. В `server { server_name finance.local; }`:
   * `location = /_budget_auth` — `internal;`,
     `proxy_pass http://go-api:8080/api/auth/check?role=ROLE_BUDGET;`,
     `proxy_pass_request_body off; proxy_set_header Content-Length "";`,
     проброс `Authorization`;
   * `location /budget/api/` — `auth_request /_budget_auth;`,
     `auth_request_set` четырёх `$upstream_http_x_auth_*`, затем
     `proxy_set_header X-Auth-*` (этим же перетираем клиентские),
     `rewrite ^/budget(/.*)$ $1 break;`, `proxy_pass http://$budget_upstream:8080;`;
   * `location /budget/` — без гейта, тот же rewrite и апстрим;
   * апстрим — через переменную + `resolver 127.0.0.11`, как сделано для
     `python-cost`: контур не поднят → nginx стартует и отдаёт 502.
2. `docker-compose.dev.yml`: сервис `budget-app` под профилем `budget`, сеть
   `finance_dev_network`.
3. Makefile: `budget-up` / `budget-down` / `budget-logs` (обязательно `-p finance`).
4. `.env.example`: переменные подъёма контура — с комментариями.

**Проверка фазы:** `/budget/` → 200; `/budget/api/health` без токена → 401;
с токеном без роли → 403; с `ROLE_BUDGET` → 200; подделанный клиентом
`X-Auth-User-Id` не доезжает.

### Ф3. Репозиторий `finance-budget`: бэкенд — 1,5–2 дня

```
app/
  main.py      API + отдача статики с <base href>, метка версии статики ?v=
  config.py    конфигурация из ENV (dataclass, без файлов настроек)
  auth.py      Identity из X-Auth-*; AUTH_MODE=proxy|stub
  deps.py      DI-провайдеры
  api.py       /api/* (health, whoami, данные раздела)
  logship.py   JSON-лог в stdout + async-трансляция в Logstash (service=finance-budget)
  sources/     clickhouse.py, olap.py, cache.py — read-only + TTL-кэш
frontend/      статика раздела (Ф4)
tests/         pytest
Dockerfile, docker-compose.yml, .env.example, README.md, AGENTS.md
swarm/{app/Dockerfile, ci-budget.yml, docker-compose.yml}   (Ф7)
```

Ключевое:
* `AUTH_MODE`: `proxy` (боевой, доверяет `X-Auth-*`) / `stub` (автономно у
  аналитика: фиктивный админ, кабинет не нужен). Роли — из ENV `ACCESS_ROLE` /
  `ADMIN_ROLE`, не захардкожены.
* `BASE_PATH=/budget` влияет только на `<base href>`: префикс срезает nginx.
* Недоступность источника → **200 с `available:false` и причиной**, а не 500.
  Наружу — фиксированный текст; строка подключения и сообщение драйвера в лог.
* `logship.py` — копия `python/app/logship.py` (намеренная копия, не общий пакет:
  репозитории и образы разные). Инвариант: отправка асинхронная, никогда не
  блокирует запрос.
* `/api/health` — без гейта (мониторинг), всё остальное — за `X-Auth-*`.

### Ф4. Каркас и фронт раздела — 1,5 дня

1. `frontend/index.html` + `app.css` + `app.js` — страница раздела в стиле
   кабинета. Стили — **токенами из `nuxt/assets/styles/design-system.css`**
   (цвета/радиусы/гутеры, числа в JetBrains Mono + tabular-nums, hairline
   borders). Файл с токенами — вендорная копия: не правим руками, перезаливаем
   при редизайне (в MP это `shell.css`/`shell.js`).
2. Шапка контура повторяет шапку кабинета, включая **переключатель разделов**:
   пункты те же, что в `nuxt/composables/useWorkspaces.ts` (Ф5), переход —
   жёсткая навигация `window.location.assign`.
3. `auth.js`:
   * `auth_token` из `localStorage` того же origin → `Authorization: Bearer`;
   * на 401 — один раз меняет `refresh_token` через
     `POST {API_BASE}/api/auth/refresh` и повторяет запрос; отказ → баннер
     «сессия истекла», а не молчаливое «нет данных»;
   * адрес ядра — из `<meta name="finance-api-base">`, пусто → тот же origin.
4. Демо-вкладка «Живые данные» — сквозная проверка маршрута «контур → источник»
   на одной витрине: доказывает, что доступы, кэш и деградация работают. Именно
   её аналитик потом заменяет своим содержимым.

### Ф5. Переключатель разделов в шапке кабинета — **сделано**

* ✅ `nuxt/composables/useWorkspaces.ts` — реестр разделов (Кабинет финансиста,
  Себестоимость, Бюджетирование) с видимостью по ролям и правилом навигации:
  внешний контур открывается жёсткой навигацией, внутренний — `navigateTo`.
* ✅ `nuxt/components/WorkspaceSwitcher.vue` — селектор в топбаре; один
  доступный раздел → статичная метка без выпадашки (как в MP).
* ✅ `nuxt/app.vue` — `EntitySwitcher` в шапке заменён на `WorkspaceSwitcher`.
* ✅ `nuxt/composables/useScope.ts` — scope `budget`
  (`ROLE_ADMIN`/`ROLE_BUDGET_ADMIN`/`ROLE_BUDGET`).
* ✅ `nuxt/composables/useEntity.ts` — дефолтный срез «вся группа»; ключ cookie
  версионирован (`fin.entity.v2`), чтобы у зашедших раньше не залипло юрлицо,
  которое больше негде сменить. Сам `useEntity` и фильтрация по нему остались —
  их потребители (планы, ВГО, контрагенты, дашборд) не тронуты.
* ✅ `NUXT_PUBLIC_BUDGET_ENABLED` (+ запись в `.env.example`): пока контур не
  поднят, пункт «Бюджетирование» скрыт — иначе он вёл бы только в 502.
* ⬜ Остаётся: компонент `EntitySwitcher.vue` в репозитории сохранён, но нигде
  не используется. Если выбор юрлица понадобится — ставим его в панель фильтров
  тех страниц, которым он нужен.

### Ф6. Управление доступом внутри контура — 1,5–2 дня, **можно отложить**

Админ раздела без глобального `ROLE_ADMIN` не попадает в `/admin/users` кабинета
и не может выдать людям доступ. Решение (как в MP) — экран «Пользователи
раздела» в контуре, **без своего хранилища и без своих проверок**: он только
вызывает API ядра, решение принимает Finance. Нужны три эндпоинта в Go
(гейт — `ROLE_BUDGET_ADMIN` **или** `ROLE_ADMIN`):

```
GET /api/app/services                      разделы, где вызывающий — админ
GET /api/app/services/{key}/members        [{userId,name,email,level,inherited}]
PUT /api/app/services/{key}/members/{uid}  {level: 'user'|'admin'|'none'}
GET /api/app/users?q=                      поиск среди заведённых пользователей
```

Гейт экрана — ответ `/api/app/services`: своего ключа нет → «недостаточно прав».
Поиск — только по тем, кто уже входил через B24. **Если фаза отложена**, доступ
выдаёт глобальный админ на `/admin/users` — это не блокирует запуск.

### Ф7. Прод: CI, стек, фасад — 1–2 дня + внешняя зависимость

1. **CI репозитория `finance-budget`** (`swarm/ci-budget.yml`, калька с
   `analytics-p`): `test` (pytest) → `publish` (образ `app-$reg_tag:$SHA` в
   `registry.markformelle.ru`) → `deploy` (`docker stack deploy`, env через
   `docker config`, шаблон `%%CONFIG_NAME%%`).
2. **Сеть.** В MP контур цепляется к внешней overlay-сети `mp_shared`; в Finance
   прод-стек использует внутреннюю `network`. Нужно завести внешнюю overlay-сеть
   (напр. `finance_shared`) и подключить к ней `go-api`, `nuxt` и `budget-app`,
   иначе фасад и подзапрос `auth_request` не дотянутся до контура по имени.
3. **Фасадный nginx (внешний, вне репо) — задача DevOps:** три location
   (`/_budget_auth`, `/budget/api/`, `/budget/`) с апстримом `budget-app:8080`.
   Заявку заводить в начале Ф2, а не в конце.
4. **ELK:** `LOGSTASH_HOST`/`LOGSTASH_PORT` в `.env` контура; проверить, что
   строки `service=finance-budget` доезжают в индекс `api-finance-*`.
5. Обязательные ENV прода: `AUTH_MODE=proxy`, `BASE_PATH=/budget`,
   `ACCESS_ROLE=ROLE_BUDGET`, `ADMIN_ROLE=ROLE_BUDGET_ADMIN`, `FINANCE_API_BASE`,
   доступы к источникам, `SOURCE_CACHE_TTL`. На стороне кабинета —
   `NUXT_PUBLIC_BUDGET_ENABLED=1`.

### Ф8. Документация — 0,5 дня

1. `docs/budget/DEVOPS-budget-integration.md` — как в MP: схема, таблица точек
   интеграции, запуск (с кабинетом / автономно / прод), ловушки.
2. `CLAUDE.md` — Bewegt-point «Обособленные контуры-разделы»: `/budget/` живёт в
   отдельном репозитории, гейтится `auth_request` → `GET /api/auth/check`,
   править его из этого репозитория нельзя. Плюс строки в «Частые ошибки»
   («502 на /budget/» → контур не поднят; «401 на /budget/api/» → раздел открыт
   не с origin кабинета).
3. `AGENTS.md` в репозитории контура — scope-ограничение.
4. `.env.example` обоих репозиториев — каждая переменная с комментарием.

---

## 4. Порядок и зависимости

```
Ф1 (Go: роли ✅ + /api/auth/check ✅)
  └─► Ф2 (nginx dev + compose)  ─┬─► Ф4 (фронт контура)
        Ф3 (репо + FastAPI)  ────┘
                                  └─► Ф7 (прод; заявку DevOps — сразу после Ф2)
Ф5 (шапка) ✅ — выполнена, от остальных фаз не зависит
Ф6 — в любой момент после Ф1, для запуска не обязательна
Ф8 — параллельно, закрывается вместе с Ф7
```

Осталось без Ф6: **5–7 рабочих дней**; с Ф6: **7–9**. Критический путь —
внешний: конфиг прод-фасада у DevOps.

## 5. Риски

| Риск | Что делаем |
|---|---|
| Прод-фасад вне репо — чужой релиз-цикл | Заявка DevOps в начале Ф2; до неё всё проверяется в dev |
| `X-Auth-*` можно подделать, если открыть порт контура наружу | Порт не публикуем; nginx перетирает клиентские заголовки; пустой `X-Auth-User-Id` при `AUTH_MODE=proxy` → 401 |
| Роли не развёрнуты по иерархии → 403 у глобального админа | Тест в Ф1 (уже есть) |
| Час жизни токена: раздел «отваливается» молча | `auth.js` сам рефрешит + баннер (Ф4) |
| Раздел открыт с `localhost:3001` — токена не видно, всё 401 | Явно в README/доке; селектор ведёт на `/budget/` того же origin |
| Тяжёлые запросы в OLAP кладут раздел | TTL-кэш + явные таймауты + деградация `available:false` |
| Контур начнёт писать в источники | Учётка read-only; в доке зафиксировано «только чтение» |

## 6. Приёмка (Definition of Done)

1. `http://finance.local/budget/` открывается, шапка показывает реального
   пользователя кабинета и переключатель разделов.
2. `/budget/api/*`: без токена → 401; с токеном без роли → 403; с `ROLE_BUDGET`
   → 200; у носителя `ROLE_ADMIN` (без прямой `ROLE_BUDGET`) → 200.
3. Подделка `X-Auth-User-Id` с клиента игнорируется.
4. Демо-вкладка показывает живые данные; при погашенном источнике раздел
   открывается и объясняет причину.
5. Роли `ROLE_BUDGET` / `ROLE_BUDGET_ADMIN` выдаются на `/admin/users`; пункт
   «Бюджетирование» виден только носителям и только при
   `NUXT_PUBLIC_BUDGET_ENABLED=1`.
6. Через час бездействия раздел работает (refresh), при отзыве сессии — баннер.
7. `cd go && go build ./... && go vet ./... && go test ./...` зелёные;
   `pytest -q` в репозитории контура зелёный.
8. Логи обоих рантаймов видны в ELK (`finance-api`, `finance-budget`),
   `X-Request-Id` сшивает цепочку.
9. Контур поднимается автономно (`docker compose up`, `AUTH_MODE=stub`) без
   Finance — условие того, что аналитик работает у себя.
