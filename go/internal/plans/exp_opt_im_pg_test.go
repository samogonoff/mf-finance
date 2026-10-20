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

// Период, где расходы уже утверждены, не получает карточки ИМ и опта (этап 1.5 не откатывается);
// период «в работе» получает.
func TestExpOptIM_NotCreatedInClosedPeriod_PG(t *testing.T) {
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
	pl, err := NewPgStore(pool).EnsureInstance(ctx, 2097, 6)
	if err != nil {
		t.Fatal(err)
	}
	defer pool.Exec(ctx, `DELETE FROM pl_instance WHERE id=$1`, pl)
	defer pool.Exec(ctx, `DELETE FROM form_card WHERE pl_id=$1 AND form_code=$2`, pl, TemplateExp)

	svc := NewCardService(NewCardStore(pool))
	// Старый период: заводим только прежние области и утверждаем их, как было до выката ИМ и опта.
	for _, key := range []string{"R-BY", "R-RU", "MP-RU"} {
		sc, ok := formScope(TemplateExp, key)
		if !ok {
			t.Fatalf("нет области %s", key)
		}
		if _, err := NewCardStore(pool).EnsureCard(ctx, Card{PlID: pl, FormCode: TemplateExp, ScopeKey: key,
			Title: "t " + key, Country: sc.Country, LegalEntity: sc.LegalEntity, Currency: sc.Currency}); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := pool.Exec(ctx, `UPDATE form_card SET status=$3 WHERE pl_id=$1 AND form_code=$2`, pl, TemplateExp, CardApproved); err != nil {
		t.Fatal(err)
	}
	has := func(cards []Card, key string) bool { return hasCardScope(cards, TemplateExp, key) }

	cards, err := svc.EnsureCards(ctx, pl, 0)
	if err != nil {
		t.Fatal(err)
	}
	if has(cards, "OPT") || has(cards, "IM-BY") {
		t.Fatal("в периоде с утверждёнными расходами карточки ИМ и опта создаваться не должны")
	}
	// Один из прежних карточек вернули в работу — период снова «в работе»: поздние области заводятся.
	if _, err := pool.Exec(ctx, `UPDATE form_card SET status=$3 WHERE pl_id=$1 AND form_code=$2 AND scope_key='R-BY'`,
		pl, TemplateExp, CardDraft); err != nil {
		t.Fatal(err)
	}
	cards, err = svc.EnsureCards(ctx, pl, 0)
	if err != nil {
		t.Fatal(err)
	}
	if !has(cards, "OPT") || !has(cards, "IM-BY") {
		t.Fatal("в периоде в работе карточки ИМ и опта должны заводиться")
	}
}
