package plans

import (
	"errors"
	"time"
)

// Производственные календари р.д. по странам (Q3, SPEC §4.1, §7.3). Дата N-го
// рабочего дня месяца считается по календарю страны ответственного: выходные
// сб/вс − праздники + перенесённые рабочие выходные. Используется для SLA-сроков
// этапов (WF-04). Помесячные курсы/полные праздники ведутся в plans_country_calendar.

// CountryCalendar — календарь страны на год.
type CountryCalendar struct {
	Country      string          `json:"country"`
	Year         int             `json:"year"`
	Holidays     map[string]bool `json:"-"` // YYYY-MM-DD → нерабочий праздник
	WorkWeekends map[string]bool `json:"-"` // YYYY-MM-DD → рабочий выходной
}

func isWorkingDay(d time.Time, cal CountryCalendar) bool {
	ymd := d.Format("2006-01-02")
	if cal.Holidays[ymd] {
		return false
	}
	if cal.WorkWeekends[ymd] {
		return true
	}
	wd := d.Weekday()
	return wd != time.Saturday && wd != time.Sunday
}

// businessDayDate — дата N-го рабочего дня месяца (n ≥ 1) по календарю страны.
func businessDayDate(year, month, n int, cal CountryCalendar) (string, error) {
	if n < 1 {
		return "", errors.New("номер рабочего дня должен быть ≥ 1")
	}
	d := time.Date(year, time.Month(month), 1, 0, 0, 0, 0, time.UTC)
	count := 0
	for int(d.Month()) == month {
		if isWorkingDay(d, cal) {
			count++
			if count == n {
				return d.Format("2006-01-02"), nil
			}
		}
		d = d.AddDate(0, 0, 1)
	}
	return "", errors.New("в месяце меньше рабочих дней, чем запрошено")
}

// CalendarSeed — стартовые календари стран на 2026 (минимум: новогодние праздники).
// Полные календари ведутся в plans_country_calendar (глобальные настройки проекта).
func CalendarSeed() []CountryCalendar {
	ny := map[string]bool{
		"2026-01-01": true, "2026-01-02": true, "2026-01-07": true,
	}
	return []CountryCalendar{
		{Country: "RU", Year: 2026, Holidays: cloneSet(ny), WorkWeekends: map[string]bool{}},
		{Country: "BY", Year: 2026, Holidays: cloneSet(ny), WorkWeekends: map[string]bool{}},
		{Country: "KZ", Year: 2026, Holidays: cloneSet(ny), WorkWeekends: map[string]bool{}},
		{Country: "UZ", Year: 2026, Holidays: cloneSet(ny), WorkWeekends: map[string]bool{}},
	}
}

func cloneSet(m map[string]bool) map[string]bool {
	out := make(map[string]bool, len(m))
	for k, v := range m {
		out[k] = v
	}
	return out
}

func calendarFor(country string, seed []CountryCalendar) CountryCalendar {
	for _, c := range seed {
		if c.Country == country {
			return c
		}
	}
	return CountryCalendar{Country: country, Year: 2026, Holidays: map[string]bool{}, WorkWeekends: map[string]bool{}}
}
