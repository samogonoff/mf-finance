package plans

import "testing"

// Спека формы зависит от режима расчёта карточки: в inverse руками заполняются
// только продажи и %СПП, а наценки/себестоимость приходят из реестра
// условий (ТЗ §3.1). Если это разъедется со спекой, форма даст пользователю
// править то, что всё равно перезатрёт пересчёт.
func TestMpFormSpecFor_InverseChangesEditableSet(t *testing.T) {
	legacy := mpEditableSetFor(CalcLegacy)
	inverse := mpEditableSetFor(CalcInverse)

	// Продажи вводятся в обоих режимах — это единственная вводимая сумма inverse.
	if !legacy[BSalesManagerGross] || !inverse[BSalesManagerGross] {
		t.Fatal("продажи по ценам менеджера должны вводиться в любом режиме")
	}
	if !inverse[BSPP] {
		t.Fatal("%СПП вводится в форме")
	}
	// Наценки и себестоимость задаются условиями и каскадом.
	for _, b := range []string{BMarkup, BMarkupTotal, BShipments, BCogsTotal} {
		if inverse[b] {
			t.Errorf("%s в inverse-режиме не должен редактироваться в сетке", b)
		}
		if b == BSPP || b == BMarkup {
			if !legacy[b] {
				t.Errorf("%s в legacy-режиме редактируется (правка расчёта)", b)
			}
		}
	}
	if inverse[BDiscount] || inverse[BMarkdown] {
		t.Fatal("в форме вводятся только продажи и %СПП")
	}
}

func TestMpFormSpecFor_InverseCostLinesCalculated(t *testing.T) {
	spec := mpFormSpecFor(CalcInverse)
	byBlock := map[string]MpLine{}
	for _, l := range spec {
		byBlock[l.BlockType] = l
	}
	for _, b := range mpCostBlocks {
		l, ok := byBlock[b]
		if !ok {
			t.Fatalf("статья %s пропала из спеки", b)
		}
		if l.Kind != KindCalc {
			t.Errorf("статья %s должна быть расчётной, получено %q", b, l.Kind)
		}
		if l.Editable {
			t.Errorf("статья %s не должна редактироваться", b)
		}
	}
}

// Строки дерева показателей ТЗ §4.1, добавленные новой редакцией, присутствуют в
// спеке — иначе форма не покажет PL от общей себестоимости и долю ПЗ в обороте.
func TestMpFormSpec_HasNewIndicatorRows(t *testing.T) {
	byBlock := mpLineByBlock()
	for _, b := range []string{BPlPlatformTotalCost, BPlPlatformTotalCostPct, BDirectShareTurnover} {
		l, ok := byBlock[b]
		if !ok {
			t.Fatalf("строка %s отсутствует в спеке формы", b)
		}
		if l.Kind != KindCalc {
			t.Errorf("строка %s — расчётная, получено %q", b, l.Kind)
		}
	}
}

// Каскады legacy и inverse на согласованных входах дают одинаковые продажи и
// комиссию: это защищает от расхождения чисел при переключении режима карточки.
func TestCascades_AgreeOnSalesAndCommission(t *testing.T) {
	const gross = 457_380_000.0
	cond := conditionsWB()

	inv, _ := computeMpPlatformInverse(MpInverseInput{SalesManagerGross: gross, Conditions: cond})
	leg := computeMpPlatform(map[string]float64{
		BSalesManagerGross: gross,
		BSPP:               cond.SppPct,
		BShipments:         inv[BShipments],
		BCogsTotal:         inv[BCogsTotal],
	}, vatBasisFromRate(cond.VatRate))

	approxMoney(t, "продажи без НДС", leg[BSalesManagerNet], inv[BSalesManagerNet])
	approxMoney(t, "цена площадки без НДС", leg[BSalesPlatNet], inv[BSalesPlatNet])
	approxMoney(t, "комиссия", leg[BCommission], inv[BCommission])
	approxMoney(t, "маржа розничная", leg[BRetailMargin], inv[BRetailMargin])
	approxMoney(t, "маржа gross", leg[BGrossMargin], inv[BGrossMargin])
}
