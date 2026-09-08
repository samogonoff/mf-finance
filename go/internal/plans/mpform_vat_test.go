package plans

import (
	"math"
	"testing"
)

// План площадки в слое стратегии: пары «с НДС / без НДС» на уровне цены менеджера
// и цены площадки. Числа — из разбора var/Формулы_форм_07.09.2026.xlsx (июнь 2026,
// Wildberries): отношение по менеджеру 1,203592, по цене площадки — своё.
func planWB() map[string]float64 {
	return map[string]float64{
		layerKey(335, BSalesManagerGross): 457_380_000,
		layerKey(335, BSalesManagerNet):   380_012_405,
		layerKey(335, BSalesPlatGross):    315_592_200,
		layerKey(335, BSalesPlatNet):      262_208_559,
	}
}

func TestVatBasis_FromPlanRatio(t *testing.T) {
	b := mpVatBasis(planWB(), nil, 335, 2026, 6, 0.20)
	if !b.FromPlan || b.Missing {
		t.Fatal("базис обязан прийти из плана, когда пара сумм есть")
	}
	// Ставка ОДНА на оба уровня цены (правка 08.09.2026): 1046 / 1045 плана.
	approx(t, "отношение из плана", b.Ratio, 457_380_000.0/380_012_405.0)
	if b.Year != 2026 || b.Month != 6 {
		t.Errorf("период базиса: got %d-%d, want 2026-6", b.Year, b.Month)
	}
	if math.Abs(b.Rate()-0.20) < 0.001 {
		t.Errorf("ставка совпала с законодательной: %.6f", b.Rate())
	}
}

// Плана месяца нет — берётся план ПРЕДЫДУЩЕГО месяца той же площадки
// (правка 08.09.2026 п. 1), а не ставка справочника.
func TestVatBasis_FallsBackToPreviousMonthPlan(t *testing.T) {
	b := mpVatBasis(map[string]float64{}, planWB(), 335, 2026, 6, 0.196)
	if !b.FromPlan || b.Missing {
		t.Fatal("план предыдущего месяца обязан считаться базой")
	}
	approx(t, "отношение из плана мая", b.Ratio, 457_380_000.0/380_012_405.0)
	if b.Year != 2026 || b.Month != 5 {
		t.Errorf("период базиса: got %d-%d, want 2026-5", b.Year, b.Month)
	}
}

// Нет ни того, ни другого — суммы считаются по ставке справочника, но базис
// помечен Missing: отправку такой формы блокирует МП-12.
func TestVatBasis_MissingBlocksSubmit(t *testing.T) {
	b := mpVatBasis(map[string]float64{}, map[string]float64{}, 335, 2026, 6, 0.196)
	if b.FromPlan || !b.Missing {
		t.Fatal("без плана базис обязан помечаться как отсутствующий")
	}
	approx(t, "аварийное отношение", b.Ratio, 1.196)
}

// Мусор в плановой паре (разные периоды, разные единицы) не должен молча уехать
// в суммы: отношение вне 1,00…1,50 отбрасывается.
func TestVatBasis_ImplausibleRatioIgnored(t *testing.T) {
	plan := map[string]float64{
		layerKey(335, BSalesManagerGross): 457_380_000,
		layerKey(335, BSalesManagerNet):   4_500_000, // отношение ~101
	}
	b := mpVatBasis(plan, nil, 335, 2026, 6, 0.20)
	if b.FromPlan {
		t.Fatal("неправдоподобное отношение принято за плановое")
	}
	approx(t, "ставка фолбэка", b.Rate(), 0.20)
}

// Каскад делит каждый уровень цены на СВОЁ отношение (прототип: строки 17 и 32).
func TestComputeMpPlatform_UsesPlanRatios(t *testing.T) {
	b := mpVatBasis(planWB(), nil, 335, 2026, 6, 0.20)
	out := computeMpPlatform(map[string]float64{
		BSalesManagerGross: 375_286_090.33,
		BSPP:               0.2560,
	}, b)

	approxMoney(t, "продажи менеджера без НДС", out[BSalesManagerNet], 375_286_090.33/b.Ratio)
	approxMoney(t, "продажи площадки с НДС", out[BSalesPlatGross], 375_286_090.33*(1-0.2560))
	approxMoney(t, "продажи площадки без НДС", out[BSalesPlatNet], out[BSalesPlatGross]/b.Ratio)
	// Контроль связности (правка 08.09.2026, лист A п. 7): на одной ставке
	// 1 − [1006]/[1045] обязано равняться %СПП.
	approx(t, "контроль связности", 1-out[BSalesPlatNet]/out[BSalesManagerNet], 0.2560)
	// Ставка 20 % дала бы ровно 312 738 408,61 — то самое число с экрана, из-за
	// которого дефект и был найден. По отношению из плана результат другой.
	if math.Abs(out[BSalesManagerNet]-312_738_408.61) < 10 {
		t.Errorf("каскад по-прежнему делит на 1,2: %.2f", out[BSalesManagerNet])
	}
}

// Шапка формы: одно число только при одинаковых ставках площадок.
func TestHeaderVat_MixedRates(t *testing.T) {
	v, mixed := headerVat(map[int]float64{335: 0.2036, 336: 0.2036})
	if mixed {
		t.Error("одинаковые ставки помечены как разные")
	}
	approx(t, "ставка шапки", v, 0.2036)

	if _, mixed := headerVat(map[int]float64{335: 0.2036, 954: 0.1662}); !mixed {
		t.Error("разные ставки площадок обязаны помечаться как разные")
	}
	if _, mixed := headerVat(map[int]float64{}); mixed {
		t.Error("пустой набор не может быть «разным»")
	}
}
