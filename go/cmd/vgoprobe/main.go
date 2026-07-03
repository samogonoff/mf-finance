// vgoprobe — одноразовая утилита для проверки данных Payments.report.FinDebt1/3
// (новый источник отчёта ВГО от аналитика, см. var/vgo/). Не для merge в master.
//
// Использование:
//
//	MSSQL-учётки из env (MSSQL_PREMASTER_*), SQL — из файла:
//	go run ./cmd/vgoprobe -db Payments -f query.sql
package main

import (
	"context"
	"database/sql"
	"flag"
	"fmt"
	"log"
	"net/url"
	"os"
	"strings"
	"time"

	_ "github.com/microsoft/go-mssqldb"
)

func main() {
	dbName := flag.String("db", "Payments", "database name")
	file := flag.String("f", "", "file with SQL query")
	timeoutSec := flag.Int("t", 120, "query timeout, seconds")
	flag.Parse()

	if *file == "" {
		log.Fatal("usage: vgoprobe -db Payments -f query.sql")
	}
	queryBytes, err := os.ReadFile(*file)
	if err != nil {
		log.Fatalf("read query: %v", err)
	}

	server := os.Getenv("MSSQL_PREMASTER_SERVER")
	port := os.Getenv("MSSQL_PREMASTER_PORT")
	if port == "" {
		port = "1433"
	}
	user := os.Getenv("MSSQL_PREMASTER_USER")
	password := os.Getenv("MSSQL_PREMASTER_PASSWORD")
	if server == "" || user == "" || password == "" {
		log.Fatal("env: MSSQL_PREMASTER_{SERVER,USER,PASSWORD} required")
	}

	u := &url.URL{
		Scheme: "sqlserver",
		User:   url.UserPassword(user, password),
		Host:   fmt.Sprintf("%s:%s", server, port),
	}
	q := url.Values{}
	q.Set("database", *dbName)
	q.Set("encrypt", "disable")
	u.RawQuery = q.Encode()

	db, err := sql.Open("sqlserver", u.String())
	if err != nil {
		log.Fatalf("open: %v", err)
	}
	defer db.Close()

	ctx, cancel := context.WithTimeout(context.Background(), time.Duration(*timeoutSec)*time.Second)
	defer cancel()

	// первая проверка — Ping (TDS-handshake бывает флаки, см. память infra-olap-mssql-tds-handshake)
	if err := db.PingContext(ctx); err != nil {
		log.Fatalf("ping: %v", err)
	}

	rows, err := db.QueryContext(ctx, string(queryBytes))
	if err != nil {
		log.Fatalf("query: %v", err)
	}
	defer rows.Close()

	for {
		cols, err := rows.Columns()
		if err != nil {
			log.Fatalf("columns: %v", err)
		}
		fmt.Println(strings.Join(cols, "\t"))
		vals := make([]any, len(cols))
		ptrs := make([]any, len(cols))
		for i := range vals {
			ptrs[i] = &vals[i]
		}
		n := 0
		for rows.Next() {
			if err := rows.Scan(ptrs...); err != nil {
				log.Fatalf("scan: %v", err)
			}
			parts := make([]string, len(cols))
			for i, v := range vals {
				parts[i] = render(v)
			}
			fmt.Println(strings.Join(parts, "\t"))
			n++
		}
		if err := rows.Err(); err != nil {
			log.Fatalf("rows: %v", err)
		}
		fmt.Printf("-- %d rows --\n", n)
		if !rows.NextResultSet() {
			break
		}
	}
}

func render(v any) string {
	switch x := v.(type) {
	case nil:
		return "NULL"
	case []byte:
		return string(x)
	case time.Time:
		return x.Format("2006-01-02 15:04:05")
	default:
		return fmt.Sprintf("%v", x)
	}
}
