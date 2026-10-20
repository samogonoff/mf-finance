package plans

import (
	"context"
	"os"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

// Реестр форм заводит карточки ИМ и Опта; у опта страны нет (одна форма на три страны, ввод в BYN),
// и экземпляр формы с пустой страной создаётся и хранит строки разных стран.
func TestExpOptIM_CardsAndInstance_PG(t *testing.T) {
	dsn := os.Getenv("PLANS_TEST_PG_DSN")
	if dsn == "" {
		t.Skip("PLANS_TEST_PG_DSN not set")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	pool, err := pgxpool.New(ctx, dsn)
	if err != nil {
		t.Fatal(err)
	}
	defer pool.Close()
	plID, err := NewPgStore(pool).EnsureInstance(ctx, 2098, 7)
	if err != nil {
		t.Fatal(err)
	}
	defer pool.Exec(ctx, `DELETE FROM pl_instance WHERE id=$1`, plID)

	cards, err := NewCardService(NewCardStore(pool)).EnsureCards(ctx, plID, 0)
	if err != nil {
		t.Fatal(err)
	}
	var opt, im *Card
	for i := range cards {
		if cards[i].FormCode != TemplateExp {
			continue
		}
		switch cards[i].ScopeKey {
		case "OPT":
			opt = &cards[i]
		case "IM-BY":
			im = &cards[i]
		}
	}
	if opt == nil || im == nil {
		t.Fatalf("карточки опта и ИМ не заведены: opt=%v im=%v", opt != nil, im != nil)
	}
	if opt.Currency != "BYN" || opt.Country != "" {
		t.Errorf("опт: валюта BYN и без страны, получили %q/%q", opt.Currency, opt.Country)
	}
	defer pool.Exec(ctx, `DELETE FROM form_card WHERE pl_id=$1 AND form_code=$2`, plID, TemplateExp)

	kind, country := expKindOf(opt.ScopeKey)
	repo := NewExpRepo(pool)
	inst, err := repo.EnsureInstance(ctx, ExpInstance{CardID: opt.ID, FormKind: kind, Country: country, PlanYear: 2098, Currency: opt.Currency}, 0)
	if err != nil {
		t.Fatalf("экземпляр формы опта: %v", err)
	}
	defer pool.Exec(ctx, `DELETE FROM exp_instance WHERE id=$1`, inst.ID)
	rows := []ExpRow{
		{CodeCFO: 35, CodePL: 24, CFO: "Отдел продаж", CountryCompany: "BY"},
		{CodeCFO: 303, CodePL: 24, CFO: "ВЭД РФ", CountryCompany: "RU"},
		{CodeCFO: 414, CodePL: 24, CFO: "Отдел продаж опта УЗ", CountryCompany: "UZ"},
	}
	if _, err := repo.SyncRows(ctx, inst.ID, rows); err != nil {
		t.Fatal(err)
	}
	got, total, err := repo.Rows(ctx, inst.ID, ExpFilter{})
	if err != nil || total != 3 || len(got) != 3 {
		t.Fatalf("строки опта: %v, total=%d, rows=%d", err, total, len(got))
	}
	countries := map[string]bool{}
	for _, r := range got {
		countries[r.CountryCompany] = true
	}
	if !countries["BY"] || !countries["RU"] || !countries["UZ"] {
		t.Errorf("страны строк опта потеряны: %v", countries)
	}
}
