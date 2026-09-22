package plans

// Каскад TPL-MP (планово-обратный, D-решение заказчика): менеджер вводит ПРОДАЖИ
// с НДС (1046) + правит %СПП и статьи затрат — всё остальное выводится. Формулы —
// из разбора аналитика (var/plan/plan/) и SPEC §11.1. Функция чистая (без БД):
// на ней держится корректность и она зеркалится в TS для живого пересчёта.

// vatByCountry — законодательная ставка страны. Остаётся ФОЛБЭКОМ последней
// очереди: ведущий механизм — отношение плановых сумм (см. mpform_vat.go), за ним
// эффективная ставка справочника dir_vat.
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
// статьи затрат. vat — базис перевода «с НДС → без НДС» этой площадки (отношение
// плановых сумм, ставка справочника только фолбэком). Возвращает полный набор
// значений по всем block_type строки формы.
func computeMpPlatform(in map[string]float64, vat MpVatBasis) map[string]float64 {
	out := map[string]float64{}

	managerGross := in[BSalesManagerGross]
	spp := in[BSPP]
	shipments := in[BShipments]
	cogs := in[BCogsTotal]

	// Оба уровня цены делятся на ОДНУ ставку (правка 08.09.2026): только так
	// контроль связности «1 − [1006]/[1045] = %СПП» сходится по определению.
	managerNet := safeDiv(managerGross, vat.Ratio)
	platGross := managerGross * (1 - spp)
	platNet := safeDiv(platGross, vat.Ratio)

	out[BSalesManagerGross] = managerGross
	out[BSalesManagerNet] = managerNet
	out[BVatRate] = vat.Rate()
	out[BSPP] = spp
	out[BSalesPlatGross] = platGross
	out[BSalesPlatNet] = platNet

	// Наценка% — расчётная, но правка допускается. Если задана вручную, она
	// становится ведущей: себестоимость по отпускным ценам — производная от неё
	// (ТЗ МП §3.4: СС_отп = S_пл / (1 + Наценка)). Раньше override подставлялся
	// в вывод, но зависимые строки (СС, маржа) считались по старой СС — форма
	// показывала несогласованный каскад.
	if v, ok := in[BMarkup]; ok && v != 0 {
		out[BMarkup] = v
		shipments = safeDiv(platNet, 1+v)
	} else {
		out[BMarkup] = safeDiv(platNet, shipments) - boolToF(shipments != 0)
	}
	out[BShipments] = shipments

	out[BRetailMargin] = platNet - shipments
	out[BRetailMarginPct] = safeDiv(platNet-shipments, platNet)

	// То же для наценки от общей себестоимости: СС_общ = S_пл / (1 + Наценка_общ).
	if v, ok := in[BMarkupTotal]; ok && v != 0 {
		out[BMarkupTotal] = v
		cogs = safeDiv(platNet, 1+v)
	} else {
		out[BMarkupTotal] = safeDiv(platNet, cogs) - boolToF(cogs != 0)
	}
	out[BCogsTotal] = cogs
	out[BGrossMargin] = platNet - cogs
	out[BGrossMarginPct] = safeDiv(platNet-cogs, platNet)

	// Комиссия площадки = менеджер_без_НДС − площадка_без_НДС (ТЗ МП §22).
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
	// База долей — цена площадки БЕЗ НДС (решение звонка 07.09.2026; в прототипе
	// E81 делит на цену менеджера). Та же база, что у статей в inverse-каскаде:
	// СПП уже удержан переходом к цене площадки, и мерить расход от цены
	// менеджера значит считать комиссию внутри каждой доли ещё раз.
	out[BDirectShare] = safeDiv(directTotal, platNet)

	// PL по площадке (правка ТЗ 02.09.2026 МП §21):
	//   PL = продажи по цене площадки без НДС − себестоимость по отпускным ценам
	//        − прямые затраты
	// Из прежней формулы убраны «общие затраты» и слагаемое «+ Σ комиссия
	// площадки», и вместо себестоимости ОБЩЕЙ (COGS осн+пошив) вычитается
	// себестоимость ПО ОТПУСКНЫМ ЦЕНАМ (8006).
	//
	// Комиссия в PL не вычитается: цена площадки уже за вычетом СПП, и вычесть
	// её ещё раз значит посчитать дважды. В прежней формуле «+ Σ комиссия»
	// компенсировало базу по ценам менеджера — теперь компенсировать нечего.
	directNoCommission := directTotal - commission
	pl := platNet - shipments - directNoCommission
	out[BPlPlatform] = pl
	out[BPlPlatformPct] = safeDiv(pl, platNet)
	// PL от себестоимости ОБЩЕЙ — тот же расчёт, но с COGS: показатель формы,
	// а не альтернативная трактовка §21.
	out[BPlPlatformTotalCost] = platNet - cogs - directNoCommission
	out[BPlPlatformTotalCostPct] = safeDiv(out[BPlPlatformTotalCost], platNet)

	return out
}

// boolToF: 1 если true (для «−1» в наценке только при ненулевом знаменателе).
func boolToF(b bool) float64 {
	if b {
		return 1
	}
	return 0
}
