package plans

import (
	"math"
	"testing"
)

// approxMoney — сверка денежной суммы с эталоном ТЗ. Допуск 10 единиц валюты:
// в Приложении Б суммы приведены целыми, а входные доли и наценки — округлёнными
// до 4 знаков, поэтому копейки расходятся по определению. Расхождение больше
// допуска означает ошибку формулы, а не округление.
func approxMoney(t *testing.T, name string, got, want float64) {
	t.Helper()
	if math.Abs(got-want) > 10 {
		t.Errorf("%s: got %.2f, want %.2f (расхождение %.2f)", name, got, want, got-want)
	}
}

// Приёмочная выборка ТЗ МП (Приложение Б): файл «Тактический план_large_МП_2026_июнь»,
// лист Маркетплейсы_large, колонка тактики на июнь 2026, четыре крупные площадки.
// Приёмка пройдена, если веб-форма на этих входных данных воспроизводит значения
// с расхождением 0. Исключение — «Доля прямых затрат в обороте»: в файле 27,92 %
// из-за дефекта формулы, корректное значение 26,32 % (проверяется отдельно).
//
// ⚠ ЭТАЛОН ЧАСТИЧНО ПЕРЕСЧИТАН НАМИ (правки ТЗ 02.09.2026 МП §20/§21) и ждёт
// подтверждения аналитика. Изменились ровно те величины, которые зависят от двух
// правок:
//   §20 — база начисления статей перешла с цены менеджера на цену площадки,
//         поэтому суммы статей (и прямые затраты) ниже прежних на СПП-часть;
//   §21 — из PL убраны «общие затраты» и слагаемое «+ Σ комиссия площадки», а
//         вместо себестоимости общей вычитается себестоимость по отпускным ценам.
// Величины, которых правки не касались (продажи, себестоимости, маржи, комиссия,
// %СПП, наценки), остались эталонными числами Приложения Б — они и держат
// приёмку. Пересчитанные значения дополнительно закреплены ИНВАРИАНТАМИ ниже:
// каждый из них выводится из правок арифметически, поэтому «подогнать» их под
// код нельзя, не сломав инвариант.

// Эффективные ставки НДС площадок (ТЗ §3.4): не законодательные 20 %.
// Считаны из данных прототипа: продажи с НДС / продажи без НДС − 1.
const (
	vatWB     = 457_380_000.0/380_012_405.0 - 1
	vatLamoda = vatWB // 20,36 % — та же группа товаров
	vatOzon   = vatWB
	vatYM     = 0.1662
)

// условияWB — условия площадки Wildberries на июнь 2026 (ТЗ §4.2, колонка WB).
func conditionsWB() MpConditionsInput {
	return MpConditionsInput{
		CodeCFO:   335,
		VatRate:   vatWB,
		SppPct:    0.31,
		MarkupPct: 0.65,
		// Наценка от общей себестоимости: в ТЗ приведена как 2,7722 (округление),
		// точное значение из контрольных сумм — 1,772150 (262 208 559 / 94 586 710 − 1).
		MarkupTotalPct: 1.772150,
		Shares: map[string]float64{
			BCostAgent:        0.0530, // 64 — агентское вознаграждение
			BCostFreight:      0.0030, // 51 — грузоперевозки экспорт
			BCostLogTransport: 0.0315, // 52 — транспортная логистика
			BCostLogWarehouse: 0.0315, // 54 — складская логистика
			BCostAds:          0.0400, // 13 — реклама и маркетинг
			BCostAdsSocial:    0.0010, // 15 — реклама-соцсети
			BCostPackaging:    0,      // 45 — упаковка
			BCostAcquiring:    0.0150, // 58 — эквайринг
			BCostIT:           0,      // 48 — у Wildberries статьи нет
			BCostPenalties:    0,      // 66 — штрафы (факт приходит из источника)
			BCostOther:        0.0020, // 66 — прочие удержания
		},
	}
}

// Контрольная площадка: все значения колонки Wildberries из Приложения Б.
func TestInverse_AcceptanceWildberriesJune2026(t *testing.T) {
	out, _ := computeMpPlatformInverse(MpInverseInput{
		SalesManagerGross: 457_380_000,
		Conditions:        conditionsWB(),
	})

	approxMoney(t, "продажи менеджера с НДС", out[BSalesManagerGross], 457_380_000)
	approxMoney(t, "продажи менеджера без НДС", out[BSalesManagerNet], 380_012_405)
	approxMoney(t, "продажи по цене площадки с НДС", out[BSalesPlatGross], 315_592_200)
	approxMoney(t, "продажи по цене площадки без НДС", out[BSalesPlatNet], 262_208_559)
	approxMoney(t, "себестоимость по отпускным ценам", out[BShipments], 158_914_278)
	approxMoney(t, "маржа розничная", out[BRetailMargin], 103_294_281)
	approxMoney(t, "себестоимость общая", out[BCogsTotal], 94_586_710)
	approxMoney(t, "маржа (gross)", out[BGrossMargin], 167_621_849)
	approxMoney(t, "комиссия площадки", out[BCommission], 117_803_845)

	// --- Пересчитано по правкам §20/§21 (ждёт подтверждения аналитика) ---
	// Статьи: 262 208 559 × Σдолей 0,1770 = 46 410 915 (было от 380 012 405).
	// Прямые затраты = комиссия 117 803 845 + статьи 46 410 915.
	approxMoney(t, "прямые затраты по площадке", out[BPlatformCosts], 164_214_760)
	// PL = 262 208 559 − 158 914 278 − 46 410 915.
	approxMoney(t, "PL по площадке (от прямых затрат)", out[BPlPlatform], 56_883_366)
	// PL от общей СС = 262 208 559 − 94 586 710 − 46 410 915.
	approxMoney(t, "PL от себестоимости общей", out[BPlPlatformTotalCost], 121_210_930)
	// База доли — цена площадки без НДС (решение звонка 07.09.2026; в Приложении Б
	// 0,4321 от цены менеджера). Пересчёт: 164 214 760 / 262 208 559 = 0,6263.
	// Инвариант ниже делает это число проверяемым: доля прямых затрат обязана
	// равняться сумме долей статей ПЛЮС доля комиссии, посчитанная от той же базы.
	approx(t, "доля прямых затрат площадки", out[BDirectShare], 0.6263)
	approx(t, "доля прямых затрат в обороте", out[BDirectShareTurnover], 0.1770)

	// --- Эталонные числа Приложения Б, которых правки не касались ---
	approx(t, "маржинальность розничная", out[BRetailMarginPct], 0.3939)
	approx(t, "маржа (gross), %", out[BGrossMarginPct], 0.6393)

	// Инварианты правок §20/§21 — они и делают пересчитанные числа проверяемыми.
	//
	// 1) Доля прямых затрат в обороте обязана равняться СУММЕ ДОЛЕЙ статей:
	//    статьи начисляются на цену площадки, и та же цена площадки стоит в
	//    знаменателе. При базе по ценам менеджера равенства не было (0,2565
	//    против 0,1770) — это и был признак прежней ошибки.
	var shareSum float64
	for _, v := range conditionsWB().Shares {
		shareSum += v
	}
	approx(t, "доля прямых в обороте = Σ долей статей", out[BDirectShareTurnover], shareSum)

	// 3) Доля прямых затрат = Σ долей статей + доля комиссии от той же базы.
	//    Раньше равенства не было: доля мерилась от цены менеджера, а статьи
	//    начислялись на цену площадки.
	approx(t, "доля прямых = Σ долей + комиссия/база", out[BDirectShare],
		shareSum+out[BCommission]/out[BSalesPlatNet])

	// 2) PL = цена площадки без НДС − СС по отпускным ценам − статьи (без комиссии).
	wantPL := out[BSalesPlatNet] - out[BShipments] - (out[BPlatformCosts] - out[BCommission])
	approxMoney(t, "PL = площадка − СС отпускная − прямые (§21)", out[BPlPlatform], wantPL)
}

// Итог по форме: PL (сумма) считается по формуле F274 с обратным прибавлением
// комиссий, а итоговая доля прямых затрат в обороте — по всем площадкам
// (дефект прототипа, дающий 27,92 %, не воспроизводится).
func TestInverse_AcceptanceFormTotalsJune2026(t *testing.T) {
	// Итоги четырёх площадок из Приложения Б («Итог по форме»).
	platforms := map[int]map[string]float64{
		1: {
			BSalesManagerGross:   983_931_572,
			BSalesManagerNet:     817_717_675,
			BSalesPlatGross:      735_746_205,
			BSalesPlatNet:        611_469_436,
			BShipments:           383_406_684,
			BCogsTotal:           216_368_745,
			BRetailMargin:        228_062_752,
			BGrossMargin:         395_100_692,
			BCommission:          206_248_239,
			BPlatformCosts:       367_209_157,
			BPlPlatform:          67_101_834,
			BPlPlatformTotalCost: 231_867_722,
		},
	}
	got := computeMpFormTotals(platforms, 4_849_714)

	// Пересчитано по §21: общие затраты (4 849 714) в PL больше не входят,
	// поэтому PL по форме = Σ PL площадок = 67 101 834 (было 62 252 120).
	approxMoney(t, "PL (сумма) по форме", got.PL, 67_101_834)
	approxMoney(t, "PL по форме = PL площадок (§21)", got.PL, platforms[1][BPlPlatform])
	approx(t, "PL (%)", got.PLPct, 0.1097)
	approx(t, "средний %СПП по форме", got.SppPct, 0.2522)
	// Корректное значение вместо дефектных 27,92 % прототипа.
	approx(t, "доля прямых затрат в обороте (итог)", got.DirectShareTurn, 0.2632)
}

// Собираем итог из ЧЕТЫРЁХ независимо посчитанных площадок: проверяем, что
// каскад по площадке и свод формы согласованы (свод = сумма площадок, МП-07).
func TestInverse_FourPlatformsFoldIntoFormTotal(t *testing.T) {
	// Продажи с НДС по площадкам июня 2026 (large). WB — из Приложения Б,
	// остальные подобраны так, чтобы сумма давала итог формы 983 931 572.
	inputs := map[int]MpInverseInput{
		335: {SalesManagerGross: 457_380_000, Conditions: conditionsWB()},
		336: {SalesManagerGross: 180_000_000, Conditions: MpConditionsInput{
			CodeCFO: 336, VatRate: vatLamoda, SppPct: 0.06, MarkupPct: 0.65, MarkupTotalPct: 1.7722,
			Shares: map[string]float64{BCostAgent: 0.2525, BCostFreight: 0.0026,
				BCostLogTransport: 0.0193, BCostLogWarehouse: 0.0067, BCostAds: 0.0400,
				BCostAcquiring: 0.0129, BCostOther: 0.0015},
		}},
		337: {SalesManagerGross: 246_551_572, Conditions: MpConditionsInput{
			CodeCFO: 337, VatRate: vatOzon, SppPct: 0.27, MarkupPct: 0.65, MarkupTotalPct: 1.7722,
			Shares: map[string]float64{BCostAgent: 0.0698, BCostFreight: 0.0038,
				BCostLogTransport: 0.0015, BCostLogWarehouse: 0.0057, BCostAds: 0.0600,
				BCostPackaging: 0.0002, BCostAcquiring: 0.0122, BCostOther: 0.0030},
		}},
		954: {SalesManagerGross: 100_000_000, Conditions: MpConditionsInput{
			CodeCFO: 954, VatRate: vatYM, SppPct: 0.20, MarkupPct: 0.65, MarkupTotalPct: 1.7722,
			Shares: map[string]float64{BCostAgent: 0.1095, BCostFreight: 0.0069,
				BCostLogTransport: 0.0150, BCostLogWarehouse: 0.0050, BCostAds: 0.0700,
				BCostAcquiring: 0.0147, BCostOther: -0.0094}, // компенсация: доля отрицательная
		}},
	}

	platforms := map[int]map[string]float64{}
	for cfo, in := range inputs {
		out, _ := computeMpPlatformInverse(in)
		platforms[cfo] = out
	}
	got := computeMpFormTotals(platforms, 4_849_714)

	// Продажи с НДС должны сойтись с итогом формы Приложения Б.
	approxMoney(t, "продажи с НДС (итог)", got.SalesManagerGross, 983_931_572)
	// Контроль сходимости МП-04: PL = маржа − (ПЗ − комиссия) по каждой площадке.
	for cfo, v := range platforms {
		want := v[BRetailMargin] - (v[BPlatformCosts] - v[BCommission])
		if diff := v[BPlPlatform] - want; diff > 0.01 || diff < -0.01 {
			t.Fatalf("площадка %d: контроль сходимости PL не равен нулю (%.4f)", cfo, diff)
		}
	}
	// Свод по группе = сумма площадок (МП-07).
	var sum float64
	for _, v := range platforms {
		sum += v[BPlPlatform]
	}
	approxMoney(t, "свод PL = сумма площадок", got.PlPlatforms, sum)
}

// Статья всегда считается от доли из условий, без ручной суммы.
func TestInverse_CostAmountComesFromConditions(t *testing.T) {
	in := MpInverseInput{
		SalesManagerGross: 457_380_000,
		Conditions:        conditionsWB(),
	}
	out, log := computeMpPlatformInverse(in)

	var cell MpInverseCell
	for _, c := range log {
		if c.BlockType == BCostAds {
			cell = c
		}
	}
	if cell.Manual {
		t.Fatal("расчётная статья не бывает ручной")
	}
	// База — цена площадки без НДС.
	approxMoney(t, "расчётное значение статьи", cell.Calculated, 262_208_559*0.04)
	approxMoney(t, "значение статьи", out[BCostAds], cell.Calculated)
	wantDirect := out[BCommission] + out[BCostAds] +
		262_208_559*(0.0530+0.0030+0.0315+0.0315+0.0010+0.0150+0.0020)
	approxMoney(t, "прямые затраты по долям", out[BPlatformCosts], wantDirect)
}

// Отрицательная доля (компенсации, ТЗ §4.2) уменьшает прямые затраты.
func TestInverse_NegativeShareAllowed(t *testing.T) {
	c := conditionsWB()
	c.Shares = map[string]float64{BCostOther: -0.0094}
	out, _ := computeMpPlatformInverse(MpInverseInput{SalesManagerGross: 120, Conditions: c})
	if out[BCostOther] >= 0 {
		t.Fatalf("компенсация должна давать отрицательную сумму, получено %.4f", out[BCostOther])
	}
}

// Ставка НДС площадки не прошита: у Yandex Market она 16,62 %, и переход
// «с НДС → без НДС» обязан это учитывать (ТЗ §3.4, МП-06).
func TestInverse_PlatformVatRate(t *testing.T) {
	c := MpConditionsInput{CodeCFO: 954, VatRate: vatYM, SppPct: 0.20}
	out, _ := computeMpPlatformInverse(MpInverseInput{SalesManagerGross: 116_620_000, Conditions: c})
	approxMoney(t, "продажи без НДС при ставке 16,62 %", out[BSalesManagerNet], 100_000_000)
}

// Приёмка по контрольным числам дельты 08.09.2026 (лист C, Wildberries, июнь).
//
// В файле доли и норматив заданы к выручке по ценам менеджера без НДС (1045), а
// база формы — цена площадки без НДС (решение звонка 07.09.2026). Числа при этом
// ОБЯЗАНЫ совпадать: перевод базы — это деление и долей, и норматива на (1 − СПП),
// суммы от него не меняются. Тест это и проверяет — иначе выбор базы молча
// сдвинул бы все статьи на СПП-часть.
func conditionsWBPlatformBase() MpConditionsInput {
	c := conditionsWB()
	c.VatRatio = 457_380_000.0 / 380_012_404.594011 // ставка из плана июня
	k := 1 - c.SppPct
	shares := map[string]float64{}
	for block, share := range c.Shares {
		if block == BCostAgent {
			continue // выводится из норматива
		}
		shares[block] = share / k
	}
	// Прочие удержания: в Приложении Б доля округлена до 0,2 %, а в примере дельты
	// стоит сумма 763 473,873516 — берём точное отношение, иначе агентское как
	// остаток норматива разойдётся с эталоном на три с половиной тысячи.
	shares[BCostOther] = 763_473.873516 / 380_012_404.594010 / k
	c.Shares = shares
	c.NormShare = 0.428 // с марта 2026
	return c
}

func TestInverse_DeltaAcceptanceWildberriesJune(t *testing.T) {
	c := conditionsWBPlatformBase()
	// Эталон задаёт агентское явно: отсутствующая доля теперь не выводится из норматива.
	c.Shares[BCostAgent] = 20_137_208.38 / 262_208_559.17
	out, _ := computeMpPlatformInverse(MpInverseInput{
		SalesManagerGross: 457_380_000,
		Conditions:        c,
	})

	approxMoney(t, "продажи менеджера без НДС", out[BSalesManagerNet], 380_012_404.59)
	approxMoney(t, "продажи площадки с НДС", out[BSalesPlatGross], 315_592_200.00)
	approxMoney(t, "продажи площадки без НДС", out[BSalesPlatNet], 262_208_559.17)
	approxMoney(t, "комиссия площадки", out[BCommission], 117_803_845.42)
	approxMoney(t, "агентское вознаграждение", out[BCostAgent], 20_137_208.38)
	approxMoney(t, "итого прямые затраты", out[BPlatformCosts], 185_066_041.04)
	approxMoney(t, "себестоимость по отпускным ценам", out[BShipments], 158_914_278.28)
	approxMoney(t, "PL по площадке", out[BPlPlatform], 36_032_085.27)
	approx(t, "доля прямых затрат в обороте", out[BDirectShareTurnover], 0.256522)

	// Контроль норматива (лист D п. 6): норматив плюс доли статей вне норматива
	// обязаны дать долю прямых затрат, то есть контроль равен нулю.
	if got := out[BNormControl]; got > 1e-6 || got < -1e-6 {
		t.Errorf("контроль норматива: got %.8f, want 0", got)
	}
	// Контроль связности (лист A п. 7).
	approx(t, "1 − 1006/1045 = %СПП", 1-out[BSalesPlatNet]/out[BSalesManagerNet], 0.31)
}

// Отсутствующая доля остаётся пустой, даже если есть норматив площадки.
func TestInverse_AgentShareMissingIsNotInferred(t *testing.T) {
	c := conditionsWB()
	c.NormShare = 0.43
	delete(c.Shares, BCostAgent)

	out, _ := computeMpPlatformInverse(MpInverseInput{SalesManagerGross: 457_380_000, Conditions: c})

	platNet := out[BSalesPlatNet]
	normBase := 0.43 / (1 - c.SppPct)
	approx(t, "отсутствующая доля агентского", out[BCostAgent]/platNet, 0)
	approx(t, "норматив в базе долей", out[BNormShare], normBase)

	// Заданная руками доля норматив не трогает: условия площадки старше.
	c2 := conditionsWB()
	c2.NormShare = 0.43
	out2, _ := computeMpPlatformInverse(MpInverseInput{SalesManagerGross: 457_380_000, Conditions: c2})
	approx(t, "заданная доля агентского сохранена", out2[BCostAgent]/out2[BSalesPlatNet], 0.0530)
}

func TestInverse_SalesSppAndOnlyConfiguredShares(t *testing.T) {
	out, _ := computeMpPlatformInverse(MpInverseInput{
		SalesManagerGross: 120,
		Conditions: MpConditionsInput{VatRatio: 1.2, SppPct: 0.1,
			MarkupPct: 0.5, MarkupTotalPct: 0.2,
			Shares: map[string]float64{BCostAds: 0.1}},
	})
	approx(t, "выручка площадки без НДС", out[BSalesPlatNet], 90)
	approx(t, "расход по заданной доле", out[BCostAds], 9)
	approx(t, "отсутствующая доля не выводится", out[BCostAgent], 0)
	approx(t, "себестоимость из наценки", out[BShipments], 60)
}

// Удержания превысили норматив: Excel считает отрицательную долю молча, кабинет
// обязан обнулить строку — блокировку ставит МП-13 (правка 08.09.2026 п. 7).
func TestInverse_NormExceededZeroesAgentShare(t *testing.T) {
	c := conditionsWB()
	delete(c.Shares, BCostAgent)
	c.NormShare = 0.20 // меньше одной только комиссии (0,31)

	out, _ := computeMpPlatformInverse(MpInverseInput{SalesManagerGross: 457_380_000, Conditions: c})
	if out[BCostAgent] != 0 {
		t.Errorf("отрицательная доля агентского записана: %.2f", out[BCostAgent])
	}
	used, exceeded := mpNormExceeded(out[BNormShare], out[BCommission]/out[BSalesPlatNet], c.Shares)
	if !exceeded {
		t.Fatalf("превышение норматива не распознано: удержания %.4f при нормативе %.4f", used, out[BNormShare])
	}
}

// Норматива нет — статья ведёт себя как обычная доля (в том числе нулевая).
func TestInverse_NoNormKeepsAgentShareZero(t *testing.T) {
	c := conditionsWB()
	delete(c.Shares, BCostAgent)
	out, _ := computeMpPlatformInverse(MpInverseInput{SalesManagerGross: 457_380_000, Conditions: c})
	if out[BCostAgent] != 0 {
		t.Errorf("без норматива агентское должно остаться нулём: %.2f", out[BCostAgent])
	}
}
