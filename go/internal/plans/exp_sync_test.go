package plans

import (
	"context"
	"errors"
	"strings"
	"sync"
	"testing"
	"time"
	"unicode/utf8"
)

// stubExpSource — источник затрат с управляемой скоростью и ответом.
type stubExpSource struct {
	mu      sync.Mutex
	calls   int
	release chan struct{} // не nil — Costs ждёт закрытия
	cells   []ExpCostCell
	err     error
}

func (s *stubExpSource) Costs(ctx context.Context, _, _ string, _ []int) ([]ExpCostCell, error) {
	s.mu.Lock()
	s.calls++
	rel := s.release
	s.mu.Unlock()
	if rel != nil {
		select {
		case <-rel:
		case <-ctx.Done():
			return nil, ctx.Err()
		}
	}
	return s.cells, s.err
}
func (s *stubExpSource) Cfo(context.Context, string, string) (map[int]ExpCfoAttrs, error) {
	return map[int]ExpCfoAttrs{}, nil
}
func (s *stubExpSource) SalesPlan(context.Context, string, int) (map[int]map[int]float64, error) {
	return nil, nil
}
func (s *stubExpSource) callCount() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.calls
}

func newSyncTestService(src ExpDataSource) (*ExpService, ExpInstance) {
	in := ExpInstance{ID: 7, FormKind: ExpKindRetail, Country: "BY", PlanYear: 2026, Currency: "BYN"}
	svc := NewExpService(&fakeExpRepo{inst: in}, nil, src)
	svc.syncWait = 20 * time.Millisecond
	return svc, in
}

func waitSyncState(t *testing.T, svc *ExpService, id int64, want string) ExpSyncStatus {
	t.Helper()
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		if st := svc.syncStatus(id); st.State == want {
			return st
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatalf("состояние %q не наступило, сейчас %+v", want, svc.syncStatus(id))
	return ExpSyncStatus{}
}

// Долгая процедура не держит открытие формы: Form получает «running» и уходит, а
// загрузка доходит до конца и ложится в БД уже после ответа.
func TestExpSync_SlowSourceDoesNotBlockForm(t *testing.T) {
	src := &stubExpSource{release: make(chan struct{}), cells: []ExpCostCell{
		{Scenario: ExpSrcFact, Year: 2025, Month: 1, CodePL: 10, CodeCFO: 100, Amount: 5},
	}}
	svc, in := newSyncTestService(src)
	start := time.Now()
	svc.ensureSync(Card{ID: 1}, in, false)
	if time.Since(start) > time.Second {
		t.Fatalf("ensureSync блокирует ответ: %v", time.Since(start))
	}
	if st := svc.syncStatus(in.ID); st.State != ExpSyncRunning {
		t.Fatalf("ждали running, got %+v", st)
	}
	// Повторное открытие формы не запускает вторую выгрузку.
	svc.ensureSync(Card{ID: 1}, in, false)
	if src.callCount() != 1 {
		t.Fatalf("параллельная выгрузка: вызовов %d", src.callCount())
	}
	close(src.release)
	st := waitSyncState(t, svc, in.ID, ExpSyncOK)
	if st.SourceCells != 1 || st.FinishedAt == nil {
		t.Fatalf("итог загрузки: %+v", st)
	}
}

// Ошибка источника видна в статусе (раньше уходила только в лог), автоматически не
// повторяется, а кнопкой «Обновить из источника» запускается снова.
func TestExpSync_ErrorIsVisibleAndNotRetriedAutomatically(t *testing.T) {
	src := &stubExpSource{err: errors.New("в выдаче процедуры нет колонки \"amount\"")}
	svc, in := newSyncTestService(src)
	svc.ensureSync(Card{ID: 1}, in, false)
	st := waitSyncState(t, svc, in.ID, ExpSyncError)
	if st.Error == "" {
		t.Fatalf("причина ошибки не передана: %+v", st)
	}
	svc.ensureSync(Card{ID: 1}, in, false)
	time.Sleep(50 * time.Millisecond)
	if src.callCount() != 1 {
		t.Fatalf("после ошибки автоповтор запустил выгрузку: вызовов %d", src.callCount())
	}
	svc.ensureSync(Card{ID: 1}, in, true)
	waitSyncState(t, svc, in.ID, ExpSyncError)
	if src.callCount() != 2 {
		t.Fatalf("кнопка не запустила повтор: вызовов %d", src.callCount())
	}
}

// Пустой ответ источника — это «ok, 0 ячеек», а не «идёт загрузка» и не ошибка.
func TestExpSync_EmptySourceIsReported(t *testing.T) {
	svc, in := newSyncTestService(&stubExpSource{})
	svc.ensureSync(Card{ID: 1}, in, false)
	st := waitSyncState(t, svc, in.ID, ExpSyncOK)
	if st.SourceCells != 0 {
		t.Fatalf("ждали 0 ячеек: %+v", st)
	}
}

// Отбор сценариев: платёжный факт в форму не идёт (ответ 44), начисления и стратегия идут.
// Колонка проекта в выдаче процедуры — ProjectName, а не Projects.
func TestExpScenarioSelectionAndProjectColumn(t *testing.T) {
	if expScenarioForKind(ExpKindRetail, ExpSrcFactPayments) {
		t.Fatal("платёжный факт не должен проходить отбор напрямую")
	}
	if !expScenarioForKind(ExpKindRetail, ExpSrcFact) || !expScenarioForKind(ExpKindRetail, ExpSrcStrategy) {
		t.Fatal("начисления и стратегия обязаны проходить отбор")
	}
	if got := expProjectsCol(map[string]int{"projectname": 3}); got != "projectname" {
		t.Fatalf("колонка проекта: %q", got)
	}
	if got := expProjectsCol(map[string]int{"projects": 3, "projectname": 4}); got != "projects" {
		t.Fatalf("колонка проекта: %q", got)
	}
}

func TestExpGroupWithoutNumber(t *testing.T) {
	for in, want := range map[string]string{"3.Магазины": "Магазины", "4.Маркетплейсы": "Маркетплейсы", "Отдел": "Отдел", "12.Прочее": "Прочее", "Склад.Центр": "Склад.Центр"} {
		if got := expGroupWithoutNumber(in); got != want {
			t.Errorf("%q → %q, want %q", in, got, want)
		}
	}
}

// Пустая выдача источника — тоже «прочитано»: форма не запускает выгрузку заново при
// каждом открытии (раньше это зацикливало фронт: reload → новая выгрузка → reload).
func TestExpSync_EmptySourceIsNotRestartedOnEveryOpen(t *testing.T) {
	src := &stubExpSource{}
	svc, in := newSyncTestService(src)
	svc.ensureSync(Card{ID: 1}, in, false)
	waitSyncState(t, svc, in.ID, ExpSyncOK)
	for i := 0; i < 3; i++ {
		svc.ensureSync(Card{ID: 1}, in, false)
	}
	time.Sleep(50 * time.Millisecond)
	if src.callCount() != 1 {
		t.Fatalf("пустой ответ источника запускает выгрузку заново: вызовов %d", src.callCount())
	}
	if st := svc.syncStatus(in.ID); st.State != ExpSyncOK || st.SourceCells != 0 {
		t.Fatalf("статус: %+v", st)
	}
}

// Ожидание очереди процедуры прерывается по контексту, а не ждёт чужую выгрузку вечно.
func TestExpProcQueue_WaitIsCancellable(t *testing.T) {
	expProcSem <- struct{}{} // очередь занята «чужой» выгрузкой
	defer func() { <-expProcSem }()
	src := &expSource{db: nil, tables: ExpTables{CostsProc: "X"}}
	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()
	start := time.Now()
	_, err := src.Costs(ctx, ExpKindRetail, "BY", []int{2026})
	if err == nil || time.Since(start) > 2*time.Second {
		t.Fatalf("ожидание очереди не прервано: err=%v за %v", err, time.Since(start))
	}
}

// На экран — обобщённая причина, без адреса сервера, порта и логина из текста драйвера.
func TestExpSyncErrText_HidesDriverDetails(t *testing.T) {
	for _, raw := range []string{
		"unable to open tcp connection with host '10.10.6.15:1433': dial tcp 10.10.6.15:1433: i/o timeout",
		"mssql proc: Login failed for user 'someone'.",
		"context deadline exceeded",
		"что-то совсем неожиданное с адресом 10.10.6.15",
	} {
		got := expSyncErrText(errors.New(raw))
		for _, leak := range []string{"10.10.6.15", "1433", "someone"} {
			if strings.Contains(got, leak) {
				t.Errorf("утечка %q в %q", leak, got)
			}
		}
	}
	// Свои ошибки про колонки показываются, обрезка — по символам (русский текст не рвётся).
	long := errors.New("в выдаче процедуры нет колонки " + strings.Repeat("ё", 400))
	if got := expSyncErrText(long); !utf8.ValidString(got) {
		t.Errorf("обрезка разрезала символ: %q", got)
	}
}

// refreshingSource — источник с ночным снимком: умеет перезаливку, которая может падать.
type refreshingSource struct {
	stubExpSource
	refreshErr error
	refreshes  int
}

func (r *refreshingSource) RefreshSource(context.Context) error {
	r.mu.Lock()
	r.refreshes++
	r.mu.Unlock()
	return r.refreshErr
}

func (r *refreshingSource) SnapshotAsOf(context.Context) (time.Time, bool) {
	return time.Unix(1792400000, 0), true
}

// Не удалась перезаливка снимка (миграция не применена, заливка у другого пользователя, сбой
// MSSQL) — форма всё равно перечитывается из того, что есть, а не уходит в ошибку.
func TestExpSync_RefreshFailureStillReadsSource(t *testing.T) {
	src := &refreshingSource{refreshErr: errors.New("create stage: table finance.exp_costs doesn't exist")}
	src.cells = []ExpCostCell{{Scenario: ExpSrcFact, Year: 2025, Month: 1, CodePL: 10, CodeCFO: 100, Amount: 5}}
	svc, in := newSyncTestService(src)
	svc.ensureSync(Card{ID: 1}, in, true)
	st := waitSyncState(t, svc, in.ID, ExpSyncOK)
	if st.SourceCells != 1 || st.Error != "" {
		t.Fatalf("ошибка перезаливки не должна ломать загрузку: %+v", st)
	}
	// Пользователь должен узнать, что перезаливка не удалась (без деталей драйвера).
	if st.Warning == "" || strings.Contains(st.Warning, "exp_costs") {
		t.Fatalf("ждали предупреждение о неудавшейся перезаливке без внутренних имён: %q", st.Warning)
	}
	if src.refreshes != 1 || src.callCount() != 1 {
		t.Fatalf("перезаливок %d, чтений %d", src.refreshes, src.callCount())
	}
}

// Параллельные загрузки не путают источник: след чтения у каждого вызова свой.
func TestExpSourceTrace_IsPerCall(t *testing.T) {
	c1, t1 := WithExpSourceTrace(context.Background())
	c2, t2 := WithExpSourceTrace(context.Background())
	noteExpSource(c1, ExpSourceKindCH)
	noteExpSource(c2, ExpSourceKindOLAP)
	if t1.Kind != ExpSourceKindCH || t2.Kind != ExpSourceKindOLAP {
		t.Fatalf("следы смешались: %q %q", t1.Kind, t2.Kind)
	}
}