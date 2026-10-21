package plans

import (
	"context"
	"fmt"
	"strings"
)

// Готовность к этапу 1.6 «Формирование по ЮЛ и каналам». Этап собирает свод по ЮЛ, поэтому его нельзя закрывать, пока
// формы, из которых свод собран, не утверждены. Блокирующие замечания не дают двигать этап; предупреждения показываются,
// но не держат.

// Уровни замечаний готовности.
const (
	LeIssueBlock = "block"
	LeIssueWarn  = "warn"
)

// LeIssue — замечание готовности.
type LeIssue struct {
	Level   string `json:"level"`
	Code    string `json:"code"`
	Message string `json:"message"`
}

// leCardClosed — карточка формы утверждена (или уже опубликована): её цифры не изменятся без возврата.
func leCardClosed(c Card) bool {
	return c.Status == CardApproved || c.Status == CardPublished
}

// leListTitles — до limit названий и «и ещё N».
func leListTitles(titles []string, limit int) string {
	if len(titles) <= limit {
		return strings.Join(titles, "; ")
	}
	return strings.Join(titles[:limit], "; ") + fmt.Sprintf(" и ещё %d", len(titles)-limit)
}

// leReadiness — чистая проверка готовности: карточки периода и собранный свод.
func leReadiness(cards []Card, svod LeSvod) []LeIssue {
	var out []LeIssue
	var salesOpen, expOpen, failed []string
	for _, c := range cards {
		switch {
		case c.FormCode == TemplateRetail || c.FormCode == TemplateMP:
			if !leCardClosed(c) {
				salesOpen = append(salesOpen, c.Title+" — "+CardStatusLabel(c.Status, c.StepCode))
			}
		case c.FormCode == TemplateExp && !strings.HasPrefix(c.ScopeKey, "CFO-"):
			if !leCardClosed(c) {
				expOpen = append(expOpen, c.Title+" — "+CardStatusLabel(c.Status, c.StepCode))
			}
		}
		if c.Status == CardPublishFailed {
			failed = append(failed, c.Title)
		}
	}
	if len(salesOpen) > 0 {
		out = append(out, LeIssue{Level: LeIssueBlock, Code: "sales_not_approved",
			Message: "Продажи не утверждены (этап 1.4): " + leListTitles(salesOpen, 4)})
	}
	if len(expOpen) > 0 {
		out = append(out, LeIssue{Level: LeIssueBlock, Code: "expenses_not_approved",
			Message: "Расходы не утверждены (этап 1.5): " + leListTitles(expOpen, 4)})
	}
	if len(failed) > 0 {
		out = append(out, LeIssue{Level: LeIssueWarn, Code: "publish_failed",
			Message: "Публикация не прошла: " + leListTitles(failed, 4)})
	}
	for _, w := range svod.Warnings {
		out = append(out, LeIssue{Level: LeIssueWarn, Code: "svod_warning", Message: w})
	}
	for _, r := range svod.Rows {
		if r.Section == LeSectionTotal && r.Key == leKeyExpTotal {
			if v := r.Total[leNoEntity]; v != 0 {
				out = append(out, LeIssue{Level: LeIssueWarn, Code: "no_legal_entity",
					Message: fmt.Sprintf("Расходы без юридического лица (у ЦФО не указана компания): %.0f BYN. Колонка «%s».", v, leNoEntity)})
			}
		}
		if r.Section == LeSectionTotal && r.Key == leKeySalesTotal {
			if v := r.Total[leNoEntity]; v != 0 {
				out = append(out, LeIssue{Level: LeIssueWarn, Code: "no_legal_entity_sales",
					Message: fmt.Sprintf("Продажи без юридического лица: %.0f BYN. Колонка «%s».", v, leNoEntity)})
			}
		}
	}
	hasSales := false
	for _, r := range svod.Rows {
		if r.Key == leKeySalesTotal && r.TotAll != 0 {
			hasSales = true
		}
	}
	if !hasSales {
		out = append(out, LeIssue{Level: LeIssueWarn, Code: "no_sales", Message: "В своде нет продаж: тактика продаж не сохранена или не прочитана."})
	}
	return out
}

// leBlockers — только блокирующие замечания.
func leBlockers(issues []LeIssue) []LeIssue {
	var b []LeIssue
	for _, i := range issues {
		if i.Level == LeIssueBlock {
			b = append(b, i)
		}
	}
	return b
}

// Gate — проверка перед движением этапа (подключается в Service.WithStageGate). Этап 1.6 нельзя отправить или
// согласовать, пока есть блокирующие замечания; возврат и «взять в работу» не проверяются.
func (s *LeSvodService) Gate(ctx context.Context, plID int64, code, action string) error {
	if code != "1.6" || (action != "submit" && action != "approve") {
		return nil
	}
	cards, err := s.cards.Cards(ctx, plID)
	if err != nil {
		return err
	}
	blockers := leBlockers(leReadiness(cards, LeSvod{}))
	if len(blockers) == 0 {
		return nil
	}
	msgs := make([]string, len(blockers))
	for i, b := range blockers {
		msgs[i] = b.Message
	}
	return fmt.Errorf("этап 1.6 нельзя завершить: %s", strings.Join(msgs, " | "))
}
