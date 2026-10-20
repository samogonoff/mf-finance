package plans

import (
	"context"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

func optRow(cfo, pl int, country string, plan float64) ExpRow {
	return ExpRow{CodeCFO: cfo, CodePL: pl, CFO: "ЦФО", Expense: "статья", CountryCompany: country,
		Values: []ExpValue{
			{Scenario: ExpScenarioPlan, Month: 10, Amount: pv(plan)},
			{Scenario: ExpScenarioFact, Month: 10, Amount: pv(plan / 2)},
		}}
}

// Строки опта раскладываются по странам их ЦФО; строка без известной страны не теряется.
func TestSplitOptRows(t *testing.T) {
	by := splitOptRows([]ExpRow{
		optRow(35, 24, "BY", 1), optRow(254, 24, "by", 1), optRow(303, 24, "RU", 1),
		optRow(414, 24, "UZ", 1), optRow(999, 24, "", 1), optRow(998, 24, "KZ", 1),
	})
	if len(by["BY"]) != 2 || len(by["RU"]) != 1 || len(by["UZ"]) != 1 || len(by["—"]) != 2 {
		t.Fatalf("раскладка по странам: BY=%d RU=%d UZ=%d прочие=%d", len(by["BY"]), len(by["RU"]), len(by["UZ"]), len(by["—"]))
	}
}

// Свод опта: колонки — страны, суммы уже в BYN (курсов нет), итог — сумма стран.
func TestBuildExpConsolidated_OptByCountry(t *testing.T) {
	card := Card{ID: 5, Title: "Опт", Year: 2026, Month: 10}
	inst := ExpInstance{ID: 9, Currency: "BYN"}
	by := splitOptRows([]ExpRow{optRow(35, 24, "BY", 100), optRow(303, 24, "RU", 200), optRow(414, 54, "UZ", 300)})
	var in []expConsInput
	for _, c := range expOptCountries {
		in = append(in, expConsInput{Country: c, Card: card, Inst: inst, Rows: by[c], Fx: sameExpFx(1)})
	}
	out := buildExpConsolidated(7, 2026, 10, ExpWindowMonth, in, expRetailPnlByCode(), expRetailPnlOrder)
	if !near(out.Total.PlanTotal, 600) || !near(out.Total.Plan["BY"], 100) || !near(out.Total.Plan["RU"], 200) || !near(out.Total.Plan["UZ"], 300) {
		t.Fatalf("итоги по странам: %+v", out.Total)
	}
	if !near(out.Total.FactCur, 0) || !near(out.Total.FactLY, 300) {
		t.Errorf("факт того же месяца прошлого года должен быть 300 (половина плана), получили %v", out.Total.FactLY)
	}
	if len(out.Countries) != 3 || out.Countries[0].Country != "BY" || out.Countries[2].Country != "UZ" {
		t.Errorf("колонки стран: %+v", out.Countries)
	}
	if len(out.Groups) < 2 || out.Groups[0].Group != "Заработная плата" || out.Groups[1].Group != "Аренда" {
		t.Errorf("группы в порядке листа Excel (ЗП, Аренда): %+v", out.Groups)
	}
}

type denyScope struct{ rules []UserScope }

func (d denyScope) UserCodeCFOs(context.Context, int64) ([]int, error) { return nil, nil }
func (d denyScope) UpsertScope(context.Context, UserScope) error       { return nil }
func (d denyScope) UserScopes(context.Context, int64) ([]UserScope, error) {
	return d.rules, nil
}

// Сервис: свод ИМ и опта собирается из карточек периода; свод опта закрыт тем, у кого нет доступа ко
// всем трём странам; неизвестный канал и период без карточек дают понятную ошибку.
func TestExpService_ChannelConsolidated_PG(t *testing.T) {
	dsn := os.Getenv("PLANS_TEST_PG_DSN")
	if dsn == "" {
		t.Skip("PLANS_TEST_PG_DSN not set")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 40*time.Second)
	defer cancel()
	pool, err := pgxpool.New(ctx, dsn)
	if err != nil {
		t.Fatal(err)
	}
	defer pool.Close()
	pl, err := NewPgStore(pool).EnsureInstance(ctx, 2096, 10)
	if err != nil {
		t.Fatal(err)
	}
	defer pool.Exec(ctx, `DELETE FROM pl_instance WHERE id=$1`, pl)
	defer pool.Exec(ctx, `DELETE FROM form_card WHERE pl_id=$1 AND form_code=$2`, pl, TemplateExp)

	cardsSvc := NewCardService(NewCardStore(pool))
	cards, err := cardsSvc.EnsureCards(ctx, pl, 0)
	if err != nil {
		t.Fatal(err)
	}
	repo := NewExpRepo(pool)
	svc := NewExpService(repo, cardsSvc, nil)

	var instIDs []int64
	defer func() {
		for _, id := range instIDs {
			pool.Exec(context.Background(), `DELETE FROM exp_instance WHERE id=$1`, id)
		}
	}()
	seed := func(scopeKey string, rows []ExpRow) {
		for _, c := range cards {
			if c.FormCode != TemplateExp || c.ScopeKey != scopeKey {
				continue
			}
			kind, country := expKindOf(c.ScopeKey)
			inst, err := repo.EnsureInstance(ctx, ExpInstance{CardID: c.ID, FormKind: kind, Country: country, PlanYear: 2096, Currency: c.Currency}, 0)
			if err != nil {
				t.Fatal(err)
			}
			instIDs = append(instIDs, inst.ID)
			if _, err := repo.SyncRows(ctx, inst.ID, rows); err != nil {
				t.Fatal(err)
			}
			amt := func(v float64) *float64 { return &v }
			var cells []ExpCell
			for _, r := range rows {
				cells = append(cells, ExpCell{CodeCFO: r.CodeCFO, CodePL: r.CodePL, Month: 10, Amount: amt(100)})
			}
			if _, err := repo.SaveValues(ctx, inst.ID, cells, 0, 0); err != nil {
				t.Fatal(err)
			}
			return
		}
		t.Fatalf("нет карточки %s", scopeKey)
	}
	seed("OPT", []ExpRow{
		{CodeCFO: 35, CodePL: 24, Expense: "ЗП", CountryCompany: "BY"},
		{CodeCFO: 303, CodePL: 24, Expense: "ЗП", CountryCompany: "RU"},
	})
	seed("IM-BY", []ExpRow{{CodeCFO: 380, CodePL: 24, Expense: "ЗП", CountryCompany: "BY"}})

	admin := Principal{UserID: 1, PlansAdmin: true}
	opt, err := svc.ChannelConsolidated(ctx, admin, pl, ExpChannelOpt, ExpWindowMonth)
	if err != nil {
		t.Fatalf("свод опта: %v", err)
	}
	if opt.Channel != ExpChannelOpt || len(opt.Countries) != 3 || len(opt.Groups) == 0 {
		t.Fatalf("свод опта: канал=%q, стран %d, групп %d", opt.Channel, len(opt.Countries), len(opt.Groups))
	}
	if !near(opt.Total.Plan["BY"], 100) || !near(opt.Total.Plan["RU"], 100) || !near(opt.Total.PlanTotal, 200) {
		t.Errorf("план опта по странам: %+v", opt.Total)
	}
	im, err := svc.ChannelConsolidated(ctx, admin, pl, ExpChannelIM, ExpWindowMonth)
	if err != nil || im.Channel != ExpChannelIM || len(im.Countries) != 1 || !near(im.Total.PlanTotal, 100) {
		t.Fatalf("свод ИМ: %v, %+v", err, im.Total)
	}
	if _, err := svc.ChannelConsolidated(ctx, admin, pl, "xxx", ""); err == nil {
		t.Error("неизвестный канал должен давать ошибку")
	}

	// Исполнитель розницы KZ не видит свод опта; тот, у кого допуск ко всем трём странам, — видит.
	svc.WithScope(denyScope{rules: []UserScope{{Country: "KZ", LegalEntity: "MFKaz"}}})
	_, err = svc.ChannelConsolidated(ctx, Principal{UserID: 2}, pl, ExpChannelOpt, ExpWindowMonth)
	if err == nil || !strings.Contains(err.Error(), "среза доступа") {
		t.Fatalf("свод опта должен быть закрыт срезом доступа: %v", err)
	}
	svc.WithScope(denyScope{rules: []UserScope{{Country: "BY"}, {Country: "RU"}, {Country: "UZ"}}})
	if _, err := svc.ChannelConsolidated(ctx, Principal{UserID: 3}, pl, ExpChannelOpt, ExpWindowMonth); err != nil {
		t.Fatalf("допущенный ко всем странам должен видеть свод опта: %v", err)
	}
}

// Период без карточек канала: понятная ошибка, а не пустой свод.
func TestExpService_ChannelConsolidated_NoCard_PG(t *testing.T) {
	dsn := os.Getenv("PLANS_TEST_PG_DSN")
	if dsn == "" {
		t.Skip("PLANS_TEST_PG_DSN not set")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	pool, err := pgxpool.New(ctx, dsn)
	if err != nil {
		t.Fatal(err)
	}
	defer pool.Close()
	pl, err := NewPgStore(pool).EnsureInstance(ctx, 2095, 3)
	if err != nil {
		t.Fatal(err)
	}
	defer pool.Exec(ctx, `DELETE FROM pl_instance WHERE id=$1`, pl)
	svc := NewExpService(NewExpRepo(pool), NewCardService(NewCardStore(pool)), nil)
	_, err = svc.ChannelConsolidated(ctx, Principal{PlansAdmin: true}, pl, ExpChannelOpt, "")
	if err == nil || !strings.Contains(err.Error(), "нет карточки") {
		t.Fatalf("ожидали ошибку про отсутствие карточки: %v", err)
	}
}
