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

// Сентинел «даты нет». В [001 CodeCFO] отсутствие даты закрытия записано не
// NULL и не пустой строкой, а 1900-01-01 — так помечены 196 из 206 действующих
// магазинов РБ. Пока сентинел проходил как обычная дата, V-09 читала его как
// «закрыт в 1900 году» и выбрасывала из формы ВЕСЬ справочник: на проде форма
// «Розница · РБ · 07.2026» открывалась с «0 из 0 магазинов».
func TestRetailDateOrEmpty_Sentinel(t *testing.T) {
	empty := []string{"1900-01-01", "1900-01-01T00:00:00Z", "1899-12-30", "", "   ", "не дата", "01.02.2026"}
	for _, raw := range empty {
		if got := retailDateOrEmpty(raw); got != "" {
			t.Errorf("%q → %q, ожидалась пустая дата", raw, got)
		}
	}
	real := map[string]string{
		"2026-02-28":            "2026-02-28",
		"2026-02-28T00:00:00Z":  "2026-02-28",
		" 2014-06-01T00:00:00Z": "2014-06-01",
	}
	for raw, want := range real {
		if got := retailDateOrEmpty(raw); got != want {
			t.Errorf("%q → %q, ожидалось %q", raw, got, want)
		}
	}
}

// V-09 на сентинеле: магазин с DateClose = 1900-01-01 действующий и обязан
// остаться в форме, а реально закрытый в прошлом — уйти.
func TestRetailStoreClosedBefore_Sentinel(t *testing.T) {
	if retailStoreClosedBefore(retailNoDate, 2026, 7) {
		t.Error("сентинел 1900-01-01 не должен считаться датой закрытия")
	}
	if retailStoreClosedBefore(retailDateOrEmpty("1900-01-01T00:00:00Z"), 2026, 7) {
		t.Error("нормализованный сентинел не должен считаться датой закрытия")
	}
	if !retailStoreClosedBefore("2026-02-28", 2026, 7) {
		t.Error("магазин, закрытый 28.02.2026, не должен попадать в период 07.2026")
	}
	if retailStoreClosedBefore("2026-09-30", 2026, 7) {
		t.Error("магазин, закрываемый позже начала периода, остаётся в форме")
	}
}
