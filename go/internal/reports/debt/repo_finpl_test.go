package debt

import (
	"testing"
)

// T4: маппинг сырой свёртки выручки Table_Fin_PL → DebtRow.
// Решения: выручка = GroupPL='ПРОДАЖИ'; валюта = USD-консолидация (AmountUSD).
func TestBuildFinPLReport_resolvesEntity(t *testing.T) {
	raw := []finplRevRow{
		{CompanyCode: "MF", Country: "BY", PartnerName: nstr("ООО «Формэль»"),
			RevenuePeriod: 100_000, RevenueLastMonth: 30_000},
	}
	got := buildFinPLReport(raw)
	if len(got) != 1 {
		t.Fatalf("len = %d, want 1", len(got))
	}
	r := got[0]
	if r.CompanyINN != "690591512" || r.Company != "ООО «Марк Формэль»" {
		t.Errorf("company = %q/%q", r.Company, r.CompanyINN)
	}
	if r.Country != CountryRB {
		t.Errorf("country = %q, want РБ", r.Country)
	}
	if r.Partner != "ООО «Формэль»" {
		t.Errorf("partner = %q", r.Partner)
	}
	if r.Currency != "USD" {
		t.Errorf("currency = %q, want USD", r.Currency)
	}
	if r.RevenuePeriod != 100_000 || r.RevenueLastMonth != 30_000 {
		t.Errorf("revenue = %v/%v", r.RevenuePeriod, r.RevenueLastMonth)
	}
	// На revenue-срезе (T4) ДЗ/КЗ ещё нули — их добавит Premaster в T7.
	if r.OpeningDZ != 0 || r.ClosingKZ != 0 || r.TurnoverDZ != 0 {
		t.Errorf("ДЗ/КЗ должны быть 0 на revenue-срезе: %+v", r)
	}
}

// ВГО-компания сверх 15 (Дримдом) резолвится через vgoExtraEntities.
func TestBuildFinPLReport_extraVGO(t *testing.T) {
	got := buildFinPLReport([]finplRevRow{{CompanyCode: "DR", Country: "BY", RevenuePeriod: 5}})
	if got[0].CompanyINN != "692221084" {
		t.Errorf("DR.CompanyINN = %q, want 692221084", got[0].CompanyINN)
	}
}

// Неизвестный код компании не теряется: показываем код как имя, страну берём из витрины.
func TestBuildFinPLReport_unknownCode(t *testing.T) {
	got := buildFinPLReport([]finplRevRow{{CompanyCode: "ZZZ", Country: "RU", RevenuePeriod: 1}})
	if got[0].Company != "ZZZ" || got[0].CompanyINN != "" {
		t.Errorf("unknown: company=%q inn=%q", got[0].Company, got[0].CompanyINN)
	}
	if got[0].Country != CountryRF {
		t.Errorf("unknown country = %q, want РФ", got[0].Country)
	}
}

func TestCountryFromFinPL(t *testing.T) {
	cases := map[string]Country{"BY": CountryRB, "RU": CountryRF, "KZ": CountryKZ, "UZ": CountryUZ}
	for in, want := range cases {
		if got := countryFromFinPL(in); got != want {
			t.Errorf("countryFromFinPL(%q) = %q, want %q", in, got, want)
		}
	}
}

// Выбор юрлиц приходит как ИНН — транслируем в коды Table_Fin_PL.Компания для SQL-фильтра.
func TestInnsToFinPLCodes(t *testing.T) {
	got := innsToFinPLCodes([]string{"690591512", "6950135110", "692221084", "999"})
	set := map[string]bool{}
	for _, c := range got {
		set[c] = true
	}
	if !set["MF"] || !set["TDMF"] || !set["DR"] {
		t.Errorf("ожидались коды MF/TDMF/DR, получили %v", got)
	}
	if set[""] || len(got) != 3 {
		t.Errorf("неизвестный ИНН не должен давать код: %v", got)
	}
}
