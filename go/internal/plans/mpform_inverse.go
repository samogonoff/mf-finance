package plans

// Инверсия направления расчёта TPL-MP — главное изменение скорректированного ТЗ
// (§3.1, §3.4). Раньше форма получала СУММЫ из источника, а доли/наценки/%СПП
// вычисляла из них. Теперь наоборот: заполняющий вводит продажи по ценам менеджера
// с НДС, %СПП, две наценки и УДЕЛЬНЫЕ ВЕСА статей прямых затрат, а вся расходная
// часть считается.
//
// Формулы взяты дословно из ТЗ §3.4 и проверены на контрольной выборке
// (Приложение Б: июнь 2026, четыре крупные площадки) — см. mpform_inverse_test.go.
// Каскад legacy (computeMpPlatform) НЕ удалён: закрытые периоды считаются тем
// алгоритмом, которым были утверждены, режим живёт на карточке (calc_mode).

// MpConditionsInput — условия площадки на период: то, что вводится в реестре
// (ТЗ §6.1) и становится источником истины для расходной части.
type MpConditionsInput struct {
	CodeCFO        int                // площадка
	VatRate        float64            // эффективная ставка НДС площадки (не законодательная)
	SppPct         float64            // % СПП — скидка, которую даёт площадка
	MarkupPct      float64            // наценка к отпускным ценам
	MarkupTotalPct float64            // наценка к общей себестоимости (Cost of goods)
	Shares         map[string]float64 // доли статей прямых затрат: block_type → доля (может быть < 0)
}

// MpInverseInput — вход расчёта по одной площадке.
type MpInverseInput struct {
	SalesManagerGross float64 // ПРОДАЖИ по ценам менеджера с НДС (1046) — единственная вводимая сумма
	Conditions        MpConditionsInput
	// Overrides — ручные переопределения расчётных сумм. Автопересчёт их НЕ
	// перезатирает (ТЗ §7.2): форма показывает «расчёт даёт X, вручную стоит Y»,
	// сброс — явное действие с записью в аудит.
	Overrides map[string]float64
}

// MpInverseCell — вычисленная ячейка с происхождением значения.
type MpInverseCell struct {
	BlockType  string  `json:"block_type"`
	Value      float64 `json:"value"`
	Calculated float64 `json:"calculated"` // что даёт расчёт (при ручной правке отличается от Value)
	Manual     bool    `json:"manual"`
	Formula    string  `json:"formula"`
}

// computeMpPlatformInverse — каскад «условия → суммы» по одной площадке.
// Возвращает значения всех строк формы и лог расчёта по каждой вычисленной ячейке.
func computeMpPlatformInverse(in MpInverseInput) (map[string]float64, []MpInverseCell) {
	c := in.Conditions
	out := map[string]float64{}
	var log []MpInverseCell

	// put — записать значение с учётом ручного переопределения.
	put := func(block string, calc float64, formula string) float64 {
		val := calc
		manual := false
		if ov, ok := in.Overrides[block]; ok {
			val, manual = ov, true
		}
		out[block] = val
		log = append(log, MpInverseCell{
			BlockType: block, Value: val, Calculated: calc, Manual: manual, Formula: formula,
		})
		return val
	}

	// Продажи. НДС не считается «законодательным»: ставка — свойство площадки.
	managerGross := put(BSalesManagerGross, in.SalesManagerGross, "ручной ввод (тактика)")
	managerNet := put(BSalesManagerNet, safeDiv(managerGross, 1+c.VatRate),
		"= продажи с НДС / (1 + ставка НДС площадки)")
	out[BSPP] = c.SppPct
	log = append(log, MpInverseCell{BlockType: BSPP, Value: c.SppPct, Calculated: c.SppPct,
		Formula: "условия площадки: % СПП"})

	// Цена площадки = цена менеджера минус СПП, который площадка удержала.
	platGross := put(BSalesPlatGross, managerGross*(1-c.SppPct),
		"= продажи менеджера с НДС × (1 − %СПП)")
	platNet := put(BSalesPlatNet, managerNet*(1-c.SppPct),
		"= продажи менеджера без НДС × (1 − %СПП)")
	_ = platGross

	// Себестоимость — производная от наценки (а не ввод, как в legacy-режиме).
	shipments := put(BShipments, safeDiv(platNet, 1+c.MarkupPct),
		"= цена площадки без НДС / (1 + наценка)")
	out[BMarkup] = c.MarkupPct
	log = append(log, MpInverseCell{BlockType: BMarkup, Value: c.MarkupPct, Calculated: c.MarkupPct,
		Formula: "условия площадки: наценка, %"})

	put(BRetailMargin, platNet-shipments, "= цена площадки без НДС − себестоимость по отпускным ценам")
	put(BRetailMarginPct, safeDiv(platNet-shipments, platNet), "= маржа розничная / цена площадки без НДС")

	cogs := put(BCogsTotal, safeDiv(platNet, 1+c.MarkupTotalPct),
		"= цена площадки без НДС / (1 + наценка от общей сс)")
	out[BMarkupTotal] = c.MarkupTotalPct
	log = append(log, MpInverseCell{BlockType: BMarkupTotal, Value: c.MarkupTotalPct,
		Calculated: c.MarkupTotalPct, Formula: "условия площадки: наценка от общей сс, %"})

	put(BGrossMargin, platNet-cogs, "= цена площадки без НДС − себестоимость общая")
	put(BGrossMarginPct, safeDiv(platNet-cogs, platNet), "= маржа (gross) / цена площадки без НДС")

	// Комиссия площадки — это и есть СПП: она уже удержана при переходе от цены
	// менеджера к цене площадки, поэтому в PL второй раз не вычитается (ТЗ §3.4).
	commission := put(BCommission, managerNet*c.SppPct,
		"= продажи менеджера без НДС × %СПП")

	// Статьи прямых затрат: доля от выручки по ценам менеджера БЕЗ НДС.
	directTotal := commission
	for _, block := range mpCostBlocks {
		share := c.Shares[block]
		v := put(block, managerNet*share, "= продажи менеджера без НДС × доля статьи")
		directTotal += v
	}

	put(BPlatformCosts, directTotal, "= комиссия площадки + Σ статей прямых затрат")
	put(BDirectShare, safeDiv(directTotal, managerNet), "= прямые затраты / продажи менеджера без НДС")

	// PL по площадке считается ОТ МАРЖИ минус прямые затраты БЕЗ комиссии:
	// комиссия уже вычтена переходом к цене площадки.
	retailMargin := out[BRetailMargin]
	grossMargin := out[BGrossMargin]
	put(BPlPlatform, retailMargin-(directTotal-commission),
		"= маржа розничная − (прямые затраты − комиссия площадки)")
	put(BPlPlatformPct, safeDiv(out[BPlPlatform], platNet), "= PL по площадке / цена площадки без НДС")
	put(BPlPlatformTotalCost, grossMargin-(directTotal-commission),
		"= маржа (gross) − (прямые затраты − комиссия площадки)")
	put(BPlPlatformTotalCostPct, safeDiv(out[BPlPlatformTotalCost], platNet),
		"= PL от себестоимости общей / цена площадки без НДС")
	// Доля прямых затрат в обороте: комиссия исключена — она уже удержана
	// переходом к цене площадки (ТЗ §3.4).
	put(BDirectShareTurnover, safeDiv(directTotal-commission, platNet),
		"= (прямые затраты − комиссия площадки) / цена площадки без НДС")

	return out, log
}

// MpFormTotals — итоги формы по всем площадкам (то, что подаётся на согласование).
type MpFormTotals struct {
	SalesManagerGross float64 `json:"sales_manager_gross"`
	SalesManagerNet   float64 `json:"sales_manager_net"`
	SalesPlatGross    float64 `json:"sales_plat_gross"`
	SalesPlatNet      float64 `json:"sales_plat_net"`
	SppPct            float64 `json:"spp_pct"`
	Shipments         float64 `json:"shipments"`
	CogsTotal         float64 `json:"cogs_total"`
	RetailMargin      float64 `json:"retail_margin"`
	GrossMargin       float64 `json:"gross_margin"`
	Commission        float64 `json:"commission"`
	DirectCosts       float64 `json:"direct_costs"`
	CommonCosts       float64 `json:"common_costs"`
	PlPlatforms       float64 `json:"pl_platforms"`
	PlTotalCost       float64 `json:"pl_total_cost"`
	PL                float64 `json:"pl"`
	PLPct             float64 `json:"pl_pct"`
	DirectShareTurn   float64 `json:"direct_share_turnover"`
}

// computeMpFormTotals — итоги формы из значений по площадкам и общих затрат.
//
// Два нюанса ТЗ, которые здесь важны:
//  1. Итоговый PL считается по формуле файла F274 (§3.4): комиссии всех площадок
//     ПРИБАВЛЯЮТСЯ обратно, потому что они уже сидят в прямых затратах, а в цене
//     площадки уже вычтены — иначе СПП вычитается дважды.
//  2. Итоговая «доля прямых затрат в обороте» считается по ВСЕМ площадкам.
//     В прототипе здесь дефект (§1 п.25): вычитаются комиссии только Wildberries
//     и Ozon, из-за чего итог показывает 27,92 % вместо 26,32 %. Дефект НЕ
//     воспроизводим — это отдельный пункт приёмки.
func computeMpFormTotals(platforms map[int]map[string]float64, commonCosts float64) MpFormTotals {
	t := MpFormTotals{CommonCosts: commonCosts}
	for _, v := range platforms {
		t.SalesManagerGross += v[BSalesManagerGross]
		t.SalesManagerNet += v[BSalesManagerNet]
		t.SalesPlatGross += v[BSalesPlatGross]
		t.SalesPlatNet += v[BSalesPlatNet]
		t.Shipments += v[BShipments]
		t.CogsTotal += v[BCogsTotal]
		t.RetailMargin += v[BRetailMargin]
		t.GrossMargin += v[BGrossMargin]
		t.Commission += v[BCommission]
		t.DirectCosts += v[BPlatformCosts]
		t.PlPlatforms += v[BPlPlatform]
		t.PlTotalCost += v[BPlPlatformTotalCost]
	}
	// Средний %СПП по форме — из сумм, а не среднее долей.
	t.SppPct = safeDiv(t.SalesManagerNet-t.SalesPlatNet, t.SalesManagerNet)
	// PL (сумма) = цена площадки − СС отпускная − общие затраты − прямые + Σ комиссий.
	t.PL = t.SalesPlatNet - t.Shipments - commonCosts - t.DirectCosts + t.Commission
	t.PLPct = safeDiv(t.PL, t.SalesPlatNet)
	t.DirectShareTurn = safeDiv(t.DirectCosts-t.Commission, t.SalesPlatNet)
	return t
}
