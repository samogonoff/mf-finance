package plans

import (
	"context"
	"testing"
	"time"
)

// fakeExpRepo — сетка формы без БД.
type fakeExpRepo struct {
	inst      ExpInstance
	rows      []ExpRow
	factBasis string
}

func (f *fakeExpRepo) FactBasis(context.Context, int64) (string, error) { return f.factBasis, nil }

func (f *fakeExpRepo) ClearPaymentsFact(context.Context, int64) error { return nil }

func (f *fakeExpRepo) EnsureInstance(context.Context, ExpInstance, int64) (ExpInstance, error) {
	return f.inst, nil
}
func (f *fakeExpRepo) Instance(context.Context, int64) (ExpInstance, error) { return f.inst, nil }
func (f *fakeExpRepo) SyncRows(context.Context, int64, []ExpRow) (int, error) {
	return len(f.rows), nil
}
func (f *fakeExpRepo) Rows(context.Context, int64, ExpFilter) ([]ExpRow, int, error) {
	return f.rows, len(f.rows), nil
}
func (f *fakeExpRepo) SaveValues(context.Context, int64, []ExpCell, int64, int) (int, error) {
	return 0, nil
}
func (f *fakeExpRepo) SaveReadOnly(context.Context, int64, string, []ExpCell) error { return nil }
func (f *fakeExpRepo) SaveComment(context.Context, int64, int, int, string) error   { return nil }
func (f *fakeExpRepo) GroupTotals(context.Context, int64) (map[string]float64, error) {
	return nil, nil
}
func (f *fakeExpRepo) GroupOrder(context.Context, string) ([]string, error) { return nil, nil }

// fakeExpReg — реестры без БД.
type fakeExpReg struct{ byKind map[string][]ExpRegRow }

func (f *fakeExpReg) RegRows(_ context.Context, _ int64, kind string) ([]ExpRegRow, error) {
	return f.byKind[kind], nil
}
func (f *fakeExpReg) SaveRegRow(context.Context, int64, ExpRegRow, int64) (int64, error) {
	return 0, nil
}
func (f *fakeExpReg) DeleteRegRow(context.Context, int64, int64, int64) error { return nil }
func (f *fakeExpReg) CopyRegRow(context.Context, int64, int64, int64) (int64, error) {
	return 0, nil
}

func expPubFixture() *ExpCardProvider {
	repo := &fakeExpRepo{
		inst: ExpInstance{ID: 1, FormKind: ExpKindMP, Country: "RU", PlanYear: 2026, Currency: "RUB"},
		rows: []ExpRow{{
			CodeCFO: 251, CodePL: 72, CFO: "Wildberries", GroupPL: "РЕКЛАМА И МАРКЕТИНГ",
			Values: []ExpValue{
				{Scenario: ExpScenarioPlan, Month: 1, Amount: pv(1000)},
				// Факт в приёмник не идёт: публикуется только план.
				{Scenario: ExpScenarioFact, Month: 1, Amount: pv(777)},
			},
		}},
	}
	reg := &fakeExpReg{byKind: map[string][]ExpRegRow{
		ExpRegTravel: {{
			Kind: ExpRegTravel, CodeCFO: 251,
			Attrs:  map[string]any{"month": float64(2), "tickets": 600.0, "per_diem": 280.0, "lodging": 1050.0},
			Months: map[int]float64{2: 1930},
		}},
		ExpRegLogistics: {{
			Kind: ExpRegLogistics, CodePL: ExpPLLogExport, Months: map[int]float64{3: 5000},
		}},
		ExpRegIT: {
			{Kind: ExpRegIT, CodeCFO: 40, CodePL: ExpPLIT, MoveTo250: true, Months: map[int]float64{4: 18000}},
			{Kind: ExpRegIT, CodeCFO: 41, CodePL: 85, Excluded: true, Months: map[int]float64{4: 7000}},
		},
	}}
	return NewExpCardProvider(repo, reg)
}

// ТЗ §10 и находка Н3: реестр командировок раскладывается на ДВЕ статьи —
// билеты и командировочные на 27, проживание на 30.
func TestExpPublishRows_TravelSplitsIntoTwoCodes(t *testing.T) {
	rows, err := expPubFixture().PublishRows(context.Background(), Card{ID: 1, PlID: 7, Month: 6})
	if err != nil {
		t.Fatal(err)
	}
	var travel, lodging float64
	for _, r := range rows {
		switch r.CodePL {
		case ExpPLTravel:
			travel += r.Value
		case ExpPLLodging:
			lodging += r.Value
		}
	}
	if !near(travel, 880) {
		t.Fatalf("статья 27 = %v, ожидали 880 (билеты 600 + командировочные 280)", travel)
	}
	if !near(lodging, 1050) {
		t.Fatalf("статья 30 = %v, ожидали проживание 1 050", lodging)
	}
}

// ТЗ §5.4 и §10: строка ИТ с признаком переноса публикуется под ЦФО 250, строка
// «не учитывать» не публикуется вовсе.
func TestExpPublishRows_ITFlags(t *testing.T) {
	rows, _ := expPubFixture().PublishRows(context.Background(), Card{ID: 1, PlID: 7, Month: 6})
	var moved bool
	for _, r := range rows {
		if r.CodePL == 85 {
			t.Fatalf("строка с признаком «не учитывать» не должна публиковаться: %+v", r)
		}
		if r.CodePL == ExpPLIT && r.CodeCFO == ExpCFOMarketplaceDept && near(r.Value, 18000) {
			moved = true
		}
		if r.CodePL == ExpPLIT && r.CodeCFO == 40 {
			t.Fatalf("строка с переносом обязана уйти под ЦФО 250, а не под свой: %+v", r)
		}
	}
	if !moved {
		t.Fatal("строка ИТ с признаком переноса не попала под ЦФО 250")
	}
}

// Реестр логистики публикуется под ЦФО 250 (ТЗ §5.3, ответ 11) — и только отсюда.
func TestExpPublishRows_LogisticsUnder250(t *testing.T) {
	rows, _ := expPubFixture().PublishRows(context.Background(), Card{ID: 1, PlID: 7, Month: 6})
	found := false
	for _, r := range rows {
		if r.CodePL == ExpPLLogExport {
			found = true
			if r.CodeCFO != ExpCFOMarketplaceDept {
				t.Fatalf("логистика обязана публиковаться под ЦФО 250: %+v", r)
			}
		}
	}
	if !found {
		t.Fatal("строки реестра логистики не попали в публикацию")
	}
}

// Публикуется ТОЛЬКО план: факт лежит в форме справочно и в приёмник не идёт.
func TestExpPublishRows_FactIsNotPublished(t *testing.T) {
	rows, _ := expPubFixture().PublishRows(context.Background(), Card{ID: 1, PlID: 7, Month: 6})
	for _, r := range rows {
		if near(r.Value, 777) {
			t.Fatalf("факт не должен уходить в приёмник: %+v", r)
		}
	}
}

// Ответ 34: перед включением записи нужно увидеть дубли между наборами формы.
func TestExpPublishDuplicates(t *testing.T) {
	d := time.Date(2026, 4, 1, 0, 0, 0, 0, time.UTC)
	rows := []PublishRow{
		{CodeCFO: 250, CodePL: 48, Date: d, Value: 100},
		{CodeCFO: 250, CodePL: 48, Date: d, Value: 50}, // тот же ключ из другого набора
		{CodeCFO: 250, CodePL: 51, Date: d, Value: 10},
	}
	got := ExpPublishDuplicates(rows)
	if len(got) != 1 {
		t.Fatalf("дублей найдено %d, ожидали один: %v", len(got), got)
	}
}

func TestExpPublishDuplicates_DifferentCompaniesDoNotConflict(t *testing.T) {
	d := time.Date(2026, 4, 1, 0, 0, 0, 0, time.UTC)
	rows := []PublishRow{
		{CodeCFO: 250, CodePL: 48, Date: d, Company: "A", Value: 100},
		{CodeCFO: 250, CodePL: 48, Date: d, Company: "B", Value: 50},
		{CodeCFO: 250, CodePL: 48, Date: d, Company: "A", Value: 25},
	}
	got := ExpPublishDuplicates(rows)
	if len(got) != 1 {
		t.Fatalf("дубли должны считаться отдельно по юрлицу: %v", got)
	}
}

func (f *fakeExpRepo) Params(context.Context, int64) ([]ExpParamRow, error) { return nil, nil }
func (f *fakeExpRepo) SaveParam(context.Context, int64, ExpParamRow, int64) error {
	return nil
}

func (f *fakeExpRepo) SyncPLLines(context.Context, []ExpPLLine) (int, error) { return 0, nil }
