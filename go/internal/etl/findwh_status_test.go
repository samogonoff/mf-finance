package etl

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestClassifyFinDWH(t *testing.T) {
	now := time.Date(2026, 10, 16, 10, 0, 0, 0, time.UTC)
	ok := func(date string) *FinDWHRunView { return &FinDWHRunView{SourceDate: date, Status: "ok"} }
	fail := &FinDWHRunView{SourceDate: "2026-10-16", Status: "error", Details: "ch insert: timeout\nstack"}
	yes, no := true, false
	since := now.Add(-5 * time.Hour)

	cases := []struct {
		name     string
		in       finDWHClassifyIn
		state    string
		severity string
		text     string // подстрока пояснения
	}{
		{"сегодня загружено", finDWHClassifyIn{Today: "2026-10-16", LastRun: ok("2026-10-16"), LastOK: ok("2026-10-16"), WorkerOn: true},
			"ok", "ok", "за сегодня"},
		{"сегодня загружено, потом упал CLI", finDWHClassifyIn{Today: "2026-10-16", LastRun: fail, LastOK: ok("2026-10-16"), WorkerOn: true},
			"ok", "warn", "ch insert: timeout"},
		{"ждём DWH утром", finDWHClassifyIn{Today: "2026-10-16", Hour: 7, SourceChecked: true, SourceReady: &no, ReadyCheck: true, LastRun: ok("2026-10-15"), LastOK: ok("2026-10-15"), WorkerOn: true},
			"waiting", "info", "Ждём"},
		{"DWH опаздывает", finDWHClassifyIn{Today: "2026-10-16", Hour: 13, SourceChecked: true, SourceReady: &no, ReadyCheck: true, LastRun: ok("2026-10-15"), LastOK: ok("2026-10-15"), WorkerOn: true},
			"late", "warn", "SQL Agent"},
		{"источник готов", finDWHClassifyIn{Today: "2026-10-16", Hour: 9, SourceChecked: true, SourceReady: &yes, LastRun: ok("2026-10-15"), LastOK: ok("2026-10-15"), WorkerOn: true},
			"queued", "info", "ближайшем тике"},
		{"без msdb: не опаздывает, а ждёт часа", finDWHClassifyIn{Today: "2026-10-16", Hour: 13, SourceChecked: true, SourceReady: &no, LastRun: ok("2026-10-15"), LastOK: ok("2026-10-15"), WorkerOn: true},
			"waiting", "info", "FINDWH_READY_AFTER_HOUR"},
		{"CH лежит", finDWHClassifyIn{CHDown: true, Today: "2026-10-16", WorkerOn: true},
			"ch_down", "error", "ClickHouse"},
		{"CH лежит, но воркер грузит", finDWHClassifyIn{CHDown: true, Running: true, RunningSince: &since, Now: now},
			"running", "info", "Загружается"},
		{"упал", finDWHClassifyIn{Today: "2026-10-16", LastRun: fail, LastOK: ok("2026-10-15"), WorkerOn: true},
			"error", "error", "Повтор"},
		{"устарело", finDWHClassifyIn{Today: "2026-10-16", SourceChecked: true, SourceReady: &no, LastRun: ok("2026-10-13"), LastOK: ok("2026-10-13"), WorkerOn: true},
			"stale", "error", "13.10.2026 (3 дн. назад)"},
		{"воркер выключен", finDWHClassifyIn{Today: "2026-10-16", LastRun: ok("2026-10-15"), LastOK: ok("2026-10-15")},
			"off", "warn", "воркер выключен"},
		{"MSSQL не ответил", finDWHClassifyIn{Today: "2026-10-16", LastRun: ok("2026-10-15"), LastOK: ok("2026-10-15"), WorkerOn: true},
			"source_unknown", "warn", "проверить не удалось"},
		{"не было прогонов", finDWHClassifyIn{Today: "2026-10-16", WorkerOn: true},
			"never", "warn", "ни разу"},
		{"грузится в норме", finDWHClassifyIn{Running: true, RunningSince: &since, TypicalSec: 3 * 3600, Progress: "пачка 3 из 40", Now: now},
			"running", "info", "пачка 3 из 40"},
		{"грузится слишком долго", finDWHClassifyIn{Running: true, RunningSince: &since, TypicalSec: 3600, Now: now},
			"running", "warn", "дольше обычного"},
	}
	for _, c := range cases {
		st, sev, text := classifyFinDWH(c.in)
		if st != c.state || sev != c.severity || !strings.Contains(text, c.text) {
			t.Errorf("%s: (%s, %s, %q), want (%s, %s, …%q…)", c.name, st, sev, text, c.state, c.severity, c.text)
		}
		if strings.Contains(text, "stack") {
			t.Errorf("%s: в пояснение попала не только первая строка ошибки: %q", c.name, text)
		}
	}
}

// fakeCH отвечает на SELECT'ы страницы статуса: первое совпадение подстроки
// по порядку (не map — иначе выбор при пересекающихся ключах случаен).
func fakeCH(t *testing.T, answers [][2]string) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		b, _ := io.ReadAll(r.Body)
		q := string(b)
		if !strings.Contains(q, "FORMAT JSONEachRow") || !strings.Contains(q, "output_format_json_quote_64bit_integers = 0") {
			t.Errorf("запрос без JSONEachRow/settings: %s", q)
		}
		for _, a := range answers {
			if strings.Contains(q, a[0]) {
				io.WriteString(w, a[1])
				return
			}
		}
		io.WriteString(w, "")
	}))
}

func TestFinDWHStatusReport(t *testing.T) {
	ch := fakeCH(t, [][2]string{
		{"LIMIT 7 BY", `{"table_name":"ex_rates","source_date":"2026-10-16","started_at":1792130000,"finished_at":1792130012,"status":"ok","rows":19500,"details":"","triggered_by":"cron"}
{"table_name":"glmf","source_date":"2026-10-16","started_at":1792128000,"finished_at":1792131600,"status":"error","rows":0,"details":"glmf MF [202510]: stage не сходится","triggered_by":"cron"}
{"table_name":"glmf","source_date":"2026-10-15","started_at":1792040000,"finished_at":1792043600,"status":"ok","rows":1200000,"details":"days=650 dirty=3","triggered_by":"cron"}
`},
		{"LIMIT 40", `{"table_name":"ex_rates","source_date":"2026-10-16","started_at":1792130000,"finished_at":1792130012,"status":"ok","rows":19500,"details":"","triggered_by":"cron"}` + "\n"},
		{"findwh_glmf_day_fp", `{"fp_rows":116999000,"days":650,"companies":9,"date_from":"2025-01-01","date_to":"2026-10-15","loaded_utc":"2026-10-15 12:25:26"}`},
		{"FROM finance.findwh_glmf", `{"rows":117000000}`},
		{"FROM finance.findwh_ex_rates", `{"rows":19500,"date_from":"2015-01-01","date_to":"2026-10-16","loaded":1792130012}`},
	})
	defer ch.Close()

	s := NewFinDWHStatus(Deps{CHURL: ch.URL}, FinDWHOpts{}, 0, nil, "FINDWH_SYNC_INTERVAL=0")
	rep := s.Report(context.Background())

	if rep.CHError != "" {
		t.Fatalf("ch error: %s", rep.CHError)
	}
	if rep.Worker.Enabled || rep.Worker.DisabledReason != "FINDWH_SYNC_INTERVAL=0" {
		t.Errorf("worker: %+v", rep.Worker)
	}
	if rep.Source.Checked || rep.Source.Error == "" {
		t.Errorf("source без MSSQL: %+v", rep.Source)
	}
	if len(rep.Runs) != 1 || rep.Runs[0].DurationSec != 12 {
		t.Errorf("runs: %+v", rep.Runs)
	}
	if len(rep.Tables) != 3 {
		t.Fatalf("tables: %d", len(rep.Tables))
	}
	by := map[string]FinDWHTableStatus{}
	for _, ts := range rep.Tables {
		by[ts.Table] = ts
	}

	g := by[FinDWHGLMF]
	if g.LastRun == nil || g.LastRun.Status != "error" || g.LastOK == nil || g.LastOK.SourceDate != "2026-10-15" {
		t.Errorf("glmf last/ok: %+v / %+v", g.LastRun, g.LastOK)
	}
	if g.State != "error" || g.TypicalDurationSec != 3600 {
		t.Errorf("glmf state=%s typical=%d", g.State, g.TypicalDurationSec)
	}
	if g.Data.LoadedAt == nil || !g.Data.LoadedAt.Equal(time.Date(2026, 10, 15, 12, 25, 26, 0, time.UTC)) {
		t.Errorf("glmf loaded_at (checked_at пишется в UTC): %v", g.Data.LoadedAt)
	}
	if g.Data.Rows == nil || *g.Data.Rows != 117000000 || g.Data.FPRows == nil || g.Data.Days != 650 || g.Data.From != "2025-01-01" {
		t.Errorf("glmf data: %+v", g.Data)
	}
	if !strings.Contains(g.StateText, "по отпечаткам 116999000") {
		t.Errorf("glmf: нет предупреждения о расхождении с отпечатками: %q", g.StateText)
	}

	if fp := by[FinDWHTableFinPL]; fp.State != "never" || fp.Data.Rows == nil || *fp.Data.Rows != 0 || fp.Data.From != "" {
		t.Errorf("table_fin_pl: %+v", fp)
	}
	if ex := by[FinDWHExRates]; ex.LoadedFor != "2026-10-16" || ex.Data.To != "2026-10-16" {
		t.Errorf("ex_rates: %+v", ex)
	}
	if rep.Overall != "error" {
		t.Errorf("overall=%s", rep.Overall)
	}
}

func TestFinDWHStatusCHDown(t *testing.T) {
	s := NewFinDWHStatus(Deps{CHURL: "http://127.0.0.1:1"}, FinDWHOpts{}, 1800, nil, "MSSQL_PREMASTER_* не заданы")
	rep := s.Report(context.Background())
	if rep.CHError == "" || rep.Overall != "error" || len(rep.Tables) != 3 || len(rep.Settings) == 0 {
		t.Errorf("ch down: overall=%s err=%q tables=%d", rep.Overall, rep.CHError, len(rep.Tables))
	}
	for _, ts := range rep.Tables {
		if ts.State != "ch_down" || ts.Severity != "error" {
			t.Errorf("%s: при недоступном CH state=%s severity=%s", ts.Table, ts.State, ts.Severity)
		}
		if ts.Data.Error == "" {
			t.Errorf("%s: без ошибки данных при недоступном CH", ts.Table)
		}
	}
}

func TestFinDWHWorkerState(t *testing.T) {
	w := NewFinDWHWorker(Deps{}, FinDWHOpts{})
	done := make(chan struct{})
	go func() { // читатель параллельно с писателем — для go test -race
		defer close(done)
		for i := 0; i < 1000; i++ {
			_ = w.State()
		}
	}()
	w.opts.report(FinDWHExRates, "")
	if st := w.State(); st.Running != FinDWHExRates || st.RunningSince == nil {
		t.Errorf("старт таблицы: %+v", st)
	}
	since := w.State().RunningSince
	w.opts.report(FinDWHExRates, "шаг")
	if st := w.State(); st.RunningSince != since || st.Progress != "шаг" {
		t.Errorf("прогресс той же таблицы сбросил время старта: %+v", st)
	}
	w.opts.report("", "")
	if st := w.State(); st.Running != "" || st.RunningSince != nil || st.Progress != "" {
		t.Errorf("конец таблицы: %+v", st)
	}
	w.opts.report(FinDWHGLMF, "пачка 1 из 2")
	if st := w.State(); st.Running != FinDWHGLMF || st.Progress != "пачка 1 из 2" {
		t.Errorf("следующая таблица: %+v", st)
	}
	<-done
}

func TestFinDWHStatusTickErrorRaisesOverall(t *testing.T) {
	ch := fakeCH(t, [][2]string{
		{"LIMIT 7 BY", `{"table_name":"ex_rates","source_date":"2026-10-15","started_at":1,"finished_at":2,"status":"ok","rows":1,"details":"","triggered_by":"cron"}`},
	})
	defer ch.Close()
	w := NewFinDWHWorker(Deps{}, FinDWHOpts{})
	w.state = FinDWHWorkerState{Enabled: true, IntervalSec: 1800, LastTickError: "findwh: часы MSSQL: EOF"}
	rep := NewFinDWHStatus(Deps{CHURL: ch.URL}, FinDWHOpts{Tables: []string{FinDWHExRates}}, 1800, w, "").Report(context.Background())
	if severityRank[rep.Overall] < severityRank["warn"] {
		t.Errorf("ошибка тика не подняла общий статус: %s", rep.Overall)
	}
}

func TestFinDWHFallbackToday(t *testing.T) {
	// MSSQL не подключён: «сегодня» по UTC+3, а не по UTC контейнера.
	s := NewFinDWHStatus(Deps{}, FinDWHOpts{}, 0, nil, "test")
	v := s.readMSSQL(context.Background(), time.Date(2026, 10, 16, 22, 30, 0, 0, time.UTC))
	if v.src.Checked || v.src.Today != "2026-10-17" {
		t.Errorf("fallback today: %+v", v.src)
	}
}

func TestRunViewHidesCHUser(t *testing.T) {
	v := finDWHRun{Details: `findwh glmf: Post "http://finance:***@10.0.0.5:8123/?query=x": EOF`}.view()
	if strings.Contains(v.Details, "finance") || !strings.Contains(v.Details, "http://10.0.0.5:8123/") {
		t.Errorf("details: %q", v.Details)
	}
}

func TestCleanErrHidesURL(t *testing.T) {
	_, err := http.Post("http://finance:secret@127.0.0.1:1/", "text/plain", nil)
	if err == nil {
		t.Skip("порт 1 неожиданно ответил")
	}
	wrapped := fmt.Errorf("findwh glmf: %w", err)
	got := cleanErr(wrapped)
	if strings.Contains(got, "finance") || strings.Contains(got, "127.0.0.1:1/") || !strings.HasPrefix(got, "findwh glmf: Post: ") {
		t.Errorf("cleanErr: %q", got)
	}
}
