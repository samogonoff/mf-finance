package bugtracker

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
)

type Repo struct{ pool *pgxpool.Pool }

func NewRepo(p *pgxpool.Pool) *Repo { return &Repo{pool: p} }

// ErrDuplicateName — источник с таким именем уже есть (UNIQUE на name).
var ErrDuplicateName = errors.New("источник с таким именем уже существует")

// selectReport — репорт с автором и причиной одним запросом: админке и
// автоматике нужны имя пользователя и название источника, а не голые id.
const selectReport = `SELECT r.id, r.user_id, r.section, r.status, r.type, r.title, r.description, r.steps, r.url, r.signature,
	r.route, r.entity_ref, r.context_snapshot, r.tech_context, r.console_logs, r.network_errors, r.js_errors,
	r.screenshots, r.admin_comment, r.source_id, s.name, r.b24_task_id,
	u.name, u.last_name, u.email,
	r.claimed_by, r.claimed_at, r.mr_iid, r.mr_url,
	r.created_at, r.updated_at, r.resolved_at
	FROM bug_reports r
	LEFT JOIN users u ON u.id = r.user_id
	LEFT JOIN bug_report_sources s ON s.id = r.source_id`

func scanReport(row pgx.Row) (*Report, error) {
	var r Report
	var (
		route, entityRef, ctxSnap, tech  []byte
		consoleLogs, networkErrs, jsErrs []byte
		sourceName                       *string
		userName, userLast, userEmail    *string
	)
	if err := row.Scan(
		&r.ID, &r.UserID, &r.Section, &r.Status, &r.Type, &r.Title, &r.Description, &r.Steps, &r.URL, &r.Signature,
		&route, &entityRef, &ctxSnap, &tech, &consoleLogs, &networkErrs, &jsErrs,
		&r.Screenshots, &r.AdminComment, &r.SourceID, &sourceName, &r.B24TaskID,
		&userName, &userLast, &userEmail,
		&r.ClaimedBy, &r.ClaimedAt, &r.MRIID, &r.MRURL,
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
	if r.SourceID != nil && sourceName != nil {
		r.Source = &SourceShort{ID: *r.SourceID, Name: *sourceName}
	}
	if r.UserID != nil && userEmail != nil {
		name := strings.TrimSpace(deref(userName) + " " + deref(userLast))
		r.User = &UserShort{ID: *r.UserID, Name: name, Email: *userEmail}
	}
	return &r, nil
}

func deref(s *string) string {
	if s == nil {
		return ""
	}
	return *s
}

// Create — INSERT, возвращает id.
func (r *Repo) Create(ctx context.Context, userID *int64, in CreateInput, signature string) (int64, error) {
	route, _ := json.Marshal(safeMap(in.Route))
	entRef, _ := json.Marshal(safeMap(in.EntityRef))
	snap, _ := json.Marshal(safeMap(in.ContextSnapshot))
	tech, _ := json.Marshal(safeMap(in.TechContext))
	cl, _ := json.Marshal(safeArr(in.ConsoleLogs))
	ne, _ := json.Marshal(safeArr(in.NetworkErrors))
	js, _ := json.Marshal(safeArr(in.JSErrors))

	var id int64
	err := r.pool.QueryRow(ctx, `
		INSERT INTO bug_reports
			(user_id, section, status, type, title, description, steps, url, signature,
			 route, entity_ref, context_snapshot, tech_context, console_logs, network_errors, js_errors)
		VALUES ($1, $2, 'new', $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14, $15)
		RETURNING id`,
		userID, in.Section, in.Type, in.Title, in.Description, in.Steps, in.URL, signature,
		route, entRef, snap, tech, cl, ne, js,
	).Scan(&id)
	return id, err
}

// CreateAuto — находка автоматики: без автора и технического контекста,
// admin_comment подписывает, кто её завёл.
func (r *Repo) CreateAuto(ctx context.Context, in AutoCreateInput, signature, comment string) (int64, error) {
	var id int64
	err := r.pool.QueryRow(ctx, `
		INSERT INTO bug_reports (section, status, type, title, description, steps, url, signature, admin_comment)
		VALUES ($1, 'new', $2, $3, $4, $5, $6, $7, $8)
		RETURNING id`,
		in.Section, in.Type, in.Title, in.Description, in.Steps, in.URL, signature, comment,
	).Scan(&id)
	return id, err
}

func safeArr(a []any) []any {
	if a == nil {
		return []any{}
	}
	return a
}

func safeMap(m map[string]any) map[string]any {
	if m == nil {
		return map[string]any{}
	}
	return m
}

// UpdateScreenshots — выставляет массив screenshots (вызывается после
// успешного storage.SaveAll).
func (r *Repo) UpdateScreenshots(ctx context.Context, id int64, names []string) error {
	_, err := r.pool.Exec(ctx,
		`UPDATE bug_reports SET screenshots=$2, updated_at=NOW() WHERE id=$1`, id, names)
	return err
}

// Get — выборка одного по id. pgx.ErrNoRows, если репорта нет.
func (r *Repo) Get(ctx context.Context, id int64) (*Report, error) {
	return scanReport(r.pool.QueryRow(ctx, selectReport+` WHERE r.id=$1`, id))
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
	if errors.Is(err, pgx.ErrNoRows) {
		return 0, nil
	}
	return id, err
}

// FindOpenBySignature — незакрытый (new/in_progress) баг с такой сигнатурой.
// Закрытый и отклонённый не мешают завести баг заново, если проблема вернулась.
func (r *Repo) FindOpenBySignature(ctx context.Context, signature string) (int64, error) {
	var id int64
	err := r.pool.QueryRow(ctx, `
		SELECT id FROM bug_reports
		WHERE signature = $1 AND status IN ('new', 'in_progress')
		ORDER BY created_at DESC LIMIT 1`, signature).Scan(&id)
	if errors.Is(err, pgx.ErrNoRows) {
		return 0, nil
	}
	return id, err
}

// statusSQL — SET-фрагмент смены статуса со всеми инвариантами, завязанными на него:
//   - resolved_at ставится при входе в resolved и сбрасывается при выходе из него,
//     иначе MTTR считает время по багу, который переоткрыли;
//   - замок автоматики живёт ровно столько, сколько статус in_progress. Иначе баг,
//     который вернули в «Новый» из админки, остаётся с claimed_by и навсегда выпадает
//     из очереди разбора (в MP так пять часов простоял репорт #30).
func statusSQL(p string) string {
	return `status = ` + p + `,
		resolved_at = CASE WHEN ` + p + ` = 'resolved' THEN COALESCE(resolved_at, NOW()) ELSE NULL END,
		claimed_by  = CASE WHEN ` + p + ` = 'in_progress' THEN claimed_by ELSE NULL END,
		claimed_at  = CASE WHEN ` + p + ` = 'in_progress' THEN claimed_at ELSE NULL END`
}

// appendCommentSQL — дописать абзац к admin_comment, не затирая прежний.
func appendCommentSQL(p string) string {
	return `admin_comment = CASE WHEN admin_comment = '' THEN ` + p + ` ELSE admin_comment || E'\n\n' || ` + p + ` END`
}

// Patch — обновляет позволенные поля. Инварианты статуса — в statusSQL,
// проверка «resolved требует причину» — в Service.Patch.
func (r *Repo) Patch(ctx context.Context, id int64, p PatchInput) error {
	set := []string{"updated_at = NOW()"}
	args := []any{id}
	if p.Status != nil {
		args = append(args, *p.Status)
		set = append(set, statusSQL("$"+itoa(len(args))+"::text"))
	}
	if p.SourceID != nil {
		args = append(args, nullIfZero(*p.SourceID))
		set = append(set, "source_id = $"+itoa(len(args)))
	}
	if p.B24TaskID != nil {
		args = append(args, nullIfEmpty(strings.TrimSpace(*p.B24TaskID)))
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

func nullIfZero(v int64) *int64 {
	if v == 0 {
		return nil
	}
	return &v
}

func nullIfEmpty(s string) *string {
	if s == "" {
		return nil
	}
	return &s
}

func (r *Repo) Delete(ctx context.Context, id int64) error {
	_, err := r.pool.Exec(ctx, `DELETE FROM bug_reports WHERE id=$1`, id)
	return err
}

// ── Автоматический разбор ───────────────────────────────────────────────────

// ListUnclaimedNew — очередь разбора: новые репорты, которые ещё никто не взял.
//
// Отсекаются три случая «этим уже занимаются»: статус не new (человек перевёл
// в работу или закрыл), непустой claimed_by (взят другим прогоном) и заведённая
// задача в Б24 — её ставит человек, когда берёт баг себе, и статус при этом
// менять забывает. Старые сверху — чтобы жалоба не висела неделю.
func (r *Repo) ListUnclaimedNew(ctx context.Context, limit int, sections []string) ([]Report, error) {
	q := selectReport + ` WHERE r.status = 'new' AND r.claimed_by IS NULL
		AND COALESCE(r.b24_task_id, '') = ''`
	args := []any{clamp(limit, 1, 50)}
	if len(sections) > 0 {
		args = append(args, sections)
		q += ` AND r.section = ANY($2)`
	}
	return r.query(ctx, q+` ORDER BY r.created_at ASC LIMIT $1`, args...)
}

// Claim — атомарно взять репорт в работу: UPDATE с условием на пустой замок.
// Условие в самом UPDATE, а не проверка-затем-запись: иначе два параллельных
// прогона оба увидят «свободно» и породят два MR на одну правку.
// false — репорт не найден, уже не new или уже занят.
func (r *Repo) Claim(ctx context.Context, id int64, by string) (bool, error) {
	tag, err := r.pool.Exec(ctx, `
		UPDATE bug_reports
		SET claimed_by = $2, claimed_at = NOW(), status = 'in_progress', updated_at = NOW()
		WHERE id = $1 AND claimed_by IS NULL AND status = 'new'`, id, by)
	if err != nil {
		return false, err
	}
	return tag.RowsAffected() > 0, nil
}

// AttachMR — привязать merge request. new → in_progress; причина и сводка —
// если переданы.
func (r *Repo) AttachMR(ctx context.Context, id int64, in MRInput) error {
	set := []string{
		"updated_at = NOW()", "mr_iid = $2", "mr_url = $3",
		"status = CASE WHEN status = 'new' THEN 'in_progress' ELSE status END",
	}
	args := []any{id, in.MRIID, in.MRURL}
	if in.SourceID > 0 {
		args = append(args, in.SourceID)
		set = append(set, "source_id = $"+itoa(len(args)))
	}
	if in.Summary != "" {
		args = append(args, in.Summary)
		set = append(set, appendCommentSQL("$"+itoa(len(args))))
	}
	_, err := r.pool.Exec(ctx, `UPDATE bug_reports SET `+strings.Join(set, ", ")+` WHERE id=$1`, args...)
	return err
}

// Release — снять замок; без MR баг возвращается в «Новый», к людям.
func (r *Repo) Release(ctx context.Context, id int64, comment string) error {
	set := []string{
		"updated_at = NOW()", "claimed_by = NULL", "claimed_at = NULL",
		"status = CASE WHEN status = 'in_progress' AND mr_iid IS NULL THEN 'new' ELSE status END",
	}
	args := []any{id}
	if comment != "" {
		args = append(args, comment)
		set = append(set, appendCommentSQL("$2"))
	}
	_, err := r.pool.Exec(ctx, `UPDATE bug_reports SET `+strings.Join(set, ", ")+` WHERE id=$1`, args...)
	return err
}

// Resolve — закрыть баг после выката (замок снимает statusSQL).
func (r *Repo) Resolve(ctx context.Context, id int64, sourceID int64, comment string) error {
	set := []string{"updated_at = NOW()", statusSQL("'resolved'")}
	args := []any{id}
	if sourceID > 0 {
		args = append(args, sourceID)
		set = append(set, "source_id = $"+itoa(len(args)))
	}
	if comment != "" {
		args = append(args, comment)
		set = append(set, appendCommentSQL("$"+itoa(len(args))))
	}
	_, err := r.pool.Exec(ctx, `UPDATE bug_reports SET `+strings.Join(set, ", ")+` WHERE id=$1`, args...)
	return err
}

// ListPendingMR — баги с готовым, но ещё не выкаченным MR.
func (r *Repo) ListPendingMR(ctx context.Context, limit int) ([]Report, error) {
	return r.query(ctx, selectReport+` WHERE r.mr_iid IS NOT NULL AND r.status = 'in_progress'
		ORDER BY r.updated_at DESC LIMIT $1`, clamp(limit, 1, 100))
}

// ListByMR — баги, которые закрывает этот MR (обычно один).
func (r *Repo) ListByMR(ctx context.Context, iid int) ([]Report, error) {
	return r.query(ctx, selectReport+` WHERE r.mr_iid = $1 ORDER BY r.id`, iid)
}

func (r *Repo) query(ctx context.Context, q string, args ...any) ([]Report, error) {
	rows, err := r.pool.Query(ctx, q, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []Report{}
	for rows.Next() {
		rep, err := scanReport(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, *rep)
	}
	return out, rows.Err()
}

func clamp(v, lo, hi int) int {
	if v < lo {
		return lo
	}
	if v > hi {
		return hi
	}
	return v
}

// ── Список и метрики ────────────────────────────────────────────────────────

// List — с фильтром, пагинацией, общим COUNT.
func (r *Repo) List(ctx context.Context, f ListFilter) ([]Report, int, error) {
	conds := []string{"1=1"}
	args := []any{}
	add := func(cond string, v any) {
		args = append(args, v)
		conds = append(conds, strings.ReplaceAll(cond, "?", "$"+itoa(len(args))))
	}
	if f.Section != "" {
		add("r.section = ?", f.Section)
	}
	if f.Status != "" {
		add("r.status = ?", f.Status)
	}
	if f.Type != "" {
		add("r.type = ?", f.Type)
	}
	if f.UserID != 0 {
		add("r.user_id = ?", f.UserID)
	}
	if f.DateFrom != nil {
		add("r.created_at >= ?", *f.DateFrom)
	}
	if f.DateTo != nil {
		add("r.created_at < ?", *f.DateTo)
	}
	if f.Search != "" {
		add("(lower(r.title) LIKE ? OR lower(r.description) LIKE ? OR lower(r.url) LIKE ?)",
			"%"+strings.ToLower(f.Search)+"%")
	}
	where := strings.Join(conds, " AND ")

	var total int
	if err := r.pool.QueryRow(ctx, `SELECT COUNT(*) FROM bug_reports r WHERE `+where, args...).Scan(&total); err != nil {
		return nil, 0, err
	}

	limit := f.Limit
	if limit <= 0 || limit > 200 {
		limit = 50
	}
	args = append(args, limit, f.Offset)
	out, err := r.query(ctx, selectReport+` WHERE `+where+
		` ORDER BY r.id DESC LIMIT $`+itoa(len(args)-1)+` OFFSET $`+itoa(len(args)), args...)
	return out, total, err
}

// Metrics — агрегаты для админки; from/to (nil — без границы) режут по created_at.
func (r *Repo) Metrics(ctx context.Context, from, to *time.Time) (*Metrics, error) {
	m := &Metrics{ByStatus: map[string]int{}, BySection: map[string]int{}, ByType: map[string]int{}, TopURLs: []URLCount{}}

	where := "1=1"
	args := []any{}
	if from != nil {
		args = append(args, *from)
		where += " AND created_at >= $" + itoa(len(args))
	}
	if to != nil {
		args = append(args, *to)
		where += " AND created_at < $" + itoa(len(args))
	}

	if err := r.pool.QueryRow(ctx, `SELECT COUNT(*) FROM bug_reports WHERE `+where, args...).Scan(&m.Total); err != nil {
		return nil, err
	}
	for col, dst := range map[string]map[string]int{"status": m.ByStatus, "section": m.BySection, "type": m.ByType} {
		if err := r.scanGroupCount(ctx, col, where, args, dst); err != nil {
			return nil, err
		}
	}

	rows, err := r.pool.Query(ctx, `SELECT url, COUNT(*) AS c FROM bug_reports
		WHERE `+where+` AND url <> '' GROUP BY url ORDER BY c DESC, url LIMIT 10`, args...)
	if err != nil {
		return nil, err
	}
	for rows.Next() {
		var u URLCount
		if err := rows.Scan(&u.URL, &u.Count); err != nil {
			rows.Close()
			return nil, err
		}
		m.TopURLs = append(m.TopURLs, u)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return nil, err
	}

	var mttr *float64
	if err := r.pool.QueryRow(ctx, `
		SELECT AVG(EXTRACT(EPOCH FROM (resolved_at - created_at)))::float8
		FROM bug_reports WHERE `+where+` AND status = 'resolved' AND resolved_at IS NOT NULL`, args...).Scan(&mttr); err != nil {
		return nil, err
	}
	if mttr != nil {
		m.MTTRSeconds = *mttr
	}
	return m, nil
}

// scanGroupCount — col приходит только из литералов Metrics, не от клиента.
func (r *Repo) scanGroupCount(ctx context.Context, col, where string, args []any, dst map[string]int) error {
	rows, err := r.pool.Query(ctx, `SELECT `+col+`, COUNT(*) FROM bug_reports WHERE `+where+` GROUP BY `+col, args...)
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

// SourceExists — есть ли источник с таким id (для внятной 404 вместо FK-ошибки).
func (r *Repo) SourceExists(ctx context.Context, id int64) (bool, error) {
	var ok bool
	err := r.pool.QueryRow(ctx, `SELECT EXISTS (SELECT 1 FROM bug_report_sources WHERE id=$1)`, id).Scan(&ok)
	return ok, err
}

func (r *Repo) CreateSource(ctx context.Context, name string) (*Source, error) {
	var s Source
	err := r.pool.QueryRow(ctx,
		`INSERT INTO bug_report_sources (name) VALUES ($1)
		 RETURNING id, name, is_active, created_at`, name).
		Scan(&s.ID, &s.Name, &s.IsActive, &s.CreatedAt)
	if isUniqueViolation(err) {
		return nil, ErrDuplicateName
	}
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
	if isUniqueViolation(err) {
		return ErrDuplicateName
	}
	return err
}

func isUniqueViolation(err error) bool {
	var pgErr *pgconn.PgError
	return errors.As(err, &pgErr) && pgErr.Code == "23505"
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
