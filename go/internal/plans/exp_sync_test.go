package plans

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"
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

// Источник без «Факта по начислениям» (так сейчас отдаёт процедура): платёжный факт
// подставляется как факт, иначе колонка факта и база расчётов остаются пустыми.
// Если начисления есть — платёжный факт по-прежнему не используется (ответ 44).
func TestExpScenarioPaymentsFallbackRules(t *testing.T) {
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