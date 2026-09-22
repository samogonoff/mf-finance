package plans

import "testing"

// Задание этапа заполнения значит «заполни форму», и его статус — следствие
// состояния карточки, а не отдельная воля человека. Проверяем чистые функции
// соответствия: живой БД для этого не нужно.

// Розница: задание на страну покрывает карточку той же страны и только её.
func TestCardsCoveredByTask_RetailByCountry(t *testing.T) {
	cards := []Card{
		{FormCode: TemplateRetail, ScopeKey: "BY", Title: "Розница BY"},
		{FormCode: TemplateRetail, ScopeKey: "RU", Title: "Розница RU"},
		{FormCode: TemplateMP, ScopeKey: "large", Title: "МП large"},
	}
	got := cardsCoveredByTask(cards, TemplateRetail, CfoFilter{Country: "BY", GroupCFO1: "Розница"})
	if len(got) != 1 || got[0].ScopeKey != "BY" {
		t.Fatalf("задание по РБ должно покрывать одну карточку BY: %+v", got)
	}
}

// МП: одно задание на все площадки (segment=all) покрывает обе карточки —
// объединённая форма ведёт крупные и мелкие сразу.
func TestCardsCoveredByTask_MpAllSegments(t *testing.T) {
	cards := []Card{
		{FormCode: TemplateMP, ScopeKey: "large"},
		{FormCode: TemplateMP, ScopeKey: "small"},
		{FormCode: TemplateRetail, ScopeKey: "BY"},
	}
	got := cardsCoveredByTask(cards, TemplateMP, CfoFilter{Segment: "all"})
	if len(got) != 2 {
		t.Fatalf("задание МП должно покрывать обе карточки: %+v", got)
	}
	one := cardsCoveredByTask(cards, TemplateMP, CfoFilter{Segment: "large"})
	if len(one) != 1 || one[0].ScopeKey != "large" {
		t.Fatalf("задание по сегменту покрывает только его карточку: %+v", one)
	}
}

// Форма отправлена — задание выполнено.
func TestFillTaskStatus_SubmittedMeansDone(t *testing.T) {
	covered := []Card{{Status: CardOnApproval, StepCode: "1.2"}}
	if got := fillTaskStatus(covered); got != TaskDone {
		t.Fatalf("отправленная форма закрывает задание: got %q, want %q", got, TaskDone)
	}
}

// Форма ещё заполняется — статус задания НЕ трогаем: там своя жизнь (взял в
// работу, делегировал, сдал на проверку), и перезаписывать её нельзя.
func TestFillTaskStatus_DraftKeepsHumanStatus(t *testing.T) {
	covered := []Card{{Status: CardDraft, StepCode: "1.1"}}
	if got := fillTaskStatus(covered); got != "" {
		t.Fatalf("для черновика статус задания меняться не должен, got %q", got)
	}
}

// Одна из покрытых карточек не отправлена — задание не закрываем.
func TestFillTaskStatus_PartialSubmitKeepsTaskOpen(t *testing.T) {
	covered := []Card{
		{Status: CardOnApproval, StepCode: "1.2"},
		{Status: CardDraft, StepCode: "1.1"},
	}
	if got := fillTaskStatus(covered); got != "" {
		t.Fatalf("пока не все формы отправлены, задание открыто: got %q", got)
	}
}

// Возврат формы возвращает задание в работу — человеку снова заполнять.
func TestFillTaskStatus_ReturnedCardReopensTask(t *testing.T) {
	covered := []Card{{Status: CardReturned, StepCode: "1.1"}}
	if got := fillTaskStatus(covered); got != TaskReturned {
		t.Fatalf("возврат формы: got %q, want %q", got, TaskReturned)
	}
	// Возврат важнее «всё отправлено»: если одна форма вернулась, а вторая ушла
	// дальше, задание всё равно требует работы.
	mixed := []Card{{Status: CardReturned, StepCode: "1.1"}, {Status: CardApproved, StepCode: "1.4"}}
	if got := fillTaskStatus(mixed); got != TaskReturned {
		t.Fatalf("смешанное состояние: got %q, want %q", got, TaskReturned)
	}
}

// Утверждённая форма тоже закрывает задание: шаг заполнения давно пройден.
func TestFillTaskStatus_ApprovedMeansDone(t *testing.T) {
	if got := fillTaskStatus([]Card{{Status: CardPublished, StepCode: "1.4"}}); got != TaskDone {
		t.Fatalf("опубликованная форма закрывает задание: got %q", got)
	}
}
