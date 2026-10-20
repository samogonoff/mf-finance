package plans

import (
	"context"
	"database/sql"
	"database/sql/driver"
	"errors"
	"io"
	"strings"
	"sync"
	"testing"
)

// Минимальный драйвер: запрос с присоединением Лисы падает (нет прав на s_klient), без него отдаёт один магазин.
type lisaTestDriver struct {
	mu      sync.Mutex
	queries []string
}

func (d *lisaTestDriver) Open(string) (driver.Conn, error) { return &lisaTestConn{d: d}, nil }

type lisaTestConn struct{ d *lisaTestDriver }

func (c *lisaTestConn) Prepare(q string) (driver.Stmt, error) { return &lisaTestStmt{c: c, q: q}, nil }
func (c *lisaTestConn) Close() error                          { return nil }
func (c *lisaTestConn) Begin() (driver.Tx, error) {
	return nil, errors.New("не поддерживается")
}

type lisaTestStmt struct {
	c *lisaTestConn
	q string
}

func (s *lisaTestStmt) Close() error  { return nil }
func (s *lisaTestStmt) NumInput() int { return -1 }
func (s *lisaTestStmt) Exec([]driver.Value) (driver.Result, error) {
	return nil, errors.New("не поддерживается")
}
func (s *lisaTestStmt) Query([]driver.Value) (driver.Rows, error) {
	s.c.d.mu.Lock()
	s.c.d.queries = append(s.c.d.queries, s.q)
	s.c.d.mu.Unlock()
	if strings.Contains(s.q, "JOIN") {
		return nil, errors.New("mssql: The SELECT permission was denied on the object 's_klient'")
	}
	return &lisaTestRows{}, nil
}

type lisaTestRows struct{ done bool }

func (r *lisaTestRows) Columns() []string {
	n := make([]string, 21)
	for i := range n {
		n[i] = "c"
	}
	return n
}
func (r *lisaTestRows) Close() error { return nil }
func (r *lisaTestRows) Next(dest []driver.Value) error {
	if r.done {
		return io.EOF
	}
	r.done = true
	for i := range dest {
		dest[i] = nil
	}
	dest[0], dest[2], dest[4] = "Магазины", "101", "BY"
	dest[15] = "новый" // LfLStatus справочника ЦФО
	return nil
}

// Нет прав на таблицу Лисы — справочник магазинов не падает: запрос повторяется без присоединения, статус
// берётся из справочника ЦФО, а подпись источника на экране об этом честно говорит.
func TestRetailStores_LisaUnavailableFallsBack(t *testing.T) {
	drv := &lisaTestDriver{}
	db := sql.OpenDB(lisaTestConnector{drv})
	defer db.Close()
	src := &mssqlRetailSource{db: db, tbl: RetailTables{StoreTable: "FinDWH.dbo.[001 CodeCFO]", StoreGroup: "Магазины", LFLTable: "DWH.dim.s_klient"}}

	stores, err := src.Stores(context.Background(), "")
	if err != nil {
		t.Fatalf("справочник не должен падать без прав на Лису: %v", err)
	}
	if len(stores) != 1 || stores[0].LFLStatus != LFLNew {
		t.Fatalf("статус из справочника ЦФО: %+v", stores)
	}
	if len(drv.queries) != 2 || !strings.Contains(drv.queries[0], "JOIN") || strings.Contains(drv.queries[1], "JOIN") {
		t.Fatalf("ожидали запрос с Лисой и повтор без неё: %v", drv.queries)
	}
	if got := src.LFLSourceLabel(); !strings.Contains(got, "недоступны") || !strings.Contains(got, "[001 CodeCFO].LfLStatus") {
		t.Fatalf("подпись при недоступной Лисе: %q", got)
	}
}

func TestRetailLFLSourceLabel(t *testing.T) {
	const cfo = "FinDWH.dbo.[001 CodeCFO]"
	if got := retailLFLSourceLabel(cfo, "", false); got != cfo+".LfLStatus" {
		t.Errorf("Лиса не настроена: %q", got)
	}
	got := retailLFLSourceLabel(cfo, "DWH.dim.s_klient", false)
	if !strings.HasPrefix(got, "DWH.dim.s_klient.LFL_STATUS") || !strings.Contains(got, "нет карточки в Лисе") {
		t.Errorf("Лиса работает: %q", got)
	}
}

type lisaTestConnector struct{ d *lisaTestDriver }

func (c lisaTestConnector) Connect(context.Context) (driver.Conn, error) { return c.d.Open("") }
func (c lisaTestConnector) Driver() driver.Driver                        { return c.d }
