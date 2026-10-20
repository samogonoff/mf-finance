package plans

import (
	"context"
	"testing"
)

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

type strategySrcStub struct {
	MpFactSource
	rows []FactRow
}

func (s strategySrcStub) MpStrategy(context.Context, int, int, string) ([]FactRow, error) {
	return s.rows, nil
}

// Один хелпер на все пути (форма, пересчёт, реестр): площадки без условий получают значения из стратегии,
// сохранённые условия и площадки без плана не меняются.
func TestMpApplyStrategyDefaultsAll(t *testing.T) {
	src := strategySrcStub{rows: []FactRow{
		{CodeCFO: 335, CodePL: 1006, Amount: 800}, {CodeCFO: 335, CodePL: 1045, Amount: 1000},
		{CodeCFO: 335, CodePL: 8006, Amount: 500}, {CodeCFO: 335, CodePL: 2006, Amount: 600}, {CodeCFO: 335, CodePL: 6006, Amount: 40},
		{CodeCFO: 337, CodePL: 1006, Amount: 800}, {CodeCFO: 337, CodePL: 1045, Amount: 1000},
	}}
	platforms := []MarketplaceRow{
		{CodeCFO: 335, Segment: "large", Country: "RU"},
		{CodeCFO: 336, Segment: "large", Country: "RU"}, // нет плана
		{CodeCFO: 337, Segment: "large", Country: "RU"}, // условия сохранены
	}
	conds := map[int]MpConditions{337: {ID: 9, CodeCFO: 337}}
	got := mpApplyStrategyDefaultsAll(context.Background(), src, 2026, 11, platforms, conds)

	if c := got[335]; c.SppPct != 0.2 || c.MarkupPct != 0.6 || c.MarkupTotalPct < 0.2 || len(c.StrategyDefaults) != 3 || c.Year != 2026 {
		t.Fatalf("площадка без условий получает значения из стратегии: %+v", c)
	}
	if _, ok := got[336]; ok {
		t.Fatal("у площадки без плана записи быть не должно")
	}
	if c := got[337]; c.SppPct != 0 || c.StrategyDefaults != nil {
		t.Fatalf("сохранённые условия не трогаются: %+v", c)
	}
	if len(conds) != 1 {
		t.Fatal("исходная карта не должна меняться")
	}
	// Нет источника или плана — без изменений.
	if out := mpApplyStrategyDefaultsAll(context.Background(), nil, 2026, 11, platforms, conds); len(out) != 1 {
		t.Fatalf("без источника: %+v", out)
	}
}
