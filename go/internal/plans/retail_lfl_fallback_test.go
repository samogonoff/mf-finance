package plans

import (
	"context"
	"database/sql"
	"database/sql/driver"
	"errors"
	"fmt"
	"io"
	"strings"
	"sync"
	"testing"
)

// sqlNumErr — ошибка сервера с номером, как mssql.Error (SQLErrorNumber).
type sqlNumErr struct{ n int32 }

func (e sqlNumErr) Error() string         { return fmt.Sprintf("mssql: error %d", e.n) }
func (e sqlNumErr) SQLErrorNumber() int32 { return e.n }

// Минимальный драйвер: запрос с присоединением Лисы падает заданной ошибкой, без него отдаёт один магазин.
type lisaTestDriver struct {
	mu       sync.Mutex
	queries  []string
	joinFail error
}

func (d *lisaTestDriver) Open(string) (driver.Conn, error) { return &lisaTestConn{d: d}, nil }

func (d *lisaTestDriver) Connect(context.Context) (driver.Conn, error) { return d.Open("") }
func (d *lisaTestDriver) Driver() driver.Driver                        { return d }

func (d *lisaTestDriver) joins() (n int) {
	d.mu.Lock()
	defer d.mu.Unlock()
	for _, q := range d.queries {
		if strings.Contains(q, "JOIN") {
			n++
		}
	}
	return n
}

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
	fail := s.c.d.joinFail
	s.c.d.mu.Unlock()
	if strings.Contains(s.q, "JOIN") && fail != nil {
		return nil, fail
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

func lisaTestSource(t *testing.T, drv *lisaTestDriver) *mssqlRetailSource {
	t.Helper()
	db := sql.OpenDB(drv)
	t.Cleanup(func() { db.Close() })
	return &mssqlRetailSource{db: db, tbl: RetailTables{StoreTable: "FinDWH.dbo.[001 CodeCFO]", StoreGroup: "Магазины", LFLTable: "DWH.dim.s_klient"}}
}

// Нет прав на таблицу Лисы (229) — справочник магазинов не падает: запрос повторяется без присоединения,
// статус берётся из справочника ЦФО, подпись источника этого чтения об этом говорит. Следующие чтения
// в течение паузы не дёргают Лису снова.
func TestRetailStores_LisaPermissionDeniedFallsBack(t *testing.T) {
	drv := &lisaTestDriver{joinFail: sqlNumErr{229}}
	src := lisaTestSource(t, drv)

	stores, label, err := src.StoresWithLFLSource(context.Background(), "")
	if err != nil {
		t.Fatalf("справочник не должен падать без прав на Лису: %v", err)
	}
	if len(stores) != 1 || stores[0].LFLStatus != LFLNew {
		t.Fatalf("статус из справочника ЦФО: %+v", stores)
	}
	if !strings.Contains(label, "недоступны") || !strings.Contains(label, "[001 CodeCFO].LfLStatus") {
		t.Fatalf("подпись при недоступной Лисе: %q", label)
	}
	if drv.joins() != 1 || len(drv.queries) != 2 {
		t.Fatalf("ожидали запрос с Лисой и повтор без неё: %v", drv.queries)
	}

	// Пауза: второе чтение сразу идёт без JOIN, подпись по-прежнему честная.
	_, label2, err := src.StoresWithLFLSource(context.Background(), "")
	if err != nil || !strings.Contains(label2, "недоступны") {
		t.Fatalf("второе чтение: %q %v", label2, err)
	}
	if drv.joins() != 1 {
		t.Fatalf("в паузе Лиса не должна запрашиваться: %v", drv.queries)
	}
}

// Случайный сбой (обрыв, deadlock, таймаут) не подменяет статусы Лисы устаревшими: ошибка возвращается,
// синхронизация падает, а уже записанные статусы остаются.
func TestRetailStores_TransientErrorIsNotFallback(t *testing.T) {
	for name, e := range map[string]error{
		"deadlock 1205": sqlNumErr{1205},
		"обрыв":         errors.New("read tcp: connection reset by peer"),
	} {
		drv := &lisaTestDriver{joinFail: e}
		src := lisaTestSource(t, drv)
		if _, _, err := src.StoresWithLFLSource(context.Background(), ""); err == nil {
			t.Errorf("%s: ошибка должна вернуться вызывающему", name)
		}
		if len(drv.queries) != 1 {
			t.Errorf("%s: запасного запроса без Лисы быть не должно: %v", name, drv.queries)
		}
		if src.lisaSkipUntil.Load() != 0 {
			t.Errorf("%s: пауза Лисы включаться не должна", name)
		}
	}
}

func TestRetailLisaUnavailable(t *testing.T) {
	for n, want := range map[int32]bool{229: true, 230: true, 208: true, 916: true, 4060: true, 1205: false, -2: false} {
		if got := retailLisaUnavailable(sqlNumErr{n}); got != want {
			t.Errorf("ошибка %d: %v, ждали %v", n, got, want)
		}
	}
	if retailLisaUnavailable(context.Canceled) || retailLisaUnavailable(nil) {
		t.Error("ошибки контекста и nil — не недоступность Лисы")
	}
	if !retailLisaUnavailable(fmt.Errorf("обёртка: %w", sqlNumErr{229})) {
		t.Error("ошибка должна находиться и в обёртке")
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
