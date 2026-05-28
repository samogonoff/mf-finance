package etl

import (
	"bytes"
	"context"
	"encoding/json"
	"log"
	"time"
)

// runLogEntry — одна строка журнала запусков в finance.etl_run_log.
type runLogEntry struct {
	CompanyID    string     `json:"company_id"`
	Phase        string     `json:"phase"`
	TriggeredBy  string     `json:"triggered_by"`
	StartedAt    time.Time  `json:"started_at"`
	FinishedAt   time.Time  `json:"finished_at"`
	DurationSec  float32    `json:"duration_sec"`
	RowsLoaded   uint64     `json:"rows_loaded"`
	LastChangeAt *time.Time `json:"last_change_at,omitempty"`
	ErrorText    string     `json:"error_text"`
}

// MarshalJSON форматирует time.Time как 'YYYY-MM-DD HH:MM:SS' (CH формат).
func (e runLogEntry) MarshalJSON() ([]byte, error) {
	type alias struct {
		CompanyID    string  `json:"company_id"`
		Phase        string  `json:"phase"`
		TriggeredBy  string  `json:"triggered_by"`
		StartedAt    string  `json:"started_at"`
		FinishedAt   string  `json:"finished_at"`
		DurationSec  float32 `json:"duration_sec"`
		RowsLoaded   uint64  `json:"rows_loaded"`
		LastChangeAt *string `json:"last_change_at,omitempty"`
		ErrorText    string  `json:"error_text"`
	}
	f := "2006-01-02 15:04:05"
	a := alias{
		CompanyID: e.CompanyID, Phase: e.Phase, TriggeredBy: e.TriggeredBy,
		StartedAt: e.StartedAt.Format(f), FinishedAt: e.FinishedAt.Format(f),
		DurationSec: e.DurationSec, RowsLoaded: e.RowsLoaded, ErrorText: e.ErrorText,
	}
	if e.LastChangeAt != nil {
		s := e.LastChangeAt.Format(f)
		a.LastChangeAt = &s
	}
	return json.Marshal(a)
}

// writeRunLog — best-effort запись в etl_run_log. Если CH недоступен — лог
// печатается в stderr (через log.Printf), но операция не валится.
func writeRunLog(ctx context.Context, ch *chClient, e runLogEntry) {
	body, err := json.Marshal(e)
	if err != nil {
		log.Printf("etl_run_log: marshal: %v", err)
		return
	}
	if err := ch.insertJSON(ctx, "finance.etl_run_log", bytes.NewReader(body)); err != nil {
		log.Printf("etl_run_log: insert: %v", err)
	}
}
