package plans

import (
	"context"
	"math"
	"testing"
	"time"
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

// Колонки факта следуют за окном свода: «месяц» — три колонки за один месяц; «месяц–декабрь» и «год» — факт того же
// периода прошлого года, факт месяца перед началом окна и факт месяцев окна текущего года (по тем, где факт уже есть).
func TestBuildExpConsolidated_FactFollowsWindow(t *testing.T) {
	r := consRow(24, "Заработная плата",
		// факт прошлого года по месяцам
		ExpValue{Scenario: ExpScenarioFact, Month: 1, Amount: consPtr(10)},
		ExpValue{Scenario: ExpScenarioFact, Month: 8, Amount: consPtr(80)},
		ExpValue{Scenario: ExpScenarioFact, Month: 9, Amount: consPtr(90)},
		ExpValue{Scenario: ExpScenarioFact, Month: 12, Amount: consPtr(120)},
		// факт текущего года: по август включительно (сентябрь — месяц периода, факт идёт)
		ExpValue{Scenario: ExpScenarioFactCur, Month: 1, Amount: consPtr(1)},
		ExpValue{Scenario: ExpScenarioFactCur, Month: 7, Amount: consPtr(7)},
		ExpValue{Scenario: ExpScenarioFactCur, Month: 8, Amount: consPtr(8)},
		ExpValue{Scenario: ExpScenarioFactCur, Month: 9, Amount: consPtr(9)},
	)
	in := []expConsInput{{Country: "BY", Card: Card{ID: 1}, Inst: ExpInstance{ID: 1, Currency: "BYN"}, Rows: []ExpRow{r}, Fx: sameExpFx(1)}}
	pnl := expApplyDefaultPnlGroups(ExpKindRetail, nil)
	const year, month = 2026, 9 // период сентябрь 2026

	cases := []struct {
		window        string
		ly, prev, cur float64
		from, to      int
	}{
		// месяц: сентябрь 2025 (90), август 2026 (8), сентябрь 2026 (9)
		{ExpWindowMonth, 90, 8, 9, 9, 9},
		// сентябрь–декабрь: сентябрь–декабрь 2025 (90 + 120), август 2026 (8), сентябрь–декабрь 2026 (9)
		{ExpWindowTail, 210, 8, 9, 9, 12},
		// год: весь 2025 (10 + 80 + 90 + 120), декабрь 2025 (120), весь 2026 по имеющимся месяцам (1 + 7 + 8 + 9)
		{ExpWindowYear, 300, 120, 25, 1, 12},
	}
	for _, c := range cases {
		out := buildExpConsolidated(7, year, month, c.window, in, pnl, expRetailPnlOrder)
		if out.FromMonth != c.from || out.ToMonth != c.to {
			t.Errorf("%s: окно %d–%d, ждали %d–%d", c.window, out.FromMonth, out.ToMonth, c.from, c.to)
		}
		if !consNear(out.Total.FactLY, c.ly) || !consNear(out.Total.FactPrev, c.prev) || !consNear(out.Total.FactCur, c.cur) {
			t.Errorf("%s: факт LY=%v prev=%v cur=%v, ждали %v / %v / %v", c.window,
				out.Total.FactLY, out.Total.FactPrev, out.Total.FactCur, c.ly, c.prev, c.cur)
		}
	}
}

// «Прошлый месяц» колонки следует за окном, и курс у него тоже: для окна «весь год» это декабрь прошлого года, а не месяц
// перед карточкой; для «месяца» и «до конца года» курс прежний (месяц перед карточкой).
func TestExpFx_FactPrevRateFollowsWindowStart(t *testing.T) {
	b := NewRateBook(nil)
	b.mu.Lock()
	b.loadedAt = time.Now()
	b.vat = VatSeed()
	b.fx = []FxRow{
		{Currency: "RUB", Year: 2025, Month: 12, RateBYN: 0.03},
		{Currency: "RUB", Year: 2026, Month: 9, RateBYN: 0.04},
		{Currency: "RUB", Year: 2026, Month: 10, RateBYN: 0.05},
	}
	b.mu.Unlock()
	svc := &ExpService{rates: b}
	ctx := context.Background()

	// Карточка октября 2026.
	for _, c := range []struct {
		name       string
		from       int
		prev, tact float64
	}{
		{"месяц", 10, 0.04, 0.04},         // прошлый месяц — сентябрь
		{"до конца года", 10, 0.04, 0.04}, // то же начало окна
		{"весь год", 1, 0.03, 0.04},       // декабрь 2025; курс плана — по-прежнему месяц перед карточкой
	} {
		fx := svc.expFx(ctx, "RUB", 2026, 10, c.from)
		if fx.NoRate || !consNear(fx.FactPrev, c.prev) || !consNear(fx.Tactic, c.tact) {
			t.Fatalf("%s: FactPrev=%v Tactic=%v NoRate=%v, ждали %v / %v", c.name, fx.FactPrev, fx.Tactic, fx.NoRate, c.prev, c.tact)
		}
	}
}
