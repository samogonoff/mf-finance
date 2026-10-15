package plans

import (
	"context"
	"os"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

func TestScopedStepOwners_PG(t *testing.T) {
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
	pl, err := NewPgStore(pool).EnsureInstance(ctx, 2099, 9)
	if err != nil {
		t.Fatal(err)
	}
	defer pool.Exec(ctx, `DELETE FROM pl_instance WHERE id=$1`, pl)
	if _, err := NewCardStore(pool).EnsureCard(ctx, Card{PlID: pl, FormCode: TemplateMP, ScopeKey: "large", Year: 2099, Month: 9}); err != nil {
		t.Fatal(err)
	}
	var ru, by int64
	for _, pair := range []struct {
		email string
		id    *int64
	}{{"scoped-owner-ru@test.invalid", &ru}, {"scoped-owner-by@test.invalid", &by}} {
		if err := pool.QueryRow(ctx, `INSERT INTO users(email) VALUES($1) RETURNING id`, pair.email).Scan(pair.id); err != nil {
			t.Fatal(err)
		}
		defer pool.Exec(ctx, `DELETE FROM users WHERE id=$1`, *pair.id)
	}
	var rowID int64
	if err := pool.QueryRow(ctx, `INSERT INTO plans_directory_row(directory_id,external_id,payload_json)
 SELECT id,'tactics-owner-test','{"code_cfo":335,"country":"RU","legal_entity":"RU-company","segment":"large"}'::jsonb
 FROM plans_directory WHERE code='dir_marketplace' RETURNING id`).Scan(&rowID); err != nil {
		t.Fatal(err)
	}
	defer pool.Exec(ctx, `DELETE FROM plans_directory_row WHERE id=$1`, rowID)
	// Маршрут МП после 0072: 1.1 → 1.2 → 1.3 → 1.4, шаг fin выключен.
	for _, a := range []UserScope{
		{UserID: ru, Role: "dept_head", StageCode: "1.2", Country: "RU", CodeCFO: []int{335}},
		{UserID: by, Role: "dept_head", StageCode: "1.2", Country: "BY", CodeCFO: []int{335}},
		{UserID: ru, Role: "fin_controller", StageCode: "fin", Country: "RU", CodeCFO: []int{335}},
		{UserID: ru, Role: "unrelated", StageCode: "2.3"}, {UserID: by, Role: "unrelated", StageCode: "2.3"},
	} {
		if err := NewPgScopeStore(pool).UpsertScope(ctx, a); err != nil {
			t.Fatal(err)
		}
	}
	got, err := NewCardStepOwnerResolver(pool, nil).StepOwners(ctx, pl, TemplateMP, "large")
	if err != nil {
		t.Fatal(err)
	}
	if got["1.2"] != ru {
		t.Fatalf("manager=%d want=%d owners=%v", got["1.2"], ru, got)
	}
	if got["fin"] != 0 {
		t.Fatalf("выключенный шаг fin не должен получать владельца у МП: %v", got)
	}
	if got["2.3"] != 0 {
		t.Fatal("an unrelated route step participates in card ownership")
	}
}
