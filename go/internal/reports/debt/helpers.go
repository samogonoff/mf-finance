package debt

import (
	"database/sql"
	"fmt"
	"regexp"
	"strings"
	"time"

	_ "github.com/microsoft/go-mssqldb"
)

// Общие хелперы отчёта «Задолженность ВГО». Собраны сюда после выпила старых
// бэкендов (Premaster/GLMF/finpl): findebt-репо (CH и live-MSSQL) переиспользуют
// эти функции, которые раньше жили в удалённых repo_premaster/clickhouse/glmf/finpl.

// identRe — валидатор SQL-идентификаторов (имена БД/схем/таблиц из env),
// анти-инъекция при подстановке в текст запроса.
var identRe = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_]*$`)

// NewOLAPPool открывает MSSQL-пул к OLAP-серверу (на нём живут и FinDWH, и
// Payments-вьюхи FinDebt). При пустых учётках возвращает (nil,nil) — сигнал
// main.go, что live-источник не настроен (UI остаётся в DEBT_MOCK=1).
// port — числовая строка ("1433" дефолт); если в server уже есть ":port",
// передавайте port="".
func NewOLAPPool(server, port, database, user, password string) (*sql.DB, error) {
	if server == "" || user == "" || password == "" {
		return nil, nil
	}
	host := server
	if port != "" && !strings.Contains(server, ":") {
		host = server + ":" + port
	}
	dsn := fmt.Sprintf(
		"sqlserver://%s:%s@%s?database=%s&encrypt=true&trustservercertificate=true&app+name=finance-api",
		user, password, host, database,
	)
	db, err := sql.Open("sqlserver", dsn)
	if err != nil {
		return nil, fmt.Errorf("debt: open mssql: %w", err)
	}
	db.SetMaxOpenConns(4)
	db.SetMaxIdleConns(2)
	return db, nil
}

// countryOfINN — страна нашего юрлица по ИНН/УНП; "" если ИНН не наш (seed.go).
func countryOfINN(inn string) Country {
	for _, e := range Entities() {
		if e.INN == strings.TrimSpace(inn) {
			return e.Country
		}
	}
	return ""
}

// AccountRoot — корень счёта (до первой точки): "62.4.1" → "62", "9010" → "9010".
func AccountRoot(acc string) string {
	if i := strings.IndexByte(acc, '.'); i > 0 {
		return acc[:i]
	}
	return acc
}

// accRootSQL — SQL-выражение корня счёта по колонке (LEFT до первой точки).
// `col + '.'` гарантирует точку даже для счёта без неё ('9010' → '9010.').
func accRootSQL(col string) string {
	return "LEFT(" + col + ", CHARINDEX('.', " + col + " + '.') - 1)"
}

// asMSSQLDate — time.Time → дата без времени (UTC) для параметра datetime MSSQL.
func asMSSQLDate(t time.Time) time.Time {
	return time.Date(t.Year(), t.Month(), t.Day(), 0, 0, 0, 0, time.UTC)
}

// asDate — time.Time → 'YYYY-MM-DD' (для инлайна в CH-SQL).
func asDate(t time.Time) string { return t.Format("2006-01-02") }

// bindIN — добавляет именованные параметры для IN-clause и возвращает их
// текстовые имена (`@i0,@i1,...`) для подстановки в SQL.
func bindIN(args []any, prefix string, values []string) ([]string, []any) {
	names := make([]string, len(values))
	for i, v := range values {
		name := fmt.Sprintf("%s%d", prefix, i)
		names[i] = "@" + name
		args = append(args, sql.Named(name, v))
	}
	return names, args
}

// quoteList — 'a','b','c' с экранированием кавычек (инлайн IN-списка для CH).
func quoteList(vals []string) string {
	q := make([]string, len(vals))
	for i, v := range vals {
		q[i] = "'" + strings.ReplaceAll(v, "'", "''") + "'"
	}
	return strings.Join(q, ",")
}

// nz — строка из NullString или "".
func nz(s sql.NullString) string {
	if s.Valid {
		return strings.TrimSpace(s.String)
	}
	return ""
}
