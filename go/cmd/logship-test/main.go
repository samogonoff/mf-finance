// Command logship-test — разовая проверка канала трансляции логов в Logstash.
// Подключается к LOGSTASH_HOST:LOGSTASH_PORT по TCP и шлёт несколько показательных
// строк ровно той формы, что пишет боевой slog finance-api (кодек json_lines: одна
// JSON-запись на строку). По этим строкам ELK-команда настраивает индекс и
// маппинг полей. Запуск: cd swarm && make logship-test.
package main

import (
	"encoding/json"
	"fmt"
	"net"
	"os"
	"time"
)

func main() {
	host := os.Getenv("LOGSTASH_HOST")
	if host == "" {
		fmt.Fprintln(os.Stderr, "LOGSTASH_HOST не задан — трансляция в ELK выключена, слать нечего")
		os.Exit(1)
	}
	port := os.Getenv("LOGSTASH_PORT")
	if port == "" {
		port = "5044"
	}
	addr := net.JoinHostPort(host, port)

	conn, err := net.DialTimeout("tcp", addr, 5*time.Second)
	if err != nil {
		fmt.Fprintf(os.Stderr, "не удалось подключиться к Logstash %s: %v\n", addr, err)
		os.Exit(1)
	}
	defer conn.Close()

	hostname, _ := os.Hostname()
	now := time.Now().UTC().Format(time.RFC3339Nano)

	// Показательный набор: по одной строке от каждого рантайма кабинета —
	// Go-API, Nitro-роуты Nuxt, FastAPI раздела «Себестоимость» и песочница
	// python-analytics. Поля те же, что в боевых логах (cmd/api/logging.go,
	// nuxt/server/utils/logger.ts, python/cost/app/logship.py,
	// python/app/logship.py). test=true и marker — чтобы ELK-команда быстро
	// нашла эти записи и не приняла их за прод-трафик.
	samples := []map[string]any{
		{
			"time": now, "level": "INFO", "msg": "http request",
			"service": "finance-api", "host": hostname,
			"request_id": "logship-test-0001", "route": "GET /api/notifications",
			"status": 200, "duration_ms": 4, "bytes": 512, "user_id": 42,
			"test": true, "marker": "logship-connectivity-test",
		},
		{
			"time": now, "level": "ERROR", "msg": "http request",
			"service": "finance-api", "host": hostname,
			"request_id": "logship-test-0002", "route": "GET /api/reports/debt/report",
			"status": 500, "duration_ms": 1730, "bytes": 64,
			"error": "sample error line for mapping",
			"test":  true, "marker": "logship-connectivity-test",
		},
		{
			"time": now, "level": "ERROR", "msg": "b24 token exchange failed",
			"service": "finance-nuxt", "host": hostname,
			"status": 400, "error": "sample error line for mapping",
			"test":  true, "marker": "logship-connectivity-test",
		},
		{
			"time": now, "level": "INFO", "msg": "http request",
			"service": "finance-cost", "host": hostname,
			"request_id": "logship-test-0003", "route": "GET /api/cost/items",
			"status": 200, "duration_ms": 87, "user": "user@markformelle.by",
			"test": true, "marker": "logship-connectivity-test",
		},
		{
			"time": now, "level": "INFO", "msg": "http request",
			"service": "finance-analytics", "host": hostname,
			"request_id": "logship-test-0004", "route": "GET /healthz",
			"status": 200, "duration_ms": 1, "bytes": 34,
			"test": true, "marker": "logship-connectivity-test",
		},
	}

	enc := json.NewEncoder(conn) // Encoder дописывает '\n' — это и есть json_lines
	for i, s := range samples {
		if err := enc.Encode(s); err != nil {
			fmt.Fprintf(os.Stderr, "ошибка отправки строки %d в %s: %v\n", i+1, addr, err)
			os.Exit(1)
		}
	}

	fmt.Printf("отправлено %d тестовых строк в Logstash %s (marker=logship-connectivity-test)\n", len(samples), addr)
	for _, s := range samples {
		b, _ := json.Marshal(s)
		fmt.Printf("  %s\n", b)
	}
}
