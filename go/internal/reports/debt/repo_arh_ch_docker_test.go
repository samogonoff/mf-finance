package debt

import (
	"context"
	"os"
	"testing"
)

// TestArhDockerSmoke exercises the generated report SQL against a real ClickHouse.
// It is opt-in so the regular unit suite stays independent of local infrastructure.
func TestArhDockerSmoke(t *testing.T) {
	url := os.Getenv("ARH_TEST_CLICKHOUSE_URL")
	if url == "" {
		t.Skip("ARH_TEST_CLICKHOUSE_URL is not set")
	}
	repo, err := NewArhCHRepo(url, os.Getenv("ARH_TEST_CLICKHOUSE_USER"), os.Getenv("ARH_TEST_CLICKHOUSE_PASSWORD"))
	if err != nil {
		t.Fatal(err)
	}

	for _, tc := range []struct {
		name string
		inn  string
	}{
		{"RB", "690591512"},
		{"RF", "6950135110"},
		{"PTIR", "9731039708"},
		{"KZ", "141240004842"},
		{"UZ", "305554644"},
	} {
		for _, lens := range []string{LensContract, LensBYN, LensUSD} {
			t.Run(tc.name+"/"+lens, func(t *testing.T) {
				rows, err := repo.Report(context.Background(), Filters{
					DateFrom:   date(2026, 9, 30),
					DateTo:     date(2026, 9, 30),
					EntityINNs: []string{tc.inn},
					Lens:       lens,
				})
				if err != nil {
					t.Fatal(err)
				}
				if len(rows) == 0 {
					t.Fatal("report returned no rows")
				}
				var dz, kz float64
				for _, row := range rows {
					dz += row.ClosingDZ
					kz += row.ClosingKZ
				}
				t.Logf("rows=%d DZ=%.2f KZ=%.2f", len(rows), dz, kz)
			})
		}
	}

	// PTIR has a real zero VGO balance after 2026-10-14. The ETL heartbeat row must
	// keep the company snapshot fresh while remaining absent from report amounts.
	t.Run("PTIR/zero_balance_snapshot", func(t *testing.T) {
		rows, err := repo.Report(context.Background(), Filters{
			DateFrom:   date(2026, 10, 21),
			DateTo:     date(2026, 10, 21),
			EntityINNs: []string{"9731039708"},
			Lens:       LensContract,
		})
		if err != nil {
			t.Fatal(err)
		}
		for _, row := range rows {
			if row.ClosingDZ != 0 || row.ClosingKZ != 0 {
				t.Fatalf("unexpected PTIR debt on zero snapshot: %+v", row)
			}
		}
	})
}
