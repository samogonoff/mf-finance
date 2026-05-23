// mssql-probe — одноразовая утилита для исследования структуры MSSQL-БД,
// конкретно — снэпшота Premaster1C_20260514 (см. debt-модуль). Не для merge
// в master: используется через `go run ./cmd/mssql-probe` для schema-probe.
//
// Читает MSSQL_PREMASTER_{SERVER,DB,USER,PASSWORD} из env. Делает:
//   1. Список таблиц с приблизительной row_count через sys.dm_db_partition_stats.
//   2. Для топ-N самых крупных — список колонок и тип.
//   3. TOP 3 строки из самой крупной таблицы.
package main

import (
	"context"
	"database/sql"
	"fmt"
	"log"
	"net/url"
	"os"
	"strconv"
	"time"

	_ "github.com/microsoft/go-mssqldb"
)

const topN = 10

type tableRow struct {
	Schema  string
	Name    string
	Rows    int64
	Columns int
}

func main() {
	// Поддерживаем оба набора имён: OLAP_* (для snapshot Premaster1C на 10.10.6.15)
	// и MSSQL_PREMASTER_* (исторический для debt-модуля).
	server := firstNonEmpty(os.Getenv("OLAP_HOST"), os.Getenv("MSSQL_PREMASTER_SERVER"))
	port := firstNonEmpty(os.Getenv("OLAP_PORT"), os.Getenv("MSSQL_PREMASTER_PORT"), "1433")
	user := firstNonEmpty(os.Getenv("OLAP_USER"), os.Getenv("MSSQL_PREMASTER_USER"))
	password := firstNonEmpty(os.Getenv("OLAP_PASSWORD"), os.Getenv("MSSQL_PREMASTER_PASSWORD"))
	database := firstNonEmpty(os.Getenv("OLAP_DATABASE"), os.Getenv("MSSQL_PREMASTER_DB"))

	if server == "" || database == "" || user == "" || password == "" {
		log.Fatalf("env: required (OLAP_HOST|MSSQL_PREMASTER_SERVER), (OLAP_USER|MSSQL_PREMASTER_USER), "+
			"(OLAP_PASSWORD|MSSQL_PREMASTER_PASSWORD), (OLAP_DATABASE|MSSQL_PREMASTER_DB)\n"+
			"got server=%q db=%q user=%q password=%s",
			server, database, user, mask(password))
	}

	dsn := buildDSN(server, port, database, user, password)
	db, err := sql.Open("sqlserver", dsn)
	if err != nil {
		log.Fatalf("open: %v", err)
	}
	defer db.Close()
	db.SetConnMaxLifetime(60 * time.Second)

	timeout := 90 * time.Second
	if v := os.Getenv("PROBE_TIMEOUT_SEC"); v != "" {
		if k, err := strconv.Atoi(v); err == nil && k > 0 {
			timeout = time.Duration(k) * time.Second
		}
	}
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()

	if err := db.PingContext(ctx); err != nil {
		log.Fatalf("ping: %v", err)
	}
	fmt.Printf("Connected to [%s].[%s] as %s\n\n", server, database, user)

	if os.Getenv("PROBE_FIND_TABLE") != "" {
		needle := os.Getenv("PROBE_FIND_TABLE")
		fmt.Printf("=== tables across all online DBs matching name LIKE %q (top 50) ===\n", needle)
		rows, err := db.QueryContext(ctx, `
DECLARE @sql NVARCHAR(MAX) = N'';
SELECT @sql = STRING_AGG(CAST(
    'SELECT TOP 50 ''' + d.name + ''' COLLATE DATABASE_DEFAULT AS db,
                   s.name COLLATE DATABASE_DEFAULT AS schema_name,
                   t.name COLLATE DATABASE_DEFAULT AS table_name
       FROM ' + QUOTENAME(d.name) + '.sys.tables t
       JOIN ' + QUOTENAME(d.name) + '.sys.schemas s ON s.schema_id = t.schema_id
      WHERE t.name LIKE @p1' AS NVARCHAR(MAX)), ' UNION ALL ')
FROM sys.databases d
WHERE d.state_desc = 'ONLINE' AND d.database_id > 4 AND HAS_DBACCESS(d.name) = 1;
EXEC sp_executesql @sql, N'@p1 NVARCHAR(200)', @p1=@p1;
`, needle)
		if err != nil {
			log.Fatalf("scan tables: %v", err)
		}
		defer rows.Close()
		for rows.Next() {
			var db, schema, tbl string
			if err := rows.Scan(&db, &schema, &tbl); err == nil {
				fmt.Printf("  [%s].[%s].[%s]\n", db, schema, tbl)
			}
		}
		return
	}

	if os.Getenv("PROBE_LIST_DB") == "1" {
		mask := os.Getenv("PROBE_DB_MASK")
		if mask == "" {
			mask = "%" // все user-БД
		}
		fmt.Printf("=== databases on this instance (mask=%q, system DBs excluded) ===\n", mask)
		rows, err := db.QueryContext(ctx, `SELECT name, state_desc, recovery_model_desc
FROM sys.databases
WHERE name LIKE @p1
  AND database_id > 4   -- master/tempdb/model/msdb
ORDER BY name`, mask)
		if err != nil {
			log.Fatalf("list databases: %v", err)
		}
		defer rows.Close()
		any := false
		for rows.Next() {
			var n, state, recovery string
			_ = rows.Scan(&n, &state, &recovery)
			fmt.Printf("  %-50s state=%s recovery=%s\n", n, state, recovery)
			any = true
		}
		if !any {
			fmt.Println("  (none)")
		}
		return
	}

	// Custom-SQL режим: если задан PROBE_SQL — выполняем как есть и печатаем результат.
	if sql := os.Getenv("PROBE_SQL"); sql != "" {
		if err := runCustomSQL(ctx, db, sql); err != nil {
			log.Fatalf("PROBE_SQL: %v", err)
		}
		return
	}

	// Focus-режим: если задан PROBE_TABLE=schema.name — смотрим только эту таблицу.
	if focus := os.Getenv("PROBE_TABLE"); focus != "" {
		schema, table := "dbo", focus
		if i := indexByte(focus, '.'); i > 0 {
			schema, table = focus[:i], focus[i+1:]
		}
		rowCount := approxRowCount(ctx, db, schema, table)
		fmt.Printf("=== [%s].[%s] — approx row count: %s ===\n", schema, table, fmtInt(rowCount))
		fmt.Println()
		fmt.Printf("=== columns ===\n")
		if err := dumpColumns(ctx, db, schema, table); err != nil {
			fmt.Printf("  (error: %v)\n", err)
		}
		fmt.Println()
		sn := 3
		if v := os.Getenv("PROBE_SAMPLE"); v != "" {
			if k, err := strconv.Atoi(v); err == nil && k > 0 && k <= 50 {
				sn = k
			}
		}
		fmt.Printf("=== TOP %d sample rows ===\n", sn)
		if err := dumpSample(ctx, db, schema, table, sn); err != nil {
			fmt.Printf("  (error: %v)\n", err)
		}
		return
	}

	tables, err := listTables(ctx, db)
	if err != nil {
		log.Fatalf("list tables: %v", err)
	}
	fmt.Printf("Total tables: %d\n\n", len(tables))

	// Уже отсортировано по rows DESC.
	limit := topN
	if len(tables) < limit {
		limit = len(tables)
	}
	fmt.Printf("=== TOP %d tables by row count (approx, via sys.dm_db_partition_stats) ===\n", limit)
	fmt.Printf("%-40s %15s %8s\n", "TABLE", "ROWS", "COLS")
	fmt.Printf("%-40s %15s %8s\n", "----------------------------------------", "---------------", "--------")
	for i := 0; i < limit; i++ {
		t := tables[i]
		fmt.Printf("%-40s %15s %8d\n", t.Schema+"."+t.Name, fmtInt(t.Rows), t.Columns)
	}
	fmt.Println()

	// Колонки для топ-3 + sample
	sampleN := 3
	if len(tables) < sampleN {
		sampleN = len(tables)
	}
	for i := 0; i < sampleN; i++ {
		t := tables[i]
		fmt.Printf("=== %s.%s — columns ===\n", t.Schema, t.Name)
		if err := dumpColumns(ctx, db, t.Schema, t.Name); err != nil {
			fmt.Printf("  (error: %v)\n", err)
		}
		fmt.Println()

		fmt.Printf("=== %s.%s — TOP 3 sample rows ===\n", t.Schema, t.Name)
		if err := dumpSample(ctx, db, t.Schema, t.Name, 3); err != nil {
			fmt.Printf("  (error: %v)\n", err)
		}
		fmt.Println()
	}
}

func buildDSN(server, port, database, user, password string) string {
	u := &url.URL{
		Scheme: "sqlserver",
		User:   url.UserPassword(user, password),
		Host:   server + ":" + port,
	}
	q := u.Query()
	q.Set("database", database)
	q.Set("encrypt", "true")
	q.Set("trustservercertificate", "true")
	q.Set("connection timeout", "30")
	q.Set("app name", "mssql-probe")
	u.RawQuery = q.Encode()
	return u.String()
}

func mask(s string) string {
	if s == "" {
		return "<empty>"
	}
	return fmt.Sprintf("<set, %d chars>", len(s))
}

func firstNonEmpty(vals ...string) string {
	for _, v := range vals {
		if v != "" {
			return v
		}
	}
	return ""
}

func indexByte(s string, b byte) int {
	for i := 0; i < len(s); i++ {
		if s[i] == b {
			return i
		}
	}
	return -1
}

// runCustomSQL — выполняет SELECT и печатает результат табличкой (для агрегатов).
func runCustomSQL(ctx context.Context, db *sql.DB, query string) error {
	rows, err := db.QueryContext(ctx, query)
	if err != nil {
		return err
	}
	defer rows.Close()
	cols, err := rows.Columns()
	if err != nil {
		return err
	}
	for _, c := range cols {
		fmt.Printf("%-30s ", c)
	}
	fmt.Println()
	for rows.Next() {
		vals := make([]any, len(cols))
		ptrs := make([]any, len(cols))
		for i := range vals {
			ptrs[i] = &vals[i]
		}
		if err := rows.Scan(ptrs...); err != nil {
			return err
		}
		for _, v := range vals {
			fmt.Printf("%-30s ", formatCell(v))
		}
		fmt.Println()
	}
	return rows.Err()
}

// approxRowCount — быстрая оценка rows через sys.dm_db_partition_stats (без сканов).
func approxRowCount(ctx context.Context, db *sql.DB, schema, table string) int64 {
	var n sql.NullInt64
	err := db.QueryRowContext(ctx, `
SELECT SUM(p.rows) FROM sys.partitions p
JOIN sys.tables t ON t.object_id = p.object_id
JOIN sys.schemas s ON s.schema_id = t.schema_id
WHERE s.name = @p1 AND t.name = @p2 AND p.index_id IN (0, 1)`,
		schema, table).Scan(&n)
	if err != nil || !n.Valid {
		return -1
	}
	return n.Int64
}

func listTables(ctx context.Context, db *sql.DB) ([]tableRow, error) {
	const q = `
SELECT s.name AS schema_name, t.name AS table_name,
       SUM(p.rows) AS row_count,
       (SELECT COUNT(*) FROM sys.columns c WHERE c.object_id = t.object_id) AS col_count
FROM sys.tables t
JOIN sys.schemas s ON s.schema_id = t.schema_id
JOIN sys.partitions p ON p.object_id = t.object_id
WHERE p.index_id IN (0, 1)  -- heap or clustered
GROUP BY s.name, t.name, t.object_id
ORDER BY row_count DESC, table_name ASC`
	rows, err := db.QueryContext(ctx, q)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []tableRow
	for rows.Next() {
		var t tableRow
		if err := rows.Scan(&t.Schema, &t.Name, &t.Rows, &t.Columns); err != nil {
			return nil, err
		}
		out = append(out, t)
	}
	return out, rows.Err()
}

func dumpColumns(ctx context.Context, db *sql.DB, schema, table string) error {
	const q = `
SELECT c.name, ty.name AS type_name,
       c.max_length, c.precision, c.scale, c.is_nullable
FROM sys.columns c
JOIN sys.types ty ON ty.user_type_id = c.user_type_id
JOIN sys.objects o ON o.object_id = c.object_id
JOIN sys.schemas s ON s.schema_id = o.schema_id
WHERE s.name = @p1 AND o.name = @p2
ORDER BY c.column_id`
	rows, err := db.QueryContext(ctx, q, schema, table)
	if err != nil {
		return err
	}
	defer rows.Close()
	fmt.Printf("  %-32s %-18s %s\n", "COLUMN", "TYPE", "NULL?")
	for rows.Next() {
		var name, ty string
		var maxLen, prec, scale int
		var nullable bool
		if err := rows.Scan(&name, &ty, &maxLen, &prec, &scale, &nullable); err != nil {
			return err
		}
		typ := ty
		switch ty {
		case "varchar", "char", "nvarchar", "nchar":
			length := maxLen
			if ty == "nvarchar" || ty == "nchar" {
				length /= 2
			}
			if length < 0 {
				typ += "(max)"
			} else {
				typ += "(" + strconv.Itoa(length) + ")"
			}
		case "decimal", "numeric":
			typ += fmt.Sprintf("(%d,%d)", prec, scale)
		}
		fmt.Printf("  %-32s %-18s %s\n", name, typ, ifNullable(nullable))
	}
	return rows.Err()
}

func ifNullable(b bool) string {
	if b {
		return "NULL"
	}
	return "NOT NULL"
}

func dumpSample(ctx context.Context, db *sql.DB, schema, table string, n int) error {
	q := fmt.Sprintf(`SELECT TOP %d * FROM [%s].[%s]`, n, schema, table)
	rows, err := db.QueryContext(ctx, q)
	if err != nil {
		return err
	}
	defer rows.Close()

	cols, err := rows.Columns()
	if err != nil {
		return err
	}
	for rows.Next() {
		vals := make([]any, len(cols))
		ptrs := make([]any, len(cols))
		for i := range vals {
			ptrs[i] = &vals[i]
		}
		if err := rows.Scan(ptrs...); err != nil {
			return err
		}
		fmt.Println("  ---")
		for i, c := range cols {
			fmt.Printf("  %-32s = %s\n", c, formatCell(vals[i]))
		}
	}
	return rows.Err()
}

func formatCell(v any) string {
	if v == nil {
		return "NULL"
	}
	full := os.Getenv("PROBE_FULL_TEXT") == "1"
	switch x := v.(type) {
	case []byte:
		s := string(x)
		if !full && len(s) > 80 {
			return s[:80] + "…"
		}
		return s
	case string:
		if !full && len(x) > 80 {
			return x[:80] + "…"
		}
		return x
	case time.Time:
		return x.Format(time.RFC3339)
	default:
		return fmt.Sprintf("%v", v)
	}
}

func fmtInt(n int64) string {
	// Простой форматтер с разделителями: 13 451 922
	s := strconv.FormatInt(n, 10)
	if len(s) <= 3 {
		return s
	}
	var out []byte
	for i, c := range s {
		if i > 0 && (len(s)-i)%3 == 0 {
			out = append(out, ' ')
		}
		out = append(out, byte(c))
	}
	return string(out)
}
