package plans

import "testing"

func TestCfoExpSpecs(t *testing.T) {
	want := map[CfoExpFormCode]struct {
		country, fact, plan string
		years               int
		le, half, expect    bool
	}{
		31: {"BY", "Факт по платежам", "Стратегический бюджет", 3, false, false, true}, 32: {"BY", "Факт по начислениям", "Тактика бюджет (таргеты)", 1, true, false, false}, 33: {"BY", "Факт по начислениям", "Тактика бюджет (таргеты)", 3, true, true, true}, 34: {"BY", "Факт по платежам", "Стратегический бюджет", 3, false, false, true}, 37: {"BY", "Факт по начислениям", "Тактика бюджет (таргеты)", 3, true, true, true}, 962: {"RU", "Факт по начислениям", "Тактика бюджет (таргеты)", 3, true, true, true},
	}
	for code, w := range want {
		s, ok := LookupCfoExpSpec(code)
		if !ok || s.Country != w.country || s.FactScenario != w.fact || s.PlanScenario != w.plan || len(s.FactYears) != w.years || s.HasLegalEntity != w.le || s.HalfYearInput != w.half || s.HasExpectation != w.expect {
			t.Errorf("%d: %+v", code, s)
		}
	}
	s, _ := LookupCfoExpSpec(CfoExp33)
	if s.InfoFactScenario != "Факт по платежам" {
		t.Fatalf("form 33 INFO fact scenario = %q", s.InfoFactScenario)
	}
}
func TestParseScope(t *testing.T) {
	for _, tc := range []struct {
		in      string
		code    CfoExpFormCode
		country string
		ok      bool
	}{{"CFO-31-BY", 31, "BY", true}, {"CFO-962-RU", 962, "RU", true}, {"cfo-31-by", 31, "BY", true}, {"CFO-31-RU", 0, "", false}, {"CFO-99-BY", 0, "", false}} {
		c, co, ok := ParseCfoExpScope(tc.in)
		if c != tc.code || co != tc.country || ok != tc.ok {
			t.Errorf("%q => %d,%s,%v", tc.in, c, co, ok)
		}
	}
	if got, ok := CfoExpCodeFromKind(CfoExpKind(CfoExp37)); !ok || got != CfoExp37 {
		t.Fatalf("kind round trip: %v %v", got, ok)
	}
}
func TestExpectationAndDeviation(t *testing.T) {
	got := CfoExpMonthExpectation([]float64{1, 2, 3}, []float64{10, 20, 30}, []bool{true, false, true})
	want := []float64{1, 20, 3}
	for i := range want {
		if got[i] != want[i] {
			t.Fatal(got)
		}
	}
	if CfoExpDeviation(110, 100) < .0999 || CfoExpDeviation(110, 100) > .1001 || CfoExpDeviation(9, 0) != 0 {
		t.Fatal("deviation")
	}
	if got := CfoExpPreviousExpectation(
		[]float64{10, 20, 30}, []float64{1, 2, 3},
		[]bool{true, false, true}, []bool{false, true, true},
	); got != 45 {
		t.Fatalf("previous expectation = %v, want 45", got)
	}
}
func TestValidateCfoExp(t *testing.T) {
	neg := -2.
	zero := 0.
	issues := ValidateCfoExp(CfoExpValidationInput{PlanMonths: []CfoExpCell{{Month: 1, Amount: &neg}, {Month: 2, Amount: &zero}}, OpenMonths: []bool{true, true, true}, Country: "RU", ExpectedCountry: "BY", Returned: true, RegistryTotal: func() *float64 { x := 102.; return &x }(), InputTotal: func() *float64 { x := 100.; return &x }(), FxFound: false})
	seen := map[string]bool{}
	for _, i := range issues {
		seen[i.Code] = true
	}
	for _, c := range []string{"V-01", "V-02", "V-05", "V-06", "V-07"} {
		if !seen[c] {
			t.Errorf("missing %s", c)
		}
	}
	if seen["V-08"] {
		t.Error("currency check must be skipped when FX is not required")
	}
	allowed := ValidateCfoExp(CfoExpValidationInput{
		PlanMonths: []CfoExpCell{{Month: 1, Amount: &neg, AllowNegative: true}},
		OpenMonths: []bool{true}, FxFound: true,
	})
	for _, i := range allowed {
		if i.Code == "V-02" {
			t.Fatal("explicitly permitted сторно must not trigger V-02")
		}
	}
}
