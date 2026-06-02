package bugtracker

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

type Repo struct{ pool *pgxpool.Pool }

func NewRepo(p *pgxpool.Pool) *Repo { return &Repo{pool: p} }

const reportCols = `id, user_id, section, status, type, title, description, steps, signature,
	route, entity_ref, context_snapshot, tech_context, console_logs, network_errors, js_errors,
	screenshots, admin_comment, source_id, b24_task_id, created_at, updated_at, resolved_at`

func scanReport(row pgx.Row) (*Report, error) {
	var r Report
	var (
		route, entityRef, ctxSnap, tech                []byte
		consoleLogs, networkErrs, jsErrs               []byte
	)
	if err := row.Scan(
		&r.ID, &r.UserID, &r.Section, &r.Status, &r.Type, &r.Title, &r.Description, &r.Steps, &r.Signature,
		&route, &entityRef, &ctxSnap, &tech, &consoleLogs, &networkErrs, &jsErrs,
		&r.Screenshots, &r.AdminComment, &r.SourceID, &r.B24TaskID,
		&r.CreatedAt, &r.UpdatedAt, &r.ResolvedAt,
	); err != nil {
		return nil, err
	}
	_ = json.Unmarshal(route, &r.Route)
	_ = json.Unmarshal(entityRef, &r.EntityRef)
	_ = json.Unmarshal(ctxSnap, &r.ContextSnapshot)
	_ = json.Unmarshal(tech, &r.TechContext)
	_ = json.Unmarshal(consoleLogs, &r.ConsoleLogs)
	_ = json.Unmarshal(networkErrs, &r.NetworkErrors)
	_ = json.Unmarshal(jsErrs, &r.JSErrors)

	if r.Route == nil {
		r.Route = map[string]any{}
	}
	if r.EntityRef == nil {
		r.EntityRef = map[string]any{}
	}
	if r.ContextSnapshot == nil {
		r.ContextSnapshot = map[string]any{}
	}
	if r.TechContext == nil {
		r.TechContext = map[string]any{}
	}
	if r.Screenshots == nil {
		r.Screenshots = []string{}
	}
	return &r, nil
}

// Create — INSERT, возвращает id.
func (r *Repo) Create(ctx context.Context, userID *int64, in CreateInput, signature string) (int64, error) {
	route, _ := json.Marshal(in.Route)
	entRef, _ := json.Marshal(in.EntityRef)
	snap, _ := json.Marshal(in.ContextSnapshot)
	tech, _ := json.Marshal(in.TechContext)
	cl, _ := json.Marshal(safeArr(in.ConsoleLogs))
	ne, _ := json.Marshal(safeArr(in.NetworkErrors))
	js, _ := json.Marshal(safeArr(in.JSErrors))

	var id int64
	err := r.pool.QueryRow(ctx, `
		INSERT INTO bug_reports
			(user_id, section, status, type, title, description, steps, signature,
			 route, entity_ref, context_snapshot, tech_context, console_logs, network_errors, js_errors)
		VALUES ($1, $2, 'new', $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14)
		RETURNING id`,
		userID, in.Section, in.Type, in.Title, in.Description, in.Steps, signature,
		route, entRef, snap, tech, cl, ne, js,
	).Scan(&id)
	return id, err
}

func safeArr(a []any) []any {
	if a == nil {
		return []any{}
	}
	return a
}

// UpdateScreenshots — выставляет массив screenshots (вызывается после
// успешного storage.SaveAll).
func (r *Repo) UpdateScreenshots(ctx context.Context, id int64, names []string) error {
	_, err := r.pool.Exec(ctx,
		`UPDATE bug_reports SET screenshots=$2, updated_at=NOW() WHERE id=$1`, id, names)
	return err
}

// Get — выборка одного по id.
func (r *Repo) Get(ctx context.Context, id int64) (*Report, error) {
	return scanReport(r.pool.QueryRow(ctx, `SELECT `+reportCols+` FROM bug_reports WHERE id=$1`, id))
}

// FindRecentDuplicate — ищет репорт того же user_id с тем же signature
// за последние `within`. Возвращает id или 0.
func (r *Repo) FindRecentDuplicate(ctx context.Context, userID *int64, signature string, within time.Duration) (int64, error) {
	if userID == nil {
		return 0, nil
	}
	var id int64
	err := r.pool.QueryRow(ctx, `
		SELECT id FROM bug_reports
		WHERE user_id = $1 AND signature = $2 AND created_at > NOW() - $3::interval
		ORDER BY id DESC LIMIT 1`,
		*userID, signature, fmt.Sprintf("%d seconds", int(within.Seconds())),
	).Scan(&id)
	if err == pgx.ErrNoRows {
		return 0, nil
	}
	return id, err
}

// Patch — обновляет позволенные поля. Если статус становится resolved —
// resolved_at = NOW().
func (r *Repo) Patch(ctx context.Context, id int64, p PatchInput) error {
	set := []string{"updated_at = NOW()"}
	args := []any{id}
	if p.Status != nil {
		args = append(args, *p.Status)
		set = append(set, "status = $"+itoa(len(args)))
		if *p.Status == StatusResolved {
			set = append(set, "resolved_at = COALESCE(resolved_at, NOW())")
		}
	}
	if p.SourceID != nil {
		args = append(args, *p.SourceID)
		set = append(set, "source_id = $"+itoa(len(args)))
	}
	if p.B24TaskID != nil {
		args = append(args, *p.B24TaskID)
		set = append(set, "b24_task_id = $"+itoa(len(args)))
	}
	if p.AdminComment != nil {
		args = append(args, *p.AdminComment)
		set = append(set, "admin_comment = $"+itoa(len(args)))
	}
	if len(set) == 1 {
		return nil // нечего обновлять
	}
	_, err := r.pool.Exec(ctx, `UPDATE bug_reports SET `+strings.Join(set, ", ")+` WHERE id=$1`, args...)
	return err
}

func (r *Repo) Delete(ctx context.Context, id int64) error {
	_, err := r.pool.Exec(ctx, `DELETE FROM bug_reports WHERE id=$1`, id)
	return err
}

// List — с фильтром, пагинацией, общим COUNT.
func (r *Repo) List(ctx context.Context, f ListFilter) ([]Report, int, error) {
	conds := []string{"1=1"}
	args := []any{}
	add := func(cond string, v any) {
		args = append(args, v)
		conds = append(conds, strings.ReplaceAll(cond, "?", "$"+itoa(len(args))))
	}
	if f.Section != "" {
		add("section = ?", f.Section)
	}
	if f.Status != "" {
		add("status = ?", f.Status)
	}
	if f.Type != "" {
		add("type = ?", f.Type)
	}
	if f.UserID != 0 {
		add("user_id = ?", f.UserID)
	}
	if f.DateFrom != nil {
		add("created_at >= ?", *f.DateFrom)
	}
	if f.DateTo != nil {
		add("created_at <= ?", *f.DateTo)
	}
	if f.Search != "" {
		args = append(args, "%"+strings.ToLower(f.Search)+"%")
		i := itoa(len(args))
		conds = append(conds, "(lower(title) LIKE $"+i+" OR lower(description) LIKE $"+i+")")
	}
	where := strings.Join(conds, " AND ")

	var total int
	if err := r.pool.QueryRow(ctx, `SELECT COUNT(*) FROM bug_reports WHERE `+where, args...).Scan(&total); err != nil {
		return nil, 0, err
	}

	limit := f.Limit
	if limit <= 0 || limit > 200 {
		limit = 50
	}
	args = append(args, limit, f.Offset)
	q := `SELECT ` + reportCols + ` FROM bug_reports WHERE ` + where +
		` ORDER BY id DESC LIMIT $` + itoa(len(args)-1) + ` OFFSET $` + itoa(len(args))

	rows, err := r.pool.Query(ctx, q, args...)
	if err != nil {
		return nil, 0, err
	}
	defer rows.Close()

	out := make([]Report, 0, limit)
	for rows.Next() {
		rep, err := scanReport(rows)
		if err != nil {
			return nil, 0, err
		}
		out = append(out, *rep)
	}
	return out, total, rows.Err()
}

// Metrics — агрегаты для админки.
func (r *Repo) Metrics(ctx context.Context) (*Metrics, error) {
	m := &Metrics{ByStatus: map[string]int{}, BySection: map[string]int{}, ByType: map[string]int{}}

	if err := r.pool.QueryRow(ctx, `SELECT COUNT(*) FROM bug_reports`).Scan(&m.Total); err != nil {
		return nil, err
	}
	if err := scanGroupCount(ctx, r.pool, `status`, m.ByStatus); err != nil {
		return nil, err
	}
	if err := scanGroupCount(ctx, r.pool, `section`, m.BySection); err != nil {
		return nil, err
	}
	if err := scanGroupCount(ctx, r.pool, `type`, m.ByType); err != nil {
		return nil, err
	}
	var mttr *float64
	if err := r.pool.QueryRow(ctx, `
		SELECT AVG(EXTRACT(EPOCH FROM (resolved_at - created_at)))
		FROM bug_reports WHERE status = 'resolved' AND resolved_at IS NOT NULL`).Scan(&mttr); err != nil {
		return nil, err
	}
	if mttr != nil {
		m.MTTRSeconds = *mttr
	}
	return m, nil
}

func scanGroupCount(ctx context.Context, p *pgxpool.Pool, col string, dst map[string]int) error {
	rows, err := p.Query(ctx, `SELECT `+col+`, COUNT(*) FROM bug_reports GROUP BY `+col)
	if err != nil {
		return err
	}
	defer rows.Close()
	for rows.Next() {
		var k string
		var n int
		if err := rows.Scan(&k, &n); err != nil {
			return err
		}
		dst[k] = n
	}
	return rows.Err()
}

// ── Источники ───────────────────────────────────────────────────────────────

func (r *Repo) ListSources(ctx context.Context, onlyActive bool) ([]Source, error) {
	q := `SELECT id, name, is_active, created_at FROM bug_report_sources`
	if onlyActive {
		q += ` WHERE is_active = TRUE`
	}
	q += ` ORDER BY name`
	rows, err := r.pool.Query(ctx, q)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []Source{}
	for rows.Next() {
		var s Source
		if err := rows.Scan(&s.ID, &s.Name, &s.IsActive, &s.CreatedAt); err != nil {
			return nil, err
		}
		out = append(out, s)
	}
	return out, rows.Err()
}

func (r *Repo) CreateSource(ctx context.Context, name string) (*Source, error) {
	var s Source
	err := r.pool.QueryRow(ctx,
		`INSERT INTO bug_report_sources (name) VALUES ($1)
		 RETURNING id, name, is_active, created_at`, name).
		Scan(&s.ID, &s.Name, &s.IsActive, &s.CreatedAt)
	return &s, err
}

func (r *Repo) PatchSource(ctx context.Context, id int64, name *string, active *bool) error {
	set := []string{}
	args := []any{id}
	if name != nil {
		args = append(args, *name)
		set = append(set, "name = $"+itoa(len(args)))
	}
	if active != nil {
		args = append(args, *active)
		set = append(set, "is_active = $"+itoa(len(args)))
	}
	if len(set) == 0 {
		return nil
	}
	_, err := r.pool.Exec(ctx, `UPDATE bug_report_sources SET `+strings.Join(set, ", ")+` WHERE id=$1`, args...)
	return err
}

// itoa скопирована, чтобы не тянуть auth-пакет ради единственного хелпера.
func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	var buf [12]byte
	i := len(buf)
	for n > 0 {
		i--
		buf[i] = byte('0' + n%10)
		n /= 10
	}
	return string(buf[i:])
}
