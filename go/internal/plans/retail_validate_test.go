package plans

import (
	"strings"
	"testing"
)

// Тесты валидаций формы «Розница» (ТЗ §6): блокирующие V-01..V-11 и
// предупреждающие W-01..W-06. Без БД.

// retailHasIssue — есть ли проверка с таким кодом (и, если задан, по такому магазину).
func retailHasIssue(list []ValidationIssue, code string, codeCFO int) bool {
	for _, i := range list {
		if i.Code == code && (codeCFO == 0 || i.CodeCFO == codeCFO) {
			return true
		}
	}
	return false
}

// retailCountIssue — сколько раз сработала проверка.
func retailCountIssue(list []ValidationIssue, code string) int {
	n := 0
	for _, i := range list {
		if i.Code == code {
			n++
		}
	}
	return n
}

// retailFilledRow — строка с заполненными планами на все месяцы (чтобы V-01 не шумела).
func retailFilledRow(code int, lfl string, months []int, amount float64) RetailRow {
	r := RetailRow{
		CodeCFO: code, NameCFO: "Магазин " + itoa(int64(code)), LFLStatus: lfl,
		LFLEffective: lfl, LegalEntity: "F", City: "Минск", StoreType: "ТЦ",
	}
	for _, m := range months {
		a := amount
		r.Values = append(r.Values, RetailValue{
			Metric: MetricSales, Year: 2026, Month: m, Amount: &a, Source: ValueManual,
		})
	}
	return r
}

// retailBaseInput — валидный вход: BY/BYN/F, июль 2026.
func retailBaseInput(rows []RetailRow, ser RetailSeries) RetailValidationInput {
	return RetailValidationInput{
		Country: "BY", LegalEntity: "F", NatCurrency: "BYN",
		Year: 2026, Month: 7, PlanMonths: []int{7, 8}, Rows: rows, Series: ser,
	}
}

// TestValidate_V03_ClosedStoreMustHaveZeroPlan — V-03: по магазину, закрытому до
// 1-го числа месяца, план должен быть пустым или 0.
func TestValidate_V03_ClosedStoreMustHaveZeroPlan(t *testing.T) {
	ser := NewRetailSeries()

	// Магазин закрыт 28.02.2026 — до начала июля: план 100 нарушает V-03.
	withPlan := retailFilledRow(100, LFLClosed, []int{7, 8}, 100)
	withPlan.DateClose = "2026-02-28"
	blocking, _ := ValidateRetailForm(retailBaseInput([]RetailRow{withPlan}, ser))
	if !retailHasIssue(blocking, "V-03", 100) {
		t.Error("V-03 должна срабатывать на непустом плане закрытого магазина")
	}
	// Такой магазин вообще не должен быть в форме — это V-09.
	if !retailHasIssue(blocking, "V-09", 100) {
		t.Error("V-09 должна срабатывать на магазине, закрытом до начала периода")
	}

	// Тот же магазин с планом 0 — V-03 не срабатывает (V-09 остаётся).
	withZero := retailFilledRow(200, LFLClosed, []int{7, 8}, 0)
	withZero.DateClose = "2026-02-28"
	blocking, _ = ValidateRetailForm(retailBaseInput([]RetailRow{withZero}, ser))
	if retailHasIssue(blocking, "V-03", 200) {
		t.Error("V-03 не должна срабатывать при нулевом плане закрытого магазина")
	}

	// Магазин, закрытый ПОСЛЕ начала периода (30.09.2026), — работает штатно.
	stillOpen := retailFilledRow(300, LFLYes, []int{7, 8}, 100)
	stillOpen.DateClose = "2026-09-30"
	blocking, _ = ValidateRetailForm(retailBaseInput([]RetailRow{stillOpen}, ser))
	if retailHasIssue(blocking, "V-03", 300) || retailHasIssue(blocking, "V-09", 300) {
		t.Error("магазин, закрываемый позже начала периода, не должен нарушать V-03/V-09")
	}
}

// TestValidate_V08_SingleLegalEntity — V-08: все строки на ОДНОМ ЮЛ страны.
func TestValidate_V08_SingleLegalEntity(t *testing.T) {
	ser := NewRetailSeries()

	// Разные ЮЛ в одной форме.
	a := retailFilledRow(100, LFLYes, []int{7, 8}, 10)
	b := retailFilledRow(200, LFLYes, []int{7, 8}, 10)
	b.LegalEntity = "TDMF"
	blocking, _ := ValidateRetailForm(retailBaseInput([]RetailRow{a, b}, ser))
	if !retailHasIssue(blocking, "V-08", 0) {
		t.Error("V-08 должна срабатывать на строках с разными ЮЛ")
	}

	// Одно ЮЛ, но НЕ то, что закреплено за страной (BY → F).
	wrong := retailFilledRow(100, LFLYes, []int{7, 8}, 10)
	wrong.LegalEntity = "MFKaz"
	blocking, _ = ValidateRetailForm(retailBaseInput([]RetailRow{wrong}, ser))
	if !retailHasIssue(blocking, "V-08", 0) {
		t.Error("V-08 должна срабатывать, если ЮЛ строк не совпадает с ЮЛ страны")
	}

	// Корректный случай: единственное ЮЛ страны.
	blocking, _ = ValidateRetailForm(retailBaseInput([]RetailRow{a}, ser))
	if retailHasIssue(blocking, "V-08", 0) {
		t.Error("V-08 не должна срабатывать на одном правильном ЮЛ")
	}
}

// TestValidate_V11_NationalCurrency — V-11: валюта формы = нац. валюта страны.
func TestValidate_V11_NationalCurrency(t *testing.T) {
	ser := NewRetailSeries()
	rows := []RetailRow{retailFilledRow(100, LFLYes, []int{7, 8}, 10)}

	in := retailBaseInput(rows, ser)
	in.NatCurrency = "RUB" // у BY должна быть BYN
	blocking, _ := ValidateRetailForm(in)
	if !retailHasIssue(blocking, "V-11", 0) {
		t.Error("V-11 должна срабатывать при валюте формы, отличной от национальной")
	}

	in.NatCurrency = "BYN"
	blocking, _ = ValidateRetailForm(in)
	if retailHasIssue(blocking, "V-11", 0) {
		t.Error("V-11 не должна срабатывать при национальной валюте страны")
	}

	// Каждая страна розницы — со своей валютой (ТЗ §1).
	for _, sc := range RetailCountries() {
		in := RetailValidationInput{
			Country: sc.Country, NatCurrency: sc.Currency, Year: 2026, Month: 7,
			PlanMonths: []int{7}, Series: ser,
		}
		if blocking, _ := ValidateRetailForm(in); retailHasIssue(blocking, "V-11", 0) {
			t.Errorf("V-11 ложно сработала для %s/%s", sc.Country, sc.Currency)
		}
	}
}

// TestValidate_W01_LFLThreshold — W-01: |LFL тактич.| > порога (дефолт 30 %);
// для «новый»/«до года»/«ххх» проверка НЕ применяется (ТЗ §6).
func TestValidate_W01_LFLThreshold(t *testing.T) {
	ser := NewRetailSeries()
	// Факт июля 2025 = 100 у всех магазинов; план 200 → LFL = +100 % > 30 %.
	ser.AddFact([]RetailFactCell{
		{CodeCFO: 100, Year: 2025, Month: 7, Amount: 100},
		{CodeCFO: 200, Year: 2025, Month: 7, Amount: 100},
		{CodeCFO: 300, Year: 2025, Month: 7, Amount: 100},
		{CodeCFO: 400, Year: 2025, Month: 7, Amount: 100},
	})

	lfl := retailFilledRow(100, LFLYes, []int{7, 8}, 200)
	lfl.Comment = "рост из-за открытия ТЦ" // V-07: комментарий есть
	_, warnings := ValidateRetailForm(retailBaseInput([]RetailRow{lfl}, ser))
	if !retailHasIssue(warnings, "W-01", 100) {
		t.Error("W-01 должна срабатывать при LFL выше порога у магазина LFL")
	}

	// «новый», «до года», «ххх» — W-01 не применяется (ТЗ §6).
	for _, st := range []string{LFLNew, LFLUnder1, LFLNoID} {
		row := retailFilledRow(200, st, []int{7, 8}, 200)
		_, warnings := ValidateRetailForm(retailBaseInput([]RetailRow{row}, ser))
		if retailHasIssue(warnings, "W-01", 200) {
			t.Errorf("W-01 не должна применяться к статусу %q", st)
		}
	}

	// Порог настраивается по LFL-статусу (ТЗ §6): поднимем до 200 % — не сработает.
	in := retailBaseInput([]RetailRow{retailFilledRow(300, LFLYes, []int{7, 8}, 200)}, ser)
	in.Params = RetailParamSet{Params: []RetailParam{
		{ParamCode: ParamLFLWarn, ScopeKind: ParamScopeLFL, ScopeValue: LFLYes, Value: 2.0},
	}}
	if _, warnings := ValidateRetailForm(in); retailHasIssue(warnings, "W-01", 300) {
		t.Error("при пороге 200 % LFL +100 % не должен давать W-01")
	}

	// V-07: сработавшее предупреждение без комментария блокирует форму.
	noComment := retailFilledRow(400, LFLYes, []int{7, 8}, 200)
	blocking, warnings := ValidateRetailForm(retailBaseInput([]RetailRow{noComment}, ser))
	if len(warnings) == 0 {
		t.Fatal("ожидалось хотя бы одно предупреждение")
	}
	if !retailHasIssue(blocking, "V-07", 400) {
		t.Error("V-07 должна требовать комментарий при сработавшем предупреждении")
	}
}

// TestValidate_V01_EmptyIsNotZero — V-01: «пусто ≠ 0», для нуля нужен явный ввод.
func TestValidate_V01_EmptyIsNotZero(t *testing.T) {
	ser := NewRetailSeries()

	// Заполнен только июль, август пуст → V-01 по августу.
	partial := retailFilledRow(100, LFLYes, []int{7}, 10)
	blocking, _ := ValidateRetailForm(retailBaseInput([]RetailRow{partial}, ser))
	if retailCountIssue(blocking, "V-01") != 1 {
		t.Errorf("V-01: ожидалось 1 срабатывание (август), got %d", retailCountIssue(blocking, "V-01"))
	}

	// Явный ноль V-01 не нарушает.
	zero := retailFilledRow(100, LFLYes, []int{7, 8}, 0)
	blocking, _ = ValidateRetailForm(retailBaseInput([]RetailRow{zero}, ser))
	if retailHasIssue(blocking, "V-01", 100) {
		t.Error("явно введённый ноль не должен нарушать V-01")
	}
}

// TestValidate_V02_RangeAndV04_Duplicates — V-02 (диапазон) и V-04 (ключ строки).
func TestValidate_V02_RangeAndV04_Duplicates(t *testing.T) {
	ser := NewRetailSeries()

	// V-02: значение выше страновой границы.
	in := retailBaseInput([]RetailRow{retailFilledRow(100, LFLYes, []int{7, 8}, 1_000_000)}, ser)
	in.ValueLimit = 500_000
	blocking, _ := ValidateRetailForm(in)
	if !retailHasIssue(blocking, "V-02", 100) {
		t.Error("V-02 должна срабатывать при превышении страновой границы")
	}

	// V-04: дублирующийся CodeCFO и дублирующийся непустой KLIENT_ID.
	a := retailFilledRow(100, LFLYes, []int{7, 8}, 10)
	a.KlientID = "1641"
	b := retailFilledRow(100, LFLYes, []int{7, 8}, 10)
	b.KlientID = "1641"
	blocking, _ = ValidateRetailForm(retailBaseInput([]RetailRow{a, b}, ser))
	if retailCountIssue(blocking, "V-04") < 2 {
		t.Errorf("V-04: ожидались дубли CodeCFO и KLIENT_ID, got %d срабатываний", retailCountIssue(blocking, "V-04"))
	}

	// Пустой KLIENT_ID («ххх») дублем не считается (ТЗ §2).
	c := retailFilledRow(100, LFLNoID, []int{7, 8}, 10)
	d := retailFilledRow(200, LFLNoID, []int{7, 8}, 10)
	blocking, _ = ValidateRetailForm(retailBaseInput([]RetailRow{c, d}, ser))
	for _, i := range blocking {
		if i.Code == "V-04" && strings.Contains(i.Message, "KLIENT_ID") {
			t.Error("пустой KLIENT_ID не должен считаться дублем")
		}
	}

	// V-04: магазина нет в справочнике активных.
	in = retailBaseInput([]RetailRow{retailFilledRow(999, LFLYes, []int{7, 8}, 10)}, ser)
	in.DirectoryCodes = map[int]bool{100: true}
	blocking, _ = ValidateRetailForm(in)
	if !retailHasIssue(blocking, "V-04", 999) {
		t.Error("V-04 должна срабатывать на магазине вне справочника")
	}
}

// TestValidate_V10_OutsidePlanPeriod — V-10: значений вне планового периода нет.
func TestValidate_V10_OutsidePlanPeriod(t *testing.T) {
	ser := NewRetailSeries()
	row := retailFilledRow(100, LFLYes, []int{7, 8}, 10)
	stray := 5.0
	// Июнь не входит в плановый период (июль–август).
	row.Values = append(row.Values, RetailValue{
		Metric: MetricSales, Year: 2026, Month: 6, Amount: &stray, Source: ValueImport,
	})
	blocking, _ := ValidateRetailForm(retailBaseInput([]RetailRow{row}, ser))
	if !retailHasIssue(blocking, "V-10", 100) {
		t.Error("V-10 должна срабатывать на значении вне планового периода")
	}

	// Ячейка ФОТ вне списка плановых месяцев V-10 не нарушает: это производная
	// статья, а V-10 — про план продаж.
	row2 := retailFilledRow(200, LFLYes, []int{7, 8}, 10)
	fot := 1.0
	row2.Values = append(row2.Values, RetailValue{
		Metric: MetricPayroll, Year: 2026, Month: 6, Amount: &fot, Source: ValuePayrollShare,
	})
	blocking, _ = ValidateRetailForm(retailBaseInput([]RetailRow{row2}, ser))
	if retailHasIssue(blocking, "V-10", 200) {
		t.Error("V-10 не должна срабатывать на статье ФОТ")
	}
}

// TestValidate_Warnings_W03_W04_W05_W06 — остальные предупреждения ТЗ §6.
func TestValidate_Warnings_W03_W04_W05_W06(t *testing.T) {
	ser := NewRetailSeries()
	ser.AddFact([]RetailFactCell{{CodeCFO: 100, Year: 2025, Month: 7, Amount: 500}})
	ser.AddStrategy([]RetailFactCell{{CodeCFO: 200, Year: 2026, Month: 7, Amount: 100}})

	// W-03: план = 0 при непустом факте того же месяца прошлого года.
	zero := retailFilledRow(100, LFLYes, []int{7, 8}, 0)
	zero.Comment = "магазин на реконструкции"
	_, warnings := ValidateRetailForm(retailBaseInput([]RetailRow{zero}, ser))
	if !retailHasIssue(warnings, "W-03", 100) {
		t.Error("W-03 должна срабатывать на нулевом плане при непустом факте ПГ")
	}

	// W-04: отклонение от стратегии больше порога (100 → 200 = +100 %).
	offStrategy := retailFilledRow(200, LFLYes, []int{7, 8}, 200)
	offStrategy.Comment = "перевыполнение согласовано"
	_, warnings = ValidateRetailForm(retailBaseInput([]RetailRow{offStrategy}, ser))
	if !retailHasIssue(warnings, "W-04", 200) {
		t.Error("W-04 должна срабатывать при отклонении от стратегии выше порога")
	}

	// W-05: план на месяцы до даты открытия.
	notOpen := retailFilledRow(300, LFLNew, []int{7, 8}, 100)
	notOpen.DateOpen = "2026-10-01"
	notOpen.Comment = "открытие переносится"
	_, warnings = ValidateRetailForm(retailBaseInput([]RetailRow{notOpen}, ser))
	if !retailHasIssue(warnings, "W-05", 300) {
		t.Error("W-05 должна срабатывать на плане до даты открытия")
	}

	// W-06: активный магазин без факта за прошедшие месяцы года.
	noFact := retailFilledRow(400, LFLYes, []int{7, 8}, 100)
	noFact.Comment = "новый в справочнике"
	_, warnings = ValidateRetailForm(retailBaseInput([]RetailRow{noFact}, ser))
	if !retailHasIssue(warnings, "W-06", 400) {
		t.Error("W-06 должна срабатывать на активном магазине без факта за прошедшие месяцы")
	}
}

// TestRetailLFLStatus — приведение LfLStatus источника к каноническим кодам.
func TestRetailLFLStatus(t *testing.T) {
	cases := map[string]string{
		"LFL":     LFLYes,
		"лфл":     LFLYes,
		"до года": LFLUnder1,
		"Новый":   LFLNew,
		"ххх":     LFLNoID,
		"xxx":     LFLNoID,
		"закрыт":  LFLClosed,
		"Closed":  LFLClosed,
		"":        "",
	}
	for raw, want := range cases {
		if got := retailLFLStatus(raw); got != want {
			t.Errorf("retailLFLStatus(%q): got %q, want %q", raw, got, want)
		}
	}
}

// TestRetailRegManagerExcluded — служебные значения РМ закрытых точек (ТЗ §9).
func TestRetailRegManagerExcluded(t *testing.T) {
	for _, rm := range []string{"", "Closed", "closed", "n/a", "N/A", "-"} {
		if !retailRegManagerExcluded(rm) {
			t.Errorf("значение РМ %q должно исключаться (ТЗ §9)", rm)
		}
	}
	if retailRegManagerExcluded("Смолер О.В.") {
		t.Error("реальный РМ исключаться не должен")
	}
}

// TestRetailPlanMonths — плановый период: от месяца карточки до декабря.
func TestRetailPlanMonths(t *testing.T) {
	if got := retailPlanMonths(7); len(got) != 6 || got[0] != 7 || got[5] != 12 {
		t.Errorf("плановые месяцы для июля: got %v", got)
	}
	if got := retailPlanMonths(12); len(got) != 1 || got[0] != 12 {
		t.Errorf("плановые месяцы для декабря: got %v", got)
	}
	if got := retailPlanMonths(0); len(got) != 12 {
		t.Errorf("некорректный месяц должен давать полный год, got %v", got)
	}
}
