package plans

import "testing"

func TestBusinessDay_NoHolidays(t *testing.T) {
	cal := CountryCalendar{Country: "RU", Year: 2026}
	// Январь 2026: 1 — четверг. Без праздников 2-й р.д. = 2 января (пт).
	d, err := businessDayDate(2026, 1, 2, cal)
	if err != nil {
		t.Fatalf("businessDayDate: %v", err)
	}
	if d != "2026-01-02" {
		t.Errorf("2-й р.д. янв-2026 (без праздников) = %s, want 2026-01-02", d)
	}
}

func TestBusinessDay_WithHolidays(t *testing.T) {
	cal := CountryCalendar{
		Country:  "RU",
		Year:     2026,
		Holidays: map[string]bool{"2026-01-01": true, "2026-01-02": true},
	}
	// 1,2 — праздники; 3,4 — выходные (сб,вс); 2-й р.д. = 6 января.
	d, err := businessDayDate(2026, 1, 2, cal)
	if err != nil {
		t.Fatalf("businessDayDate: %v", err)
	}
	if d != "2026-01-06" {
		t.Errorf("2-й р.д. с праздниками = %s, want 2026-01-06", d)
	}
}

func TestBusinessDay_WorkingWeekend(t *testing.T) {
	cal := CountryCalendar{
		Country:      "RU",
		Year:         2026,
		WorkWeekends: map[string]bool{"2026-01-03": true}, // суббота сделана рабочей
	}
	// 1(чт),2(пт),3(сб-раб) → 3-й р.д. = 3 января.
	d, err := businessDayDate(2026, 1, 3, cal)
	if err != nil {
		t.Fatalf("businessDayDate: %v", err)
	}
	if d != "2026-01-03" {
		t.Errorf("3-й р.д. с рабочей субботой = %s, want 2026-01-03", d)
	}
}

func TestBusinessDay_TooLarge(t *testing.T) {
	cal := CountryCalendar{Country: "RU", Year: 2026}
	if _, err := businessDayDate(2026, 1, 99, cal); err == nil {
		t.Error("ожидалась ошибка: в месяце меньше 99 рабочих дней")
	}
}
