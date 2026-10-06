package plans

import (
	"context"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

// The peer check must run after the period lock, using the state committed by
// a concurrent return. This test uses a disposable period in the isolated PG DB.
func TestRetailCountryGateTransaction_PG(t *testing.T) {
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
	const year, month = 2099, 8
	var plID int64
	_ = pool.QueryRow(ctx, `SELECT id FROM pl_instance WHERE period_year=$1 AND period_month=$2`, year, month).Scan(&plID)
	if plID != 0 {
		_, _ = pool.Exec(ctx, `DELETE FROM pl_instance WHERE id=$1`, plID)
	}
	plID, err = NewPgStore(pool).EnsureInstance(ctx, year, month)
	if err != nil {
		t.Fatal(err)
	}
	defer pool.Exec(context.Background(), `DELETE FROM pl_instance WHERE id=$1`, plID)
	cards, err := NewCardService(NewCardStore(pool)).EnsureCards(ctx, plID, 0)
	if err != nil {
		t.Fatal(err)
	}
	var current, peer Card
	for _, c := range cards {
		if c.FormCode != TemplateRetail {
			continue
		}
		_, err = pool.Exec(ctx, `UPDATE form_card SET status='on_approval', step_code='1.3' WHERE id=$1`, c.ID)
		if err != nil {
			t.Fatal(err)
		}
		if c.ScopeKey == "BY" {
			current = c
		}
		if c.ScopeKey == "KZ" {
			peer = c
		}
	}
	if current.ID == 0 || peer.ID == 0 {
		t.Fatal("retail test cards missing")
	}
	tx, err := pool.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	const retailLockNamespace int64 = 660066000000000000
	if _, err = tx.Exec(ctx, `SELECT pg_advisory_xact_lock($1)`, retailLockNamespace+plID); err != nil {
		t.Fatal(err)
	}
	result := make(chan error, 1)
	go func() {
		_, saveErr := NewCardStore(pool).SaveTransition(ctx, current.ID, CardTransition{
			Status: CardOnApproval, StepCode: "1.4", VersionBump: true,
			ExpectedStatus: CardOnApproval, ExpectedStep: "1.3", ExpectedVersion: 0,
			RetailPlID: plID, RequireRetailCountryGate: true,
			Approvals: []ApprovalWrite{{StepCode: "1.3", Decision: "approve"}},
		}, []byte(`{}`), 0)
		result <- saveErr
	}()
	if _, err = tx.Exec(ctx, `UPDATE form_card SET status='returned', step_code='1.1' WHERE id=$1`, peer.ID); err != nil {
		t.Fatal(err)
	}
	if err = tx.Commit(ctx); err != nil {
		t.Fatal(err)
	}
	if err = <-result; err == nil || !strings.Contains(err.Error(), "KZ") {
		t.Fatalf("concurrent return must block final transition: %v", err)
	}
	now, err := NewCardStore(pool).Card(ctx, current.ID)
	if err != nil || now.StepCode != "1.3" {
		t.Fatalf("blocked card changed: %+v %v", now, err)
	}
	// A form save can change values without a route version bump. The card
	// timestamp detects the stale snapshot after that save commits.
	if _, err := pool.Exec(ctx, `UPDATE form_card SET updated_at=updated_at + interval '1 second' WHERE id=$1`, current.ID); err != nil {
		t.Fatal(err)
	}
	_, err = NewCardStore(pool).SaveTransition(ctx, current.ID, CardTransition{
		Status: CardOnApproval, StepCode: "1.4", ExpectedStatus: now.Status,
		ExpectedStep: now.StepCode, ExpectedVersion: now.CurrentVersion,
		ExpectedUpdatedAt: now.UpdatedAt, RetailPlID: plID,
	}, nil, 0)
	if err == nil || !strings.Contains(err.Error(), "карточка изменилась") {
		t.Fatalf("stale form timestamp must block transition: %v", err)
	}
}
