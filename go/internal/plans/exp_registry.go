package plans

// Реестры формы этапа 1.5 (ТЗ §5) — модель и ЧИСТЫЕ формулы.
//
// Ни БД, ни HTTP здесь нет намеренно: контрольные числа эталона (§5.1–§5.4 и
// находка Н3) проверяются табличными тестами без базы, как у формы МП. Любое
// расхождение с ними — регресс.
//
// Уточнения ответов аналитика 10.09.2026:
//   - 38: ставки соцвзносов НЕТ. Реестр ФОТ содержит только «ФОТ чистый», налоги
//     от выплат — самостоятельные статьи 26, 86–89 со своими суммами. Расчёт
//     «Налоги = ФОТ × Ставка» не делается;
//   - Н3: в командировках появились билеты по нормативу перелёта и проживание
//     отдельной статьёй 30 (не 27);
//   - 40: пара статей ФОТ выбирается по цепочке «код ЦФО → ЮЛ → пара».

// Виды реестров.
const (
	ExpRegPayroll   = "payroll"
	ExpRegTravel    = "travel"
	ExpRegLogistics = "logistics"
	ExpRegIT        = "it"
)

// ExpRegKinds — все виды реестра в порядке ТЗ §5.
func ExpRegKinds() []string {
	return []string{ExpRegPayroll, ExpRegTravel, ExpRegLogistics, ExpRegIT}
}

// ExpRegKindKnown — существует ли такой реестр.
func ExpRegKindKnown(kind string) bool {
	for _, k := range ExpRegKinds() {
		if k == kind {
			return true
		}
	}
	return false
}

// Статьи PL, которые наполняются реестрами.
const (
	ExpPLSalary      = 24 // заработная плата — итог реестра ФОТ (ответ 40)
	ExpPLTravel      = 27 // расходы на командировки: билеты и командировочные (Н3)
	ExpPLLodging     = 30 // проживание в командировке — ОТДЕЛЬНАЯ статья (Н3)
	ExpPLIT          = 48 // ИТ-расходы
	ExpPLLogExport   = 51 // грузоперевозки экспорт
	ExpPLLogInternal = 52 // грузоперевозки развоз и перемещение
	// ExpCFOMarketplaceDept — «Отдел маркетплейса»: под ним публикуются реестр
	// логистики (ТЗ §5.3) и строки ИТ с признаком переноса (§5.4).
	ExpCFOMarketplaceDept = 250
)

// ExpRegRow — строка реестра. Месяцы в Months: nil-значение — не заполнено.
type ExpRegRow struct {
	ID        int64           `json:"id"`
	Kind      string          `json:"kind"`
	SortOrder int             `json:"sort_order"`
	CodeCFO   int             `json:"code_cfo"`
	CodePL    int             `json:"code_pl"`
	MoveTo250 bool            `json:"move_to_250"`
	Excluded  bool            `json:"excluded"`
	Attrs     map[string]any  `json:"attrs"`
	Note      string          `json:"note,omitempty"`
	Months    map[int]float64 `json:"months"`
}

// Total — итог строки: сумма заполненных месяцев.
func (r ExpRegRow) Total() float64 {
	var t float64
	for _, v := range r.Months {
		t += v
	}
	return t
}

// MonthAmount — сумма месяца строки.
func (r ExpRegRow) MonthAmount(month int) float64 { return r.Months[month] }

// ── §5.1 Реестр ФОТ ─────────────────────────────────────────────────────────

// ExpPayrollTotals — итоги реестра ФОТ.
//
// Налогов здесь нет: ответ 38 отменил расчёт «ФОТ × СтавкаСоцвзносов». Строки с
// нулями по всем месяцам — это резерв (ТЗ §5.1): они сохраняются и на итог не
// влияют, что и происходит само собой при суммировании.
type ExpPayrollTotals struct {
	ByMonth map[int]float64 `json:"by_month"`
	Year    float64         `json:"year"`
}

// ExpPayrollSum — итоги реестра ФОТ по месяцам и за год.
func ExpPayrollSum(rows []ExpRegRow) ExpPayrollTotals {
	out := ExpPayrollTotals{ByMonth: map[int]float64{}}
	for _, r := range rows {
		if r.Kind != ExpRegPayroll || r.Excluded {
			continue
		}
		for m := 1; m <= 12; m++ {
			v := r.MonthAmount(m)
			out.ByMonth[m] += v
			out.Year += v
		}
	}
	return out
}

// expPayrollPairs — пара статей «зарплата / налоги от выплат» по юридическому
// лицу (ответ 40). ЮЛ вне таблицы — строка не публикуется, факт пишется в журнал
// как ошибка сопоставления: молча отнести её на код 24 нельзя, это чужие деньги.
var expPayrollPairs = map[string][2]int{
	"МАРК Формэль ООО":    {24, 26},
	"ТД Марк Формэль":     {24, 26},
	"филиал Марк Формэль": {78, 86},
	"ФОРМЭЛЬ ООО":         {79, 87},
	"Филиал Формэль":      {80, 88},
	"ООО Гранд-Партнер":   {81, 89},
}

// ExpPayrollPair — пара статей по юридическому лицу: (зарплата, налоги).
// Второй результат false — ЮЛ в таблице ответа 40 нет.
func ExpPayrollPair(legalEntity string) ([2]int, bool) {
	p, ok := expPayrollPairs[legalEntity]
	return p, ok
}

// ── §5.2 Реестр командировок (формулы находки Н3) ───────────────────────────

// ExpTravelInput — вход расчёта строки командировки.
type ExpTravelInput struct {
	Country   string
	City      string
	Days      float64
	Nights    float64
	People    float64
	OneWay    bool    // перелёт в одну сторону; иначе туда и обратно
	Lodging   float64 // ставка проживания за ночь, USD — не нормирована, вводится
	FlightBYN float64 // норматив перелёта в одну сторону, бел. руб. (dir_flight_norm)
	PerDiem   float64 // суточные страны, USD (dir_per_diem)
	FxUSD     float64 // курс доллара к валюте ввода на месяц строки
}

// ExpTravelRow — расчётная часть строки командировки.
type ExpTravelRow struct {
	Tickets float64 `json:"tickets"`
	Lodging float64 `json:"lodging"`
	PerDiem float64 `json:"per_diem"`
	Total   float64 `json:"total"`
}

// ExpTravelCalc — расчёт строки командировки (находка Н3).
//
//	Билеты          = НормативПерелёта(страна, город) × (1 в одну сторону, иначе 2) × Человек
//	Проживание      = СтавкаПроживания × Ночей × Человек × КурсUSD
//	Командировочные = Суточные(страна) × Дней × Человек × КурсUSD
//	Итого строки    = Билеты + Проживание + Командировочные
//
// Норматив перелёта задан СРАЗУ в белорусских рублях, поэтому на курс он не
// умножается: в эталоне 300 бел. руб. в одну сторону по РФ — это уже BYN.
func ExpTravelCalc(in ExpTravelInput) ExpTravelRow {
	legs := 2.0
	if in.OneWay {
		legs = 1
	}
	out := ExpTravelRow{
		Tickets: in.FlightBYN * legs * in.People,
		Lodging: in.Lodging * in.Nights * in.People * in.FxUSD,
		PerDiem: in.PerDiem * in.Days * in.People * in.FxUSD,
	}
	out.Total = out.Tickets + out.Lodging + out.PerDiem
	return out
}

// ExpTravelCodePL — на какую статью идёт часть строки командировки (Н3):
// билеты и командировочные — 27, проживание — 30.
func ExpTravelCodePL(part string) int {
	if part == "lodging" {
		return ExpPLLodging
	}
	return ExpPLTravel
}

// ── §5.3 Реестр логистики по ЦФО 250 ────────────────────────────────────────

// ExpLogisticsShare — доля статьи в продажах месяца: Факт(статья) ÷ Факт(продажи).
// Нулевые продажи дают нулевую долю, а не деление на ноль: месяц без продаж —
// обычное дело для новой площадки, и форма из-за него открываться не перестаёт.
func ExpLogisticsShare(factItem, factSales float64) float64 {
	if factSales == 0 {
		return 0
	}
	return factItem / factSales
}

// ExpLogisticsPlan — план статьи месяца: Доля × План(продажи).
func ExpLogisticsPlan(share, planSales float64) float64 { return share * planSales }

// ExpLogisticsAvgShare — доля статьи в продажах за базовый год.
//
// Ответ 5.3 (11.09.2026): доля считается СРЕДНИМ помесячных долей, а не
// отношением годовых сумм. На эталоне 2024 года по статье 51 помесячные доли
// 0,10701 · 0,08733 · 0,12999 · 0,10037 · 0,07396 · 0,10924 · 0,09853 · 0,09178
// дают 0,0998; отношение годовых сумм дало бы другое число, и план поехал бы.
//
// Месяцы без продаж в среднее не входят: доля там не «ноль», а «не определена».
func ExpLogisticsAvgShare(factItem, factSales map[int]float64) float64 {
	var sum float64
	n := 0
	for m := 1; m <= 12; m++ {
		sales := factSales[m]
		if sales == 0 {
			continue
		}
		sum += factItem[m] / sales
		n++
	}
	if n == 0 {
		return 0
	}
	return sum / float64(n)
}

// ── §5.4 Реестр ИТ-расходов ─────────────────────────────────────────────────

// Блоки ИТ-реестра (ответ И-1 от 11.09.2026). В эталоне их три, и различаются
// они НЕ признаками строки, а положением в листе:
//
//	первый блок  — ЦФО 40 без пометок, свой подытог (в эталоне 18 000 в месяц);
//	блок переноса— ЦФО 250 с пометкой «ПЕРЕНОСИМ НА ЦФО 250» (15 000);
//	основной блок— ЦФО 40, одна строка помечена «НЕ БЕРЁМ В РАСЧЁТ» (Тотал 25 500).
//
// ОБЩИЕ РАСХОДЫ = Тотал основного блока + подытог ПЕРВОГО: 25 500 + 18 000 = 43 500.
// Блок переноса не входит ни в один из двух итогов и публикуется отдельной суммой
// под ЦФО 250.
//
// Поэтому блок — атрибут строки: по признакам его не вычислить, обе группы сидят
// на ЦФО 40 и обе без пометок.
const (
	ExpITBlockFirst = "first" // первый блок со своим подытогом
	ExpITBlockMain  = "main"  // основной блок, из него считается Тотал
)

// ExpITBlockOf — блок строки ИТ-реестра. По умолчанию основной: так ведёт себя
// строка, заведённая руками без явного выбора блока.
func ExpITBlockOf(r ExpRegRow) string {
	if b, _ := r.Attrs["block"].(string); b == ExpITBlockFirst {
		return ExpITBlockFirst
	}
	return ExpITBlockMain
}

// ExpITTotals — итоги реестра ИТ.
type ExpITTotals struct {
	// First — подытог первого блока (в эталоне 18 000 в месяц).
	First float64 `json:"first"`
	// Transfer — блок переноса на ЦФО 250. Не входит НИ В ОДИН итог: он
	// публикуется отдельной суммой под другим ЦФО.
	Transfer float64 `json:"transfer"`
	// Total — Тотал основного блока без строк «не учитывать».
	Total float64 `json:"total"`
	// Common — ОБЩИЕ РАСХОДЫ = Total + First.
	Common float64 `json:"common"`
	// Excluded — сумма строк «не берём в расчёт». В итоги не входит, но
	// показывается: это решение человека, и оно должно быть видно.
	Excluded float64 `json:"excluded"`
}

// ExpITSum — итоги реестра ИТ (ТЗ §5.4 в трактовке ответа И-1).
//
// Прежняя версия считала ОБЩИЕ = Тотал + перенос и давала 25 500 + 15 000 = 40 500
// вместо 43 500. На фикстурах это не всплывало: там блок был один.
func ExpITSum(rows []ExpRegRow) ExpITTotals {
	var out ExpITTotals
	for _, r := range rows {
		if r.Kind != ExpRegIT {
			continue
		}
		t := r.Total()
		switch {
		case r.Excluded:
			out.Excluded += t
		case r.MoveTo250:
			out.Transfer += t
		case ExpITBlockOf(r) == ExpITBlockFirst:
			out.First += t
		default:
			out.Total += t
		}
	}
	out.Common = out.Total + out.First
	return out
}

// ExpITPublishCFO — под каким ЦФО публикуется строка ИТ-реестра (ТЗ §5.4, §10).
// Второй результат false — строка не публикуется вовсе.
func ExpITPublishCFO(r ExpRegRow) (int, bool) {
	if r.Excluded {
		return 0, false
	}
	if r.MoveTo250 {
		return ExpCFOMarketplaceDept, true
	}
	return r.CodeCFO, true
}
