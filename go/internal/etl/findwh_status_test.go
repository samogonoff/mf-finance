package etl

import (
	"context"
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
		{"ждём DWH утром", finDWHClassifyIn{Today: "2026-10-16", Hour: 7, SourceChecked: true, SourceReady: &no, LastRun: ok("2026-10-15"), LastOK: ok("2026-10-15"), WorkerOn: true},
			"waiting", "info", "Ждём"},
		{"DWH опаздывает", finDWHClassifyIn{Today: "2026-10-16", Hour: 13, SourceChecked: true, SourceReady: &no, LastRun: ok("2026-10-15"), LastOK: ok("2026-10-15"), WorkerOn: true},
			"late", "warn", "SQL Agent"},
		{"источник готов", finDWHClassifyIn{Today: "2026-10-16", Hour: 9, SourceChecked: true, SourceReady: &yes, LastRun: ok("2026-10-15"), LastOK: ok("2026-10-15"), WorkerOn: true},
			"queued", "info", "ближайшем тике"},
		{"упал", finDWHClassifyIn{Today: "2026-10-16", LastRun: fail, LastOK: ok("2026-10-15"), WorkerOn: true},
			"error", "error", "Повтор"},
		{"устарело", finDWHClassifyIn{Today: "2026-10-16", SourceChecked: true, SourceReady: &no, LastRun: ok("2026-10-13"), LastOK: ok("2026-10-13"), WorkerOn: true},
			"stale", "error", "13.10.2026 (3 дн. назад)"},
		{"воркер выключен", finDWHClassifyIn{Today: "2026-10-16", LastRun: ok("2026-10-15"), LastOK: ok("2026-10-15")},
			"off", "warn", "воркер выключен"},
		{"MSSQL не ответил", finDWHClassifyIn{Today: "2026-10-16", LastRun: ok("2026-10-15"), LastOK: ok("2026-10-15"), WorkerOn: true},
			"unknown", "warn", "проверить не удалось"},
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

// fakeCH отвечает на SELECT'ы страницы статуса по подстроке запроса.
func fakeCH(t *testing.T, answers map[string]string) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		b, _ := io.ReadAll(r.Body)
		q := string(b)
		if !strings.Contains(q, "FORMAT JSONEachRow") || !strings.Contains(q, "output_format_json_quote_64bit_integers = 0") {
			t.Errorf("запрос без JSONEachRow/settings: %s", q)
		}
		for k, v := range answers {
			if strings.Contains(q, k) {
				io.WriteString(w, v)
				return
			}
		}
		io.WriteString(w, "")
	}))
}

func TestFinDWHStatusReport(t *testing.T) {
	ch := fakeCH(t, map[string]string{
		"LIMIT 7 BY": `{"table_name":"ex_rates","source_date":"2026-10-16","started_at":1792130000,"finished_at":1792130012,"status":"ok","rows":19500,"details":"","triggered_by":"cron"}
{"table_name":"glmf","source_date":"2026-10-16","started_at":1792128000,"finished_at":1792131600,"status":"error","rows":0,"details":"glmf MF [202510]: stage не сходится","triggered_by":"cron"}
{"table_name":"glmf","source_date":"2026-10-15","started_at":1792040000,"finished_at":1792043600,"status":"ok","rows":1200000,"details":"days=650 dirty=3","triggered_by":"cron"}
`,
		"LIMIT 40":                     `{"table_name":"ex_rates","source_date":"2026-10-16","started_at":1792130000,"finished_at":1792130012,"status":"ok","rows":19500,"details":"","triggered_by":"cron"}` + "\n",
		"FROM finance.findwh_glmf\n":   `{"rows":117000000}`,
		"findwh_glmf_day_fp":           `{"fp_rows":116999000,"days":650,"companies":9,"date_from":"2025-01-01","date_to":"2026-10-15","loaded":1792043600}`,
		"FROM finance.findwh_ex_rates": `{"rows":19500,"date_from":"2015-01-01","date_to":"2026-10-16","loaded":1792130012}`,
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
		if ts.State != "unknown" || ts.Severity != "error" {
			t.Errorf("%s: при недоступном CH state=%s severity=%s", ts.Table, ts.State, ts.Severity)
		}
		if ts.Data.Error == "" {
			t.Errorf("%s: без ошибки данных при недоступном CH", ts.Table)
		}
	}
}
