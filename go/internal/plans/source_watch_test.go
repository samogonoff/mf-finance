package plans

import (
	"errors"
	"strings"
	"testing"
)

// ТЗ доработок п. 1.1: отклонение числа строк больше порога от предыдущего
// успешного прогона — алерт; ошибка источника — алерт; первый прогон — норма.
func TestSourceVerdict(t *testing.T) {
	cases := []struct {
		name       string
		rows, prev int
		err        error
		healthy    bool
		reasonPart string
	}{
		{"первый прогон", 10, -1, nil, true, ""},
		{"в пределах порога", 75, 100, nil, true, ""},
		{"падение сверх порога", 60, 100, nil, false, "−40 %"},
		{"источник пуст", 0, 100, nil, false, "строк 0 против 100"},
		{"рост — норма", 150, 100, nil, true, ""},
		{"ошибка", 0, 100, errors.New("TDS EOF"), false, "не отвечает"},
	}
	for _, c := range cases {
		healthy, reason := sourceVerdict(c.rows, c.prev, c.err, 0.3)
		if healthy != c.healthy || !strings.Contains(reason, c.reasonPart) {
			t.Errorf("%s: healthy=%v reason=%q", c.name, healthy, reason)
		}
	}
}
