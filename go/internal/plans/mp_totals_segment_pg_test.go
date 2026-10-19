package plans

import (
	"context"
	"math"
	"os"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

func TestMpTotalFor(t *testing.T) {
	both := map[string]bool{"large": true, "small": true}
	cases := []struct {
		name  string
		by    map[string]float64
		segs  map[string]bool
		want  float64
		found bool
	}{
		{"сумма сегментов", map[string]float64{"large": 1000, "small": 300}, both, 1300, true},
		{"только крупные", map[string]float64{"large": 1000, "small": 300}, map[string]bool{"large": true}, 1000, true},
		{"старая строка не считается дважды", map[string]float64{"large": 1000, "": 50}, both, 1000, true},
		{"нет своих строк — берётся общая", map[string]float64{"": 50}, both, 50, true},
		{"у сегмента нет строки — общая", map[string]float64{"small": 300, "": 50}, map[string]bool{"large": true}, 50, true},
		{"пусто", map[string]float64{}, both, 0, false},
	}
	for _, c := range cases {
		got, ok := mpTotalFor(c.by, c.segs)
		if ok != c.found || math.Abs(got-c.want) > 1e-9 {
			t.Errorf("%s: got %v/%v, want %v/%v", c.name, got, ok, c.want, c.found)
		}
	}
}

func TestMpTotalsSegmentFor(t *testing.T) {
	segOf := map[int]string{335: "large", 953: "small"}
	if got := mpTotalsSegmentFor("small", segOf, []int{335, 953}); got != "small" {
		t.Errorf("разрез small у смешанного задания: %q", got)
	}
	// Чужой разрез (у задания нет площадок мелких МП) не принимается: итог остаётся в своём сегменте.
	if got := mpTotalsSegmentFor("small", segOf, []int{335}); got != "large" {
		t.Errorf("чужой разрез small у задания на крупные: %q, want large", got)
	}
	if got := mpTotalsSegmentFor("", segOf, []int{335, 953}); got != "" {
		t.Errorf("без разреза смешанное задание — общий итог, got %q", got)
	}
	if got := mpTotalsSegmentFor("", segOf, []int{335}); got != "large" {
		t.Errorf("однородное задание: %q", got)
	}
}

// Итоговые строки скидок/уценок у крупных и мелких МП свои: сводка P&L суммирует
// их, а не берёт максимум, строка без сегмента не считается дважды, и свод
// (boardTactic) не зависит от порядка строк.
func TestPnlAndBoardTotalsBySegmentPG(t *testing.T) {
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

	const year = 2097
	cleanup := func() {
		var id int64
		if pool.QueryRow(ctx, `SELECT id FROM pl_instance WHERE period_year=$1 AND period_month=3`, year).Scan(&id) == nil {
			_, _ = pool.Exec(ctx, `DELETE FROM pl_instance WHERE id=$1`, id)
		}
	}
	cleanup()
	defer cleanup()

	metrics := NewPgStore(pool)
	pl, err := metrics.EnsureInstance(ctx, year, 3)
	if err != nil {
		t.Fatal(err)
	}
	line := mpLineByBlock()[BDiscount].CodePL
	put := func(seg string, amount float64) {
		t.Helper()
		if _, err := pool.Exec(ctx, `
			INSERT INTO pl_metric (pl_id, template_code, segment, line_code, block_type, profit_center, country, scenario, period_year, period_month, currency, amount, is_manual)
			VALUES ($1,'TPL-MP',$2,$3,$4,0,'',$5,$6,3,'RUB',$7,false)`,
			pl, seg, line, BDiscount, ScenarioTactic, year, amount); err != nil {
			t.Fatal(err)
		}
	}
	put("large", 1_000_000)
	put("small", 300_000)
	put("", 50) // сохранено до разделения по сегментам

	store := NewTaskStore(pool, nil)
	sum, err := store.Pnl(ctx, pl)
	if err != nil {
		t.Fatal(err)
	}
	var got *float64
	for _, r := range sum.Rows {
		if r.CodeCFO == 0 && r.BlockType == BDiscount {
			got = r.Tactic
		}
	}
	if got == nil || math.Abs(*got-1_300_000) > 1e-6 {
		t.Fatalf("P&L: скидка = %v, want 1300000 (сумма крупных и мелких, без старой строки)", got)
	}

	_, totals, _, _ := store.boardTactic(ctx, pl, "RUB")
	both := map[string]bool{"large": true, "small": true}
	if v, ok := mpTotalFor(totals[BDiscount], both); !ok || math.Abs(v-1_300_000) > 1e-6 {
		t.Fatalf("свод: скидка = %v/%v, want 1300000", v, ok)
	}
	if v, ok := mpTotalFor(totals[BDiscount], map[string]bool{"small": true}); !ok || math.Abs(v-300_000) > 1e-6 {
		t.Fatalf("свод по мелким: скидка = %v/%v, want 300000", v, ok)
	}
}