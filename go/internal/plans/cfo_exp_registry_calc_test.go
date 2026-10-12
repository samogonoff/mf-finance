package plans

import "testing"

func TestCfoExpenseRegistryTravelCalc(t *testing.T) {
	r := CfoExpenseRegistryRow{Attrs: map[string]any{"currency": "USD", "fx": 3, "people": 2, "tickets": 100, "lodging": 50, "nights": 2, "per_diem": 20, "days": 3}}
	got, issues := CfoExpenseRegistryCalc("travel", r, nil, nil)
	if len(issues) != 0 || got != 1260 {
		t.Fatalf("got %v issues=%v", got, issues)
	}
}
func TestCfoExpenseRegistryRoutesAndNullMonths(t *testing.T) {
	v := 12.0
	r := CfoExpenseRegistryRow{Attrs: map[string]any{"trips_week": 2, "price": 10, "rate": 3}, Months: map[int]*float64{1: &v, 2: nil}}
	got, _ := CfoExpenseRegistryCalc("route93", r, nil, nil)
	if got != 2880 {
		t.Fatalf("route=%v", got)
	}
	if r.SumMonths() != 12 {
		t.Fatalf("nullable month sum")
	}
}
func TestCfoExpenseRegistryTravelMissingFXBlocks(t *testing.T) {
	r := CfoExpenseRegistryRow{Attrs: map[string]any{"currency": "USD", "people": 1, "tickets": 100}}
	_, issues := CfoExpenseRegistryCalc("travel", r, nil, nil)
	if len(issues) != 1 || issues[0].Code != "V-08" || !issues[0].Blocking {
		t.Fatalf("issues=%v", issues)
	}
}
