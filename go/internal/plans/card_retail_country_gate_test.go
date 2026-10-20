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

func countryGateFixtureAt(step string) (*CardService, *countryCardStore) {
	own := Card{ID: 1, PlID: 7, FormCode: TemplateRetail, ScopeKey: "BY", Country: "BY", Status: CardOnApproval, StepCode: step}
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

func countryGateFixture() (*CardService, *countryCardStore) { return countryGateFixtureAt("1.3") }

func addOtherCountries(store *countryCardStore, step string, countries ...string) {
	for i, country := range countries {
		store.period = append(store.period, Card{ID: int64(i + 2), PlID: 7, FormCode: TemplateRetail,
			ScopeKey: country, Country: country, Status: CardOnApproval, StepCode: step})
	}
}

// Директор принимает свою страну независимо от остальных: ждать их незачем, Сипарова увидит розницу,
// когда страны соберутся.
func TestRetailCountryGateDoesNotBlockDirector(t *testing.T) {
	svc, store := countryGateFixture()
	got, err := svc.Action(context.Background(), 1, CardActionInput{Action: "approve", ActorID: 8})
	if err != nil || got.StepCode != "1.4" || store.card.StepCode != "1.4" {
		t.Fatalf("директор должен принять страну, не дожидаясь остальных: %+v %v", got, err)
	}
}

// Решение шага 1.4 (утвердить или вернуть) — только когда все четыре страны дошли до этого шага.
func TestRetailCountryGateBlocksFinalDecisionUntilAllCountriesReady(t *testing.T) {
	for _, in := range []CardActionInput{
		{Action: "approve", ActorID: 9},
		{Action: "return", ActorID: 9, TargetStep: "1.1", Comment: "пересчитать"},
	} {
		svc, store := countryGateFixtureAt("1.4")
		addOtherCountries(store, "1.4", "RU")
		addOtherCountries(store, "1.3", "KZ", "UZ") // KZ и UZ ещё у директора
		store.period[2].ID, store.period[3].ID = 3, 4
		_, err := svc.Action(context.Background(), 1, in)
		if err == nil || !strings.Contains(err.Error(), "KZ, UZ") || !strings.Contains(err.Error(), "Сипарова") {
			t.Fatalf("%s: ждали отказ с перечнем KZ, UZ: %v", in.Action, err)
		}
		if store.card.StepCode != "1.4" || store.card.Status != CardOnApproval {
			t.Fatalf("%s: отказ не должен менять карточку: %+v", in.Action, store.card)
		}
	}
}

func TestRetailCountryGateAllowsFinalDecisionWhenAllReady(t *testing.T) {
	svc, store := countryGateFixtureAt("1.4")
	for i, country := range []string{"RU", "KZ", "UZ"} {
		store.period = append(store.period, Card{ID: int64(i + 2), PlID: 7, FormCode: TemplateRetail,
			ScopeKey: country, Country: country, Status: CardOnApproval, StepCode: "1.4"})
	}
	got, err := svc.Action(context.Background(), 1, CardActionInput{Action: "approve", ActorID: 9})
	if err != nil || got.Status != CardApproved {
		t.Fatalf("четыре страны на шаге 1.4 — утверждение должно пройти: %+v %v", got, err)
	}
	// Уже утверждённая страна остаётся «готовой» для остальных: первая утверждённая не блокирует вторую.
	store.period[0].Status = CardApproved
	store.card = store.period[1]
	if _, err := svc.Action(context.Background(), 2, CardActionInput{Action: "approve", ActorID: 9}); err != nil {
		t.Fatalf("вторая страна после утверждения первой: %v", err)
	}
}

func TestRetailCountryGateCanBeDisabledForTestRun(t *testing.T) {
	svc, _ := countryGateFixtureAt("1.4")
	svc.WithRetailCountryGate(false)
	got, err := svc.Action(context.Background(), 1, CardActionInput{Action: "approve", ActorID: 9})
	if err != nil || got.Status != CardApproved {
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
	svc, store := countryGateFixtureAt("1.4")
	store.card.FormCode = TemplateMP
	store.period = nil
	got, err := svc.Action(context.Background(), 1, CardActionInput{Action: "approve", ActorID: 9})
	if err != nil || got.Status != CardApproved {
		t.Fatalf("MP should be independent: %+v %v", got, err)
	}
}

// Страна, возвращённая с 1.4 на доработку, снова считается «не готовой»: остальные ждут её возвращения.
func TestRetailCountryGateReturnMakesCountryMissingAgain(t *testing.T) {
	svc, store := countryGateFixtureAt("1.4")
	for i, country := range []string{"RU", "KZ", "UZ"} {
		store.period = append(store.period, Card{ID: int64(i + 2), FormCode: TemplateRetail,
			Country: country, Status: CardOnApproval, StepCode: "1.4"})
	}
	store.period[2].Status, store.period[2].StepCode = CardReturned, "1.1"
	_, err := svc.Action(context.Background(), 1, CardActionInput{Action: "approve", ActorID: 9})
	if err == nil || !strings.Contains(err.Error(), "KZ") {
		t.Fatalf("returned country must block: %v", err)
	}
}

func TestMissingRetailCountries(t *testing.T) {
	cards := []Card{
		{ID: 1, FormCode: TemplateRetail, Country: "BY", Status: CardOnApproval, StepCode: "1.4"},
		{ID: 2, FormCode: TemplateRetail, Country: "RU", Status: CardOnApproval, StepCode: "1.3"},
		{ID: 3, FormCode: TemplateRetail, Country: "KZ", Status: CardApproved, StepCode: "1.4"},
		{ID: 4, FormCode: TemplateMP, Country: "UZ", Status: CardOnApproval, StepCode: "1.4"}, // не розница
	}
	got := missingRetailCountries(cards, 0, false)
	if strings.Join(got, ",") != "RU,UZ" {
		t.Fatalf("не дошли до 1.4: %v", got)
	}
}

// На своде решение шага 1.4 доступно согласующему этого шага и админу процессов; вернуть можно на шаги раньше 1.4.
func TestRetailConsolidatedFinalStepRights(t *testing.T) {
	cards, _ := countryGateFixtureAt("1.4")
	svc := NewRetailService(nil, cards, nil)
	c := Card{ID: 1, PlID: 7, FormCode: TemplateRetail, ScopeKey: "BY", Country: "BY", Status: CardOnApproval, StepCode: "1.4"}

	if !svc.canDecideFinal(context.Background(), Principal{UserID: 9}, c) {
		t.Fatal("согласующий шага 1.4 должен видеть кнопки")
	}
	if svc.canDecideFinal(context.Background(), Principal{UserID: 8}, c) {
		t.Fatal("директору (шаг 1.3) решение шага 1.4 недоступно")
	}
	if !svc.canDecideFinal(context.Background(), Principal{UserID: 5, PlansAdmin: true}, c) {
		t.Fatal("админ процессов может решать")
	}
	if svc.canDecideFinal(context.Background(), Principal{}, c) {
		t.Fatal("без пользователя решение недоступно")
	}

	var codes []string
	for _, st := range svc.retailReturnSteps(context.Background()) {
		codes = append(codes, st.Code)
	}
	if strings.Join(codes, ",") != "1.1,fin,1.3" {
		t.Fatalf("шаги возврата: %v", codes)
	}
}

// Выключатель шлюза действует и на своде: при PLANS_RETAIL_COUNTRY_GATE=0 ждать некого.
func TestRetailPendingRespectsGateSwitch(t *testing.T) {
	cards, _ := countryGateFixtureAt("1.4")
	svc := NewRetailService(nil, cards, nil)
	period := []Card{{ID: 1, FormCode: TemplateRetail, Country: "BY", Status: CardOnApproval, StepCode: "1.4"}}
	if got := svc.retailPending(period); strings.Join(got, ",") != "RU,KZ,UZ" {
		t.Fatalf("шлюз включён: %v", got)
	}
	cards.WithRetailCountryGate(false)
	if got := svc.retailPending(period); got == nil || len(got) != 0 {
		t.Fatalf("шлюз выключен — ждать некого, список пуст (не nil): %v", got)
	}
}
