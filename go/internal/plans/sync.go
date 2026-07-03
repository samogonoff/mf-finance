package plans

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// Sync-движок справочников Лиса/1С (ТЗ DIR-03/04/05). Полная пересборка
// ограниченного среза в транзакции; до неё считаем diff (added/changed/removed)
// по external_id; ведём журнал plans_dir_sync_log; при ошибке — экспоненциальный
// backoff (next_retry_at); по истечении stale_after помечаем sync_status=stale.

const (
	maxDiffSample = 20
	backoffBase   = 2 * time.Minute
	backoffCap    = 6 * time.Hour
)

// SyncResult — итог одной синхронизации.
type SyncResult struct {
	Code     string `json:"code"`
	Status   string `json:"status"` // ok|error
	RowsIn   int    `json:"rows_in"`
	Added    int    `json:"added"`
	Changed  int    `json:"changed"`
	Removed  int    `json:"removed"`
	Duration int    `json:"duration_ms"`
	Error    string `json:"error,omitempty"`
}

// Syncer — оркестратор синхронизации.
type Syncer struct {
	pool      *pgxpool.Pool
	providers map[string]Provider
	cache     *DirCache
}

// NewSyncer собирает движок из набора провайдеров.
func NewSyncer(pool *pgxpool.Pool, providers []Provider, cache *DirCache) *Syncer {
	m := make(map[string]Provider, len(providers))
	for _, p := range providers {
		m[p.Code()] = p
	}
	return &Syncer{pool: pool, providers: m, cache: cache}
}

// Codes — список синхронизируемых справочников.
func (s *Syncer) Codes() []string {
	out := make([]string, 0, len(s.providers))
	for c := range s.providers {
		out = append(out, c)
	}
	return out
}

// Sync синхронизирует один справочник. triggeredBy: "cron" | "manual:<userID>".
func (s *Syncer) Sync(ctx context.Context, code, triggeredBy string) (SyncResult, error) {
	start := time.Now()
	res := SyncResult{Code: code, Status: "error"}

	prov, ok := s.providers[code]
	if !ok {
		return res, fmt.Errorf("нет провайдера синхронизации для %q", code)
	}
	var dirID int64
	if err := s.pool.QueryRow(ctx, `SELECT id FROM plans_directory WHERE code=$1`, code).Scan(&dirID); err != nil {
		return res, fmt.Errorf("справочник %q не зарегистрирован: %w", code, err)
	}

	logID := s.openLog(ctx, dirID, triggeredBy)

	newRows, err := prov.Fetch(ctx)
	if err != nil {
		s.markError(ctx, dirID, logID, err, start)
		res.Error = err.Error()
		res.Duration = int(time.Since(start).Milliseconds())
		return res, err
	}

	existing, err := s.existingPayloads(ctx, dirID)
	if err != nil {
		s.markError(ctx, dirID, logID, err, start)
		res.Error = err.Error()
		return res, err
	}
	added, changed, removed, sample := diffRows(existing, newRows)

	if err := s.replaceRows(ctx, dirID, newRows); err != nil {
		s.markError(ctx, dirID, logID, err, start)
		res.Error = err.Error()
		return res, err
	}

	dur := int(time.Since(start).Milliseconds())
	// Версию бампаем ТОЛЬКО при реальных изменениях (ТЗ: «менять версию только
	// если были изменения»); синхронный прогон без diff версию не двигает.
	hasChanges := added+changed+removed > 0
	var newVer int
	_ = s.pool.QueryRow(ctx, `
		UPDATE plans_directory
		SET sync_status='ok', synced_at=NOW(),
		    version = version + CASE WHEN $5 THEN 1 ELSE 0 END,
		    last_error='', retry_count=0, next_retry_at=NULL,
		    last_sync_added=$2, last_sync_changed=$3, last_sync_removed=$4
		WHERE id=$1 RETURNING version`, dirID, added, changed, removed, hasChanges).Scan(&newVer)
	diffJSON, _ := json.Marshal(sample)
	if hasChanges {
		_, _ = s.pool.Exec(ctx, `
			INSERT INTO plans_dir_version (directory_id, version, source, added, changed, removed, summary, diff_sample)
			VALUES ($1,$2,'sync',$3,$4,$5,$6,$7)`,
			dirID, newVer, added, changed, removed,
			fmt.Sprintf("Синхронизация: +%d ~%d −%d", added, changed, removed), diffJSON)
	}
	_, _ = s.pool.Exec(ctx, `
		UPDATE plans_dir_sync_log
		SET finished_at=NOW(), status='ok', rows_in=$2, rows_added=$3, rows_changed=$4,
		    rows_removed=$5, duration_ms=$6, diff_sample=$7
		WHERE id=$1`, logID, len(newRows), added, changed, removed, dur, diffJSON)

	if s.cache != nil {
		_ = s.cache.Invalidate(ctx, code)
		_ = s.cache.Warm(ctx, code)
	}

	res = SyncResult{Code: code, Status: "ok", RowsIn: len(newRows), Added: added, Changed: changed, Removed: removed, Duration: dur}
	return res, nil
}

// SyncAll синхронизирует все справочники, у которых пришло время повтора.
func (s *Syncer) SyncAll(ctx context.Context, triggeredBy string) []SyncResult {
	out := make([]SyncResult, 0, len(s.providers))
	for code := range s.providers {
		if !s.dueForSync(ctx, code) {
			continue
		}
		r, err := s.Sync(ctx, code, triggeredBy)
		if err != nil {
			log.Printf("plans sync %s: %v", code, err)
		}
		out = append(out, r)
	}
	return out
}

// dueForSync — пора ли синхронизировать (нет next_retry_at в будущем).
func (s *Syncer) dueForSync(ctx context.Context, code string) bool {
	var due bool
	err := s.pool.QueryRow(ctx, `
		SELECT next_retry_at IS NULL OR next_retry_at <= NOW()
		FROM plans_directory WHERE code=$1`, code).Scan(&due)
	return err == nil && due
}

// RefreshStale помечает sync_status=stale у справочников, чьи данные старше
// stale_after_seconds (DIR-05). Запускается на каждом тике cron.
func (s *Syncer) RefreshStale(ctx context.Context) {
	_, _ = s.pool.Exec(ctx, `
		UPDATE plans_directory
		SET sync_status='stale'
		WHERE source IN ('lisa','1c')
		  AND sync_status='ok' AND synced_at IS NOT NULL
		  AND synced_at + (stale_after_seconds * INTERVAL '1 second') < NOW()`)
}

// Start запускает фоновый цикл синхронизации с интервалом interval.
func (s *Syncer) Start(ctx context.Context, interval time.Duration) {
	if len(s.providers) == 0 {
		return
	}
	go func() {
		// Первый прогон через минуту после старта (даём подняться зависимостям).
		t := time.NewTimer(time.Minute)
		defer t.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-t.C:
				s.RefreshStale(ctx)
				s.SyncAll(ctx, "cron")
				t.Reset(interval)
			}
		}
	}()
}

func (s *Syncer) openLog(ctx context.Context, dirID int64, by string) int64 {
	var id int64
	_ = s.pool.QueryRow(ctx, `
		INSERT INTO plans_dir_sync_log (directory_id, triggered_by)
		VALUES ($1, $2) RETURNING id`, dirID, by).Scan(&id)
	return id
}

func (s *Syncer) markError(ctx context.Context, dirID, logID int64, cause error, start time.Time) {
	// Экспоненциальный backoff: base * 2^retry, капается.
	var retry int
	_ = s.pool.QueryRow(ctx, `SELECT retry_count FROM plans_directory WHERE id=$1`, dirID).Scan(&retry)
	delay := backoffBase << min(retry, 12)
	if delay > backoffCap || delay <= 0 {
		delay = backoffCap
	}
	_, _ = s.pool.Exec(ctx, `
		UPDATE plans_directory
		SET sync_status='error', last_error=$2, retry_count=retry_count+1,
		    next_retry_at=NOW() + $3::interval
		WHERE id=$1`, dirID, cause.Error(), fmt.Sprintf("%d seconds", int(delay.Seconds())))
	if logID > 0 {
		_, _ = s.pool.Exec(ctx, `
			UPDATE plans_dir_sync_log SET finished_at=NOW(), status='error', error=$2, duration_ms=$3
			WHERE id=$1`, logID, cause.Error(), int(time.Since(start).Milliseconds()))
	}
}

// existingPayloads читает текущие строки (external_id → canonical JSON payload).
func (s *Syncer) existingPayloads(ctx context.Context, dirID int64) (map[string]string, error) {
	rows, err := s.pool.Query(ctx, `SELECT COALESCE(external_id,''), payload_json FROM plans_directory_row WHERE directory_id=$1`, dirID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := map[string]string{}
	for rows.Next() {
		var ext string
		var raw []byte
		if err := rows.Scan(&ext, &raw); err != nil {
			return nil, err
		}
		out[ext] = canonicalJSON(raw)
	}
	return out, rows.Err()
}

// replaceRows атомарно заменяет строки справочника (full refresh).
func (s *Syncer) replaceRows(ctx context.Context, dirID int64, newRows []SyncRow) error {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	if _, err := tx.Exec(ctx, `DELETE FROM plans_directory_row WHERE directory_id=$1`, dirID); err != nil {
		return err
	}
	batch := &pgx.Batch{}
	for _, r := range newRows {
		payload, _ := json.Marshal(r.Payload)
		batch.Queue(`INSERT INTO plans_directory_row (directory_id, external_id, payload_json) VALUES ($1,$2,$3)`,
			dirID, r.ExternalID, payload)
	}
	br := tx.SendBatch(ctx, batch)
	for range newRows {
		if _, err := br.Exec(); err != nil {
			br.Close()
			return err
		}
	}
	if err := br.Close(); err != nil {
		return err
	}
	return tx.Commit(ctx)
}

// diffRows считает added/changed/removed и собирает примеры изменений (DIR-03 UI).
func diffRows(existing map[string]string, newRows []SyncRow) (added, changed, removed int, sample []map[string]string) {
	seen := map[string]bool{}
	for _, r := range newRows {
		seen[r.ExternalID] = true
		payload, _ := json.Marshal(r.Payload)
		canon := canonicalJSON(payload)
		old, ok := existing[r.ExternalID]
		switch {
		case !ok:
			added++
			if len(sample) < maxDiffSample {
				sample = append(sample, map[string]string{"op": "added", "id": r.ExternalID})
			}
		case old != canon:
			changed++
			if len(sample) < maxDiffSample {
				sample = append(sample, map[string]string{"op": "changed", "id": r.ExternalID})
			}
		}
	}
	for ext := range existing {
		if !seen[ext] {
			removed++
			if len(sample) < maxDiffSample {
				sample = append(sample, map[string]string{"op": "removed", "id": ext})
			}
		}
	}
	return
}

// canonicalJSON нормализует JSON (Go сортирует ключи map при Marshal) для сравнения.
func canonicalJSON(raw []byte) string {
	var v map[string]any
	if err := json.Unmarshal(raw, &v); err != nil {
		return string(raw)
	}
	b, _ := json.Marshal(v)
	return string(b)
}
