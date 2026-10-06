package plans

import (
	"context"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

func TestRetailSyncRowsNoOpAndCardLock_PG(t *testing.T) {
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

	const year, month = 2099, 10
	var oldID int64
	_ = pool.QueryRow(ctx, `SELECT id FROM pl_instance WHERE period_year=$1 AND period_month=$2`, year, month).Scan(&oldID)
	if oldID != 0 {
		if _, err := pool.Exec(ctx, `DELETE FROM pl_instance WHERE id=$1`, oldID); err != nil {
			t.Fatal(err)
		}
	}
	plID, err := NewPgStore(pool).EnsureInstance(ctx, year, month)
	if err != nil {
		t.Fatal(err)
	}
	defer pool.Exec(context.Background(), `DELETE FROM pl_instance WHERE id=$1`, plID)
	cards, err := NewCardService(NewCardStore(pool)).EnsureCards(ctx, plID, 0)
	if err != nil {
		t.Fatal(err)
	}
	var card Card
	for _, c := range cards {
		if c.FormCode == TemplateRetail && c.ScopeKey == "BY" {
			card = c
			break
		}
	}
	if card.ID == 0 {
		t.Fatal("BY retail card missing")
	}
	repo := NewRetailRepo(pool)
	inst, err := repo.EnsureInstance(ctx, card, 0)
	if err != nil {
		t.Fatal(err)
	}
	store := RetailStore{CodeCFO: 9900001, NameCFO: "test store", City: "Minsk", Country: "BY"}
	if n, _, _, err := repo.SyncRows(ctx, inst, []RetailStore{store}, nil, 0); err != nil || n != 1 {
		t.Fatalf("initial sync: added=%d err=%v", n, err)
	}
	var first time.Time
	var version int
	if err := pool.QueryRow(ctx, `SELECT c.updated_at, r.row_version FROM form_card c JOIN tp_instance i ON i.card_id=c.id JOIN tp_row r ON r.instance_id=i.id WHERE c.id=$1`, card.ID).Scan(&first, &version); err != nil {
		t.Fatal(err)
	}
	if a, u, d, err := repo.SyncRows(ctx, inst, []RetailStore{store}, nil, 0); err != nil || a != 0 || u != 0 || d != 0 {
		t.Fatalf("unchanged sync mutated rows: %d/%d/%d %v", a, u, d, err)
	}
	var second time.Time
	var secondVersion int
	if err := pool.QueryRow(ctx, `SELECT c.updated_at, r.row_version FROM form_card c JOIN tp_instance i ON i.card_id=c.id JOIN tp_row r ON r.instance_id=i.id WHERE c.id=$1`, card.ID).Scan(&second, &secondVersion); err != nil {
		t.Fatal(err)
	}
	if !first.Equal(second) || version != secondVersion {
		t.Fatalf("no-op sync changed card/row: %v/%d -> %v/%d", first, version, second, secondVersion)
	}

	store.City = "Brest"
	if _, u, _, err := repo.SyncRows(ctx, inst, []RetailStore{store}, nil, 0); err != nil || u != 1 {
		t.Fatalf("changed sync: updated=%d err=%v", u, err)
	}
	var third time.Time
	if err := pool.QueryRow(ctx, `SELECT updated_at FROM form_card WHERE id=$1`, card.ID).Scan(&third); err != nil {
		t.Fatal(err)
	}
	if !third.After(second) {
		t.Fatalf("changed sync did not advance card timestamp: %v -> %v", second, third)
	}

	if _, err := pool.Exec(ctx, `UPDATE form_card SET status='on_approval', step_code='1.2' WHERE id=$1`, card.ID); err != nil {
		t.Fatal(err)
	}
	store.City = "Gomel"
	if _, _, _, err := repo.SyncRows(ctx, inst, []RetailStore{store}, nil, 0); err == nil || !strings.Contains(err.Error(), "закрыт на запись") {
		t.Fatalf("closed card accepted sync: %v", err)
	}
	amount := 1.0
	if _, err := repo.SaveValues(ctx, inst.ID, []RetailCellWrite{{CodeCFO: store.CodeCFO, Metric: MetricSales, Year: year, Month: month, Amount: &amount}}, 0, "test"); err == nil || !strings.Contains(err.Error(), "закрыт на запись") {
		t.Fatalf("closed card accepted values: %v", err)
	}
}
