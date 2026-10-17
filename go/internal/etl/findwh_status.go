package etl

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"net/http"
	"net/url"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"sync"
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

// Бюджеты ручки. Общий заметно меньше WriteTimeout сервера (30 с): иначе при
// медленном источнике ответ дописывался бы после дедлайна записи и браузер
// получил бы обрыв вместо частичного отчёта. Ветки MSSQL и CH идут параллельно.
const (
	finDWHStatusBudget = 20 * time.Second
	finDWHMSSQLBudget  = 10 * time.Second // часы + все проверки готовности
	finDWHCHQueryLimit = 5 * time.Second  // на каждый запрос к CH
)

// finDWHSourceTZ — пояс сервера MSSQL (Минск, UTC+3, без перехода на летнее
// время). Нужен, только когда MSSQL не ответил и «сегодня» считаем сами: контейнер
// go-api живёт в UTC, и с 00:00 до 03:00 вчерашняя загрузка сошла бы за сегодняшнюю.
var finDWHSourceTZ = time.FixedZone("UTC+3", 3*3600)

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

// FinDWHStatus — сборщик статуса. deps.MSSQL — отдельный маленький пул (не пул
// воркера), может быть nil (креды не заданы, mock) — тогда готовность источника
// не проверяется. worker nil — воркер не запущен, причина в workerOff.
type FinDWHStatus struct {
	deps        Deps
	opts        FinDWHOpts
	intervalSec int
	worker      *FinDWHWorker
	workerOff   string
	settingErr  map[string]string // env → ошибка разбора (значение подменено дефолтом)
}

// NewFinDWHStatus — конструктор.
func NewFinDWHStatus(deps Deps, opts FinDWHOpts, intervalSec int, worker *FinDWHWorker, workerOff string) *FinDWHStatus {
	opts.normalize()
	return &FinDWHStatus{deps: deps, opts: opts, intervalSec: intervalSec, worker: worker, workerOff: workerOff,
		settingErr: map[string]string{}}
}

// NoteSettingError — значение env не разобрано и подменено дефолтом; страница
// покажет это рядом с действующим значением, а не выдаст дефолт за настройку.
// Звать до регистрации ручки.
func (s *FinDWHStatus) NoteSettingError(env, msg string) {
	s.settingErr[env] = msg
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
	Error string `json:"error,omitempty"`
}

// FinDWHSourceState — часы сервера MSSQL: по ним живёт расписание DWH.
type FinDWHSourceState struct {
	Checked bool   `json:"checked"`         // удалось спросить MSSQL
	Today   string `json:"today"`           // YYYY-MM-DD по MSSQL (без него — по UTC+3)
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
	Rows *int64 `json:"rows,omitempty"`
	From string `json:"from,omitempty"` // диапазон дат данных
	To   string `json:"to,omitempty"`
	// LoadedAt — когда данные последний раз менялись: снимки — время заливки,
	// GLMF — последняя запись отпечатков (пишутся только для перегруженных дней,
	// так что это не время последней сверки — его даёт last_ok.finished_at).
	LoadedAt *time.Time `json:"loaded_at,omitempty"`
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
	State              string          `json:"state"`    // ok | running | waiting | queued | late | error | stale | off | source_unknown | ch_down | never
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
	ctx, cancel := context.WithTimeout(r.Context(), finDWHStatusBudget)
	defer cancel()
	rep := s.Report(ctx)
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	if err := json.NewEncoder(w).Encode(rep); err != nil {
		log.Printf("findwh-status: encode: %v", err)
	}
}

// finDWHMSSQLView — что узнали у MSSQL.
type finDWHMSSQLView struct {
	src      FinDWHSourceState
	ready    map[string]*bool
	readyErr map[string]string
}

// finDWHCHView — что прочитали из CH.
type finDWHCHView struct {
	err     string
	recent  []finDWHRun
	journal []finDWHRun
	data    map[string]FinDWHDataStats
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

	var (
		wg sync.WaitGroup
		ms finDWHMSSQLView
		cv finDWHCHView
	)
	wg.Add(2)
	go func() { defer wg.Done(); ms = s.readMSSQL(ctx, now) }()
	go func() { defer wg.Done(); cv = s.readCH(ctx) }()
	wg.Wait()

	rep.Source, rep.CHError = ms.src, cv.err
	for _, r := range cv.journal {
		rep.Runs = append(rep.Runs, r.view())
	}
	for _, table := range s.opts.Tables {
		rep.Tables = append(rep.Tables, s.tableStatus(table, ms, cv, rep.Worker, now))
	}

	rep.Overall = "ok"
	if rep.CHError != "" {
		rep.Overall = "error"
	}
	// Выключенный воркер и упавший тик (например, не ответили часы MSSQL — до
	// журнала дело не дошло, и таблицы выглядят как «ждём») — повод посмотреть.
	if !rep.Worker.Enabled || rep.Worker.LastTickError != "" {
		rep.Overall = worseSeverity(rep.Overall, "warn")
	}
	for _, t := range rep.Tables {
		rep.Overall = worseSeverity(rep.Overall, t.Severity)
	}
	return rep
}

// readMSSQL — часы источника и готовность всех таблиц, общий бюджет
// finDWHMSSQLBudget. Проверки готовности запускаются параллельно, но пул статуса
// — на одно соединение, так что сервер они не нагружают.
func (s *FinDWHStatus) readMSSQL(ctx context.Context, now time.Time) finDWHMSSQLView {
	v := finDWHMSSQLView{
		src:      FinDWHSourceState{Today: now.In(finDWHSourceTZ).Format("2006-01-02")},
		ready:    map[string]*bool{},
		readyErr: map[string]string{},
	}
	if s.deps.MSSQL == nil {
		v.src.Error = "MSSQL не подключён: " + s.workerOff
		return v
	}
	ctx, cancel := context.WithTimeout(ctx, finDWHMSSQLBudget)
	defer cancel()
	date, hour, err := sourceClock(ctx, s.deps.MSSQL)
	if err != nil {
		v.src.Error = cleanErr(err)
		return v
	}
	v.src.Checked, v.src.Today, v.src.Hour = true, date, hour

	var (
		mu sync.Mutex
		wg sync.WaitGroup
	)
	for _, table := range s.opts.Tables {
		wg.Add(1)
		go func(table string) {
			defer wg.Done()
			ready, err := sourceReady(ctx, s.deps.MSSQL, s.opts, table, hour)
			mu.Lock()
			defer mu.Unlock()
			if err != nil {
				v.readyErr[table] = cleanErr(err)
				return
			}
			v.ready[table] = &ready
		}(table)
	}
	wg.Wait()
	return v
}

// readCH — журнал прогонов и объём реплики, каждый запрос не дольше finDWHCHQueryLimit.
func (s *FinDWHStatus) readCH(ctx context.Context) finDWHCHView {
	v := finDWHCHView{data: map[string]FinDWHDataStats{}}
	ch, err := newCHClient(s.deps.CHURL, s.deps.CHUser, s.deps.CHPass)
	if err == nil {
		// Последние 7 успешных и 7 упавших на таблицу: из них последний прогон,
		// последний успешный и типичная длительность. Журнал — отдельно, по времени.
		v.recent, err = chQueryJSON[finDWHRun](ctx, ch, finDWHRunsSQL+" LIMIT 7 BY table_name, status")
	}
	if err == nil {
		v.journal, err = chQueryJSON[finDWHRun](ctx, ch, finDWHRunsSQL+" LIMIT 40")
	}
	if err != nil {
		v.err = cleanErr(err)
		return v
	}
	for _, table := range s.opts.Tables {
		v.data[table] = finDWHDataStats(ctx, ch, table)
	}
	return v
}

const finDWHRunsSQL = `SELECT table_name, toString(source_date) AS source_date,
  toUnixTimestamp(started_at) AS started_at, toUnixTimestamp(finished_at) AS finished_at,
  status, rows, details, triggered_by
FROM finance.findwh_etl_runs ORDER BY started_at DESC`

func (s *FinDWHStatus) settings() []FinDWHSetting {
	interval := strconv.Itoa(s.intervalSec)
	if s.intervalSec > 0 {
		interval += " (" + (time.Duration(s.intervalSec) * time.Second).String() + ")"
	}
	readyHint := "грузить после успешного шага SQL Agent за сегодня (нужен SELECT на msdb)"
	if !s.opts.ReadyCheck {
		readyHint = "без msdb: грузить по часам MSSQL после FINDWH_READY_AFTER_HOUR"
	}
	out := []FinDWHSetting{
		{Env: "FINDWH_SYNC_INTERVAL", Value: interval, Hint: "как часто воркер проверяет, не пора ли грузить, сек; 0 — воркер выключен"},
		{Env: "FINDWH_DB", Value: s.opts.DB, Hint: "БД источника на сервере MSSQL_PREMASTER_SERVER"},
		{Env: "FINDWH_GLMF_FROM", Value: s.opts.GLMFFrom.Format("2006-01-02"), Hint: "нижняя граница GLMF.Date в реплике"},
		{Env: "FINDWH_GLMF_CHUNK_MONTHS", Value: strconv.Itoa(s.opts.ChunkMonths), Hint: "месяцев одной компании за один запрос к MSSQL"},
		{Env: "FINDWH_BATCH", Value: strconv.Itoa(s.opts.BatchSize), Hint: "строк в одном INSERT в ClickHouse"},
		{Env: "FINDWH_READY_CHECK", Value: boolEnv(s.opts.ReadyCheck), Hint: readyHint},
		{Env: "FINDWH_READY_AFTER_HOUR", Value: strconv.Itoa(s.opts.ReadyAfterHour), Hint: "час по MSSQL, раньше которого не грузим (только при FINDWH_READY_CHECK=0)"},
	}
	for i := range out {
		out[i].Error = s.settingErr[out[i].Env]
	}
	return out
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

func (s *FinDWHStatus) tableStatus(table string, ms finDWHMSSQLView, cv finDWHCHView,
	ws FinDWHWorkerState, now time.Time) FinDWHTableStatus {
	meta := finDWHMeta[table]
	ts := FinDWHTableStatus{
		Table: table, Title: meta.Title, Source: meta.Source, Target: meta.Target,
		Method: meta.Method, ReadyRule: s.readyRule(table),
	}
	if ts.Title == "" {
		ts.Title = table
	}

	var okDur []int64
	for _, r := range cv.recent { // recent отсортированы по started_at DESC
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
		if lag, ok := daysBetween(ts.LastOK.SourceDate, ms.src.Today); ok {
			ts.LagDays = &lag
		}
	}

	ts.SourceReady = ms.ready[table]
	ts.SourceReadyError = ms.readyErr[table]
	if !ms.src.Checked {
		ts.SourceReadyError = ms.src.Error
	}

	if cv.err != "" {
		ts.Data.Error = "ClickHouse недоступен"
	} else {
		ts.Data = cv.data[table]
	}

	ts.State, ts.Severity, ts.StateText = classifyFinDWH(finDWHClassifyIn{
		CHDown:        cv.err != "",
		Today:         ms.src.Today,
		Hour:          ms.src.Hour,
		SourceChecked: ms.src.Checked,
		SourceReady:   ts.SourceReady,
		ReadyCheck:    s.opts.ReadyCheck,
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
			"не записались отпечатки; лечится полной перегрузкой (FINDWH_GLMF_FULL=1). Если GLMF прямо сейчас "+
			"грузится из CLI — это нормально до конца прогона", *ts.Data.Rows, *ts.Data.FPRows)
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
	ReadyCheck    bool // готовность — по SQL Agent (иначе по часам)
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
		return "ch_down", "error", "Журнал прогонов не прочитан: ClickHouse недоступен"
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
		return "source_unknown", "warn", "Сегодня ещё не загружалась; готовность источника проверить не удалось"
	}
	if *in.SourceReady {
		return "queued", "info", "Источник готов — загрузка на ближайшем тике воркера"
	}
	// Без ReadyCheck «не готов» значит лишь «ещё не FINDWH_READY_AFTER_HOUR»:
	// про сборку DWH мы ничего не знаем, и советовать смотреть джоб нечестно.
	if !in.ReadyCheck {
		return "waiting", "info", "Ждём часа загрузки (FINDWH_READY_AFTER_HOUR); вчерашние данные на месте"
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
		LoadedUTC string `json:"loaded_utc"`
		FPRows    int64  `json:"fp_rows"`
		Days      int64  `json:"days"`
		Companies int64  `json:"companies"`
	}
	var st FinDWHDataStats
	one := func(q string) (row, bool) {
		rs, err := chQueryJSON[row](ctx, ch, q)
		if err != nil {
			st.Error = cleanErr(err)
			return row{}, false
		}
		if len(rs) == 0 {
			return row{}, true
		}
		return rs[0], true
	}

	switch table {
	case FinDWHGLMF:
		// count() по MergeTree — из метаданных; диапазон — по отпечаткам (тысячи
		// строк), а не сканом 100+ млн строк реплики.
		if r, ok := one("SELECT count() AS rows FROM finance.findwh_glmf"); ok {
			st.Rows = &r.Rows
		}
		// checked_at — DateTime64 без пояса, ETL пишет туда время UTC строкой;
		// toUnixTimestamp прочитал бы его в поясе сервера CH, поэтому берём
		// настенное время как есть и разбираем в Go как UTC.
		if r, ok := one(`SELECT sum(n) AS fp_rows, count() AS days, uniqExact(company) AS companies,
  toString(min(date)) AS date_from, toString(max(date)) AS date_to,
  formatDateTime(max(checked_at), '%Y-%m-%d %H:%i:%S') AS loaded_utc
FROM finance.findwh_glmf_day_fp FINAL WHERE is_deleted = 0`); ok {
			st.FPRows = &r.FPRows
			st.Days, st.Companies = r.Days, r.Companies
			if r.Days > 0 {
				st.From, st.To = r.DateFrom, r.DateTo
				if t, err := time.Parse("2006-01-02 15:04:05", r.LoadedUTC); err == nil {
					st.LoadedAt = &t
				}
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
				if r.Loaded > 0 {
					t := time.Unix(r.Loaded, 0)
					st.LoadedAt = &t
				}
			}
		}
	}
	return st
}

// chQueryJSON — SELECT в JSONEachRow, построчно в T, не дольше finDWHCHQueryLimit.
// 64-битные числа без кавычек, иначе count()/rows пришли бы строками.
func chQueryJSON[T any](ctx context.Context, ch *chClient, q string) ([]T, error) {
	ctx, cancel := context.WithTimeout(ctx, finDWHCHQueryLimit)
	defer cancel()
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

// cleanErr — текст ошибки для страницы без URL запроса: в *url.Error он
// целиком, вместе с логином CH (пароль http.Client маскирует, логин — нет), а
// ошибка разбора CLICKHOUSE_HTTP_URL показала бы адрес дословно. Обёртки
// («findwh glmf: …») сохраняются, вырезается только URL.
func cleanErr(err error) string {
	msg := err.Error()
	var ue *url.Error
	if errors.As(err, &ue) {
		msg = strings.Replace(msg, ue.Error(), ue.Op+": "+ue.Err.Error(), 1)
	}
	return msg
}

// chUserInURL — логин в URL ошибки HTTP-клиента («http://finance:***@host»):
// строки журнала, записанные до cleanErr в RunFinDWHDaily, несут его в details.
var chUserInURL = regexp.MustCompile(`://[^/@\s:]+(:[^/@\s]*)?@`)

func (r finDWHRun) view() FinDWHRunView {
	r.Details = chUserInURL.ReplaceAllString(r.Details, "://")
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
