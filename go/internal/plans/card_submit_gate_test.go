package plans

import (
	"context"
	"errors"
	"strings"
	"testing"
)

// Гейт отправки: карточка не уходит на согласование, если форма не проходит
// блокирующие проверки. До гейта проверки жили только в интерфейсе — форму можно
// было отправить запросом мимо него, и согласующий обязан был её вернуть.

// stubGate — управляемый гейт.
type stubGate struct {
	reasons []string
	err     error
	calls   int
}

func (g *stubGate) CanSubmit(context.Context, Card) ([]string, error) {
	g.calls++
	return g.reasons, g.err
}

func gateFixture(g SubmitGate) (*CardService, *fakeCardStore) {
	cards := &fakeCardStore{card: Card{
		ID: 42, PlID: 7, FormCode: TemplateRetail, ScopeKey: "BY", Title: "Розница BY",
		Country: "BY", Status: CardDraft, StepCode: "1.1", CurrentVersion: 1,
	}}
	svc := NewCardService(cards)
	svc.cards = cards
	if g != nil {
		svc.WithSubmitGate(TemplateRetail, g)
	}
	// Маршрут: заполнение → руководитель → утверждение.
	cards.route = []RouteStep{
		{StepCode: "1.1", SortOrder: 10, Kind: StepFill, Enabled: true},
		{StepCode: "1.2", SortOrder: 20, Kind: StepApprove, Enabled: true},
		{StepCode: "1.4", SortOrder: 30, Kind: StepFinal, Enabled: true},
	}
	return svc, cards
}

func TestSubmitGate_BlocksSubmitWithReasons(t *testing.T) {
	g := &stubGate{reasons: []string{"V-01: не заполнен план по 12 магазинам", "V-05: сверка R-01 не сошлась"}}
	svc, cards := gateFixture(g)

	_, err := svc.Action(context.Background(), 42, CardActionInput{Action: "submit", ActorID: 7})
	if err == nil {
		t.Fatal("отправка с блокирующими замечаниями должна быть отклонена")
	}
	var blocked *SubmitBlockedError
	if !errors.As(err, &blocked) {
		t.Fatalf("ожидался SubmitBlockedError, получено %T (%v)", err, err)
	}
	if len(blocked.Reasons) != 2 {
		t.Fatalf("причины должны дойти до вызывающего: %+v", blocked.Reasons)
	}
	if !strings.Contains(blocked.Error(), "V-01") {
		t.Fatalf("в тексте должны быть коды замечаний: %q", blocked.Error())
	}
	if cards.marked {
		t.Fatal("карточка не должна меняться при отказе гейта")
	}
}

// Замечаний нет — отправка проходит.
func TestSubmitGate_AllowsCleanForm(t *testing.T) {
	g := &stubGate{}
	svc, _ := gateFixture(g)

	if _, err := svc.Action(context.Background(), 42, CardActionInput{Action: "submit", ActorID: 7}); err != nil {
		t.Fatalf("чистая форма должна отправляться: %v", err)
	}
	if g.calls != 1 {
		t.Fatalf("гейт должен вызываться ровно раз, вызовов %d", g.calls)
	}
}

// Гейт применяется ТОЛЬКО к submit: дальше по маршруту значения заморожены, и
// согласующий решает по существу, а не формальной проверкой.
func TestSubmitGate_NotAppliedToApprove(t *testing.T) {
	g := &stubGate{reasons: []string{"V-01: не заполнено"}}
	svc, cards := gateFixture(g)
	cards.card.Status = CardOnApproval
	cards.card.StepCode = "1.2"

	if _, err := svc.Action(context.Background(), 42, CardActionInput{Action: "approve", ActorID: 8}); err != nil {
		t.Fatalf("согласование не должно упираться в гейт отправки: %v", err)
	}
	if g.calls != 0 {
		t.Fatalf("на approve гейт вызываться не должен, вызовов %d", g.calls)
	}
}

// Проверка не выполнилась (источник факта недоступен) — отправку НЕ блокируем:
// иначе процесс встанет из-за VPN, а не из-за данных.
func TestSubmitGate_UnavailableCheckDoesNotBlock(t *testing.T) {
	g := &stubGate{err: errors.New("OLAP недоступен")}
	svc, _ := gateFixture(g)

	if _, err := svc.Action(context.Background(), 42, CardActionInput{Action: "submit", ActorID: 7}); err != nil {
		t.Fatalf("недоступность проверки не должна блокировать отправку: %v", err)
	}
}

// Формы без гейта работают как раньше — иначе подключение оболочки процесса к
// форме требовало бы сразу написать ей валидации.
func TestSubmitGate_FormWithoutGatePassesThrough(t *testing.T) {
	svc, _ := gateFixture(nil)
	if _, err := svc.Action(context.Background(), 42, CardActionInput{Action: "submit", ActorID: 7}); err != nil {
		t.Fatalf("форма без гейта должна отправляться: %v", err)
	}
}
