.DEFAULT_GOAL := help

help:                  ## список команд
	@fgrep -h "##" $(MAKEFILE_LIST) | fgrep -v fgrep | sed -e 's/\\$$//' | sed -e 's/##//'

up:                    ## поднять dev-контур
	docker compose --env-file ../.env -f docker-compose.dev.yml up --build -d
	@echo "→ Nuxt:   http://finance.local       (или http://localhost:3001 без nginx)"
	@echo "→ API:    http://api.finance.local   (или http://localhost:8081 без nginx)"
	@echo "→ PG:     psql -h localhost -p 55432 -U finance finance"
	@echo "→ Redis:  redis-cli -h localhost -p 63791"

down:                  ## остановить контур
	docker compose --env-file ../.env -f docker-compose.dev.yml down

down-remove:           ## остановить + убрать orphan-ов
	docker compose --env-file ../.env -f docker-compose.dev.yml down --remove-orphans

down-volumes:          ## ⚠ остановить и удалить volume'ы (БД пропадёт)
	docker compose --env-file ../.env -f docker-compose.dev.yml down -v

logs:                  ## tail логов всех сервисов
	docker compose --env-file ../.env -f docker-compose.dev.yml logs -f --tail=200

logs-go:               ## логи go-api
	docker compose --env-file ../.env -f docker-compose.dev.yml logs -f go-api

logs-nuxt:             ## логи nuxt
	docker compose --env-file ../.env -f docker-compose.dev.yml logs -f nuxt

ps:                    ## статус контейнеров
	docker compose --env-file ../.env -f docker-compose.dev.yml ps

console-go:            ## shell в go-api
	docker exec -it swarm-go-api-1 sh

console-nuxt:          ## shell в nuxt
	docker exec -it swarm-nuxt-1 sh

console-py:            ## shell в python-analytics
	docker exec -it swarm-python-analytics-1 sh

psql:                  ## psql в БД finance
	docker exec -it swarm-postgres-1 psql -U finance finance

migrate:               ## применить миграции (заново — для существующего volume)
	docker exec -it swarm-postgres-1 sh -c \
	  'for f in /docker-entrypoint-initdb.d/migrations/*.up.sql; do echo "applying $$f"; psql -U finance -d finance -f "$$f"; done'

reset-db:              ## ⚠ полностью пересоздать БД (drop volume + миграции)
	docker compose --env-file ../.env -f docker-compose.dev.yml stop postgres
	docker compose --env-file ../.env -f docker-compose.dev.yml rm -fv postgres
	docker volume rm swarm_db_data || true
	docker compose --env-file ../.env -f docker-compose.dev.yml up -d postgres

build:                 ## пересобрать все образы без кэша
	docker compose --env-file ../.env -f docker-compose.dev.yml build --no-cache

restart-%:             ## перезапустить сервис: make restart-go-api
	docker compose --env-file ../.env -f docker-compose.dev.yml restart $*

logship-test:          ## послать тестовые строки в Logstash (настройка индекса ELK)
	docker exec \
	  -e LOGSTASH_HOST="$$(grep -E '^LOGSTASH_HOST=' ../.env | cut -d= -f2-)" \
	  -e LOGSTASH_PORT="$$(grep -E '^LOGSTASH_PORT=' ../.env | cut -d= -f2-)" \
	  swarm-go-api-1 go run ./cmd/logship-test

# === Раздел «Себестоимость» — изолированный контур ============================
# Запускается отдельно от основного dev-контура: своя БД, свой env, свой nginx.
# Конфиг разработчика — python/cost/.env (создаётся из .env.example).
COST_COMPOSE := docker compose --env-file ../python/cost/.env -f docker-compose.cost.yml

cost-up:               ## поднять контур раздела «Себестоимость»
	@test -f ../python/cost/.env || cp ../python/cost/.env.example ../python/cost/.env
	$(COST_COMPOSE) up --build -d
	@echo "→ UI:    http://localhost:$${COST_NGINX_PORT:-8088}/cost"
	@echo "→ API:   http://localhost:$${COST_NGINX_PORT:-8088}/api/cost/items"
	@echo "→ PG:    psql -h localhost -p $${COST_POSTGRES_PORT:-55433} -U cost cost"

cost-down:             ## остановить контур
	$(COST_COMPOSE) down

cost-logs:             ## tail логов контура
	$(COST_COMPOSE) logs -f --tail=200

cost-ps:               ## статус контейнеров контура
	$(COST_COMPOSE) ps

cost-psql:             ## psql в БД cost
	docker exec -it swarm-postgres-cost-1 psql -U cost cost

cost-console-py:       ## shell в python-cost
	docker exec -it swarm-python-cost-1 sh

cost-reset-db:         ## ⚠ полностью пересоздать БД раздела
	$(COST_COMPOSE) stop postgres-cost
	$(COST_COMPOSE) rm -fv postgres-cost
	docker volume rm swarm_cost_db_data || true
	$(COST_COMPOSE) up -d postgres-cost

# === Superset — BI-контур ======================================================
# Изолирован от остальных контуров: своя метабаза, свой redis, свои порты.
# До источников ходит через host.docker.internal, поэтому поднимается независимо
# от `make up` / `make cost-up` (но данные будут только у тех БД, что запущены).
#
# Дашборды живут в метабазе, а в git — в swarm/superset/assets/ как YAML.
# Цикл разработки: правим в UI → superset-assets-export → git diff → commit.
SUPERSET_COMPOSE := docker compose --env-file ../.env -f docker-compose.superset.yml

superset-up:           ## поднять BI-контур (Superset + MCP)
	$(SUPERSET_COMPOSE) up --build -d
	@echo "→ UI:    http://localhost:$${SUPERSET_PORT:-8093}  (admin / $${SUPERSET_ADMIN_PASSWORD:-admin})"
	@echo "→ MCP:   http://localhost:$${MCP_SERVICE_PORT:-5008}/mcp"
	@echo "→ PG:    psql -h localhost -p $${SUPERSET_POSTGRES_PORT:-55434} -U superset superset"
	@echo "→ дальше: make superset-assets-import  (накатить подключения и дашборды из git)"

superset-down:         ## остановить BI-контур
	$(SUPERSET_COMPOSE) down

superset-logs:         ## tail логов BI-контура
	$(SUPERSET_COMPOSE) logs -f --tail=200

superset-logs-mcp:     ## логи только MCP-сервера
	$(SUPERSET_COMPOSE) logs -f --tail=200 superset-mcp

superset-logs-worker:  ## логи Celery-воркера (async-запросы)
	$(SUPERSET_COMPOSE) logs -f --tail=200 superset-worker

superset-ps:           ## статус контейнеров BI-контура
	$(SUPERSET_COMPOSE) ps

superset-psql:         ## psql в метабазу Superset
	$(SUPERSET_COMPOSE) exec postgres-superset psql -U superset superset

superset-console:      ## shell в контейнер Superset
	docker exec -it swarm-superset-1 bash

superset-assets-import: ## git → Superset: накатить подключения и дашборды из assets/
	docker exec swarm-superset-1 python /app/assets_sync.py import

superset-assets-export: ## Superset → git: выгрузить дашборды/чарты/датасеты в assets/
	docker exec swarm-superset-1 python /app/assets_sync.py export
	@echo "→ теперь: git -C .. diff -- swarm/superset/assets/"

superset-reset-db:     ## ⚠ снести метабазу Superset (дашборды пропадут, git-ассеты нет)
	$(SUPERSET_COMPOSE) down
	docker volume rm swarm-superset_superset_db_data || true
	$(SUPERSET_COMPOSE) up --build -d
