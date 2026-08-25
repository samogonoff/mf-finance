package plans

import "testing"

// Спека формы зависит от режима расчёта карточки: в inverse руками заполняются
// только продажи и total-строки, а %СПП/наценки/себестоимость приходят из реестра
// условий (ТЗ §3.1). Если это разъедется со спекой, форма даст пользователю
// править то, что всё равно перезатрёт пересчёт.
func TestMpFormSpecFor_InverseChangesEditableSet(t *testing.T) {
	legacy := mpEditableSetFor(CalcLegacy)
	inverse := mpEditableSetFor(CalcInverse)

	// Продажи вводятся в обоих режимах — это единственная вводимая сумма inverse.
	if !legacy[BSalesManagerGross] || !inverse[BSalesManagerGross] {
		t.Fatal("продажи по ценам менеджера должны вводиться в любом режиме")
	}
	// В inverse условия площадки задаются в реестре, а не в сетке.
	for _, b := range []string{BSPP, BMarkup, BMarkupTotal, BShipments, BCogsTotal} {
		if inverse[b] {
			t.Errorf("%s в inverse-режиме не должен редактироваться в сетке", b)
		}
		if b == BSPP || b == BMarkup {
			if !legacy[b] {
				t.Errorf("%s в legacy-режиме редактируется (правка расчёта)", b)
			}
		}
	}
	// Скидки/уценки — ввод в обоих режимах (одно значение на все площадки).
	if !inverse[BDiscount] || !inverse[BMarkdown] {
		t.Fatal("скидки и уценки вводятся и в inverse-режиме")
	}
}

// Суммы статей затрат в inverse остаются переопределяемыми: ТЗ §7.2 требует, чтобы
// ручное значение не перезатиралось автопересчётом, а значит его нужно где-то ввести.
func TestMpFormSpecFor_InverseKeepsCostLinesOverridable(t *testing.T) {
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
		if l.Kind != KindCalcEditable {
			t.Errorf("статья %s должна быть расчётной с возможностью правки, получено %q", b, l.Kind)
		}
		if !l.Editable {
			t.Errorf("статья %s должна допускать ручное переопределение (ТЗ §7.2)", b)
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
	}, cond.VatRate)

	approxMoney(t, "продажи без НДС", leg[BSalesManagerNet], inv[BSalesManagerNet])
	approxMoney(t, "цена площадки без НДС", leg[BSalesPlatNet], inv[BSalesPlatNet])
	approxMoney(t, "комиссия", leg[BCommission], inv[BCommission])
	approxMoney(t, "маржа розничная", leg[BRetailMargin], inv[BRetailMargin])
	approxMoney(t, "маржа gross", leg[BGrossMargin], inv[BGrossMargin])
}
