package plans

import (
	"context"
	"testing"
)

// Эффективная ставка площадки перебивает страновой дефолт (ТЗ МП §3.4).
func TestPickVat_PlatformOverridesCountry(t *testing.T) {
	rows := VatSeed()
	if got := pickVat(rows, "RU", 954); got != 0.1662 {
		t.Fatalf("Yandex Market: ожидалась эффективная 16,62 %%, получено %.4f", got)
	}
	if got := pickVat(rows, "RU", 335); got != 0.2036 {
		t.Fatalf("Wildberries: ожидалась эффективная 20,36 %%, получено %.4f", got)
	}
	// Площадка без своей строки — страновой дефолт.
	if got := pickVat(rows, "KZ", 474); got != 0.12 {
		t.Fatalf("KZ-площадка без своей ставки: ожидалось 12 %%, получено %.4f", got)
	}
	if got := pickVat(rows, "XX", 0); got != 0.20 {
		t.Fatalf("неизвестная страна: ожидался фолбэк 20 %%, получено %.4f", got)
	}
}

// Курс выбирается от частного (валюта+год+месяц) к общему (дефолт).
func TestPickFx_MonthlyPrecedence(t *testing.T) {
	rows := []FxRow{
		{Currency: "RUB", RateBYN: 0.0376},                       // дефолт
		{Currency: "RUB", Year: 2026, RateBYN: 0.0380},           // на год
		{Currency: "RUB", Year: 2026, Month: 6, RateBYN: 0.0390}, // на месяц
		{Currency: "KZT", RateBYN: 0.0068},
	}
	if got := pickFx(rows, "RUB", 2026, 6); got != 0.0390 {
		t.Fatalf("июнь-2026: ожидался помесячный курс, получено %.4f", got)
	}
	if got := pickFx(rows, "RUB", 2026, 7); got != 0.0380 {
		t.Fatalf("июль-2026: ожидался годовой курс, получено %.4f", got)
	}
	if got := pickFx(rows, "RUB", 2025, 3); got != 0.0376 {
		t.Fatalf("2025: ожидался дефолтный курс, получено %.4f", got)
	}
	if got := pickFx(rows, "BYN", 2026, 6); got != 1.0 {
		t.Fatalf("BYN — база, ожидался 1.0, получено %.4f", got)
	}
	if got := pickFx(rows, "UZS", 2026, 6); got != 1.0 {
		t.Fatalf("валюта без курса: ожидался нейтральный 1.0, получено %.4f", got)
	}
}

// Валюты площадок KZT/UZS поддержаны (ТЗ МП §3.3, МП-10: ввод в валюте площадки).
func TestRateBook_ConvertPlatformCurrencies(t *testing.T) {
	b := NewRateBook(nil)
	ctx := context.Background()

	// 1 000 000 KZT → BYN по seed-курсу 0,0068.
	if got := b.Convert(ctx, 1_000_000, "KZT", "BYN", 2026, 6); got != 6800 {
		t.Fatalf("KZT→BYN: ожидалось 6800, получено %.2f", got)
	}
	// 1 000 000 UZS → BYN по 0,00026.
	if got := b.Convert(ctx, 1_000_000, "UZS", "BYN", 2026, 6); got != 260 {
		t.Fatalf("UZS→BYN: ожидалось 260, получено %.2f", got)
	}
	if got := b.Convert(ctx, 100, "RUB", "RUB", 2026, 6); got != 100 {
		t.Fatalf("одинаковая валюта не должна пересчитываться, получено %.2f", got)
	}
	if got := normalizeAnyCurrency("kzt"); got != "KZT" {
		t.Fatalf("normalizeAnyCurrency: %s", got)
	}
}

// Снимок курсов фиксируется в версии карточки.
func TestRateBook_Snapshot(t *testing.T) {
	b := NewRateBook(nil)
	snap := b.Snapshot(context.Background(), 2026, 6, "RUB", "BYN", "KZT")
	rates, ok := snap["rates_byn"].(map[string]float64)
	if !ok || len(rates) != 3 {
		t.Fatalf("снимок курсов: %+v", snap)
	}
	if rates["RUB"] != 0.0376 {
		t.Fatalf("курс RUB в снимке: %.4f", rates["RUB"])
	}
}
