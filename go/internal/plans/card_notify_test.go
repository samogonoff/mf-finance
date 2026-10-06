package plans

import (
	"context"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/company/finance-api/internal/notifications"
)

// Уведомления по карточкам форм: согласующий должен узнать, что форма ждёт его
// решения, — иначе этап согласования выглядит «мёртвым» (заданий у него нет).

// recordingNotifier — собирает отправленные уведомления.
type recordingNotifier struct {
	mu   sync.Mutex
	sent []notifications.Input
}

func (n *recordingNotifier) Create(_ context.Context, in notifications.Input) (*notifications.Notification, error) {
	n.mu.Lock()
	defer n.mu.Unlock()
	n.sent = append(n.sent, in)
	return &notifications.Notification{}, nil
}

func (n *recordingNotifier) wait(t *testing.T, want int) []notifications.Input {
	t.Helper()
	return n.waitFor(want, 2*time.Second)
}

// waitFor — ожидание с явным таймаутом. Отдельный метод нужен проверке «ничего
// не отправлено»: там ждать полный таймаут бессмысленно, тест только тормозит.
func (n *recordingNotifier) waitFor(want int, timeout time.Duration) []notifications.Input {
	deadline := time.Now().Add(timeout)
	for {
		n.mu.Lock()
		got := append([]notifications.Input(nil), n.sent...)
		n.mu.Unlock()
		if len(got) >= want || time.Now().After(deadline) {
			return got
		}
		time.Sleep(5 * time.Millisecond)
	}
}

func notifyFixture() (*CardService, *recordingNotifier) {
	n := &recordingNotifier{}
	cards := &fakeCardStore{card: Card{
		ID: 42, PlID: 7, FormCode: TemplateRetail, ScopeKey: "BY", Title: "Розница BY",
		Country: "BY", CurrentVersion: 1, Status: CardDraft, StepCode: "1.1", CreatedBy: 100,
	}}
	svc := NewCardService(cards).WithNotifier(n)
	return svc, n
}

// Отправка на согласование уведомляет владельца ЦЕЛЕВОГО шага.
func TestCardNotify_SubmitNotifiesNextStepOwner(t *testing.T) {
	svc, n := notifyFixture()
	next := Card{ID: 42, PlID: 7, FormCode: TemplateRetail, Title: "Розница BY",
		Status: CardOnApproval, StepCode: "1.2"}

	svc.notifyCardTransition(context.Background(), Card{CreatedBy: 100}, next,
		CardActionInput{Action: "submit", StepOwner: map[string]int64{"1.2": 55, "1.3": 66}})

	got := n.wait(t, 1)
	if len(got) != 1 {
		t.Fatalf("ожидалось одно уведомление, получено %d", len(got))
	}
	if got[0].UserID != 55 {
		t.Fatalf("уведомление должно уйти владельцу шага 1.2 (55), получено %d", got[0].UserID)
	}
	if got[0].ObjectType != notifications.ObjPlanCard {
		t.Fatalf("тип объекта: got %q", got[0].ObjectType)
	}
	if got[0].Data["url"] != "/plans/7?card=42" {
		t.Fatalf("ссылка должна вести на экран периода, got %v", got[0].Data["url"])
	}
}

// Возврат формы продаж уведомляет только владельца целевого шага.
func TestCardNotify_ReturnNotifiesTargetStepOnly(t *testing.T) {
	svc, n := notifyFixture()
	next := Card{ID: 42, PlID: 7, FormCode: TemplateRetail, Title: "Розница BY",
		Status: CardReturned, StepCode: "1.1"}

	svc.notifyCardTransition(context.Background(), Card{CreatedBy: 100}, next,
		CardActionInput{Action: "return", Comment: "пересчитать август",
			StepOwner: map[string]int64{"1.1": 11, "1.2": 55}})

	got := n.wait(t, 1)
	if len(got) != 1 {
		t.Fatalf("ожидалось уведомление владельцу шага: %+v", got)
	}
	users := map[int64]bool{}
	for _, in := range got {
		users[in.UserID] = true
		// Причина возврата обязательна по маршруту — она должна быть и в
		// уведомлении, иначе человек идёт искать её в карточке.
		if !strings.Contains(in.Message, "пересчитать август") {
			t.Fatalf("в тексте должна быть причина возврата: %q", in.Message)
		}
	}
	if !users[11] || users[100] {
		t.Fatalf("получатели: got %v, want только владельца шага 11", users)
	}
}

// Один и тот же человек не должен получить возврат дважды (он и владелец шага, и
// автор карточки).
func TestCardNotify_ReturnDoesNotDuplicateSameUser(t *testing.T) {
	svc, n := notifyFixture()
	next := Card{ID: 42, PlID: 7, Status: CardReturned, StepCode: "1.1"}

	svc.notifyCardTransition(context.Background(), Card{CreatedBy: 11}, next,
		CardActionInput{Action: "return", Comment: "исправить", StepOwner: map[string]int64{"1.1": 11}})

	if got := n.waitFor(2, 100*time.Millisecond); len(got) != 1 {
		t.Fatalf("ожидалось одно уведомление без дубля: %+v", got)
	}
}

// Финальное утверждение уведомляет владельца текущего шага.
func TestCardNotify_ApprovedNotifiesFinalOwner(t *testing.T) {
	svc, n := notifyFixture()
	next := Card{ID: 42, PlID: 7, FormCode: TemplateRetail, Title: "Розница BY",
		Status: CardPublished, StepCode: "1.4"}

	svc.notifyCardTransition(context.Background(), Card{CreatedBy: 100}, next,
		CardActionInput{Action: "approve", StepOwner: map[string]int64{"1.4": 77}})

	got := n.wait(t, 1)
	if len(got) != 1 || got[0].UserID != 77 {
		t.Fatalf("утверждение должно уведомить владельца шага (77): %+v", got)
	}
	// «Утверждена» и «уехала в приёмник» — разные события, и это должно быть видно.
	if !strings.Contains(got[0].Message, "опубликована") {
		t.Fatalf("для published в тексте должна быть публикация: %q", got[0].Message)
	}
}

// Владелец шага не назначен — уведомление не уходит, и это не ошибка.
func TestCardNotify_NoOwnerIsSilent(t *testing.T) {
	svc, n := notifyFixture()
	next := Card{ID: 42, PlID: 7, Status: CardOnApproval, StepCode: "1.2"}

	svc.notifyCardTransition(context.Background(), Card{}, next,
		CardActionInput{Action: "submit", StepOwner: map[string]int64{}})

	if got := n.waitFor(1, 50*time.Millisecond); len(got) != 0 {
		t.Fatalf("без владельца шага уведомлений быть не должно: %+v", got)
	}
}

func TestCardNotify_DistinguishesCardsAndStepsForSameOwner(t *testing.T) {
	svc, n := notifyFixture()
	owners := map[string]int64{"fin": 55, "1.3": 55}
	by := Card{ID: 42, PlID: 7, FormCode: TemplateRetail, Country: "BY", ScopeKey: "BY", StepCode: "fin", Status: CardOnApproval}
	ru := Card{ID: 43, PlID: 7, FormCode: TemplateRetail, Country: "RU", ScopeKey: "RU", StepCode: "1.3", Status: CardOnApproval}
	svc.notifyCardTransition(context.Background(), Card{}, by, CardActionInput{Action: "submit", StepOwner: owners})
	svc.notifyCardTransition(context.Background(), Card{}, ru, CardActionInput{Action: "approve", StepOwner: owners})
	got := n.wait(t, 2)
	if len(got) != 2 {
		t.Fatalf("two cards/steps need two messages: %+v", got)
	}
	for _, in := range got {
		if in.UserID != 55 {
			t.Fatalf("wrong assignee: %+v", in)
		}
		if !strings.Contains(in.Title, "Розница") || !strings.Contains(in.Title, "шаг") {
			t.Fatalf("title omits form/step: %q", in.Title)
		}
	}
	if got[0].Title == got[1].Title || got[0].Data["card_id"] == got[1].Data["card_id"] {
		t.Fatalf("distinct cards merged: %+v", got)
	}
}
