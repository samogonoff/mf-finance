package plans

import "testing"

// Группы розницы — как на листе «Розница» Excel: каждый код в одной группе, порядок групп задан,
// НСИ старше умолчаний, прочие виды форм умолчаний не получают.
func TestExpRetailPnlGroups(t *testing.T) {
	seen := map[int]string{}
	for g, codes := range expRetailPnlCodes {
		for _, c := range codes {
			if prev, dup := seen[c]; dup {
				t.Fatalf("код %d и в «%s», и в «%s»", c, prev, g)
			}
			seen[c] = g
		}
	}
	if len(expRetailPnlOrder) != len(expRetailPnlCodes) {
		t.Fatalf("в порядке %d групп, в составе %d", len(expRetailPnlOrder), len(expRetailPnlCodes))
	}
	for _, g := range expRetailPnlOrder {
		if len(expRetailPnlCodes[g]) == 0 {
			t.Fatalf("группа «%s» из порядка без кодов", g)
		}
	}
	if seen[24] != "Заработная плата" || seen[53] != "Охрана и страхование" || seen[4004] != "Прочие доходы и расходы" {
		t.Fatalf("маппинг: %v %v %v", seen[24], seen[53], seen[4004])
	}

	// НСИ перекрывает умолчание; для не-розницы умолчаний нет.
	got := expApplyDefaultPnlGroups(ExpKindRetail, map[int]string{24: "Моя группа"})
	if got[24] != "Моя группа" || got[27] != "Прочие расходы на персонал" {
		t.Fatalf("НСИ должен быть старше умолчаний: %v / %v", got[24], got[27])
	}
	if mp := expApplyDefaultPnlGroups(ExpKindMP, map[int]string{}); len(mp) != 0 {
		t.Fatalf("для МП умолчаний быть не должно: %v", mp)
	}
}

func TestExpFoldToPnlGroupsOrdered(t *testing.T) {
	mk := func(group string, code int) ExpSummaryRow {
		return ExpSummaryRow{CodePL: code, PnlGroup: group, ByScenario: map[string]map[int]float64{ExpScenarioPlan: {1: 10}}}
	}
	rows := []ExpSummaryRow{
		mk("Налоги", 57), mk(ExpSummaryUnassigned, 999), mk("Аренда", 54), mk("Чужая группа", 5), mk("Заработная плата", 24),
	}
	out := ExpFoldToPnlGroupsOrdered(rows, expRetailPnlOrder)
	var names []string
	for _, g := range out {
		names = append(names, g.PnlGroup)
	}
	want := []string{"Заработная плата", "Аренда", "Налоги", "Чужая группа", ExpSummaryUnassigned}
	for i := range want {
		if names[i] != want[i] {
			t.Fatalf("порядок групп: %v, want %v", names, want)
		}
	}
	// Без порядка — прежнее поведение: алфавит, «Не распределено» последней.
	plain := ExpFoldToPnlGroups(rows)
	if plain[len(plain)-1].PnlGroup != ExpSummaryUnassigned {
		t.Fatalf("«Не распределено» должна быть последней: %v", plain)
	}
}
