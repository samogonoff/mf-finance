package plans

import (
	"math"
	"testing"
)

// Сентябрь в витрине недогружен (как на 13.10.2026) — окно берёт июнь–август.
func TestMpSeedClosedMonthsSkipsIncomplete(t *testing.T) {
	cand := mpSeedCandidateMonths(2026, 11) // окт, сен, авг, июл, июн, май
	if cand[0] != [2]int{2026, 10} || len(cand) != mpSeedLookback {
		t.Fatalf("кандидаты: %v", cand)
	}
	rows := []MpArticleFactRow{
		{Year: 2026, Month: 9, Rows: 30},
		{Year: 2026, Month: 8, Rows: 100},
		{Year: 2026, Month: 7, Rows: 95},
		{Year: 2026, Month: 6, Rows: 90},
		{Year: 2026, Month: 5, Rows: 99},
	}
	got := mpSeedClosedMonths(cand, rows, 3)
	want := [][2]int{{2026, 8}, {2026, 7}, {2026, 6}}
	if len(got) != len(want) {
		t.Fatalf("окно %v, ждали %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("окно %v, ждали %v", got, want)
		}
	}
	if len(mpSeedClosedMonths(cand, nil, 3)) != 0 {
		t.Fatal("без факта окно должно быть пустым")
	}
}

// Доля — Σ статьи ÷ Σ|1006| за окно (взвешено по продажам), месяцы вне окна и
// чужие ЦФО не участвуют; сторно и агентское при нормативе отбрасываются с причиной.
func TestComputeSeedShares(t *testing.T) {
	months := [][2]int{{2026, 8}, {2026, 7}}
	rows := []MpArticleFactRow{
		{CodeCFO: 335, CodePL: 1006, Year: 2026, Month: 8, Amount: -3000},
		{CodeCFO: 335, CodePL: 1006, Year: 2026, Month: 7, Amount: -1000},
		{CodeCFO: 335, CodePL: 13, Year: 2026, Month: 8, Amount: 300},
		{CodeCFO: 335, CodePL: 13, Year: 2026, Month: 7, Amount: 100},
		{CodeCFO: 335, CodePL: 52, Year: 2026, Month: 8, Amount: -40},  // сторно
		{CodeCFO: 335, CodePL: 66, Year: 2026, Month: 8, Amount: -20},  // компенсация — можно
		{CodeCFO: 335, CodePL: 64, Year: 2026, Month: 8, Amount: 500},  // агентское
		{CodeCFO: 335, CodePL: 13, Year: 2026, Month: 6, Amount: 9999}, // вне окна
		{CodeCFO: 337, CodePL: 13, Year: 2026, Month: 8, Amount: 9999}, // чужой ЦФО
	}

	shares, base, dropped := computeSeedShares(rows, 335, months, 0)
	if base != 4000 {
		t.Fatalf("база %v, ждали 4000", base)
	}
	if math.Abs(shares[BCostAds]-0.1) > 1e-9 {
		t.Errorf("реклама %v, ждали 0.1", shares[BCostAds])
	}
	if math.Abs(shares[BCostOther]+0.005) > 1e-9 {
		t.Errorf("прочие %v, ждали −0.005", shares[BCostOther])
	}
	if math.Abs(shares[BCostAgent]-0.125) > 1e-9 {
		t.Errorf("агентское без норматива %v, ждали 0.125", shares[BCostAgent])
	}
	if _, ok := shares[BCostLogTransport]; ok || dropped[BCostLogTransport] == "" {
		t.Errorf("сторно 52 должно уйти в dropped: shares=%v dropped=%v", shares, dropped)
	}

	// При нормативе агентское — остаток норматива, из факта не берём.
	shares, _, dropped = computeSeedShares(rows, 335, months, 0.428)
	if _, ok := shares[BCostAgent]; ok || dropped[BCostAgent] == "" {
		t.Errorf("агентское при нормативе: shares=%v dropped=%v", shares, dropped)
	}

	if _, base, _ := computeSeedShares(rows, 954, months, 0); base != 0 {
		t.Errorf("площадка без продаж: база %v", base)
	}
}

func TestSeedWindowLabel(t *testing.T) {
	got := seedWindowLabel([]PeriodRef{{2026, 6}, {2026, 7}, {2026, 8}})
	if got != "06–08.2026" {
		t.Fatalf("метка %q", got)
	}
	if got := seedWindowLabel([]PeriodRef{{2025, 12}, {2026, 1}}); got != "12.2025–01.2026" {
		t.Fatalf("метка через год %q", got)
	}
}
