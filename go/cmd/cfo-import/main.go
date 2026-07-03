// cfo-import — загрузка справочника ЦФО из «Справочник ЦФО и ЦЗ.xlsx» в dir_cfo.
// Использование: go run ./cmd/cfo-import <путь.xlsx>  (POSTGRES_URL из env/дефолт).
package main

import (
	"context"
	"log"
	"os"
	"time"

	"github.com/company/finance-api/internal/plans"
	"github.com/jackc/pgx/v5/pgxpool"
)

func main() {
	if len(os.Args) < 2 {
		log.Fatal("usage: cfo-import <file.xlsx>")
	}
	data, err := os.ReadFile(os.Args[1])
	if err != nil {
		log.Fatal(err)
	}
	dsn := os.Getenv("POSTGRES_URL")
	if dsn == "" {
		dsn = "postgres://finance:finance@localhost:55432/finance?sslmode=disable"
	}
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	pool, err := pgxpool.New(ctx, dsn)
	if err != nil {
		log.Fatal(err)
	}
	defer pool.Close()
	n, err := plans.ImportCFO(ctx, pool, data)
	if err != nil {
		log.Fatal(err)
	}
	log.Printf("dir_cfo: загружено %d строк", n)
}
