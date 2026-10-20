package plans

import (
	"context"
	"errors"
	"testing"
)

func TestPlanRetailLFLRefresh(t *testing.T) {
	rows := []RetailLFLRefreshRow{
		{RowID: 1, CardID: 10, CodeCFO: 101, Status: "new", Country: "BY", Year: 2026, Month: 5, CardStatus: "Утверждено", Editable: false},
		{RowID: 2, CardID: 10, CodeCFO: 102, Status: "новый", Country: "BY", Year: 2026, Month: 5, CardStatus: "Утверждено", Editable: false}, // сырой статус тоже приводится
		{RowID: 3, CardID: 11, CodeCFO: 101, Status: "new", Country: "BY", Year: 2026, Month: 11, CardStatus: "Черновик", Editable: true},
		{RowID: 4, CardID: 11, CodeCFO: 103, Status: "lfl", Country: "BY", Year: 2026, Month: 11, CardStatus: "Черновик", Editable: true},    // уже верный
		{RowID: 5, CardID: 11, CodeCFO: 104, Status: "new", Country: "BY", Year: 2026, Month: 11, CardStatus: "Черновик", Editable: true},    // нет в Лисе
		{RowID: 6, CardID: 11, CodeCFO: 105, Status: "closed", Country: "BY", Year: 2026, Month: 11, CardStatus: "Черновик", Editable: true}, // статус в Лисе не присвоен
	}
	lisa := map[int]string{101: LFLYes, 102: LFLUnder1, 103: LFLYes, 105: ""}
	changes, res := planRetailLFLRefresh(rows, lisa)

	if len(changes) != 3 || changes[0].RowID != 1 || changes[0].To != LFLYes || changes[1].RowID != 2 || changes[1].To != LFLUnder1 || changes[2].RowID != 3 {
		t.Fatalf("изменения: %+v", changes)
	}
	if res.RowsScanned != 6 || res.RowsChanged != 3 || res.CardsChanged != 2 || res.LockedCards != 1 || res.StoresInLisa != 4 {
		t.Fatalf("итог: %+v", res)
	}
	if len(res.Transitions) != 2 || res.Transitions[0].From != LFLNew || res.Transitions[0].To != LFLYes || res.Transitions[0].Rows != 2 {
		t.Fatalf("переходы: %+v", res.Transitions)
	}
	// Периоды — от новых к старым; закрытая для правок карточка помечена.
	if len(res.Periods) != 2 || res.Periods[0].Month != 11 || !res.Periods[0].Editable || res.Periods[1].Month != 5 || res.Periods[1].Editable || res.Periods[1].Changed != 2 {
		t.Fatalf("периоды: %+v", res.Periods)
	}
	// Без изменений — пустой, но не nil список (JSON []).
	_, none := planRetailLFLRefresh(rows[3:4], lisa)
	if none.RowsChanged != 0 || none.Transitions == nil || none.Periods == nil {
		t.Fatalf("без изменений: %+v", none)
	}
}

type lfRefreshRepo struct {
	RetailRepo
	rows    []RetailLFLRefreshRow
	applied []RetailLFLChange
}

func (r *lfRefreshRepo) LFLRefreshRows(context.Context) ([]RetailLFLRefreshRow, error) {
	return r.rows, nil
}

func (r *lfRefreshRepo) ApplyLFLStatuses(_ context.Context, ch []RetailLFLChange) (int, error) {
	r.applied = ch
	return len(ch), nil
}

type lfLisaSource struct {
	RetailDataSource
	stores []RetailStore
	err    error
}

func (s lfLisaSource) StoresFromLisa(context.Context) ([]RetailStore, error) { return s.stores, s.err }

// Предпросмотр ничего не пишет, применение пишет; без Лисы или без источника с Лисой статусы не трогаются.
func TestRetailService_RefreshLFLStatuses(t *testing.T) {
	repo := &lfRefreshRepo{rows: []RetailLFLRefreshRow{
		{RowID: 1, CardID: 10, CodeCFO: 101, Status: LFLNew, Country: "BY", Year: 2026, Month: 5},
		{RowID: 2, CardID: 10, CodeCFO: 102, Status: LFLNew, Country: "BY", Year: 2026, Month: 5},
	}}
	src := lfLisaSource{stores: []RetailStore{
		{CodeCFO: 101, LFLStatus: LFLYes, LisaFound: true},
		{CodeCFO: 102, LFLStatus: LFLNew}, // не найден в Лисе: статус справочника ЦФО, обновлять нечем
	}}
	svc := NewRetailService(repo, nil, src)

	prev, err := svc.RefreshLFLStatuses(context.Background(), Principal{}, false)
	if err != nil || !prev.DryRun || prev.RowsChanged != 1 || repo.applied != nil {
		t.Fatalf("предпросмотр: %+v %v applied=%v", prev, err, repo.applied)
	}
	res, err := svc.RefreshLFLStatuses(context.Background(), Principal{}, true)
	if err != nil || res.DryRun || len(repo.applied) != 1 || repo.applied[0].RowID != 1 || repo.applied[0].To != LFLYes {
		t.Fatalf("применение: %+v %v applied=%v", res, err, repo.applied)
	}

	repo.applied = nil
	if _, err := NewRetailService(repo, nil, lfLisaSource{err: errors.New("нет прав")}).RefreshLFLStatuses(context.Background(), Principal{}, true); err == nil || repo.applied != nil {
		t.Fatalf("без Лисы статусы не обновляются: err=%v applied=%v", err, repo.applied)
	}
	if _, err := NewRetailService(repo, nil, NewMockRetailSource()).RefreshLFLStatuses(context.Background(), Principal{}, true); err == nil {
		t.Fatal("источник без Лисы (mock) обновлять статусы не должен")
	}
}
