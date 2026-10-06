package plans

import "testing"

// Этапы 1.1–1.4 — агрегат состояния КАРТОЧЕК форм. До этого этапы, задания и
// карточки двигались независимо, и таймлайн мог показывать «этап 1.2 готово»,
// когда формы лежали в черновиках, — именно на это наткнулся заказчик.

func syncStages() []StageState {
	return []StageState{
		{Code: "1.1", Status: "pending"},
		{Code: "1.2", Status: "pending", IsApproval: true},
		{Code: "1.3", Status: "pending", IsApproval: true},
		{Code: "1.4", Status: "pending", IsApproval: true},
		// Этап без карточек — движется вручную и пересчётом не затрагивается.
		{Code: "2.1", Status: "pending"},
	}
}

func statusByCode(stages []StageState, code string) string {
	for _, s := range stages {
		if s.Code == code {
			return s.Status
		}
	}
	return ""
}

// Формы в черновиках — этапы согласования НЕ готовы.
func TestStageSync_DraftCardsKeepApprovalStagesOpen(t *testing.T) {
	cards := []Card{
		{Title: "Розница BY", Status: CardDraft, StepCode: "1.1"},
		{Title: "Розница RU", Status: CardDraft, StepCode: "1.1"},
	}
	got := StageStatusFromCards(syncStages(), cards)

	if s := statusByCode(got, "1.1"); s != "in_progress" {
		t.Fatalf("этап 1.1 при черновиках: got %q, want in_progress", s)
	}
	for _, code := range []string{"1.2", "1.3", "1.4"} {
		if s := statusByCode(got, code); s == "completed" {
			t.Fatalf("этап %s не может быть завершён, пока формы в черновиках (got %q)", code, s)
		}
	}
}

// Все формы ушли с шага 1.1 → этап 1.1 завершён, 1.2 в работе.
func TestStageSync_SubmittedCardsCloseFillStage(t *testing.T) {
	cards := []Card{
		{Title: "Розница BY", Status: CardOnApproval, StepCode: "1.2"},
		{Title: "Розница RU", Status: CardOnApproval, StepCode: "1.2"},
	}
	got := StageStatusFromCards(syncStages(), cards)

	if s := statusByCode(got, "1.1"); s != "completed" {
		t.Fatalf("этап 1.1: got %q, want completed", s)
	}
	if s := statusByCode(got, "1.2"); s != "in_progress" {
		t.Fatalf("этап 1.2: got %q, want in_progress", s)
	}
	if s := statusByCode(got, "1.3"); s == "completed" {
		t.Fatal("этап 1.3 завершать нечем: формы до него не дошли")
	}
}

// Одна форма отстала — этап не закрывается: 1.1 остаётся в работе.
func TestStageSync_OneLaggingCardKeepsStageOpen(t *testing.T) {
	cards := []Card{
		{Title: "Розница BY", Status: CardOnApproval, StepCode: "1.3"},
		{Title: "Розница RU", Status: CardDraft, StepCode: "1.1"},
	}
	got := StageStatusFromCards(syncStages(), cards)
	if s := statusByCode(got, "1.1"); s != "in_progress" {
		t.Fatalf("пока одна форма на 1.1, этап 1.1 не завершён: got %q", s)
	}
}

// Возврат виден на таймлайне: «returned» важнее «in_progress», иначе о возврате
// узнает только тот, кто откроет карточку.
func TestStageSync_ReturnedCardMarksStageReturned(t *testing.T) {
	cards := []Card{
		{Title: "Розница BY", Status: CardReturned, StepCode: "1.1"},
		{Title: "Розница RU", Status: CardOnApproval, StepCode: "1.2"},
	}
	got := StageStatusFromCards(syncStages(), cards)
	if s := statusByCode(got, "1.1"); s != "returned" {
		t.Fatalf("этап 1.1 при возврате формы: got %q, want returned", s)
	}
}

// Утверждённые и опубликованные карточки закрывают все шаги маршрута.
func TestStageSync_ApprovedCardsCloseEveryStep(t *testing.T) {
	cards := []Card{
		{Title: "Розница BY", Status: CardPublished, StepCode: "1.4"},
		{Title: "Розница RU", Status: CardApproved, StepCode: "1.4"},
	}
	got := StageStatusFromCards(syncStages(), cards)
	for _, code := range []string{"1.1", "1.2", "1.3", "1.4"} {
		if s := statusByCode(got, code); s != "completed" {
			t.Fatalf("этап %s: got %q, want completed", code, s)
		}
	}
	// Этап производства карточками не управляется — его статус не тронут.
	if s := statusByCode(got, "2.1"); s != "pending" {
		t.Fatalf("этап без карточек не должен пересчитываться: got %q", s)
	}
}

// Карточек нет — пересчёт ничего не меняет (новый период до создания карточек).
func TestStageSync_NoCardsKeepsStages(t *testing.T) {
	before := syncStages()
	got := StageStatusFromCards(before, nil)
	for i := range before {
		if got[i].Status != before[i].Status {
			t.Fatalf("без карточек статусы не должны меняться: %+v", got)
		}
	}
}

// Шаг «Финансист» внутри формы своего этапа не имеет: карточка на шаге fin уже
// прошла 1.1, но до 1.2 не дошла.
func TestStageSync_FinStepBelongsBetweenFillAndApproval(t *testing.T) {
	cards := []Card{{Title: "Розница BY", Status: CardOnApproval, StepCode: "fin"}}
	got := StageStatusFromCards(syncStages(), cards)
	if s := statusByCode(got, "1.1"); s != "completed" {
		t.Fatalf("на шаге fin этап 1.1 уже пройден: got %q", s)
	}
	if s := statusByCode(got, "1.2"); s == "completed" {
		t.Fatal("этап 1.2 ещё не пройден: карточка стоит на fin")
	}
}

// Право финансиста в форме — явный шаг «fin» в правах-срезах, а не любое широкое
// правило: иначе участник процесса с правилом «все шаги» получал бы полномочия
// финансового контролёра (переопределение LFL, правка расчётных статей).
func TestScopeFilter_ExplicitFinStep(t *testing.T) {
	wide := NewScopeFilter(false, []UserScope{{Country: "BY"}}) // правило без шага
	if wide.HasExplicitStep(StepFinCode) {
		t.Fatal("широкое правило не должно давать роль финансиста")
	}
	if !wide.AllowsStep("1.2") {
		t.Fatal("широкое правило по-прежнему разрешает участие в шагах")
	}

	fin := NewScopeFilter(false, []UserScope{{Country: "BY", StageCode: StepFinCode}})
	if !fin.HasExplicitStep(StepFinCode) {
		t.Fatal("явное правило на шаг fin должно давать роль финансиста")
	}
	other := NewScopeFilter(false, []UserScope{{Country: "BY", StageCode: "1.2"}})
	if other.HasExplicitStep(StepFinCode) {
		t.Fatal("правило на другой шаг роль финансиста не даёт")
	}
}

func TestFinancialControllerCardDrivesSalesStage12(t *testing.T) {
	stages := []StageState{{Code: "1.2", Track: "sales", Status: "pending"}}
	cards := []Card{{FormCode: TemplateRetail, Status: CardOnApproval, StepCode: StepFinCode}}
	got := StageStatusFromCards(stages, cards)
	if got[0].Status != "in_progress" {
		t.Fatalf("fin should drive calendar stage 1.2: %+v", got[0])
	}
	cards[0].Status = CardReturned
	got = StageStatusFromCards(stages, cards)
	if got[0].Status != "returned" {
		t.Fatalf("return to fin should appear on calendar: %+v", got[0])
	}
}
