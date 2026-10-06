package plans

import (
	"context"
	"encoding/json"
	"os"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

func TestMpConditionsPeriodAndSnapshotPG(t *testing.T) {
	dsn := os.Getenv("PLANS_TEST_PG_DSN")
	if dsn == "" {
		t.Skip("PLANS_TEST_PG_DSN не задан")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	pool, err := pgxpool.New(ctx, dsn)
	if err != nil {
		t.Fatal(err)
	}
	defer pool.Close()
	metrics := NewPgStore(pool)
	conditions := NewMpConditionsStore(pool)
	for _, month := range []int{11, 12} {
		var id int64
		if pool.QueryRow(ctx, `SELECT id FROM pl_instance WHERE period_year=2099 AND period_month=$1`, month).Scan(&id) == nil {
			_, _ = pool.Exec(ctx, `DELETE FROM pl_instance WHERE id=$1`, id)
		}
	}
	defer func() {
		for _, month := range []int{11, 12} {
			var id int64
			if pool.QueryRow(ctx, `SELECT id FROM pl_instance WHERE period_year=2099 AND period_month=$1`, month).Scan(&id) == nil {
				_, _ = pool.Exec(ctx, `DELETE FROM pl_instance WHERE id=$1`, id)
			}
		}
	}()
	nov, err := metrics.EnsureInstance(ctx, 2099, 11)
	if err != nil {
		t.Fatal(err)
	}
	dec, err := metrics.EnsureInstance(ctx, 2099, 12)
	if err != nil {
		t.Fatal(err)
	}
	base := MpConditions{PlID: nov, CodeCFO: 335, Year: 2099, Month: 11,
		ValidFrom: "2099-11-01", SppPct: .1, MarkupPct: .5, MarkupTotalPct: .2,
		Shares: map[string]float64{BCostAds: 0}, Currency: "RUB"}
	if _, err := conditions.SaveConditions(ctx, base); err != nil {
		t.Fatal(err)
	}
	list, err := conditions.Conditions(ctx, dec, 2099, 12)
	if err != nil || len(list) != 1 {
		t.Fatalf("условия декабря: %v %+v", err, list)
	}
	if share, present := list[0].Shares[BCostAds]; !present || share != 0 {
		t.Fatalf("нулевая доля должна отличаться от отсутствующей: %+v", list[0].Shares)
	}
	if _, present := list[0].Shares[BCostAgent]; present {
		t.Fatal("отсутствующая доля появилась")
	}
	inverse, _ := computeMpPlatformInverse(MpInverseInput{SalesManagerGross: 120, Conditions: conditionsToInput(list[0], vatBasisFromRate(.2))})
	if _, present := inverse[BCostAds]; !present {
		t.Fatal("заданная нулевая доля должна дать нулевую статью")
	}
	if _, present := inverse[BCostAgent]; present {
		t.Fatal("отсутствующая доля должна оставить статью пустой")
	}

	card, err := NewCardStore(pool).EnsureCard(ctx, Card{PlID: nov, FormCode: TemplateMP, ScopeKey: "large", Title: "test", Currency: "RUB"})
	if err != nil {
		t.Fatal(err)
	}
	provider := NewMpCardProvider(metrics, nil).WithCommonCosts(conditions)
	snapshot, err := provider.FormSnapshot(ctx, card)
	if err != nil {
		t.Fatal(err)
	}
	raw, err := json.Marshal(snapshot)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, `INSERT INTO card_version(card_id,version_no,payload) VALUES($1,1,$2)`, card.ID, raw); err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, `UPDATE form_card SET status='approved',locked=true,current_version=1 WHERE id=$1`, card.ID); err != nil {
		t.Fatal(err)
	}
	card.Status, card.Locked, card.CurrentVersion = "approved", true, 1
	changed := base
	changed.Shares = map[string]float64{BCostAds: .2}
	if _, err := conditions.SaveConditions(ctx, changed); err != nil {
		t.Fatal(err)
	}
	frozen, _ := (&TaskStore{pool: pool}).mpClosedSnapshot(ctx, card, "RUB")
	if frozen[335].Shares[BCostAds] != 0 {
		t.Fatalf("закрытая версия изменилась: %+v", frozen[335].Shares)
	}

	// Строка будущего периода с датой начала в ноябре не меняет ноябрь.
	retro := changed
	retro.PlID, retro.Year, retro.Month = dec, 2099, 12
	retro.ValidFrom = "2099-11-15"
	retro.Shares = map[string]float64{BCostAds: .3}
	if _, err := conditions.SaveConditions(ctx, retro); err != nil {
		t.Fatal(err)
	}
	novRows, err := conditions.Conditions(ctx, nov, 2099, 11)
	if err != nil || len(novRows) != 1 || novRows[0].Shares[BCostAds] != .2 {
		t.Fatalf("будущая правка попала в ноябрь: %v %+v", err, novRows)
	}

	// Старая сумма без доли исчезает при пересчёте.
	if err := metrics.UpsertMetrics(ctx, dec, "large", []MetricRow{{LineCode: 64, BlockType: BCostAgent, ProfitCenter: 335,
		Scenario: ScenarioTactic, Year: 2099, Month: 12, Currency: "RUB", Amount: 99}}); err != nil {
		t.Fatal(err)
	}
	svc := &MpConditionsService{metrics: metrics}
	if err := svc.persist(ctx, Card{PlID: dec, Year: 2099, Month: 12, Currency: "RUB"}, map[int]map[string]float64{335: inverse}, 0); err != nil {
		t.Fatal(err)
	}
	var count int
	if err := pool.QueryRow(ctx, `SELECT count(*) FROM pl_metric WHERE pl_id=$1 AND profit_center=335 AND block_type=$2`, dec, BCostAgent).Scan(&count); err != nil {
		t.Fatal(err)
	}
	if count != 0 {
		t.Fatalf("отсутствующая доля оставила %d старых сумм", count)
	}
}
