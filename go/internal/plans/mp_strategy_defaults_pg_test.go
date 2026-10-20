package plans

import (
	"context"
	"os"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

type strategyOnlyFact struct{ MpFactSource }

func (strategyOnlyFact) MpFact(context.Context, int, int, string) ([]FactRow, error) { return nil, nil }
func (strategyOnlyFact) MpTaktTarget(context.Context, int, int, string) ([]FactRow, error) {
	return nil, nil
}

func (strategyOnlyFact) MpStrategy(_ context.Context, _, _ int, _ string) ([]FactRow, error) {
	return []FactRow{
		{CodeCFO: 335, CodePL: 1006, Amount: 800}, {CodeCFO: 335, CodePL: 1045, Amount: 1000},
		{CodeCFO: 335, CodePL: 8006, Amount: 500}, {CodeCFO: 335, CodePL: 2006, Amount: 640},
	}, nil
}

// Значения из стратегии считают маржу в пересчёте и показываются в реестре, но МП-01 по-прежнему требует
// сохранить «Условия площадок»; в закрытой карточке реестр подстановку не получает.
func TestMpStrategyDefaultsValidationAndClosedCardPG(t *testing.T) {
	dsn := os.Getenv("PLANS_TEST_PG_DSN")
	if dsn == "" {
		t.Skip("PLANS_TEST_PG_DSN не задан")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	pool, err := pgxpool.New(ctx, dsn)
	if err != nil {
		t.Fatal(err)
	}
	defer pool.Close()

	const year, month = 2097, 5
	var old int64
	if pool.QueryRow(ctx, `SELECT id FROM pl_instance WHERE period_year=$1 AND period_month=$2`, year, month).Scan(&old) == nil {
		_, _ = pool.Exec(ctx, `DELETE FROM pl_instance WHERE id=$1`, old)
	}
	metrics := NewPgStore(pool)
	plID, err := metrics.EnsureInstance(ctx, year, month)
	if err != nil {
		t.Fatal(err)
	}
	defer pool.Exec(context.Background(), `DELETE FROM pl_instance WHERE id=$1`, plID)
	cards := NewCardStore(pool)
	card, err := cards.EnsureCard(ctx, Card{PlID: plID, FormCode: TemplateMP, ScopeKey: "large", Title: "test", Currency: "RUB"})
	if err != nil {
		t.Fatal(err)
	}
	if err := metrics.UpsertMetrics(ctx, plID, "large", []MetricRow{{
		LineCode: blockToPL()[BSalesManagerGross], BlockType: BSalesManagerGross, ProfitCenter: 335,
		Scenario: ScenarioTactic, Year: year, Month: month, Currency: "RUB", Amount: 1_000_000,
	}}); err != nil {
		t.Fatal(err)
	}
	svc := NewMpConditionsService(NewMpConditionsStore(pool), metrics, cards, nil, strategyOnlyFact{}, nil)
	admin := Principal{PlansAdmin: true}

	res, err := svc.Recalc(ctx, card.ID, true, admin)
	if err != nil {
		t.Fatal(err)
	}
	if res.Platforms[335][BRetailMargin] <= 0 {
		t.Fatalf("маржа должна считаться по значениям из стратегии: %v", res.Platforms[335][BRetailMargin])
	}
	var mp01 bool
	for _, is := range res.Issues {
		mp01 = mp01 || is.Code == "МП-01" && is.Level == MpBlocking && is.CodeCFO == 335
	}
	if !mp01 || !res.Blocking {
		t.Fatalf("условия не сохранены — МП-01 обязана блокировать отправку: %+v", res.Issues)
	}

	view, err := svc.Conditions(ctx, card.ID, admin)
	if err != nil {
		t.Fatal(err)
	}
	var shown bool
	for _, c := range view.Conditions {
		shown = shown || c.CodeCFO == 335 && c.ID == 0 && len(c.StrategyDefaults) == 3
	}
	if !shown {
		t.Fatalf("в реестре редактируемой карточки значения из стратегии должны быть помечены: %+v", view.Conditions)
	}

	// Карточка закрыта: реестр показывает только сохранённое.
	if _, err := pool.Exec(ctx, `UPDATE form_card SET status='approved', locked=true WHERE id=$1`, card.ID); err != nil {
		t.Fatal(err)
	}
	view, err = svc.Conditions(ctx, card.ID, admin)
	if err != nil {
		t.Fatal(err)
	}
	for _, c := range view.Conditions {
		if len(c.StrategyDefaults) != 0 {
			t.Fatalf("в закрытой карточке подстановки быть не должно: %+v", c)
		}
	}
}
