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

func retailComparisonFactAt(s RetailSeries, codeCFO, year, month int) (float64, bool) {
	if s.ComparisonLoaded {
		v, ok := s.ComparisonFact[RetailCellKey{codeCFO, year, month}]
		return v, ok
	}
	return retailFactAt(s, codeCFO, year, month)
}

// ComputeRetailIndicators — все индикаторы §4 для одной строки за месяц плана.
//
// year/month — период карточки (месяц, на котором стоит форма). Тактика берётся
// из ячеек строки: то, что ввёл пользователь.
func ComputeRetailIndicators(row RetailRow, s RetailSeries, year, month int, planMonths []int) RetailIndicators {
	var ind RetailIndicators

	// Факт того же месяца прошлого года — Факт[M, Y−1] (§4).
	if v, ok := retailComparisonFactAt(s, row.CodeCFO, year-1, month); ok {
		ind.FactPrevYearMonth = fptr(v)
	}
	// Факт предыдущего месяца — Факт[M−1, Y]; для января это Факт[12, Y−1] (§4).
	py, pm := retailPrevMonth(year, month)
	if v, ok := retailComparisonFactAt(s, row.CodeCFO, py, pm); ok {
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

// RetailFxRates — курсы отображения ПО НАЗНАЧЕНИЮ колонки.
//
// Одного курса на строку не существует: в файле ТО (лист Июн_26, шапка строк 2–6)
// их четыре, и каждая колонка делится на свой — факт прошлого года на T2, ТАКТИКА
// на V2, факт текущего года на W2. При этом V2 = `Май_26!W2`, то есть курс тактики
// это КУРС ФАКТА ПРЕДЫДУЩЕГО МЕСЯЦА, а не плановый: константа 3,2 стоит в колонке
// U и подписана «план 26».
//
// Кабинет до 07.09.2026 пересчитывал всё одним справочным курсом (3,2 и 0,0376) —
// проверка на числах файла: «Розница РБ» 35 452 000 BYN даёт 12 749 781,03 USD,
// отношение 2,7806, а не 3,2.
//
// Каждый курс — «сколько нац. валюты в единице валюты отображения»: значение
// УМНОЖАЕТСЯ на него (как и прежний rate). Ноль означает «курса нет» — тогда
// работает Tactic как основной курс формы.
type RetailFxRates struct {
	Tactic       float64 `json:"tactic"`         // тактика, план периода, ожидание года
	FactCurrent  float64 `json:"fact_current"`   // факт текущего года и производные от него
	FactPrevYear float64 `json:"fact_prev_year"` // факт того же месяца прошлого года
	Strategy     float64 `json:"strategy"`       // стратегия (в файле — плановый курс)
}

// SameRetailFx — все колонки по одному курсу: пересчёт в BYN внутри страны, где
// разделения на сценарии нет, и совместимость со старым поведением.
func SameRetailFx(rate float64) RetailFxRates {
	return RetailFxRates{Tactic: rate, FactCurrent: rate, FactPrevYear: rate, Strategy: rate}
}

// with — курс назначения, если он задан, иначе основной (курс тактики).
func (r RetailFxRates) with(rate float64) float64 {
	if rate > 0 {
		return rate
	}
	return r.Tactic
}

// identity — пересчитывать нечего: все курсы единичные либо не заданы.
func (r RetailFxRates) identity() bool {
	for _, v := range []float64{r.Tactic, r.with(r.FactCurrent), r.with(r.FactPrevYear), r.with(r.Strategy)} {
		if v != 1 && v != 0 {
			return false
		}
	}
	return true
}

// ConvertRetailRows — слой ОТОБРАЖЕНИЯ в другой валюте (ТЗ §4: «пересчёт в
// BYN/USD — отдельный слой отображения, индикаторы на нём НЕ пересчитываются»).
//
// Поэтому пересчитываются только денежные величины, а проценты (LFL, LFM,
// % к стратегии, % вып.) остаются ТЕМИ ЖЕ ЧИСЛАМИ: отношение не зависит от
// валюты, а повторный расчёт по округлённым суммам дал бы другие проценты и
// расхождение с экраном согласования.
func ConvertRetailRows(rows []RetailRow, fx RetailFxRates) []RetailRow {
	if fx.identity() {
		return rows
	}
	tactic := fx.Tactic
	if tactic == 0 {
		tactic = 1
	}
	out := make([]RetailRow, len(rows))
	copy(out, rows)
	for i := range out {
		// Месячные ячейки — это ввод тактики, поэтому курс тактики.
		vals := make([]RetailValue, len(out[i].Values))
		copy(vals, out[i].Values)
		for j := range vals {
			if vals[j].Amount != nil {
				vals[j].Amount = fptr(*vals[j].Amount * tactic)
			}
		}
		out[i].Values = vals
		ind := out[i].Indicators
		scalePtr(&ind.FactPrevYearMonth, fx.with(fx.FactPrevYear))
		scalePtr(&ind.FactPrevMonth, fx.with(fx.FactCurrent))
		scalePtr(&ind.FactCurMonth, fx.with(fx.FactCurrent))
		scalePtr(&ind.AvgMonthFact, fx.with(fx.FactCurrent))
		scalePtr(&ind.Strategy, fx.with(fx.Strategy))
		scalePtr(&ind.TacticApproved, tactic)
		scalePtr(&ind.Tactic, tactic)
		ind.PeriodTotal *= tactic
		// Ожидание года смешивает факт закрытых месяцев с тактикой остальных;
		// в файле оно стоит в блоке тактики, поэтому и курс тактики.
		ind.YearExpectation *= tactic
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
