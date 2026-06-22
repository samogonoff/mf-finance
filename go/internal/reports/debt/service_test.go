package debt

import (
	"database/sql"
	"testing"
	"time"
)

// nstr — short helper для sql.NullString.
func nstr(s string) sql.NullString {
	if s == "" {
		return sql.NullString{}
	}
	return sql.NullString{String: s, Valid: true}
}

// nint/ntime — в drilldown_test.go (тот же пакет).

func day(y int, m time.Month, d int) time.Time {
	return time.Date(y, m, d, 0, 0, 0, 0, time.UTC)
}

func TestBuildReport_RF_62_IsDZ(t *testing.T) {
	// РФ-юрлицо (6950135110 = ТД Марк Формэль), счёт 62 = ДЗ.
	in := []rawRow{
		{
			CompanyID:      "6950135110",
			CounterpartyID: nstr("7826156685"),
			AccountRoot:    "62",
			OpeningSigned:  10000,
			TurnoverSigned: 5000,
			ClosingSigned:  15000,
		},
	}
	got := BuildReport(in)
	if len(got) != 1 {
		t.Fatalf("BuildReport len = %d, want 1", len(got))
	}
	r := got[0]
	if r.Country != CountryRF {
		t.Errorf("Country = %q, want %q", r.Country, CountryRF)
	}
	if r.OpeningDZ != 10000 || r.TurnoverDZ != 5000 || r.ClosingDZ != 15000 {
		t.Errorf("DZ amounts wrong: %+v", r)
	}
	if r.OpeningKZ != 0 || r.TurnoverKZ != 0 || r.ClosingKZ != 0 {
		t.Errorf("KZ должны быть нулевые для счёта ДЗ: %+v", r)
	}
}

func TestBuildReport_RF_60_IsKZ_SignFlipped(t *testing.T) {
	// Счёт 60 = пассивный (мы должны поставщику). В Premaster проводка Cr 60.x
	// → signed-сальдо отрицательное. BuildReport должен **перевернуть знак**,
	// чтобы наш долг показывался как положительная KZ-сумма.
	in := []rawRow{
		{
			CompanyID:      "6950135110",
			CounterpartyID: nstr("5032283181"),
			AccountRoot:    "60",
			OpeningSigned:  -8000,
			TurnoverSigned: -2000,
			ClosingSigned:  -10000,
		},
	}
	got := BuildReport(in)
	r := got[0]
	if r.OpeningKZ != 8000 || r.TurnoverKZ != 2000 || r.ClosingKZ != 10000 {
		t.Errorf("KZ должны быть положительные (sign flip): %+v", r)
	}
	if r.OpeningDZ != 0 || r.TurnoverDZ != 0 || r.ClosingDZ != 0 {
		t.Errorf("DZ должны быть нулевые для счёта КЗ: %+v", r)
	}
}

func TestBuildReport_RF_90_IsRevenue_SignFlipped(t *testing.T) {
	// Cr 90.x = выручка. signed = -amt → положительная выручка после flip.
	in := []rawRow{
		{
			CompanyID:      "6950135110",
			CounterpartyID: nstr("7705935687"),
			AccountRoot:    "90",
			OpeningSigned:  0,
			TurnoverSigned: -100000,
			ClosingSigned:  -100000,
		},
	}
	got := BuildReport(in)
	r := got[0]
	if r.RevenuePeriod != 100000 {
		t.Errorf("RevenuePeriod = %v, want 100000 (sign flip)", r.RevenuePeriod)
	}
	// для Revenue не заполняем DZ/KZ — это P&L, не баланс
	if r.OpeningDZ != 0 || r.OpeningKZ != 0 || r.ClosingDZ != 0 || r.ClosingKZ != 0 {
		t.Errorf("Revenue не должна заполнять DZ/KZ: %+v", r)
	}
}

func TestBuildReport_DropsRowsWithoutCounterparty(t *testing.T) {
	// Курсовые разницы, переоценки, начисление налогов — CounterpartyID NULL.
	// В debt-отчёте они не нужны.
	in := []rawRow{
		{
			CompanyID:      "6950135110",
			CounterpartyID: nstr(""), // NULL/пусто
			AccountRoot:    "91",
			ClosingSigned:  500,
		},
		{
			CompanyID:      "6950135110",
			CounterpartyID: sql.NullString{}, // явный NULL
			AccountRoot:    "60",
			ClosingSigned:  -100,
		},
	}
	if got := BuildReport(in); len(got) != 0 {
		t.Errorf("Строки без CounterpartyID должны отбрасываться, got %d: %+v", len(got), got)
	}
}

func TestBuildReport_DropsUnknownAccounts(t *testing.T) {
	// Счёт 99 (прибыли/убытки) не в chart для РФ → KindOther → отбросить.
	in := []rawRow{
		{
			CompanyID:      "6950135110",
			CounterpartyID: nstr("7826156685"),
			AccountRoot:    "99",
			ClosingSigned:  1000,
		},
	}
	if got := BuildReport(in); len(got) != 0 {
		t.Errorf("Неизвестные счета должны отбрасываться, got %d: %+v", len(got), got)
	}
}

func TestBuildReport_DropsUnknownCompany(t *testing.T) {
	// CompanyID не в Entities() — отбрасываем (не знаем страну → не знаем chart).
	in := []rawRow{
		{
			CompanyID:      "9999999999",
			CounterpartyID: nstr("7826156685"),
			AccountRoot:    "62",
			ClosingSigned:  100,
		},
	}
	if got := BuildReport(in); len(got) != 0 {
		t.Errorf("Неизвестные юрлица должны отбрасываться, got %d: %+v", len(got), got)
	}
}

func TestBuildReport_PartnerFallbackToINN(t *testing.T) {
	// В M1 partner-имена не загружены — должно быть PartnerINN в поле Partner.
	in := []rawRow{
		{
			CompanyID:      "6950135110",
			CounterpartyID: nstr("7826156685"),
			AccountRoot:    "62",
			ClosingSigned:  1,
		},
	}
	got := BuildReport(in)
	if got[0].Partner != "7826156685" {
		t.Errorf("Partner должен быть = PartnerINN (fallback), got %q", got[0].Partner)
	}
	if got[0].PartnerINN != "7826156685" {
		t.Errorf("PartnerINN = %q, want '7826156685'", got[0].PartnerINN)
	}
}

func TestBuildReport_KZ_4DigitPlan(t *testing.T) {
	// ТОО МФ Казахстан (141240004842), 4-значный план: 1210=ДЗ.
	in := []rawRow{
		{
			CompanyID:      "141240004842",
			CounterpartyID: nstr("KZ-PARTNER-1"),
			AccountRoot:    "1210",
			OpeningSigned:  500000,
			ClosingSigned:  600000,
		},
	}
	got := BuildReport(in)
	if len(got) != 1 {
		t.Fatalf("KZ юрлицо должно дать одну строку, got %d", len(got))
	}
	r := got[0]
	if r.Country != CountryKZ {
		t.Errorf("Country = %q, want KZ", r.Country)
	}
	if r.OpeningDZ != 500000 || r.ClosingDZ != 600000 {
		t.Errorf("KZ DZ amounts: %+v", r)
	}
}

func TestBuildReport_FillsCurrencyForRF(t *testing.T) {
	in := []rawRow{
		{
			CompanyID:      "6950135110", // РФ
			CounterpartyID: nstr("7826156685"),
			AccountRoot:    "62",
			ClosingSigned:  100,
		},
	}
	got := BuildReport(in)
	if got[0].Currency != "RUB" {
		t.Errorf("Currency для РФ-юрлица должна быть RUB, got %q", got[0].Currency)
	}
}

func TestBuildReport_FillsCurrencyForKZ(t *testing.T) {
	in := []rawRow{
		{
			CompanyID:      "141240004842", // КЗ
			CounterpartyID: nstr("KZ-PARTNER"),
			AccountRoot:    "1210",
			ClosingSigned:  100,
		},
	}
	got := BuildReport(in)
	if got[0].Currency != "KZT" {
		t.Errorf("Currency для КЗ-юрлица должна быть KZT, got %q", got[0].Currency)
	}
}

func TestBuildReport_RevenueLastMonthSeparateField(t *testing.T) {
	// Cr 90.x за период: TurnoverSigned=-100000 (вся выручка периода),
	// LastMonthSigned=-30000 (последний месяц периода).
	// После flip: RevenuePeriod=100000, RevenueLastMonth=30000.
	in := []rawRow{
		{
			CompanyID:       "6950135110",
			CounterpartyID:  nstr("7705935687"),
			AccountRoot:     "90",
			TurnoverSigned:  -100000,
			LastMonthSigned: -30000,
			ClosingSigned:   -100000,
		},
	}
	got := BuildReport(in)
	r := got[0]
	if r.RevenuePeriod != 100000 {
		t.Errorf("RevenuePeriod = %v, want 100000", r.RevenuePeriod)
	}
	if r.RevenueLastMonth != 30000 {
		t.Errorf("RevenueLastMonth = %v, want 30000", r.RevenueLastMonth)
	}
}

func TestBuildReport_NonRevenueLeavesRevenueLastMonthZero(t *testing.T) {
	// Для ДЗ/КЗ-счетов LastMonthSigned не используется, RevenueLastMonth = 0.
	in := []rawRow{
		{
			CompanyID:       "6950135110",
			CounterpartyID:  nstr("7826156685"),
			AccountRoot:     "62",
			TurnoverSigned:  5000,
			LastMonthSigned: 1000, // должно игнорироваться для KindDZ
			ClosingSigned:   5000,
		},
	}
	got := BuildReport(in)
	if got[0].RevenueLastMonth != 0 {
		t.Errorf("RevenueLastMonth для ДЗ-счёта должна быть 0, got %v", got[0].RevenueLastMonth)
	}
}

func TestBuildReport_PartnerNameFromCounterparty1C(t *testing.T) {
	// Контрагент попал по ICO=1 и НЕ входит в наши 15 ЮЛ → seed-имени нет.
	// Имя/канал/менеджер должны прийти из Counterparty1C.
	in := []rawRow{
		{
			CompanyID:      "6950135110",
			CounterpartyID: nstr("7700000001"), // не из seed
			AccountRoot:    "62",
			ClosingSigned:  1000,
			PartnerName:    nstr("ООО «Внешний Контрагент»"),
			Channel:        nstr("Опт"),
			Manager:        nstr("Иванов И.И."),
		},
	}
	got := BuildReport(in)
	r := got[0]
	if r.Partner != "ООО «Внешний Контрагент»" {
		t.Errorf("Partner = %q, want имя из Counterparty1C", r.Partner)
	}
	if r.Channel != "Опт" || r.Manager != "Иванов И.И." {
		t.Errorf("Channel/Manager не проставлены: %q / %q", r.Channel, r.Manager)
	}
}

func TestBuildReport_PartnerNameFallsBackToSeedWhenNoCounterparty(t *testing.T) {
	// Counterparty1C не подключён (PartnerName=NULL), но контрагент — наше ЮЛ:
	// имя берётся из seed, канал/менеджер пустые.
	in := []rawRow{
		{
			CompanyID:      "6950135110",
			CounterpartyID: nstr("690591512"), // ООО «Марк Формэль» из seed
			AccountRoot:    "62",
			ClosingSigned:  1000,
		},
	}
	got := BuildReport(in)
	r := got[0]
	if r.Partner != "ООО «Марк Формэль»" {
		t.Errorf("Partner = %q, want seed-имя", r.Partner)
	}
	if r.Channel != "" || r.Manager != "" {
		t.Errorf("без Counterparty1C канал/менеджер должны быть пустыми: %q / %q", r.Channel, r.Manager)
	}
}

// ── договор / срок / просрочка (Report-уровень) ────────────────────────────

func TestBuildReport_ContractNameAndRef(t *testing.T) {
	ref := `{"#",376807bc-0d88-4c06-9eb2-42b72b970afb,32:b7a690e2ba57de9411effb4b88d87f0d}`
	in := []rawRow{{
		CompanyID: "6950135110", CounterpartyID: nstr("7826156685"), AccountRoot: "62",
		ClosingSigned: 15000,
		ContractRef:   nstr(ref),
		ContractName:  nstr("Договор поставки № 1.34 от 18.09.2025"),
	}}
	got := BuildReport(in)
	if len(got) != 1 {
		t.Fatalf("len = %d, want 1", len(got))
	}
	if got[0].Contract != "Договор поставки № 1.34 от 18.09.2025" {
		t.Errorf("Contract = %q", got[0].Contract)
	}
	if got[0].ContractRef != ref {
		t.Errorf("ContractRef = %q, want raw 1С-ссылку для drill-down", got[0].ContractRef)
	}
}

func TestBuildReport_ContractTermAndOverdueFromDelay(t *testing.T) {
	// Срок = дата документа + отсрочка; просрочка считается от срока до reportDate.
	// 2026-01-01 + 30 дн = 2026-01-31; от него до 2026-03-01 = 29 дней (2026 не високосный).
	in := []rawRow{{
		CompanyID: "6950135110", CounterpartyID: nstr("7826156685"), AccountRoot: "62",
		ClosingSigned:   100,
		ContractDelay:   nint(30),
		ContractDocDate: ntime(day(2026, 1, 1)),
	}}
	got := BuildReport(in, day(2026, 3, 1))
	r := got[0]
	if r.PaymentTermDays != 30 {
		t.Errorf("PaymentTermDays = %d, want 30", r.PaymentTermDays)
	}
	if !r.PaymentDueDate.Equal(day(2026, 1, 31)) {
		t.Errorf("PaymentDueDate = %v, want 2026-01-31", r.PaymentDueDate)
	}
	if r.OverdueDays != 29 {
		t.Errorf("OverdueDays = %d, want 29", r.OverdueDays)
	}
}

func TestBuildReport_ExplicitPaymentDateWins(t *testing.T) {
	// Явная PaymentDate приоритетнее, чем дата документа + отсрочка.
	in := []rawRow{{
		CompanyID: "6950135110", CounterpartyID: nstr("7826156685"), AccountRoot: "62",
		ClosingSigned:   100,
		ContractDelay:   nint(30),
		ContractDocDate: ntime(day(2026, 1, 1)),
		ContractPayDate: ntime(day(2026, 2, 15)),
	}}
	got := BuildReport(in, day(2026, 3, 1))
	if !got[0].PaymentDueDate.Equal(day(2026, 2, 15)) {
		t.Errorf("PaymentDueDate = %v, want явную 2026-02-15", got[0].PaymentDueDate)
	}
	if got[0].OverdueDays != 14 {
		t.Errorf("OverdueDays = %d, want 14 (15.02→01.03)", got[0].OverdueDays)
	}
}

func TestBuildReport_NoReportDate_NoOverdueButTermKept(t *testing.T) {
	// Без reportDate просрочку не считаем, но отсрочку/срок проставляем.
	in := []rawRow{{
		CompanyID: "6950135110", CounterpartyID: nstr("7826156685"), AccountRoot: "62",
		ClosingSigned:   100,
		ContractDelay:   nint(45),
		ContractDocDate: ntime(day(2026, 1, 1)),
	}}
	got := BuildReport(in) // без reportDate
	if got[0].PaymentTermDays != 45 {
		t.Errorf("PaymentTermDays = %d, want 45", got[0].PaymentTermDays)
	}
	if got[0].OverdueDays != 0 {
		t.Errorf("OverdueDays = %d, want 0 без reportDate", got[0].OverdueDays)
	}
}

func TestBuildReport_NoContractLeavesFieldsEmpty(t *testing.T) {
	// Кредиторка/без договора: поля договора и срока пустые (срока нет нигде).
	in := []rawRow{{
		CompanyID: "6950135110", CounterpartyID: nstr("7826156685"), AccountRoot: "60",
		ClosingSigned: -500, // пассив → перевернётся в KZ
	}}
	got := BuildReport(in, day(2026, 3, 1))
	r := got[0]
	if r.Contract != "" || r.ContractRef != "" {
		t.Errorf("без договора Contract/ContractRef должны быть пустыми: %q / %q", r.Contract, r.ContractRef)
	}
	if r.PaymentTermDays != 0 || !r.PaymentDueDate.IsZero() || r.OverdueDays != 0 {
		t.Errorf("без договора срок/просрочка должны быть нулевыми: %+v", r)
	}
}
