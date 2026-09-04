package plans

import (
	"context"
	"fmt"
	"log"
	"time"

	"github.com/company/finance-api/internal/notifications"
)

// Уведомления по карточкам форм.
//
// Зачем: задания-ввода уведомления уже рассылали (назначение, делегирование,
// возврат, сдача), а КАРТОЧКИ форм — нет. Из-за этого этапы согласования
// выглядели «мёртвыми»: форма ушла на 1.2, но руководитель об этом не узнавал и
// искал задание, которого у этапа согласования не бывает. Отсюда же вопрос
// «что согласовывать».
//
// Кому что уходит:
//   - отправили на согласование → владельцу ЦЕЛЕВОГО шага: «форма ждёт решения»;
//   - согласовали и пошли дальше → владельцу следующего шага;
//   - вернули → владельцу шага, на который вернули (он и будет исправлять);
//   - утвердили и опубликовали → тому, кто отправлял форму в маршрут.
//
// Доставка best-effort и в фоне (как у заданий): сбой канала не должен ронять
// переход по маршруту — иначе процесс встанет из-за недоступного Bitrix.

// CardNotifier — канал уведомлений. Сигнатура та же, что у заданий
// (TaskNotifier): это один и тот же notifications.Service, и колокольчик с
// дублированием в Bitrix работает без отдельной настройки.
type CardNotifier interface {
	Create(ctx context.Context, in notifications.Input) (*notifications.Notification, error)
}

// WithNotifier подключает уведомления к процессу карточек.
func (s *CardService) WithNotifier(n CardNotifier) *CardService {
	s.notify = n
	return s
}

// notifyCard — одно уведомление в фоне. userID=0 (владелец шага не назначен) —
// молча пропускаем: это нормальное состояние настройки, а не ошибка.
func (s *CardService) notifyCard(userID int64, c Card, title, message, link string) {
	if s.notify == nil || userID == 0 {
		return
	}
	in := notifications.Input{
		UserID:     userID,
		Title:      title,
		Message:    message,
		Type:       notifications.TypeInfo,
		ObjectType: notifications.ObjPlanCard,
		Data: map[string]any{
			"card_id":   c.ID,
			"pl_id":     c.PlID,
			"form_code": c.FormCode,
			"step_code": c.StepCode,
			"url":       link,
		},
	}
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 8*time.Second)
		defer cancel()
		if _, err := s.notify.Create(ctx, in); err != nil {
			log.Printf("plans: уведомление по карточке не отправлено (user %d): %v", userID, err)
		}
	}()
}

// notifyCardTransition — разослать уведомления по итогу перехода карточки.
//
// Ссылка ведёт на экран периода: лист согласования — панель на нём, отдельной
// страницы у карточки нет. Формулировки — от лица получателя («ждёт вашего
// решения»), а не описания состояния системы: человек должен понять, что от него
// требуется, из текста уведомления.
func (s *CardService) notifyCardTransition(ctx context.Context, prev Card, next Card, in CardActionInput) {
	if s.notify == nil {
		return
	}
	owners := in.StepOwner
	if owners == nil && s.owners != nil {
		if o, err := s.owners.StepOwners(ctx, next.PlID, next.FormCode, next.ScopeKey); err == nil {
			owners = o
		}
	}
	link := fmt.Sprintf("/plans/%d", next.PlID)
	title := next.Title
	if title == "" {
		title = next.FormCode
	}

	switch in.Action {
	case "submit", "approve":
		if next.Status == CardOnApproval {
			s.notifyCard(owners[next.StepCode], next,
				"Планы: форма ждёт согласования",
				fmt.Sprintf("«%s» на шаге %s — ждёт вашего решения (согласовать или вернуть).", title, next.StepCode),
				link)
			return
		}
		// Дальше идти некуда — маршрут пройден.
		if next.Status == CardApproved || next.Status == CardPublished {
			s.notifyCard(prev.CreatedBy, next,
				"Планы: форма утверждена",
				fmt.Sprintf("«%s» прошла маршрут и утверждена%s.", title, publishedSuffix(next.Status)),
				link)
		}
	case "return":
		s.notifyCard(owners[next.StepCode], next,
			"Планы: форма возвращена на доработку",
			fmt.Sprintf("«%s» вернули на шаг %s%s.", title, next.StepCode, reasonSuffix(in.Comment)),
			link)
	case "reopen":
		s.notifyCard(owners[next.StepCode], next,
			"Планы: период переоткрыт",
			fmt.Sprintf("«%s» открыта на запись заново%s.", title, reasonSuffix(in.Comment)),
			link)
	}
}

// publishedSuffix — уточнение про публикацию: «утверждена» и «уехала в приёмник»
// это разные события, и получателю важно понимать, случилось ли второе.
func publishedSuffix(status string) string {
	if status == CardPublished {
		return " и опубликована во внешний контур"
	}
	return ""
}

func reasonSuffix(comment string) string {
	if comment == "" {
		return ""
	}
	return ": " + comment
}
