package etl

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"regexp"
	"strings"
	"sync"
	"testing"
)

// fakeArhCH — минимальный «ClickHouse» для сценариев RunFinDWHArh: stage, основная таблица
// по датам и журнал отпечатков (ReplacingMergeTree по ver). Ведёт журнал операций.
type fakeArhCH struct {
	mu         sync.Mutex
	stageRows  map[string]int
	staleStage map[string]bool
	main       map[string]int      // дата → строк в основной таблице
	fp         map[string]arhFPRow // дата → последняя версия отпечатка
	ops        []string            // журнал: CREATE/DROP, STAGE+n, DELETE d, SWAP, FP d rows
	failSwap   bool                // INSERT … SELECT из stage падает
	lastDel    string
	cleanupQ   string
}

var (
	reDelete = regexp.MustCompile(`DELETE WHERE \w+ = toDate\('([0-9-]+)'\)`)
	reDate   = regexp.MustCompile(`toDate\('([0-9-]+)'\)`)
	reStage  = regexp.MustCompile(`finance\.fact_arh_(?:debt|sales)_stage_[0-9]+_[0-9]+_[0-9]+`)
)

func newFakeCH() (*fakeArhCH, *chClient, func()) {
	f := &fakeArhCH{stageRows: map[string]int{}, staleStage: map[string]bool{}, main: map[string]int{}, fp: map[string]arhFPRow{}}
	srv := httptest.NewServer(f)
	c, _ := newCHClient(srv.URL, "u", "p")
	return f, c, srv.Close
}

func (f *fakeArhCH) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	f.mu.Lock()
	defer f.mu.Unlock()
	body, _ := io.ReadAll(r.Body)
	q := r.URL.Query().Get("query")
	if q == "" {
		q = string(body)
	}
	switch {
	case strings.Contains(q, "FROM system.tables") && strings.Contains(q, "fact_arh_"):
		f.cleanupQ = q
		for stage, stale := range f.staleStage {
			if stale {
				_, _ = fmt.Fprintln(w, strings.TrimPrefix(stage, "finance."))
			}
		}
	case strings.HasPrefix(q, "CREATE TABLE"):
		stage := reStage.FindString(q)
		f.stageRows[stage] = 0
		f.ops = append(f.ops, "CREATE "+stage)
	case strings.HasPrefix(q, "DROP TABLE IF EXISTS"):
		stage := reStage.FindString(q)
		delete(f.stageRows, stage)
		delete(f.staleStage, stage)
		f.ops = append(f.ops, "DROP "+stage)
	case strings.HasPrefix(q, "INSERT INTO finance.fact_arh_day_fp"):
		for _, line := range strings.Split(strings.TrimSpace(string(body)), "\n") {
			var row arhFPRow
			_ = json.Unmarshal([]byte(line), &row)
			if cur, ok := f.fp[row.Day]; !ok || row.Ver > cur.Ver {
				f.fp[row.Day] = row
			}
			f.ops = append(f.ops, fmt.Sprintf("FP %s %d", row.Day, row.SrcRows))
		}
	case strings.HasPrefix(q, "INSERT INTO finance.fact_arh_debt_stage"):
		stage := reStage.FindString(q)
		n := len(strings.Split(strings.TrimSpace(string(body)), "\n"))
		f.stageRows[stage] += n
		f.ops = append(f.ops, fmt.Sprintf("STAGE+%d", n))
	case strings.Contains(q, "SELECT * FROM finance.fact_arh_debt_stage"):
		if f.failSwap {
			http.Error(w, "boom", http.StatusInternalServerError)
			return
		}
		stage := reStage.FindString(q)
		f.main[f.lastDel] = f.stageRows[stage]
		f.ops = append(f.ops, "SWAP "+f.lastDel)
	case strings.HasPrefix(q, "SELECT count() FROM finance.fact_arh_debt_stage"):
		_, _ = fmt.Fprint(w, f.stageRows[reStage.FindString(q)])
	case strings.HasPrefix(q, "ALTER TABLE finance.fact_arh_debt DELETE"):
		m := reDelete.FindStringSubmatch(q)
		f.lastDel = m[1]
		delete(f.main, m[1])
		f.ops = append(f.ops, "DELETE "+m[1])
	case strings.Contains(q, "FROM finance.fact_arh_day_fp"):
		in := reDate.FindAllStringSubmatch(q, -1)
		for _, m := range in {
			if row, ok := f.fp[m[1]]; ok {
				_, _ = fmt.Fprintf(w, "%s\t%s\t%d\n", row.Day, row.SrcChangedAt, row.SrcRows)
			}
		}
	case strings.Contains(q, "FROM finance.fact_arh_debt WHERE snapshot_date IN"):
		for _, m := range reDate.FindAllStringSubmatch(q, -1) {
			if f.main[m[1]] > 0 {
				_, _ = fmt.Fprintln(w, m[1])
			}
		}
	default:
		http.Error(w, "unexpected query: "+q, http.StatusBadRequest)
	}
}

type fakeSrc struct {
	fp       map[string]arhFP
	rows     int            // строк в день
	failAt   map[string]int // дата → оборвать после стольких строк
	dayCalls []string
}

func (s *fakeSrc) Fingerprints(_ context.Context, _ arhStream, _ []string) (map[string]arhFP, error) {
	return s.fp, nil
}

func (s *fakeSrc) Day(_ context.Context, _ arhStream, day string, emit func(any) error) error {
	s.dayCalls = append(s.dayCalls, day)
	for i := 0; i < s.rows; i++ {
		if k, ok := s.failAt[day]; ok && i == k {
			return errors.New("обрыв соединения с MSSQL")
		}
		if err := emit(map[string]any{"i": i}); err != nil {
			return err
		}
	}
	return nil
}

func arhTestOpts() FinDWHOpts {
	return FinDWHOpts{BatchSize: 2, Arh: ArhOpts{Tables: testArhTables, MinDate: "2026-06-01", WindowDays: 1}}
}

func TestArhStageTableUniqueForConcurrentReloads(t *testing.T) {
	const n = 100
	names := make(chan string, n)
	var wg sync.WaitGroup
	for range n {
		wg.Add(1)
		go func() {
			defer wg.Done()
			names <- arhStageTable("finance.fact_arh_debt_stage")
		}()
	}
	wg.Wait()
	close(names)

	seen := make(map[string]struct{}, n)
	for name := range names {
		if reStage.FindString(name) != name {
			t.Fatalf("недопустимое имя stage-таблицы: %q", name)
		}
		if _, exists := seen[name]; exists {
			t.Fatalf("повторное имя stage-таблицы: %q", name)
		}
		seen[name] = struct{}{}
	}
}

func TestCleanupArhStagesDropsOnlyStaleTables(t *testing.T) {
	f, ch, closeFn := newFakeCH()
	defer closeFn()
	stale := "finance.fact_arh_debt_stage_10_1700000000000000000_1"
	recent := "finance.fact_arh_debt_stage_11_1700000000000000001_2"
	f.stageRows[stale], f.stageRows[recent] = 5, 7
	f.staleStage[stale] = true

	s, _ := arhStreamFor(FinDWHArhDebt, testArhTables)
	if err := cleanupArhStages(context.Background(), ch, s); err != nil {
		t.Fatal(err)
	}
	if _, exists := f.stageRows[stale]; exists {
		t.Errorf("старая stage-таблица не удалена: %v", f.stageRows)
	}
	if f.stageRows[recent] != 7 {
		t.Errorf("свежая stage-таблица затронута: %v", f.stageRows)
	}
	if !strings.Contains(f.cleanupQ, "metadata_modification_time < now() - INTERVAL 12 HOUR") {
		t.Errorf("в cleanup-запросе нет порога возраста: %s", f.cleanupQ)
	}
}

func TestValidArhStageName(t *testing.T) {
	base := "fact_arh_debt_stage"
	if !validArhStageName(base, "fact_arh_debt_stage_10_1700000000000000000_1") {
		t.Fatal("валидное имя отклонено")
	}
	for _, name := range []string{
		"fact_arh_debt_stage", "fact_arh_debt_stage_1_2", "fact_arh_debt_stage_1_2_3;DROP", "fact_arh_sales_stage_1_2_3",
	} {
		if validArhStageName(base, name) {
			t.Errorf("недопустимое имя принято: %q", name)
		}
	}
}

// Обрыв посреди даты: основная таблица и отпечаток не тронуты, stage очищен, дата
// перепланируется и на следующем прогоне загружается целиком.
func TestRunArh_InterruptedDateIsRescheduled(t *testing.T) {
	f, ch, closeFn := newFakeCH()
	defer closeFn()
	f.main["2026-10-19"] = 5
	f.fp["2026-10-19"] = arhFPRow{Day: "2026-10-19", SrcChangedAt: "2026-10-19 09:00:00", SrcRows: 5, Ver: 1}
	src := &fakeSrc{rows: 6, failAt: map[string]int{"2026-10-19": 3},
		fp: map[string]arhFP{"2026-10-19": {Changed: "2026-10-19 12:00:00", Rows: 6}}}

	if _, err := runArh(context.Background(), src, ch, arhTestOpts(), FinDWHArhDebt, "2026-10-19"); err == nil {
		t.Fatalf("ожидалась ошибка обрыва")
	}
	if f.main["2026-10-19"] != 5 {
		t.Errorf("основная таблица тронута при обрыве: %d строк", f.main["2026-10-19"])
	}
	if f.fp["2026-10-19"].SrcRows != 5 || len(f.stageRows) != 0 {
		t.Errorf("отпечаток/stage: fp=%+v stage=%v", f.fp["2026-10-19"], f.stageRows)
	}
	for _, op := range f.ops {
		if strings.HasPrefix(op, "DELETE") || strings.HasPrefix(op, "SWAP") {
			t.Errorf("до полной загрузки замены быть не должно: %v", f.ops)
		}
	}

	// следующий прогон: источник здоров — дата перепланирована и заменена целиком
	src.failAt = nil
	f.ops = nil
	st, err := runArh(context.Background(), src, ch, arhTestOpts(), FinDWHArhDebt, "2026-10-19")
	if err != nil || st.Reloaded != 1 || st.Rows != 6 {
		t.Fatalf("st=%+v err=%v", st, err)
	}
	if f.main["2026-10-19"] != 6 || f.fp["2026-10-19"].SrcRows != 6 || f.fp["2026-10-19"].SrcChangedAt != "2026-10-19 12:00:00" {
		t.Errorf("после замены: main=%d fp=%+v", f.main["2026-10-19"], f.fp["2026-10-19"])
	}
	// порядок: маркер отпечатка → DELETE → SWAP → отпечаток дня ПОСЛЕ успеха
	order := strings.Join(f.ops, ",")
	if !regexp.MustCompile(`FP 2026-10-19 0,DELETE 2026-10-19,SWAP 2026-10-19,FP 2026-10-19 6`).MatchString(order) {
		t.Errorf("порядок операций: %s", order)
	}
}

// Отказ INSERT … SELECT после DELETE: отпечаток остаётся «не загружено» (src_rows = 0) и
// дата перепланируется, а не считается актуальной.
func TestRunArh_FailedSwapKeepsMarker(t *testing.T) {
	f, ch, closeFn := newFakeCH()
	defer closeFn()
	f.main["2026-10-19"] = 5
	f.fp["2026-10-19"] = arhFPRow{Day: "2026-10-19", SrcChangedAt: "2026-10-19 09:00:00", SrcRows: 5, Ver: 1}
	f.failSwap = true
	src := &fakeSrc{rows: 4, fp: map[string]arhFP{"2026-10-19": {Changed: "2026-10-19 12:00:00", Rows: 4}}}
	if _, err := runArh(context.Background(), src, ch, arhTestOpts(), FinDWHArhDebt, "2026-10-19"); err == nil {
		t.Fatalf("ожидалась ошибка замены")
	}
	if f.fp["2026-10-19"].SrcRows != 0 {
		t.Errorf("после оборванной замены отпечаток должен быть маркером: %+v", f.fp["2026-10-19"])
	}
	f.failSwap, f.ops = false, nil
	st, err := runArh(context.Background(), src, ch, arhTestOpts(), FinDWHArhDebt, "2026-10-19")
	if err != nil || st.Reloaded != 1 || f.main["2026-10-19"] != 4 {
		t.Errorf("перепланирование: st=%+v err=%v main=%d", st, err, f.main["2026-10-19"])
	}
}

// Совпали отпечатки (MAX DateChange и число строк) — дата пропускается без единой загрузки.
func TestRunArh_MatchingFingerprintSkips(t *testing.T) {
	f, ch, closeFn := newFakeCH()
	defer closeFn()
	f.main["2026-10-19"] = 4
	f.fp["2026-10-19"] = arhFPRow{Day: "2026-10-19", SrcChangedAt: "2026-10-19 12:00:00", SrcRows: 4, Ver: 1}
	src := &fakeSrc{rows: 4, fp: map[string]arhFP{"2026-10-19": {Changed: "2026-10-19 12:00:00", Rows: 4}}}
	st, err := runArh(context.Background(), src, ch, arhTestOpts(), FinDWHArhDebt, "2026-10-19")
	if err != nil || st.Reloaded != 0 || len(src.dayCalls) != 0 || len(f.ops) != 0 {
		t.Errorf("st=%+v err=%v dayCalls=%v ops=%v", st, err, src.dayCalls, f.ops)
	}
	// то же число строк, но другой MAX(DateChange) → перезагрузка
	src.fp["2026-10-19"] = arhFP{Changed: "2026-10-19 13:00:00", Rows: 4}
	if st, _ := runArh(context.Background(), src, ch, arhTestOpts(), FinDWHArhDebt, "2026-10-19"); st.Reloaded != 1 {
		t.Errorf("изменился DateChange — дата должна перезагрузиться: %+v", st)
	}
}

// Дата исчезла в источнике, а в CH осталась (данные и/или отпечаток) — удаляется.
func TestRunArh_VanishedDateDropped(t *testing.T) {
	f, ch, closeFn := newFakeCH()
	defer closeFn()
	f.main["2026-10-18"] = 3
	f.fp["2026-10-18"] = arhFPRow{Day: "2026-10-18", SrcChangedAt: "2026-10-18 09:00:00", SrcRows: 3, Ver: 1}
	f.main["2026-09-30"] = 2 // данные без валидного отпечатка (оборванная замена)
	src := &fakeSrc{rows: 1, fp: map[string]arhFP{}}
	st, err := runArh(context.Background(), src, ch, arhTestOpts(), FinDWHArhDebt, "2026-10-19")
	if err != nil || st.Dropped != 2 {
		t.Fatalf("st=%+v err=%v", st, err)
	}
	if len(f.main) != 0 || f.fp["2026-10-18"].SrcRows != 0 {
		t.Errorf("данные должны быть удалены, отпечаток — маркер: main=%v fp=%+v", f.main, f.fp["2026-10-18"])
	}
}
