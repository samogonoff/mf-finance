package plans

import (
	"math"
	"testing"
)

func consPtr(v float64) *float64 { return &v }

func consRow(code int, expense string, vals ...ExpValue) ExpRow {
	return ExpRow{CodeCFO: 100, CodePL: code, Expense: expense, Values: vals}
}

func consNear(a, b float64) bool { return math.Abs(a-b) < 1e-6 }

// Свод расходов розницы: окно плана, пересчёт в BYN курсами по назначению, факт трёх периодов,
// группы Excel в порядке листа, «Не распределено» — последней.
func TestBuildExpConsolidated(t *testing.T) {
	by := consRow(24, "Заработная плата",
		ExpValue{Scenario: ExpScenarioPlan, Month: 10, Amount: consPtr(999)}, // раньше окна — не входит
		ExpValue{Scenario: ExpScenarioPlan, Month: 11, Amount: consPtr(100)},
		ExpValue{Scenario: ExpScenarioPlan, Month: 12, Amount: consPtr(50)},
		ExpValue{Scenario: ExpScenarioFact, Month: 11, Amount: consPtr(80)},    // тот же месяц прошлого года
		ExpValue{Scenario: ExpScenarioFactCur, Month: 10, Amount: consPtr(70)}, // прошлый месяц
		ExpValue{Scenario: ExpScenarioFactCur, Month: 11, Amount: consPtr(10)}, // текущий месяц
	)
	ru := consRow(24, "Заработная плата", ExpValue{Scenario: ExpScenarioPlan, Month: 11, Amount: consPtr(1000)}, ExpValue{Scenario: ExpScenarioFact, Month: 11, Amount: consPtr(2000)})
	unk := consRow(7777, "Новая статья", ExpValue{Scenario: ExpScenarioPlan, Month: 11, Amount: consPtr(5)})
	rent := consRow(54, "Аренда помещений", ExpValue{Scenario: ExpScenarioPlan, Month: 12, Amount: consPtr(20)})
	in := []expConsInput{
		{Country: "BY", Card: Card{ID: 1, Title: "Розница РБ"}, Inst: ExpInstance{ID: 1, Currency: "BYN"}, Rows: []ExpRow{by, unk, rent}, Fx: sameExpFx(1)},
		{Country: "RU", Card: Card{ID: 2}, Inst: ExpInstance{ID: 2, Currency: "RUB"}, Rows: []ExpRow{ru}, Fx: expFxToBYN{Tactic: 0.04, FactLY: 0.03, FactPrev: 0.04, FactCur: 0.04}},
	}
	pnl := expApplyDefaultPnlGroups(ExpKindRetail, nil)

	tail := buildExpConsolidated(7, 2026, 11, ExpWindowTail, in, pnl, expRetailPnlOrder)
	if tail.FromMonth != 11 || tail.ToMonth != 12 || tail.Window != ExpWindowTail || tail.Currency != "BYN" {
		t.Fatalf("окно: %+v", tail)
	}
	// Порядок групп: по Excel, неизвестная — последней.
	var names []string
	for _, g := range tail.Groups {
		names = append(names, g.Group)
	}
	wantOrder := []string{"Заработная плата", "Аренда", ExpSummaryUnassigned}
	for i, w := range wantOrder {
		if names[i] != w {
			t.Fatalf("порядок групп %v, ждали %v", names, wantOrder)
		}
	}
	zp := tail.Groups[0]
	if !consNear(zp.Plan["BY"], 150) || !consNear(zp.Plan["RU"], 1000*0.04) || !consNear(zp.PlanTotal, 150+40) {
		t.Fatalf("план ЗП: %+v", zp.ExpConsAmounts)
	}
	if !consNear(zp.FactLY, 80+2000*0.03) || !consNear(zp.FactPrev, 70) || !consNear(zp.FactCur, 10) {
		t.Fatalf("факт ЗП: LY=%v prev=%v cur=%v", zp.FactLY, zp.FactPrev, zp.FactCur)
	}
	if len(zp.Lines) != 1 || zp.Lines[0].CodePL != 24 || zp.Lines[0].Expense != "Заработная плата" {
		t.Fatalf("статьи группы: %+v", zp.Lines)
	}
	if !consNear(tail.Total.PlanTotal, 150+40+20+5) {
		t.Fatalf("итог плана: %v", tail.Total.PlanTotal)
	}
	if !consNear(tail.Countries[1].Plan, 40) || tail.Countries[1].Rate != 0.04 {
		t.Fatalf("строка страны: %+v", tail.Countries[1])
	}

	// Окно «месяц»: декабрь не входит; «год»: октябрь входит.
	if m := buildExpConsolidated(7, 2026, 11, ExpWindowMonth, in, pnl, expRetailPnlOrder); !consNear(m.Groups[0].Plan["BY"], 100) {
		t.Fatalf("окно month: %v", m.Groups[0].Plan)
	}
	if y := buildExpConsolidated(7, 2026, 11, ExpWindowYear, in, pnl, nil); !consNear(y.Total.Plan["BY"], 999+100+50+20+5) {
		t.Fatalf("окно year: %v", y.Total.Plan)
	}

	// Январь: прошлый месяц — декабрь ПРОШЛОГО года из «факта прошлого года».
	jan := consRow(24, "Заработная плата", ExpValue{Scenario: ExpScenarioFact, Month: 12, Amount: consPtr(33)}, ExpValue{Scenario: ExpScenarioFactCur, Month: 12, Amount: consPtr(44)})
	j := buildExpConsolidated(7, 2026, 1, ExpWindowTail, []expConsInput{{Country: "BY", Card: Card{ID: 1}, Rows: []ExpRow{jan}, Fx: sameExpFx(1)}}, pnl, nil)
	if !consNear(j.Groups[0].FactPrev, 33) {
		t.Fatalf("январь: прошлый месяц = %v, ждали 33 (декабрь прошлого года)", j.Groups[0].FactPrev)
	}
}

func TestExpWindowMonths(t *testing.T) {
	for _, c := range []struct {
		w           string
		m, from, to int
		norm        string
	}{
		{"", 10, 10, 12, ExpWindowTail}, {"month", 10, 10, 10, ExpWindowMonth}, {"year", 10, 1, 12, ExpWindowYear},
		{"мусор", 3, 3, 12, ExpWindowTail}, {"tail", 0, 1, 12, ExpWindowTail},
	} {
		f, to, n := expWindowMonths(c.w, c.m)
		if f != c.from || to != c.to || n != c.norm {
			t.Errorf("%q/%d: %d-%d %s, ждали %d-%d %s", c.w, c.m, f, to, n, c.from, c.to, c.norm)
		}
	}
}

// Нет курса к BYN: сумма страны в нац. валюте не складывается с BYN (итог завышался бы в десятки и
// тысячи раз) — страна отмечена NoRate, а в итоги и группы не входит.
func TestBuildExpConsolidated_NoRateCountryExcluded(t *testing.T) {
	by := consRow(24, "Заработная плата", ExpValue{Scenario: ExpScenarioPlan, Month: 11, Amount: consPtr(100)},
		ExpValue{Scenario: ExpScenarioFact, Month: 11, Amount: consPtr(80)})
	uz := consRow(24, "Заработная плата", ExpValue{Scenario: ExpScenarioPlan, Month: 11, Amount: consPtr(5_000_000)},
		ExpValue{Scenario: ExpScenarioFact, Month: 11, Amount: consPtr(4_000_000)})
	in := []expConsInput{
		{Country: "BY", Card: Card{ID: 1}, Inst: ExpInstance{ID: 1, Currency: "BYN"}, Rows: []ExpRow{by}, Fx: sameExpFx(1)},
		{Country: "UZ", Card: Card{ID: 4}, Inst: ExpInstance{ID: 4, Currency: "UZS"}, Rows: []ExpRow{uz}, Fx: expFxToBYN{NoRate: true}},
	}
	got := buildExpConsolidated(7, 2026, 11, ExpWindowMonth, in, expApplyDefaultPnlGroups(ExpKindRetail, nil), expRetailPnlOrder)

	if !consNear(got.Total.PlanTotal, 100) || !consNear(got.Total.FactLY, 80) {
		t.Fatalf("итог без UZ: план %v, факт %v", got.Total.PlanTotal, got.Total.FactLY)
	}
	if _, ok := got.Groups[0].Plan["UZ"]; ok {
		t.Fatalf("сумм страны без курса в группе быть не должно: %v", got.Groups[0].Plan)
	}
	if !got.Countries[1].NoRate || got.Countries[0].NoRate {
		t.Fatalf("признак NoRate: %+v", got.Countries)
	}
	if len(got.Groups[0].Lines) != 1 {
		t.Fatalf("статья остаётся в списке: %+v", got.Groups[0].Lines)
	}
}