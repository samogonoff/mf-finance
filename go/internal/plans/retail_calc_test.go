package plans

import (
	"math"
	"testing"
)

// Тесты индикаторов формы «Розница» (ТЗ §4). Без БД: на входе строка и срезы.

func retApprox(t *testing.T, name string, got *float64, want float64) {
	t.Helper()
	if got == nil {
		t.Fatalf("%s: got nil, want %.6f", name, want)
	}
	if math.Abs(*got-want) > 1e-6 {
		t.Errorf("%s: got %.6f, want %.6f", name, *got, want)
	}
}

func retNil(t *testing.T, name string, got *float64) {
	t.Helper()
	if got != nil {
		t.Errorf("%s: ожидалось пусто (nil), получено %.6f", name, *got)
	}
}

// retailRow — строка с планом продаж по месяцам.
func retailRow(code int, lfl string, plan map[int]float64) RetailRow {
	r := RetailRow{CodeCFO: code, LFLEffective: lfl, LFLStatus: lfl}
	for m, v := range plan {
		amount := v
		r.Values = append(r.Values, RetailValue{
			Metric: MetricSales, Year: 2026, Month: m, Amount: &amount, Source: ValueManual,
		})
	}
	return r
}

// TestRetailIndicators_PrototypeFormulas — индикаторы совпадают с формулами
// прототипа DJ6 (LFL тактич.), DK6 (LFM тактич.) и DL6 (% к стратегии), а также
// с «% вып. тактич. плана» из ТЗ §4.
//
// Данные подобраны так, чтобы каждое отношение считалось «в уме»:
// Такт[07.2026]=120, Факт[07.2025]=100, Факт[06.2026]=150, Страт[07.2026]=160,
// Факт[07.2026]=96.
func TestRetailIndicators_PrototypeFormulas(t *testing.T) {
	ser := NewRetailSeries()
	ser.AddFact([]RetailFactCell{
		{CodeCFO: 100, Year: 2025, Month: 7, Amount: 100},
		{CodeCFO: 100, Year: 2026, Month: 6, Amount: 150},
		{CodeCFO: 100, Year: 2026, Month: 7, Amount: 96},
	})
	ser.AddStrategy([]RetailFactCell{{CodeCFO: 100, Year: 2026, Month: 7, Amount: 160}})
	ser.AddApproved([]RetailFactCell{{CodeCFO: 100, Year: 2026, Month: 7, Amount: 118}})

	row := retailRow(100, LFLYes, map[int]float64{7: 120})
	ind := ComputeRetailIndicators(row, ser, 2026, 7, []int{7, 8, 9, 10, 11, 12})

	retApprox(t, "факт M прошлого года", ind.FactPrevYearMonth, 100)
	retApprox(t, "факт предыдущего месяца", ind.FactPrevMonth, 150)
	retApprox(t, "план стратегический", ind.Strategy, 160)
	retApprox(t, "ранее утверждённая тактика", ind.TacticApproved, 118)
	// % вып. тактич. плана = IFERROR(Факт / Такт; 0) = 96/120 = 0.8
	retApprox(t, "% вып. тактич. плана", ind.PlanDonePct, 0.8)
	// DJ6: LFL тактич. = Такт/Факт[M,Y−1] − 1 = 120/100 − 1 = 0.2
	retApprox(t, "DJ6 LFL тактич.", ind.LFLTactic, 0.2)
	// DK6: LFM тактич. = Такт/Факт[M−1,Y] − 1 = 120/150 − 1 = −0.2
	retApprox(t, "DK6 LFM тактич.", ind.LFMTactic, -0.2)
	// DL6: % к стратегии = Такт/Страт − 1 = 120/160 − 1 = −0.25
	retApprox(t, "DL6 % к стратегии", ind.VsStrategyPct, -0.25)
}

// TestRetailIndicators_IferrorZeroDenominator — IFERROR(...;0) ТЗ §4: нулевой
// знаменатель даёт РОВНО НОЛЬ, а не −1 и не «пусто».
func TestRetailIndicators_IferrorZeroDenominator(t *testing.T) {
	ser := NewRetailSeries() // ни факта, ни стратегии
	row := retailRow(100, LFLYes, map[int]float64{7: 120})
	ind := ComputeRetailIndicators(row, ser, 2026, 7, []int{7})

	retApprox(t, "% вып. при нулевой тактике-знаменателе", ind.PlanDonePct, 0)
	retApprox(t, "LFL при отсутствии факта ПГ", ind.LFLTactic, 0)
	retApprox(t, "LFM при отсутствии факта пред. месяца", ind.LFMTactic, 0)
	retApprox(t, "% к стратегии при отсутствии стратегии", ind.VsStrategyPct, 0)
	retNil(t, "факт ПГ отсутствует", ind.FactPrevYearMonth)
	retNil(t, "стратегия отсутствует", ind.Strategy)
}

// TestRetailIndicators_NoBaselineStores — для «нового» и «ххх» LFL тактич. и
// % вып. НЕ рассчитываются (ТЗ §4): базы сравнения нет, выводим пусто.
func TestRetailIndicators_NoBaselineStores(t *testing.T) {
	ser := NewRetailSeries()
	ser.AddFact([]RetailFactCell{
		{CodeCFO: 100, Year: 2025, Month: 7, Amount: 100},
		{CodeCFO: 100, Year: 2026, Month: 6, Amount: 150},
		{CodeCFO: 100, Year: 2026, Month: 7, Amount: 96},
	})
	for _, lfl := range []string{LFLNew, LFLNoID} {
		row := retailRow(100, lfl, map[int]float64{7: 120})
		ind := ComputeRetailIndicators(row, ser, 2026, 7, []int{7})
		retNil(t, "LFL тактич. для статуса "+lfl, ind.LFLTactic)
		retNil(t, "% вып. для статуса "+lfl, ind.PlanDonePct)
		// LFM считается: запрет ТЗ касается только LFL и % вып.
		retApprox(t, "LFM тактич. для статуса "+lfl, ind.LFMTactic, 120.0/150.0-1)
	}
}

// TestRetailYearExpectation_ClosedMonthsFromCalendar — «Ожидание года» (ТЗ §4.4)
// берёт закрытые месяцы ТОЛЬКО из календаря, а не «по наличию чисел».
//
// Ключевой случай: у мая ЕСТЬ и факт, и тактика. Если месяц закрыт календарём —
// в ожидание идёт факт; если не закрыт — тактика, даже когда факт присутствует.
func TestRetailYearExpectation_ClosedMonthsFromCalendar(t *testing.T) {
	ser := NewRetailSeries()
	ser.AddFact([]RetailFactCell{
		{CodeCFO: 100, Year: 2026, Month: 1, Amount: 10},
		{CodeCFO: 100, Year: 2026, Month: 2, Amount: 20},
		{CodeCFO: 100, Year: 2026, Month: 5, Amount: 500}, // факт есть, но месяц открыт
	})
	row := retailRow(100, LFLYes, map[int]float64{
		1: 999, 2: 999, // тактика в закрытых месяцах игнорируется
		5: 50, 6: 60,
	})

	// Календарь: закрыты январь и февраль.
	ser.ClosedMonths = map[int]bool{1: true, 2: true}
	got := RetailYearExpectation(row, ser, 2026)
	// 10 + 20 (факт закрытых) + 50 + 60 (тактика открытых) = 140
	if math.Abs(got-140) > 1e-9 {
		t.Errorf("ожидание года при закрытых 01–02: got %.2f, want 140", got)
	}

	// Тот же набор данных, но календарь НИЧЕГО не закрыл: ожидание = только тактика.
	ser.ClosedMonths = map[int]bool{}
	got = RetailYearExpectation(row, ser, 2026)
	// 999 + 999 + 50 + 60 = 2108; факт мая (500) не участвует — месяц открыт.
	if math.Abs(got-2108) > 1e-9 {
		t.Errorf("ожидание года без закрытых месяцев: got %.2f, want 2108", got)
	}

	// И наоборот: календарь закрыл май, у которого есть и факт, и тактика.
	ser.ClosedMonths = map[int]bool{5: true}
	got = RetailYearExpectation(row, ser, 2026)
	// 999 + 999 (тактика открытых 01,02) + 500 (факт закрытого мая) + 60 = 2558
	if math.Abs(got-2558) > 1e-9 {
		t.Errorf("ожидание года при закрытом мае: got %.2f, want 2558", got)
	}
}

// TestRetailIndicators_PeriodTotalAndAvgFact — «Итого за период» и «Ср. мес. факт».
func TestRetailIndicators_PeriodTotalAndAvgFact(t *testing.T) {
	ser := NewRetailSeries()
	// Факт за январь–май 2026: 100, 200, 300, 0 и «нет данных» за май.
	ser.AddFact([]RetailFactCell{
		{CodeCFO: 100, Year: 2026, Month: 1, Amount: 100},
		{CodeCFO: 100, Year: 2026, Month: 2, Amount: 200},
		{CodeCFO: 100, Year: 2026, Month: 3, Amount: 300},
		{CodeCFO: 100, Year: 2026, Month: 4, Amount: 0},
	})
	row := retailRow(100, LFLYes, map[int]float64{6: 10, 7: 20, 8: 30})
	ind := ComputeRetailIndicators(row, ser, 2026, 6, []int{6, 7, 8, 9})

	if math.Abs(ind.PeriodTotal-60) > 1e-9 {
		t.Errorf("итого за период: got %.2f, want 60", ind.PeriodTotal)
	}
	// AVG по месяцам 1..5, где факт ЕСТЬ: (100+200+300+0)/4 = 150.
	retApprox(t, "ср. мес. факт", ind.AvgMonthFact, 150)
}

// TestRetailIndicators_JanuaryPrevMonthCrossesYear — для января «факт
// предыдущего месяца» = Факт[12, Y−1] (ТЗ §4).
func TestRetailIndicators_JanuaryPrevMonthCrossesYear(t *testing.T) {
	ser := NewRetailSeries()
	ser.AddFact([]RetailFactCell{{CodeCFO: 100, Year: 2025, Month: 12, Amount: 777}})
	row := retailRow(100, LFLYes, map[int]float64{1: 800})
	ind := ComputeRetailIndicators(row, ser, 2026, 1, []int{1})
	retApprox(t, "факт декабря прошлого года", ind.FactPrevMonth, 777)
	retApprox(t, "LFM через границу года", ind.LFMTactic, 800.0/777.0-1)
}

// TestConvertRetailRows_PercentsNotRecomputed — слой отображения (ТЗ §4):
// денежные величины пересчитываются, проценты остаются теми же числами.
func TestConvertRetailRows_PercentsNotRecomputed(t *testing.T) {
	ser := NewRetailSeries()
	ser.AddFact([]RetailFactCell{{CodeCFO: 100, Year: 2025, Month: 7, Amount: 100}})
	rows := ComputeRetailForm([]RetailRow{retailRow(100, LFLYes, map[int]float64{7: 120})},
		ser, 2026, 7, []int{7})
	before := rows[0].Indicators

	converted := ConvertRetailRows(rows, 0.5)
	after := converted[0].Indicators

	retApprox(t, "факт ПГ пересчитан", after.FactPrevYearMonth, 50)
	if math.Abs(after.PeriodTotal-60) > 1e-9 {
		t.Errorf("итого за период пересчитано: got %.2f, want 60", after.PeriodTotal)
	}
	retApprox(t, "LFL НЕ пересчитан", after.LFLTactic, *before.LFLTactic)
	// Исходные строки не должны мутировать: слой отображения — копия.
	retApprox(t, "исходный факт ПГ не изменился", rows[0].Indicators.FactPrevYearMonth, 100)
}
