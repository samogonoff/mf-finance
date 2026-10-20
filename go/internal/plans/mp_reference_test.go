package plans

import "testing"

// Своды за периоды сравнения считаются по ФАКТУ периода и не подмешивают план: продажи берутся из
// факта (1046), а не из таргета; период без факта по площадке в ответ не попадает.
func TestMpReferenceTotals(t *testing.T) {
	l := newMpLayers()
	l.factPrev[[2]int{335, 1046}] = 1200  // тот же месяц прошлого года
	l.factPrev[[2]int{335, 13}] = 100     // реклама (статья 13)
	l.fact[[2]int{335, 1046}] = 600       // текущий месяц, факт на момент загрузки
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