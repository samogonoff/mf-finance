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
	if got[0].Data["url"] != "/plans/7" {
		t.Fatalf("ссылка должна вести на экран периода, got %v", got[0].Data["url"])
	}
}

// Возврат уведомляет владельца шага, НА КОТОРЫЙ вернули, И автора карточки.
//
// Владелец шага заполнения часто не назначен (заполняют по заданиям, а не по
// этапу), и без автора о возврате не узнавал никто — форма просто зависала.
func TestCardNotify_ReturnNotifiesTargetStepAndAuthor(t *testing.T) {
	svc, n := notifyFixture()
	next := Card{ID: 42, PlID: 7, FormCode: TemplateRetail, Title: "Розница BY",
		Status: CardReturned, StepCode: "1.1"}

	svc.notifyCardTransition(context.Background(), Card{CreatedBy: 100}, next,
		CardActionInput{Action: "return", Comment: "пересчитать август",
			StepOwner: map[string]int64{"1.1": 11, "1.2": 55}})

	got := n.wait(t, 2)
	if len(got) != 2 {
		t.Fatalf("ожидались уведомления владельцу шага и автору: %+v", got)
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
	if !users[11] || !users[100] {
		t.Fatalf("получатели: got %v, want владельца шага 11 и автора 100", users)
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

// Утверждение (и публикация) уведомляет того, кто отправлял форму в маршрут.
func TestCardNotify_ApprovedNotifiesAuthor(t *testing.T) {
	svc, n := notifyFixture()
	next := Card{ID: 42, PlID: 7, FormCode: TemplateRetail, Title: "Розница BY",
		Status: CardPublished, StepCode: "1.4"}

	svc.notifyCardTransition(context.Background(), Card{CreatedBy: 100}, next,
		CardActionInput{Action: "approve", StepOwner: map[string]int64{}})

	got := n.wait(t, 1)
	if len(got) != 1 || got[0].UserID != 100 {
		t.Fatalf("утверждение должно уведомить автора карточки (100): %+v", got)
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
