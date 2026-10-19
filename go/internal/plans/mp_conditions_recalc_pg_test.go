package plans

import (
	"context"
	"math"
	"os"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

// Сквозная проверка экрана «Условия площадок»: сохранённые условия и пересчёт
// действительно меняют расходную часть карточки, предпросмотр с черновиком ничего
// не пишет, а правка действует на месяц карточки и последующие, но не на прошлые.
func TestMpConditionsRecalcAndPeriodPG(t *testing.T) {
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

	const year = 2098
	cleanup := func() {
		for _, m := range []int{9, 10, 11} {
			var id int64
			if pool.QueryRow(ctx, `SELECT id FROM pl_instance WHERE period_year=$1 AND period_month=$2`, year, m).Scan(&id) == nil {
				_, _ = pool.Exec(ctx, `DELETE FROM pl_instance WHERE id=$1`, id)
			}
		}
	}
	cleanup()
	defer cleanup()

	metrics := NewPgStore(pool)
	store := NewMpConditionsStore(pool)
	cards := NewCardStore(pool)
	sep, _ := metrics.EnsureInstance(ctx, year, 9)
	oct, err := metrics.EnsureInstance(ctx, year, 10)
	if err != nil {
		t.Fatal(err)
	}
	nov, _ := metrics.EnsureInstance(ctx, year, 11)
	card, err := cards.EnsureCard(ctx, Card{PlID: oct, FormCode: TemplateMP, ScopeKey: "large", Title: "test", Currency: "RUB"})
	if err != nil {
		t.Fatal(err)
	}
	if card.Year != year || card.Month != 10 || card.CalcMode != CalcInverse {
		t.Fatalf("карточка октября: %+v", card)
	}
	// Продажи площадки вводятся в форме; расходная часть считается от них.
	if err := metrics.UpsertMetrics(ctx, oct, "large", []MetricRow{{
		LineCode: blockToPL()[BSalesManagerGross], BlockType: BSalesManagerGross, ProfitCenter: 335,
		Scenario: ScenarioTactic, Year: year, Month: 10, Currency: "RUB", Amount: 1_000_000,
	}}); err != nil {
		t.Fatal(err)
	}

	svc := NewMpConditionsService(store, metrics, cards, nil, nil, nil)
	admin := Principal{PlansAdmin: true}
	save := func(share float64) {
		t.Helper()
		if _, err := svc.SaveConditions(ctx, card.ID, MpConditions{
			CodeCFO: 335, SppPct: .10, MarkupPct: .50, MarkupTotalPct: .20,
			Shares: map[string]float64{BCostAgent: share},
		}, admin); err != nil {
			t.Fatal(err)
		}
	}
	stored := func(plID int64, m int) (float64, bool) {
		var amount float64
		err := pool.QueryRow(ctx, `SELECT amount FROM pl_metric WHERE pl_id=$1 AND profit_center=335
			AND block_type=$2 AND scenario=$3 AND period_year=$4 AND period_month=$5`,
			plID, BCostAgent, ScenarioTactic, year, m).Scan(&amount)
		return amount, err == nil
	}

	// 1. Сохранили условия → пересчёт записал расходную часть в тактику.
	save(.05)
	r1, err := svc.Recalc(ctx, card.ID, false, admin)
	if err != nil {
		t.Fatal(err)
	}
	first := r1.Platforms[335][BCostAgent]
	if first <= 0 {
		t.Fatalf("расходная часть не посчиталась от условий: %v", first)
	}
	if got, ok := stored(oct, 10); !ok || math.Abs(got-first) > 1e-6 {
		t.Fatalf("пересчёт не записал сумму в тактику: %v %v (ожидалось %v)", got, ok, first)
	}

	// 2. Правка доли + пересчёт → сумма пересчиталась (доля ×2 → статья ×2).
	save(.10)
	r2, err := svc.Recalc(ctx, card.ID, false, admin)
	if err != nil {
		t.Fatal(err)
	}
	second := r2.Platforms[335][BCostAgent]
	if math.Abs(second-2*first) > 1e-6*first {
		t.Fatalf("после правки доли сумма должна удвоиться: было %v, стало %v", first, second)
	}
	if got, _ := stored(oct, 10); math.Abs(got-second) > 1e-6 {
		t.Fatalf("в тактике осталась старая сумма: %v, ожидалось %v", got, second)
	}

	// 3. Предпросмотр с несохранённым черновиком показывает эффект и ничего не пишет.
	pv, err := svc.Recalc(ctx, card.ID, true, admin, MpConditions{
		CodeCFO: 335, SppPct: .10, MarkupPct: .50, MarkupTotalPct: .20,
		Shares: map[string]float64{BCostAgent: .20},
	})
	if err != nil {
		t.Fatal(err)
	}
	if got := pv.Platforms[335][BCostAgent]; math.Abs(got-4*first) > 1e-6*first {
		t.Fatalf("предпросмотр должен учесть черновик (доля ×4): %v, ожидалось %v", got, 4*first)
	}
	if got, _ := stored(oct, 10); math.Abs(got-second) > 1e-6 {
		t.Fatalf("предпросмотр записал данные в тактику: %v", got)
	}
	saved, _ := store.Conditions(ctx, oct, year, 10)
	if len(saved) != 1 || saved[0].Shares[BCostAgent] != .10 {
		t.Fatalf("предпросмотр изменил сохранённые условия: %+v", saved)
	}

	// 4. Период: условия октября действуют на ноябрь (пока нет более новых), но
	// не на сентябрь; суммы тактики ноября сами не появляются — нужен пересчёт там.
	if list, err := store.Conditions(ctx, nov, year, 11); err != nil || len(list) != 1 || list[0].Shares[BCostAgent] != .10 {
		t.Fatalf("условия октября должны действовать на ноябрь: %v %+v", err, list)
	}
	if list, err := store.Conditions(ctx, sep, year, 9); err != nil || len(list) != 0 {
		t.Fatalf("правка октября не должна менять прошлые месяцы: %v %+v", err, list)
	}
	if _, ok := stored(nov, 11); ok {
		t.Fatal("суммы ноября не должны появляться без пересчёта карточки ноября")
	}

	// 5. Источник доли («из стратегии» / «вручную») хранится, читается и
	// копируется в следующий месяц; произвольный текст сбрасывается в «вручную».
	if _, err := svc.SaveConditions(ctx, card.ID, MpConditions{
		CodeCFO: 335, SppPct: .10, MarkupPct: .50, MarkupTotalPct: .20,
		Shares:       map[string]float64{BCostAgent: .10, BCostFreight: .01},
		ShareSources: map[string]string{BCostAgent: "strategy:2098-09", BCostFreight: "что-угодно"},
	}, admin); err != nil {
		t.Fatal(err)
	}
	withSrc, err := store.Conditions(ctx, oct, year, 10)
	if err != nil || len(withSrc) != 1 {
		t.Fatalf("условия октября: %v %+v", err, withSrc)
	}
	if withSrc[0].ShareSources[BCostAgent] != "strategy:2098-09" {
		t.Fatalf("источник «из стратегии» не сохранился: %+v", withSrc[0].ShareSources)
	}
	if _, ok := withSrc[0].ShareSources[BCostFreight]; ok {
		t.Fatalf("произвольный источник должен сбрасываться в «вручную»: %+v", withSrc[0].ShareSources)
	}
	novCard, err := cards.EnsureCard(ctx, Card{PlID: nov, FormCode: TemplateMP, ScopeKey: "large", Title: "test nov", Currency: "RUB"})
	if err != nil {
		t.Fatal(err)
	}
	if n, err := svc.CopyConditionsFromPrev(ctx, novCard.ID, admin); err != nil || n == 0 {
		t.Fatalf("копирование условий в ноябрь: %d %v", n, err)
	}
	novList, err := store.Conditions(ctx, nov, year, 11)
	if err != nil || len(novList) != 1 || novList[0].ShareSources[BCostAgent] != "strategy:2098-09" {
		t.Fatalf("источник доли потерялся при копировании: %v %+v", err, novList)
	}
}
