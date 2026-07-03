package etl

import (
	"database/sql"

	"github.com/jackc/pgx/v5/pgxpool"
)

// Deps — общие зависимости ETL-заливок (после выпила старых потоков остался
// только FinDebt, см. bootstrap_findebt.go). PG опционален: у потока FinDebt
// нет per-company чекпоинтов (watermark берётся из CH).
type Deps struct {
	MSSQL  *sql.DB
	PG     *pgxpool.Pool
	CHURL  string // http://host:port
	CHUser string
	CHPass string
}

// accRootSQL — корень счёта (LEFT до первой точки) для MSSQL-выражений extract'а.
// Дублируется здесь (пакет etl не импортирует reports/debt — иначе цикл импорта).
func accRootSQL(col string) string {
	return "LEFT(" + col + ", CHARINDEX('.', " + col + " + '.') - 1)"
}
