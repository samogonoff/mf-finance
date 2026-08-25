package plans

import (
	"math"
	"testing"
)

// Тесты массовых операций формы «Розница» (ТЗ §5). Без БД: ApplyRetailBulk
// возвращает diff, ничего не пишет.

// bulkCtx — контекст с одним LFL-магазином и заданными срезами.
func bulkCtx(rows []RetailRow, ser RetailSeries, params []RetailParam) RetailBulkContext {
	return RetailBulkContext{
		Country: "BY", Year: 2026, Month: 7, PlanMonths: []int{7, 8},
		Rows: rows, Series: ser, Params: RetailParamSet{Params: params},
		RentPrevFact: map[int]float64{},
	}
}

// rowWithCell — строка с одной ячейкой заданного source.
func rowWithCell(code int, lfl string, month int, amount float64, source string) RetailRow {
	a := amount
	return RetailRow{
		CodeCFO: code, NameCFO: "Магазин " + itoa(int64(code)), LFLEffective: lfl,
		City: "Минск", StoreType: "ТЦ",
		Values: []RetailValue{{
			Metric: MetricSales, Year: 2026, Month: month, Amount: &a, Source: source,
		}},
	}
}

// TestBulk_ManualNotOverwritten — ячейка с source=manual НЕ перезатирается
// повторным запуском массовой операции (ТЗ §5); сброс — только явный ResetManual.
func TestBulk_ManualNotOverwritten(t *testing.T) {
	ser := NewRetailSeries()
	ser.AddStrategy([]RetailFactCell{
		{CodeCFO: 100, Year: 2026, Month: 7, Amount: 500},
		{CodeCFO: 100, Year: 2026, Month: 8, Amount: 600},
	})
	// Июль занят ручным значением, август — значением от предыдущего прогона.
	row := RetailRow{CodeCFO: 100, NameCFO: "Магазин", LFLEffective: LFLYes}
	manual, generated := 111.0, 222.0
	row.Values = []RetailValue{
		{Metric: MetricSales, Year: 2026, Month: 7, Amount: &manual, Source: ValueManual},
		{Metric: MetricSales, Year: 2026, Month: 8, Amount: &generated, Source: ValueFromStrategy},
	}

	req := RetailBulkRequest{Op: BulkCopyScenario, Scope: BulkScopeAll, Source: CopyFromStrategy,
		SourceYear: 2026, Months: []int{7, 8}, Preview: true}
	res, err := ApplyRetailBulk(req, bulkCtx([]RetailRow{row}, ser, nil))
	if err != nil {
		t.Fatalf("операция вернула ошибку: %v", err)
	}
	if res.Protected != 1 {
		t.Errorf("защищено manual: got %d, want 1", res.Protected)
	}
	if res.Changed != 1 {
		t.Fatalf("изменится ячеек: got %d, want 1 (только август)", res.Changed)
	}
	if res.Diff[0].Month != 8 {
		t.Errorf("изменилась не та ячейка: месяц %d, ожидался 8", res.Diff[0].Month)
	}
	if math.Abs(res.Diff[0].After-600) > 1e-9 {
		t.Errorf("новое значение августа: got %.2f, want 600", res.Diff[0].After)
	}

	// Явный сброс защиты — ТЗ §5: «сброс — явное действие».
	req.ResetManual = true
	res, err = ApplyRetailBulk(req, bulkCtx([]RetailRow{row}, ser, nil))
	if err != nil {
		t.Fatalf("операция со сбросом вернула ошибку: %v", err)
	}
	if res.Protected != 0 {
		t.Errorf("после ResetManual защищённых быть не должно, got %d", res.Protected)
	}
	if res.Changed != 2 {
		t.Errorf("после ResetManual изменится ячеек: got %d, want 2", res.Changed)
	}
}

// TestBulk_SalesIndex — «Индекс роста продаж» (ТЗ §5): Тактика = База × (1 + Индекс),
// приоритет переопределения от частного к общему, «новый»/«ххх» пропускаются.
func TestBulk_SalesIndex(t *testing.T) {
	ser := NewRetailSeries()
	// База по умолчанию — факт предыдущего месяца (июнь 2026).
	ser.AddFact([]RetailFactCell{
		{CodeCFO: 100, Year: 2026, Month: 6, Amount: 1000},
		{CodeCFO: 200, Year: 2026, Month: 6, Amount: 2000},
		{CodeCFO: 300, Year: 2026, Month: 6, Amount: 3000},
	})
	rows := []RetailRow{
		{CodeCFO: 100, LFLEffective: LFLYes, City: "Минск", StoreType: "ТЦ"},
		{CodeCFO: 200, LFLEffective: LFLYes, City: "Гомель", StoreType: "стрит"},
		{CodeCFO: 300, LFLEffective: LFLNew, City: "Минск", StoreType: "ТЦ"}, // индекс не применяется
	}
	params := []RetailParam{
		{ParamCode: ParamSalesIndex, ScopeKind: ParamScopeCountry, ScopeValue: "BY", Value: 0.10},
		// Переопределение по магазину 100 — приоритетнее странового (ТЗ §5).
		{ParamCode: ParamSalesIndex, ScopeKind: ParamScopeStore, ScopeValue: "100", Value: -0.05},
	}

	req := RetailBulkRequest{Op: BulkSalesIndex, Scope: BulkScopeAll, Months: []int{7}, Preview: true}
	res, err := ApplyRetailBulk(req, bulkCtx(rows, ser, params))
	if err != nil {
		t.Fatalf("операция вернула ошибку: %v", err)
	}
	got := map[int]float64{}
	for _, d := range res.Diff {
		got[d.CodeCFO] = d.After
	}
	// Магазин 100: индекс по магазину −5 % → 1000 × 0.95 = 950.
	if math.Abs(got[100]-950) > 1e-9 {
		t.Errorf("магазин 100 (переопределение): got %.2f, want 950", got[100])
	}
	// Магазин 200: страновой индекс +10 % → 2000 × 1.10 = 2200.
	if math.Abs(got[200]-2200) > 1e-9 {
		t.Errorf("магазин 200 (страновой индекс): got %.2f, want 2200", got[200])
	}
	// Магазин 300 «новый» — пропущен (ТЗ §5).
	if _, ok := got[300]; ok {
		t.Errorf("к магазину со статусом «новый» индекс применяться не должен")
	}
}

// TestBulk_SalesIndexBases — все четыре базы индекса (ТЗ §5).
func TestBulk_SalesIndexBases(t *testing.T) {
	ser := NewRetailSeries()
	ser.AddFact([]RetailFactCell{
		{CodeCFO: 100, Year: 2026, Month: 6, Amount: 100}, // fact_prev_month
		{CodeCFO: 100, Year: 2025, Month: 7, Amount: 200}, // fact_prev_year
	})
	ser.AddApproved([]RetailFactCell{{CodeCFO: 100, Year: 2026, Month: 7, Amount: 300}})
	ser.AddStrategy([]RetailFactCell{{CodeCFO: 100, Year: 2026, Month: 7, Amount: 400}})
	rows := []RetailRow{{CodeCFO: 100, LFLEffective: LFLYes}}
	params := []RetailParam{{ParamCode: ParamSalesIndex, ScopeKind: ParamScopeCountry, ScopeValue: "BY", Value: 0}}

	cases := []struct {
		base string
		want float64
	}{
		{IndexBaseFactPrevMonth, 100},
		{IndexBaseFactPrevYear, 200},
		{IndexBaseApprovedPrev, 300},
		{IndexBaseStrategy, 400},
	}
	for _, c := range cases {
		req := RetailBulkRequest{Op: BulkSalesIndex, Scope: BulkScopeAll, Months: []int{7},
			IndexBase: c.base, Preview: true}
		res, err := ApplyRetailBulk(req, bulkCtx(rows, ser, params))
		if err != nil {
			t.Fatalf("база %s: %v", c.base, err)
		}
		if len(res.Diff) != 1 {
			t.Fatalf("база %s: ожидалась одна изменённая ячейка, got %d", c.base, len(res.Diff))
		}
		if math.Abs(res.Diff[0].After-c.want) > 1e-9 {
			t.Errorf("база %s: got %.2f, want %.2f", c.base, res.Diff[0].After, c.want)
		}
	}
}

// TestBulk_Distribute — «Методика Е.В.» (ТЗ §5): доля = база/Σбаза, план = доля ×
// целевой итог; целевой итог задан на период и делится по целевым месяцам.
func TestBulk_Distribute(t *testing.T) {
	ser := NewRetailSeries()
	// База — среднее факта за апрель–май: магазин 100 → 100, магазин 200 → 300.
	ser.AddFact([]RetailFactCell{
		{CodeCFO: 100, Year: 2026, Month: 4, Amount: 80},
		{CodeCFO: 100, Year: 2026, Month: 5, Amount: 120},
		{CodeCFO: 200, Year: 2026, Month: 4, Amount: 200},
		{CodeCFO: 200, Year: 2026, Month: 5, Amount: 400},
	})
	rows := []RetailRow{{CodeCFO: 100, LFLEffective: LFLYes}, {CodeCFO: 200, LFLEffective: LFLYes}}

	req := RetailBulkRequest{Op: BulkDistribute, Scope: BulkScopeAll, Months: []int{7, 8},
		BaseMonths: []int{4, 5}, TargetTotal: 8000, Preview: true}
	res, err := ApplyRetailBulk(req, bulkCtx(rows, ser, nil))
	if err != nil {
		t.Fatalf("операция вернула ошибку: %v", err)
	}
	// Σбаза = 100 + 300 = 400; доли 0.25 и 0.75. На месяц приходится 8000/2 = 4000.
	want := map[int]float64{100: 1000, 200: 3000}
	total := 0.0
	for _, d := range res.Diff {
		total += d.After
		if math.Abs(d.After-want[d.CodeCFO]) > 1e-9 {
			t.Errorf("магазин %d, месяц %d: got %.2f, want %.2f", d.CodeCFO, d.Month, d.After, want[d.CodeCFO])
		}
		if d.AfterSource != ValueDistributed {
			t.Errorf("source распределения: got %q, want %q", d.AfterSource, ValueDistributed)
		}
	}
	// Итог за период должен сойтись с целевой суммой (ТЗ §5).
	if math.Abs(total-8000) > 1e-6 {
		t.Errorf("сумма распределения за период: got %.2f, want 8000", total)
	}
}

// TestBulk_PayrollCap — «ФОТ от продаж» (ТЗ §5): MIN(План; База × Порог) × Уд.вес,
// порог по умолчанию 106 %, при срабатывании — пояснение в ячейке.
func TestBulk_PayrollCap(t *testing.T) {
	ser := NewRetailSeries()
	// База ограничения — стратегия июля = 1000; порог 106 % → предел 1060.
	ser.AddStrategy([]RetailFactCell{{CodeCFO: 100, Year: 2026, Month: 7, Amount: 1000}})
	params := []RetailParam{
		{ParamCode: ParamPayrollShare, ScopeKind: ParamScopeCountry, ScopeValue: "BY", Value: 0.10},
	}

	// План 2000 > 1060 → ФОТ = 1060 × 0.10 = 106, с пометкой о пороге.
	over := rowWithCell(100, LFLYes, 7, 2000, ValueManual)
	req := RetailBulkRequest{Op: BulkPayroll, Scope: BulkScopeAll, Months: []int{7}, Preview: true}
	res, err := ApplyRetailBulk(req, bulkCtx([]RetailRow{over}, ser, params))
	if err != nil {
		t.Fatalf("операция вернула ошибку: %v", err)
	}
	if len(res.Diff) != 1 {
		t.Fatalf("ожидалась одна ячейка ФОТ, got %d", len(res.Diff))
	}
	if math.Abs(res.Diff[0].After-106) > 1e-9 {
		t.Errorf("ФОТ при сработавшем пороге: got %.2f, want 106", res.Diff[0].After)
	}
	if res.Diff[0].Note == "" {
		t.Error("при срабатывании порога ячейка должна нести пояснение (ТЗ §5)")
	}
	if res.Diff[0].Metric != MetricPayroll {
		t.Errorf("метрика ФОТ: got %q, want %q", res.Diff[0].Metric, MetricPayroll)
	}

	// План 500 < 1060 → порог не срабатывает: ФОТ = 500 × 0.10 = 50, без пометки.
	under := rowWithCell(100, LFLYes, 7, 500, ValueManual)
	res, err = ApplyRetailBulk(req, bulkCtx([]RetailRow{under}, ser, params))
	if err != nil {
		t.Fatalf("операция вернула ошибку: %v", err)
	}
	if math.Abs(res.Diff[0].After-50) > 1e-9 {
		t.Errorf("ФОТ без порога: got %.2f, want 50", res.Diff[0].After)
	}
	if res.Diff[0].Note != "" {
		t.Errorf("без срабатывания порога пометки быть не должно, got %q", res.Diff[0].Note)
	}
}

// TestBulk_Rent — «Аренда» (ТЗ §5): Фикс + MAX(0; Выручка − Порог) × Ставка;
// без параметров оборотной части — только факт предыдущего периода с пометкой.
func TestBulk_Rent(t *testing.T) {
	ser := NewRetailSeries()
	row := rowWithCell(100, LFLYes, 7, 5000, ValueManual)
	ctx := bulkCtx([]RetailRow{row}, ser, []RetailParam{
		{ParamCode: ParamRentThresh, ScopeKind: ParamScopeCountry, ScopeValue: "BY", Value: 4000},
		{ParamCode: ParamRentRate, ScopeKind: ParamScopeCountry, ScopeValue: "BY", Value: 0.05},
	})
	ctx.RentPrevFact = map[int]float64{100: 700}

	req := RetailBulkRequest{Op: BulkRent, Scope: BulkScopeAll, Months: []int{7}, Preview: true}
	res, err := ApplyRetailBulk(req, ctx)
	if err != nil {
		t.Fatalf("операция вернула ошибку: %v", err)
	}
	// 700 + MAX(0; 5000 − 4000) × 0.05 = 700 + 50 = 750.
	if math.Abs(res.Diff[0].After-750) > 1e-9 {
		t.Errorf("аренда: got %.2f, want 750", res.Diff[0].After)
	}

	// Этап 1 без параметров оборотной части: только фикс, с пометкой.
	ctx2 := bulkCtx([]RetailRow{row}, ser, nil)
	ctx2.RentPrevFact = map[int]float64{100: 700}
	res, err = ApplyRetailBulk(req, ctx2)
	if err != nil {
		t.Fatalf("операция без параметров вернула ошибку: %v", err)
	}
	if math.Abs(res.Diff[0].After-700) > 1e-9 {
		t.Errorf("аренда без ставки: got %.2f, want 700 (только факт пред. периода)", res.Diff[0].After)
	}
	if res.Diff[0].Note == "" {
		t.Error("без параметров оборотной части ячейка должна нести пометку (ТЗ §5)")
	}
}

// TestBulk_ScopeSelection — области применения (ТЗ §5): all / filtered / selected.
func TestBulk_ScopeSelection(t *testing.T) {
	ser := NewRetailSeries()
	ser.AddStrategy([]RetailFactCell{
		{CodeCFO: 100, Year: 2026, Month: 7, Amount: 10},
		{CodeCFO: 200, Year: 2026, Month: 7, Amount: 20},
	})
	rows := []RetailRow{{CodeCFO: 100, LFLEffective: LFLYes}, {CodeCFO: 200, LFLEffective: LFLYes}}

	base := RetailBulkRequest{Op: BulkCopyScenario, Source: CopyFromStrategy,
		SourceYear: 2026, Months: []int{7}, Preview: true}

	all := base
	all.Scope = BulkScopeAll
	res, _ := ApplyRetailBulk(all, bulkCtx(rows, ser, nil))
	if res.RowsIn != 2 || res.Changed != 2 {
		t.Errorf("scope=all: строк %d, изменений %d, ожидалось 2/2", res.RowsIn, res.Changed)
	}

	sel := base
	sel.Scope = BulkScopeSelected
	sel.CodeCFOs = []int{200}
	res, _ = ApplyRetailBulk(sel, bulkCtx(rows, ser, nil))
	if res.RowsIn != 1 || res.Changed != 1 || res.Diff[0].CodeCFO != 200 {
		t.Errorf("scope=selected: строк %d, изменений %d", res.RowsIn, res.Changed)
	}

	// selected без списка магазинов — понятная ошибка, а не пустой diff.
	bad := base
	bad.Scope = BulkScopeSelected
	if _, err := ApplyRetailBulk(bad, bulkCtx(rows, ser, nil)); err == nil {
		t.Error("scope=selected без code_cfos должен возвращать ошибку")
	}
}

// TestBulk_CopyScenarioCoefficient — коэффициент операции (ТЗ §5): × k и + x %.
func TestBulk_CopyScenarioCoefficient(t *testing.T) {
	ser := NewRetailSeries()
	ser.AddFact([]RetailFactCell{{CodeCFO: 100, Year: 2025, Month: 7, Amount: 1000}})
	rows := []RetailRow{{CodeCFO: 100, LFLEffective: LFLYes}}

	req := RetailBulkRequest{Op: BulkCopyScenario, Scope: BulkScopeAll, Source: CopyFromFact,
		SourceYear: 2025, Months: []int{7}, Coefficient: 2, PercentDelta: 0.1, Preview: true}
	res, err := ApplyRetailBulk(req, bulkCtx(rows, ser, nil))
	if err != nil {
		t.Fatalf("операция вернула ошибку: %v", err)
	}
	// 1000 × 2 × 1.1 = 2200.
	if math.Abs(res.Diff[0].After-2200) > 1e-9 {
		t.Errorf("копирование с коэффициентом: got %.2f, want 2200", res.Diff[0].After)
	}
}

// TestValidateBulkRequest — понятные ошибки запроса до расчёта.
func TestValidateBulkRequest(t *testing.T) {
	cases := []struct {
		name    string
		req     RetailBulkRequest
		wantErr bool
	}{
		{"пустая операция", RetailBulkRequest{}, true},
		{"неизвестная операция", RetailBulkRequest{Op: "wat"}, true},
		{"неизвестная область", RetailBulkRequest{Op: BulkPayroll, Scope: "wat"}, true},
		{"неизвестная база индекса", RetailBulkRequest{Op: BulkSalesIndex, IndexBase: "wat"}, true},
		{"копирование без источника", RetailBulkRequest{Op: BulkCopyScenario}, true},
		{"корректный индекс", RetailBulkRequest{Op: BulkSalesIndex, Scope: BulkScopeAll}, false},
	}
	for _, c := range cases {
		err := ValidateBulkRequest(c.req)
		if (err != nil) != c.wantErr {
			t.Errorf("%s: err=%v, ожидалась ошибка=%v", c.name, err, c.wantErr)
		}
	}
}
