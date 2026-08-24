package plans

import "testing"

// Golden-тест каскада TPL-MP: фиксирует поведение legacy-режима («суммы → доли»)
// на весь период рефакторинга под новые ТЗ. Любое расхождение здесь = регресс
// действующей формы, даже если новый inverse-режим (ТЗ МП §3.4) зелёный.
func TestGolden_MpCascade_Legacy(t *testing.T) {
	in := map[string]float64{
		BSalesManagerGross: 457_380_000, // 1046 — ввод
		BSPP:               0.31,
		BShipments:         158_914_278, // 8006 — ввод
		BCogsTotal:         94_586_710,  // 2006+6006 — ввод
		BCostAgent:         20_137_208,
		BCostFreight:       1_140_037,
		BCostLogTransport:  11_970_391,
		BCostLogWarehouse:  11_970_391,
		BCostAds:           15_200_496,
		BCostAdsSocial:     380_012,
		BCostAcquiring:     5_700_186,
		BCostOther:         760_025,
	}
	out := computeMpPlatform(in, 0.20)

	// Продажи: менеджер без НДС и цена площадки.
	approx(t, "менеджер без НДС", out[BSalesManagerNet], 381_150_000)
	approx(t, "площадка с НДС", out[BSalesPlatGross], 315_592_200)
	approx(t, "площадка без НДС", out[BSalesPlatNet], 262_993_500)
	// Комиссия = менеджер_без_НДС − площадка_без_НДС (= СПП, удержанный площадкой).
	approx(t, "комиссия", out[BCommission], 118_156_500)

	// Маржа розничная и gross.
	approx(t, "маржа розничная", out[BRetailMargin], 104_079_222)
	approx(t, "маржа gross", out[BGrossMargin], 168_406_790)

	// Прямые затраты = комиссия + Σ статей.
	approx(t, "прямые затраты", out[BPlatformCosts], 185_415_246)
	approx(t, "доля прямых затрат", out[BDirectShare], 0.4865)

	// PL по площадке = маржа gross − прямые затраты.
	approx(t, "PL по площадке", out[BPlPlatform], -17_008_456)
}

// Наценка%, заданная вручную, ведёт себестоимость по отпускным ценам
// (ТЗ МП §3.4: СС_отп = S_пл / (1 + Наценка)) — а не только подставляется в вывод.
func TestComputeMpPlatform_MarkupOverrideDrivesCost(t *testing.T) {
	base := map[string]float64{
		BSalesManagerGross: 120, // менеджер без НДС 100
		BSPP:               0.15, // площадка без НДС 85
		BShipments:         50,
		BCogsTotal:         55,
	}
	// Без override: наценка = 85/50 − 1 = 0,70.
	out := computeMpPlatform(base, 0.20)
	approx(t, "наценка расчётная", out[BMarkup], 0.70)
	approx(t, "СС отпускная", out[BShipments], 50)

	// С override наценки 0,65: СС = 85/1,65 = 51,515, маржа = 85 − 51,515.
	in := map[string]float64{}
	for k, v := range base {
		in[k] = v
	}
	in[BMarkup] = 0.65
	out = computeMpPlatform(in, 0.20)
	approx(t, "наценка override", out[BMarkup], 0.65)
	approx(t, "СС от наценки", out[BShipments], 51.5152)
	approx(t, "маржа розничная от наценки", out[BRetailMargin], 33.4848)
	approx(t, "маржинальность от наценки", out[BRetailMarginPct], 0.3939)

	// Наценка от общей себестоимости ведёт COGS так же.
	in[BMarkupTotal] = 1.7722
	out = computeMpPlatform(in, 0.20)
	approx(t, "COGS от наценки общей", out[BCogsTotal], 30.6620)
	approx(t, "маржа gross от наценки общей", out[BGrossMargin], 54.3380)
}
