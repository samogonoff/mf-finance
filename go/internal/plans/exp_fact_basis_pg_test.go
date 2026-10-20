package plans

import (
	"context"
	"os"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

// Платёжный факт помечается в самих данных: признак переживает рестарт процесса, а повторная загрузка
// по начислениям снимает пометку.
func TestExpRepo_FactBasisPersists_PG(t *testing.T) {
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
	pl, err := NewPgStore(pool).EnsureInstance(ctx, 2099, 8)
	if err != nil {
		t.Fatal(err)
	}
	defer pool.Exec(ctx, `DELETE FROM pl_instance WHERE id=$1`, pl)
	card, err := NewCardStore(pool).EnsureCard(ctx, Card{PlID: pl, FormCode: TemplateExp, ScopeKey: "R-BY", Year: 2099, Month: 8, Country: "BY", Currency: "BYN"})
	if err != nil {
		t.Fatal(err)
	}
	repo := NewExpRepo(pool)
	inst, err := repo.EnsureInstance(ctx, ExpInstance{CardID: card.ID, FormKind: ExpKindRetail, Country: "BY", PlanYear: 2099, Currency: "BYN"}, 0)
	if err != nil {
		t.Fatal(err)
	}
	defer pool.Exec(ctx, `DELETE FROM exp_instance WHERE id=$1`, inst.ID)
	if _, err := repo.SyncRows(ctx, inst.ID, []ExpRow{{CodeCFO: 101, CodePL: 24, Expense: "ЗП"}}); err != nil {
		t.Fatal(err)
	}
	amt := 10.0
	cell := func(src string) []ExpCell {
		return []ExpCell{{CodeCFO: 101, CodePL: 24, Month: 3, Amount: &amt, Source: src}}
	}
	if b, err := repo.FactBasis(ctx, inst.ID); err != nil || b != "" {
		t.Fatalf("до загрузки пометки нет: %q %v", b, err)
	}
	if err := repo.SaveReadOnly(ctx, inst.ID, ExpScenarioFact, cell(ExpFactBasisPayments)); err != nil {
		t.Fatal(err)
	}
	if b, err := repo.FactBasis(ctx, inst.ID); err != nil || b != ExpFactBasisPayments {
		t.Fatalf("платёжный факт должен быть помечен: %q %v", b, err)
	}
	// Начисления появились: перезапись той же ячейки снимает пометку.
	if err := repo.SaveReadOnly(ctx, inst.ID, ExpScenarioFact, cell("")); err != nil {
		t.Fatal(err)
	}
	if b, err := repo.FactBasis(ctx, inst.ID); err != nil || b != "" {
		t.Fatalf("пометка должна сняться: %q %v", b, err)
	}
}