package plans

import (
	"context"
	"testing"
)

type cfoRepoFake struct{ saved []CfoExpenseCell }

func (f *cfoRepoFake) Sync(context.Context, int64, []CfoExpenseRow) error { return nil }

func (f *cfoRepoFake) Rows(context.Context, int64, CfoExpenseFilter) ([]CfoExpenseRow, int, error) {
	return []CfoExpenseRow{{CodeCFO: 31, CodePL: 24, Country: "BY", LegalEntity: "LE"}}, 1, nil
}
func (f *cfoRepoFake) Save(_ context.Context, _ int64, c []CfoExpenseCell, _ int64) (int, error) {
	f.saved = append(f.saved, c...)
	return len(c), nil
}
func (f *cfoRepoFake) Comment(context.Context, int64, CfoExpenseCell, string) error { return nil }
func (f *cfoRepoFake) Bulk(context.Context, int64, CfoExpenseCell, string, int64) (int, error) {
	return 1, nil
}

type cfoCardsFake struct{ card Card }

func (f cfoCardsFake) Card(context.Context, int64) (Card, error) { return f.card, nil }

type cfoScopesFake struct{}

func (cfoScopesFake) UserCodeCFOs(context.Context, int64) ([]int, error) { return []int{31}, nil }
func (cfoScopesFake) UpsertScope(context.Context, UserScope) error       { return nil }
func (cfoScopesFake) UserScopes(context.Context, int64) ([]UserScope, error) {
	return []UserScope{{CodeCFO: []int{31}, Country: "BY", LegalEntity: "LE"}}, nil
}
func TestCfoExpenseServiceScopeAndPlanOnly(t *testing.T) {
	r := &cfoRepoFake{}
	s := NewCfoExpenseService(r, cfoCardsFake{Card{ID: 1, FormCode: TemplateExp, ScopeKey: "CFO-31-BY", Country: "BY", LegalEntity: "LE", Year: 2026, Status: CardDraft}}, nil, cfoScopesFake{})
	if _, e := s.Form(context.Background(), Principal{UserID: 1}, 1, CfoExpenseFilter{}); e != nil {
		t.Fatal(e)
	}
	v := 2.
	if _, e := s.Save(context.Background(), Principal{UserID: 1}, 1, []CfoExpenseCell{{CodeCFO: 31, CodePL: 24, LegalEntity: "LE", Month: 1, Amount: &v}}, nil); e != nil {
		t.Fatal(e)
	}
	if len(r.saved) != 1 || r.saved[0].Amount == nil || r.saved[0].Year != 2026 || r.saved[0].Scenario != "plan" {
		t.Fatal("plan cell not saved")
	}
}

func TestCfoExpenseRowsKeepLegalEntitiesSeparate(t *testing.T) {
	amount := 10.0
	rows := cfoExpenseRowsFromCells(CfoExpSpec{Code: CfoExp32, Country: "BY", HasLegalEntity: true}, []ExpCostCell{
		{CodeCFO: 32, CodePL: 24, Company: "A", Scenario: "Факт по начислениям", Year: 2026, Month: 1, Amount: amount},
		{CodeCFO: 32, CodePL: 24, Company: "B", Scenario: "Факт по начислениям", Year: 2026, Month: 1, Amount: amount},
	})
	if len(rows) != 2 || rows[0].LegalEntity == rows[1].LegalEntity {
		t.Fatalf("legal entities must not collide: %+v", rows)
	}
}
