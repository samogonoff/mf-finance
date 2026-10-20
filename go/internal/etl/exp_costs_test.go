package etl

import (
	"context"
	"database/sql"
	"errors"
	"sync"
	"testing"
	"time"
)

func TestExpCostsValue(t *testing.T) {
	d := time.Date(2025, 3, 1, 0, 0, 0, 0, time.UTC)
	cases := []struct {
		in   any
		kind colKind
		want sql.NullString
	}{
		{nil, kStr, sql.NullString{}},
		{d, kDate, sql.NullString{String: "2025-03-01", Valid: true}},
		{[]byte(" 12.50 "), kDec, sql.NullString{String: "12.50", Valid: true}},
		{float64(1234567.891), kDec, sql.NullString{String: "1234567.891", Valid: true}},
		{float64(1e9), kDec, sql.NullString{String: "1000000000", Valid: true}}, // без экспоненты
		{int64(42), kInt, sql.NullString{String: "42", Valid: true}},
		{true, kStr, sql.NullString{String: "1", Valid: true}},
		{"  Магазины ", kStr, sql.NullString{String: "Магазины", Valid: true}},
		// Нечисловой мусор в числовой колонке даёт 0, а не отказ всего батча в CH.
		{"n/a", kInt, sql.NullString{String: "0", Valid: true}},
		{"", kInt, sql.NullString{String: "0", Valid: true}},
		{" 101.0 ", kInt, sql.NullString{String: "101", Valid: true}},
		{float64(250), kInt, sql.NullString{String: "250", Valid: true}},
		{"12,5", kDec, sql.NullString{String: "12.5", Valid: true}},
		{"abc", kDec, sql.NullString{String: "0", Valid: true}},
		{nil, kInt, sql.NullString{}},
	}
	for _, c := range cases {
		if got := expCostsValue(c.in, c.kind); got != c.want {
			t.Errorf("%v: got %+v, want %+v", c.in, got, c.want)
		}
	}
}

// Колонки процедуры ищутся без учёта регистра; нет обязательной — ошибка с перечнем;
// необязательных (например, company, которых процедура не отдаёт) может не быть.
func TestExpCostsLayout(t *testing.T) {
	cols := []string{"HalfYear", "scenario", "COUNTRY", "Month", "CodePL", "GroupPL", "Expense", "CodeCFO", "GroupCFO1", "Amount"}
	idx, err := expCostsLayout(cols)
	if err != nil {
		t.Fatal(err)
	}
	if idx[0] != 1 || idx[1] != 2 || idx[2] != 3 {
		t.Fatalf("позиции: %v", idx)
	}
	for i, c := range expCostsCols {
		if c.Name == "cfo" && idx[i] != -1 {
			t.Errorf("отсутствующая необязательная колонка cfo должна быть -1, got %d", idx[i])
		}
	}
	if _, err := expCostsLayout([]string{"Scenario", "Month"}); err == nil {
		t.Fatal("ждали ошибку про обязательные колонки")
	}
}

func TestExpCostsOptsDefaults(t *testing.T) {
	var o ExpCostsOpts
	o.normalize()
	if o.AfterHour != 5 || o.BeforeHour != 9 || o.MinYear != 2024 || o.Proc == "" {
		t.Fatalf("дефолты: %+v", o)
	}
	// Имя процедуры без базы (как в .env.example) дополняется базой Budgeting.
	o2 := ExpCostsOpts{Proc: "ZATRATY_DLYA_OKSANY_BYSTRYE"}
	o2.normalize()
	if o2.Proc != "[Budgeting].[dbo].[ZATRATY_DLYA_OKSANY_BYSTRYE]" {
		t.Fatalf("имя процедуры: %q", o2.Proc)
	}
	if got := qualifyBudgetingProc("fin.Proc"); got != "[Budgeting].[fin].[Proc]" {
		t.Fatalf("схема.имя: %q", got)
	}
	o3 := ExpCostsOpts{Proc: "[Budgeting].[dbo].[X]"}
	o3.normalize()
	if o3.Proc != "[Budgeting].[dbo].[X]" {
		t.Fatalf("имя с базой должно сохраняться: %q", o3.Proc)
	}
}

func TestExpCostsSwapAllowed(t *testing.T) {
	cases := []struct {
		rows, have int64
		ok         bool
	}{
		{771165, 0, true},      // первая заливка
		{771165, 760000, true}, // обычный день
		{400000, 771165, true}, // сократилось, но меньше чем вдвое не упало
		{300000, 771165, false},
		{0, 771165, false}, // пустой ответ процедуры
		{0, 0, false},
	}
	for _, c := range cases {
		if err := expCostsSwapAllowed(c.rows, c.have); (err == nil) != c.ok {
			t.Errorf("rows=%d have=%d: err=%v, want ok=%v", c.rows, c.have, err, c.ok)
		}
	}
}

// Колонка проекта: и ProjectName (действующая выдача), и Projects (как в ТЗ) попадают в
// project_name — иначе фильтр проектов молча пропустил бы всё и суммы задвоились бы.
func TestExpCostsLayout_ProjectColumnAlternatives(t *testing.T) {
	base := []string{"Scenario", "Country", "Month", "CodePL", "CodeCFO", "GroupCFO1", "Amount"}
	for _, name := range []string{"ProjectName", "Projects"} {
		idx, err := expCostsLayout(append(append([]string{}, base...), name))
		if err != nil {
			t.Fatal(err)
		}
		found := false
		for i, c := range expCostsCols {
			if c.Name == "project_name" && idx[i] == len(base) {
				found = true
			}
		}
		if !found {
			t.Errorf("колонка %s не сопоставлена project_name", name)
		}
	}
}

// stubExpCostsHooks подменяет часы источника, проверку «нужна ли заливка» и саму заливку;
// load вызывается вместо настоящей, need — вместо проверки журнала.
func stubExpCostsHooks(t *testing.T, need func() (bool, error), load func(ctx context.Context) (int64, int64, error)) {
	t.Helper()
	oc, on, ol := expCostsSourceClock, expCostsNeededFn, expCostsLoad
	expCostsSourceClock = func(context.Context, *sql.DB) (string, int, error) { return "2026-10-19", 6, nil }
	expCostsNeededFn = func(context.Context, *chClient, string, int, ExpCostsOpts) (bool, error) { return need() }
	expCostsLoad = func(ctx context.Context, _ Deps, _ *chClient, _ ExpCostsOpts) (int64, int64, error) { return load(ctx) }
	t.Cleanup(func() { expCostsSourceClock, expCostsNeededFn, expCostsLoad = oc, on, ol })
}

// Кнопка «Обновить из источника», пока идёт чужая заливка, ждёт её окончания (и свежий снимок
// получает), а не отвечает ошибкой «заливка уже идёт» и не перезаливает второй раз; ожидание
// прерывается по контексту.
func TestExpCostsRefreshNow_WaitsForRunningLoad(t *testing.T) {
	release := make(chan struct{})
	var mu sync.Mutex
	loads := 0
	stubExpCostsHooks(t, func() (bool, error) { return true, nil }, func(ctx context.Context) (int64, int64, error) {
		mu.Lock()
		loads++
		mu.Unlock()
		select {
		case <-release:
		case <-ctx.Done():
		}
		return 10, 0, nil
	})
	w := NewExpCostsWorker(Deps{CHURL: "http://localhost:1"}, ExpCostsOpts{})

	first := make(chan error, 1)
	go func() { first <- w.RefreshNow(context.Background()) }()
	deadline := time.Now().Add(2 * time.Second)
	for {
		mu.Lock()
		n := loads
		mu.Unlock()
		if n == 1 {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("первая заливка не началась")
		}
		time.Sleep(5 * time.Millisecond)
	}

	second := make(chan error, 1)
	go func() { second <- w.RefreshNow(context.Background()) }()
	select {
	case err := <-second:
		t.Fatalf("вторая кнопка не дождалась окончания чужой заливки: %v", err)
	case <-time.After(150 * time.Millisecond):
	}
	// Ожидание прерывается по контексту.
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Millisecond)
	if err := w.RefreshNow(ctx); err == nil {
		t.Fatal("ожидание должно прерываться по контексту")
	}
	cancel()

	close(release)
	for _, ch := range []chan error{first, second} {
		select {
		case err := <-ch:
			if err != nil {
				t.Fatalf("ждали nil, got %v", err)
			}
		case <-time.After(2 * time.Second):
			t.Fatal("кнопка не вернулась после окончания заливки")
		}
	}
	mu.Lock()
	defer mu.Unlock()
	if loads != 1 {
		t.Fatalf("заливок %d, ждали 1 (вторая кнопка только дожидается первой)", loads)
	}
}

// Короткая проверка воркера «нужна ли заливка?» не занимает очередь заливок: кнопка в это
// время не принимает её за чужую заливку.
func TestExpCostsQueueNotHeldByNeedCheck(t *testing.T) {
	inCheck := make(chan struct{})
	releaseCheck := make(chan struct{})
	stubExpCostsHooks(t, func() (bool, error) {
		close(inCheck)
		<-releaseCheck
		return false, nil // заливка не нужна
	}, func(context.Context) (int64, int64, error) { return 0, 0, nil })
	w := NewExpCostsWorker(Deps{CHURL: "http://localhost:1"}, ExpCostsOpts{})
	done := make(chan struct{})
	go func() {
		_, _, _ = RunExpCostsDaily(context.Background(), w.deps, w.opts)
		close(done)
	}()
	<-inCheck
	select {
	case expCostsRunSem <- struct{}{}:
		<-expCostsRunSem
	default:
		t.Fatal("очередь заливок занята во время проверки «нужна ли заливка»")
	}
	close(releaseCheck)
	<-done
}

// Кнопка, дождавшаяся чужой заливки, узнаёт, что та упала, и сообщает об этом.
func TestExpCostsRefreshNow_ReportsFailureOfAwaitedLoad(t *testing.T) {
	release := make(chan struct{})
	started := make(chan struct{})
	var once sync.Once
	stubExpCostsHooks(t, func() (bool, error) { return true, nil }, func(ctx context.Context) (int64, int64, error) {
		once.Do(func() { close(started) })
		<-release
		return 0, 0, errors.New("процедура вернула 0 строк")
	})
	w := NewExpCostsWorker(Deps{CHURL: "http://localhost:1"}, ExpCostsOpts{})
	nightly := make(chan error, 1)
	go func() { // «ночная» заливка: берёт очередь первой и падает
		o := w.opts
		o.Force = true
		_, _, err := RunExpCostsDaily(context.Background(), w.deps, o)
		nightly <- err
	}()
	<-started
	button := make(chan error, 1)
	go func() { button <- w.RefreshNow(context.Background()) }()
	time.Sleep(100 * time.Millisecond)
	close(release)
	<-nightly
	select {
	case err := <-button:
		if err == nil {
			t.Fatal("сбой чужой заливки должен дойти до кнопки")
		}
	case <-time.After(2 * time.Second):
		t.Fatal("кнопка не вернулась")
	}
}
