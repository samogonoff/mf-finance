package debt

import "testing"

func TestCurrencyForCountry(t *testing.T) {
	cases := []struct {
		c    Country
		want string
	}{
		{CountryRB, "BYN"}, // подтверждено CurrID=1
		{CountryRF, "RUB"},
		{CountryKZ, "KZT"},
		{CountryUZ, "UZS"},
		{CountryTR, "TRY"},
		{CountryCZ, "CZK"},
		{CountryGB, "GBP"},
		{CountryCN, "CNY"},
		{CountryKG, "KGS"},
		{"", ""},            // пустая страна
		{Country("XX"), ""}, // неизвестная страна
	}
	for _, tc := range cases {
		if got := CurrencyForCountry(tc.c); got != tc.want {
			t.Errorf("CurrencyForCountry(%q) = %q, want %q", tc.c, got, tc.want)
		}
	}
}

func TestCurrencyForINN_KnownRF(t *testing.T) {
	// ООО ТД «Марк Формэль» — РФ
	if got := CurrencyForINN("6950135110"); got != "RUB" {
		t.Errorf("CurrencyForINN(6950135110) = %q, want RUB", got)
	}
}

func TestCurrencyForINN_KnownKZ(t *testing.T) {
	// ТОО МФ Казахстан
	if got := CurrencyForINN("141240004842"); got != "KZT" {
		t.Errorf("CurrencyForINN(141240004842) = %q, want KZT", got)
	}
}

func TestCurrencyForINN_KnownUZ(t *testing.T) {
	// ООО МАРК ФОРМЭЛЬ IT (Узбекистан)
	if got := CurrencyForINN("305554644"); got != "UZS" {
		t.Errorf("CurrencyForINN(305554644) = %q, want UZS", got)
	}
}

func TestCurrencyForINN_KnownBY(t *testing.T) {
	// ООО «Марк Формэль» — РБ
	if got := CurrencyForINN("690591512"); got != "BYN" {
		t.Errorf("CurrencyForINN(690591512) = %q, want BYN", got)
	}
}

func TestCurrencyForINN_Unknown(t *testing.T) {
	if got := CurrencyForINN("0000000000"); got != "" {
		t.Errorf("CurrencyForINN(0000000000) = %q, want '' (неизвестный ИНН)", got)
	}
}
