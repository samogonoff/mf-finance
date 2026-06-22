package debt

import "testing"

func TestNormalizeCountry(t *testing.T) {
	cases := []struct {
		in   string
		want Country
		ok   bool
	}{
		{"РФ", CountryRF, true},
		{"  Турция  ", CountryTR, true}, // trim
		{"Кыргызстан", CountryKG, true},
		{"Узбекистан", "", false}, // не наш формат (ждём "УЗ")
		{"", "", false},
		{"Марс", "", false},
	}
	for _, c := range cases {
		got, ok := normalizeCountry(c.in)
		if ok != c.ok || got != c.want {
			t.Errorf("normalizeCountry(%q) = (%q,%v), want (%q,%v)", c.in, got, ok, c.want, c.ok)
		}
	}
}

func TestInstallRevenueOverlay(t *testing.T) {
	// После теста вернуть классификацию к хардкод-базе.
	defer InstallRevenueOverlay(nil)

	InstallRevenueOverlay(map[Country][]string{
		CountryTR: {"600", "601"}, // у TR в defaultChart счетов нет — оверлей их добавит
		CountryRF: {"90", "60"},   // 90 уже revenue; 60 — KZ, перетирать НЕЛЬЗЯ
	})

	// Новая страна получила revenue-счета.
	if got := ClassifyAccount(CountryTR, "600"); got != KindRevenue {
		t.Errorf("TR 600 = %v, want KindRevenue", got)
	}
	if got := ClassifyAccount(CountryTR, "601.5"); got != KindRevenue { // по корню
		t.Errorf("TR 601.5 = %v, want KindRevenue (root 601)", got)
	}
	// Счёт не из оверлея у TR остаётся неизвестным.
	if got := ClassifyAccount(CountryTR, "320"); got != KindOther {
		t.Errorf("TR 320 = %v, want KindOther", got)
	}
	// База приоритетна: 60 у РФ остаётся КЗ, а не становится revenue.
	if got := ClassifyAccount(CountryRF, "60"); got != KindKZ {
		t.Errorf("RF 60 = %v, want KindKZ (overlay не должен перетирать базу)", got)
	}
	// Существующая ДЗ-классификация не тронута.
	if got := ClassifyAccount(CountryRF, "62.01"); got != KindDZ {
		t.Errorf("RF 62.01 = %v, want KindDZ", got)
	}
	// AccountsForCountry для TR теперь содержит revenue-счета.
	accs := AccountsForCountry(CountryTR)
	if len(accs) != 2 {
		t.Errorf("AccountsForCountry(TR) = %v, want 2 счёта", accs)
	}

	// Reset вернул дефолт: revenue-оверлей TR исчез.
	InstallRevenueOverlay(nil)
	if got := ClassifyAccount(CountryTR, "600"); got != KindOther {
		t.Errorf("после reset TR 600 = %v, want KindOther", got)
	}
}
