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
	payments string // ответ на запасной запрос платёжного факта
	accrual  string // ответ на запрос факта по начислениям из реплики P&L
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
	case strings.Contains(q, "finance.findwh_table_fin_pl"):
		fmt.Fprint(w, c.accrual)
	case strings.Contains(q, "scenario = '"+ExpSrcFactPayments+"'"):
		fmt.Fprint(w, c.payments)
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

const chRowFact = `{"scenario":"Факт по начислениям","country":"BY","y":2025,"m":3,"code_pl":24,"group_pl":"ВЫПЛАТЫ РАБОТНИКАМ","expense":"Заработная плата","code_cfo":101,"cfo":"Магазин Минск","group_cfo1":"3.Магазины","group_cfo2":"Минск","project_name":"Нет проектов!","special_access":"0","amount":999}`

const chRowRetail = `{"scenario":"Стратегический бюджет","country":"BY","y":2026,"m":3,"code_pl":24,"group_pl":"ВЫПЛАТЫ РАБОТНИКАМ","expense":"Заработная плата","code_cfo":101,"cfo":"Магазин Минск","group_cfo1":"3.Магазины","group_cfo2":"Минск","project_name":"Нет проектов!","special_access":"0","amount":1234.5}`

func TestExpCHSource_ReadsSnapshot(t *testing.T) {
	stub := &chStub{count: "960595\n", rows: chRowRetail + "\n" + chRowRetail + "\n" + chRowFact + "\n", asOf: "1792400000\n"}
	fb := &fallbackSource{}
	src := newCHTest(t, stub, fb)
	ctx, trace := WithExpSourceTrace(context.Background())
	cells, err := src.Costs(ctx, ExpKindRetail, "BY", []int{2025, 2026})
	if err != nil {
		t.Fatal(err)
	}
	if trace.Kind != ExpSourceKindCH {
		t.Fatalf("источник чтения: %q", trace.Kind)
	}
	if fb.calls != 0 {
		t.Fatal("при наполненном снимке процедуру звать нельзя")
	}
	if len(cells) != 3 {
		t.Fatalf("ждали 3 ячейки, got %d", len(cells))
	}
	c := cells[0]
	if c.Scenario != "Стратегический бюджет" || c.Year != 2026 || c.Month != 3 || c.CodePL != 24 || c.CodeCFO != 101 ||
		c.GroupCFO1 != "3.Магазины" || c.GroupCFO2 != "Минск" || c.Country != "BY" || c.Amount != 1234.5 || c.Expense != "Заработная плата" {
		t.Fatalf("ячейка разобрана неверно: %+v", c)
	}
	if asOf, ok := src.(ExpSourceInfo).SnapshotAsOf(context.Background()); !ok || asOf.Unix() != 1792400000 {
		t.Fatalf("SnapshotAsOf: %v %v", asOf, ok)
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
		ctx, trace := WithExpSourceTrace(context.Background())
		cells, err := src.Costs(ctx, ExpKindRetail, "BY", []int{2026})
		if err != nil || fb.calls != 1 || len(cells) != 1 || cells[0].CodeCFO != 5 {
			t.Fatalf("%s: err=%v calls=%d cells=%v", name, err, fb.calls, cells)
		}
		if trace.Kind != ExpSourceKindOLAP {
			t.Fatalf("%s: источник должен быть помечен как olap, got %q", name, trace.Kind)
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

// Пока в выдаче нет «Факта по начислениям», факт берётся платёжный — иначе колонки факта рядом с
// вводом пустые; при наличии начислений платёжный не используется (ответ 44).
func TestExpCHSource_PaymentsFactFallback(t *testing.T) {
	pay := strings.Replace(chRowFact, "Факт по начислениям", ExpSrcFactPayments, 1)
	stub := &chStub{count: "10\n", rows: chRowRetail + "\n", payments: pay + "\n"}
	ctx, trace := WithExpSourceTrace(context.Background())
	cells, err := newCHTest(t, stub, &fallbackSource{}).Costs(ctx, ExpKindRetail, "BY", []int{2025, 2026})
	if err != nil {
		t.Fatal(err)
	}
	if trace.FactBasis != ExpFactBasisPayments {
		t.Fatalf("экран должен узнать, что факт по платежам: %q", trace.FactBasis)
	}
	var fact int
	for _, c := range cells {
		if c.Scenario == ExpSrcFact && c.Year == 2025 && c.Amount == 999 {
			fact++
		}
	}
	if fact != 1 {
		t.Fatalf("платёжный факт не подставлен как факт: %+v", cells)
	}
	// Для форм отделов (CFO-kind) подмены нет.
	stub2 := &chStub{count: "10\n", rows: chRowRetail + "\n", payments: pay + "\n"}
	cells2, _ := newCHTest(t, stub2, &fallbackSource{}).Costs(context.Background(), CfoExpKind(CfoExp31), "BY", []int{2025, 2026})
	for _, c := range cells2 {
		if c.Scenario == ExpSrcFact && c.Amount == 999 {
			t.Fatal("для форм отделов платёжный факт подставлять нельзя")
		}
	}
}

// Факт по начислениям из реплики P&L: включён флагом, идёт вместо платёжного; пусто или выключено — платёжный.
func TestExpCHSource_AccrualFactFromFinPL(t *testing.T) {
	pay := strings.Replace(chRowFact, "Факт по начислениям", ExpSrcFactPayments, 1)
	acc := strings.Replace(chRowFact, `"amount":999`, `"amount":555`, 1)
	count := func(cells []ExpCostCell, amount float64) (n int) {
		for _, c := range cells {
			if c.Scenario == ExpSrcFact && c.Amount == amount {
				n++
			}
		}
		return n
	}

	// Включено и в реплике есть факт: он, платёжный не нужен, экран не помечает факт «по платежам».
	stub := &chStub{count: "10\n", rows: chRowRetail + "\n", payments: pay + "\n", accrual: acc + "\n"}
	ctx, trace := WithExpSourceTrace(context.Background())
	src := ExpCHSourceAccrualFact(newCHTest(t, stub, &fallbackSource{}), true)
	cells, err := src.Costs(ctx, ExpKindRetail, "BY", []int{2025, 2026})
	if err != nil {
		t.Fatal(err)
	}
	if count(cells, 555) != 1 || count(cells, 999) != 0 || trace.FactBasis != "" {
		t.Fatalf("факт по начислениям: %+v basis=%q", cells, trace.FactBasis)
	}

	// Месяцы без начислений дополняются платёжным фактом (ячейки помечены), месяцы с начислениями — нет.
	payApr := strings.Replace(pay, `"m":3`, `"m":4`, 1)
	stubGap := &chStub{count: "10\n", rows: chRowRetail + "\n", payments: pay + "\n" + payApr + "\n", accrual: acc + "\n"}
	ctxG, traceG := WithExpSourceTrace(context.Background())
	cellsG, _ := ExpCHSourceAccrualFact(newCHTest(t, stubGap, &fallbackSource{}), true).Costs(ctxG, ExpKindRetail, "BY", []int{2025, 2026})
	var gapAcc, gapPay int
	for _, c := range cellsG {
		if c.Scenario != ExpSrcFact {
			continue
		}
		switch {
		case c.Month == 3 && c.Amount == 555 && c.Basis == "":
			gapAcc++
		case c.Month == 4 && c.Amount == 999 && c.Basis == ExpFactBasisPayments:
			gapPay++
		default:
			t.Fatalf("лишняя ячейка факта: %+v", c)
		}
	}
	if gapAcc != 1 || gapPay != 1 || traceG.FactBasis != "" {
		t.Fatalf("дополнение платёжным фактом: acc=%d pay=%d basis=%q %+v", gapAcc, gapPay, traceG.FactBasis, cellsG)
	}
	if expCellBasis(ExpCostCell{Basis: ExpFactBasisPayments}, "") != ExpFactBasisPayments || expCellBasis(ExpCostCell{}, ExpFactBasisPayments) != ExpFactBasisPayments || expCellBasis(ExpCostCell{}, "") != "" {
		t.Fatal("expCellBasis")
	}

	// Реплика пуста — запасной платёжный факт.
	stub2 := &chStub{count: "10\n", rows: chRowRetail + "\n", payments: pay + "\n"}
	ctx2, trace2 := WithExpSourceTrace(context.Background())
	cells2, _ := ExpCHSourceAccrualFact(newCHTest(t, stub2, &fallbackSource{}), true).Costs(ctx2, ExpKindRetail, "BY", []int{2025, 2026})
	if count(cells2, 999) != 1 || trace2.FactBasis != ExpFactBasisPayments {
		t.Fatalf("при пустой реплике ждали платёжный факт: %+v basis=%q", cells2, trace2.FactBasis)
	}

	// Выключено — реплику не читаем вовсе.
	stub3 := &chStub{count: "10\n", rows: chRowRetail + "\n", payments: pay + "\n", accrual: acc + "\n"}
	cells3, _ := newCHTest(t, stub3, &fallbackSource{}).Costs(context.Background(), ExpKindRetail, "BY", []int{2025, 2026})
	if count(cells3, 555) != 0 || count(cells3, 999) != 1 {
		t.Fatalf("выключенный флаг: %+v", cells3)
	}
	for _, q := range stub3.queries {
		if strings.Contains(q, "findwh_table_fin_pl") {
			t.Fatal("при выключенном флаге реплика P&L читаться не должна")
		}
	}

	// Формы отделов — свои сценарии, начисления из P&L для них не подставляются.
	stub4 := &chStub{count: "10\n", rows: chRowRetail + "\n", accrual: acc + "\n"}
	cells4, _ := ExpCHSourceAccrualFact(newCHTest(t, stub4, &fallbackSource{}), true).Costs(context.Background(), CfoExpKind(CfoExp31), "BY", []int{2025, 2026})
	if count(cells4, 555) != 0 {
		t.Fatal("для форм отделов факт из P&L подставлять нельзя")
	}
}

// Запрос факта по начислениям: только факт расходных статей, группа ЦФО без номера, опт — в BYN, без служебных кодов.
func TestExpAccrualFactSQL(t *testing.T) {
	q := expAccrualFactSQL(ExpKindRetail, "BY", []int{2026})
	for _, want := range []string{
		"scenario = 'факт'", "for_report IN ('PL', 'BS_20', 'BS_25')", "FROM finance.findwh_table_fin_pl",
		"= 'Магазины'", "'3.Магазины' AS group_cfo1", "country = 'BY'", "toYear(month) IN (2026)",
		"f.pl NOT IN (1,50,93)", "'ПРОДАЖИ'", "toFloat64(amount) AS amt",
	} {
		if !strings.Contains(q, want) {
			t.Errorf("в запросе нет %q:\n%s", want, q)
		}
	}
	opt := expAccrualFactSQL(ExpKindOpt, "", nil)
	if !strings.Contains(opt, "toFloat64(amount_byn) AS amt") || !strings.Contains(opt, "group_cfo3 IN ('ОПТ РБ', 'ОПТ РФ', 'ОПТ УЗ')") ||
		!strings.Contains(opt, "any(f.g1) AS group_cfo1") {
		t.Errorf("запрос опта:\n%s", opt)
	}
	if expStripGroupNumber("3.Магазины") != "Магазины" || expStripGroupNumber("Магазины") != "Магазины" {
		t.Error("expStripGroupNumber")
	}
}
