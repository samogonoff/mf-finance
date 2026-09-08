package plans

import "testing"

// Курс тактики розницы — курс ФАКТА ПРЕДЫДУЩЕГО МЕСЯЦА (файл ТО: V2 = Май_26!W2),
// а не плановые 3,2. Проверяем на слое отображения: месячные ячейки (ввод тактики)
// и индикаторы тактики пересчитываются одним курсом, факт — своим.
func TestConvertRetailRows_ColumnsUseOwnRates(t *testing.T) {
	rows := []RetailRow{{
		CodeCFO: 101,
		Values:  []RetailValue{{Year: 2026, Month: 6, Amount: fptr(35_452_000)}},
		Indicators: RetailIndicators{
			Tactic:            fptr(35_452_000),
			FactCurMonth:      fptr(30_000_000),
			FactPrevYearMonth: fptr(28_000_000),
			Strategy:          fptr(40_000_000),
			PeriodTotal:       35_452_000,
			LFLTactic:         fptr(0.25),
		},
	}}
	// Курсы заданы как «умножить, чтобы получить валюту отображения»: 1/курс BYN→USD.
	fx := RetailFxRates{
		Tactic:       1 / 2.7805967, // курс факта мая 2026 (Май_26!W2)
		FactCurrent:  1 / 2.80051, // факт 2026
		FactPrevYear: 1 / 2.9836,  // факт 2025
		Strategy:     1 / 3.2,     // «план 26»
	}
	out := ConvertRetailRows(rows, fx)
	ind := out[0].Indicators

	// Контрольное число файла: 35 452 000 BYN → 12 749 781,03 USD.
	approxMoney(t, "тактика в USD", *ind.Tactic, 12_749_781.03)
	approxMoney(t, "ячейка месяца в USD", *out[0].Values[0].Amount, 12_749_781.03)
	approxMoney(t, "итого за период", ind.PeriodTotal, 12_749_781.03)
	// Плановый курс 3,2 дал бы 11 078 750 — именно так считал кабинет.
	if diff := *ind.Tactic - 35_452_000/3.2; diff < 10 && diff > -10 {
		t.Error("тактика по-прежнему пересчитана плановым курсом 3,2")
	}
	approxMoney(t, "факт текущего года", *ind.FactCurMonth, 30_000_000/2.80051)
	approxMoney(t, "факт прошлого года", *ind.FactPrevYearMonth, 28_000_000/2.9836)
	approxMoney(t, "стратегия", *ind.Strategy, 40_000_000/3.2)
	// Проценты — те же числа: пересчёт валюты их не касается (ТЗ §4).
	approx(t, "LFL не пересчитан", *ind.LFLTactic, 0.25)
}

// Курс назначения не задан — работает курс тактики, а не единица: иначе колонка
// молча осталась бы в нац. валюте рядом с пересчитанными.
func TestRetailFxRates_FallbackToTactic(t *testing.T) {
	fx := RetailFxRates{Tactic: 0.5}
	rows := []RetailRow{{Indicators: RetailIndicators{FactCurMonth: fptr(100), Strategy: fptr(100)}}}
	out := ConvertRetailRows(rows, fx)
	approxMoney(t, "факт по курсу тактики", *out[0].Indicators.FactCurMonth, 50)
	approxMoney(t, "стратегия по курсу тактики", *out[0].Indicators.Strategy, 50)
}

// Сценарий — измерение справочника: строка сценария старше общей, а строки
// прежнего справочника (сценарий подписан словами) продолжают работать.
func TestPickFx_ScenarioWins(t *testing.T) {
	rows := []FxRow{
		{Currency: "USD", Year: 0, Month: 0, RateBYN: 3.2, Scenario: "Тактика бюджет (таргеты)"},
		{Currency: "USD", Year: 2026, Month: 5, RateBYN: 2.7806, Scenario: "fact"},
		{Currency: "USD", Year: 0, Month: 0, RateBYN: 3.2, Scenario: "plan"},
	}
	if got := pickFx(rows, "USD", 2026, 5, FxScenarioFact); got != 2.7806 {
		t.Errorf("курс факта мая: got %v, want 2.7806", got)
	}
	if got := pickFx(rows, "USD", 2026, 6, FxScenarioPlan); got != 3.2 {
		t.Errorf("плановый курс: got %v, want 3.2", got)
	}
	// Сценария нет ни своего, ни пустого — берём любую строку валюты, иначе
	// пересчёт стал бы единичным и суммы в USD сравнялись бы с BYN.
	if got := pickFx(rows, "USD", 2026, 6, FxScenarioFact); got != 3.2 {
		t.Errorf("фолбэк на любую строку: got %v, want 3.2", got)
	}
	// Строки прежнего справочника (0034) читаются как сценарий «tactic».
	if got := pickFx(rows, "USD", 2026, 6, FxScenarioTactic); got != 3.2 {
		t.Errorf("сценарий словами: got %v, want 3.2", got)
	}
}
