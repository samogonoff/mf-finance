package plans

import (
	"math"
	"testing"
)

// ТЗ доработок п. 3.2: итог свода — сумма стран в BYN, проценты — от агрегатов,
// а не среднее процентов стран.
func TestSumMetricsRatiosFromAggregates(t *testing.T) {
	a := RetailSummaryMetrics{Tactic: 110, Strategy: 100, FactPrevYear: 100}
	b := RetailSummaryMetrics{Tactic: 300, Strategy: 400, FactPrevYear: 200}
	tot := sumMetrics([]RetailSummaryMetrics{a, b})
	if tot.Tactic != 410 || tot.Strategy != 500 {
		t.Fatalf("суммы: %+v", tot)
	}
	if math.Abs(tot.TacticVsStrat-0.82) > 1e-9 {
		t.Errorf("тактика/стратегия %v, ждали 0.82 (410/500)", tot.TacticVsStrat)
	}
	if math.Abs(tot.LFL-(410.0/300-1)) > 1e-9 {
		t.Errorf("LFL %v", tot.LFL)
	}
}

// Строка страны в BYN пересчитана курсом, магазины отсортированы по тактике.
func TestConsolidateCountry(t *testing.T) {
	tac1, tac2, str := 100.0, 300.0, 200.0
	rows := []RetailRow{
		{CodeCFO: 1, NameCFO: "A", Indicators: RetailIndicators{Tactic: &tac1, Strategy: &str}},
		{CodeCFO: 2, NameCFO: "B", Indicators: RetailIndicators{Tactic: &tac2}},
	}
	nat, byn, stores := consolidateCountry(rows, RetailSummaryInput{}, SameRetailFx(0.5))
	if nat.Tactic != 400 || byn.Tactic != 200 {
		t.Fatalf("нац. %v, BYN %v", nat.Tactic, byn.Tactic)
	}
	if len(stores) != 2 || stores[0].CodeCFO != 2 || stores[0].Metrics.Tactic != 150 {
		t.Fatalf("магазины: %+v", stores)
	}
}

// ТЗ доработок п. 5.2: статусы — как в справочнике, с числом магазинов;
// строки-итоги источника не считаются.
func TestSummarizeLFLStatuses(t *testing.T) {
	stores := []RetailStore{
		{CodeCFO: 1, Country: "BY", LFLRaw: "LFL", LFLStatus: LFLYes},
		{CodeCFO: 2, Country: "RU", LFLRaw: "LFL", LFLStatus: LFLYes},
		{CodeCFO: 3, Country: "BY", LFLRaw: "реконструкция", LFLStatus: LFLReconstruction},
		{CodeCFO: 4, Country: "BY"},
	}
	got := summarizeLFLStatuses(stores)
	if len(got) != 3 || got[0].Raw != "LFL" || got[0].Stores != 2 || got[0].ByCountry["RU"] != 1 {
		t.Fatalf("статусы: %+v", got)
	}
	found := false
	for _, s := range got {
		if s.Raw == "реконструкция" && s.Canonical == LFLReconstruction {
			found = true
		}
	}
	if !found {
		t.Errorf("реконструкция должна быть в списке со своим кодом: %+v", got)
	}
}
