package plans

import (
	"context"
	"sort"
)

// Фикстуры факта МП (PLANS_MOCK=1). Значения — из прототипа
// var/plan/Тактический план_large_МП_2026_июнь.xlsx, лист «Маркетплейсы_large»,
// блок code_pl=1046 («ПРОДАЖИ по ценам менеджера с НДС»), факт 2026 помесячно
// (колонки Y..AC), RUB. Контрольная точка CHECKPOINT A:
// WB(335)/1046/2026-05 = 357034569.85. Остальные code_pl/валюты — VS7/далее.

type mpFactKey struct {
	codeCFO, codePL, year, month int
}

// mockMpFact — факт 1046 по large-площадкам, янв–май 2026 (RUB).
var mockMpFact = map[mpFactKey]float64{
	// Wildberries 335
	{335, 1046, 2026, 1}: 349675098.04, {335, 1046, 2026, 2}: 273149073.39,
	{335, 1046, 2026, 3}: 415204827.44, {335, 1046, 2026, 4}: 352003891.41,
	{335, 1046, 2026, 5}: 357034569.85451651,
	// Lamoda 336
	{336, 1046, 2026, 1}: 103441703.01, {336, 1046, 2026, 2}: 116840577.15,
	{336, 1046, 2026, 3}: 122407794.33, {336, 1046, 2026, 4}: 120897634.35,
	{336, 1046, 2026, 5}: 134132603.07,
	// Ozon 337
	{337, 1046, 2026, 1}: 155183912.60, {337, 1046, 2026, 2}: 173629025.55,
	{337, 1046, 2026, 3}: 169806771.43, {337, 1046, 2026, 4}: 177801511.13,
	{337, 1046, 2026, 5}: 272341903.37,
	// Yandex Market 954
	{954, 1046, 2026, 1}: 7507116.53, {954, 1046, 2026, 2}: 8783300.34,
	{954, 1046, 2026, 3}: 8875922.47, {954, 1046, 2026, 4}: 7957978.43,
	{954, 1046, 2026, 5}: 21179713.09,
}

// MockFactSource отдаёт факт из фикстур, без БД.
type MockFactSource struct{}

// NewMockFactSource — конструктор.
func NewMockFactSource() *MockFactSource { return &MockFactSource{} }

// MpFact — факт сегмента за период из фикстур. Неизвестный сегмент → пусто.
func (s *MockFactSource) MpFact(_ context.Context, year, month int, segment string) ([]FactRow, error) {
	platforms := segmentPlatforms(segment)
	if len(platforms) == 0 {
		return []FactRow{}, nil
	}
	out := make([]FactRow, 0, len(platforms))
	for key, amount := range mockMpFact {
		if key.year != year || key.month != month {
			continue
		}
		name, ok := platforms[key.codeCFO]
		if !ok {
			continue
		}
		out = append(out, FactRow{
			CodeCFO:  key.codeCFO,
			NameCFO:  name,
			CodePL:   key.codePL,
			Year:     year,
			Month:    month,
			Scenario: "Факт",
			Currency: "RUB",
			Amount:   amount,
		})
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].CodeCFO != out[j].CodeCFO {
			return out[i].CodeCFO < out[j].CodeCFO
		}
		return out[i].CodePL < out[j].CodePL
	})
	return out, nil
}
