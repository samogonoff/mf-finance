package plans

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
)

// chStub — подменяет HTTP-интерфейс ClickHouse: отвечает по тексту запроса.
type chStub struct {
	mu       sync.Mutex
	queries  []string
	count    string // ответ на SELECT count() FROM finance.exp_costs
	rows     string // ответ на выборку затрат (JSONEachRow)
	asOf     string // ответ на max(finished_at)
	failWith int    // не 0 — отвечать этим статусом на всё
}

func (c *chStub) handler(w http.ResponseWriter, r *http.Request) {
	b := new(strings.Builder)
	buf := make([]byte, 4096)
	for {
		n, err := r.Body.Read(buf)
		b.Write(buf[:n])
		if err != nil {
			break
		}
	}
	q := b.String()
	c.mu.Lock()
	c.queries = append(c.queries, q)
	c.mu.Unlock()
	if c.failWith != 0 {
		http.Error(w, "boom", c.failWith)
		return
	}
	switch {
	case strings.Contains(q, "SELECT count() FROM finance.exp_costs"):
		fmt.Fprint(w, c.count)
	case strings.Contains(q, "exp_costs_etl_runs"):
		fmt.Fprint(w, c.asOf)
	default:
		fmt.Fprint(w, c.rows)
	}
}

// fallbackSource — «процедура на OLAP»: считает вызовы.
type fallbackSource struct {
	calls int
	cells []ExpCostCell
}

func (f *fallbackSource) Costs(context.Context, string, string, []int) ([]ExpCostCell, error) {
	f.calls++
	return f.cells, nil
}
func (f *fallbackSource) Cfo(context.Context, string, string) (map[int]ExpCfoAttrs, error) {
	return map[int]ExpCfoAttrs{7: {CodeCFO: 7}}, nil
}
func (f *fallbackSource) SalesPlan(context.Context, string, int) (map[int]map[int]float64, error) {
	return nil, nil
}

func newCHTest(t *testing.T, stub *chStub, fb *fallbackSource) ExpDataSource {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(stub.handler))
	t.Cleanup(srv.Close)
	return NewExpCHSource(fb, srv.URL, "u", "p", nil)
}

const chRowRetail = `{"scenario":"Стратегический бюджет","country":"BY","y":2026,"m":3,"code_pl":24,"group_pl":"ВЫПЛАТЫ РАБОТНИКАМ","expense":"Заработная плата","code_cfo":101,"cfo":"Магазин Минск","group_cfo1":"3.Магазины","group_cfo2":"Минск","project_name":"Нет проектов!","special_access":"0","amount":1234.5}`

func TestExpCHSource_ReadsSnapshot(t *testing.T) {
	stub := &chStub{count: "960595\n", rows: chRowRetail + "\n" + chRowRetail + "\n", asOf: "1792400000\n"}
	fb := &fallbackSource{}
	src := newCHTest(t, stub, fb)
	cells, err := src.Costs(context.Background(), ExpKindRetail, "BY", []int{2025, 2026})
	if err != nil {
		t.Fatal(err)
	}
	if fb.calls != 0 {
		t.Fatal("при наполненном снимке процедуру звать нельзя")
	}
	if len(cells) != 2 {
		t.Fatalf("ждали 2 ячейки, got %d", len(cells))
	}
	c := cells[0]
	if c.Scenario != "Стратегический бюджет" || c.Year != 2026 || c.Month != 3 || c.CodePL != 24 || c.CodeCFO != 101 ||
		c.GroupCFO1 != "3.Магазины" || c.GroupCFO2 != "Минск" || c.Country != "BY" || c.Amount != 1234.5 || c.Expense != "Заработная плата" {
		t.Fatalf("ячейка разобрана неверно: %+v", c)
	}
	kind, asOf, ok := src.(ExpSourceInfo).SourceInfo(context.Background())
	if kind != ExpSourceKindCH || !ok || asOf.Unix() != 1792400000 {
		t.Fatalf("SourceInfo: %q %v %v", kind, asOf, ok)
	}
	// Справочник ЦФО идёт из процедуры/справочника, не из снимка.
	if m, _ := src.Cfo(context.Background(), ExpKindRetail, "BY"); m[7].CodeCFO != 7 {
		t.Fatal("Cfo обязан делегироваться базовому источнику")
	}
}

// Пустая таблица (до первой заливки) и недоступный CH не оставляют форму без строк.
func TestExpCHSource_FallsBackToProcedure(t *testing.T) {
	want := []ExpCostCell{{CodeCFO: 5, CodePL: 6}}
	for name, stub := range map[string]*chStub{
		"пустая таблица": {count: "0\n"},
		"CH отвечает 500": {failWith: 500},
	} {
		fb := &fallbackSource{cells: want}
		src := newCHTest(t, stub, fb)
		cells, err := src.Costs(context.Background(), ExpKindRetail, "BY", []int{2026})
		if err != nil || fb.calls != 1 || len(cells) != 1 || cells[0].CodeCFO != 5 {
			t.Fatalf("%s: err=%v calls=%d cells=%v", name, err, fb.calls, cells)
		}
		if kind, _, ok := src.(ExpSourceInfo).SourceInfo(context.Background()); kind != ExpSourceKindOLAP || ok {
			t.Fatalf("%s: источник должен быть помечен как olap, got %q ok=%v", name, kind, ok)
		}
	}
}

// Наполненный снимок, но по этой форме строк нет — это «0 строк», а не повод идти в процедуру.
func TestExpCHSource_EmptyResultIsNotFallback(t *testing.T) {
	stub := &chStub{count: "100\n", rows: ""}
	fb := &fallbackSource{cells: []ExpCostCell{{CodeCFO: 1}}}
	cells, err := newCHTest(t, stub, fb).Costs(context.Background(), ExpKindRetail, "UZ", []int{2026})
	if err != nil || len(cells) != 0 || fb.calls != 0 {
		t.Fatalf("err=%v cells=%d fallbackCalls=%d", err, len(cells), fb.calls)
	}
}

// Отбор ТЗ §2.1 — те же условия, что у чтения процедуры.
func TestExpCostsSQL_Filters(t *testing.T) {
	q := expCostsSQL(ExpKindRetail, "BY", []int{2025, 2026})
	for _, want := range []string{
		"group_cfo1 = '3.Магазины'", "country = 'BY'", "toYear(month) IN (2025,2026)", "month >= '2024-01-01'",
		"code_pl NOT IN (", "project_name = ''", "Нет проектов!", "scenario != ''", "scenario != 'Факт по платежам'",
	} {
		if !strings.Contains(q, want) {
			t.Errorf("в запросе нет %q:\n%s", want, q)
		}
	}
	for _, c := range []string{"1", "50", "93"} {
		if !strings.Contains(q, c) {
			t.Errorf("служебный код %s не отброшен", c)
		}
	}
	// Страна не задана — условия по стране нет; кавычка в значении не ломает запрос.
	if q2 := expCostsSQL(ExpKindMP, "", nil); strings.Contains(q2, "country =") || strings.Contains(q2, "toYear(month) IN") {
		t.Errorf("лишние условия: %s", q2)
	}
	if got := chLit(`a'b\c`); got != `'a\'b\\c'` {
		t.Errorf("chLit: %s", got)
	}
}

func TestExpCostsSQL_CfoFormsUseSpecScenarios(t *testing.T) {
	code := CfoExp31
	spec, ok := LookupCfoExpSpec(code)
	if !ok {
		t.Skip("нет спецификации формы")
	}
	q := expCostsSQL(CfoExpKind(code), "BY", []int{2026})
	if !strings.Contains(q, "group_cfo1 = 'Отдел'") || !strings.Contains(q, fmt.Sprintf("code_cfo = %d", int(code))) ||
		!strings.Contains(q, chLit(spec.FactScenario)) || !strings.Contains(q, chLit(spec.PlanScenario)) {
		t.Errorf("запрос формы отдела:\n%s", q)
	}
}