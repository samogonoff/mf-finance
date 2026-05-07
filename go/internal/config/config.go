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
}

func Load() Config {
	return Config{
		HTTPAddr:    env("HTTP_ADDR", ":8080"),
		PostgresURL: env("POSTGRES_URL", "postgres://finance:finance@postgres:5432/finance?sslmode=disable"),
		RedisAddr:   env("REDIS_ADDR", "redis:6379"),
		CORSOrigins: splitCSV(env("CORS_ORIGINS", "*")),
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
