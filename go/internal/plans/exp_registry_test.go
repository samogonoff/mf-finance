package plans

import (
	"context"
	"math"
	"testing"
)

func near(a, b float64) bool { return math.Abs(a-b) < 0.005 }

func regRow(kind string, months map[int]float64) ExpRegRow {
	return ExpRegRow{Kind: kind, Months: months}
}

func twelve(v float64) map[int]float64 {
	m := map[int]float64{}
	for i := 1; i <= 12; i++ {
		m[i] = v
	}
	return m
}

// ТЗ §5.1: «3 500 × 12 = 42 000 по строке; итог реестра 55 169,60 в месяц и
// 662 035,20 за год». Проверяем и построчный итог, и годовой итог реестра.
func TestExpPayrollSum_SpecCheckNumbers(t *testing.T) {
	row := regRow(ExpRegPayroll, twelve(3500))
	if got := row.Total(); !near(got, 42000) {
		t.Fatalf("итог сотрудника %v, ТЗ §5.1 даёт 42 000", got)
	}

	// Реестр из эталона: итог месяца 55 169,60. Собираем его из строки-остатка,
	// чтобы проверить именно суммирование, а не конкретный состав сотрудников.
	rows := []ExpRegRow{
		regRow(ExpRegPayroll, twelve(3500)),
		regRow(ExpRegPayroll, twelve(51669.60)),
		// Резерв: нули по всем месяцам сохраняются и на итог не влияют (ТЗ §5.1).
		regRow(ExpRegPayroll, twelve(0)),
	}
	tot := ExpPayrollSum(rows)
	if !near(tot.ByMonth[1], 55169.60) {
		t.Fatalf("итог реестра за месяц %v, ТЗ §5.1 даёт 55 169,60", tot.ByMonth[1])
	}
	if !near(tot.Year, 662035.20) {
		t.Fatalf("итог реестра за год %v, ТЗ §5.1 даёт 662 035,20", tot.Year)
	}
}

// Ответ 38: ставки соцвзносов нет, налоги от выплат — самостоятельные статьи.
// Реестр обязан отдавать «ФОТ чистый» без всяких надбавок.
func TestExpPayrollSum_NoSocialTax(t *testing.T) {
	tot := ExpPayrollSum([]ExpRegRow{regRow(ExpRegPayroll, twelve(1000))})
	if !near(tot.ByMonth[3], 1000) {
		t.Fatalf("месяц реестра %v — к ФОТ ничего не добавляется (ответ 38)", tot.ByMonth[3])
	}
}

// Ответ 40: пара статей выбирается по юридическому лицу; ЮЛ вне таблицы —
// строку не публиковать, а не относить молча на код 24.
func TestExpPayrollPair(t *testing.T) {
	cases := map[string][2]int{
		"ТД Марк Формэль":     {24, 26},
		"филиал Марк Формэль": {78, 86},
		"ФОРМЭЛЬ ООО":         {79, 87},
		"Филиал Формэль":      {80, 88},
		"ООО Гранд-Партнер":   {81, 89},
	}
	for le, want := range cases {
		got, ok := ExpPayrollPair(le)
		if !ok || got != want {
			t.Fatalf("%s → %v (%v), ожидали %v", le, got, ok, want)
		}
	}
	if _, ok := ExpPayrollPair("ООО Ромашка"); ok {
		t.Fatal("неизвестное ЮЛ не должно получать пару статей")
	}
}

// Находка Н3, контрольная строка эталона: билеты 600 + проживание 1 050 +
// командировочные 280 = 1 930; две одинаковые строки дают 3 860.
// Условия: РФ, 2 дня, 2 ночи, 1 человек, туда-обратно, курс 3,5,
// норматив перелёта 300 бел. руб., проживание 150 USD, суточные 40 USD.
func TestExpTravelCalc_SpecCheckRow(t *testing.T) {
	in := ExpTravelInput{
		Country: "RU", Days: 2, Nights: 2, People: 1, OneWay: false,
		Lodging: 150, FlightBYN: 300, PerDiem: 40, FxUSD: 3.5,
	}
	got := ExpTravelCalc(in)
	if !near(got.Tickets, 600) {
		t.Fatalf("билеты %v, эталон 600 (300 × 2 плеча × 1 чел)", got.Tickets)
	}
	if !near(got.Lodging, 1050) {
		t.Fatalf("проживание %v, эталон 1 050 (150 × 2 ночи × 1 чел × 3,5)", got.Lodging)
	}
	if !near(got.PerDiem, 280) {
		t.Fatalf("командировочные %v, эталон 280 (40 × 2 дня × 1 чел × 3,5)", got.PerDiem)
	}
	if !near(got.Total, 1930) {
		t.Fatalf("итог строки %v, эталон 1 930", got.Total)
	}
	if !near(got.Total*2, 3860) {
		t.Fatalf("две строки дают %v, эталон 3 860", got.Total*2)
	}

	// Перелёт в одну сторону — одно плечо, а не два.
	in.OneWay = true
	if one := ExpTravelCalc(in); !near(one.Tickets, 300) {
		t.Fatalf("в одну сторону билеты %v, ожидали 300", one.Tickets)
	}
}

// Проживание публикуется ОТДЕЛЬНОЙ статьёй 30, билеты и командировочные — 27.
func TestExpTravelCodePL(t *testing.T) {
	if got := ExpTravelCodePL("lodging"); got != 30 {
		t.Fatalf("проживание → статья %d, находка Н3 требует 30", got)
	}
	for _, part := range []string{"tickets", "per_diem", ""} {
		if got := ExpTravelCodePL(part); got != 27 {
			t.Fatalf("%q → статья %d, ожидали 27", part, got)
		}
	}
}

// ТЗ §5.3: 32 771,32 ÷ 306 242,86 = 0,11; обратная проверка 2025 года —
// 55 546,65 ÷ 556 710,71 = 0,0998.
func TestExpLogistics_SpecChecks(t *testing.T) {
	share := ExpLogisticsShare(32771.32, 306242.86)
	if math.Abs(share-0.11) > 0.005 {
		t.Fatalf("доля %v, ТЗ §5.3 даёт ≈0,11", share)
	}
	back := ExpLogisticsShare(55546.65, 556710.71)
	if math.Abs(back-0.0998) > 0.0005 {
		t.Fatalf("обратная проверка %v, ТЗ §5.3 даёт 0,0998", back)
	}
	// План = Доля × План продаж.
	if got := ExpLogisticsPlan(0.1, 500000); !near(got, 50000) {
		t.Fatalf("план статьи %v, ожидали 50 000", got)
	}
	// Месяц без продаж не делит на ноль.
	if got := ExpLogisticsShare(1000, 0); got != 0 {
		t.Fatalf("доля при нулевых продажах %v, ожидали 0", got)
	}
}

// Ответ И-1 (11.09.2026): в эталоне ТРИ блока, и 18 000 — это первый блок по
// ЦФО 40, а не блок переноса. ОБЩИЕ = Тотал основного блока + подытог первого:
// 25 500 + 18 000 = 43 500 в месяц, 320 500 + 216 000 = 536 500 за год.
// Прежняя формула «Тотал + перенос» давала 40 500 — расхождение в 3 000 в месяц.
func TestExpITSum_SpecCheckNumbers(t *testing.T) {
	first := ExpRegRow{Kind: ExpRegIT, CodeCFO: 40, Months: twelve(18000),
		Attrs: map[string]any{"block": ExpITBlockFirst}}
	transfer := ExpRegRow{Kind: ExpRegIT, CodeCFO: 250, MoveTo250: true, Months: twelve(15000)}
	main := ExpRegRow{Kind: ExpRegIT, CodeCFO: 40, Months: twelve(25500)}
	excluded := ExpRegRow{Kind: ExpRegIT, CodeCFO: 40, CodePL: 85, Excluded: true, Months: twelve(12000)}

	got := ExpITSum([]ExpRegRow{first, transfer, main, excluded})
	if !near(got.First, 18000*12) {
		t.Fatalf("подытог первого блока %v, эталон 216 000 за год", got.First)
	}
	if !near(got.Total, 25500*12) {
		t.Fatalf("Тотал %v, эталон 320 500 за год", got.Total)
	}
	if !near(got.Common, 43500*12) {
		t.Fatalf("ОБЩИЕ РАСХОДЫ %v, эталон 536 500 = Тотал + первый блок", got.Common)
	}
	if !near(got.Transfer, 15000*12) {
		t.Fatalf("блок переноса %v, эталон 15 000 в месяц", got.Transfer)
	}
	// Перенос не входит ни в Тотал, ни в Общие: он публикуется отдельной суммой
	// под ЦФО 250.
	if near(got.Common, (25500+15000)*12) {
		t.Fatal("блок переноса не должен складываться в ОБЩИЕ РАСХОДЫ")
	}
	if !near(got.Excluded, 12000*12) {
		t.Fatalf("исключённые %v — они обязаны быть видны отдельно", got.Excluded)
	}
	// Март эталона проверяет именно исключение: строка «не берём в расчёт»
	// стоит 12 000, а Тотал остаётся 25 500.
	if near(got.Total, (25500+12000)*12) {
		t.Fatal("строка «не учитывать» не должна входить в Тотал")
	}
}

// Строка, заведённая руками без явного блока, попадает в основной.
func TestExpITBlockOf_DefaultsToMain(t *testing.T) {
	if got := ExpITBlockOf(ExpRegRow{Kind: ExpRegIT}); got != ExpITBlockMain {
		t.Fatalf("блок по умолчанию %q, ожидали основной", got)
	}
	if got := ExpITBlockOf(ExpRegRow{Kind: ExpRegIT, Attrs: map[string]any{"block": "first"}}); got != ExpITBlockFirst {
		t.Fatalf("явный первый блок не распознан: %q", got)
	}
}

// ТЗ §5.4 и §10: признак переноса меняет ЦФО публикации, признак исключения
// снимает строку с публикации совсем.
func TestExpITPublishCFO(t *testing.T) {
	if cfo, ok := ExpITPublishCFO(ExpRegRow{Kind: ExpRegIT, CodeCFO: 40}); !ok || cfo != 40 {
		t.Fatalf("обычная строка → ЦФО %d (%v), ожидали свой 40", cfo, ok)
	}
	if cfo, ok := ExpITPublishCFO(ExpRegRow{Kind: ExpRegIT, CodeCFO: 40, MoveTo250: true}); !ok || cfo != 250 {
		t.Fatalf("строка с переносом → ЦФО %d (%v), ожидали 250", cfo, ok)
	}
	if _, ok := ExpITPublishCFO(ExpRegRow{Kind: ExpRegIT, CodeCFO: 40, Excluded: true}); ok {
		t.Fatal("строка «не учитывать» не публикуется")
	}
}

// E-19 (ТЗ §9): ФИО в реестре ФОТ выбирается из справочника, а не вводится
// текстом. Проверка идёт по идентификатору сотрудника: непустая строка в поле
// ФИО — это ровно то, что валидация запрещает.
func TestValidateExpRegRow_E19_EmployeeFromDirectory(t *testing.T) {
	textOnly := ExpRegRow{Kind: ExpRegPayroll, Attrs: map[string]any{
		"employee_name": "Бартош Анна Дмитриевна",
	}}
	if err := validateExpRegRow(textOnly); err == nil {
		t.Fatal("ФИО текстом должно отклоняться (E-19)")
	}
	picked := ExpRegRow{Kind: ExpRegPayroll, Attrs: map[string]any{
		"employee_id":   float64(451), // как приходит из JSON
		"employee_name": "Бартош Анна Дмитриевна",
		"legal_entity":  "ТД Марк Формэль",
	}}
	if err := validateExpRegRow(picked); err != nil {
		t.Fatalf("сотрудник из справочника должен приниматься: %v", err)
	}
}

// Ответ 40: ЮЛ вне таблицы пар — строку публиковать нельзя, и узнать об этом
// нужно при вводе, а не при публикации.
func TestValidateExpRegRow_UnknownLegalEntityRejected(t *testing.T) {
	r := ExpRegRow{Kind: ExpRegPayroll, Attrs: map[string]any{
		"employee_id": float64(1), "legal_entity": "ООО Ромашка",
	}}
	if err := validateExpRegRow(r); err == nil {
		t.Fatal("ЮЛ без пары статей должно отклоняться")
	}
}

// E-20 (ТЗ §9): у строки ИТ не могут стоять оба признака сразу.
func TestValidateExpRegRow_E20_FlagsAreExclusive(t *testing.T) {
	both := ExpRegRow{Kind: ExpRegIT, CodeCFO: 40, MoveTo250: true, Excluded: true}
	if err := validateExpRegRow(both); err == nil {
		t.Fatal("оба признака одновременно должны отклоняться (E-20)")
	}
	for _, r := range []ExpRegRow{
		{Kind: ExpRegIT, CodeCFO: 40, MoveTo250: true},
		{Kind: ExpRegIT, CodeCFO: 40, Excluded: true},
		{Kind: ExpRegIT, CodeCFO: 40},
	} {
		if err := validateExpRegRow(r); err != nil {
			t.Fatalf("строка с одним признаком должна приниматься: %+v → %v", r, err)
		}
	}
}

// Месяц вне 1..12 — мусор, который потом не найдёт ни один отчёт.
func TestValidateExpRegRow_MonthRange(t *testing.T) {
	r := ExpRegRow{Kind: ExpRegTravel, Months: map[int]float64{13: 100}}
	if err := validateExpRegRow(r); err == nil {
		t.Fatal("месяц 13 должен отклоняться")
	}
}

// stubNorms — справочники командировок с фиксированными значениями эталона.
type stubNorms struct{ noFlight bool }

func (s stubNorms) PerDiem(context.Context, string, int) (float64, bool) { return 40, true }
func (s stubNorms) FlightNorm(context.Context, string, string) (float64, bool) {
	if s.noFlight {
		return 0, false
	}
	return 300, true
}
func (s stubNorms) FxUSD(context.Context, string, int, int) (float64, bool) { return 3.5, true }

// Строка командировки — ОДНА поездка со своим месяцем (ТЗ §5.2), а не двенадцать
// месяцев: итог обязан лечь ровно в месяц поездки, иначе итог реестра по месяцам
// разъедется с формой.
func TestComputeTravelRow_PutsTotalIntoTripMonth(t *testing.T) {
	svc := &ExpService{norms: stubNorms{}}
	in := ExpInstance{PlanYear: 2026, Currency: "BYN"}
	row := ExpRegRow{Kind: ExpRegTravel, Attrs: map[string]any{
		"month": float64(2), "country": "RU", "city": "Москва",
		"days": float64(2), "nights": float64(2), "people": float64(1),
		"lodging_rate": float64(150),
	}}
	svc.computeTravelRow(context.Background(), in, &row)

	if !near(row.Attrs["tickets"].(float64), 600) {
		t.Fatalf("билеты %v, эталон 600", row.Attrs["tickets"])
	}
	if !near(row.Attrs["lodging"].(float64), 1050) {
		t.Fatalf("проживание %v, эталон 1 050", row.Attrs["lodging"])
	}
	if !near(row.Attrs["per_diem"].(float64), 280) {
		t.Fatalf("командировочные %v, эталон 280", row.Attrs["per_diem"])
	}
	if len(row.Months) != 1 {
		t.Fatalf("месяцев в строке %d, поездка занимает ровно один: %+v", len(row.Months), row.Months)
	}
	if !near(row.Months[2], 1930) {
		t.Fatalf("итог поездки в феврале %v, эталон 1 930", row.Months[2])
	}
	if row.CodePL != ExpPLTravel {
		t.Fatalf("статья строки %d, командировки идут на 27", row.CodePL)
	}
}

// Норматива перелёта в справочнике нет — строка сохраняется, но билеты нулевые,
// и это видно прямо в строке, а не всплывает потом на своде.
func TestComputeTravelRow_MissingNormLeavesZero(t *testing.T) {
	svc := &ExpService{norms: stubNorms{noFlight: true}}
	row := ExpRegRow{Kind: ExpRegTravel, Attrs: map[string]any{
		"month": float64(5), "country": "UZ", "city": "Фергана",
		"days": float64(1), "people": float64(1),
	}}
	svc.computeTravelRow(context.Background(), ExpInstance{PlanYear: 2026, Currency: "BYN"}, &row)
	if row.Attrs["tickets"].(float64) != 0 {
		t.Fatalf("без норматива билеты обязаны быть нулевыми: %v", row.Attrs["tickets"])
	}
	if !near(row.Months[5], 140) { // суточные 40 × 1 день × 1 чел × 3,5
		t.Fatalf("остальные части считаются: %v", row.Months[5])
	}
}

// Месяц вне 1..12 не должен ронять строку в несуществующую колонку.
func TestComputeTravelRow_MonthFallback(t *testing.T) {
	svc := &ExpService{norms: stubNorms{}}
	row := ExpRegRow{Kind: ExpRegTravel, Attrs: map[string]any{
		"month": float64(0), "country": "RU", "days": float64(1), "people": float64(1),
	}}
	svc.computeTravelRow(context.Background(), ExpInstance{PlanYear: 2026, Currency: "BYN"}, &row)
	if _, ok := row.Months[1]; !ok {
		t.Fatalf("без месяца поездка должна попасть в январь: %+v", row.Months)
	}
}

// Ответ 5.3 (11.09.2026): доля логистики — СРЕДНЕЕ помесячных долей базового
// года, а не отношение годовых сумм. На эталоне 2024 по статье 51 помесячные
// доли дают 0,0998; отношение годовых сумм дало бы другое число.
func TestExpLogisticsAvgShare_MeanOfMonthlyShares(t *testing.T) {
	item := map[int]float64{}
	sales := map[int]float64{}
	monthly := []float64{0.10701, 0.08733, 0.12999, 0.10037, 0.07396, 0.10924, 0.09853, 0.09178}
	for i, d := range monthly {
		// Продажи разные по месяцам — иначе среднее долей совпало бы с отношением
		// сумм, и тест ничего бы не проверял.
		s := 100000 * float64(i+1)
		sales[i+1] = s
		item[i+1] = d * s
	}
	got := ExpLogisticsAvgShare(item, sales)
	if math.Abs(got-0.0998) > 0.0005 {
		t.Fatalf("средняя доля %v, эталон 0,0998", got)
	}

	// Отношение годовых сумм даёт ДРУГОЕ число — на этом и строится ответ 5.3.
	var si, ss float64
	for m := 1; m <= 12; m++ {
		si += item[m]
		ss += sales[m]
	}
	if math.Abs(si/ss-got) < 0.0005 {
		t.Fatal("тест не различает среднее долей и отношение сумм — фикстура вырождена")
	}

	// Месяц без продаж в среднее не входит: доля там не «ноль», а «не определена».
	sales[3] = 0
	item[3] = 0
	if withZero := ExpLogisticsAvgShare(item, sales); math.Abs(withZero-got) < 1e-9 {
		t.Fatal("месяц без продаж обязан выпадать из среднего, а не считаться нулём")
	}
	if ExpLogisticsAvgShare(map[int]float64{}, map[int]float64{}) != 0 {
		t.Fatal("без данных доля равна нулю, а не NaN")
	}
}
