package plans

import "testing"

// routeFixture — маршрут как в сиде 0031: 1.1 → [fin выключен] → 1.2 → 1.3 → 1.4.
func routeFixture(finEnabled, skipSame bool) []RouteStep {
	return []RouteStep{
		{StepCode: "1.1", StepName: "Заполнение", SortOrder: 10, Kind: StepFill, Enabled: true},
		{StepCode: "fin", StepName: "Финансист", SortOrder: 20, Kind: StepApprove, Enabled: finEnabled},
		{StepCode: "1.2", StepName: "Руководитель", SortOrder: 30, Kind: StepApprove, Enabled: true, SkipIfSameUser: skipSame},
		{StepCode: "1.3", StepName: "Директор", SortOrder: 40, Kind: StepApprove, Enabled: true},
		{StepCode: "1.4", StepName: "Утверждение", SortOrder: 50, Kind: StepFinal, Enabled: true, PublishOnApprove: true},
	}
}

func TestCardRoute_ForwardWithoutFinancier(t *testing.T) {
	c := Card{Status: CardDraft, StepCode: "1.1"}
	route := routeFixture(false, false)

	tr, err := applyCardAction(c, route, CardActionInput{Action: "submit", ActorID: 7})
	if err != nil {
		t.Fatalf("submit: %v", err)
	}
	// Шаг «Финансист» выключен → сразу 1.2.
	if tr.Status != CardOnApproval || tr.StepCode != "1.2" {
		t.Fatalf("после отправки ожидался on_approval на 1.2, получено %s/%s", tr.Status, tr.StepCode)
	}
	if CardStatusLabel(tr.Status, tr.StepCode) != "on_approval_1.2" {
		t.Fatalf("метка статуса: %s", CardStatusLabel(tr.Status, tr.StepCode))
	}
}

// Шаг «Финансист» включается данными, без правки кода (ТЗ МП §2.1).
func TestCardRoute_FinancierStepEnabledByData(t *testing.T) {
	c := Card{Status: CardDraft, StepCode: "1.1"}
	tr, err := applyCardAction(c, routeFixture(true, false), CardActionInput{Action: "submit", ActorID: 7})
	if err != nil {
		t.Fatalf("submit: %v", err)
	}
	if tr.StepCode != "fin" {
		t.Fatalf("ожидался шаг финансиста, получен %s", tr.StepCode)
	}
}

// Вырожденное самосогласование: 1.2 проходится автоматически, если согласующий —
// тот же человек (ТЗ Розница §2.1, skip_if_same_user).
func TestCardRoute_SkipSelfApproval(t *testing.T) {
	c := Card{Status: CardDraft, StepCode: "1.1"}
	in := CardActionInput{Action: "submit", ActorID: 42, StepOwner: map[string]int64{"1.2": 42, "1.3": 99}}
	tr, err := applyCardAction(c, routeFixture(false, true), in)
	if err != nil {
		t.Fatalf("submit: %v", err)
	}
	if tr.StepCode != "1.3" {
		t.Fatalf("шаг 1.2 должен быть пропущен, карточка на %s", tr.StepCode)
	}
	var skipped bool
	for _, a := range tr.Approvals {
		if a.StepCode == "1.2" && a.Decision == "auto_skipped" {
			skipped = true
		}
	}
	if !skipped {
		t.Fatalf("пропуск шага должен попасть в лист согласования: %+v", tr.Approvals)
	}
}

// Другой согласующий на 1.2 — шаг не пропускается.
func TestCardRoute_SkipOnlyForSameUser(t *testing.T) {
	c := Card{Status: CardDraft, StepCode: "1.1"}
	in := CardActionInput{Action: "submit", ActorID: 42, StepOwner: map[string]int64{"1.2": 7}}
	tr, _ := applyCardAction(c, routeFixture(false, true), in)
	if tr.StepCode != "1.2" {
		t.Fatalf("шаг 1.2 не должен пропускаться для другого согласующего, карточка на %s", tr.StepCode)
	}
}

func TestCardRoute_FinalApproveLocksAndPublishes(t *testing.T) {
	c := Card{Status: CardOnApproval, StepCode: "1.4"}
	tr, err := applyCardAction(c, routeFixture(false, false), CardActionInput{Action: "approve", ActorID: 5})
	if err != nil {
		t.Fatalf("approve 1.4: %v", err)
	}
	if tr.Status != CardApproved || !tr.Locked || !tr.Publish {
		t.Fatalf("утверждение должно закрыть карточку и триггернуть публикацию: %+v", tr)
	}
	if cardEditable(Card{Status: CardApproved, Locked: true}) {
		t.Fatal("после утверждения запись должна быть закрыта для всех")
	}
}

func TestCardRoute_ReturnRules(t *testing.T) {
	route := routeFixture(false, false)
	c := Card{Status: CardOnApproval, StepCode: "1.3"}

	if _, err := applyCardAction(c, route, CardActionInput{Action: "return", TargetStep: "1.1"}); err == nil {
		t.Fatal("возврат без комментария должен быть отклонён")
	}
	if _, err := applyCardAction(c, route, CardActionInput{Action: "return", Comment: "переделать"}); err == nil {
		t.Fatal("возврат без целевого шага должен быть отклонён")
	}
	if _, err := applyCardAction(c, route, CardActionInput{Action: "return", TargetStep: "1.4", Comment: "вперёд"}); err == nil {
		t.Fatal("возврат «вперёд» должен быть отклонён")
	}
	tr, err := applyCardAction(c, route, CardActionInput{Action: "return", TargetStep: "1.1", Comment: "нет обоснования долей"})
	if err != nil {
		t.Fatalf("return: %v", err)
	}
	if tr.Status != CardReturned || tr.StepCode != "1.1" || tr.RevokeFrom != "1.1" {
		t.Fatalf("возврат: %+v", tr)
	}
	if !cardEditable(Card{Status: CardReturned}) {
		t.Fatal("после возврата исполнитель должен получить право правки")
	}
}

func TestCardRoute_ReopenRequiresReason(t *testing.T) {
	route := routeFixture(false, false)
	approved := Card{Status: CardApproved, StepCode: "1.4", Locked: true}

	if _, err := applyCardAction(approved, route, CardActionInput{Action: "reopen"}); err == nil {
		t.Fatal("reopen без причины должен быть отклонён")
	}
	if _, err := applyCardAction(Card{Status: CardDraft, StepCode: "1.1"}, route,
		CardActionInput{Action: "reopen", Comment: "просто"}); err == nil {
		t.Fatal("reopen черновика бессмысленен и должен быть отклонён")
	}
	tr, err := applyCardAction(approved, route, CardActionInput{Action: "reopen", Comment: "корректировка по решению финблока"})
	if err != nil {
		t.Fatalf("reopen: %v", err)
	}
	if tr.Status != CardDraft || tr.StepCode != "1.1" || tr.Locked {
		t.Fatalf("reopen должен вернуть карточку в работу с первого шага: %+v", tr)
	}
}

// Утверждённую карточку нельзя двигать вперёд в обход reopen.
func TestCardRoute_ApprovedIsClosed(t *testing.T) {
	c := Card{Status: CardApproved, StepCode: "1.4", Locked: true}
	if _, err := applyCardAction(c, routeFixture(false, false), CardActionInput{Action: "approve"}); err == nil {
		t.Fatal("approve утверждённой карточки должен быть отклонён")
	}
}
