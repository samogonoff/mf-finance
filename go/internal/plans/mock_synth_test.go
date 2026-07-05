package plans

import "testing"

func TestMockFact_SynthesizesFullCascadeLines(t *testing.T) {
	rows, err := NewMockFactSource().MpFact(nil, 2026, 5, "large")
	if err != nil {
		t.Fatal(err)
	}
	idx := map[[2]int]bool{}
	for _, r := range rows {
		idx[[2]int{r.CodeCFO, r.CodePL}] = true
	}
	// WB(335): 1046 (база) + производные + статьи затрат.
	for _, pl := range []int{1046, 1045, 1022, 1006, 8006, 2006, 6006, 64, 51, 52, 54, 13, 15, 45, 58, 48, 66, 6600} {
		if !idx[[2]int{335, pl}] {
			t.Errorf("нет строки WB code_pl=%d", pl)
		}
	}
}
