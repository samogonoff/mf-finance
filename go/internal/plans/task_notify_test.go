package plans

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/company/finance-api/internal/notifications"
)

// fakeNotifier ловит отправленные уведомления (канал — доставка фоновая).
type fakeNotifier struct{ got chan notifications.Input }

func newFakeNotifier() *fakeNotifier { return &fakeNotifier{got: make(chan notifications.Input, 4)} }

func (f *fakeNotifier) Create(_ context.Context, in notifications.Input) (*notifications.Notification, error) {
	f.got <- in
	return &notifications.Notification{ID: 1, UserID: in.UserID}, nil
}

func (f *fakeNotifier) wait(t *testing.T) notifications.Input {
	t.Helper()
	select {
	case in := <-f.got:
		return in
	case <-time.After(2 * time.Second):
		t.Fatal("уведомление не отправлено")
		return notifications.Input{}
	}
}

func TestTaskLink(t *testing.T) {
	if got := taskLink(Task{ID: 7, PlID: 3, FormCode: "TPL-MP"}); got != "/plans/mp-form/7" {
		t.Errorf("форма МП ведёт на %q", got)
	}
	if got := taskLink(Task{ID: 7, PlID: 3, FormCode: "TPL-CFO-EXP"}); got != "/plans/process/3" {
		t.Errorf("прочие задания ведут на %q", got)
	}
}

func TestTaskPeriodLabel(t *testing.T) {
	if got := taskPeriodLabel(Task{Year: 2026, Month: 7}); got != "2026-07" {
		t.Errorf("период = %q", got)
	}
	if got := taskPeriodLabel(Task{}); got != "" {
		t.Errorf("без периода ждали пусто, получили %q", got)
	}
}

func TestNotifyDelegatedContent(t *testing.T) {
	f := newFakeNotifier()
	s := (&TaskStore{}).WithNotifier(f)
	task := Task{ID: 42, PlID: 5, StageCode: "1.1", Title: "Бюджет продаж · МП large", FormCode: "TPL-MP", Year: 2026, Month: 7}

	s.notifyDelegated(77, task, "Мурашко")
	in := f.wait(t)

	if in.UserID != 77 {
		t.Errorf("получатель %d, ждали 77", in.UserID)
	}
	if !strings.Contains(in.Message, "Мурашко") || !strings.Contains(in.Message, "1.1") {
		t.Errorf("в тексте нет автора/этапа: %q", in.Message)
	}
	if in.ObjectType != notifications.ObjPlanTask {
		t.Errorf("object_type = %q", in.ObjectType)
	}
	if in.Data["url"] != "/plans/mp-form/42" {
		t.Errorf("ссылка = %v", in.Data["url"])
	}
}

func TestNotifyAssignedIncludesPeriod(t *testing.T) {
	f := newFakeNotifier()
	s := (&TaskStore{}).WithNotifier(f)

	s.notifyAssigned(9, Task{ID: 1, PlID: 2, StageCode: "1.5", Title: "Расходы ЦП", Year: 2026, Month: 7})
	in := f.wait(t)

	if !strings.Contains(in.Message, "2026-07") {
		t.Errorf("в тексте нет периода: %q", in.Message)
	}
}

// Без подключённого канала уведомления просто не шлются — движок заданий должен
// работать и в конфигурации без notifications (например, в тестах).
func TestNotifyNoopWithoutNotifier(t *testing.T) {
	s := &TaskStore{}
	s.notifyAssigned(1, Task{ID: 1})
	s.notifyTask(0, Task{ID: 1}, "t", "m")
}
