package plans

import "testing"

// В справочнике [001 CodeCFO] есть технические коды вроде «40RUBK» (валютные
// разрезы). Раньше синк читал CodeCFO как int64 и падал на первой такой строке —
// вместе с ним пропадал ВЕСЬ справочник магазинов, а не одна запись.
func TestParseNumericCFO(t *testing.T) {
	ok := map[string]int{"100": 100, " 335 ": 335, "0": 0, "1597": 1597}
	for raw, want := range ok {
		got, valid := parseNumericCFO(raw)
		if !valid || got != want {
			t.Errorf("%q → (%d, %v), ожидалось (%d, true)", raw, got, valid, want)
		}
	}
	for _, raw := range []string{"40RUBK", "", "  ", "12A", "RUB", "-5", "1.5"} {
		if got, valid := parseNumericCFO(raw); valid {
			t.Errorf("%q должен считаться нечисловым, получено %d", raw, got)
		}
	}
}
