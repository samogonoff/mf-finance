package plans

import (
	"context"
	"fmt"
)

// Слои read-only сценариев формы и свода МП, приведённые к валюте отображения:
//
//	fact     — факт периода (Budgeting.dbo.FormToLoadFact + штрафы FINDWH)
//	factPrev — факт того же месяца ПРОШЛОГО года («сравнение с фактом 25», SPEC §9.6)
//	strategy — стратегия/план (FormToLoadPlan)
//	target   — тактика-таргет (FormToLoaTaktTarget)
//
// fact/factPrev индексируются по (code_cfo, code_pl) — так их читает factSeed,
// который склеивает 2006+6006 в COGS и разводит штрафы. strategy/target уже
// свёрнуты в block_type (несколько code_pl могут лечь в одну строку формы).
//
// Источники отдают BYN (штрафы — RUB), поэтому каждая сумма проходит через
// convertAmount в валюту формы. Сбой любого источника не роняет остальные —
// слой просто остаётся пустым (колонка в UI покажет «—»).
type mpLayers struct {
	fact     map[[2]int]float64
	factPrev map[[2]int]float64
	strategy map[string]float64
	target   map[string]float64
}

func newMpLayers() mpLayers {
	return mpLayers{
		fact:     map[[2]int]float64{},
		factPrev: map[[2]int]float64{},
		strategy: map[string]float64{},
		target:   map[string]float64{},
	}
}

// layerKey — ключ строки в слоях, свёрнутых по block_type.
func layerKey(cfo int, block string) string { return fmt.Sprintf("%d:%s", cfo, block) }

// mpSourceLayers читает все read-only сценарии сегмента за период.
// allowed == nil — без фильтра по площадкам.
func (s *TaskStore) mpSourceLayers(ctx context.Context, year, month int, segment string, allowed map[int]bool, currency string) mpLayers {
	l := newMpLayers()
	if s.fact == nil || segment == "" {
		return l
	}
	pass := func(cfo int) bool { return allowed == nil || allowed[cfo] }
	byPL := func(rows []FactRow, dst map[[2]int]float64) {
		for _, r := range rows {
			if pass(r.CodeCFO) {
				dst[[2]int{r.CodeCFO, r.CodePL}] += convertAmount(r.Amount, r.Currency, currency)
			}
		}
	}
	byBlock := func(rows []FactRow, dst map[string]float64) {
		pb := plToBlock()
		for _, r := range rows {
			bt := pb[r.CodePL]
			if bt == "" || !pass(r.CodeCFO) {
				continue
			}
			// COGS осн(2006)+пошив(6006) складываются в одну строку формы.
			dst[layerKey(r.CodeCFO, bt)] += convertAmount(r.Amount, r.Currency, currency)
		}
	}
	if rows, err := s.fact.MpFact(ctx, year, month, segment); err == nil {
		byPL(rows, l.fact)
	}
	if rows, err := s.fact.MpFact(ctx, year-1, month, segment); err == nil {
		byPL(rows, l.factPrev)
	}
	if rows, err := s.fact.MpStrategy(ctx, year, month, segment); err == nil {
		byBlock(rows, l.strategy)
	}
	if rows, err := s.fact.MpTaktTarget(ctx, year, month, segment); err == nil {
		byBlock(rows, l.target)
	}
	return l
}
