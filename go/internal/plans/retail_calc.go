package plans

// Расчётные индикаторы формы «Розница» (ТЗ §4). Чистые функции: на входе строка
// формы, срезы факта/стратегии/истории и календарь, на выходе — числа. Ни БД, ни
// HTTP, ни времени — поэтому весь §4 покрыт табличными тестами
// (retail_calc_test.go).
//
// Две вещи, которые легко сделать неправильно и которые здесь зафиксированы:
//
//  1. IFERROR(...;0) из ТЗ — это НЕ «пусто». Формулы §4 при нулевом знаменателе
//     дают РОВНО НОЛЬ (так считает прототип), и мы повторяем это буквально.
//     «Пусто» (nil) появляется только там, где ТЗ прямо запрещает расчёт: для
//     магазинов «новый» и «ххх» LFL тактич. и % вып. НЕ рассчитываются, потому
//     что базы сравнения нет. Вернуть там 0 значило бы соврать: −100 % роста.
//
//  2. «Ожидание года» (§4.4) складывает ФАКТ по ЗАКРЫТЫМ месяцам и ТАКТИКУ по
//     остальным, причём закрытость месяца берётся ТОЛЬКО из производственного
//     календаря (CalendarStore.ClosedMonths), а не «по наличию чисел». Иначе
//     незаполненный план в открытом месяце молча превращался бы в «факт = 0».

// iferrorDiv — деление в семантике IFERROR(a/b; 0) прототипа.
func iferrorDiv(a, b float64) float64 {
	if b == 0 {
		return 0
	}
	return a / b
}

// iferrorRatioMinus1 — IFERROR(a/b − 1; 0). При b=0 всё выражение = 0, а не −1:
// в прототипе IFERROR перехватывает ошибку деления целиком.
func iferrorRatioMinus1(a, b float64) float64 {
	if b == 0 {
		return 0
	}
	return a/b - 1
}

// retailFactAt — факт магазина за (год, месяц) из среза. Второй результат —
// было ли значение вообще (нужно для W-06 и «ср. мес. факт»).
func retailFactAt(s RetailSeries, codeCFO, year, month int) (float64, bool) {
	v, ok := s.Fact[RetailCellKey{codeCFO, year, month}]
	return v, ok
}

// ComputeRetailIndicators — все индикаторы §4 для одной строки за месяц плана.
//
// year/month — период карточки (месяц, на котором стоит форма). Тактика берётся
// из ячеек строки: то, что ввёл пользователь.
func ComputeRetailIndicators(row RetailRow, s RetailSeries, year, month int, planMonths []int) RetailIndicators {
	var ind RetailIndicators

	// Факт того же месяца прошлого года — Факт[M, Y−1] (§4).
	if v, ok := retailFactAt(s, row.CodeCFO, year-1, month); ok {
		ind.FactPrevYearMonth = fptr(v)
	}
	// Факт предыдущего месяца — Факт[M−1, Y]; для января это Факт[12, Y−1] (§4).
	py, pm := retailPrevMonth(year, month)
	if v, ok := retailFactAt(s, row.CodeCFO, py, pm); ok {
		ind.FactPrevMonth = fptr(v)
	}
	// Факт текущего месяца — нужен для % вып. тактич. плана и % факт/факт ПГ.
	if v, ok := retailFactAt(s, row.CodeCFO, year, month); ok {
		ind.FactCurMonth = fptr(v)
	}
	// План стратегический — Страт[M, Y] (§4).
	if v, ok := s.Strategy[RetailCellKey{row.CodeCFO, year, month}]; ok {
		ind.Strategy = fptr(v)
	}
	// Ранее утверждённая тактика — из версий приложения / истории плана (§4).
	if v, ok := s.Approved[RetailCellKey{row.CodeCFO, year, month}]; ok {
		ind.TacticApproved = fptr(v)
	}

	tacticPtr := row.valueOf(year, month)
	if tacticPtr != nil {
		ind.Tactic = fptr(*tacticPtr)
	}
	tactic := 0.0
	if tacticPtr != nil {
		tactic = *tacticPtr
	}

	factCur, factPrevYear, factPrevMonth, strategy := 0.0, 0.0, 0.0, 0.0
	if ind.FactCurMonth != nil {
		factCur = *ind.FactCurMonth
	}
	if ind.FactPrevYearMonth != nil {
		factPrevYear = *ind.FactPrevYearMonth
	}
	if ind.FactPrevMonth != nil {
		factPrevMonth = *ind.FactPrevMonth
	}
	if ind.Strategy != nil {
		strategy = *ind.Strategy
	}

	noBase := retailNoBaseline(row.LFLEffective)

	// % вып. тактич. плана = IFERROR(Факт[M,Y] / Такт[M,Y]; 0).
	// Для «новый»/«ххх» не рассчитывается (§4) — базы сравнения нет.
	if !noBase {
		ind.PlanDonePct = fptr(iferrorDiv(factCur, tactic))
	}
	// LFL тактич. = IFERROR(Такт[M,Y] / Факт[M,Y−1] − 1; 0)  (прототип DJ6).
	if !noBase {
		ind.LFLTactic = fptr(iferrorRatioMinus1(tactic, factPrevYear))
	}
	// LFM тактич. = IFERROR(Такт[M,Y] / Факт[M−1,Y] − 1; 0)  (прототип DK6).
	// Считается и для новых магазинов: предыдущий месяц у них есть, как только
	// магазин открылся, — запрет §4 касается только LFL и % вып.
	ind.LFMTactic = fptr(iferrorRatioMinus1(tactic, factPrevMonth))
	// % к стратегии = IFERROR(Такт[M,Y] / Страт[M,Y] − 1; 0)  (прототип DL6).
	ind.VsStrategyPct = fptr(iferrorRatioMinus1(tactic, strategy))

	// Итого за период = SUM(месяцы периода) (§4).
	for _, m := range planMonths {
		if v := row.valueOf(year, m); v != nil {
			ind.PeriodTotal += *v
		}
	}

	// Ср. мес. факт = AVG(Факт[1..M−1, Y]) (§4). Усредняем по месяцам, за которые
	// факт ЕСТЬ: делить на (M−1) с нулями за месяцы без данных значило бы занижать
	// среднее у магазинов, открытых посреди года. Нет ни одного месяца → пусто.
	sum, cnt := 0.0, 0
	for m := 1; m < month; m++ {
		if v, ok := retailFactAt(s, row.CodeCFO, year, m); ok {
			sum += v
			cnt++
		}
	}
	if cnt > 0 {
		ind.AvgMonthFact = fptr(sum / float64(cnt))
	}

	// Ожидание года = Σ Факт[закрытые месяцы] + Σ Тактика[остальные] (§4.4).
	ind.YearExpectation = RetailYearExpectation(row, s, year)

	// Откл. ожидания к факту прошлого года = Ожидание / Факт[прошлый год] − 1.
	prevYearTotal := 0.0
	for m := 1; m <= 12; m++ {
		if v, ok := retailFactAt(s, row.CodeCFO, year-1, m); ok {
			prevYearTotal += v
		}
	}
	ind.YearExpVsPrevPct = fptr(iferrorRatioMinus1(ind.YearExpectation, prevYearTotal))

	return ind
}

// RetailYearExpectation — «Ожидание года» (ТЗ §4.4).
//
// Признак закрытого месяца — ТОЛЬКО RetailSeries.ClosedMonths, наполненный из
// CalendarStore.ClosedMonths. По закрытым месяцам берём ФАКТ (даже если он 0 —
// месяц закрыт, значит цифра окончательная), по остальным — ТАКТИКУ.
func RetailYearExpectation(row RetailRow, s RetailSeries, year int) float64 {
	total := 0.0
	for m := 1; m <= 12; m++ {
		if s.ClosedMonths[m] {
			if v, ok := retailFactAt(s, row.CodeCFO, year, m); ok {
				total += v
			}
			continue
		}
		if v := row.valueOf(year, m); v != nil {
			total += *v
		}
	}
	return total
}

// ComputeRetailForm — индикаторы всех строк формы (ТЗ §4). Отдельная функция,
// чтобы порядок и состав расчёта были одинаковы в сетке ввода, в экране
// согласования и в экспорте.
func ComputeRetailForm(rows []RetailRow, s RetailSeries, year, month int, planMonths []int) []RetailRow {
	out := make([]RetailRow, len(rows))
	copy(out, rows)
	for i := range out {
		out[i].Indicators = ComputeRetailIndicators(out[i], s, year, month, planMonths)
	}
	return out
}

// ConvertRetailRows — слой ОТОБРАЖЕНИЯ в другой валюте (ТЗ §4: «пересчёт в
// BYN/USD — отдельный слой отображения, индикаторы на нём НЕ пересчитываются»).
//
// Поэтому пересчитываются только денежные величины, а проценты (LFL, LFM,
// % к стратегии, % вып.) остаются ТЕМИ ЖЕ ЧИСЛАМИ: отношение не зависит от
// валюты, а повторный расчёт по округлённым суммам дал бы другие проценты и
// расхождение с экраном согласования.
func ConvertRetailRows(rows []RetailRow, rate float64) []RetailRow {
	if rate == 1 || rate == 0 {
		return rows
	}
	out := make([]RetailRow, len(rows))
	copy(out, rows)
	for i := range out {
		vals := make([]RetailValue, len(out[i].Values))
		copy(vals, out[i].Values)
		for j := range vals {
			if vals[j].Amount != nil {
				vals[j].Amount = fptr(*vals[j].Amount * rate)
			}
		}
		out[i].Values = vals
		ind := out[i].Indicators
		scalePtr(&ind.FactPrevYearMonth, rate)
		scalePtr(&ind.FactPrevMonth, rate)
		scalePtr(&ind.FactCurMonth, rate)
		scalePtr(&ind.Strategy, rate)
		scalePtr(&ind.TacticApproved, rate)
		scalePtr(&ind.Tactic, rate)
		scalePtr(&ind.AvgMonthFact, rate)
		ind.PeriodTotal *= rate
		ind.YearExpectation *= rate
		out[i].Indicators = ind
	}
	return out
}

// scalePtr — пересчёт денежного индикатора (nil остаётся nil: «пусто» — это
// отсутствие базы сравнения, а не нулевая сумма).
func scalePtr(p **float64, rate float64) {
	if *p == nil {
		return
	}
	*p = fptr(**p * rate)
}
