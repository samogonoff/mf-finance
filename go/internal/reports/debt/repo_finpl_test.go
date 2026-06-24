package debt

import (
	"context"
	"testing"
	"time"
)

// T5/T6: в mock-режиме с finpl-провайдером сервис отдаёт revenue-строки нового
// формата (USD, ДЗ/КЗ=0), а не старые premaster-фикстуры.
func TestService_FinPLMock(t *testing.T) {
	s := NewService(true, nil, "finpl")
	resp, err := s.Report(context.Background(), Filters{DateTo: time.Date(2025, 6, 1, 0, 0, 0, 0, time.UTC)})
	if err != nil {
		t.Fatalf("Report: %v", err)
	}
	if len(resp.Rows) == 0 {
		t.Fatal("finpl mock пуст")
	}
	for _, r := range resp.Rows {
		if r.Currency != "USD" {
			t.Errorf("строка не USD: %+v", r)
		}
		if r.RevenuePeriod == 0 {
			t.Errorf("revenue=0 в finpl-фикстуре: %+v", r)
		}
		if r.OpeningDZ != 0 || r.ClosingKZ != 0 {
			t.Errorf("ДЗ/КЗ должны быть 0 в revenue-фикстуре: %+v", r)
		}
	}
}

// Дефолтный (nil) провайдер сохраняет старые premaster-фикстуры (ДЗ/КЗ).
func TestService_DefaultMockUnchanged(t *testing.T) {
	s := NewService(true, nil, "mssql")
	resp, err := s.Report(context.Background(), Filters{DateTo: time.Date(2026, 4, 1, 0, 0, 0, 0, time.UTC)})
	if err != nil {
		t.Fatalf("Report: %v", err)
	}
	if len(resp.Rows) != len(mockRows()) {
		t.Errorf("дефолтный mock = %d строк, want %d", len(resp.Rows), len(mockRows()))
	}
}

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

// T7: слияние выручки (finpl) и ДЗ/КЗ (premaster). Выручка из premaster-строк
// обнуляется (источник — finpl), чисто-выручочные premaster-строки выкидываются,
// finpl revenue-строки добавляются. Двойного счёта выручки нет.
func TestMergeFinPLPremaster(t *testing.T) {
	rev := []DebtRow{
		{Company: "ООО «Марк Формэль»", CompanyINN: "690591512", Partner: "ООО «Формэль»",
			Currency: "USD", RevenuePeriod: 1_000_000, RevenueLastMonth: 300_000},
	}
	prem := []DebtRow{
		// ДЗ-строка с собственной (премастерской) выручкой — выручку обнулить, строку оставить.
		{Company: "ООО «Марк Формэль»", CompanyINN: "690591512", Partner: "ООО «Формэль»",
			Account: "62", Currency: "BYN", ClosingDZ: 500_000, RevenuePeriod: 777_000},
		// Чисто-выручочная premaster-строка (без ДЗ/КЗ) — должна быть выкинута.
		{Company: "ООО «Марк Формэль»", CompanyINN: "690591512", Partner: "ООО «Формэль»",
			Account: "90", Currency: "BYN", RevenuePeriod: 999_000},
	}
	got := mergeFinPLPremaster(rev, prem)
	if len(got) != 2 {
		t.Fatalf("len = %d, want 2 (1 ДЗ premaster + 1 revenue finpl)", len(got))
	}
	// Premaster ДЗ-строка: выручка обнулена, сальдо сохранено.
	var dz, finRev *DebtRow
	for i := range got {
		switch got[i].Account {
		case "62":
			dz = &got[i]
		case "":
			finRev = &got[i]
		}
	}
	if dz == nil || dz.ClosingDZ != 500_000 {
		t.Fatalf("ДЗ-строка потеряна/искажена: %+v", dz)
	}
	if dz.RevenuePeriod != 0 {
		t.Errorf("выручка premaster не обнулена: %v", dz.RevenuePeriod)
	}
	if finRev == nil || finRev.RevenuePeriod != 1_000_000 || finRev.Currency != "USD" {
		t.Errorf("finpl revenue-строка отсутствует/искажена: %+v", finRev)
	}
}

func TestHasBalance(t *testing.T) {
	if hasBalance(DebtRow{RevenuePeriod: 100}) {
		t.Error("строка только с выручкой не должна считаться балансовой")
	}
	if !hasBalance(DebtRow{ClosingKZ: 1}) {
		t.Error("строка с ClosingKZ должна быть балансовой")
	}
	if !hasBalance(DebtRow{OpeningDZ: -5}) {
		t.Error("строка с OpeningDZ должна быть балансовой")
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
