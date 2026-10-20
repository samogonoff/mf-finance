package plans

import "testing"

// Скидки и уценки вводятся по каждой площадке (итог — сумма), а не одной строкой на группу, и в
// каскад (маржа, PL) не входят: пользователь пишет их в строках по площадкам.
func TestMpDiscountsArePerPlatformInputs(t *testing.T) {
	for _, mode := range []string{CalcLegacy, CalcInverse} {
		editable := mpEditableSetFor(mode)
		for _, l := range mpFormSpecFor(mode) {
			if l.BlockType != BDiscount && l.BlockType != BMarkdown {
				continue
			}
			if l.Scope != ScopePlatform || !l.Editable || l.Kind != KindInput || l.ValueKind != ValueMoney {
				t.Errorf("%s/%s: скидка должна быть денежным вводом по площадке: %+v", mode, l.BlockType, l)
			}
			if !editable[l.BlockType] {
				t.Errorf("%s/%s: должна сохраняться из сетки", mode, l.BlockType)
			}
		}
	}
}

func TestMpDiscountsPassThroughAndDoNotAffectMargin(t *testing.T) {
	base := map[string]float64{BSalesManagerGross: 1200, BSPP: 0.1}
	with := map[string]float64{BSalesManagerGross: 1200, BSPP: 0.1, BDiscount: 777, BMarkdown: 55}
	vat := MpVatBasis{Ratio: 1.2}
	a := computeMpPlatform(base, vat)
	b := computeMpPlatform(with, vat)
	if b[BDiscount] != 777 || b[BMarkdown] != 55 {
		t.Fatalf("скидки/уценки не переданы в значения площадки: %v %v", b[BDiscount], b[BMarkdown])
	}
	for _, blk := range []string{BRetailMargin, BGrossMargin, BPlPlatform, BSalesPlatNet} {
		if a[blk] != b[blk] {
			t.Errorf("скидка изменила %s: %v → %v", blk, a[blk], b[blk])
		}
	}
	// Режим «от условий»: то же правило.
	s := &TaskStore{}
	v := s.platformValues(CalcInverse, vat, 0, MpConditions{CodeCFO: 335}, map[string]float64{
		layerKey(335, BSalesManagerGross): 1200,
		layerKey(335, BDiscount):          777,
	}, mpLayers{}, 335)
	if v[BDiscount] != 777 || v[BMarkdown] != 0 {
		t.Fatalf("inverse: скидка %v, уценка %v", v[BDiscount], v[BMarkdown])
	}
}

// Импорт .xlsx формы: скидки и уценки без кода PL загружаются по названию блока (раньше такие
// строки пропускались, и подгрузить значения по площадкам было нечем); строки с кодом PL — как раньше.
func TestParseMpImportRows_DiscountsByBlockName(t *testing.T) {
	rows := [][]string{
		{"Площадка", "Код ЦФО", "Блок", "КодPL", "Факт", "Стратегия", "Тактика"},
		{"Wildberries", "335", "Скидки", "0", "", "", "1 000,5"},
		{"Lamoda", "336", "Уценки", "0", "", "", "40"},
		{"Ozon", "337", "Скидки", "0", "", "", ""},           // пусто — пропуск
		{"Ozon", "337", "% СПП (скидка)", "0", "", "", "10"}, // без кода PL и не скидка — не трогаем
		{"Wildberries", "335", "Реклама", "13", "", "", "200"},
	}
	got, err := parseMpImportRows(rows)
	if err != nil {
		t.Fatal(err)
	}
	want := map[string]float64{
		"335:" + BDiscount: 1000.5, "336:" + BMarkdown: 40, "335:" + BCostAds: 200,
	}
	if len(got) != len(want) {
		t.Fatalf("разобрано %d строк, ждали %d: %+v", len(got), len(want), got)
	}
	for _, r := range got {
		k := layerKey(r.CodeCFO, r.BlockType)
		if v, ok := want[k]; !ok || v != r.Amount {
			t.Errorf("%s: %v (ждали %v)", k, r.Amount, want[k])
		}
	}
	if _, err := parseMpImportRows([][]string{{"Код ЦФО", "Блок"}}); err == nil {
		t.Fatal("нет колонок «КодPL»/«Тактика» — ждали ошибку")
	}
}
