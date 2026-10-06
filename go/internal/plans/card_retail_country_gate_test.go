package plans

import (
	"context"
	"strings"
	"testing"
)

type countryCardStore struct {
	*fakeCardStore
	period []Card
}

func (s *countryCardStore) Cards(context.Context, int64) ([]Card, error) { return s.period, nil }

func countryGateFixture() (*CardService, *countryCardStore) {
	own := Card{ID: 1, PlID: 7, FormCode: TemplateRetail, ScopeKey: "BY", Country: "BY", Status: CardOnApproval, StepCode: "1.3"}
	store := &countryCardStore{fakeCardStore: &fakeCardStore{card: own, route: []RouteStep{
		{StepCode: "1.1", SortOrder: 10, Kind: StepFill, Enabled: true},
		{StepCode: "fin", SortOrder: 20, Kind: StepApprove, Enabled: true},
		{StepCode: "1.3", SortOrder: 30, Kind: StepApprove, Enabled: true},
		{StepCode: "1.4", SortOrder: 40, Kind: StepFinal, Enabled: true},
	}}, period: []Card{own}}
	return NewCardService(store).WithOwners(ownerStub{owners: map[string]int64{
		"1.1": 7, "fin": 8, "1.3": 8, "1.4": 9,
	}}), store
}

func TestRetailCountryGateBlocksBeforeSiparova(t *testing.T) {
	svc, store := countryGateFixture()
	_, err := svc.Action(context.Background(), 1, CardActionInput{Action: "approve", ActorID: 8})
	if err == nil || !strings.Contains(err.Error(), "RU, KZ, UZ") {
		t.Fatalf("missing countries: %v", err)
	}
	if store.card.StepCode != "1.3" {
		t.Fatal("blocked transition changed card")
	}
}

func TestRetailCountryGateAllowsFourReadyCountries(t *testing.T) {
	svc, store := countryGateFixture()
	for i, country := range []string{"RU", "KZ", "UZ"} {
		store.period = append(store.period, Card{ID: int64(i + 2), PlID: 7, FormCode: TemplateRetail,
			ScopeKey: country, Country: country, Status: CardOnApproval, StepCode: "1.3"})
	}
	got, err := svc.Action(context.Background(), 1, CardActionInput{Action: "approve", ActorID: 8})
	if err != nil || got.StepCode != "1.4" {
		t.Fatalf("four ready countries should enter final step: %+v %v", got, err)
	}
}

func TestRetailCountryGateCanBeDisabledForTestRun(t *testing.T) {
	svc, _ := countryGateFixture()
	svc.WithRetailCountryGate(false)
	got, err := svc.Action(context.Background(), 1, CardActionInput{Action: "approve", ActorID: 8})
	if err != nil || got.StepCode != "1.4" {
		t.Fatalf("test switch: %+v %v", got, err)
	}
}

func TestRetailSubmitRequiresTotalsReview(t *testing.T) {
	svc, store := countryGateFixture()
	store.card.Status, store.card.StepCode = CardDraft, "1.1"
	_, err := svc.Action(context.Background(), 1, CardActionInput{Action: "submit", ActorID: 7})
	if err == nil || !strings.Contains(err.Error(), "подтвердите итоги") {
		t.Fatalf("missing review should block submit: %v", err)
	}
	if store.card.StepCode != "1.1" {
		t.Fatal("unreviewed card was submitted")
	}
}

func TestCountryGateDoesNotBlockMarketplace(t *testing.T) {
	svc, store := countryGateFixture()
	store.card.FormCode = TemplateMP
	store.period = nil
	got, err := svc.Action(context.Background(), 1, CardActionInput{Action: "approve", ActorID: 8})
	if err != nil || got.StepCode != "1.4" {
		t.Fatalf("MP should be independent: %+v %v", got, err)
	}
}

func TestRetailCountryGateReturnMakesCountryMissingAgain(t *testing.T) {
	svc, store := countryGateFixture()
	for i, country := range []string{"RU", "KZ", "UZ"} {
		store.period = append(store.period, Card{ID: int64(i + 2), FormCode: TemplateRetail,
			Country: country, Status: CardOnApproval, StepCode: "1.3"})
	}
	store.period[2].Status = CardReturned
	_, err := svc.Action(context.Background(), 1, CardActionInput{Action: "approve", ActorID: 8})
	if err == nil || !strings.Contains(err.Error(), "KZ") {
		t.Fatalf("returned country must block: %v", err)
	}
}
