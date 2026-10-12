package plans

import (
	"context"
	"errors"
	"fmt"
)

type CfoExpenseService struct {
	repo   CfoExpenseRepo
	cards  CfoExpenseCardStore
	source CfoExpenseSource
	scope  ScopeStore
}

func NewCfoExpenseService(r CfoExpenseRepo, c CfoExpenseCardStore, src CfoExpenseSource, scope ScopeStore) *CfoExpenseService {
	return &CfoExpenseService{repo: r, cards: c, source: src, scope: scope}
}
func (s *CfoExpenseService) card(ctx context.Context, p Principal, id int64) (Card, CfoExpSpec, ScopeFilter, error) {
	if s.cards == nil {
		return Card{}, CfoExpSpec{}, ScopeFilter{}, errors.New("card store unavailable")
	}
	c, e := s.cards.Card(ctx, id)
	if e != nil {
		return c, CfoExpSpec{}, ScopeFilter{}, e
	}
	if c.FormCode != TemplateExp {
		return c, CfoExpSpec{}, ScopeFilter{}, errors.New("card is not CFO expense")
	}
	sp, ok := CfoExpSpecForScope(c.ScopeKey)
	if !ok {
		return c, CfoExpSpec{}, ScopeFilter{}, errors.New("invalid CFO scope")
	}
	f, e := scopeFilterFor(ctx, s.scope, p)
	if e != nil {
		return c, *sp, f, e
	}
	if !f.Allows(int(sp.Code), c.Country, c.LegalEntity) {
		return c, *sp, f, errors.New("среза доступа недостаточно")
	}
	return c, *sp, f, nil
}
func (s *CfoExpenseService) Form(ctx context.Context, p Principal, id int64, f CfoExpenseFilter) (CfoExpenseForm, error) {
	c, sp, sf, e := s.card(ctx, p, id)
	if e != nil {
		return CfoExpenseForm{}, e
	}
	rows, total, e := s.repo.Rows(ctx, id, f)
	if e != nil {
		return CfoExpenseForm{}, e
	}
	sourceErr := ""
	if total == 0 && s.source != nil {
		years := append([]int(nil), sp.FactYears...)
		seenYear := map[int]bool{}
		for _, y := range years {
			seenYear[y] = true
		}
		if !seenYear[sp.PlanYear] {
			years = append(years, sp.PlanYear)
		}
		for _, y := range sp.ComparisonYears {
			if !seenYear[y] {
				years = append(years, y)
				seenYear[y] = true
			}
		}
		cells, syncErr := s.source.Sync(ctx, sp.Code, sp.Country, years)
		if syncErr != nil {
			sourceErr = syncErr.Error()
		} else if len(cells) > 0 {
			built := cfoExpenseRowsFromCells(sp, cells)
			if syncErr = s.repo.Sync(ctx, id, built); syncErr != nil {
				sourceErr = syncErr.Error()
			} else {
				rows, total, e = s.repo.Rows(ctx, id, f)
				if e != nil {
					return CfoExpenseForm{}, e
				}
			}
		}
	}
	if !sf.Admin {
		out := rows[:0]
		for _, r := range rows {
			if sf.Allows(r.CodeCFO, r.Country, r.LegalEntity) {
				out = append(out, r)
			}
		}
		rows = out
		total = len(rows)
	}
	open := make([]bool, 12)
	if cardEditable(c) {
		for i := range open {
			open[i] = true
		}
	}
	return CfoExpenseForm{Card: c, Config: sp, Rows: rows, Total: total, Offset: f.Offset, ReadOnly: !cardEditable(c), SourceError: sourceErr, OpenMonths: open}, nil
}

func cfoExpenseRowsFromCells(spec CfoExpSpec, cells []ExpCostCell) []CfoExpenseRow {
	type key struct {
		cfo, pl int
		company string
	}
	byKey := map[key]int{}
	rows := make([]CfoExpenseRow, 0)
	for _, c := range cells {
		company := c.Company
		if !spec.HasLegalEntity {
			company = ""
		}
		k := key{c.CodeCFO, c.CodePL, company}
		i, ok := byKey[k]
		if !ok {
			byKey[k] = len(rows)
			rows = append(rows, CfoExpenseRow{CodeCFO: c.CodeCFO, CFO: c.CFO, CodePL: c.CodePL, LegalEntity: company, GroupPL: c.GroupPL, Expense: c.Expense, Country: c.Country, CountryCompany: c.CountryCompany})
			i = len(rows) - 1
		}
		amount := c.Amount
		rows[i].Values = append(rows[i].Values, CfoExpenseValue{Scenario: c.Scenario, Year: c.Year, Month: c.Month, Amount: &amount})
	}
	return rows
}
func (s *CfoExpenseService) Save(ctx context.Context, p Principal, id int64, cells []CfoExpenseCell, comments []CfoExpenseComment) (int, error) {
	c, _, sf, e := s.card(ctx, p, id)
	if e != nil {
		return 0, e
	}
	if !cardEditable(c) {
		return 0, errors.New("card is read-only")
	}
	normalized := make([]CfoExpenseCell, len(cells))
	for i, v := range cells {
		if v.Scenario != "" && v.Scenario != "plan" {
			return 0, errors.New("only plan scenario is editable")
		}
		if !sf.Allows(v.CodeCFO, c.Country, v.LegalEntity) {
			return 0, errors.New("среза доступа недостаточно")
		}
		v.Scenario = "plan"
		v.Year = c.Year
		normalized[i] = v
	}
	n, e := s.repo.Save(ctx, id, normalized, p.UserID)
	for _, x := range comments {
		if e == nil {
			if !sf.Allows(x.CodeCFO, c.Country, x.LegalEntity) {
				return n, errors.New("среза доступа недостаточно")
			}
			e = s.repo.Comment(ctx, id, CfoExpenseCell{CodeCFO: x.CodeCFO, CodePL: x.CodePL, LegalEntity: x.LegalEntity}, x.Comment)
		}
	}
	return n, e
}
func (s *CfoExpenseService) Bulk(ctx context.Context, p Principal, id int64, c CfoExpenseCell, op string) (int, error) {
	c2, _, sf, e := s.card(ctx, p, id)
	if e != nil {
		return 0, e
	}
	if !cardEditable(c2) {
		return 0, errors.New("card is read-only")
	}
	if !sf.Allows(c.CodeCFO, c2.Country, c.LegalEntity) {
		return 0, errors.New("среза доступа недостаточно")
	}
	c.Scenario = "plan"
	c.Year = c2.Year
	return s.repo.Bulk(ctx, id, c, op, p.UserID)
}
func (s *CfoExpenseService) Validate(ctx context.Context, p Principal, id int64) ([]CfoExpenseIssue, error) {
	form, e := s.Form(ctx, p, id, CfoExpenseFilter{Limit: 50000})
	if e != nil {
		return nil, e
	}
	var raw []CfoExpIssue
	for _, r := range form.Rows {
		in := CfoExpValidationInput{OpenMonths: form.OpenMonths, ExpectedCountry: form.Card.Country, Country: r.Country, FxFound: true, FxRequired: false}
		for _, v := range r.Values {
			if v.Scenario == "plan" {
				in.PlanMonths = append(in.PlanMonths, CfoExpCell{Month: v.Month, Amount: v.Amount})
			}
		}
		raw = append(raw, ValidateCfoExp(in)...)
	}
	out := make([]CfoExpenseIssue, len(raw))
	for i, x := range raw {
		out[i] = CfoExpenseIssue{Code: x.Code, Message: x.Message, Month: x.Month, Blocking: x.Blocking}
	}
	return out, nil
}
func (s *CfoExpenseService) Sync(ctx context.Context, p Principal, id int64) (string, error) {
	_, sp, _, e := s.card(ctx, p, id)
	if e != nil {
		return "", e
	}
	if s.source == nil {
		return "source unavailable", errors.New("source unavailable")
	}
	rows, e := s.source.Sync(ctx, sp.Code, sp.Country, sp.FactYears)
	if e != nil {
		return e.Error(), e
	}
	_ = rows
	return fmt.Sprintf("synced %d", len(rows)), nil
}
