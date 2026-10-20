package plans

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestPlanRetailLFLRefresh(t *testing.T) {
	rows := []RetailLFLRefreshRow{
		{RowID: 1, CardID: 10, CodeCFO: 101, Status: "new", Country: "BY", Year: 2026, Month: 5, CardStatus: "Утверждено", Editable: false},
		{RowID: 2, CardID: 10, CodeCFO: 102, Status: "новый", Country: "BY", Year: 2026, Month: 5, CardStatus: "Утверждено", Editable: false}, // «до года» в прошлое не переносится
		{RowID: 3, CardID: 11, CodeCFO: 101, Status: "new", Country: "BY", Year: 2026, Month: 11, CardStatus: "Черновик", Editable: true},
		{RowID: 4, CardID: 11, CodeCFO: 103, Status: "lfl", Country: "BY", Year: 2026, Month: 11, CardStatus: "Черновик", Editable: true},                           // уже верный
		{RowID: 5, CardID: 11, CodeCFO: 104, Status: "new", Country: "BY", Year: 2026, Month: 11, CardStatus: "Черновик", Editable: true},                           // нет в Лисе
		{RowID: 6, CardID: 11, CodeCFO: 105, Status: "closed", Country: "BY", Year: 2026, Month: 11, CardStatus: "Черновик", Editable: true},                        // статус в Лисе не присвоен
		{RowID: 7, CardID: 10, CodeCFO: 106, Status: "new", Country: "BY", Year: 2026, Month: 5, CardStatus: "Утверждено", Editable: false},                         // «закрыт» в прошлое не переносится
		{RowID: 8, CardID: 10, CodeCFO: 107, Status: "new", DateOpen: "2026-03-01", Country: "BY", Year: 2026, Month: 5, CardStatus: "Утверждено", Editable: false}, // в мае был ещё не сравнимым
		{RowID: 9, CardID: 10, CodeCFO: 107, Status: "new", DateOpen: "2024-01-01", Country: "BY", Year: 2026, Month: 5, CardStatus: "Утверждено", Editable: false},
	}
	lisa := map[int]string{101: LFLYes, 102: LFLUnder1, 103: LFLYes, 105: "", 106: LFLClosed, 107: LFLYes}
	changes, res := planRetailLFLRefresh(rows, lisa)

	if len(changes) != 3 || changes[0].RowID != 1 || changes[0].To != LFLYes || changes[1].RowID != 3 || changes[2].RowID != 9 {
		t.Fatalf("изменения: %+v", changes)
	}
	if res.RowsScanned != 9 || res.RowsChanged != 3 || res.RowsSkipped != 3 || res.CardsChanged != 2 || res.LockedCards != 1 || res.StoresInLisa != 6 {
		t.Fatalf("итог: %+v", res)
	}
	if len(res.Transitions) != 1 || res.Transitions[0].From != LFLNew || res.Transitions[0].To != LFLYes || res.Transitions[0].Rows != 3 {
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

// Правило переноса в прошлое: только устаревшее «новый» → LFL у уже сравнимого магазина.
func TestRetailLFLRefreshAllowed(t *testing.T) {
	may := RetailLFLRefreshRow{Year: 2026, Month: 5}
	cases := []struct {
		name       string
		date       string
		have, want string
		ok         bool
	}{
		{"новый → LFL, дата неизвестна", "", LFLNew, LFLYes, true},
		{"пустой → LFL", "", "", LFLYes, true},
		{"открыт больше года назад", "2025-05-01", LFLNew, LFLYes, true},
		{"открыт меньше года назад", "2025-06-01", LFLNew, LFLYes, false},
		{"закрыт в прошлое не переносится", "", LFLNew, LFLClosed, false},
		{"реконструкция не переносится", "", LFLYes, LFLReconstruction, false},
		{"до года не переносится", "", LFLNew, LFLUnder1, false},
		{"верный LFL не трогаем на закрытие", "", LFLYes, LFLClosed, false},
		{"«до года» → LFL не трогаем (может быть верным в периоде)", "", LFLUnder1, LFLYes, false},
	}
	for _, c := range cases {
		r := may
		r.DateOpen = c.date
		if got := retailLFLRefreshAllowed(r, c.have, c.want); got != c.ok {
			t.Errorf("%s: %v, ждали %v", c.name, got, c.ok)
		}
	}
}

type lfRefreshRepo struct {
	RetailRepo
	rows    []RetailLFLRefreshRow
	applied []RetailLFLChange
	err     error
}

func (r *lfRefreshRepo) LFLRefreshRows(context.Context) ([]RetailLFLRefreshRow, error) {
	return r.rows, r.err
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

// Коды ответа: источник без Лисы — 409 (конфигурация), Лиса недоступна — 502, ошибка собственной БД — 500.
func TestRetailHandlerLFLRefreshStatusCodes(t *testing.T) {
	code := func(svc *RetailService) int {
		h := NewRetailHandler(svc, nil)
		rec := httptest.NewRecorder()
		h.LFLRefresh(rec, httptest.NewRequest(http.MethodPost, "/api/plans/retail/lfl-refresh?dry_run=1", nil))
		return rec.Code
	}
	if got := code(NewRetailService(&lfRefreshRepo{}, nil, NewMockRetailSource())); got != http.StatusConflict {
		t.Errorf("источник без Лисы: %d", got)
	}
	if got := code(NewRetailService(&lfRefreshRepo{}, nil, lfLisaSource{err: errors.New("нет прав")})); got != http.StatusBadGateway {
		t.Errorf("Лиса недоступна: %d", got)
	}
	failing := &lfRefreshRepo{err: errors.New("pg: connection reset")}
	if got := code(NewRetailService(failing, nil, lfLisaSource{})); got != http.StatusInternalServerError {
		t.Errorf("ошибка собственной БД: %d", got)
	}
}
