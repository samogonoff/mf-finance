package plans

import (
	"context"
	"os"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

// A preexisting retail row must remain auditable in storage while every
// current read and publication counts CFO 40 only in the internet shop.
func TestExpLegacyRetailCFO40Excluded_PG(t *testing.T) {
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
	pl, err := NewPgStore(pool).EnsureInstance(ctx, 2191, 10)
	if err != nil {
		t.Fatal(err)
	}
	defer pool.Exec(context.Background(), `DELETE FROM pl_instance WHERE id=$1`, pl)
	repo := NewExpRepo(pool)
	makeInstance := func(scope string) (Card, ExpInstance) {
		card, err := NewCardStore(pool).EnsureCard(ctx, Card{PlID: pl, FormCode: TemplateExp, ScopeKey: scope, Country: "BY", Currency: "BYN"})
		if err != nil {
			t.Fatal(err)
		}
		kind, country := expKindOf(scope)
		inst, err := repo.EnsureInstance(ctx, ExpInstance{CardID: card.ID, FormKind: kind, Country: country, PlanYear: 2191, Currency: "BYN"}, 0)
		if err != nil {
			t.Fatal(err)
		}
		return card, inst
	}
	retailCard, retail := makeInstance("R-BY")
	imCard, im := makeInstance("IM-BY")
	row := ExpRow{CodeCFO: 40, CodePL: 87, GroupPL: "ВЫПЛАТЫ", Expense: "Налоги от выплат", LegalEntity: "ФОРМЭЛЬ ООО"}
	if _, err := repo.SyncRows(ctx, retail.ID, []ExpRow{row}); err != nil {
		t.Fatal(err)
	}
	if _, err := repo.SyncRows(ctx, im.ID, []ExpRow{row}); err != nil {
		t.Fatal(err)
	}
	var oldRowID int64
	if err := pool.QueryRow(ctx, `SELECT id FROM exp_row WHERE instance_id=$1 AND code_cfo=40 AND code_pl=87`, retail.ID).Scan(&oldRowID); err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, `INSERT INTO exp_value(row_id, scenario, month, amount, source) VALUES($1,'plan',10,100,'manual')`, oldRowID); err != nil {
		t.Fatal(err)
	}
	amount := 200.0
	if _, err := repo.SaveValues(ctx, im.ID, []ExpCell{{CodeCFO: 40, CodePL: 87, Month: 10, Amount: &amount}}, 0, 0); err != nil {
		t.Fatal(err)
	}
	if _, err := repo.SaveValues(ctx, retail.ID, []ExpCell{{CodeCFO: 40, CodePL: 87, Month: 10, Amount: &amount}}, 0, 0); err == nil {
		t.Fatal("legacy retail CFO 40 must not accept new plan writes")
	}
	for _, tc := range []struct {
		name      string
		card      Card
		inst      ExpInstance
		wantCount int
		wantTotal float64
	}{
		{"retail", retailCard, retail, 0, 0},
		{"internet shop", imCard, im, 1, 200},
	} {
		t.Run(tc.name, func(t *testing.T) {
			rows, total, err := repo.Rows(ctx, tc.inst.ID, ExpFilter{Limit: 100})
			if err != nil || len(rows) != tc.wantCount || total != tc.wantCount {
				t.Fatalf("visible rows=%d total=%d err=%v", len(rows), total, err)
			}
			totals, err := repo.GroupTotals(ctx, tc.inst.ID)
			if err != nil || totals["ВЫПЛАТЫ"] != tc.wantTotal {
				t.Fatalf("group totals=%v err=%v", totals, err)
			}
			pub, err := NewExpCardProvider(repo, nil).PublishRows(ctx, tc.card)
			if err != nil || len(pub) != tc.wantCount {
				t.Fatalf("publish rows=%v err=%v", pub, err)
			}
		})
	}
	var preserved float64
	if err := pool.QueryRow(ctx, `SELECT amount FROM exp_value WHERE row_id=$1 AND scenario='plan' AND month=10`, oldRowID).Scan(&preserved); err != nil || preserved != 100 {
		t.Fatalf("legacy manual amount was lost: %v, %v", preserved, err)
	}
	storedCard, err := NewCardStore(pool).Card(ctx, retailCard.ID)
	if err != nil || !storedCard.HasData {
		t.Fatalf("legacy manual input must keep the draft marked as containing data: %+v, %v", storedCard, err)
	}
}
