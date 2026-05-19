# Finance Cabinet — AGENTS.md

## Quick start

```bash
cp .env.example .env          # fill B24_CLIENT_SECRET + NUXT_PUBLIC_B24_CLIENT_ID
# /etc/hosts: 127.0.0.1 finance.local api.finance.local
cd swarm && make up           # full dev contour
make logs                     # tail all services
```

| Service | Local URL | Direct port |
|---------|-----------|-------------|
| Nuxt | http://finance.local | http://localhost:3001 |
| Go API | http://api.finance.local | http://localhost:8081 |
| Postgres | `psql -h localhost -p 55432 -U finance finance` | — |
| Redis | `redis-cli -h localhost -p 63791` | — |

## Structure

```
finance/
├── go/              # Go 1.22 API — stdlib net/http, no framework
├── nuxt/            # Nuxt 3 frontend
├── python/          # Python analytics sandbox + cost module
│   └── cost/        # ⚠ Self-contained "Себестоимость" module
├── migrations/      # SQL migrations (finance DB)
└── swarm/           # Docker Swarm configs + CI
```

## Go backend (`go/`)

### Entrypoint
`go/cmd/api/main.go` — stdlib `http.ServeMux`, no Gin/Echo/Chi. Graceful shutdown via signals.

### Routes
```
POST /api/auth/b24/callback    # B24 OAuth → issue tokens
POST /api/auth/refresh          # refresh token → new access
GET  /api/auth/me              # Auth: user info
POST /api/auth/logout           # Auth: revoke access token
GET  /healthz                   # health check
```

All authenticated routes use `auth.RequireBearer(authSvc, handler)` middleware. Token in `Authorization: Bearer <uuid>` header.

### Auth flow
1. Nuxt OAuth callback → exchanges code for B24 token → POSTs user data to Go API
2. Go API upserts user by email (role default: `["ROLE_USER", "ROLE_FINANCE"]`)
3. Go issues `(access, refresh)` UUID pairs stored in Redis
4. Frontend stores tokens in `localStorage` (`auth_token`, `refresh_token`)

### Redis token schema (TTL hardcoded)
- `auth_token:{uuid}` → userID (1h)
- `refresh_token:{uuid}` → userID (30d)
- `user_tokens:{userId}` → SET of all tokens (for logout-all)

### Config
All env vars, no YAML/TOML. Defaults in `go/internal/config/config.go`:
- `HTTP_ADDR` → `:8080`
- `POSTGRES_URL` → `postgres://finance:finance@postgres:5432/finance?sslmode=disable`
- `REDIS_ADDR` → `redis:6379`
- `CORS_ORIGINS` → `*` (comma-separated in prod)

### Dependencies
`github.com/jackc/pgx/v5`, `github.com/redis/go-redis/v9`, `github.com/google/uuid`.

### Known gaps
- **Zero tests.** No `_test.go` files exist anywhere in `go/`.
- **No external router** — raw `http.ServeMux`.
- **No middleware framework** — hand-rolled CORS + auth middleware.

### Dev hot-reload
`go/.air.toml` — `make up` runs air inside container. Falls back to `go run ./cmd/api` if air unavailable.

## Nuxt frontend (`nuxt/`)

### Config
`nuxt.config.ts` extends `../python/cost/nuxt-layer` (cost module is always built in). Uses `@nuxt/icon`.

### Scripts
```bash
npm run dev    # nuxt dev
npm run build  # nuxt build
npm run start  # nuxt start
```

### Pages (file-based routing)
- `/` — Dashboard (mock KPIs/charts/tables)
- `/login` — B24 OAuth login
- `/operations`, `/reports`, `/counterparties` — mock tables
- `/analytics` — placeholder for Python sandbox
- `/account` — user profile
- `/cost` — comes from `python/cost/nuxt-layer/` (cost calculation module)

### Composables (auto-imported)
| Name | Key behavior |
|------|-------------|
| `useAuth` | Tokens in `localStorage` (`auth_token`, `refresh_token`). `fetchUser()` calls `/api/auth/me` |
| `useTheme` | Light/dark/auto. Persisted in cookie `fin.theme`. SSR-safe |
| `useScope` | Role checks: `ROLE_FINANCE_ADMIN`, `ROLE_FINANCE`, `ROLE_ANALYST` |
| `useEntity` | Multi-tenant entity selection. Cookie `fin.entity`. Mock data only |
| `useOauthState` | CSRF state cookie for B24 OAuth |

### Runtime config (`useRuntimeConfig()`)
```
public.apiBase       → http://api.finance.local
public.mpUrl         → http://mp.local
public.b24ClientId   → local.finance.xxx
public.authRedirect  → http://finance.local/api/auth/b24/callback
public.costOnly      → "1" in cost-only contour
b24ClientSecret      → (private)
```

### Auth gate
- `middleware/scope-guard.ts` — global guard, runs client-side only (token is in localStorage).
- `plugins/api-unauthorized.client.ts` — intercepts 401 on `/api/*`, clears tokens, redirects to `/login`.

### Design system
- **All CSS variables** in `assets/styles/design-system.css`. No hardcoded colors.
- Numbers: `JetBrains Mono`, `tabular-nums`, class `.num` / `.num-strong`.
- Semantic deltas: `var(--pos)` green (`#0a7f3f`), `var(--neg)` red (`#b42318`), `var(--warn)` amber.
- Accent: indigo `#4338ca` (light) / `#818cf8` (dark).
- Tables use `ag-grid-community` + `ag-grid-enterprise`.

### Known gaps
- **No tests.** No vitest, cypress, or test files.
- **No linting/formatting config.** No eslint, prettier, biome.
- **No Dockerfile in `nuxt/`** — Dockerfiles are in `swarm/nuxt/`.
- **All page data is mock** — real API endpoints not yet connected.

## Python analytics (`python/`)

### `python/app/main.py`
Placeholder stdlib HTTP server. Connects as `analytics_ro` role (read-only). Only serves `/healthz`.

### `python/cost/` — "Себестоимость" module
Self-contained: FastAPI + Nuxt layer + migrations + its own DB contour.

**FastAPI endpoints** (prefixed `/api/cost`):
- `GET /filter-options` — cascading filter values from MSSQL
- `POST /aggregated` — GROUP BY on CostHistory
- `POST /details` — detail by model/articul
- `GET /price-levels` — price level reference
- `POST /save-changes` — save single change
- `POST /save-batch` — bulk save

All save endpoints require `X-Username` header (frontend sets from `useAuth`).

### External data sources
| Source | Host | DB | Access |
|--------|------|----|--------|
| MSSQL | `10.10.6.107` | `Checks` (CostHistory ~13M rows) | RO |
| MSSQL | `10.10.6.107` | `Gpartner` (price levels) | RO |
| MSSQL (OLAP) | `10.10.6.15` | `FinSandBox` (changes receiver) | RW |

Set `COST_MOCK=1` in `python/cost/.env` to use mock data (no VPN needed).

## Infrastructure

### Dev contour (`swarm/docker-compose.dev.yml`)
Services: `postgres` + `redis` + `go-api` + `nuxt` + `python-analytics` + `nginx`.

Two networks:
- `finance_dev_network` — all services
- `data_net` — isolated postgres ↔ python-analytics only

### Cost contour (`swarm/docker-compose.cost.yml`)
Separate from main dev contour: `postgres-cost` + `python-cost` + `nuxt-cost` + `nginx-cost`.
```bash
make cost-up    # start
make cost-down  # stop
```

### Production (`swarm/docker-compose.yml`)
Docker Swarm stack with overlay network. Images from `registry.markformelle.ru`.

### GitLab CI (`swarm/ci-finance.yml`)
```
publish → migrations → deploy
```
- Branch `stage` → staging, `master` → production
- 5 images built: `go-api`, `nuxt`, `python-cost`, `redis`, `migrations`
- Deployment: `docker stack deploy` with configs from env file
- Rollback: manual job via GitLab UI
- Post-deploy verification: checks every service image tag matches `CI_COMMIT_SHORT_SHA`

### Migrations
- Location: `migrations/` (finance) + `python/cost/migrations/` (cost)
- Format: `NNNN_name.{up,down}.sql`
- Applied on first postgres container start via `docker-entrypoint-initdb.d`
- Manual re-apply: `make migrate` (finance) or `docker run migrate/migrate` (both via `swarm/migrate.dev.sh`)

### Migrations tool (`swarm/migrate.dev.sh`)
Uses `migrate/migrate:v4.17.0` via Docker. Supports: `up`, `down N`, `version`, `force N`.
```bash
./swarm/migrate.sh up          # apply all pending
./swarm/migrate.sh down 1      # rollback 1 version
```

### Environment files
- Root `.env` — B24 OAuth + DB/Redis. Required for dev.
- `python/cost/.env` — cost section: MSSQL/OLAP creds, ports, mock flag.
- Production env is injected via Docker configs (not `.env` file).

### Nginx
- Dev: `swarm/config/default_dev.conf` — routes `finance.local` → Nuxt, `api.finance.local` → Go API, `/api/cost/*` → python-cost
- Prod: similar via `swarm/config/nginx.conf`

### Critical paths for builds
- **Nuxt production build** needs `python/cost/nuxt-layer` mounted (it's imported via `extends`).
- **Go production build** uses base images from `registry.markformelle.ru/services/docker-registry/finance-go-${tag}` (not public).
