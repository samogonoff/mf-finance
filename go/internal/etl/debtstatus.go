// Package etl — ETL-инфраструктура: bootstrap Premaster1C → ClickHouse,
// инкрементальный pull по DateOfChange, admin-эндпоинты для управления и мониторинга.
//
// Admin endpoints (все за ROLE_ADMIN):
//   GET  /api/admin/etl/debt/status     — список ЮЛ + checkpoint + count в CH
//   GET  /api/admin/etl/debt/settings   — текущие настройки инкремента
//   PUT  /api/admin/etl/debt/settings   — изменить (enabled, interval)
//   POST /api/admin/etl/debt/bootstrap  — запустить bootstrap для одного ЮЛ
//   GET  /api/admin/etl/debt/log        — последние N запусков из CH etl_run_log
package etl

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"
)

// --- Models ---------------------------------------------------------------

type CompanyStatus struct {
	CompanyID  string  `json:"company_id"`
	Name       string  `json:"name,omitempty"`
	Country    string  `json:"country,omitempty"`
	Bootstrap  *Status `json:"bootstrap,omitempty"`
	Increment  *Status `json:"incremental,omitempty"`
	CHRows     *int64  `json:"ch_rows,omitempty"`
	Running    bool    `json:"running"` // bootstrap идёт прямо сейчас
}

type Status struct {
	RowsLoaded   int64      `json:"rows_loaded"`
	StartedAt    time.Time  `json:"started_at"`
	UpdatedAt    time.Time  `json:"updated_at"`
	FinishedAt   *time.Time `json:"finished_at,omitempty"`
	DurationSec  *float64   `json:"duration_seconds,omitempty"`
	LastChangeAt *time.Time `json:"last_change_at,omitempty"`
	ErrorText    *string    `json:"error_text,omitempty"`
}

type Settings struct {
	Enabled         bool      `json:"incremental_enabled"`
	IntervalMinutes int       `json:"incremental_interval_minutes"`
	LastTickAt      string    `json:"incremental_last_tick_at,omitempty"`
	LastError       string    `json:"incremental_last_error,omitempty"`
	UpdatedAt       time.Time `json:"updated_at"`
}

type LogEntry struct {
	CompanyID    string    `json:"company_id"`
	Phase        string    `json:"phase"`
	TriggeredBy  string    `json:"triggered_by"`
	StartedAt    string    `json:"started_at"`
	FinishedAt   string    `json:"finished_at"`
	DurationSec  float64   `json:"duration_sec"`
	RowsLoaded   int64     `json:"rows_loaded"`
	LastChangeAt string    `json:"last_change_at,omitempty"`
	ErrorText    string    `json:"error_text,omitempty"`
}

// --- Handler --------------------------------------------------------------

type AdminHandler struct {
	deps    Deps
	ch      *chClient // переиспользуем один экземпляр на handler — CH-операции тонкие
	running sync.Map  // company_id → struct{} (защита от двойного bootstrap'а одной ЮЛ)
}

func NewAdminHandler(deps Deps) *AdminHandler {
	ch, _ := newCHClient(deps.CHURL, deps.CHUser, deps.CHPass)
	return &AdminHandler{deps: deps, ch: ch}
}

// Status — GET /api/admin/etl/debt/status
func (h *AdminHandler) Status(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	// ?source=premaster (default) | glmf — прогресс соответствующего потока.
	source := r.URL.Query().Get("source")
	if source == "" {
		source = "premaster"
	}
	if source != "premaster" && source != "glmf" {
		writeErr(w, http.StatusBadRequest, "source must be premaster|glmf")
		return
	}

	rows, err := h.deps.PG.Query(ctx, `
		SELECT company_id, phase, rows_loaded, started_at, updated_at,
		       finished_at, error_text, last_change_at
		  FROM debt_etl_checkpoint
		 WHERE source=$1
		 ORDER BY company_id, phase`, source)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "pg query: "+err.Error())
		return
	}
	defer rows.Close()

	chCounts, chErr := h.fetchCHCounts(ctx, source)

	byCompany := map[string]*CompanyStatus{}
	for rows.Next() {
		var inn, phase string
		var s Status
		var finished *time.Time
		var errText *string
		var lastChange *time.Time
		if err := rows.Scan(&inn, &phase, &s.RowsLoaded,
			&s.StartedAt, &s.UpdatedAt, &finished, &errText, &lastChange); err != nil {
			writeErr(w, http.StatusInternalServerError, "scan: "+err.Error())
			return
		}
		if finished != nil {
			s.FinishedAt = finished
			d := finished.Sub(s.StartedAt).Seconds()
			s.DurationSec = &d
		}
		s.ErrorText = errText
		s.LastChangeAt = lastChange

		c, ok := byCompany[inn]
		if !ok {
			c = &CompanyStatus{CompanyID: inn, Name: companyNames[inn], Country: CountryByINN[inn]}
			byCompany[inn] = c
		}
		switch phase {
		case "bootstrap":
			c.Bootstrap = &s
		case "incremental":
			c.Increment = &s
		}
	}
	if rows.Err() != nil {
		writeErr(w, http.StatusInternalServerError, "rows: "+rows.Err().Error())
		return
	}

	// добавим ЮЛ из справочника, по которым ещё ничего не делали — для удобства UI.
	for inn, name := range companyNames {
		if _, ok := byCompany[inn]; !ok {
			byCompany[inn] = &CompanyStatus{CompanyID: inn, Name: name, Country: CountryByINN[inn]}
		}
	}

	// в-сорт по ИНН, проставим CHRows + running.
	out := make([]CompanyStatus, 0, len(byCompany))
	for _, c := range byCompany {
		if chCounts != nil {
			if v, ok := chCounts[c.CompanyID]; ok {
				cnt := v
				c.CHRows = &cnt
			}
		}
		if _, ok := h.running.Load(source + ":" + c.CompanyID); ok {
			c.Running = true
		}
		out = append(out, *c)
	}

	resp := map[string]any{"items": out}
	if chErr != nil {
		resp["ch_error"] = chErr.Error()
	}
	writeJSON(w, http.StatusOK, resp)
}

// Settings — GET /api/admin/etl/debt/settings
func (h *AdminHandler) Settings(w http.ResponseWriter, r *http.Request) {
	s, err := h.readSettings(r.Context())
	if err != nil {
		writeErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, s)
}

// UpdateSettings — PUT /api/admin/etl/debt/settings
// Body: {"incremental_enabled": true, "incremental_interval_minutes": 10}
func (h *AdminHandler) UpdateSettings(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Enabled         *bool `json:"incremental_enabled"`
		IntervalMinutes *int  `json:"incremental_interval_minutes"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeErr(w, http.StatusBadRequest, "invalid body: "+err.Error())
		return
	}
	ctx := r.Context()
	if body.Enabled != nil {
		v := "0"
		if *body.Enabled {
			v = "1"
		}
		if _, err := h.deps.PG.Exec(ctx, `
			INSERT INTO debt_etl_settings (key, value, updated_at)
			VALUES ('incremental_enabled', $1, NOW())
			ON CONFLICT (key) DO UPDATE SET value=$1, updated_at=NOW()`, v); err != nil {
			writeErr(w, http.StatusInternalServerError, "set enabled: "+err.Error())
			return
		}
	}
	if body.IntervalMinutes != nil {
		if *body.IntervalMinutes < 1 || *body.IntervalMinutes > 1440 {
			writeErr(w, http.StatusBadRequest, "incremental_interval_minutes must be 1..1440")
			return
		}
		if _, err := h.deps.PG.Exec(ctx, `
			INSERT INTO debt_etl_settings (key, value, updated_at)
			VALUES ('incremental_interval_minutes', $1, NOW())
			ON CONFLICT (key) DO UPDATE SET value=$1, updated_at=NOW()`,
			strconv.Itoa(*body.IntervalMinutes)); err != nil {
			writeErr(w, http.StatusInternalServerError, "set interval: "+err.Error())
			return
		}
	}
	s, _ := h.readSettings(ctx)
	writeJSON(w, http.StatusOK, s)
}

// StartBootstrap — POST /api/admin/etl/debt/bootstrap
// Body: {"company_id": "6950135110"}
// Возвращает 202 Accepted, если запуск принят. Если для этого ЮЛ уже идёт — 409.
func (h *AdminHandler) StartBootstrap(w http.ResponseWriter, r *http.Request) {
	var body struct {
		CompanyID string `json:"company_id"`
		BatchSize int    `json:"batch_size,omitempty"`
		// Source: premaster (default) | glmf | contract. См. bootstrapRunnerFor.
		Source string `json:"source,omitempty"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeErr(w, http.StatusBadRequest, "invalid body: "+err.Error())
		return
	}
	if body.CompanyID == "" {
		writeErr(w, http.StatusBadRequest, "company_id required")
		return
	}
	runner, ok := bootstrapRunnerFor(body.Source)
	if !ok {
		writeErr(w, http.StatusBadRequest, "unknown source (premaster|glmf|contract): "+body.Source)
		return
	}
	source := body.Source
	if source == "" {
		source = "premaster"
	}
	// Ключ блокировки включает source — bootstrap разных источников одного ЮЛ не мешают.
	runKey := source + ":" + body.CompanyID
	if _, busy := h.running.LoadOrStore(runKey, struct{}{}); busy {
		writeErr(w, http.StatusConflict, "bootstrap already in progress for "+runKey)
		return
	}

	go func(inn string, batch int) {
		defer h.running.Delete(runKey)
		bgCtx, cancel := context.WithTimeout(context.Background(), 6*time.Hour)
		defer cancel()
		if _, err := runner(bgCtx, h.deps, BootstrapOpts{
			CompanyID:   inn,
			BatchSize:   batch,
			TriggeredBy: "admin-ui",
		}); err != nil {
			fmt.Printf("admin-ui bootstrap %s %s: %v\n", body.Source, inn, err)
		}
	}(body.CompanyID, body.BatchSize)

	writeJSON(w, http.StatusAccepted, map[string]string{
		"company_id": body.CompanyID,
		"source":     body.Source,
		"status":     "accepted",
	})
}

// Log — GET /api/admin/etl/debt/log?limit=50
func (h *AdminHandler) Log(w http.ResponseWriter, r *http.Request) {
	limit := 50
	if v := r.URL.Query().Get("limit"); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n > 0 && n <= 500 {
			limit = n
		}
	}
	if h.ch == nil {
		writeErr(w, http.StatusServiceUnavailable, "clickhouse not configured")
		return
	}
	q := fmt.Sprintf(`
		SELECT
		    company_id, phase, triggered_by,
		    formatDateTime(started_at, '%%Y-%%m-%%dT%%H:%%i:%%SZ') AS started_at,
		    formatDateTime(finished_at, '%%Y-%%m-%%dT%%H:%%i:%%SZ') AS finished_at,
		    toFloat64(duration_sec) AS duration_sec,
		    toInt64(rows_loaded) AS rows_loaded,
		    if(isNull(last_change_at), '', formatDateTime(last_change_at, '%%Y-%%m-%%dT%%H:%%i:%%SZ')) AS last_change_at,
		    error_text
		FROM finance.etl_run_log
		ORDER BY started_at DESC
		LIMIT %d
		FORMAT JSONEachRow`, limit)
	body, err := h.ch.queryString(r.Context(), q)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	out := []LogEntry{}
	dec := json.NewDecoder(strings.NewReader(body))
	for dec.More() {
		var e LogEntry
		if err := dec.Decode(&e); err != nil {
			writeErr(w, http.StatusInternalServerError, "decode: "+err.Error())
			return
		}
		out = append(out, e)
	}
	writeJSON(w, http.StatusOK, map[string]any{"items": out})
}

// --- Internals ------------------------------------------------------------

func (h *AdminHandler) readSettings(ctx context.Context) (*Settings, error) {
	rows, err := h.deps.PG.Query(ctx, `SELECT key, value, updated_at FROM debt_etl_settings`)
	if err != nil {
		return nil, fmt.Errorf("pg: %w", err)
	}
	defer rows.Close()
	s := &Settings{IntervalMinutes: 10}
	for rows.Next() {
		var k, v string
		var upd time.Time
		if err := rows.Scan(&k, &v, &upd); err != nil {
			return nil, err
		}
		if upd.After(s.UpdatedAt) {
			s.UpdatedAt = upd
		}
		switch k {
		case "incremental_enabled":
			s.Enabled = v == "1"
		case "incremental_interval_minutes":
			if n, err := strconv.Atoi(v); err == nil {
				s.IntervalMinutes = n
			}
		case "incremental_last_tick_at":
			s.LastTickAt = v
		case "incremental_last_error":
			s.LastError = v
		}
	}
	return s, rows.Err()
}

func (h *AdminHandler) fetchCHCounts(ctx context.Context, source string) (map[string]int64, error) {
	if h.ch == nil {
		return nil, fmt.Errorf("clickhouse url not configured")
	}
	table := "finance.fact_premaster"
	if source == "glmf" {
		table = "finance.fact_glmf"
	}
	q := "SELECT company_id, toString(count()) AS cnt FROM " + table + " GROUP BY company_id FORMAT JSONEachRow"
	body, err := h.ch.queryString(ctx, q)
	if err != nil {
		return nil, err
	}
	out := map[string]int64{}
	dec := json.NewDecoder(strings.NewReader(body))
	for dec.More() {
		var r struct {
			CompanyID string `json:"company_id"`
			Cnt       string `json:"cnt"`
		}
		if err := dec.Decode(&r); err != nil {
			return nil, err
		}
		c, _ := strconv.ParseInt(r.Cnt, 10, 64)
		out[r.CompanyID] = c
	}
	return out, nil
}

// companyNames — дубликат справочника имён из internal/reports/debt/seed.go.
// Цикл импорта (debt → etl → debt) обходится дублированием. При расширении
// списка ЮЛ — синхронизировать обе копии.
var companyNames = map[string]string{
	"690591512":    "ООО «Марк Формэль»",
	"690719790":    "ООО «Формэль»",
	"6950135110":   "ООО ТД «Марк Формэль»",
	"5031159833":   "ООО «МАРК ФОРМЭЛЬ ТЕКС»",
	"9909349268":   "Филиал ООО «Марк Формэль»",
	"9731039708":   "ООО «ПТИР»",
	"695018688905": "ИП Сипарова Светлана Геннадьевна",
}

// --- Helpers --------------------------------------------------------------

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

func writeErr(w http.ResponseWriter, status int, msg string) {
	writeJSON(w, status, map[string]string{"error": msg})
}

