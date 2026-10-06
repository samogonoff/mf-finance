package plans

import "testing"

func TestRetailComparisonFactMissingDoesNotBecomeZero(t *testing.T) {
	s := NewRetailSeries()
	s.AddFact([]RetailFactCell{{CodeCFO: 101, Year: 2025, Month: 7, Amount: 900}})
	s.AddComparisonFact(nil) // Budgeting answered successfully, but has no row.
	r := RetailRow{CodeCFO: 101, LFLEffective: LFLYes}
	i := ComputeRetailIndicators(r, s, 2026, 7, []int{7})
	if i.FactPrevYearMonth != nil || i.FactPrevMonth != nil {
		t.Fatalf("missing Budgeting fact must stay null, got %+v", i)
	}
	s.AddComparisonFact([]RetailFactCell{{CodeCFO: 101, Year: 2025, Month: 7, Amount: 1000}})
	i = ComputeRetailIndicators(r, s, 2026, 7, []int{7})
	if i.FactPrevYearMonth == nil || *i.FactPrevYearMonth != 1000 {
		t.Fatalf("comparison must use Budgeting fact, got %+v", i.FactPrevYearMonth)
	}
}
