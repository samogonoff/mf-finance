package plans

import (
	"context"
	"time"
)

// Богатые метаданные справочника для UI: реестр (DIR-01) + версия/актуальность
// (DIR-02) + статус синхронизации (DIR-03/04) + настройки кэша/stale (DIR-05) +
// презентационная схема из реестра. Состояние — из БД, схема — из dirSchemas.

// DirMetaRich — полная карточка справочника для экрана «Справочники».
type DirMetaRich struct {
	Code        string     `json:"code"`
	Name        string     `json:"name"`
	Description string     `json:"description"`
	Group       DirGroup   `json:"group"`
	Icon        string     `json:"icon"`
	Stage       string     `json:"stage"`
	Source      string     `json:"source"`
	Editable    bool       `json:"editable"`
	Syncable    bool       `json:"syncable"`
	SyncStatus  string     `json:"sync_status"`
	SyncedAt    *time.Time `json:"synced_at"`
	LastError   string     `json:"last_error"`
	Version     int        `json:"version"`
	RowCount    int        `json:"row_count"`
	CacheTTL    int        `json:"cache_ttl_seconds"`
	StaleAfter  int        `json:"stale_after_seconds"`
	RetryCount  int        `json:"retry_count"`
	LastAdded   int        `json:"last_sync_added"`
	LastChanged int        `json:"last_sync_changed"`
	LastRemoved int        `json:"last_sync_removed"`

	Columns []DirColumn `json:"columns"`
	GroupBy []string    `json:"group_by,omitempty"`
}

// DirectoriesRich — реестр справочников с метаданными + схемой для UI.
// syncableCodes — справочники, для которых есть провайдер синхронизации.
func (r *DirRepo) DirectoriesRich(ctx context.Context, syncableCodes map[string]bool) ([]DirMetaRich, error) {
	rows, err := r.pool.Query(ctx, `
		SELECT d.code, d.source, d.sync_status, d.synced_at, COALESCE(d.last_error,''),
		       d.version, d.cache_ttl_seconds, d.stale_after_seconds, d.retry_count,
		       d.last_sync_added, d.last_sync_changed, d.last_sync_removed, COUNT(rw.id)
		FROM plans_directory d
		LEFT JOIN plans_directory_row rw ON rw.directory_id = d.id
		GROUP BY d.id
		ORDER BY (d.source <> 'manual'), d.code`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := make([]DirMetaRich, 0)
	for rows.Next() {
		var m DirMetaRich
		var syncedAt *time.Time
		if err := rows.Scan(&m.Code, &m.Source, &m.SyncStatus, &syncedAt, &m.LastError,
			&m.Version, &m.CacheTTL, &m.StaleAfter, &m.RetryCount,
			&m.LastAdded, &m.LastChanged, &m.LastRemoved, &m.RowCount); err != nil {
			return nil, err
		}
		m.SyncedAt = syncedAt
		m.Editable = m.Source == "manual"
		m.Syncable = syncableCodes[m.Code]
		sch := SchemaFor(m.Code, m.Source)
		m.Name, m.Description, m.Group, m.Icon, m.Stage, m.Columns = sch.Name, sch.Description, sch.Group, sch.Icon, sch.Stage, sch.Columns
		m.GroupBy = sch.GroupBy
		out = append(out, m)
	}
	return out, rows.Err()
}

// SyncLogEntry — запись журнала синхронизаций.
type SyncLogEntry struct {
	ID         int64      `json:"id"`
	StartedAt  time.Time  `json:"started_at"`
	FinishedAt *time.Time `json:"finished_at"`
	Status     string     `json:"status"`
	TriggeredBy string    `json:"triggered_by"`
	RowsIn     int        `json:"rows_in"`
	Added      int        `json:"rows_added"`
	Changed    int        `json:"rows_changed"`
	Removed    int        `json:"rows_removed"`
	DurationMs int        `json:"duration_ms"`
	Error      string     `json:"error"`
}

// SyncLog — последние N записей журнала по справочнику.
func (r *DirRepo) SyncLog(ctx context.Context, code string, limit int) ([]SyncLogEntry, error) {
	if limit <= 0 || limit > 100 {
		limit = 20
	}
	rows, err := r.pool.Query(ctx, `
		SELECT l.id, l.started_at, l.finished_at, l.status, l.triggered_by,
		       l.rows_in, l.rows_added, l.rows_changed, l.rows_removed, l.duration_ms, l.error
		FROM plans_dir_sync_log l
		JOIN plans_directory d ON d.id = l.directory_id
		WHERE d.code = $1
		ORDER BY l.started_at DESC
		LIMIT $2`, code, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := make([]SyncLogEntry, 0)
	for rows.Next() {
		var e SyncLogEntry
		if err := rows.Scan(&e.ID, &e.StartedAt, &e.FinishedAt, &e.Status, &e.TriggeredBy,
			&e.RowsIn, &e.Added, &e.Changed, &e.Removed, &e.DurationMs, &e.Error); err != nil {
			return nil, err
		}
		out = append(out, e)
	}
	return out, rows.Err()
}

// UpdateCacheSettings меняет TTL кэша и порог устаревания (страница настроек справочника).
func (r *DirRepo) UpdateCacheSettings(ctx context.Context, code string, ttl, stale int) error {
	_, err := r.pool.Exec(ctx, `
		UPDATE plans_directory SET cache_ttl_seconds=$2, stale_after_seconds=$3 WHERE code=$1`,
		code, ttl, stale)
	return err
}
