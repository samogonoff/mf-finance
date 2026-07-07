package plans

import (
	"context"
	"database/sql"
)

// FactRow — read-only факт МП по площадке/статье/периоду (лист «Источник_МП»).
// Валюта в MVP — RUB (значения как в прототипе Маркетплейсы_large); тривалютность
// BYN/RUB/USD и пересчёт через dir_fx_rate приходят в VS7. См. SPEC §10.1.
type FactRow struct {
	CodeCFO  int     `json:"code_cfo"`
	NameCFO  string  `json:"name_cfo"`
	CodePL   int     `json:"code_pl"`
	Year     int     `json:"year"`
	Month    int     `json:"month"`
	Scenario string  `json:"scenario"` // "Факт"
	Currency string  `json:"currency"` // "RUB" (MVP)
	Amount   float64 `json:"amount"`
}

// MpFactSource — источник факта/стратегии МП. PLANS_MOCK=1 → mock; иначе online FinDWH.
type MpFactSource interface {
	MpFact(ctx context.Context, year, month int, segment string) ([]FactRow, error)
	// MpStrategy — план/стратегия (read-only колонка формы). Может быть пустым.
	MpStrategy(ctx context.Context, year, month int, segment string) ([]FactRow, error)
}

// NewMpFactSource выбирает источник: mock при mock=true или отсутствии MSSQL,
// иначе online-коннектор к FinDWH (тот же *sql.DB, что у ВГО-отчёта).
func NewMpFactSource(mock bool, db *sql.DB, factTable, planTable, penaltyView string) MpFactSource {
	if mock || db == nil {
		return NewMockFactSource()
	}
	return NewOlapFactSource(db, factTable, planTable, penaltyView)
}

// segmentGroup — соответствие сегмента группе в источнике (Group_МП_new).
func segmentGroup(segment string) (string, bool) {
	switch segment {
	case "large":
		return "Маркетплейсы_large", true
	case "small":
		return "Маркетплейсы_small", true
	default:
		return "", false
	}
}

// segmentPlatforms — площадки сегмента (code_cfo → name_cfo) из seed dir_marketplace.
func segmentPlatforms(segment string) map[int]string {
	out := map[int]string{}
	for _, m := range MarketplaceSeed() {
		if m.Segment == segment {
			out[m.CodeCFO] = m.NameCFO
		}
	}
	return out
}
