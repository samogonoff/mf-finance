package config

import (
	"os"
	"strings"
)

type Config struct {
	HTTPAddr    string
	PostgresURL string
	RedisAddr   string
	CORSOrigins []string

	// Premaster1C — MSSQL-витрина для отчёта «Задолженность ВГО».
	// На OLAP-сервере 10.10.6.15 это ТАБЛИЦА [FinDWH].[dbo].[Premaster1C]
	// (а не отдельная БД). PremasterDatabase = "FinDWH", PremasterTable = "Premaster1C".
	// Имена вынесены в env, чтобы можно было быстро переключиться на снэпшот
	// (например, [FinDWH].[dbo].[Premaster1C_20260514]) без правки кода.
	PremasterServer        string
	PremasterPort          string
	PremasterDatabase      string
	PremasterSchema        string
	PremasterTable         string
	PremasterObjectsTable  string
	PremasterUser          string
	PremasterPassword      string
	DebtMock               bool

	// DEBT_BACKEND — какой источник дёргает отчёт «Задолженность ВГО».
	//   "mssql" (default) → repo_premaster.go, ходит в Premaster1C напрямую.
	//   "ch"              → repo_clickhouse.go, ходит в локальный CH-снэпшот.
	// Drilldown в ch-режиме пока не реализован — falls back to mssql.
	DebtBackend       string
	ClickHouseHTTPURL string
	ClickHouseUser    string
	ClickHousePass    string
}

func Load() Config {
	return Config{
		HTTPAddr:    env("HTTP_ADDR", ":8080"),
		PostgresURL: env("POSTGRES_URL", "postgres://finance:finance@postgres:5432/finance?sslmode=disable"),
		RedisAddr:   env("REDIS_ADDR", "redis:6379"),
		CORSOrigins: splitCSV(env("CORS_ORIGINS", "*")),

		PremasterServer:       env("MSSQL_PREMASTER_SERVER", ""),
		PremasterPort:         env("MSSQL_PREMASTER_PORT", "1433"),
		PremasterDatabase:     env("MSSQL_PREMASTER_DB", "FinDWH"),
		PremasterSchema:       env("MSSQL_PREMASTER_SCHEMA", "dbo"),
		PremasterTable:        env("MSSQL_PREMASTER_TABLE", "Premaster1C"),
		PremasterObjectsTable: env("MSSQL_PREMASTER_OBJECTS_TABLE", "Objects"),
		PremasterUser:         env("MSSQL_PREMASTER_USER", ""),
		PremasterPassword:     env("MSSQL_PREMASTER_PASSWORD", ""),
		DebtMock:              env("DEBT_MOCK", "0") == "1",

		DebtBackend:       strings.ToLower(env("DEBT_BACKEND", "mssql")),
		ClickHouseHTTPURL: env("CLICKHOUSE_HTTP_URL", "http://clickhouse:8123"),
		ClickHouseUser:    env("CLICKHOUSE_USER", "finance"),
		ClickHousePass:    env("CLICKHOUSE_PASSWORD", "finance"),
	}
}

func env(k, def string) string {
	if v := os.Getenv(k); v != "" {
		return v
	}
	return def
}

func splitCSV(s string) []string {
	parts := strings.Split(s, ",")
	out := make([]string, 0, len(parts))
	for _, p := range parts {
		if p = strings.TrimSpace(p); p != "" {
			out = append(out, p)
		}
	}
	return out
}
