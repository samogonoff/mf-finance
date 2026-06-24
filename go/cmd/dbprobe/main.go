package main

import (
	"bufio"
	"context"
	"database/sql"
	"fmt"
	"os"
	"strings"
	"time"

	_ "github.com/microsoft/go-mssqldb"
)

func loadEnv(p string) map[string]string {
	m := map[string]string{}
	f, _ := os.Open(p)
	if f == nil {
		return m
	}
	defer f.Close()
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		l := strings.TrimSpace(sc.Text())
		if l == "" || strings.HasPrefix(l, "#") {
			continue
		}
		kv := strings.SplitN(l, "=", 2)
		if len(kv) != 2 {
			continue
		}
		m[strings.TrimSpace(kv[0])] = strings.Trim(strings.TrimSpace(kv[1]), `"'`)
	}
	return m
}
func fv(v any) string {
	switch t := v.(type) {
	case nil:
		return "<nil>"
	case []byte:
		return string(t)
	case time.Time:
		return t.Format("2006-01-02")
	default:
		return fmt.Sprintf("%v", t)
	}
}
func q(ctx context.Context, c *sql.DB, title, query string) {
	fmt.Printf("\n=== %s ===\n", title)
	rows, err := c.QueryContext(ctx, query)
	if err != nil {
		fmt.Println("ERR:", err)
		return
	}
	defer rows.Close()
	cols, _ := rows.Columns()
	fmt.Println(strings.Join(cols, " | "))
	n := 0
	for rows.Next() {
		vals := make([]any, len(cols))
		p := make([]any, len(cols))
		for i := range vals {
			p[i] = &vals[i]
		}
		rows.Scan(p...)
		parts := []string{}
		for i := range cols {
			parts = append(parts, fv(vals[i]))
		}
		fmt.Println(strings.Join(parts, " | "))
		n++
		if n > 80 {
			break
		}
	}
}
func main() {
	env := loadEnv("../.env")
	dsn := fmt.Sprintf("sqlserver://%s:%s@%s:1433?database=FinDWH&encrypt=true&trustservercertificate=true&app+name=dbprobe", env["MSSQL_PREMASTER_USER"], env["MSSQL_PREMASTER_PASSWORD"], env["MSSQL_PREMASTER_SERVER"])
	ctx, cancel := context.WithTimeout(context.Background(), 250*time.Second)
	defer cancel()
	var c *sql.DB
	for i := 0; i < 6; i++ {
		c, _ = sql.Open("sqlserver", dsn)
		pc, cc := context.WithTimeout(ctx, 20*time.Second)
		err := c.PingContext(pc)
		cc()
		if err == nil {
			break
		}
		fmt.Printf("ping %d: %v\n", i+1, err)
		c.Close()
		c = nil
		t := time.NewTimer(3 * time.Second)
		<-t.C
	}
	if c == nil {
		fmt.Println("no conn")
		os.Exit(1)
	}
	defer c.Close()
	fmt.Println("CONNECTED")
	q(ctx, c, "Table_Fin_PL rowcount now", `SELECT COUNT(*) FROM dbo.Table_Fin_PL`)
	q(ctx, c, "ВГО CounterpartyName -> ИНН (vGLMFAddUSD, all history)", `SELECT CounterpartyID inn, MAX(CounterpartyName) nm, COUNT(*) c FROM [FinDWH].[dbo].[vGLMFAddUSD] WHERE ICO=1 AND CounterpartyID IS NOT NULL GROUP BY CounterpartyID ORDER BY c DESC`)
	q(ctx, c, "distinct CounterpartyName values where ICO=1 (как в витрине)", `SELECT DISTINCT TOP 60 CounterpartyName, CounterpartyID FROM [FinDWH].[dbo].[vGLMFAddUSD] WHERE ICO=1 ORDER BY CounterpartyName`)
}
