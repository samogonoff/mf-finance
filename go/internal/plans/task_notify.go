package plans

import (
	"context"
	"fmt"
	"time"

	"github.com/company/finance-api/internal/notifications"
)

// Уведомления участникам заданий. До этого модуль планов вообще не был подключён
// к общему потоку уведомлений: человека назначали или делегировали ему задание, а
// узнать об этом он мог только зайдя на /plans. Канал тот же, что у остального
// кабинета (колокольчик + опциональное дублирование в B24).
//
// Доставка — best-effort в фоне: сбой уведомления не должен ронять смену
// исполнителя или делегирование.

// TaskNotifier — то, что модулю планов нужно от notifications.Service.
// Интерфейс (а не *Service) — чтобы движок заданий тестировался без БД.
type TaskNotifier interface {
	Create(ctx context.Context, in notifications.Input) (*notifications.Notification, error)
}

// WithNotifier подключает канал уведомлений к движку заданий.
func (s *TaskStore) WithNotifier(n TaskNotifier) *TaskStore {
	s.notify = n
	return s
}

// notifyTask шлёт уведомление одному пользователю про задание (в фоне).
func (s *TaskStore) notifyTask(userID int64, t Task, title, message string) {
	if s.notify == nil || userID == 0 {
		return
	}
	in := notifications.Input{
		UserID:     userID,
		Title:      title,
		Message:    message,
		Type:       notifications.TypeInfo,
		ObjectType: notifications.ObjPlanTask,
		Data: map[string]any{
			"task_id":    t.ID,
			"pl_id":      t.PlID,
			"stage_code": t.StageCode,
			"url":        taskLink(t),
		},
	}
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 8*time.Second)
		defer cancel()
		_, _ = s.notify.Create(ctx, in)
	}()
}

// taskLink — куда ведёт уведомление: форма МП открывается напрямую, прочие
// задания — в кокпит процесса своего периода.
func taskLink(t Task) string {
	if t.FormCode == "TPL-MP" {
		return fmt.Sprintf("/plans/mp-form/%d", t.ID)
	}
	return fmt.Sprintf("/plans/process/%d", t.PlID)
}

// taskPeriodLabel — «2026-07» для подписи уведомления (пусто, если период неизвестен).
func taskPeriodLabel(t Task) string {
	if t.Year == 0 {
		return ""
	}
	return fmt.Sprintf("%d-%02d", t.Year, t.Month)
}

// notifyAssigned — «на вас назначили задание».
func (s *TaskStore) notifyAssigned(userID int64, t Task) {
	msg := fmt.Sprintf("Этап %s · %s", t.StageCode, t.Title)
	if p := taskPeriodLabel(t); p != "" {
		msg += " · период " + p
	}
	s.notifyTask(userID, t, "Планы: назначено задание", msg)
}

// notifyDelegated — «вам делегировали задание».
func (s *TaskStore) notifyDelegated(userID int64, t Task, from string) {
	msg := fmt.Sprintf("Этап %s · %s", t.StageCode, t.Title)
	if from != "" {
		msg += "\nДелегировал: " + from
	}
	s.notifyTask(userID, t, "Планы: вам делегировали задание", msg)
}

// notifyReturned — «задание вернули на доработку».
func (s *TaskStore) notifyReturned(userID int64, t Task) {
	s.notifyTask(userID, t, "Планы: задание возвращено на доработку",
		fmt.Sprintf("Этап %s · %s", t.StageCode, t.Title))
}

// notifySubmitted — «задание сдано, ждёт вашей приёмки».
func (s *TaskStore) notifySubmitted(userID int64, t Task, by string) {
	msg := fmt.Sprintf("Этап %s · %s", t.StageCode, t.Title)
	if by != "" {
		msg += "\nСдал: " + by
	}
	s.notifyTask(userID, t, "Планы: задание сдано на проверку", msg)
}
