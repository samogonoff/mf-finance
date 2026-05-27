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
	// В dev держится в .env пустым; реальные значения берутся из var/original/.env.
	// По умолчанию указывает на СНЭПШОТ Premaster1C_20260514 — на живую витрину
	// `Premaster1C` ходить не следует, пока не выясним схему по снэпшоту.
	PremasterServer   string
	PremasterDatabase string
	PremasterUser     string
	PremasterPassword string
	DebtMock          bool
}

func Load() Config {
	return Config{
		HTTPAddr:    env("HTTP_ADDR", ":8080"),
		PostgresURL: env("POSTGRES_URL", "postgres://finance:finance@postgres:5432/finance?sslmode=disable"),
		RedisAddr:   env("REDIS_ADDR", "redis:6379"),
		CORSOrigins: splitCSV(env("CORS_ORIGINS", "*")),

		PremasterServer:   env("MSSQL_PREMASTER_SERVER", ""),
		PremasterDatabase: env("MSSQL_PREMASTER_DB", "Premaster1C_20260514"),
		PremasterUser:     env("MSSQL_PREMASTER_USER", ""),
		PremasterPassword: env("MSSQL_PREMASTER_PASSWORD", ""),
		DebtMock:          env("DEBT_MOCK", "0") == "1",
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
