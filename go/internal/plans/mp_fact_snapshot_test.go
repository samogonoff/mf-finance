package plans

import (
	"context"
	"testing"
	"time"
)

// Снимок факта МП (правки ТЗ 02.09.2026, часть 2 и МП §12/§13). Живой БД тесты не
// требуют: проверяется отсечение горизонта и поведение источника поверх снимка —
// именно там правки меняют смысл.

// Мусор за плановым горизонтом отсекается (правка МП §13: в факте есть месяц
// 2027-09-01 на две строки с суммой −1119).
func TestSnapshot_TrimsGarbageBeyondHorizon(t *testing.T) {
	s := &MpFactSnapshot{}
	rows := []FactRow{
		{CodeCFO: 335, CodePL: 1046, Year: 2026, Month: 6, Amount: 100},
		{CodeCFO: 335, CodePL: 1046, Year: 2026, Month: 12, Amount: 200},
		{CodeCFO: 335, CodePL: 1046, Year: 2027, Month: 9, Amount: -1119}, // мусор
		{CodeCFO: 335, CodePL: 1046, Amount: 5},                           // без периода
	}
	got := s.trimHorizon(rows, 2026, 6)
	if len(got) != 2 {
		t.Fatalf("после отсечения ожидались 2 строки, получено %d: %+v", len(got), got)
	}
	for _, r := range got {
		if r.Year == 2027 {
			t.Fatal("строка за плановым горизонтом не должна попадать в снимок")
		}
		if r.Year == 0 || r.Month == 0 {
			t.Fatal("строка без периода не должна попадать в снимок")
		}
	}
}

// Горизонт — ГОД ПЛАНА периода, а не «сейчас плюс N месяцев»: тактика ведётся на
// год, таргеты в приёмнике покрывают месяцы до конца года. Поэтому декабрь того
// же года остаётся, а следующий год отсекается независимо от месяца периода.
func TestSnapshot_HorizonIsPlanYear(t *testing.T) {
	s := &MpFactSnapshot{}
	next := []FactRow{{CodeCFO: 335, CodePL: 1046, Year: 2027, Month: 9, Amount: 10}}
	if got := s.trimHorizon(next, 2026, 6); len(got) != 0 {
		t.Fatalf("для плана 2026 года сентябрь 2027 за горизонтом: %+v", got)
	}
	if got := s.trimHorizon(next, 2026, 12); len(got) != 0 {
		t.Fatalf("декабрьский период не расширяет горизонт на следующий год: %+v", got)
	}
	dec := []FactRow{{CodeCFO: 335, CodePL: 1046, Year: 2026, Month: 12, Amount: 10}}
	if got := s.trimHorizon(dec, 2026, 6); len(got) != 1 {
		t.Fatalf("декабрь года плана обязан оставаться: %+v", got)
	}
	// Прошлые периоды нужны для колонок «факт прошлого года» — их не режем.
	prev := []FactRow{{CodeCFO: 335, CodePL: 1046, Year: 2025, Month: 6, Amount: 10}}
	if got := s.trimHorizon(prev, 2026, 6); len(got) != 1 {
		t.Fatalf("прошлый год нужен форме (факт прошлого года): %+v", got)
	}
}

// Регламент собирает снимок за ПРОШЛЫЙ месяц: данные отчётного месяца готовы не
// раньше 20-го числа текущего (правка МП §12).
func TestSnapshotRefresher_UsesPreviousMonth(t *testing.T) {
	src := &recordingFactSource{}
	snap := &MpFactSnapshot{src: src} // pool=nil → Refresh не пишет
	r := NewSnapshotRefresher(snap, []string{"large"}, time.Hour)
	r.now = func() time.Time { return time.Date(2026, 9, 3, 10, 0, 0, 0, time.UTC) }

	r.RefreshCurrent(context.Background())
	if src.year != 0 || src.month != 0 {
		t.Fatalf("без пула Refresh не должен читать источник, got %04d-%02d", src.year, src.month)
	}

	// С пулом обращение к источнику пошло бы за 2026-08; проверяем расчёт периода
	// отдельно, чтобы тест не требовал БД.
	y, m := hintPrevMonth(r.now())
	if y != 2026 || m != 8 {
		t.Fatalf("регламент должен работать с прошлым месяцем, got %04d-%02d", y, m)
	}
}

// recordingFactSource — запоминает, за какой период его спросили.
type recordingFactSource struct{ year, month int }

func (s *recordingFactSource) MpFact(_ context.Context, y, m int, _ string) ([]FactRow, error) {
	s.year, s.month = y, m
	return nil, nil
}
func (s *recordingFactSource) MpStrategy(context.Context, int, int, string) ([]FactRow, error) {
	return nil, nil
}
func (s *recordingFactSource) MpTaktTarget(context.Context, int, int, string) ([]FactRow, error) {
	return nil, nil
}
