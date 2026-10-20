package plans

import "testing"

func TestMpStrategyDefaults(t *testing.T) {
	plan := map[string]float64{
		layerKey(335, BSalesPlatNet):    800,
		layerKey(335, BSalesManagerNet): 1000,
		layerKey(335, BShipments):       500,
		layerKey(335, BCogsTotal):       640,
	}
	spp, mk, mkt, okS, okM, okT := mpStrategyDefaults(plan, 335)
	if !okS || !okM || !okT || spp != 0.2 || mk != 0.6 || mkt != 0.25 {
		t.Fatalf("spp=%v mk=%v mkt=%v ok=%v/%v/%v", spp, mk, mkt, okS, okM, okT)
	}
	// Нет базы — нет значения, а не ноль из деления.
	if _, _, _, a, b, c := mpStrategyDefaults(plan, 999); a || b || c {
		t.Fatal("площадка без плана не должна получать значения")
	}
}

// Несохранённые условия получают %СПП и наценки из стратегии; сохранённые (ID != 0) и уже заполненные
// значения не трогаются.
func TestMpApplyStrategyDefaults(t *testing.T) {
	plan := map[string]float64{
		layerKey(335, BSalesPlatNet): 800, layerKey(335, BSalesManagerNet): 1000,
		layerKey(335, BShipments): 500, layerKey(335, BCogsTotal): 640,
	}
	got := mpApplyStrategyDefaults(MpConditions{CodeCFO: 335, MarkupPct: 0.3}, plan)
	if got.SppPct != 0.2 || got.MarkupPct != 0.3 || got.MarkupTotalPct != 0.25 {
		t.Fatalf("введённая наценка сохраняется, остальное из стратегии: %+v", got)
	}
	if len(got.StrategyDefaults) != 2 || got.StrategyDefaults[0] != "spp" || got.StrategyDefaults[1] != "markup_total" {
		t.Fatalf("отметка источника: %v", got.StrategyDefaults)
	}
	saved := MpConditions{ID: 5, CodeCFO: 335}
	if got := mpApplyStrategyDefaults(saved, plan); got.SppPct != 0 || got.MarkupTotalPct != 0 || got.StrategyDefaults != nil {
		t.Fatalf("сохранённые условия не меняются: %+v", got)
	}
}

// Из подставленных наценок каскад даёт ненулевую маржу и PL (раньше при нулях маржа была 0).
func TestMpStrategyDefaultsMakeInverseCascadeCompute(t *testing.T) {
	plan := map[string]float64{
		layerKey(335, BSalesPlatNet): 800, layerKey(335, BSalesManagerNet): 1000,
		layerKey(335, BShipments): 500, layerKey(335, BCogsTotal): 640,
	}
	c := mpApplyStrategyDefaults(MpConditions{CodeCFO: 335}, plan)
	vals, _ := computeMpPlatformInverse(MpInverseInput{SalesManagerGross: 1200, Conditions: conditionsToInputNorm(c, MpVatBasis{Ratio: 1.2}, 0)})
	if vals[BRetailMargin] <= 0 || vals[BGrossMargin] <= 0 {
		t.Fatalf("маржа должна считаться: розничная %v, gross %v", vals[BRetailMargin], vals[BGrossMargin])
	}
	zero, _ := computeMpPlatformInverse(MpInverseInput{SalesManagerGross: 1200, Conditions: conditionsToInputNorm(MpConditions{CodeCFO: 335}, MpVatBasis{Ratio: 1.2}, 0)})
	if zero[BRetailMargin] != 0 {
		t.Fatalf("без условий маржа 0 (контроль теста): %v", zero[BRetailMargin])
	}
}
