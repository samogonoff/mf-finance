package etl

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"log"
	"strings"
	"time"
)

// IncrementalWorker — фоновый поллер delta-изменений Premaster1C → CH.
// Запускается goroutine'ой при старте go-api, на каждом тике читает settings
// (incremental_enabled, interval) и если включён — пробегает по всем ЮЛ
// с завершённым bootstrap'ом, тянет дельту по DateOfChange.
type IncrementalWorker struct {
	deps Deps
	glmf bool // также гнать дельту fact_glmf по DateOfLoad (когда DEBT_CH_SOURCE=glmf)
}

func NewIncrementalWorker(deps Deps) *IncrementalWorker {
	return &IncrementalWorker{deps: deps}
}

// NewIncrementalWorkerWithGLMF — воркер, который вдобавок к premaster тянет дельту
// fact_glmf (по DateOfLoad). Включается при DEBT_CH_SOURCE=glmf.
func NewIncrementalWorkerWithGLMF(deps Deps, glmf bool) *IncrementalWorker {
	return &IncrementalWorker{deps: deps, glmf: glmf}
}

// Start запускает loop в отдельной goroutine.
func (w *IncrementalWorker) Start(ctx context.Context) {
	go w.loop(ctx)
}

func (w *IncrementalWorker) loop(ctx context.Context) {
	log.Printf("etl: incremental worker started")
	for {
		interval := w.readInterval(ctx)
		t := time.NewTimer(interval)
		select {
		case <-ctx.Done():
			t.Stop()
			log.Printf("etl: incremental worker stopped")
			return
		case <-t.C:
			w.tick(ctx)
		}
	}
}

func (w *IncrementalWorker) tick(ctx context.Context) {
	enabled := w.readEnabled(ctx)
	tickStart := time.Now()

	if !enabled {
		w.writeSetting(ctx, "incremental_last_tick_at", tickStart.Format(time.RFC3339))
		return
	}

	inns, err := w.listActiveCompanies(ctx)
	if err != nil {
		log.Printf("etl tick: listActiveCompanies: %v", err)
		w.writeSetting(ctx, "incremental_last_error", err.Error())
		return
	}

	ch, err := newCHClient(w.deps.CHURL, w.deps.CHUser, w.deps.CHPass)
	if err != nil {
		log.Printf("etl tick: ch client: %v", err)
		w.writeSetting(ctx, "incremental_last_error", err.Error())
		return
	}

	var firstErr string
	for _, inn := range inns {
		if err := w.pullDelta(ctx, ch, inn); err != nil {
			log.Printf("etl tick: incremental %s: %v", inn, err)
			if firstErr == "" {
				firstErr = inn + ": " + err.Error()
			}
		}
	}

	// Поток GLMF: дельта fact_glmf по DateOfLoad для ЮЛ с завершённым glmf-bootstrap.
	if w.glmf {
		for _, inn := range w.listActiveGLMFCompanies(ctx) {
			if _, err := RunIncrementalGLMF(ctx, w.deps, inn); err != nil {
				log.Printf("etl tick: incremental-glmf %s: %v", inn, err)
				if firstErr == "" {
					firstErr = "glmf " + inn + ": " + err.Error()
				}
			}
		}
	}

	w.writeSetting(ctx, "incremental_last_tick_at", tickStart.Format(time.RFC3339))
	w.writeSetting(ctx, "incremental_last_error", firstErr)
}

// pullDelta — один инкрементальный pull для одного ЮЛ.
func (w *IncrementalWorker) pullDelta(ctx context.Context, ch *chClient, inn string) error {
	startedAt := time.Now()

	last, err := w.getLastChangeAt(ctx, ch, inn)
	if err != nil {
		return fmt.Errorf("last_change_at: %w", err)
	}

	// ВГО-фильтр: дельта-синк тоже тянет только внутригрупповые проводки —
	// та же логика, что в bootstrap (vgoFilter).
	vgoClause, vgoArgs := vgoFilter()
	q := extractSelectFrom() + `
WHERE p.CompanyID = @inn AND p.DateOfChange > @last` + vgoClause + `
ORDER BY p.DateOfChange, p.DocID, p.RwNm`

	args := append([]interface{}{sql.Named("inn", inn), sql.Named("last", last)}, vgoArgs...)
	rows, err := w.deps.MSSQL.QueryContext(ctx, q, args...)
	if err != nil {
		return fmt.Errorf("mssql query: %w", err)
	}
	defer rows.Close()

	country := CountryByINN[inn]
	const batchSize = 5000
	batch := make([]row, 0, batchSize)
	var totalLoaded int64
	var maxChange time.Time = last

	flush := func() error {
		if len(batch) == 0 {
			return nil
		}
		var buf bytes.Buffer
		enc := json.NewEncoder(&buf)
		for i := range batch {
			if err := enc.Encode(&batch[i]); err != nil {
				return err
			}
		}
		if err := ch.insertJSON(ctx, "finance.fact_premaster", &buf); err != nil {
			return err
		}
		totalLoaded += int64(len(batch))
		batch = batch[:0]
		return nil
	}

	for rows.Next() {
		r, err := scanExtractRow(rows, country)
		if err != nil {
			return err
		}
		if t, err := time.Parse("2006-01-02 15:04:05", r.DateOfChange); err == nil && t.After(maxChange) {
			maxChange = t
		}
		batch = append(batch, r)
		if len(batch) >= batchSize {
			if err := flush(); err != nil {
				return err
			}
		}
	}
	if err := rows.Err(); err != nil {
		return fmt.Errorf("rows: %w", err)
	}
	if err := flush(); err != nil {
		return err
	}

	finishedAt := time.Now()
	durSec := finishedAt.Sub(startedAt).Seconds()

	if totalLoaded > 0 {
		log.Printf("etl: incremental company=%s delta=%d dur=%.1fs last_change=%s",
			inn, totalLoaded, durSec, maxChange.Format(time.RFC3339))
	}

	// Чекпоинт phase='incremental'.
	if _, err := w.deps.PG.Exec(ctx, `
		INSERT INTO debt_etl_checkpoint
		    (source, phase, company_id, last_change_at, rows_loaded, started_at, updated_at, finished_at)
		VALUES ('premaster','incremental',$1,$2,$3,$4,$4,$4)
		ON CONFLICT (source, phase, company_id) DO UPDATE
		   SET last_change_at = EXCLUDED.last_change_at,
		       rows_loaded    = EXCLUDED.rows_loaded,
		       updated_at     = EXCLUDED.updated_at,
		       finished_at    = EXCLUDED.finished_at,
		       error_text     = NULL`,
		inn, maxChange, totalLoaded, finishedAt); err != nil {
		log.Printf("etl: checkpoint update %s: %v", inn, err)
	}

	// Лог в CH — только если действительно что-то загрузили (избегаем спама пустыми тиками).
	if totalLoaded > 0 {
		writeRunLog(ctx, ch, runLogEntry{
			CompanyID:    inn,
			Phase:        "incremental",
			TriggeredBy:  "cron",
			StartedAt:    startedAt,
			FinishedAt:   finishedAt,
			DurationSec:  float32(durSec),
			RowsLoaded:   uint64(totalLoaded),
			LastChangeAt: &maxChange,
		})
	}
	return nil
}

// getLastChangeAt — берёт из PG checkpoint'а phase='incremental'.
// Если нет — спрашивает CH (max(date_of_change) WHERE company_id=...).
func (w *IncrementalWorker) getLastChangeAt(ctx context.Context, ch *chClient, inn string) (time.Time, error) {
	var t sql.NullTime
	err := w.deps.PG.QueryRow(ctx, `
		SELECT last_change_at FROM debt_etl_checkpoint
		 WHERE source='premaster' AND phase='incremental' AND company_id=$1`, inn).Scan(&t)
	if err == nil && t.Valid {
		return t.Time, nil
	}
	// Fallback — спросить CH.
	q := fmt.Sprintf(
		"SELECT toUnixTimestamp(coalesce(max(date_of_change), toDateTime('1970-01-01'))) FROM finance.fact_premaster WHERE company_id = '%s'",
		strings.ReplaceAll(inn, "'", "''"))
	s, err := ch.queryString(ctx, q)
	if err != nil {
		return time.Time{}, err
	}
	var ts int64
	if _, err := fmt.Sscanf(s, "%d", &ts); err != nil {
		return time.Time{}, fmt.Errorf("parse max(date_of_change): %w", err)
	}
	return time.Unix(ts, 0), nil
}

func (w *IncrementalWorker) listActiveCompanies(ctx context.Context) ([]string, error) {
	rows, err := w.deps.PG.Query(ctx, `
		SELECT company_id FROM debt_etl_checkpoint
		 WHERE source='premaster' AND phase='bootstrap' AND finished_at IS NOT NULL
		 ORDER BY company_id`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []string{}
	for rows.Next() {
		var c string
		if err := rows.Scan(&c); err != nil {
			return nil, err
		}
		out = append(out, c)
	}
	return out, rows.Err()
}

// listActiveGLMFCompanies — ЮЛ с завершённым glmf-bootstrap (source='glmf').
func (w *IncrementalWorker) listActiveGLMFCompanies(ctx context.Context) []string {
	rows, err := w.deps.PG.Query(ctx, `
		SELECT company_id FROM debt_etl_checkpoint
		 WHERE source='glmf' AND phase='bootstrap' AND finished_at IS NOT NULL
		 ORDER BY company_id`)
	if err != nil {
		log.Printf("etl tick: listActiveGLMFCompanies: %v", err)
		return nil
	}
	defer rows.Close()
	out := []string{}
	for rows.Next() {
		var c string
		if err := rows.Scan(&c); err == nil {
			out = append(out, c)
		}
	}
	return out
}

func (w *IncrementalWorker) readEnabled(ctx context.Context) bool {
	return w.readSetting(ctx, "incremental_enabled") == "1"
}

func (w *IncrementalWorker) readInterval(ctx context.Context) time.Duration {
	s := w.readSetting(ctx, "incremental_interval_minutes")
	var n int
	if _, err := fmt.Sscanf(s, "%d", &n); err != nil || n <= 0 {
		n = 10
	}
	return time.Duration(n) * time.Minute
}

func (w *IncrementalWorker) readSetting(ctx context.Context, key string) string {
	var v string
	if err := w.deps.PG.QueryRow(ctx,
		`SELECT value FROM debt_etl_settings WHERE key=$1`, key).Scan(&v); err != nil {
		return ""
	}
	return v
}

func (w *IncrementalWorker) writeSetting(ctx context.Context, key, value string) {
	if _, err := w.deps.PG.Exec(ctx, `
		INSERT INTO debt_etl_settings (key, value, updated_at)
		VALUES ($1, $2, NOW())
		ON CONFLICT (key) DO UPDATE
		   SET value = EXCLUDED.value, updated_at = NOW()`, key, value); err != nil {
		log.Printf("etl writeSetting %s: %v", key, err)
	}
}
