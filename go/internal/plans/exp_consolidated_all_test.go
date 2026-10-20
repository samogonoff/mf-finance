package plans

import (
	"context"
	"os"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

func consPart(label string, plan float64, groups ...ExpConsGroup) expAllPart {
	c := ExpConsolidated{Year: 2026, Month: 10, Window: ExpWindowTail, FromMonth: 10, ToMonth: 12, Total: newExpConsAmounts(),
		Countries: []ExpConsCountry{{Country: "BY", Rows: len(groups)}}}
	c.Total.PlanTotal = plan
	c.Total.FactLY = plan / 2
	c.Groups = groups
	return expAllPart{Label: label, Cons: c}
}

func consGroup(name string, code int, plan float64) ExpConsGroup {
	a := newExpConsAmounts()
	a.PlanTotal, a.FactLY = plan, plan/2
	l := ExpConsLine{CodePL: code, Expense: "статья", ExpConsAmounts: newExpConsAmounts()}
	l.PlanTotal, l.FactLY = plan, plan/2
	return ExpConsGroup{Group: name, Lines: []ExpConsLine{l}, ExpConsAmounts: a}
}

// Общий свод: колонка — канал, итог — сумма каналов, группы объединяются по названию в порядке листа Excel.
func TestMergeExpConsolidatedChannels(t *testing.T) {
	parts := []expAllPart{
		consPart(expAllLabelRetail, 1000, consGroup("Аренда", 54, 600), consGroup("Заработная плата", 24, 400)),
		consPart(expAllLabelIM, 100, consGroup("Заработная плата", 24, 100)),
		consPart(expAllLabelOpt, 50, consGroup("Аренда", 54, 20), consGroup("Прочие расходы", 47, 30)),
	}
	out := mergeExpConsolidatedChannels(7, parts, expRetailPnlOrder)
	if out.Channel != ExpChannelAll || out.Year != 2026 || out.Month != 10 || out.Window != ExpWindowTail {
		t.Fatalf("шапка свода: %+v", out)
	}
	if !near(out.Total.PlanTotal, 1150) || !near(out.Total.Plan[expAllLabelRetail], 1000) ||
		!near(out.Total.Plan[expAllLabelIM], 100) || !near(out.Total.Plan[expAllLabelOpt], 50) || !near(out.Total.FactLY, 575) {
		t.Fatalf("итоги: %+v", out.Total)
	}
	if len(out.Countries) != 3 || out.Countries[0].Country != expAllLabelRetail || out.Countries[2].Country != expAllLabelOpt {
		t.Fatalf("колонки: %+v", out.Countries)
	}
	byName := map[string]ExpConsGroup{}
	var order []string
	for _, g := range out.Groups {
		byName[g.Group] = g
		order = append(order, g.Group)
	}
	// Порядок листа Excel: «Заработная плата» раньше «Аренды», «Прочие расходы» — позже.
	if len(order) != 3 || order[0] != "Заработная плата" || order[1] != "Аренда" || order[2] != "Прочие расходы" {
		t.Fatalf("порядок групп: %v", order)
	}
	zp := byName["Заработная плата"]
	if !near(zp.PlanTotal, 500) || !near(zp.Plan[expAllLabelRetail], 400) || !near(zp.Plan[expAllLabelIM], 100) || len(zp.Lines) != 1 || !near(zp.Lines[0].PlanTotal, 500) {
		t.Errorf("группа «Заработная плата» должна объединить розницу и ИМ: %+v", zp)
	}
	if rent := byName["Аренда"]; !near(rent.PlanTotal, 620) || !near(rent.Plan[expAllLabelOpt], 20) {
		t.Errorf("группа «Аренда»: %+v", rent)
	}
}

// Канал без карточки (или без доступа) остаётся колонкой с пояснением и в итог не входит.
func TestMergeExpConsolidatedChannels_ErrorChannel(t *testing.T) {
	parts := []expAllPart{
		consPart(expAllLabelRetail, 1000, consGroup("Аренда", 54, 1000)),
		{Label: expAllLabelOpt, Err: "карточка вне вашего среза доступа"},
	}
	out := mergeExpConsolidatedChannels(7, parts, expRetailPnlOrder)
	if !near(out.Total.PlanTotal, 1000) {
		t.Errorf("канал с ошибкой не должен входить в итог: %+v", out.Total)
	}
	if len(out.Countries) != 2 || out.Countries[1].Error == "" {
		t.Errorf("канал с ошибкой должен остаться колонкой с пояснением: %+v", out.Countries)
	}
}

// Сервис на PG: общий свод собирается из карточек розницы, ИМ и опта; опт закрыт срезом доступа.
func TestExpService_AllChannelsConsolidated_PG(t *testing.T) {
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
	pl, err := NewPgStore(pool).EnsureInstance(ctx, 2092, 10)
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
	seed := func(scopeKey string, rows []ExpRow, v float64) {
		for _, c := range cards {
			if c.FormCode != TemplateExp || c.ScopeKey != scopeKey {
				continue
			}
			kind, country := expKindOf(c.ScopeKey)
			inst, err := repo.EnsureInstance(ctx, ExpInstance{CardID: c.ID, FormKind: kind, Country: country, PlanYear: 2092, Currency: c.Currency}, 0)
			if err != nil {
				t.Fatal(err)
			}
			instIDs = append(instIDs, inst.ID)
			if _, err := repo.SyncRows(ctx, inst.ID, rows); err != nil {
				t.Fatal(err)
			}
			var cells []ExpCell
			for _, r := range rows {
				amt := v
				cells = append(cells, ExpCell{CodeCFO: r.CodeCFO, CodePL: r.CodePL, Month: 10, Amount: &amt})
			}
			if _, err := repo.SaveValues(ctx, inst.ID, cells, 0, 0); err != nil {
				t.Fatal(err)
			}
			return
		}
		t.Fatalf("нет карточки %s", scopeKey)
	}
	seed("R-BY", []ExpRow{{CodeCFO: 101, CodePL: 24, Expense: "ЗП", GroupPL: "ВЫПЛАТЫ"}}, 1000)
	seed("IM-BY", []ExpRow{{CodeCFO: 380, CodePL: 24, Expense: "ЗП", CountryCompany: "BY"}}, 100)
	seed("OPT", []ExpRow{{CodeCFO: 35, CodePL: 24, Expense: "ЗП", CountryCompany: "BY"}, {CodeCFO: 303, CodePL: 24, Expense: "ЗП", CountryCompany: "RU"}}, 10)

	admin := Principal{UserID: 1, PlansAdmin: true}
	out, err := svc.ChannelConsolidated(ctx, admin, pl, ExpChannelAll, ExpWindowMonth)
	if err != nil {
		t.Fatalf("общий свод: %v", err)
	}
	if out.Channel != ExpChannelAll || len(out.Countries) != 3 {
		t.Fatalf("общий свод: канал %q, колонок %d", out.Channel, len(out.Countries))
	}
	for _, c := range out.Countries {
		if c.Error != "" {
			t.Errorf("колонка %s с ошибкой: %s", c.Country, c.Error)
		}
	}
	// Розница РБ 1000 + ИМ 100 + опт 10 (БЕЛ) + 10 (РФ) = 1120.
	if !near(out.Total.Plan[expAllLabelRetail], 1000) || !near(out.Total.Plan[expAllLabelIM], 100) || !near(out.Total.Plan[expAllLabelOpt], 20) || !near(out.Total.PlanTotal, 1120) {
		t.Fatalf("итоги по каналам: %+v", out.Total)
	}

	// Исполнитель розницы KZ: опт ему недоступен — колонка с пояснением, в итог не входит.
	svc.WithScope(denyScope{rules: []UserScope{{Country: "BY", LegalEntity: "F"}}})
	restricted, err := svc.ChannelConsolidated(ctx, Principal{UserID: 2}, pl, ExpChannelAll, ExpWindowMonth)
	if err != nil {
		t.Fatalf("общий свод при ограниченном доступе: %v", err)
	}
	var optErr string
	for _, c := range restricted.Countries {
		if c.Country == expAllLabelOpt {
			optErr = c.Error
		}
	}
	if optErr == "" || near(restricted.Total.Plan[expAllLabelOpt], 20) {
		t.Errorf("опт при ограниченном доступе должен быть закрыт: ошибка %q, итоги %+v", optErr, restricted.Total)
	}
}
