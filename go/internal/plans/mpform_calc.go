package plans

// Каскад TPL-MP (планово-обратный, D-решение заказчика): менеджер вводит ПРОДАЖИ
// с НДС (1046) + правит %СПП и статьи затрат — всё остальное выводится. Формулы —
// из разбора аналитика (var/plan/plan/) и SPEC §11.1. Функция чистая (без БД):
// на ней держится корректность и она зеркалится в TS для живого пересчёта.

// vatByCountry — ставка НДС по стране разреза (large — всё RU = 20%).
func vatByCountry(country string) float64 {
	switch country {
	case "KZ", "UZ":
		return 0.12
	default: // RU, BY и прочее
		return 0.20
	}
}

func safeDiv(a, b float64) float64 {
	if b == 0 {
		return 0
	}
	return a / b
}

// mpCostBlocks — статьи прямых затрат, входящие в «Итого прямые затраты» (кроме
// комиссии, которая расчётная). Порядок — как в форме.
var mpCostBlocks = []string{
	BCostAgent, BCostFreight, BCostLogTransport, BCostLogWarehouse,
	BCostAds, BCostAdsSocial, BCostPackaging, BCostAcquiring, BCostIT,
	BCostPenalties, BCostOther,
}

// computeMpPlatform считает весь каскад по площадке из введённых значений.
// in — значения редактируемых строк (block_type → сумма/доля), включая %СПП и
// статьи затрат. Возвращает полный набор значений по всем block_type строки формы.
func computeMpPlatform(in map[string]float64, vat float64) map[string]float64 {
	out := map[string]float64{}

	managerGross := in[BSalesManagerGross]
	spp := in[BSPP]
	shipments := in[BShipments]
	cogs := in[BCogsTotal]

	managerNet := safeDiv(managerGross, 1+vat)
	platGross := managerGross * (1 - spp)
	platNet := safeDiv(platGross, 1+vat)

	out[BSalesManagerGross] = managerGross
	out[BSalesManagerNet] = managerNet
	out[BSPP] = spp
	out[BSalesPlatGross] = platGross
	out[BSalesPlatNet] = platNet

	out[BShipments] = shipments
	// Наценка% — расчётная (правка допускается: если задана вручную, берём её).
	if v, ok := in[BMarkup]; ok && v != 0 {
		out[BMarkup] = v
	} else {
		out[BMarkup] = safeDiv(platNet, shipments) - boolToF(shipments != 0)
	}

	out[BRetailMargin] = platNet - shipments
	out[BRetailMarginPct] = safeDiv(platNet-shipments, platNet)

	out[BCogsTotal] = cogs
	out[BMarkupTotal] = safeDiv(platNet, cogs) - boolToF(cogs != 0)
	out[BGrossMargin] = platNet - cogs
	out[BGrossMarginPct] = safeDiv(platNet-cogs, platNet)

	// Комиссия площадки = удержание МП = менеджер_без_НДС − площадка_без_НДС.
	commission := managerNet - platNet
	out[BCommission] = commission

	// Прямые затраты: комиссия + все статьи (ввод).
	directTotal := commission
	for _, b := range mpCostBlocks {
		v := in[b]
		out[b] = v
		directTotal += v
	}
	out[BPlatformCosts] = directTotal
	out[BDirectShare] = safeDiv(directTotal, managerNet)

	pl := (platNet - cogs) - directTotal
	out[BPlPlatform] = pl
	out[BPlPlatformPct] = safeDiv(pl, platNet)

	return out
}

// boolToF: 1 если true (для «−1» в наценке только при ненулевом знаменателе).
func boolToF(b bool) float64 {
	if b {
		return 1
	}
	return 0
}
