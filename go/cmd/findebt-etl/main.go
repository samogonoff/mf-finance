// findebt-etl — CLI-заливка потока FinDebt (Payments.report.FinDebt1/3) в
// ClickHouse finance.fact_findebt/_docs. Вся логика — в internal/etl/
// bootstrap_findebt.go.
//
// ============================================================================
// Локальный dev-запуск (полная заливка всей истории):
//
//	docker exec \
//	  -e MODE=bootstrap -e FINDEBT_MIN_DATE=2021-01-01 \
//	  swarm-go-api-1 sh -c 'cd /var/www/finance/go && go run ./cmd/findebt-etl'
//
// Инкремент (докат новых снэпшотов; на cron):
//
//	docker exec -e MODE=incremental swarm-go-api-1 /findebt-etl
//
// ============================================================================
// ENV:
//
//	MODE                  — bootstrap | incremental (дефолт bootstrap).
//	FINDEBT_MIN_DATE      — нижняя граница снэпшотов для bootstrap, YYYY-MM-DD (дефолт 2021-01-01).
//	FINDEBT_BATCH         — размер батча, дефолт 5000.
//	MSSQL_PAYMENTS_DB     — БД с вьюхами FinDebt (дефолт Payments).
//	MSSQL_FINDEBT_SCHEMA  — схема (дефолт report).
//	MSSQL_FINDEBT1_TABLE / MSSQL_FINDEBT3_TABLE — имена вьюх (дефолт FinDebt1/FinDebt3).
//	MSSQL_PREMASTER_*     — коннект к OLAP (тот же сервер).
//	CLICKHOUSE_HTTP_URL   — дефолт http://clickhouse:8123.
//	CLICKHOUSE_USER, CLICKHOUSE_PASSWORD — дефолт finance/finance.
//	MSSQL_ENCRYPT         — disable|true|false (дефолт disable).
package main

import (
	"context"
	"database/sql"
	"fmt"
	"log"
	"os"
	"strconv"
	"strings"

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
	mode := strings.ToLower(envOr("MODE", "bootstrap"))
	batch, _ := strconv.Atoi(envOr("FINDEBT_BATCH", "5000"))
	if batch <= 0 {
		batch = 5000
	}

	db := envOr("MSSQL_PAYMENTS_DB", "Payments")
	schema := envOr("MSSQL_FINDEBT_SCHEMA", "report")
	fin1 := envOr("MSSQL_FINDEBT1_TABLE", "FinDebt1")
	fin3 := envOr("MSSQL_FINDEBT3_TABLE", "FinDebt3")
	tables := etl.FinDebtTables{
		Fin1FQN: "[" + db + "].[" + schema + "].[" + fin1 + "]",
		Fin3FQN: "[" + db + "].[" + schema + "].[" + fin3 + "]",
	}

	ctx := context.Background()
	mssqlDB := openMSSQL()
	defer mssqlDB.Close()

	// PG не нужен: у потока FinDebt нет per-company чекпоинтов (watermark — из CH).
	deps := etl.Deps{
		MSSQL:  mssqlDB,
		CHURL:  envOr("CLICKHOUSE_HTTP_URL", "http://clickhouse:8123"),
		CHUser: envOr("CLICKHOUSE_USER", "finance"),
		CHPass: envOr("CLICKHOUSE_PASSWORD", "finance"),
	}
	opts := etl.FinDebtOpts{
		Tables:      tables,
		MinDate:     envOr("FINDEBT_MIN_DATE", "2021-01-01"),
		BatchSize:   batch,
		TriggeredBy: "cli",
	}

	var (
		n   int64
		err error
	)
	switch mode {
	case "incremental":
		n, err = etl.RunIncrementalFinDebt(ctx, deps, opts)
	default:
		n, err = etl.RunBootstrapFinDebt(ctx, deps, opts)
	}
	if err != nil {
		log.Fatalf("findebt-etl (mode=%s): %v", mode, err)
	}
	log.Printf("findebt-etl done (mode=%s): rows=%d", mode, n)
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
		"sqlserver://%s:%s@%s?database=%s&encrypt=%s&trustservercertificate=true&app+name=findebt-etl",
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
