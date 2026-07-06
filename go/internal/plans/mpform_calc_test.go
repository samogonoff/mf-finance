package plans

import (
	"math"
	"testing"
)

func approx(t *testing.T, name string, got, want float64) {
	t.Helper()
	if math.Abs(got-want) > 0.01 {
		t.Errorf("%s: got %.4f, want %.4f", name, got, want)
	}
}

// Пример аналитика (Шпаргалка_примеры_расчетов_BL): менеджер без НДС 100,
// площадка без НДС 85 (СПП 15%), себестоимость 50, COGS 55. В нашей модели ввод —
// менеджер С НДС = 120 (НДС 20%).
func TestComputeMpPlatform_AnalystExample(t *testing.T) {
	in := map[string]float64{
		BSalesManagerGross: 120,
		BSPP:               0.15,
		BShipments:         50,
		BCogsTotal:         55,
	}
	out := computeMpPlatform(in, 0.20)

	approx(t, "менеджер без НДС", out[BSalesManagerNet], 100)
	approx(t, "площадка с НДС", out[BSalesPlatGross], 102)
	approx(t, "площадка без НДС", out[BSalesPlatNet], 85)
	approx(t, "комиссия", out[BCommission], 15)
	approx(t, "наценка%", out[BMarkup], 0.70)
	approx(t, "маржа розничная", out[BRetailMargin], 35)
	approx(t, "маржинальность%", out[BRetailMarginPct], 0.4118)
	approx(t, "маржа gross", out[BGrossMargin], 30)
	approx(t, "маржа gross%", out[BGrossMarginPct], 0.3529)
	approx(t, "наценка от COGS%", out[BMarkupTotal], 0.5454)
}

// Прямые затраты и PL по площадке.
func TestComputeMpPlatform_DirectCostsAndPL(t *testing.T) {
	in := map[string]float64{
		BSalesManagerGross: 120, // менеджер без НДС = 100
		BSPP:               0.15, // площадка без НДС = 85, комиссия = 15
		BShipments:         50,
		BCogsTotal:         55, // маржа gross = 30
		BCostAds:           10,
		BCostAcquiring:     2,
		BCostPenalties:     3,
	}
	out := computeMpPlatform(in, 0.20)

	// Итого прямые = комиссия 15 + 10 + 2 + 3 = 30.
	approx(t, "прямые затраты", out[BPlatformCosts], 30)
	// PL = маржа gross 30 − прямые 30 = 0.
	approx(t, "PL по площадке", out[BPlPlatform], 0)
}

// Нулевые знаменатели не роняют расчёт и дают 0.
func TestComputeMpPlatform_ZeroSafe(t *testing.T) {
	out := computeMpPlatform(map[string]float64{}, 0.20)
	approx(t, "наценка% при 0", out[BMarkup], 0)
	approx(t, "маржинальность% при 0", out[BRetailMarginPct], 0)
	approx(t, "PL% при 0", out[BPlPlatformPct], 0)
}
