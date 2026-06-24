package debt

import (
	"strings"
	"testing"
)

// T2: выручка по ТОЧНЫМ Дт/Кт-корреспонденциям ТЗ (per country), префиксное
// совпадение субсчёта (62.1 ⊇ 62.1.1). См. docs/reports/debt/tz-requirements.md.
func TestIsRevenue(t *testing.T) {
	cases := []struct {
		country  Country
		dr, cr   string
		want     bool
	}{
		{CountryRB, "62.1.1", "90.1.1", true},     // РБ Дт 62.1 Кт 90.1.1
		{CountryRB, "62.1", "90.1.1.2", true},     // префиксы с обеих сторон
		{CountryRB, "62.2", "90.1.1", false},      // не 62.1
		{CountryRF, "62.01", "90.01.1", true},     // РФ Дт 62 Кт 90.01
		{CountryRF, "76.09", "90.01", true},       // РФ второй вариант
		{CountryRF, "62", "91.01", false},         // 91 — не выручка
		{CountryKZ, "1210", "6010", true},         // КЗ
		{CountryKZ, "1210", "6020", false},
		{CountryUZ, "4015", "9010", true},         // УЗ
		{CountryUZ, "4010", "9010", false},
		{CountryRB, "90.1.1", "62.1", false},      // обратное направление — не выручка
	}
	for _, c := range cases {
		if got := IsRevenue(c.country, c.dr, c.cr); got != c.want {
			t.Errorf("IsRevenue(%s, %s→%s) = %v, want %v", c.country, c.dr, c.cr, got, c.want)
		}
	}
}

func TestAccMatch(t *testing.T) {
	if !accMatch("62.1.1", "62.1") {
		t.Error("62.1.1 должен матчить префикс 62.1")
	}
	if accMatch("62.10", "62.1") {
		t.Error("62.10 НЕ должен матчить префикс 62.1 (только точечная граница)")
	}
	if !accMatch("62.1", "62.1") {
		t.Error("точное равенство должно матчить")
	}
}

// CH-условие выручки — OR по странам, с префиксными LIKE.
func TestRevenueCHClause(t *testing.T) {
	cl := revenueCHClause()
	for _, frag := range []string{"country = 'РБ'", "dr_acc = '62.1'", "dr_acc LIKE '62.1.%'",
		"cr_acc = '90.1.1'", "country = 'РФ'", "76.09", "1210", "6010", "4015", "9010"} {
		if !strings.Contains(cl, frag) {
			t.Errorf("revenueCHClause не содержит %q", frag)
		}
	}
}
