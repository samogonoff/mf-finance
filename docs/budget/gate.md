# Гейт раздела «Бюджетирование»

Раздел `/budget/` — обособленный контур: свой репозиторий (`finance-projects/budget`),
свой образ, свой FastAPI. Кабинет отвечает за одно — **кто пущен внутрь**.

Полная схема контура и фазы работ — `PLAN-budget-contour.md`; здесь только
гейт: ручка кабинета, роли и конфиг фасада.

Контур токен не проверяет и прав не раздаёт. Решение принимает nginx-фасад
подзапросом `auth_request` в Go-API, а контуру уходит готовая identity
заголовками `X-Auth-*`. Заголовкам от клиента контур не верит: фасад ставит свои
поверх любых присланных.

```
браузер ──/budget/api/*──► nginx ──auth_request──► go-api GET /api/auth/check?role=ROLE_BUDGET
                             │                          │ 200 + X-Auth-User-Id/Email/Name/Roles
                             └──────── proxy ───────────┴──► budget-app  (префикс /budget срезан)
```

## Контракт `GET /api/auth/check`

| Ответ | Когда | Что делает nginx |
|---|---|---|
| `200` + `X-Auth-User-Id/Email/Name/Roles` | токен жив, роль есть | пускает, переносит заголовки в проксируемый запрос |
| `401` | нет `Authorization: Bearer` или токен не жив | отдаёт клиенту 401 как есть |
| `403` | роли не хватает **или** роль неизвестна | отдаёт клиенту 403 как есть |

Других кодов ручка не отдаёт сознательно: **любой ответ, кроме 2xx/401/403,
nginx превращает в 500 у каждого пользователя раздела**. Именно так выглядел
раздел до появления этой ручки — страница открывалась, а все `/budget/api/*`
отвечали 500, потому что `auth_request` получал 404.

Имя и почта кодируются percent-encoding'ом (в заголовке допустим только ASCII,
а имена кириллические); пробел — `%20`, не `+`: контур раскодирует их
`urllib.parse.unquote`. Роли уходят уже развёрнутыми по иерархии.

## Роли

`ROLE_ADMIN ⊇ ROLE_BUDGET_ADMIN ⊇ ROLE_BUDGET` (`go/internal/auth/roles.go`).
`ROLE_BUDGET` — доступ к разделу, `ROLE_BUDGET_ADMIN` — админ раздела. Обе роли
в `Allowed`, то есть админ выдаёт их на `/admin/users` без правок кода.

Имена ролей заданы контуром (`ACCESS_ROLE`/`ADMIN_ROLE` в его `.env`) — поэтому
`ROLE_BUDGET`, а не `ROLE_BUDGET_USER`, как было бы по образцу COST/PLANS.
Переименование — только вместе с `ENV_FILE` контура и конфигом фасада.

## nginx

Dev-фасад лежит в репозитории: `swarm/config/default_dev.conf`, сервер
`finance.local`. Прод-фасад в репозиторий не входит (он на сервере) — ниже
готовый блок для него; `swarm/config/default_dev.conf` — эталон, расхождения
править в обе стороны.

```nginx
# --- Раздел «Бюджетирование» (прод) ---
resolver 127.0.0.11 ipv6=off valid=30s;
set $budget_upstream http://budget-app:8080;   # swarm-сервис в сети finance_shared
# Если фасад вне swarm-сети — через опубликованный порт: http://127.0.0.1:9014

location = /_budget_auth {
    internal;
    proxy_pass http://go-api:8080/api/auth/check?role=ROLE_BUDGET;
    proxy_pass_request_body off;
    proxy_set_header Content-Length "";
    proxy_set_header X-Original-URI $request_uri;
}

location /budget/api/ {
    auth_request /_budget_auth;
    auth_request_set $budget_user_id $upstream_http_x_auth_user_id;
    auth_request_set $budget_email   $upstream_http_x_auth_email;
    auth_request_set $budget_name    $upstream_http_x_auth_name;
    auth_request_set $budget_roles   $upstream_http_x_auth_roles;

    proxy_set_header X-Auth-User-Id $budget_user_id;
    proxy_set_header X-Auth-Email   $budget_email;
    proxy_set_header X-Auth-Name    $budget_name;
    proxy_set_header X-Auth-Roles   $budget_roles;
    proxy_set_header X-Request-Id   $request_id;

    rewrite ^/budget/(.*)$ /$1 break;
    proxy_pass $budget_upstream;
    proxy_http_version 1.1;
    proxy_set_header Host $host;
    proxy_set_header X-Real-IP $remote_addr;
    proxy_set_header X-Forwarded-For $proxy_add_x_forwarded_for;
    proxy_set_header X-Forwarded-Proto $scheme;
}

# Статика раздела не гейтится: токен видит только JS, который дёргает API.
location /budget/ {
    rewrite ^/budget/(.*)$ /$1 break;
    proxy_pass $budget_upstream;
    proxy_http_version 1.1;
    proxy_set_header Host $host;
    proxy_set_header X-Real-IP $remote_addr;
    proxy_set_header X-Forwarded-For $proxy_add_x_forwarded_for;
    proxy_set_header X-Forwarded-Proto $scheme;
    proxy_set_header X-Request-Id $request_id;
}

location = /budget {
    absolute_redirect off;
    return 301 /budget/;
}
```

Почему именно так:

* `location /budget/api/` описан **до** общего `/budget/` по смыслу, но порядок
  не важен — nginx берёт самый длинный префикс. Важно, что API гейтится, а
  статика нет;
* `rewrite … break` + `proxy_pass $var` — из-за переменной в `proxy_pass` nginx
  не подменяет URI сам, префикс приходится срезать явно;
* клиентские `?role=` в подзапрос не протекают: `proxy_pass` с собственной
  query-строкой отдаёт в go-api ровно `role=ROLE_BUDGET` (проверено);
* пустая переменная в `proxy_set_header` = заголовок не уходит вовсе, поэтому
  подделать `X-Auth-*` клиенту нечем;
* `absolute_redirect off` — TLS терминируется выше, иначе редирект с `/budget`
  уводил бы на `http://`.

## Проверка

```bash
TOKEN=<access-токен из localStorage кабинета>

# 1. Гейт отвечает и отдаёт identity
curl -i -H "Authorization: Bearer $TOKEN" \
     https://api-finance.markformelle.ru/api/auth/check?role=ROLE_BUDGET
#    200 + X-Auth-User-Id / X-Auth-Roles

# 2. Без токена — 401, с ролью-опечаткой — 403 (а не 500)
curl -s -o /dev/null -w '%{http_code}\n' https://api-finance.markformelle.ru/api/auth/check?role=ROLE_BUDGET
curl -s -o /dev/null -w '%{http_code}\n' -H "Authorization: Bearer $TOKEN" \
     "https://api-finance.markformelle.ru/api/auth/check?role=ROLE_BUDGE"

# 3. Раздел целиком
curl -s -o /dev/null -w '%{http_code}\n' https://finance.markformelle.ru/budget/            # 200
curl -s -o /dev/null -w '%{http_code}\n' -H "Authorization: Bearer $TOKEN" \
     https://finance.markformelle.ru/budget/api/whoami                                      # 200
```

Если `/budget/api/*` отвечает 500 — смотреть не контур, а `auth_request`:
`grep "auth request" /var/log/nginx/*error.log`.

### Обязательная проверка: фасад не протекает

Четыре `proxy_set_header X-Auth-*` в `location /budget/api/` нужны **и** чтобы
донести identity, **и** чтобы затереть присланное клиентом. Поставить только
`X-Auth-User-Id` — распространённая ошибка с двумя последствиями сразу: роли до
контура не доезжают (раздел отвечает 403 всем, даже глобальному админу), а
клиент может прислать их сам и объявить себя админом раздела.

Проверяется одним запросом — подделываем заголовки при валидном токене:

```bash
curl -s -H "Authorization: Bearer $TOKEN" \
     -H 'X-Auth-Roles: ROLE_BUDGET,ROLE_BUDGET_ADMIN' \
     -H 'X-Auth-User-Id: 999999' -H 'X-Auth-Email: fake@example.com' \
     https://finance.markformelle.ru/budget/api/whoami
```

Правильный фасад ответит ровно тем, что отдал гейт: свой `userId`, своя почта,
свои роли. Если в ответе приехали `999999`, `fake@example.com` или
`"isAdmin": true` — фасад перетирает не все заголовки, чинить до того, как в
разделе появятся данные.

## Что ещё нужно контуру

`FINANCE_API_BASE` в `ENV_FILE` раздела — публичный адрес Go-API,
`https://api-finance.markformelle.ru` (тот же, что `NUXT_PUBLIC_API_BASE` у SPA).
Пустым оставлять нельзя: со страницы раздела продлевается access-токен
(`POST /api/auth/refresh`), а на домене кабинета весь `/api/*` уходит в Nuxt, где
этой ручки нет.

Пункт раздела в шапке кабинета уже есть (`nuxt/composables/useWorkspaces.ts`,
`WorkspaceSwitcher.vue`), но скрыт, пока не задан `NUXT_PUBLIC_BUDGET_ENABLED=1` —
его включаем последним, когда гейт и контур на проде уже отвечают. Список
разделов продублирован в `frontend/shell.js` контура: правим оба места.
Выдача ролей пользователям — `/admin/users`.
