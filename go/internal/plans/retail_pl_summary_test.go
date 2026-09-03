package plans

import (
	"math"
	"testing"
)

// Лист «результат ввода в BYN» (правки ТЗ §27/§28): вся структура PL, значения в
// BYN, месяцы по столбцам, USD-блока нет.

func plSummaryInput() RetailPLInput {
	return RetailPLInput{
		CardID: 7, Country: "RU", Year: 2026, Months: []int{7, 8},
		FxRate: 0.0376, FxSource: "факт: SUM(NetsalesCurrencyBelRubFact)/SUM(NetsalesCurrencyFact)",
		Lines: []PLLineRow{
			{CodePL: 1001, Name: "ПРОДАЖИ", GroupPL: "ПРОДАЖИ"},
			{CodePL: 54, Name: "Аренда помещений (стоянки)", GroupPL: "АРЕНДА"},
			{CodePL: 55, Name: "Аренда имущества", GroupPL: "АРЕНДА"},
			{CodePL: 24, Name: "Заработная плата", GroupPL: "ВЫПЛАТЫ РАБОТНИКАМ"},
		},
		Values: map[RetailPLKey]float64{
			{CodePL: 1001, Month: 7}: 1000,
			{CodePL: 1001, Month: 8}: 2000,
			{CodePL: 54, Month: 7}:   100,
		},
		Sources: map[int]string{1001: "plan", 54: "plan"},
	}
}

// Структура отдаётся ЦЕЛИКОМ, включая статьи без значений: по листу согласующий
// проверяет, что статья не забыта, а не только смотрит заполненные.
func TestRetailPLSummary_AllLinesIncludingEmpty(t *testing.T) {
	sum := ComputeRetailPLSummary(plSummaryInput())
	if len(sum.Rows) != 4 {
		t.Fatalf("строк: got %d, want 4 (вся структура PL, включая нулевые)", len(sum.Rows))
	}
	if sum.Currency != "BYN" {
		t.Fatalf("лист именно в BYN, got %q", sum.Currency)
	}
	// Статья без значений: карта месяцев пустая, а не нули — «нет строки» и «ноль»
	// в PL значат разное.
	for _, r := range sum.Rows {
		if r.CodePL == 55 && len(r.Months) != 0 {
			t.Fatalf("статья 55 без данных не должна получать нули: %+v", r.Months)
		}
	}
}

// Значения пересчитываются в BYN курсом ИЗ ФАКТА, и курс виден в ответе.
func TestRetailPLSummary_ConvertedToBYNWithFactRate(t *testing.T) {
	sum := ComputeRetailPLSummary(plSummaryInput())
	var sales RetailPLRow
	for _, r := range sum.Rows {
		if r.CodePL == 1001 {
			sales = r
		}
	}
	if math.Abs(sales.Months[7]-37.6) > 1e-9 {
		t.Errorf("июль: got %.4f, want 37.60 (1000 × 0,0376)", sales.Months[7])
	}
	if math.Abs(sales.Months[8]-75.2) > 1e-9 {
		t.Errorf("август: got %.4f, want 75.20 (2000 × 0,0376)", sales.Months[8])
	}
	if math.Abs(sales.Total-112.8) > 1e-9 {
		t.Errorf("итого: got %.4f, want 112.80", sales.Total)
	}
	if sum.FxRate != 0.0376 || sum.FxSource == "" {
		t.Errorf("курс и его источник обязаны быть в ответе: %.4f %q", sum.FxRate, sum.FxSource)
	}
}

// Порядок: группы в порядке справочника, внутри группы — по коду статьи.
// Стабильный порядок важен: иначе таблица «прыгает» между запросами.
func TestRetailPLSummary_StableOrderByGroupThenCode(t *testing.T) {
	sum := ComputeRetailPLSummary(plSummaryInput())
	wantGroups := []string{"ПРОДАЖИ", "АРЕНДА", "ВЫПЛАТЫ РАБОТНИКАМ"}
	if len(sum.Groups) != len(wantGroups) {
		t.Fatalf("групп: got %v, want %v", sum.Groups, wantGroups)
	}
	for i, g := range wantGroups {
		if sum.Groups[i] != g {
			t.Fatalf("порядок групп: got %v, want %v", sum.Groups, wantGroups)
		}
	}
	wantCodes := []int{1001, 54, 55, 24}
	for i, code := range wantCodes {
		if sum.Rows[i].CodePL != code {
			t.Fatalf("строка %d: got код %d, want %d", i, sum.Rows[i].CodePL, code)
		}
	}
}

// Итоги по группам — для строк-заголовков листа.
func TestRetailPLGroupTotals(t *testing.T) {
	totals := RetailPLGroupTotals(ComputeRetailPLSummary(plSummaryInput()))
	if math.Abs(totals["ПРОДАЖИ"]-112.8) > 1e-9 {
		t.Errorf("итог ПРОДАЖИ: got %.4f, want 112.80", totals["ПРОДАЖИ"])
	}
	if math.Abs(totals["АРЕНДА"]-3.76) > 1e-9 {
		t.Errorf("итог АРЕНДА: got %.4f, want 3.76 (100 × 0,0376)", totals["АРЕНДА"])
	}
}

// Курс не задан — работаем без пересчёта, а не умножаем на ноль.
func TestRetailPLSummary_ZeroRateMeansNoConversion(t *testing.T) {
	in := plSummaryInput()
	in.FxRate = 0
	sum := ComputeRetailPLSummary(in)
	for _, r := range sum.Rows {
		if r.CodePL == 1001 && math.Abs(r.Months[7]-1000) > 1e-9 {
			t.Fatalf("без курса значение должно остаться как есть, got %.2f", r.Months[7])
		}
	}
	if sum.FxRate != 1 {
		t.Fatalf("в ответе должен быть применённый курс 1, got %.4f", sum.FxRate)
	}
}
