package plans

import (
	"context"
	"math"
	"testing"
)

func TestCalcRuleSeed_HasCascade(t *testing.T) {
	seed := CalcRuleSeed()
	need := map[string]bool{"sales_net": false, "gross_margin": false, "markup_pct": false}
	for _, r := range seed {
		if _, ok := need[r.Code]; ok {
			need[r.Code] = true
			if r.FormulaExpr == "" {
				t.Errorf("формула %s пуста", r.Code)
			}
		}
	}
	for code, ok := range need {
		if !ok {
			t.Errorf("seed должен содержать правило %s", code)
		}
	}
}

func TestComputeCascade_MarginAndMarkup(t *testing.T) {
	vars := map[string]float64{"sales": 1000, "cost": 600, "vat": 0.2}
	out := computeCascade(vars, resolveFormulas(CalcRuleSeed(), nil))

	if math.Abs(out["gross_margin"]-400) > 1e-6 {
		t.Errorf("gross_margin = %v, want 400", out["gross_margin"])
	}
	if math.Abs(out["markup_pct"]-(400.0/600.0)) > 1e-6 {
		t.Errorf("markup_pct = %v, want %v", out["markup_pct"], 400.0/600.0)
	}
	if math.Abs(out["sales_net"]-(1000.0/1.2)) > 1e-6 {
		t.Errorf("sales_net = %v, want %v", out["sales_net"], 1000.0/1.2)
	}
}

func TestComputeCascade_SkipsDivByZero(t *testing.T) {
	vars := map[string]float64{"sales": 1000, "cost": 0, "vat": 0.2}
	out := computeCascade(vars, resolveFormulas(CalcRuleSeed(), nil))
	if _, ok := out["markup_pct"]; ok {
		t.Error("markup_pct не должен считаться при cost=0 (деление на ноль)")
	}
	// gross_margin не делит — должен посчитаться.
	if math.Abs(out["gross_margin"]-1000) > 1e-6 {
		t.Errorf("gross_margin = %v, want 1000", out["gross_margin"])
	}
}

func TestComputeMp_DerivesMarginFromTactic(t *testing.T) {
	store := newMemStore()
	svc := NewService(store, NewMockFactSource(), newMemScope())
	ctx := context.Background()

	// Тактика: продажи 1046 = 1000000, себестоимость 8006 = 600000 по WB.
	req := SaveMpFormRequest{
		Segment: "large", Period: PeriodRef{Year: 2026, Month: 5},
		Rows: []SaveRow{
			{CodeCFO: 335, CodePL: 1046, BlockType: "sales_manager_price", Amount: 1000000, IsManual: true, Comment: "c"},
			{CodeCFO: 335, CodePL: 8006, BlockType: "shipments", Amount: 600000, IsManual: true, Comment: "c"},
		},
	}
	if _, err := svc.SaveMpForm(ctx, adminP, req, []byte(`{}`)); err != nil {
		t.Fatalf("seed: %v", err)
	}
	rows, err := svc.ComputeMp(ctx, adminP, 2026, 5, "large", "RUB")
	if err != nil {
		t.Fatalf("ComputeMp: %v", err)
	}
	var wb *ComputedRow
	for i := range rows {
		if rows[i].CodeCFO == 335 {
			wb = &rows[i]
		}
	}
	if wb == nil {
		t.Fatal("нет WB(335) в расчёте")
	}
	if math.Abs(wb.Values["gross_margin"]-400000) > 1e-6 {
		t.Errorf("маржа WB = %v, want 400000", wb.Values["gross_margin"])
	}
}

func TestResolveFormulas_OverrideWins(t *testing.T) {
	overrides := map[string]string{"markup_pct": "sales / cost"}
	formulas := resolveFormulas(CalcRuleSeed(), overrides)
	if formulas["markup_pct"] != "sales / cost" {
		t.Errorf("override должен иметь приоритет, got %q", formulas["markup_pct"])
	}
	// Не переопределённое остаётся из seed.
	if formulas["gross_margin"] == "" {
		t.Error("неопределённое override-правило должно остаться из seed")
	}
}
