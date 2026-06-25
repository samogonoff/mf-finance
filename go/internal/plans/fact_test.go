package plans

import (
	"context"
	"math"
	"testing"
)

func TestMockMpFact_LargeMay2026_WB(t *testing.T) {
	src := NewMockFactSource()
	rows, err := src.MpFact(context.Background(), 2026, 5, "large")
	if err != nil {
		t.Fatalf("MpFact error: %v", err)
	}
	var wb *FactRow
	for i := range rows {
		if rows[i].CodeCFO == 335 && rows[i].CodePL == 1046 {
			wb = &rows[i]
		}
	}
	if wb == nil {
		t.Fatalf("нет факта WB(335)/1046 за 2026-05 в %d строках", len(rows))
	}
	// Контрольная точка CHECKPOINT A — значение из прототипа Маркетплейсы_large (E/AC12).
	if math.Abs(wb.Amount-357034569.85) > 0.01 {
		t.Errorf("WB 1046 2026-05 = %.2f, want 357034569.85", wb.Amount)
	}
	if wb.Scenario != "Факт" || wb.Currency != "RUB" || wb.NameCFO == "" {
		t.Errorf("неполные атрибуты факта: %+v", *wb)
	}
}

func TestMockMpFact_SegmentIsolation(t *testing.T) {
	src := NewMockFactSource()
	large, _ := src.MpFact(context.Background(), 2026, 5, "large")
	for _, r := range large {
		// 338 (Kaspi), 339 (Uzmarket) — small; не должны попадать в large.
		if r.CodeCFO == 338 || r.CodeCFO == 339 {
			t.Errorf("small-площадка %d просочилась в large", r.CodeCFO)
		}
	}
}

func TestMockMpFact_UnknownSegmentEmpty(t *testing.T) {
	src := NewMockFactSource()
	rows, err := src.MpFact(context.Background(), 2026, 5, "bogus")
	if err != nil {
		t.Fatalf("unknown segment не должен быть ошибкой, got %v", err)
	}
	if len(rows) != 0 {
		t.Errorf("unknown segment должен дать пусто, got %d", len(rows))
	}
}
