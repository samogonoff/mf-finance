package plans

import (
	"context"
	"testing"
)

type readOnlyRepoFake struct {
	rows      []CfoExpenseRow
	rowsCalls int
	syncCalls int
}

func (f *readOnlyRepoFake) Sync(context.Context, int64, []CfoExpenseRow) error {
	f.syncCalls++
	return nil
}
func (f *readOnlyRepoFake) Rows(context.Context, int64, CfoExpenseFilter) ([]CfoExpenseRow, int, error) {
	f.rowsCalls++
	return f.rows, len(f.rows), nil
}
func (f *readOnlyRepoFake) Save(context.Context, int64, []CfoExpenseCell, int64) (int, error) {
	return 0, nil
}
func (f *readOnlyRepoFake) Comment(context.Context, int64, CfoExpenseCell, string) error {
	return nil
}
func (f *readOnlyRepoFake) Bulk(context.Context, int64, CfoExpenseCell, string, int64) (int, error) {
	return 0, nil
}

type readOnlySourceFake struct {
	calls int
	cells []ExpCostCell
}

func (f *readOnlySourceFake) Sync(context.Context, CfoExpFormCode, string, []int) ([]ExpCostCell, error) {
	f.calls++
	return f.cells, nil
}

func readOnlyServiceFixture(repo CfoExpenseRepo, source CfoExpenseSource) *CfoExpenseService {
	return NewCfoExpenseService(repo, cfoCardsFake{Card{
		ID: 1, FormCode: TemplateExp, ScopeKey: "CFO-31-BY", Country: "BY", LegalEntity: "LE",
		Year: 2026, Status: CardDraft,
	}}, source, cfoScopesFake{})
}

func TestCfoExpenseReadOnlyMethodsDoNotHydrateRows(t *testing.T) {
	repo := &readOnlyRepoFake{}
	source := &readOnlySourceFake{cells: []ExpCostCell{{CodeCFO: 31, CodePL: 24, Country: "BY", Amount: 10}}}
	svc := readOnlyServiceFixture(repo, source)

	if _, err := svc.FormReadOnly(context.Background(), Principal{UserID: 1}, 1, CfoExpenseFilter{}); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.ValidateReadOnly(context.Background(), Principal{UserID: 1}, 1); err != nil {
		t.Fatal(err)
	}
	if _, _, err := svc.ExportReadOnly(context.Background(), Principal{UserID: 1}, 1, CfoExpenseFilter{}); err != nil {
		t.Fatal(err)
	}
	if source.calls != 0 || repo.syncCalls != 0 {
		t.Fatalf("read-only methods hydrated rows: source calls=%d repo sync calls=%d", source.calls, repo.syncCalls)
	}
	if repo.rowsCalls != 3 {
		t.Fatalf("read-only methods must read each result independently, rows calls=%d", repo.rowsCalls)
	}
}

func TestCfoExpenseFormKeepsNormalLazyHydration(t *testing.T) {
	repo := &readOnlyRepoFake{}
	source := &readOnlySourceFake{cells: []ExpCostCell{{CodeCFO: 31, CodePL: 24, Country: "BY", Amount: 10}}}
	svc := readOnlyServiceFixture(repo, source)

	if _, err := svc.Form(context.Background(), Principal{UserID: 1}, 1, CfoExpenseFilter{}); err != nil {
		t.Fatal(err)
	}
	if source.calls != 1 || repo.syncCalls != 1 {
		t.Fatalf("normal form lost lazy hydration: source calls=%d repo sync calls=%d", source.calls, repo.syncCalls)
	}
}
