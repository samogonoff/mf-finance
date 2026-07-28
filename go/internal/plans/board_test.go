package plans

import (
	"context"
	"math"
	"testing"
)

func TestBoardUniverseFilters(t *testing.T) {
	cases := []struct {
		name    string
		filter  BoardFilter
		allowed map[int]bool
		want    []int
	}{
		{name: "сегмент large", filter: BoardFilter{Segment: "large"}, want: []int{335, 336, 337, 954}},
		{name: "страна KZ", filter: BoardFilter{Country: "KZ"}, want: []int{338, 474, 475}},
		{name: "ЮЛ MF Tex", filter: BoardFilter{LegalEntity: "MF Tex"}, want: []int{957, 958, 959, 990, 991}},
		{name: "явный список площадок", filter: BoardFilter{CodeCFO: []int{335, 337}}, want: []int{335, 337}},
		{name: "ABAC режет до своего среза", filter: BoardFilter{Segment: "large"}, allowed: map[int]bool{337: true}, want: []int{337}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			_, got := boardUniverse(c.filter, c.allowed)
			if len(got) != len(c.want) {
				t.Fatalf("площадок %d, ждали %d: %+v", len(got), len(c.want), got)
			}
			for i, p := range got {
				if p.CodeCFO != c.want[i] {
					t.Errorf("[%d] code_cfo=%d, ждали %d", i, p.CodeCFO, c.want[i])
				}
			}
		})
	}
}

// ABAC-срез не должен влиять на список площадок для фильтров: пользователь видит
// в выпадашке только то, к чему допущен.
func TestBoardUniverseAllShrinksWithScope(t *testing.T) {
	all, _ := boardUniverse(BoardFilter{}, map[int]bool{335: true, 337: true})
	if len(all) != 2 {
		t.Fatalf("в списке фильтров %d площадок, ждали 2", len(all))
	}
}

// Итог по процентной строке — не сумма процентов, а пересчёт из агрегатов.
func TestBoardTotalPct(t *testing.T) {
	agg := map[string]float64{
		BSalesManagerNet: 1000,
		BSalesPlatNet:    800,
		BShipments:       500,
		BCogsTotal:       400,
		BRetailMargin:    300,
		BGrossMargin:     400,
		BPlPlatform:      200,
		BPlatformCosts:   250,
	}
	cases := map[string]float64{
		BSPP:             0.2,  // 1 − 800/1000
		BMarkup:          0.6,  // 800/500 − 1
		BMarkupTotal:     1.0,  // 800/400 − 1
		BRetailMarginPct: 0.375,
		BGrossMarginPct:  0.5,
		BPlPlatformPct:   0.25,
		BDirectShare:     0.25,
	}
	for block, want := range cases {
		if got := boardTotalPct(block, agg); math.Abs(got-want) > 1e-9 {
			t.Errorf("%s = %v, ждали %v", block, got, want)
		}
	}
}

// Пустые агрегаты не должны давать деления на ноль / NaN.
func TestBoardTotalPctEmpty(t *testing.T) {
	for _, block := range []string{BSPP, BMarkup, BMarkupTotal, BRetailMarginPct, BGrossMarginPct, BPlPlatformPct, BDirectShare} {
		if got := boardTotalPct(block, map[string]float64{}); got != 0 || math.IsNaN(got) {
			t.Errorf("%s на пустых агрегатах = %v, ждали 0", block, got)
		}
	}
}

func TestAggValue(t *testing.T) {
	f := func(v float64) *float64 { return &v }
	if got := aggValue([]*float64{nil, nil}); got != nil {
		t.Errorf("все nil → ждали nil, получили %v", *got)
	}
	got := aggValue([]*float64{f(10), nil, f(5)})
	if got == nil || *got != 15 {
		t.Fatalf("сумма = %v, ждали 15", got)
	}
}

// Входы каскада: сохранённая тактика важнее факта; %СПП деривируется из источника,
// наценка остаётся расчётной.
func TestMpPlatformInputs(t *testing.T) {
	factIdx := map[[2]int]float64{
		{335, 1046}: 1000, // продажи менеджера с НДС
		{335, 1045}: 833,
		{335, 1006}: 700,
		{335, 8006}: 400,
	}
	tactic := map[string]float64{layerKey(335, BSalesManagerGross): 1200}

	in := mpPlatformInputs(335, tactic, factIdx)
	if in[BSalesManagerGross] != 1200 {
		t.Errorf("тактика должна перебивать факт: %v", in[BSalesManagerGross])
	}
	if in[BShipments] != 400 {
		t.Errorf("себестоимость из факта: %v", in[BShipments])
	}
	wantSPP := 1 - 700.0/833.0
	if math.Abs(in[BSPP]-wantSPP) > 1e-9 {
		t.Errorf("%%СПП = %v, ждали %v", in[BSPP], wantSPP)
	}
	if _, ok := in[BMarkup]; ok {
		t.Errorf("наценка не сидируется — она расчётная")
	}
}

// Объединённая форма МП покрывает оба сегмента: набор сегментов задания должен
// определяться по площадкам, а слои — подбираться под каждую площадку свои.
func TestDistinctSegments(t *testing.T) {
	got := distinctSegments(map[int]string{335: "large", 337: "large", 953: "small", 999: ""})
	if len(got) != 2 || got[0] != "large" || got[1] != "small" {
		t.Fatalf("сегменты = %v, ждали [large small]", got)
	}
	if n := len(distinctSegments(map[int]string{335: "large"})); n != 1 {
		t.Errorf("однросегментное задание дало %d сегментов", n)
	}
}

func TestMpLayerSetForCfo(t *testing.T) {
	large := newMpLayers()
	large.fact[[2]int{335, 1046}] = 100
	small := newMpLayers()
	small.fact[[2]int{953, 1046}] = 7

	set := mpLayerSet{
		bySegment: map[string]mpLayers{"large": large, "small": small},
		segmentOf: map[int]string{335: "large", 953: "small"},
	}
	if v := set.forCfo(335).fact[[2]int{335, 1046}]; v != 100 {
		t.Errorf("large-площадка получила %v, ждали 100", v)
	}
	if v := set.forCfo(953).fact[[2]int{953, 1046}]; v != 7 {
		t.Errorf("small-площадка получила %v, ждали 7", v)
	}
	// Неизвестная площадка не должна ронять сборку формы — пустые слои.
	if len(set.forCfo(42).fact) != 0 {
		t.Error("неизвестная площадка должна давать пустые слои")
	}
}

func TestNormalizeCurrency(t *testing.T) {
	cases := map[string]string{"": "RUB", "rub": "RUB", " byn ": "BYN", "usd": "USD", "EUR": "RUB"}
	for in, want := range cases {
		if got := normalizeCurrency(in); got != want {
			t.Errorf("normalizeCurrency(%q) = %q, ждали %q", in, got, want)
		}
	}
}

// Демо-таргет mock-источника — факт того же периода с надбавкой (колонка «Таргет»
// должна быть наблюдаема при PLANS_MOCK=1).
func TestMockTaktTargetAboveFact(t *testing.T) {
	src := NewMockFactSource()
	ctx := context.Background()
	fact, err := src.MpFact(ctx, 2026, 5, "large")
	if err != nil {
		t.Fatal(err)
	}
	target, err := src.MpTaktTarget(ctx, 2026, 5, "large")
	if err != nil {
		t.Fatal(err)
	}
	if len(target) != len(fact) || len(target) == 0 {
		t.Fatalf("строк таргета %d, факта %d", len(target), len(fact))
	}
	for i := range fact {
		want := math.Round(fact[i].Amount*mockTaktUplift*100) / 100
		if target[i].Amount != want {
			t.Fatalf("таргет %d = %v, ждали %v", i, target[i].Amount, want)
		}
	}
}
