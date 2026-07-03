package plans

import (
	"context"
	"math"
	"testing"
)

func TestConvertAmount_Identity(t *testing.T) {
	if convertAmount(100, "RUB", "RUB") != 100 {
		t.Error("конвертация в ту же валюту должна быть тождественной")
	}
}

func TestConvertAmount_RubToByn(t *testing.T) {
	// 1 RUB = fxRateBYN(RUB) BYN.
	got := convertAmount(1000, "RUB", "BYN")
	want := 1000 * fxRateBYN("RUB")
	if math.Abs(got-want) > 1e-6 {
		t.Errorf("RUB→BYN = %.6f, want %.6f", got, want)
	}
}

func TestConvertAmount_RubToUsd_ViaByn(t *testing.T) {
	// RUB → BYN → USD: amount*rateRUB/rateUSD.
	got := convertAmount(3200, "RUB", "USD")
	want := 3200 * fxRateBYN("RUB") / fxRateBYN("USD")
	if math.Abs(got-want) > 1e-6 {
		t.Errorf("RUB→USD = %.6f, want %.6f", got, want)
	}
}

func TestConvertAmount_EmptyTreatedAsRub(t *testing.T) {
	if convertAmount(50, "", "RUB") != 50 {
		t.Error("пустая валюта-источник трактуется как RUB")
	}
}

func TestMpForm_CurrencyConversion_USD(t *testing.T) {
	svc := NewService(newMemStore(), NewMockFactSource(), newMemScope())
	form, err := svc.MpForm(context.Background(), adminP, 2026, 5, "large", "USD")
	if err != nil {
		t.Fatalf("MpForm: %v", err)
	}
	if form.Header.Currency != "USD" {
		t.Errorf("валюта шапки = %q, want USD", form.Header.Currency)
	}
	wantWB := convertAmount(357034569.85451651, "RUB", "USD")
	for _, b := range form.Blocks {
		if b.CodePL != 1046 {
			continue
		}
		for _, r := range b.Rows {
			if r.CodeCFO == 335 && math.Abs(r.Fact-wantWB) > 0.01 {
				t.Errorf("факт WB в USD = %.4f, want %.4f", r.Fact, wantWB)
			}
		}
	}
}

func TestMpForm_SmallSegment_PlatformsAndFact(t *testing.T) {
	svc := NewService(newMemStore(), NewMockFactSource(), newMemScope())
	form, err := svc.MpForm(context.Background(), adminP, 2026, 5, "small", "RUB")
	if err != nil {
		t.Fatalf("MpForm small: %v", err)
	}
	byCode := map[int]bool{}
	for _, p := range form.Platforms {
		byCode[p.CodeCFO] = true
	}
	if !byCode[338] || !byCode[339] {
		t.Errorf("small-форма должна показывать Kaspi(338)/Uzmarket(339), got %d площадок", len(form.Platforms))
	}
	var kaspiFact float64
	for _, b := range form.Blocks {
		if b.CodePL != 1046 {
			continue
		}
		for _, r := range b.Rows {
			if r.CodeCFO == 338 {
				kaspiFact = r.Fact
			}
		}
	}
	if kaspiFact == 0 {
		t.Error("факт Kaspi(338) должен подтянуться в small-форме")
	}
}

func TestFxRateSeed_HasThreeCurrencies(t *testing.T) {
	seen := map[string]bool{}
	for _, r := range FxRateSeed() {
		seen[r.Currency] = true
		if r.RateBYN <= 0 {
			t.Errorf("курс %s должен быть > 0", r.Currency)
		}
	}
	for _, c := range []string{"BYN", "RUB", "USD"} {
		if !seen[c] {
			t.Errorf("dir_fx_rate должен содержать %s", c)
		}
	}
}
