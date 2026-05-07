.DEFAULT_GOAL := help

help:                  ## список команд
	@fgrep -h "##" $(MAKEFILE_LIST) | fgrep -v fgrep | sed -e 's/\\$$//' | sed -e 's/##//'

up:                    ## поднять dev-контур
	docker compose -f docker-compose.dev.yml up --build -d
	@echo "→ Nuxt:   http://finance.local       (или http://localhost:3001 без nginx)"
	@echo "→ API:    http://api.finance.local   (или http://localhost:8081 без nginx)"
	@echo "→ PG:     psql -h localhost -p 55432 -U finance finance"
	@echo "→ Redis:  redis-cli -h localhost -p 63791"

down:                  ## остановить контур
	docker compose -f docker-compose.dev.yml down

down-remove:           ## остановить + убрать orphan-ов
	docker compose -f docker-compose.dev.yml down --remove-orphans

down-volumes:          ## ⚠ остановить и удалить volume'ы (БД пропадёт)
	docker compose -f docker-compose.dev.yml down -v

logs:                  ## tail логов всех сервисов
	docker compose -f docker-compose.dev.yml logs -f --tail=200

logs-go:               ## логи go-api
	docker compose -f docker-compose.dev.yml logs -f go-api

logs-nuxt:             ## логи nuxt
	docker compose -f docker-compose.dev.yml logs -f nuxt

ps:                    ## статус контейнеров
	docker compose -f docker-compose.dev.yml ps

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
	docker compose -f docker-compose.dev.yml stop postgres
	docker compose -f docker-compose.dev.yml rm -fv postgres
	docker volume rm swarm_db_data || true
	docker compose -f docker-compose.dev.yml up -d postgres

build:                 ## пересобрать все образы без кэша
	docker compose -f docker-compose.dev.yml build --no-cache

restart-%:             ## перезапустить сервис: make restart-go-api
	docker compose -f docker-compose.dev.yml restart $*
