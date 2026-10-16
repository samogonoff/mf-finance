package etl

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"sort"
	"strconv"
	"strings"
	"time"
)

// Страница статуса реплики FinDWH → CH (GET /api/admin/etl/findwh, /admin/etl в
// кабинете): настройки, состояние воркера и по каждой таблице — свежесть данных,
// последний прогон, готовность источника и объём реплики. Всё читается на лету:
// журнал прогонов и данные — из CH, «сегодня» и готовность — из MSSQL, текущая
// загрузка — из памяти воркера.

// finDWHLateHour — час по часам MSSQL, после которого несобранная в DWH таблица
// уже повод разбираться: по расписанию (docs/bi/findwh-etl.md §2) всё готово к ≈08:30.
const finDWHLateHour = 12

// finDWHTableMeta — подписи таблицы для страницы.
type finDWHTableMeta struct {
	Title  string
	Source string
	Target string
	Method string
}

var finDWHMeta = map[string]finDWHTableMeta{
	FinDWHExRates: {
		Title:  "Курсы USD",
		Source: "dbo.ExRates, ExRatesRU/KZ/UZ/TR",
		Target: "finance.findwh_ex_rates",
		Method: "полный снимок, подмена EXCHANGE TABLES",
	},
	FinDWHTableFinPL: {
		Title:  "ОПУ (Table_Fin_PL)",
		Source: "dbo.Table_Fin_PL",
		Target: "finance.findwh_table_fin_pl",
		Method: "полный снимок, подмена EXCHANGE TABLES",
	},
	FinDWHGLMF: {
		Title:  "Проводки GLMF",
		Source: "dbo.GLMF",
		Target: "finance.findwh_glmf",
		Method: "сверка отпечатков дней, замена секций REPLACE PARTITION",
	},
}

// FinDWHStatus — сборщик статуса. deps.MSSQL может быть nil (креды не заданы,
// mock) — тогда готовность источника не проверяется. worker nil — воркер не
// запущен, причина в workerOff.
type FinDWHStatus struct {
	deps        Deps
	opts        FinDWHOpts
	intervalSec int
	worker      *FinDWHWorker
	workerOff   string
}

// NewFinDWHStatus — конструктор.
func NewFinDWHStatus(deps Deps, opts FinDWHOpts, intervalSec int, worker *FinDWHWorker, workerOff string) *FinDWHStatus {
	opts.normalize()
	return &FinDWHStatus{deps: deps, opts: opts, intervalSec: intervalSec, worker: worker, workerOff: workerOff}
}

// FinDWHStatusReport — ответ ручки статуса.
type FinDWHStatusReport struct {
	CheckedAt time.Time           `json:"checked_at"`
	Overall   string              `json:"overall"` // ok | info | warn | error
	Settings  []FinDWHSetting     `json:"settings"`
	Worker    FinDWHWorkerState   `json:"worker"`
	Source    FinDWHSourceState   `json:"source"`
	CHError   string              `json:"ch_error,omitempty"`
	Tables    []FinDWHTableStatus `json:"tables"`
	Runs      []FinDWHRunView     `json:"runs"`
}

// FinDWHSetting — переменная окружения и её действующее значение.
type FinDWHSetting struct {
	Env   string `json:"env"`
	Value string `json:"value"`
	Hint  string `json:"hint"`
}

// FinDWHSourceState — часы сервера MSSQL: по ним живёт расписание DWH.
type FinDWHSourceState struct {
	Checked bool   `json:"checked"`         // удалось спросить MSSQL
	Today   string `json:"today"`           // YYYY-MM-DD по MSSQL (без него — по часам go-api)
	Hour    int    `json:"hour"`            // час по MSSQL
	Error   string `json:"error,omitempty"` // почему не спросили / не ответил
}

// FinDWHRunView — прогон из finance.findwh_etl_runs.
type FinDWHRunView struct {
	Table       string    `json:"table"`
	SourceDate  string    `json:"source_date"`
	StartedAt   time.Time `json:"started_at"`
	FinishedAt  time.Time `json:"finished_at"`
	DurationSec int64     `json:"duration_sec"`
	Status      string    `json:"status"`
	Rows        int64     `json:"rows"`
	Details     string    `json:"details"`
	TriggeredBy string    `json:"triggered_by"`
}

// FinDWHDataStats — что сейчас лежит в реплике.
type FinDWHDataStats struct {
	Rows     *int64     `json:"rows,omitempty"`
	From     string     `json:"from,omitempty"` // диапазон дат данных
	To       string     `json:"to,omitempty"`
	LoadedAt *time.Time `json:"loaded_at,omitempty"` // GLMF — последняя сверка отпечатков
	Error    string     `json:"error,omitempty"`
	// Только GLMF: сколько строк, дней и компаний по отпечаткам. FPRows ≠ Rows —
	// секции заменили, а отпечатки не записали (или правили руками).
	FPRows    *int64 `json:"fp_rows,omitempty"`
	Days      int64  `json:"days,omitempty"`
	Companies int64  `json:"companies,omitempty"`
}

// FinDWHTableStatus — статус одной таблицы.
type FinDWHTableStatus struct {
	Table              string          `json:"table"`
	Title              string          `json:"title"`
	Source             string          `json:"source"`
	Target             string          `json:"target"`
	Method             string          `json:"method"`
	ReadyRule          string          `json:"ready_rule"`
	State              string          `json:"state"`    // ok | running | waiting | queued | late | error | stale | off | unknown | never
	Severity           string          `json:"severity"` // ok | info | warn | error
	StateText          string          `json:"state_text"`
	SourceReady        *bool           `json:"source_ready,omitempty"`
	SourceReadyError   string          `json:"source_ready_error,omitempty"`
	LoadedFor          string          `json:"loaded_for,omitempty"` // дата источника последней успешной загрузки
	LagDays            *int            `json:"lag_days,omitempty"`
	LastRun            *FinDWHRunView  `json:"last_run,omitempty"`
	LastOK             *FinDWHRunView  `json:"last_ok,omitempty"`
	TypicalDurationSec int64           `json:"typical_duration_sec,omitempty"` // медиана последних успешных
	Data               FinDWHDataStats `json:"data"`
}

// ServeHTTP — GET /api/admin/etl/findwh.
func (s *FinDWHStatus) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := context.WithTimeout(r.Context(), 30*time.Second)
	defer cancel()
	rep := s.Report(ctx)
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	if err := json.NewEncoder(w).Encode(rep); err != nil {
		log.Printf("findwh-status: encode: %v", err)
	}
}

// Report собирает статус. Ошибки источников не валят ответ, а попадают в поля
// *Error — страница должна открываться именно тогда, когда что-то сломано.
func (s *FinDWHStatus) Report(ctx context.Context) FinDWHStatusReport {
	now := time.Now()
	rep := FinDWHStatusReport{CheckedAt: now, Settings: s.settings(), Runs: []FinDWHRunView{}}
	if s.worker != nil {
		rep.Worker = s.worker.State()
	} else {
		rep.Worker = FinDWHWorkerState{DisabledReason: s.workerOff, IntervalSec: s.intervalSec}
	}

	// Часы MSSQL: «сегодня» и час для расписания DWH.
	rep.Source.Today = now.Format("2006-01-02")
	if s.deps.MSSQL == nil {
		rep.Source.Error = "MSSQL не подключён: " + s.workerOff
	} else {
		mctx, cancel := context.WithTimeout(ctx, 10*time.Second)
		date, hour, err := sourceClock(mctx, s.deps.MSSQL)
		cancel()
		if err != nil {
			rep.Source.Error = err.Error()
		} else {
			rep.Source.Checked, rep.Source.Today, rep.Source.Hour = true, date, hour
		}
	}

	var (
		recent  []finDWHRun
		journal []finDWHRun
	)
	ch, err := newCHClient(s.deps.CHURL, s.deps.CHUser, s.deps.CHPass)
	if err == nil {
		// Последние 7 успешных и 7 упавших на таблицу: из них последний прогон,
		// последний успешный и типичная длительность. Журнал — отдельно, по времени.
		recent, err = chQueryJSON[finDWHRun](ctx, ch, `SELECT table_name, toString(source_date) AS source_date,
  toUnixTimestamp(started_at) AS started_at, toUnixTimestamp(finished_at) AS finished_at,
  status, rows, details, triggered_by
FROM finance.findwh_etl_runs ORDER BY started_at DESC LIMIT 7 BY table_name, status`)
	}
	if err == nil {
		journal, err = chQueryJSON[finDWHRun](ctx, ch, `SELECT table_name, toString(source_date) AS source_date,
  toUnixTimestamp(started_at) AS started_at, toUnixTimestamp(finished_at) AS finished_at,
  status, rows, details, triggered_by
FROM finance.findwh_etl_runs ORDER BY started_at DESC LIMIT 40`)
	}
	if err != nil {
		rep.CHError = err.Error()
		ch = nil
	}
	for _, r := range journal {
		rep.Runs = append(rep.Runs, r.view())
	}

	for _, table := range s.opts.Tables {
		rep.Tables = append(rep.Tables, s.tableStatus(ctx, ch, table, recent, rep.Source, rep.Worker, now))
	}

	rep.Overall = "ok"
	if rep.CHError != "" {
		rep.Overall = "error"
	}
	if !rep.Worker.Enabled {
		rep.Overall = worseSeverity(rep.Overall, "warn")
	}
	for _, t := range rep.Tables {
		rep.Overall = worseSeverity(rep.Overall, t.Severity)
	}
	return rep
}

func (s *FinDWHStatus) settings() []FinDWHSetting {
	interval := strconv.Itoa(s.intervalSec)
	if s.intervalSec > 0 {
		interval += " (" + (time.Duration(s.intervalSec) * time.Second).String() + ")"
	}
	readyHint := "грузить после успешного шага SQL Agent за сегодня (нужен SELECT на msdb)"
	if !s.opts.ReadyCheck {
		readyHint = "без msdb: грузить по часам MSSQL после FINDWH_READY_AFTER_HOUR"
	}
	return []FinDWHSetting{
		{"FINDWH_SYNC_INTERVAL", interval, "как часто воркер проверяет, не пора ли грузить, сек; 0 — воркер выключен"},
		{"FINDWH_DB", s.opts.DB, "БД источника на сервере MSSQL_PREMASTER_SERVER"},
		{"FINDWH_GLMF_FROM", s.opts.GLMFFrom.Format("2006-01-02"), "нижняя граница GLMF.Date в реплике"},
		{"FINDWH_GLMF_CHUNK_MONTHS", strconv.Itoa(s.opts.ChunkMonths), "месяцев одной компании за один запрос к MSSQL"},
		{"FINDWH_BATCH", strconv.Itoa(s.opts.BatchSize), "строк в одном INSERT в ClickHouse"},
		{"FINDWH_READY_CHECK", boolEnv(s.opts.ReadyCheck), readyHint},
		{"FINDWH_READY_AFTER_HOUR", strconv.Itoa(s.opts.ReadyAfterHour), "час по MSSQL, раньше которого не грузим (только при FINDWH_READY_CHECK=0)"},
	}
}

func boolEnv(b bool) string {
	if b {
		return "1"
	}
	return "0"
}

func (s *FinDWHStatus) readyRule(table string) string {
	if !s.opts.ReadyCheck {
		return fmt.Sprintf("по часам MSSQL, не раньше %d:00", s.opts.ReadyAfterHour)
	}
	if j, ok := finDWHReadyJobs[table]; ok {
		return fmt.Sprintf("SQL Agent «%s», шаг %d — успешно за сегодня", j.Job, j.Step)
	}
	return ""
}

func (s *FinDWHStatus) tableStatus(ctx context.Context, ch *chClient, table string, recent []finDWHRun,
	src FinDWHSourceState, ws FinDWHWorkerState, now time.Time) FinDWHTableStatus {
	meta := finDWHMeta[table]
	ts := FinDWHTableStatus{
		Table: table, Title: meta.Title, Source: meta.Source, Target: meta.Target,
		Method: meta.Method, ReadyRule: s.readyRule(table),
	}
	if ts.Title == "" {
		ts.Title = table
	}

	var okDur []int64
	for _, r := range recent { // recent отсортированы по started_at DESC
		if r.TableName != table {
			continue
		}
		if ts.LastRun == nil {
			v := r.view()
			ts.LastRun = &v
		}
		if r.Status == "ok" {
			if ts.LastOK == nil {
				v := r.view()
				ts.LastOK = &v
			}
			okDur = append(okDur, r.FinishedAt-r.StartedAt)
		}
	}
	ts.TypicalDurationSec = median(okDur)
	if ts.LastOK != nil {
		ts.LoadedFor = ts.LastOK.SourceDate
		if lag, ok := daysBetween(ts.LastOK.SourceDate, src.Today); ok {
			ts.LagDays = &lag
		}
	}

	if src.Checked {
		mctx, cancel := context.WithTimeout(ctx, 10*time.Second)
		ready, err := sourceReady(mctx, s.deps.MSSQL, s.opts, table, src.Hour)
		cancel()
		if err != nil {
			ts.SourceReadyError = err.Error()
		} else {
			ts.SourceReady = &ready
		}
	} else {
		ts.SourceReadyError = src.Error
	}

	if ch != nil {
		ts.Data = finDWHDataStats(ctx, ch, table)
	} else {
		ts.Data.Error = "ClickHouse недоступен"
	}

	ts.State, ts.Severity, ts.StateText = classifyFinDWH(finDWHClassifyIn{
		CHDown:        ch == nil,
		Today:         src.Today,
		Hour:          src.Hour,
		SourceChecked: src.Checked,
		SourceReady:   ts.SourceReady,
		LastRun:       ts.LastRun,
		LastOK:        ts.LastOK,
		WorkerOn:      ws.Enabled,
		Running:       ws.Running == table,
		RunningSince:  ws.RunningSince,
		Progress:      ws.Progress,
		TypicalSec:    ts.TypicalDurationSec,
		Now:           now,
	})

	// GLMF: строки реплики против отпечатков. Пока таблица грузится, секции и
	// отпечатки пишутся не одновременно — расхождение в этот момент нормально.
	if table == FinDWHGLMF && ws.Running != table && ts.Data.Rows != nil && ts.Data.FPRows != nil &&
		*ts.Data.Rows != *ts.Data.FPRows {
		ts.Severity = worseSeverity(ts.Severity, "warn")
		ts.StateText += fmt.Sprintf(". Строк в реплике %d, а по отпечаткам %d — реплику правили мимо ETL или "+
			"не записались отпечатки; лечится полной перегрузкой (FINDWH_GLMF_FULL=1)", *ts.Data.Rows, *ts.Data.FPRows)
	}
	return ts
}

// finDWHClassifyIn — всё, из чего складывается статус таблицы.
type finDWHClassifyIn struct {
	CHDown        bool   // журнал прогонов не прочитан
	Today         string // YYYY-MM-DD по MSSQL
	Hour          int
	SourceChecked bool
	SourceReady   *bool
	LastRun       *FinDWHRunView
	LastOK        *FinDWHRunView
	WorkerOn      bool
	Running       bool
	RunningSince  *time.Time
	Progress      string
	TypicalSec    int64
	Now           time.Time
}

// classifyFinDWH — состояние таблицы, его важность и человеческое пояснение.
func classifyFinDWH(in finDWHClassifyIn) (state, severity, text string) {
	if in.Running {
		text = "Загружается"
		sev := "info"
		if in.RunningSince != nil {
			el := in.Now.Sub(*in.RunningSince)
			text += " " + humanDuration(int64(el.Seconds()))
			if in.TypicalSec > 0 {
				text += ", обычно " + humanDuration(in.TypicalSec)
				if el > 2*time.Duration(in.TypicalSec)*time.Second+10*time.Minute {
					sev = "warn"
					text += " — заметно дольше обычного"
				}
			}
		}
		if in.Progress != "" {
			text += ". " + in.Progress
		}
		return "running", sev, text
	}
	if in.CHDown {
		return "unknown", "error", "Журнал прогонов не прочитан: ClickHouse недоступен"
	}
	if in.LastRun == nil {
		text = "Ещё ни разу не загружалась"
		if !in.WorkerOn {
			text += "; воркер выключен — первая загрузка вручную, см. docs/bi/findwh-etl.md §9"
		}
		return "never", "warn", text
	}

	if in.LastOK != nil && in.LastOK.SourceDate == in.Today {
		if in.LastRun.Status == "error" {
			return "ok", "warn", "Данные за сегодня загружены, но более поздний прогон упал: " + firstLine(in.LastRun.Details)
		}
		return "ok", "ok", "Загружено за сегодня"
	}

	lag := -1
	if in.LastOK != nil {
		lag, _ = daysBetween(in.LastOK.SourceDate, in.Today)
	}
	stale := ""
	if in.LastOK == nil {
		stale = "успешных загрузок ещё не было"
	} else if lag >= 2 {
		stale = fmt.Sprintf("последняя успешная загрузка — за %s (%d дн. назад)", ruDate(in.LastOK.SourceDate), lag)
	}

	if in.LastRun.Status == "error" {
		text = "Последний прогон упал: " + firstLine(in.LastRun.Details)
		if in.WorkerOn {
			text += ". Повтор — на ближайшем тике воркера"
		}
		if stale != "" {
			text += "; " + stale
		}
		return "error", "error", text
	}
	if stale != "" {
		return "stale", "error", "Данные устарели: " + stale
	}
	if !in.WorkerOn {
		return "off", "warn", "Сегодня не загружалась: воркер выключен"
	}
	if !in.SourceChecked || in.SourceReady == nil {
		return "unknown", "warn", "Сегодня ещё не загружалась; готовность источника проверить не удалось"
	}
	if *in.SourceReady {
		return "queued", "info", "DWH собрал таблицу — загрузка на ближайшем тике воркера"
	}
	if in.Hour >= finDWHLateHour {
		return "late", "warn", "DWH до сих пор не собрал таблицу (обычно к ≈08:30) — проверьте джоб SQL Agent"
	}
	return "waiting", "info", "Ждём ночную сборку DWH; вчерашние данные на месте"
}

// finDWHDataStats — объём и диапазон дат реплики.
func finDWHDataStats(ctx context.Context, ch *chClient, table string) FinDWHDataStats {
	type row struct {
		Rows      int64  `json:"rows"`
		DateFrom  string `json:"date_from"`
		DateTo    string `json:"date_to"`
		Loaded    int64  `json:"loaded"`
		FPRows    int64  `json:"fp_rows"`
		Days      int64  `json:"days"`
		Companies int64  `json:"companies"`
	}
	var st FinDWHDataStats
	one := func(q string) (row, bool) {
		rs, err := chQueryJSON[row](ctx, ch, q)
		if err != nil {
			st.Error = err.Error()
			return row{}, false
		}
		if len(rs) == 0 {
			return row{}, true
		}
		return rs[0], true
	}
	setLoaded := func(sec int64) {
		if sec > 0 {
			t := time.Unix(sec, 0)
			st.LoadedAt = &t
		}
	}

	switch table {
	case FinDWHGLMF:
		// count() по MergeTree — из метаданных; диапазон — по отпечаткам (тысячи
		// строк), а не сканом 100+ млн строк реплики.
		if r, ok := one("SELECT count() AS rows FROM finance.findwh_glmf"); ok {
			st.Rows = &r.Rows
		}
		if r, ok := one(`SELECT sum(n) AS fp_rows, count() AS days, uniqExact(company) AS companies,
  toString(min(date)) AS date_from, toString(max(date)) AS date_to,
  toUnixTimestamp(max(checked_at)) AS loaded
FROM finance.findwh_glmf_day_fp FINAL WHERE is_deleted = 0`); ok {
			st.FPRows = &r.FPRows
			st.Days, st.Companies = r.Days, r.Companies
			if r.Days > 0 {
				st.From, st.To = r.DateFrom, r.DateTo
				setLoaded(r.Loaded)
			}
		}
	case FinDWHTableFinPL, FinDWHExRates:
		col := "month"
		if table == FinDWHExRates {
			col = "data"
		}
		if r, ok := one(fmt.Sprintf(`SELECT count() AS rows, toString(min(%[1]s)) AS date_from,
  toString(max(%[1]s)) AS date_to, toUnixTimestamp(max(loaded_at)) AS loaded
FROM %[2]s`, col, finDWHMeta[table].Target)); ok {
			st.Rows = &r.Rows
			if r.Rows > 0 {
				st.From, st.To = r.DateFrom, r.DateTo
				setLoaded(r.Loaded)
			}
		}
	}
	return st
}

// chQueryJSON — SELECT в JSONEachRow, построчно в T. 64-битные числа без кавычек,
// иначе count()/rows пришли бы строками.
func chQueryJSON[T any](ctx context.Context, ch *chClient, q string) ([]T, error) {
	body, err := ch.queryString(ctx, q+"\nSETTINGS output_format_json_quote_64bit_integers = 0\nFORMAT JSONEachRow")
	if err != nil {
		return nil, err
	}
	var out []T
	for _, line := range strings.Split(body, "\n") {
		if line = strings.TrimSpace(line); line == "" {
			continue
		}
		var v T
		if err := json.Unmarshal([]byte(line), &v); err != nil {
			return nil, fmt.Errorf("ch json: %w: %q", err, line)
		}
		out = append(out, v)
	}
	return out, nil
}

func (r finDWHRun) view() FinDWHRunView {
	return FinDWHRunView{
		Table:       r.TableName,
		SourceDate:  r.SourceDate,
		StartedAt:   time.Unix(r.StartedAt, 0),
		FinishedAt:  time.Unix(r.FinishedAt, 0),
		DurationSec: r.FinishedAt - r.StartedAt,
		Status:      r.Status,
		Rows:        r.Rows,
		Details:     r.Details,
		TriggeredBy: r.TriggeredBy,
	}
}

var severityRank = map[string]int{"ok": 0, "info": 1, "warn": 2, "error": 3}

func worseSeverity(a, b string) string {
	if severityRank[b] > severityRank[a] {
		return b
	}
	return a
}

func median(xs []int64) int64 {
	if len(xs) == 0 {
		return 0
	}
	s := append([]int64(nil), xs...)
	sort.Slice(s, func(i, j int) bool { return s[i] < s[j] })
	return s[len(s)/2]
}

// daysBetween — сколько дней от from до to (обе YYYY-MM-DD).
func daysBetween(from, to string) (int, bool) {
	a, err1 := time.Parse("2006-01-02", from)
	b, err2 := time.Parse("2006-01-02", to)
	if err1 != nil || err2 != nil {
		return 0, false
	}
	return int(b.Sub(a).Hours() / 24), true
}

func ruDate(iso string) string {
	if t, err := time.Parse("2006-01-02", iso); err == nil {
		return t.Format("02.01.2006")
	}
	return iso
}

func humanDuration(sec int64) string {
	switch {
	case sec < 60:
		return fmt.Sprintf("%d с", sec)
	case sec < 3600:
		return fmt.Sprintf("%d мин", sec/60)
	default:
		return fmt.Sprintf("%d ч %02d мин", sec/3600, sec%3600/60)
	}
}

func firstLine(s string) string {
	if i := strings.IndexByte(s, '\n'); i >= 0 {
		s = s[:i]
	}
	if r := []rune(s); len(r) > 300 {
		s = string(r[:300]) + "…"
	}
	return s
}
