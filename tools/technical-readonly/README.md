# Finance technical read-only access

Этот контур предназначен для фоновых API и browser-проверок Finance. Он принимает только bearer с префиксом `finance_ro_`, сопоставляет SHA-256 digest с публичным реестром и разрешает только проверенные `GET`/`HEAD` маршруты из [`go/internal/auth/technical_readonly_routes.json`](../../go/internal/auth/technical_readonly_routes.json). `POST`, `PUT`, `PATCH`, `DELETE`, неизвестные маршруты, path aliases, duplicate query keys, тела запроса и redirects отклоняются до CORS, аудита и handler.

Production allowlist сейчас содержит identity read и read-only методы отчёта задолженности ВГО:

- `/api/auth/me` — `auth.Me` (проверка временной identity и ролей);

- `/api/reports/debt/filter-options` — `debt.FilterOptions`;
- `/api/reports/debt/report` — `debt.Report`;
- `/api/reports/debt/drilldown` — `debt.Drilldown`;
- `/api/reports/debt/saved-filters` — `debt.ListSavedFilters`.

Для post-deploy smoke формы расходов ЦФО v2 также доступны только через
проверенные GET-маршруты:

- `/api/plans/cfo-exp/{cardId}/form` — текущее сохранённое состояние формы;
- `/api/plans/cfo-exp/{cardId}/validate` — проверки сохранённых строк;
- `/api/plans/cfo-exp/{cardId}/export` — экспорт сохранённых строк;
- `/api/plans/cfo-exp/{cardId}/registry/{sheet}` — ручной реестр и его расчёт.

Эти обработчики используют read-only путь без ленивой синхронизации источника.
Новый GET-маршрут можно добавлять в manifest только вместе с явной проверкой,
что handler и все вызываемые методы не создают и не обновляют данные. Обычные
GET-обработчики с lazy hydration, cache warm или иным побочным эффектом в
technical read-only manifest добавлять нельзя.
Для бинарного экспорта `check.py` сохраняет только статус, тип, размер и хеш
ответа; содержимое файла в результаты проверки не попадает.

Остальные Finance UI GET-маршруты требуют отдельного review каждого handler и
его вызываемых методов перед добавлением в manifest.

Каждая запись manifest связывает точный ServeMux pattern с именем handler и path regexp. Добавление маршрута требует отдельного review вызываемых методов: GET сам по себе не является разрешением на запись, refresh, sync, lazy creation или запуск фоновой работы.

## Выпуск и отзыв

Секрет создаётся локально после review. В Git попадает только digest, имя технического доступа, email существующего пользователя и срок действия; bearer сохраняется в приватном файле с mode `0600` вне checkout:

```sh
python3 tools/technical-readonly/issue.py \
  --subject-email reader@example.test \
  --name finance-browser-check \
  --origin https://finance-api.example.test \
  --secret-file /secure/finance/finance-browser-check.json
```

`--origin` должен быть точным HTTPS origin без path, query или userinfo. Срок по умолчанию 30 дней, допустимо 1–90. Issuer не обращается к production API, БД или Redis. После проверки diff реестра изменения активируются обычной сборкой и развёртыванием приложения. Живой bearer никогда не коммитится и не передаётся через URL, argv, логи, Jira/Slack или пользовательский браузер.

Для отзыва удалите запись по имени и задеплойте новую сборку:

```sh
python3 tools/technical-readonly/revoke.py --name finance-browser-check
```

При ротации сначала выпустите новый секрет с новым `--name`, затем отзовите старый. Приватные файлы issuer/revoker не удаляют автоматически.

## API и browser-проверки

Проверка использует отдельный headless-процесс и bearer header. Она не следует redirects, закрепляет origin из credential-файла, нормализует query по manifest и редактирует bearer из ответа:

```sh
python3 tools/technical-readonly/check.py \
  --credential-file /secure/finance/finance-browser-check.json \
  --path '/api/reports/debt/report?date_from=2026-01-01&date_to=2026-01-31'
```

Для браузерного smoke-теста используйте отдельный headless профиль и передайте этот bearer в API request header (или изолированное localStorage только внутри этого профиля). Не используйте пользовательский профиль, cookie или обычную browser-сессию. Ответы/результаты сохраняются только в приватные файлы.

`subject_email` разрешается только через `SELECT` существующего незаблокированного пользователя, у которого уже есть `ROLE_FINANCE_ADMIN` для debt routes. Его роли и identity копируются в request-local объект на один запрос; upsert, login hook, refresh, Redis token и изменение БД не выполняются.

Проверки:

```sh
go test ./internal/auth ./internal/reports/debt ./cmd/api
PYTHONDONTWRITEBYTECODE=1 python3 -m unittest discover -s tools/technical-readonly -p 'test_*.py'
```
