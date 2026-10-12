package plans

import (
	"context"
	"time"
)

// CfoExpenseProvider exposes only grid plan values for shared snapshots/publication.
type CfoExpenseProvider struct{ repo CfoExpenseRepo }

// RoutedCfoExpenseProvider keeps legacy 1.5 cards on their existing provider
// while routing CFO-31…962 cards to the v2 provider under the same form code.
type RoutedCfoExpenseProvider struct {
	legacy    SnapshotProvider
	cfo       SnapshotProvider
	legacyPub PublishSource
	cfoPub    PublishSource
}

func NewRoutedCfoExpenseProvider(legacy SnapshotProvider, legacyPub PublishSource, cfo SnapshotProvider, cfoPub PublishSource) *RoutedCfoExpenseProvider {
	return &RoutedCfoExpenseProvider{legacy: legacy, legacyPub: legacyPub, cfo: cfo, cfoPub: cfoPub}
}
func (p *RoutedCfoExpenseProvider) isCFO(c Card) bool {
	_, ok := CfoExpSpecForScope(c.ScopeKey)
	return ok
}
func (p *RoutedCfoExpenseProvider) FormSnapshot(ctx context.Context, c Card) (map[string]any, error) {
	if p.isCFO(c) {
		return p.cfo.FormSnapshot(ctx, c)
	}
	return p.legacy.FormSnapshot(ctx, c)
}
func (p *RoutedCfoExpenseProvider) PublishRows(ctx context.Context, c Card) ([]PublishRow, error) {
	if p.isCFO(c) {
		return p.cfoPub.PublishRows(ctx, c)
	}
	return p.legacyPub.PublishRows(ctx, c)
}

func NewCfoExpenseProvider(repo CfoExpenseRepo) *CfoExpenseProvider {
	return &CfoExpenseProvider{repo: repo}
}
func (p *CfoExpenseProvider) FormSnapshot(ctx context.Context, c Card) (map[string]any, error) {
	rows, n, e := p.repo.Rows(ctx, c.ID, CfoExpenseFilter{Limit: 50000})
	if e != nil {
		return nil, e
	}
	return map[string]any{"card_id": c.ID, "rows": rows, "total": n}, nil
}
func (p *CfoExpenseProvider) PublishRows(ctx context.Context, c Card) ([]PublishRow, error) {
	rows, _, e := p.repo.Rows(ctx, c.ID, CfoExpenseFilter{Limit: 50000})
	if e != nil {
		return nil, e
	}
	out := []PublishRow{}
	for _, r := range rows {
		for _, v := range r.Values {
			if v.Scenario != "plan" || v.Amount == nil || *v.Amount == 0 {
				continue
			}
			out = append(out, PublishRow{BlockType: "cfo_exp", Param: "VFORMTOLOADTAKTTARGET", Country: c.Country, CodeCFO: r.CodeCFO, CodePL: r.CodePL, Date: time.Date(v.Year, time.Month(v.Month), 1, 0, 0, 0, 0, time.UTC), Value: *v.Amount, Currency: c.Currency, Company: r.LegalEntity})
		}
	}
	return out, nil
}
