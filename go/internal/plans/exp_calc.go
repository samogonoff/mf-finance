package plans

import "math"

// Расчёты формы этапа 1.5 (ТЗ §6) — чистые функции.
//
// §6.1 «Фонд оплаты труда розницы от продаж» и §6.2 «Аренда» переехали сюда из
// формы продаж (ответ 35): там точка входа закрыта, а формулы переиспользуются.
//
// Порог по §6.1 ограничивает БАЗУ НАЧИСЛЕНИЯ, а не саму сумму выплат:
//
//	ФОТ = MIN( ПланПродаж ; База × Порог ) × УдельныйВес
//
// Формулировка встречи — «выше 106 % никому не могут выплатить планы».

// Коды параметров периода (ТЗ §6.1).
const (
	ExpParamPayrollShare = "payroll_share" // удельный вес ФОТ в продажах
	ExpParamPayrollCap   = "payroll_cap"   // порог базы начисления, по умолчанию 1,06
)

// ExpPayrollCapDefault — значение порога по умолчанию (ТЗ §6.1).
const ExpPayrollCapDefault = 1.06

// ExpPayrollInput — вход расчёта ФОТ по одной строке-месяцу.
type ExpPayrollInput struct {
	SalesPlan float64 // план продаж магазина на месяц
	Base      float64 // база ограничения (по умолчанию факт того же месяца прошлого года)
	HasBase   bool    // базы может не быть — новый магазин
	Cap       float64 // порог базы начисления
	Share     float64 // удельный вес
}

// ExpPayrollResult — результат расчёта одной ячейки.
type ExpPayrollResult struct {
	Amount float64
	// Capped — ограничение сработало: строка подсвечивается с пояснением
	// «ФОТ ограничен порогом» (ТЗ §6.1).
	Capped bool
	// NoBase — базы по магазину нет, ограничение не применялось. ТЗ требует
	// пометить такую строку отдельно: это не то же самое, что «порог не сработал».
	NoBase bool
}

// ExpPayrollFromSales — ФОТ от продаж (ТЗ §6.1).
func ExpPayrollFromSales(in ExpPayrollInput) ExpPayrollResult {
	share := in.Share
	if share <= 0 {
		return ExpPayrollResult{}
	}
	cap := in.Cap
	if cap <= 0 {
		cap = ExpPayrollCapDefault
	}
	if !in.HasBase || in.Base == 0 {
		// Базы нет — ограничение не применяем, считаем от плана (ТЗ §6.1).
		return ExpPayrollResult{Amount: round2(in.SalesPlan * share), NoBase: true}
	}
	limit := in.Base * cap
	basis := in.SalesPlan
	capped := false
	if limit < basis {
		basis, capped = limit, true
	}
	return ExpPayrollResult{Amount: round2(basis * share), Capped: capped}
}

// ExpRentInput — вход расчёта аренды (ТЗ §6.2 с уточнением ответа 37).
type ExpRentInput struct {
	PrevFact    float64 // факт аренды предыдущего периода по тому же ЦФО
	HasPrev     bool
	SalesPlan   float64
	FixedPart   float64 // фиксированная часть договора
	Threshold   float64 // порог оборота
	Rate        float64 // ставка переменной части
	HasContract bool    // порог и ставка заданы справочником
}

// ExpRentResult — результат расчёта аренды.
type ExpRentResult struct {
	Amount float64
	// FromPrev — посчитано по факту предыдущего периода: строка помечается
	// «по факту предыдущего периода» (ТЗ §6.2).
	FromPrev bool
}

// ExpRentPlan — аренда (ТЗ §6.2).
//
// Ответ 37 (10.09.2026): в эталоне аренда задана помесячными суммами, формулы от
// оборота нет — расчёт по договору был отменён ещё правкой ТЗ 02.09.2026.
// Поэтому ветка «от оборота» СПЯЩАЯ: она срабатывает, только если по магазину
// заданы и порог, и ставка. Справочник пустой — ветка не работает, и это
// ожидаемое поведение, а не сбой.
func ExpRentPlan(in ExpRentInput) ExpRentResult {
	if in.HasContract && in.Threshold > 0 && in.Rate > 0 {
		over := in.SalesPlan - in.Threshold
		if over < 0 {
			over = 0
		}
		return ExpRentResult{Amount: round2(in.FixedPart + over*in.Rate)}
	}
	if in.HasPrev {
		return ExpRentResult{Amount: round2(in.PrevFact), FromPrev: true}
	}
	return ExpRentResult{}
}

func round2(v float64) float64 { return math.Round(v*100) / 100 }
