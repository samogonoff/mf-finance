package plans

import (
	"context"
	"math"
	"testing"
)

// Тесты экрана согласования (ТЗ §8) и контрольных сверок (ТЗ §7). Без БД.

// summaryInput — вход с двумя LFL-магазинами и одним новым.
func summaryInput(rows []RetailRow, ser RetailSeries) RetailSummaryInput {
	return RetailSummaryInput{
		CardID: 1, Country: "BY", NatCurrency: "BYN", Year: 2026, Month: 7,
		PlanMonths: []int{7, 8}, Rows: rows, Series: ser,
		FxRates: map[string]float64{"BYN": 1, "USD": 0.3}, DirectoryActiveCount: -1,
	}
}

// TestSummary_CurrencyBlocksAndFieldSets — три валютных блока (ТЗ §8): нац.
// валюта — все 11 показателей, BYN и USD — сокращённый набор из 6.
func TestSummary_CurrencyBlocksAndFieldSets(t *testing.T) {
	ser := NewRetailSeries()
	ser.AddFact([]RetailFactCell{{CodeCFO: 100, Year: 2025, Month: 7, Amount: 1000}})
	rows := []RetailRow{retailFilledRow(100, LFLYes, []int{7, 8}, 1200)}

	sum := ComputeRetailSummary(summaryInput(rows, ser))

	// Нац. валюта BY — BYN, поэтому дубль блока не создаётся: BYN + USD = 2 блока.
	if len(sum.Blocks) != 2 {
		t.Fatalf("валютных блоков: got %d, want 2 (BYN нац. + USD)", len(sum.Blocks))
	}
	nat := sum.Blocks[0]
	if nat.Currency != "BYN" || !nat.Full {
		t.Errorf("первый блок должен быть нац. валютой с полным набором, got %+v", nat.Currency)
	}
	if len(nat.Fields) != 11 {
		t.Errorf("нац. блок: показателей %d, ТЗ §8 требует 11", len(nat.Fields))
	}
	usd := sum.Blocks[1]
	if usd.Currency != "USD" || usd.Full {
		t.Errorf("второй блок должен быть USD с сокращённым набором, got %q full=%v", usd.Currency, usd.Full)
	}
	if len(usd.Fields) != 6 {
		t.Errorf("USD-блок: показателей %d, ТЗ §8 требует 6", len(usd.Fields))
	}

	// Обязательные разрезы ТЗ §8: LFL / до года / новые / закрыты.
	if len(nat.Sections) != 4 {
		t.Fatalf("разрезов: got %d, want 4", len(nat.Sections))
	}
	wantKeys := []string{"lfl", "under1y", "new", "closed"}
	for i, k := range wantKeys {
		if nat.Sections[i].Key != k {
			t.Errorf("разрез %d: got %q, want %q", i, nat.Sections[i].Key, k)
		}
	}

	// Дополнительные группировки ТЗ §8: город, РМ, тип, ЮЛ, категория.
	if len(sum.Groups) != 5 {
		t.Errorf("дополнительных группировок: got %d, want 5", len(sum.Groups))
	}
}

// TestSummary_MoneyScaledPercentsNot — в валютном блоке пересчитываются только
// денежные показатели; проценты остаются теми же (ТЗ §4/§8).
func TestSummary_MoneyScaledPercentsNot(t *testing.T) {
	ser := NewRetailSeries()
	ser.AddFact([]RetailFactCell{{CodeCFO: 100, Year: 2025, Month: 7, Amount: 1000}})
	rows := []RetailRow{retailFilledRow(100, LFLYes, []int{7, 8}, 1200)}

	sum := ComputeRetailSummary(summaryInput(rows, ser))
	nat, usd := sum.Blocks[0].Total, sum.Blocks[1].Total

	if math.Abs(nat.Tactic-1200) > 1e-9 {
		t.Errorf("тактика в нац. валюте: got %.2f, want 1200", nat.Tactic)
	}
	if math.Abs(usd.Tactic-360) > 1e-6 {
		t.Errorf("тактика в USD (курс 0.3): got %.2f, want 360", usd.Tactic)
	}
	// LFL = 1200/1000 − 1 = 0.2 в ОБОИХ блоках.
	if math.Abs(nat.LFL-0.2) > 1e-9 || math.Abs(usd.LFL-0.2) > 1e-9 {
		t.Errorf("LFL должен быть одинаков в блоках: нац. %.4f, USD %.4f", nat.LFL, usd.LFL)
	}
}

// TestSummary_AggregateRatiosFromTotals — проценты считаются от агрегатов, а не
// как среднее построчных процентов.
func TestSummary_AggregateRatiosFromTotals(t *testing.T) {
	ser := NewRetailSeries()
	ser.AddFact([]RetailFactCell{
		{CodeCFO: 100, Year: 2025, Month: 7, Amount: 100},
		{CodeCFO: 200, Year: 2025, Month: 7, Amount: 900},
	})
	rows := []RetailRow{
		retailFilledRow(100, LFLYes, []int{7, 8}, 200), // LFL строки +100 %
		retailFilledRow(200, LFLYes, []int{7, 8}, 900), // LFL строки 0 %
	}
	sum := ComputeRetailSummary(summaryInput(rows, ser))
	// Агрегат: тактика 1100, факт ПГ 1000 → LFL = 0.1.
	// Среднее построчных дало бы 0.5 — это и есть та ошибка, которую тест ловит.
	if math.Abs(sum.Blocks[0].Total.LFL-0.1) > 1e-9 {
		t.Errorf("LFL от агрегатов: got %.4f, want 0.1", sum.Blocks[0].Total.LFL)
	}
}

// TestReconciliations_AllZeroOnConsistentForm — четыре сверки §7 дают 0 на
// согласованной форме.
func TestReconciliations_AllZeroOnConsistentForm(t *testing.T) {
	ser := NewRetailSeries()
	ser.AddFact([]RetailFactCell{
		{CodeCFO: 100, Year: 2026, Month: 7, Amount: 500},
		{CodeCFO: 200, Year: 2026, Month: 7, Amount: 700},
	})
	rows := []RetailRow{
		retailFilledRow(100, LFLYes, []int{7, 8}, 1000),
		retailFilledRow(200, LFLUnder1, []int{7, 8}, 2000),
	}
	in := summaryInput(rows, ser)
	sourceTotal := 1200.0 // 500 + 700 — тот же итог, что в срезе факта
	in.SourceFactTotal = &sourceTotal
	in.DirectoryActiveCount = 2

	sum := ComputeRetailSummary(in)
	if len(sum.Reconciliations) != 4 {
		t.Fatalf("сверок: got %d, want 4 (ТЗ §7)", len(sum.Reconciliations))
	}
	for _, r := range sum.Reconciliations {
		if r.Skipped {
			t.Errorf("сверка %s пропущена, хотя данные есть", r.Code)
			continue
		}
		if !r.OK {
			t.Errorf("сверка %s должна давать 0: left=%.2f right=%.2f diff=%.2f",
				r.Code, r.Left, r.Right, r.Diff)
		}
	}
}

// TestReconciliations_R04DirectoryCompleteness — R-04: полнота справочника
// (активных CodeCFO в источнике − строк формы) с расшифровкой при расхождении.
func TestReconciliations_R04DirectoryCompleteness(t *testing.T) {
	ser := NewRetailSeries()
	rows := []RetailRow{retailFilledRow(100, LFLYes, []int{7, 8}, 10)}
	in := summaryInput(rows, ser)
	in.DirectoryActiveCount = 3 // в справочнике 3, в форме 1 → расхождение 2

	sum := ComputeRetailSummary(in)
	var r04 *RetailReconciliation
	for i := range sum.Reconciliations {
		if sum.Reconciliations[i].Code == "R-04" {
			r04 = &sum.Reconciliations[i]
		}
	}
	if r04 == nil {
		t.Fatal("сверка R-04 отсутствует")
	}
	if r04.OK || math.Abs(r04.Diff-2) > 1e-9 {
		t.Errorf("R-04: ok=%v diff=%.2f, ожидалось ok=false diff=2", r04.OK, r04.Diff)
	}
}

// TestReconciliations_SkippedWhenSourceUnavailable — недоступный источник
// помечается skipped, а не «расхождение 0» (иначе согласующий увидит зелёный
// отчёт на пустых данных).
func TestReconciliations_SkippedWhenSourceUnavailable(t *testing.T) {
	ser := NewRetailSeries()
	rows := []RetailRow{retailFilledRow(100, LFLYes, []int{7, 8}, 10)}
	in := summaryInput(rows, ser) // SourceFactTotal=nil, DirectoryActiveCount=-1

	sum := ComputeRetailSummary(in)
	skipped := map[string]bool{}
	for _, r := range sum.Reconciliations {
		if r.Skipped {
			skipped[r.Code] = true
			if r.OK {
				t.Errorf("сверка %s помечена skipped, но ok=true", r.Code)
			}
		}
	}
	for _, code := range []string{"R-02", "R-04"} {
		if !skipped[code] {
			t.Errorf("сверка %s должна быть skipped при недоступном источнике", code)
		}
	}
}

// TestReconciliations_R03SectionCoverage — R-03: разрезы мастер-представления
// покрывают все строки без пересечений. Магазин с неизвестным LFL-статусом не
// попадает ни в один разрез — сверка это ловит с расшифровкой.
func TestReconciliations_R03SectionCoverage(t *testing.T) {
	ser := NewRetailSeries()
	orphan := retailFilledRow(100, "какой-то_новый_статус", []int{7, 8}, 500)
	sum := ComputeRetailSummary(summaryInput([]RetailRow{orphan}, ser))

	var r03 *RetailReconciliation
	for i := range sum.Reconciliations {
		if sum.Reconciliations[i].Code == "R-03" {
			r03 = &sum.Reconciliations[i]
		}
	}
	if r03 == nil {
		t.Fatal("сверка R-03 отсутствует")
	}
	if r03.OK {
		t.Error("R-03 должна не сойтись: строка не попала ни в один разрез")
	}
	if len(r03.Details) == 0 {
		t.Error("ненулевая сверка обязана отдавать расшифровку по строкам (ТЗ §7)")
	}
}

// TestMockRetailSource — фикстуры отдают консистентный набор (ТЗ §11:
// «обязательный mock»), на котором работают все ветки расчёта.
func TestMockRetailSource(t *testing.T) {
	src := NewMockRetailSource()
	ctx := context.Background()

	stores, err := src.Stores(ctx, "BY")
	if err != nil {
		t.Fatalf("магазины mock: %v", err)
	}
	if len(stores) < 15 {
		t.Errorf("в фикстуре %d магазинов, ожидалось не менее 15", len(stores))
	}
	// Все статусы, на которых ветвится расчёт, должны присутствовать.
	seen := map[string]bool{}
	for _, s := range stores {
		seen[s.LFLStatus] = true
		if s.CodeCFO == 0 {
			t.Error("в фикстуре есть магазин без CodeCFO — это ключ строки (V-04)")
		}
		if s.CompanyMF != "F" {
			t.Errorf("магазин %d: ЮЛ %q, у BY должно быть F (V-08)", s.CodeCFO, s.CompanyMF)
		}
	}
	for _, st := range []string{LFLYes, LFLUnder1, LFLNew, LFLNoID, LFLClosed} {
		if !seen[st] {
			t.Errorf("в фикстуре нет магазина со статусом %q", st)
		}
	}

	// Другая страна — пусто (mock покрывает РБ).
	if other, _ := src.Stores(ctx, "KZ"); len(other) != 0 {
		t.Errorf("для KZ фикстура должна быть пустой, got %d", len(other))
	}

	fact, err := src.Fact(ctx, "BY", []int{2025, 2026})
	if err != nil {
		t.Fatalf("факт mock: %v", err)
	}
	if len(fact) == 0 {
		t.Fatal("факт фикстуры пуст")
	}
	// Факт не должен появляться до открытия и после закрытия магазина.
	byCode := map[int]RetailStore{}
	for _, s := range stores {
		byCode[s.CodeCFO] = s
	}
	for _, c := range fact {
		s := byCode[c.CodeCFO]
		if retailStoreOpenedAfter(s.DateOpen, c.Year, c.Month) {
			t.Errorf("магазин %d: факт за %d-%02d раньше открытия %s", c.CodeCFO, c.Year, c.Month, s.DateOpen)
		}
		if s.DateClose != "" && retailStoreClosedBefore(s.DateClose, c.Year, c.Month) {
			t.Errorf("магазин %d: факт за %d-%02d после закрытия %s", c.CodeCFO, c.Year, c.Month, s.DateClose)
		}
	}

	// Стратегия и история плана есть, KLIENT_ID подтягивается из истории (§2).
	if strat, _ := src.Strategy(ctx, "BY", 2026); len(strat) == 0 {
		t.Error("стратегия фикстуры пуста")
	}
	hist, klient, err := src.PlanHistory(ctx, "BY", 2026)
	if err != nil {
		t.Fatalf("история плана mock: %v", err)
	}
	if len(hist) == 0 || len(klient) == 0 {
		t.Error("история плана или соответствие CodeCFO→KLIENT_ID пусты")
	}
}

// TestRetailStoreProvider — провайдер синхронизации справочника (ТЗ §11).
func TestRetailStoreProvider(t *testing.T) {
	p := NewRetailStoreProvider(NewMockRetailSource())
	if p.Code() != "dir_retail_store" {
		t.Errorf("код справочника: got %q", p.Code())
	}
	rows, err := p.Fetch(context.Background())
	if err != nil {
		t.Fatalf("Fetch: %v", err)
	}
	if len(rows) == 0 {
		t.Fatal("провайдер не вернул строк")
	}
	for _, r := range rows {
		if r.ExternalID == "" {
			t.Error("строка справочника без external_id: диффы sync.go по нему считаются")
		}
		if _, ok := r.Payload["reg_manager"]; !ok {
			t.Error("в payload нет reg_manager — без него не работают права РМ (§9)")
		}
	}

	// Без источника — понятная ошибка, а не паника.
	if _, err := NewRetailStoreProvider(nil).Fetch(context.Background()); err == nil {
		t.Error("провайдер без источника должен возвращать ошибку")
	}
}

// TestParamSet_ResolvePriority — приоритет параметров периода от частного к
// общему (ТЗ §5).
func TestParamSet_ResolvePriority(t *testing.T) {
	row := RetailRow{CodeCFO: 100, City: "Минск", StoreType: "ТЦ", LFLEffective: LFLYes}
	ps := RetailParamSet{Params: []RetailParam{
		{ParamCode: ParamSalesIndex, ScopeKind: ParamScopeCountry, ScopeValue: "BY", Value: 0.01},
		{ParamCode: ParamSalesIndex, ScopeKind: ParamScopeCity, ScopeValue: "Минск", Value: 0.02},
		{ParamCode: ParamSalesIndex, ScopeKind: ParamScopeLFL, ScopeValue: LFLYes, Value: 0.03},
		{ParamCode: ParamSalesIndex, ScopeKind: ParamScopeStoreType, ScopeValue: "ТЦ", Value: 0.04},
		{ParamCode: ParamSalesIndex, ScopeKind: ParamScopeStore, ScopeValue: "100", Value: 0.05},
	}}
	// Полный набор → выигрывает магазин.
	if v, ok := ps.Resolve(ParamSalesIndex, row, "BY"); !ok || math.Abs(v-0.05) > 1e-9 {
		t.Errorf("приоритет «магазин»: got %.4f ok=%v, want 0.05", v, ok)
	}
	// Убираем уровни по одному — каждый следующий должен подхватываться.
	wants := []float64{0.04, 0.03, 0.02, 0.01}
	for i, want := range wants {
		ps.Params = ps.Params[:len(ps.Params)-1]
		_ = i
		if v, ok := ps.Resolve(ParamSalesIndex, row, "BY"); !ok || math.Abs(v-want) > 1e-9 {
			t.Errorf("после снятия уровня: got %.4f ok=%v, want %.4f", v, ok, want)
		}
	}
	// Ничего не задано → дефолт вызывающего.
	ps.Params = nil
	if _, ok := ps.Resolve(ParamSalesIndex, row, "BY"); ok {
		t.Error("при отсутствии параметра Resolve должен возвращать ok=false")
	}
	if v := ps.ResolveOr(ParamPayrollCap, row, "BY", DefaultPayrollCap); math.Abs(v-1.06) > 1e-9 {
		t.Errorf("дефолт порога ФОТ: got %.4f, want 1.06", v)
	}
}

// TestRetailImport_UnknownStoreGoesToReport — импорт (ТЗ §5): строки вне
// справочника попадают в отчёт об ошибках, а не игнорируются молча.
func TestRetailImport_UnknownStoreGoesToReport(t *testing.T) {
	header := []string{"code_cfo", "2026-07", "2026-08"}
	rows := [][]string{
		{"100", "1000", "1100"}, // известный магазин — сохраняем
		{"999", "500", "600"},   // вне формы → в отчёт
		{"100", "-5", ""},       // отрицательное → в отчёт (V-02)
		{"100", "", "не число"}, // нечисловое → в отчёт
	}
	data, err := writeXlsx("Розница BY", header, rows)
	if err != nil {
		t.Fatalf("writeXlsx: %v", err)
	}

	cells, issues, inFile, err := parseRetailImport(data, 2026, map[int]bool{7: true, 8: true},
		map[int]bool{100: true}, 0)
	if err != nil {
		t.Fatalf("parseRetailImport: %v", err)
	}
	if inFile != len(rows) {
		t.Errorf("строк в файле: got %d, want %d", inFile, len(rows))
	}
	// Первая строка даёт 2 ячейки, третья и четвёртая — по проблеме.
	if len(cells) != 2 {
		t.Errorf("сохранено ячеек: got %d, want 2 (%+v)", len(cells), cells)
	}
	if len(issues) != 3 {
		t.Errorf("проблем в отчёте: got %d, want 3 (%+v)", len(issues), issues)
	}
	// Неизвестный магазин обязан быть НАЗВАН в отчёте.
	found := false
	for _, i := range issues {
		if i.CodeCFO == 999 {
			found = true
		}
	}
	if !found {
		t.Error("магазин вне справочника должен попасть в отчёт об ошибках (ТЗ §5)")
	}
	for _, c := range cells {
		if c.Source != ValueImport {
			t.Errorf("source импортированной ячейки: got %q, want %q", c.Source, ValueImport)
		}
	}
}

// TestRetailImport_MonthOutsidePlanPeriod — колонка вне планового периода → в отчёт.
func TestRetailImport_MonthOutsidePlanPeriod(t *testing.T) {
	data, err := writeXlsx("Розница BY", []string{"code_cfo", "2026-06"}, [][]string{{"100", "10"}})
	if err != nil {
		t.Fatalf("writeXlsx: %v", err)
	}
	cells, issues, _, err := parseRetailImport(data, 2026, map[int]bool{7: true},
		map[int]bool{100: true}, 0)
	if err != nil {
		t.Fatalf("parseRetailImport: %v", err)
	}
	if len(cells) != 0 || len(issues) != 1 {
		t.Errorf("ячеек %d, проблем %d — ожидалось 0/1 (V-10)", len(cells), len(issues))
	}
}

// TestRetailImport_NoMonthColumns — файл без колонок-месяцев отклоняется целиком:
// это не «нечего импортировать», а неверный шаблон.
func TestRetailImport_NoMonthColumns(t *testing.T) {
	data, _ := writeXlsx("Розница", []string{"code_cfo", "cfo"}, [][]string{{"100", "Магазин"}})
	if _, _, _, err := parseRetailImport(data, 2026, map[int]bool{7: true}, map[int]bool{100: true}, 0); err == nil {
		t.Error("файл без колонок-месяцев должен отклоняться с ошибкой")
	}
}
