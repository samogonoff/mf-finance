package debt

import (
	"testing"
	"time"
)

func d(y int, m time.Month, day int) time.Time {
	return time.Date(y, m, day, 0, 0, 0, 0, time.UTC)
}

// T3: ВГО-фильтр finpl — безусловный [ВГО]=1 (флаг канонично посчитан апстримом
// Update_Table_Fin_PL, шире хардкода OurINNs: включает Дримдом/DR2/GP).
func TestVgoFinPLClause(t *testing.T) {
	got := vgoFinPLClause()
	if got != " AND [ВГО] = 1" {
		t.Errorf("vgoFinPLClause() = %q", got)
	}
}

// Период округляется к началу месяца; нижняя граница клампится к min (2025-01).
func TestFinPLMonthRange(t *testing.T) {
	min := parseFinPLMinMonth("2025-01-01")

	cases := []struct {
		name           string
		from, to       time.Time
		wantFrom, wantTo time.Time
	}{
		{"середина месяца → начало", d(2025, 3, 17), d(2025, 6, 20), d(2025, 3, 1), d(2025, 6, 1)},
		{"from раньше min → кламп", d(2024, 12, 15), d(2025, 5, 9), d(2025, 1, 1), d(2025, 5, 1)},
		{"первое число без сдвига", d(2025, 2, 1), d(2025, 2, 1), d(2025, 2, 1), d(2025, 2, 1)},
	}
	for _, c := range cases {
		gf := finPLMonthFrom(c.from, min)
		gt := finPLMonthTo(c.to)
		if !gf.Equal(c.wantFrom) {
			t.Errorf("%s: from = %s, want %s", c.name, gf.Format("2006-01-02"), c.wantFrom.Format("2006-01-02"))
		}
		if !gt.Equal(c.wantTo) {
			t.Errorf("%s: to = %s, want %s", c.name, gt.Format("2006-01-02"), c.wantTo.Format("2006-01-02"))
		}
	}
}

// Невалидный/пустой min → дефолт 2025-01-01.
func TestParseFinPLMinMonth(t *testing.T) {
	if got := parseFinPLMinMonth("2024-06-01"); !got.Equal(d(2024, 6, 1)) {
		t.Errorf("parseFinPLMinMonth(valid) = %s", got)
	}
	if got := parseFinPLMinMonth(""); !got.Equal(d(2025, 1, 1)) {
		t.Errorf("parseFinPLMinMonth(empty) = %s, want 2025-01-01", got)
	}
	if got := parseFinPLMinMonth("мусор"); !got.Equal(d(2025, 1, 1)) {
		t.Errorf("parseFinPLMinMonth(junk) = %s, want 2025-01-01", got)
	}
}
