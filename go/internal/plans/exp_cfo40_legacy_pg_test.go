package plans

import (
	"context"
	"os"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

// A preexisting retail row stays hidden in editable cards but remains visible
// in cards whose values were already submitted or approved.
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
	// The rollout migration marks rows in editable retail cards. The marker is
	// persistent, so entering approval cannot make the excluded row reappear.
	if _, err := pool.Exec(ctx, `UPDATE exp_row SET attrs=jsonb_set(attrs,'{retail_cfo40_reassigned}','true'::jsonb) WHERE id=$1`, oldRowID); err != nil {
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
	// A card marked while editable stays reclassified after submission.
	for _, tc := range []struct {
		status string
		locked bool
		want   int
	}{
		{CardOnApproval, false, 0},
		{CardApproved, false, 0},
		{CardPublished, false, 0},
		{CardDraft, true, 0},
		{CardReturned, false, 0},
	} {
		if _, err := pool.Exec(ctx, `UPDATE form_card SET status=$1, locked=$2 WHERE id=$3`, tc.status, tc.locked, retailCard.ID); err != nil {
			t.Fatal(err)
		}
		rows, total, err := repo.Rows(ctx, retail.ID, ExpFilter{Limit: 100})
		if err != nil || len(rows) != tc.want || total != tc.want {
			t.Fatalf("status=%s locked=%v visible rows=%d total=%d err=%v", tc.status, tc.locked, len(rows), total, err)
		}
		totals, err := repo.GroupTotals(ctx, retail.ID)
		if err != nil || totals["ВЫПЛАТЫ"] != float64(tc.want)*100 {
			t.Fatalf("status=%s locked=%v group totals=%v err=%v", tc.status, tc.locked, totals, err)
		}
		provider := NewExpCardProvider(repo, nil)
		pub, err := provider.PublishRows(ctx, retailCard)
		if err != nil || len(pub) != tc.want {
			t.Fatalf("status=%s locked=%v publish rows=%v err=%v", tc.status, tc.locked, pub, err)
		}
		snapshot, err := provider.FormSnapshot(ctx, retailCard)
		if err != nil || len(snapshot["values"].([]map[string]any)) != tc.want {
			t.Fatalf("status=%s locked=%v snapshot=%v err=%v", tc.status, tc.locked, snapshot, err)
		}
	}
	// A card already submitted before rollout has no marker and keeps its
	// approved CFO 40 value, including on repeat publication.
	if _, err := pool.Exec(ctx, `UPDATE exp_row SET attrs=attrs-'retail_cfo40_reassigned' WHERE id=$1`, oldRowID); err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, `UPDATE form_card SET status=$1, locked=false WHERE id=$2`, CardOnApproval, retailCard.ID); err != nil {
		t.Fatal(err)
	}
	oldRows, _, err := repo.Rows(ctx, retail.ID, ExpFilter{Limit: 100})
	if err != nil || len(oldRows) != 1 {
		t.Fatalf("submitted historical row must remain visible: rows=%d err=%v", len(oldRows), err)
	}
	oldPub, err := NewExpCardProvider(repo, nil).PublishRows(ctx, retailCard)
	if err != nil || len(oldPub) != 1 {
		t.Fatalf("historical publication must retain CFO 40: rows=%d err=%v", len(oldPub), err)
	}
	// Returning to an approval step keeps the historical row and its totals.
	if _, err := NewCardStore(pool).SaveTransition(ctx, retailCard.ID,
		CardTransition{Status: CardReturned, StepCode: "1.2", VersionBump: true}, []byte(`{}`), 0); err != nil {
		t.Fatal(err)
	}
	approvalRows, _, err := repo.Rows(ctx, retail.ID, ExpFilter{Limit: 100})
	if err != nil || len(approvalRows) != 1 {
		t.Fatalf("approval return must retain CFO 40: rows=%d err=%v", len(approvalRows), err)
	}
	approvalTotals, err := repo.GroupTotals(ctx, retail.ID)
	if err != nil || approvalTotals["ВЫПЛАТЫ"] != 100 {
		t.Fatalf("approval return changed old total: totals=%v err=%v", approvalTotals, err)
	}
	provider := NewExpCardProvider(repo, nil)
	approvalSnapshot, err := provider.FormSnapshot(ctx, retailCard)
	if err != nil || len(approvalSnapshot["values"].([]map[string]any)) != 1 {
		t.Fatalf("approval return changed snapshot: snapshot=%v err=%v", approvalSnapshot, err)
	}
	approvalPub, err := provider.PublishRows(ctx, retailCard)
	if err != nil || len(approvalPub) != 1 {
		t.Fatalf("approval return changed publication: rows=%d err=%v", len(approvalPub), err)
	}
	// A later return to the fill step starts a new editing cycle and marks the
	// row in the same transaction as the status change.
	if _, err := pool.Exec(ctx, `UPDATE form_card SET status=$1, step_code='1.4' WHERE id=$2`, CardOnApproval, retailCard.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := NewCardStore(pool).SaveTransition(ctx, retailCard.ID,
		CardTransition{Status: CardReturned, StepCode: "1.1", VersionBump: true}, []byte(`{}`), 0); err != nil {
		t.Fatal(err)
	}
	returnedRows, _, err := repo.Rows(ctx, retail.ID, ExpFilter{Limit: 100})
	if err != nil || len(returnedRows) != 0 {
		t.Fatalf("returned retail card must move CFO 40 to IM: rows=%d err=%v", len(returnedRows), err)
	}
	if _, err := pool.Exec(ctx, `UPDATE form_card SET status=$1 WHERE id=$2`, CardOnApproval, retailCard.ID); err != nil {
		t.Fatal(err)
	}
	resubmittedRows, _, err := repo.Rows(ctx, retail.ID, ExpFilter{Limit: 100})
	if err != nil || len(resubmittedRows) != 0 {
		t.Fatalf("resubmitted row must remain hidden: rows=%d err=%v", len(resubmittedRows), err)
	}
	var preserved float64
	if err := pool.QueryRow(ctx, `SELECT amount FROM exp_value WHERE row_id=$1 AND scenario='plan' AND month=10`, oldRowID).Scan(&preserved); err != nil || preserved != 100 {
		t.Fatalf("legacy manual amount was lost: %v, %v", preserved, err)
	}
	if _, err := pool.Exec(ctx, `UPDATE form_card SET status=$1, locked=false WHERE id=$2`, CardDraft, retailCard.ID); err != nil {
		t.Fatal(err)
	}
	storedCard, err := NewCardStore(pool).Card(ctx, retailCard.ID)
	if err != nil || !storedCard.HasData {
		t.Fatalf("legacy manual input must keep the draft marked as containing data: %+v, %v", storedCard, err)
	}
}
