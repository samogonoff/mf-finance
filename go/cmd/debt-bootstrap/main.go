// debt-bootstrap — CLI-обёртка над пакетом internal/etl: тянет полную историю
// одного юрлица из Premaster1C в ClickHouse fact_premaster.
//
// Вся реальная логика живёт в internal/etl/bootstrap.go (та же, что использует
// admin-эндпоинт POST /api/admin/etl/debt/bootstrap). Здесь только парсинг env
// и циклический вызов RunBootstrap для списка company_id.
//
// ============================================================================
// Локальный dev-запуск:
//   docker exec \
//     -e BOOTSTRAP_COMPANIES="5031159833" \
//     -e BOOTSTRAP_BATCH=10000 \
//     swarm-go-api-1 sh -c 'cd /var/www/finance/go && go run ./cmd/debt-bootstrap'
//
// ============================================================================
// Прод-запуск (Swarm-stack, бинарник /debt-bootstrap внутри go-api image):
//   docker service ps --no-trunc --filter desired-state=running --format '{{.Name}}' finance_go-api
//   docker exec \
//     -e BOOTSTRAP_COMPANIES="6950135110" \
//     -e BOOTSTRAP_BATCH=10000 \
//     finance_go-api.<TASK_HASH> /debt-bootstrap
//
// Мониторинг прогресса параллельно:
//   curl -H "Authorization: Bearer $ADMIN_TOKEN" \
//        https://api-finance.markformelle.ru/api/admin/etl/debt/status | jq
//
// ============================================================================
// ENV:
//   BOOTSTRAP_COMPANIES   — список ИНН через запятую (обязательно).
//   BOOTSTRAP_BATCH       — размер батча, дефолт 5000.
//   MSSQL_PREMASTER_*     — коннект к OLAP.
//   POSTGRES_URL          — для checkpoint.
//   CLICKHOUSE_HTTP_URL   — дефолт http://clickhouse:8123.
//   CLICKHOUSE_USER, CLICKHOUSE_PASSWORD
//   MSSQL_ENCRYPT         — disable|true|false (дефолт disable).
package main

import (
	"context"
	"database/sql"
	"fmt"
	"log"
	"os"
	"strconv"
	"strings"

	"github.com/jackc/pgx/v5/pgxpool"
	_ "github.com/microsoft/go-mssqldb"

	"github.com/company/finance-api/internal/etl"
)

func mustEnv(k string) string {
	v := os.Getenv(k)
	if v == "" {
		log.Fatalf("env %s required", k)
	}
	return v
}

func envOr(k, def string) string {
	if v := os.Getenv(k); v != "" {
		return v
	}
	return def
}

func main() {
	companiesRaw := mustEnv("BOOTSTRAP_COMPANIES")
	batchSize, _ := strconv.Atoi(envOr("BOOTSTRAP_BATCH", "5000"))
	if batchSize <= 0 {
		batchSize = 5000
	}

	ctx := context.Background()

	mssqlDB := openMSSQL()
	defer mssqlDB.Close()
	pg, err := pgxpool.New(ctx, mustEnv("POSTGRES_URL"))
	if err != nil {
		log.Fatalf("pg open: %v", err)
	}
	defer pg.Close()

	deps := etl.Deps{
		MSSQL:  mssqlDB,
		PG:     pg,
		CHURL:  envOr("CLICKHOUSE_HTTP_URL", "http://clickhouse:8123"),
		CHUser: envOr("CLICKHOUSE_USER", "finance"),
		CHPass: envOr("CLICKHOUSE_PASSWORD", "finance"),
	}

	for _, inn := range strings.Split(companiesRaw, ",") {
		inn = strings.TrimSpace(inn)
		if inn == "" {
			continue
		}
		if _, err := etl.RunBootstrap(ctx, deps, etl.BootstrapOpts{
			CompanyID:   inn,
			BatchSize:   batchSize,
			TriggeredBy: "manual",
		}); err != nil {
			log.Fatalf("company %s: %v", inn, err)
		}
	}
	log.Println("bootstrap done")
}

func openMSSQL() *sql.DB {
	host := mustEnv("MSSQL_PREMASTER_SERVER")
	port := envOr("MSSQL_PREMASTER_PORT", "1433")
	db := mustEnv("MSSQL_PREMASTER_DB")
	user := mustEnv("MSSQL_PREMASTER_USER")
	pass := mustEnv("MSSQL_PREMASTER_PASSWORD")
	if !strings.Contains(host, ":") {
		host = host + ":" + port
	}
	encrypt := envOr("MSSQL_ENCRYPT", "disable")
	dsn := fmt.Sprintf(
		"sqlserver://%s:%s@%s?database=%s&encrypt=%s&trustservercertificate=true&app+name=debt-bootstrap",
		user, pass, host, db, encrypt,
	)
	conn, err := sql.Open("sqlserver", dsn)
	if err != nil {
		log.Fatalf("mssql open: %v", err)
	}
	conn.SetMaxOpenConns(2)
	if err := conn.Ping(); err != nil {
		log.Fatalf("mssql ping: %v", err)
	}
	return conn
}
