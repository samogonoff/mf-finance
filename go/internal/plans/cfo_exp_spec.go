package plans

import (
	"strconv"
	"strings"
)

// CfoExpFormCode identifies one CFO tactical-expense form from TЗ v2.
type CfoExpFormCode int

const (
	CfoExp31  CfoExpFormCode = 31
	CfoExp32  CfoExpFormCode = 32
	CfoExp33  CfoExpFormCode = 33
	CfoExp34  CfoExpFormCode = 34
	CfoExp37  CfoExpFormCode = 37
	CfoExp962 CfoExpFormCode = 962
)

// CfoExpSpec is the immutable, domain-level configuration of a form.
type CfoExpSpec struct {
	Code               CfoExpFormCode `json:"code"`
	Country            string         `json:"country"`
	FactScenario       string         `json:"fact_scenario"`
	InfoFactScenario   string         `json:"info_fact_scenario"`
	PlanScenario       string         `json:"plan_scenario"`
	FactYears          []int          `json:"fact_years"`
	ComparisonScenario string         `json:"comparison_scenario,omitempty"`
	ComparisonYears    []int          `json:"comparison_years,omitempty"`
	PlanYear           int            `json:"plan_year"`
	HasLegalEntity     bool           `json:"has_legal_entity"`
	HalfYearInput      bool           `json:"half_year_input"`
	HasExpectation     bool           `json:"has_expectation"`
}

var cfoExpSpecs = map[CfoExpFormCode]CfoExpSpec{
	CfoExp31:  {Code: CfoExp31, Country: "BY", FactScenario: "Факт по платежам", InfoFactScenario: "Факт по платежам", FactYears: []int{2024, 2025, 2026}, ComparisonScenario: "Стратегический бюджет", ComparisonYears: []int{2025}, PlanScenario: "Стратегический бюджет", PlanYear: 2026, HasExpectation: true},
	CfoExp32:  {Code: CfoExp32, Country: "BY", FactScenario: "Факт по начислениям", InfoFactScenario: "Факт по начислениям", FactYears: []int{2026}, ComparisonScenario: "Тактика бюджет (таргеты)", ComparisonYears: []int{2025}, PlanScenario: "Тактика бюджет (таргеты)", PlanYear: 2026, HasLegalEntity: true},
	CfoExp33:  {Code: CfoExp33, Country: "BY", FactScenario: "Факт по начислениям", InfoFactScenario: "Факт по платежам", FactYears: []int{2024, 2025, 2026}, ComparisonScenario: "Тактика бюджет (таргеты)", ComparisonYears: []int{2025}, PlanScenario: "Тактика бюджет (таргеты)", PlanYear: 2026, HasLegalEntity: true, HalfYearInput: true, HasExpectation: true},
	CfoExp34:  {Code: CfoExp34, Country: "BY", FactScenario: "Факт по платежам", InfoFactScenario: "Факт по платежам", FactYears: []int{2024, 2025, 2026}, ComparisonScenario: "Стратегический бюджет", ComparisonYears: []int{2025}, PlanScenario: "Стратегический бюджет", PlanYear: 2026, HasExpectation: true},
	CfoExp37:  {Code: CfoExp37, Country: "BY", FactScenario: "Факт по начислениям", InfoFactScenario: "Факт по начислениям", FactYears: []int{2024, 2025, 2026}, ComparisonScenario: "Тактика бюджет (таргеты)", ComparisonYears: []int{2025}, PlanScenario: "Тактика бюджет (таргеты)", PlanYear: 2026, HasLegalEntity: true, HalfYearInput: true, HasExpectation: true},
	CfoExp962: {Code: CfoExp962, Country: "RU", FactScenario: "Факт по начислениям", InfoFactScenario: "Факт по начислениям", FactYears: []int{2024, 2025, 2026}, ComparisonScenario: "Тактика бюджет (таргеты)", ComparisonYears: []int{2025}, PlanScenario: "Тактика бюджет (таргеты)", PlanYear: 2026, HasLegalEntity: true, HalfYearInput: true, HasExpectation: true},
}

// LookupCfoExpSpec returns a copy so callers cannot mutate the registry.
func LookupCfoExpSpec(code CfoExpFormCode) (CfoExpSpec, bool) {
	s, ok := cfoExpSpecs[code]
	if !ok {
		return CfoExpSpec{}, false
	}
	s.FactYears = append([]int(nil), s.FactYears...)
	return s, true
}

// CfoExpKind is the storage kind used by the shared expense-form service.
// Keeping it namespaced avoids colliding with the legacy retail/mp kinds.
func CfoExpKind(code CfoExpFormCode) string {
	return "cfo:" + strconv.Itoa(int(code))
}

// CfoExpCodeFromKind reverses CfoExpKind.
func CfoExpCodeFromKind(kind string) (CfoExpFormCode, bool) {
	if !strings.HasPrefix(strings.ToLower(strings.TrimSpace(kind)), "cfo:") {
		return 0, false
	}
	n, err := strconv.Atoi(strings.TrimSpace(kind)[4:])
	if err != nil {
		return 0, false
	}
	code := CfoExpFormCode(n)
	_, ok := LookupCfoExpSpec(code)
	return code, ok
}

// CfoExpSpecForScope returns the v2 form spec for a card scope, if present.
func CfoExpSpecForScope(scope string) (*CfoExpSpec, bool) {
	code, _, ok := ParseCfoExpScope(scope)
	if !ok {
		return nil, false
	}
	s, ok := LookupCfoExpSpec(code)
	if !ok {
		return nil, false
	}
	return &s, true
}

// ParseCfoExpScope parses CFO-31-BY and CFO-962-RU.
func ParseCfoExpScope(scope string) (CfoExpFormCode, string, bool) {
	p := strings.Split(strings.ToUpper(strings.TrimSpace(scope)), "-")
	if len(p) != 3 || p[0] != "CFO" {
		return 0, "", false
	}
	var code CfoExpFormCode
	switch p[1] {
	case "31":
		code = CfoExp31
	case "32":
		code = CfoExp32
	case "33":
		code = CfoExp33
	case "34":
		code = CfoExp34
	case "37":
		code = CfoExp37
	case "962":
		code = CfoExp962
	default:
		return 0, "", false
	}
	s, ok := cfoExpSpecs[code]
	if !ok || p[2] != s.Country {
		return 0, "", false
	}
	return code, p[2], true
}

// CfoExpMonthExpectation combines closed-month fact with open-month tactic.
func CfoExpMonthExpectation(fact2026, tactic2026 []float64, closed []bool) []float64 {
	n := len(fact2026)
	if len(tactic2026) > n {
		n = len(tactic2026)
	}
	if len(closed) > n {
		n = len(closed)
	}
	out := make([]float64, n)
	for i := range out {
		if i < len(closed) && closed[i] {
			if i < len(fact2026) {
				out[i] = fact2026[i]
			}
		} else if i < len(tactic2026) {
			out[i] = tactic2026[i]
		}
	}
	return out
}

// CfoExpDeviation computes value/base-1; zero base is defined as zero.
func CfoExpDeviation(value, base float64) float64 {
	if base == 0 {
		return 0
	}
	return value/base - 1
}

// CfoExpPreviousExpectation is the base used by forms 31 and 34:
// fact months with a fact flag plus strategy months with a strategy flag.
func CfoExpPreviousExpectation(fact2025, strategy2025 []float64, factFlags, strategyFlags []bool) float64 {
	n := len(fact2025)
	if len(strategy2025) > n {
		n = len(strategy2025)
	}
	var total float64
	for i := 0; i < n; i++ {
		if i < len(factFlags) && factFlags[i] && i < len(fact2025) {
			total += fact2025[i]
		}
		if i < len(strategyFlags) && strategyFlags[i] && i < len(strategy2025) {
			total += strategy2025[i]
		}
	}
	return total
}

type CfoExpCell struct {
	Month  int
	Amount *float64
	// AllowNegative is set only for explicitly permitted сторнирующие статьи.
	AllowNegative bool
}
type CfoExpValidationInput struct {
	PlanMonths                []CfoExpCell
	OpenMonths                []bool // indexed by month-1; true means input is open
	LegalEntity, Country      string
	ExpectedCountry           string
	Returned                  bool
	ReturnComment             string
	RegistryTotal, InputTotal *float64
	FxFound                   bool
	FxRequired                bool
	InvalidNumber             bool
	NilWasCoerced             bool
}
type CfoExpIssue struct {
	Code, Message string
	Month         int
	Blocking      bool
}

// ValidateCfoExp returns V-01..V-08 issues. Negative amounts are permitted.
func ValidateCfoExp(in CfoExpValidationInput) []CfoExpIssue {
	var out []CfoExpIssue
	add := func(code, msg string, month int, blocking bool) {
		out = append(out, CfoExpIssue{Code: code, Message: msg, Month: month, Blocking: blocking})
	}
	filled := map[int]bool{}
	for _, c := range in.PlanMonths {
		if c.Month >= 1 && c.Month <= 12 && c.Amount != nil {
			filled[c.Month] = true
			if *c.Amount < 0 && !c.AllowNegative {
				add("V-02", "сумма не может быть отрицательной", c.Month, true)
			}
		}
	}
	for m, open := range in.OpenMonths {
		if open && !filled[m+1] {
			add("V-01", "плановый месяц не заполнен", m+1, true)
		}
	}
	if in.InvalidNumber {
		add("V-03", "значение не является числом", 0, true)
	}
	if in.NilWasCoerced {
		add("V-04", "пустое значение преобразовано в ноль", 0, true)
	}
	if in.ExpectedCountry != "" && in.Country != "" && !strings.EqualFold(in.Country, in.ExpectedCountry) {
		add("V-05", "страна не соответствует справочнику", 0, false)
	}
	if in.Returned && strings.TrimSpace(in.ReturnComment) == "" {
		add("V-06", "для возврата нужен комментарий", 0, true)
	}
	if in.RegistryTotal != nil && in.InputTotal != nil {
		d := *in.RegistryTotal - *in.InputTotal
		if d < 0 {
			d = -d
		}
		denom := abs(*in.InputTotal)
		if abs(*in.RegistryTotal) > denom {
			denom = abs(*in.RegistryTotal)
		}
		if denom == 0 {
			denom = 1
		}
		if d/denom > .01 {
			add("V-07", "реестр расходится с ручным вводом более чем на 1%", 0, false)
		}
	}
	if in.FxRequired && !in.FxFound {
		add("V-08", "курс валюты на дату не найден", 0, true)
	}
	return out
}
func abs(v float64) float64 {
	if v < 0 {
		return -v
	}
	return v
}
