package plans

import (
	"context"
	"log"
	"time"
)

// Источник факта МП поверх снимка (правка ТЗ 02.09.2026 МП §12: «читать вживую
// нельзя»).
//
// Реализует тот же MpFactSource, что и OLAP-коннектор, поэтому форма не знает,
// откуда пришли данные — меняется только конструктор в main. Живого обращения к
// OLAP здесь нет: если снимок не собран, отдаём пусто, а не «на всякий случай»
// идём в базу. Иначе поведение зависело бы от того, успел ли пройти регламент, —
// то есть ровно от того, что правка запрещает.

// snapshotFactSource — MpFactSource, читающий снимок.
type snapshotFactSource struct {
	snap *MpFactSnapshot
}

// NewSnapshotFactSource — источник формы поверх снимка.
func NewSnapshotFactSource(snap *MpFactSnapshot) MpFactSource {
	return &snapshotFactSource{snap: snap}
}

func (s *snapshotFactSource) MpFact(ctx context.Context, year, month int, segment string) ([]FactRow, error) {
	return s.layer(ctx, year, month, segment, SnapLayerFact)
}

func (s *snapshotFactSource) MpStrategy(ctx context.Context, year, month int, segment string) ([]FactRow, error) {
	return s.layer(ctx, year, month, segment, SnapLayerStrategy)
}

func (s *snapshotFactSource) MpTaktTarget(ctx context.Context, year, month int, segment string) ([]FactRow, error) {
	return s.layer(ctx, year, month, segment, SnapLayerTakt)
}

func (s *snapshotFactSource) layer(ctx context.Context, year, month int, segment, layer string) ([]FactRow, error) {
	rows, meta, err := s.snap.Rows(ctx, year, month, layer, segment)
	if err != nil {
		return nil, err
	}
	if !meta.Collected {
		// Не ошибка: форма открывается и показывает отметку «снимок не собран».
		// Ошибкой это делать нельзя — иначе новый период невозможно начать
		// заполнять до первого прогона регламента.
		log.Printf("plans: снимок факта МП (%s, %04d-%02d, сегмент %q) не собран", layer, year, month, segment)
		return nil, nil
	}
	return rows, nil
}

// SnapshotRefresher — регламентное обновление снимка (часть 2 правок: «данные
// питаются за период»).
type SnapshotRefresher struct {
	snap     *MpFactSnapshot
	segments []string
	interval time.Duration
	now      func() time.Time
}

// NewSnapshotRefresher — конструктор. interval=0 → раз в сутки.
func NewSnapshotRefresher(snap *MpFactSnapshot, segments []string, interval time.Duration) *SnapshotRefresher {
	if interval <= 0 {
		interval = 24 * time.Hour
	}
	return &SnapshotRefresher{snap: snap, segments: segments, interval: interval, now: time.Now}
}

// Start — фоновое обновление. Первый прогон сразу: после деплоя снимок должен
// появиться, не дожидаясь суток.
func (r *SnapshotRefresher) Start(ctx context.Context) {
	if r.snap == nil {
		return
	}
	go func() {
		t := time.NewTicker(r.interval)
		defer t.Stop()
		for {
			r.RefreshCurrent(ctx)
			select {
			case <-ctx.Done():
				return
			case <-t.C:
			}
		}
	}()
}

// RefreshCurrent — собрать снимок за ПРОШЛЫЙ месяц (правка МП §12: данные
// отчётного месяца готовы не раньше 20-го числа текущего, поэтому форма работает
// с прошлым месяцем).
func (r *SnapshotRefresher) RefreshCurrent(ctx context.Context) {
	year, month := hintPrevMonth(r.now())
	for _, seg := range r.segments {
		if _, err := r.snap.Refresh(ctx, year, month, seg); err != nil {
			log.Printf("plans: регламент снимка факта МП (%04d-%02d, %q): %v", year, month, seg, err)
		}
	}
}
