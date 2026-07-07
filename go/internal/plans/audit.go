package plans

import (
	"context"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

// Аудит «Тактических планов» (AUD-01..05, SPEC §17). Включается PLANS_AUDIT_ENABLED.
// Запись — fire-and-forget на уровне хендлера (ошибки не валят основную операцию).

// AuditEvent — событие аудита.
type AuditEvent struct {
	ID            int64  `json:"id"`
	TS            string `json:"ts"`
	UserID        int64  `json:"user_id"`
	Action        string `json:"action"`
	EntityType    string `json:"entity_type"`
	EntityID      int64  `json:"entity_id"`
	IP            string `json:"ip"`
	CorrelationID string `json:"correlation_id"`
}

// AuditFilter — фильтры списка аудита.
type AuditFilter struct {
	UserID   int64
	EntityID int64
	From     string // YYYY-MM-DD
	To       string
	Limit    int
}

// Auditor — журнал аудита.
type Auditor interface {
	Record(ctx context.Context, e AuditEvent) error
	List(ctx context.Context, f AuditFilter) ([]AuditEvent, error)
	Enabled() bool
}

// NewAuditor — pgAuditor при enabled, иначе no-op.
func NewAuditor(enabled bool, pool *pgxpool.Pool) Auditor {
	if !enabled || pool == nil {
		return noopAuditor{}
	}
	return &pgAuditor{pool: pool}
}

// noopAuditor — выключенный аудит (ничего не пишет/не отдаёт).
type noopAuditor struct{}

func (noopAuditor) Record(context.Context, AuditEvent) error           { return nil }
func (noopAuditor) List(context.Context, AuditFilter) ([]AuditEvent, error) { return []AuditEvent{}, nil }
func (noopAuditor) Enabled() bool                                      { return false }

// pgAuditor — пишет/читает plans_audit_event.
type pgAuditor struct{ pool *pgxpool.Pool }

func (a *pgAuditor) Enabled() bool { return true }

func (a *pgAuditor) Record(ctx context.Context, e AuditEvent) error {
	var uid, eid any
	if e.UserID != 0 {
		uid = e.UserID
	}
	if e.EntityID != 0 {
		eid = e.EntityID
	}
	_, err := a.pool.Exec(ctx, `
		INSERT INTO plans_audit_event (user_id, action, entity_type, entity_id, ip, correlation_id)
		VALUES ($1, $2, $3, $4, $5, $6)`,
		uid, e.Action, e.EntityType, eid, e.IP, e.CorrelationID)
	return err
}

func (a *pgAuditor) List(ctx context.Context, f AuditFilter) ([]AuditEvent, error) {
	limit := f.Limit
	if limit <= 0 || limit > 1000 {
		limit = 200
	}
	rows, err := a.pool.Query(ctx, `
		SELECT id, ts, COALESCE(user_id, 0), action, entity_type, COALESCE(entity_id, 0), ip, correlation_id
		FROM plans_audit_event
		WHERE ($1 = 0 OR user_id = $1)
		  AND ($2 = 0 OR entity_id = $2)
		  AND ($3 = '' OR ts >= $3::date)
		  AND ($4 = '' OR ts < ($4::date + INTERVAL '1 day'))
		ORDER BY ts DESC
		LIMIT $5`, f.UserID, f.EntityID, f.From, f.To, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := make([]AuditEvent, 0)
	for rows.Next() {
		var e AuditEvent
		var ts time.Time
		if err := rows.Scan(&e.ID, &ts, &e.UserID, &e.Action, &e.EntityType, &e.EntityID, &e.IP, &e.CorrelationID); err != nil {
			return nil, err
		}
		e.TS = ts.Format(time.RFC3339)
		out = append(out, e)
	}
	return out, rows.Err()
}
