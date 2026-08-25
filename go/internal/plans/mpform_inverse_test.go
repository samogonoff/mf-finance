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
	approxMoney(t, "прямые затраты по площадке", out[BPlatformCosts], 185_066_041)
	approxMoney(t, "PL по площадке (от прямых затрат)", out[BPlPlatform], 36_032_085)
	approxMoney(t, "PL от себестоимости общей", out[BPlPlatformTotalCost], 100_359_654)

	// Доли-индикаторы (ТЗ §3.4): 48,70 % и 25,65 %.
	approx(t, "доля прямых затрат площадки", out[BDirectShare], 0.4870)
	approx(t, "доля прямых затрат в обороте", out[BDirectShareTurnover], 0.2565)
	approx(t, "маржинальность розничная", out[BRetailMarginPct], 0.3939)
	approx(t, "маржа (gross), %", out[BGrossMarginPct], 0.6393)
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

	approxMoney(t, "PL (сумма) по форме", got.PL, 62_252_120)
	approx(t, "PL (%)", got.PLPct, 0.1018)
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

// Ручное переопределение расчётной суммы не перезатирается автопересчётом
// (ТЗ §7.2): форма показывает и расчёт, и введённое значение.
func TestInverse_ManualOverrideNotOverwritten(t *testing.T) {
	in := MpInverseInput{
		SalesManagerGross: 457_380_000,
		Conditions:        conditionsWB(),
		Overrides:         map[string]float64{BCostAds: 10_000_000},
	}
	out, log := computeMpPlatformInverse(in)

	if out[BCostAds] != 10_000_000 {
		t.Fatalf("ручное значение должно сохраниться, получено %.2f", out[BCostAds])
	}
	var cell MpInverseCell
	for _, c := range log {
		if c.BlockType == BCostAds {
			cell = c
		}
	}
	if !cell.Manual {
		t.Fatal("ячейка должна быть помечена как ручная")
	}
	// Расчёт по условиям продолжает считаться и показывается рядом.
	approxMoney(t, "расчётное значение статьи", cell.Calculated, 380_012_405*0.04)
	// Прямые затраты пересчитаны с ручным значением, а не с расчётным.
	wantDirect := out[BCommission] + 10_000_000 +
		380_012_405*(0.0530+0.0030+0.0315+0.0315+0.0010+0.0150+0.0020)
	approxMoney(t, "прямые затраты с ручной правкой", out[BPlatformCosts], wantDirect)
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
