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
//   - утвердили и опубликовали → владельцу финального шага карточки продаж.
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
func (s *CardService) notifyCard(userID int64, c Card, action, title, message, link string) {
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
			"scope_key": c.ScopeKey,
			"country":   c.Country,
			"step_code": c.StepCode,
			"action":    action,
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
// Ссылка ведёт на раскрытую карточку периода. Формулировки — от лица получателя («ждёт вашего
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
	link := fmt.Sprintf("/plans/%d?card=%d", next.PlID, next.ID)
	sales := next.FormCode == TemplateRetail || next.FormCode == TemplateMP
	title := next.Title
	if title == "" {
		title = next.FormCode
	}
	form := "форма " + next.FormCode
	scope := next.Country
	if next.FormCode == TemplateRetail {
		form = "Розница"
	}
	if next.FormCode == TemplateMP {
		form = "Маркетплейсы"
		if next.ScopeKey == "large" {
			scope = "крупные МП"
		} else if next.ScopeKey == "small" {
			scope = "мелкие МП"
		} else {
			scope = next.ScopeKey
		}
	}
	identity := fmt.Sprintf("%s · %s · карточка #%d", form, scope, next.ID)

	switch in.Action {
	case "submit", "approve":
		if next.Status == CardOnApproval {
			s.notifyCard(owners[next.StepCode], next,
				in.Action,
				fmt.Sprintf("Планы: %s · шаг %s · %s", identity, next.StepCode, actionNotificationLabel(in.Action)),
				fmt.Sprintf("%s: %s на шаг %s (%s). Проверьте форму, согласуйте или верните.", identity, actionNotificationLabel(in.Action), next.StepCode, title),
				link)
			return
		}
		// Дальше идти некуда — маршрут пройден.
		if next.Status == CardApproved || next.Status == CardPublished {
			recipient := prev.CreatedBy
			if sales {
				recipient = owners[next.StepCode]
			}
			s.notifyCard(recipient, next,
				in.Action,
				fmt.Sprintf("Планы: %s · шаг %s · согласовано", identity, next.StepCode),
				fmt.Sprintf("%s: согласовано на шаге %s%s.", identity, next.StepCode, publishedSuffix(next.Status)),
				link)
		}
	case "return":
		// Возврат: владелец целевого шага плюс автор карточки. Владелец шага
		// заполнения часто не назначен (заполняют по заданиям, а не по этапу), и
		// без автора о возврате не узнавал НИКТО — форма просто зависала.
		ids := recipients(owners[next.StepCode], prev.CreatedBy)
		if sales {
			ids = recipients(owners[next.StepCode])
		}
		for _, uid := range ids {
			s.notifyCard(uid, next,
				in.Action,
				fmt.Sprintf("Планы: %s · шаг %s · возвращено", identity, next.StepCode),
				fmt.Sprintf("%s: возвращено на шаг %s%s.", identity, next.StepCode, reasonSuffix(in.Comment)),
				link)
		}
	case "reopen":
		for _, uid := range recipients(owners[next.StepCode], prev.CreatedBy) {
			s.notifyCard(uid, next,
				in.Action,
				"Планы: период переоткрыт",
				fmt.Sprintf("«%s» открыта на запись заново%s.", title, reasonSuffix(in.Comment)),
				link)
		}
	}
}

func actionNotificationLabel(action string) string {
	if action == "approve" {
		return "согласовано"
	}
	return "отправлено"
}

// recipients — получатели без нулей и дублей: один человек не должен получить
// два одинаковых уведомления, если он и владелец шага, и автор карточки.
func recipients(ids ...int64) []int64 {
	seen := map[int64]bool{}
	out := make([]int64, 0, len(ids))
	for _, id := range ids {
		if id == 0 || seen[id] {
			continue
		}
		seen[id] = true
		out = append(out, id)
	}
	return out
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
