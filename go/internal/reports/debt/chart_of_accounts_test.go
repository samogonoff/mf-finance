package debt

import (
	"sort"
	"testing"
)

func TestAccountRoot(t *testing.T) {
	cases := []struct{ in, want string }{
		{"62", "62"},
		{"62.01", "62"},
		{"62.01.1", "62"},
		{"90.01.1", "90"},
		{"9010", "9010"},
		{"1210", "1210"},
		{"", ""},
	}
	for _, c := range cases {
		if got := AccountRoot(c.in); got != c.want {
			t.Errorf("AccountRoot(%q) = %q, want %q", c.in, got, c.want)
		}
	}
}

func TestClassifyAccount_RF(t *testing.T) {
	cases := []struct {
		acc  string
		want AccountKind
	}{
		{"60.01", KindKZ},
		{"60.02", KindKZ},
		{"60", KindKZ},
		{"62.01", KindDZ},
		{"62.02", KindDZ},
		{"76.05", KindDZ},
		{"76.06", KindDZ},
		{"90.01.1", KindRevenue},
		{"90.07", KindRevenue},
		{"99", KindOther}, // прибыли/убытки — не наш кейс
		{"44.01", KindOther},
		{"51", KindOther},
		{"", KindOther},
	}
	for _, c := range cases {
		got := ClassifyAccount(CountryRF, c.acc)
		if got != c.want {
			t.Errorf("ClassifyAccount(RF, %q) = %d, want %d", c.acc, got, c.want)
		}
	}
}

func TestClassifyAccount_KZ_4DigitPlan(t *testing.T) {
	cases := []struct {
		acc  string
		want AccountKind
	}{
		{"1210", KindDZ},
		{"3310", KindKZ},
		{"3510", KindKZ},
		{"6010", KindRevenue},
		{"6020", KindOther}, // не в карте
		{"9000", KindOther},
	}
	for _, c := range cases {
		got := ClassifyAccount(CountryKZ, c.acc)
		if got != c.want {
			t.Errorf("ClassifyAccount(KZ, %q) = %d, want %d", c.acc, got, c.want)
		}
	}
}

func TestClassifyAccount_UnknownCountry(t *testing.T) {
	// TR/CZ/GB/CN/KG — пустая карта → всё KindOther.
	for _, country := range []Country{CountryTR, CountryCZ, CountryGB, CountryCN, CountryKG} {
		if got := ClassifyAccount(country, "60"); got != KindOther {
			t.Errorf("ClassifyAccount(%s, '60') = %d, want KindOther (карта не заполнена)", country, got)
		}
	}
}

func TestAccountsForCountry(t *testing.T) {
	got := AccountsForCountry(CountryRF)
	sort.Strings(got)
	want := []string{"60", "62", "76", "90"}
	if len(got) != len(want) {
		t.Fatalf("AccountsForCountry(RF) len = %d, want %d (%v)", len(got), len(want), got)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("AccountsForCountry(RF)[%d] = %q, want %q", i, got[i], want[i])
		}
	}
}

func TestAccountsForCountries_Dedup(t *testing.T) {
	// РФ и РБ имеют одинаковые корни (60, 62, 76, 90).
	got := AccountsForCountries([]Country{CountryRF, CountryRB})
	if len(got) != 4 {
		t.Errorf("РФ+РБ должны дать ровно 4 уникальных счёта (60/62/76/90), got %d: %v", len(got), got)
	}
}

func TestAccountsForCountries_Union(t *testing.T) {
	got := AccountsForCountries([]Country{CountryRF, CountryKZ})
	// РФ: 60, 62, 76, 90; KZ: 1210, 3310, 3510, 6010 — итого 8.
	if len(got) != 8 {
		t.Errorf("РФ+КЗ объединение должно дать 8 уникальных счетов, got %d: %v", len(got), got)
	}
}
