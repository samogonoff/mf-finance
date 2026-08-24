package plans

import (
	"context"
	"testing"
)

// Возврат без комментария невозможен (ТЗ МП §2.3 / Розница V-06).
func TestStageAction_ReturnRequiresComment(t *testing.T) {
	store := newMemStore()
	svc := NewService(store, NewMockFactSource(), newMemScope())
	ctx := context.Background()
	plID, _ := store.EnsureInstance(ctx, 2026, 6)

	if _, err := svc.StageAction(ctx, adminP, plID, 2026, 6, "RU", "1.1", "submit", "", ""); err != nil {
		t.Fatalf("submit 1.1: %v", err)
	}
	if _, err := svc.StageAction(ctx, adminP, plID, 2026, 6, "RU", "1.2", "return", "1.1", ""); err == nil {
		t.Fatal("возврат без комментария должен быть отклонён")
	}
	if _, err := svc.StageAction(ctx, adminP, plID, 2026, 6, "RU", "1.2", "return", "1.1", "переделать продажи"); err != nil {
		t.Fatalf("возврат с комментарием: %v", err)
	}
	list, _ := svc.Approvals(ctx, plID)
	if len(list) != 1 || list[0].Comment != "переделать продажи" || list[0].TargetStage != "1.1" {
		t.Fatalf("лист согласования: %+v", list)
	}
}

// При возврате решения от целевого этапа и выше аннулируются (revoked), но
// остаются в листе согласования.
func TestStageAction_ReturnRevokesApprovals(t *testing.T) {
	store := newMemStore()
	svc := NewService(store, NewMockFactSource(), newMemScope())
	ctx := context.Background()
	plID, _ := store.EnsureInstance(ctx, 2026, 6)

	for _, code := range []string{"1.1", "1.2", "1.3"} {
		if _, err := svc.StageAction(ctx, adminP, plID, 2026, 6, "RU", code, "submit", "", ""); err != nil {
			t.Fatalf("submit %s: %v", code, err)
		}
	}
	if _, err := svc.StageAction(ctx, adminP, plID, 2026, 6, "RU", "1.2", "approve", "", ""); err != nil {
		t.Fatalf("approve 1.2: %v", err)
	}
	if _, err := svc.StageAction(ctx, adminP, plID, 2026, 6, "RU", "1.3", "approve", "", ""); err != nil {
		t.Fatalf("approve 1.3: %v", err)
	}
	if _, err := svc.StageAction(ctx, adminP, plID, 2026, 6, "RU", "1.4", "return", "1.2", "не сходится свод"); err != nil {
		t.Fatalf("return: %v", err)
	}

	list, _ := svc.Approvals(ctx, plID)
	var revoked, alive int
	for _, e := range list {
		if e.Decision != "approve" {
			continue
		}
		if e.Revoked {
			revoked++
		} else {
			alive++
		}
	}
	if revoked != 2 || alive != 0 {
		t.Fatalf("ожидалось 2 аннулированных согласования, получено revoked=%d alive=%d (%+v)", revoked, alive, list)
	}
}
