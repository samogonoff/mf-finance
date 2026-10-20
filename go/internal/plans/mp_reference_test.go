package plans

import "testing"

// Своды за периоды сравнения считаются по ФАКТУ периода и не подмешивают план: продажи берутся из
// факта (1046), а не из таргета; период без факта по площадке в ответ не попадает.
func TestMpReferenceTotals(t *testing.T) {
	l := newMpLayers()
	l.factPrev[[2]int{335, 1046}] = 1200                // тот же месяц прошлого года
	l.factPrev[[2]int{335, 13}] = 100                   // реклама (статья 13)
	l.fact[[2]int{335, 1046}] = 600                     // текущий месяц, факт на момент загрузки
	l.target[layerKey(335, BSalesManagerGross)] = 99999 // план: в сводах быть не должен
	set := mpLayerSet{bySegment: map[string]mpLayers{"large": l}, segmentOf: map[int]string{335: "large"}}
	ref := mpReferenceTotals([]MpFormPlatform{{CodeCFO: 335, Segment: "large"}}, set, map[int]MpVatBasis{335: {Ratio: 1.2}})

	ly := ref[MpRefLastYear][335]
	if ly == nil || ly[BSalesManagerGross] != 1200 || ly[BCostAds] != 100 {
		t.Fatalf("прошлый год: %v", ly)
	}
	if cur := ref[MpRefCurrentFct][335]; cur == nil || cur[BSalesManagerGross] != 600 {
		t.Fatalf("текущий месяц: %v", cur)
	}
	if _, ok := ref[MpRefPrevMonth]; ok {
		t.Fatalf("у прошлого месяца нет факта — периода в ответе быть не должно: %v", ref[MpRefPrevMonth])
	}
	// Неизвестная площадка и пустые слои не падают.
	if got := mpReferenceTotals([]MpFormPlatform{{CodeCFO: 1}}, mpLayerSet{}, nil); len(got) != 0 {
		t.Fatalf("пустые слои: %v", got)
	}
}

// Каскад периода сравнения получает %СПП, себестоимость и НДС из самого факта: без них комиссия и
// себестоимость обнулялись, а маржа получалась 100 %.
func TestMpReferenceTotalsCascadeFromFact(t *testing.T) {
	l := newMpLayers()
	l.factPrev[[2]int{335, 1046}] = 1200 // с НДС, цена менеджера
	l.factPrev[[2]int{335, 1045}] = 1000 // без НДС: отношение 1,2
	l.factPrev[[2]int{335, 1006}] = 800  // цена площадки без НДС: %СПП = 1 − 800/1000 = 20 %
	l.factPrev[[2]int{335, 8006}] = 500  // себестоимость по отпускным ценам
	l.factPrev[[2]int{335, 2006}] = 100  // себестоимость общая = 2006 + 6006
	l.factPrev[[2]int{335, 6006}] = 50
	set := mpLayerSet{bySegment: map[string]mpLayers{"large": l}, segmentOf: map[int]string{335: "large"}}
	// Базис плана специально другой: период сравнения должен считаться по своему отношению.
	ref := mpReferenceTotals([]MpFormPlatform{{CodeCFO: 335, Segment: "large"}}, set, map[int]MpVatBasis{335: {Ratio: 1.1}})

	v := ref[MpRefLastYear][335]
	near := func(name string, got, want float64) {
		if d := got - want; d > 1e-6 || d < -1e-6 {
			t.Errorf("%s: %.6f, ожидали %.6f", name, got, want)
		}
	}
	near("%СПП", v[BSPP], 0.2)
	near("цена площадки без НДС", v[BSalesPlatNet], 800)
	near("комиссия", v[BCommission], 200)
	near("себестоимость по отпускным", v[BShipments], 500)
	near("маржа розничная", v[BRetailMargin], 300)
	near("себестоимость общая", v[BCogsTotal], 150)
	near("маржа gross", v[BGrossMargin], 650)
}

// Слои сегмента общие: факт у одной площадки не должен давать нули у соседней без факта.
func TestMpReferenceTotalsSkipsPlatformWithoutFact(t *testing.T) {
	l := newMpLayers()
	l.factPrev[[2]int{335, 1046}] = 1200
	set := mpLayerSet{bySegment: map[string]mpLayers{"large": l}, segmentOf: map[int]string{335: "large", 336: "large"}}
	ref := mpReferenceTotals([]MpFormPlatform{{CodeCFO: 335, Segment: "large"}, {CodeCFO: 336, Segment: "large"}}, set, nil)

	if _, ok := ref[MpRefLastYear][335]; !ok {
		t.Fatal("у площадки с фактом свод должен быть")
	}
	if v, ok := ref[MpRefLastYear][336]; ok {
		t.Fatalf("у площадки без факта периода сводов быть не должно, получили %v", v)
	}
}
