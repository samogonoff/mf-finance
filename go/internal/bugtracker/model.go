// Package bugtracker — внутренний баг-трекер кабинета.
// Эталон: MP src/Entity/BugTracker/BugReport.php.
package bugtracker

import "time"

// Статусы и типы — белые списки, валидируются на входе.
const (
	StatusNew        = "new"
	StatusInProgress = "in_progress"
	StatusResolved   = "resolved"
	StatusDuplicate  = "duplicate"
	StatusRejected   = "rejected"

	TypeBug         = "bug"
	TypeData        = "data"
	TypeUI          = "ui"
	TypePerformance = "performance"
	TypeOther       = "other"

	SectionOps = "ops"
)

// Пределы входных данных — как в MP BugReportController: репорт не должен
// раздуваться логами, а описание — превращаться в выгрузку.
const (
	maxTitleRunes       = 200
	maxDescriptionRunes = 5000
	maxStepsRunes       = 2000
	maxURLRunes         = 2048
	maxLogEntries       = 50 // console/network/js — оставляем последние N
	maxAgentRunes       = 64
)

var (
	allowedStatuses = map[string]struct{}{
		StatusNew: {}, StatusInProgress: {}, StatusResolved: {},
		StatusDuplicate: {}, StatusRejected: {},
	}
	allowedTypes = map[string]struct{}{
		TypeBug: {}, TypeData: {}, TypeUI: {}, TypePerformance: {}, TypeOther: {},
	}
	// Секции выводятся фронтом из route; на бэке валидируются по списку.
	// ops — находки мониторинга: их заводит автоматика через /internal/bugtracker,
	// а не человек с экрана (нет ни URL страницы, ни шагов), поэтому отдельным
	// разделом, чтобы не мешать с жалобами пользователей в фильтрах.
	allowedSections = map[string]struct{}{
		"finance": {}, "cost": {}, "operations": {}, "reports": {}, "plans": {},
		"counterparties": {}, "analytics": {}, "account": {}, "admin": {}, "other": {},
		SectionOps: {},
	}
)

func IsValidStatus(s string) bool  { _, ok := allowedStatuses[s]; return ok }
func IsValidType(t string) bool    { _, ok := allowedTypes[t]; return ok }
func IsValidSection(s string) bool { _, ok := allowedSections[s]; return ok }

// Report — строка таблицы bug_reports (миграция 0004).
type Report struct {
	ID              int64          `json:"id"`
	UserID          *int64         `json:"user_id,omitempty"`
	Section         string         `json:"section"`
	Status          string         `json:"status"`
	Type            string         `json:"type"`
	Title           string         `json:"title"`
	Description     string         `json:"description"`
	Steps           string         `json:"steps"`
	URL             string         `json:"url"`
	Signature       string         `json:"signature"`
	Route           map[string]any `json:"route"`
	EntityRef       map[string]any `json:"entity_ref"`
	ContextSnapshot map[string]any `json:"context_snapshot"`
	TechContext     map[string]any `json:"tech_context"`
	ConsoleLogs     []any          `json:"console_logs"`
	NetworkErrors   []any          `json:"network_errors"`
	JSErrors        []any          `json:"js_errors"`
	Screenshots     []string       `json:"screenshots"`
	AdminComment    string         `json:"admin_comment"`
	SourceID        *int64         `json:"source_id,omitempty"`
	Source          *SourceShort   `json:"source"`
	B24TaskID       *string        `json:"b24_task_id,omitempty"`
	User            *UserShort     `json:"user"`
	// Автоматический разбор (миграция 0059). claimed_by — замок: пока он стоит,
	// баг «в работе» у автоматики и в очередь разбора не попадает.
	ClaimedBy  *string    `json:"claimed_by"`
	ClaimedAt  *time.Time `json:"claimed_at"`
	MRIID      *int       `json:"mr_iid"`
	MRURL      *string    `json:"mr_url"`
	CreatedAt  time.Time  `json:"created_at"`
	UpdatedAt  time.Time  `json:"updated_at"`
	ResolvedAt *time.Time `json:"resolved_at,omitempty"`
}

// UserShort — автор репорта (LEFT JOIN users).
type UserShort struct {
	ID    int64  `json:"id"`
	Name  string `json:"name"`
	Email string `json:"email"`
}

// SourceShort — причина, которой закрыт баг.
type SourceShort struct {
	ID   int64  `json:"id"`
	Name string `json:"name"`
}

// Source — справочник причин (cause) бага.
type Source struct {
	ID        int64     `json:"id"`
	Name      string    `json:"name"`
	IsActive  bool      `json:"is_active"`
	CreatedAt time.Time `json:"created_at"`
}

// CreateInput — payload пользовательской формы (JSON-часть multipart-запроса).
type CreateInput struct {
	Section         string         `json:"section"`
	Type            string         `json:"type"`
	Title           string         `json:"title"`
	Description     string         `json:"description"`
	Steps           string         `json:"steps"`
	URL             string         `json:"url"`
	Route           map[string]any `json:"route"`
	EntityRef       map[string]any `json:"entity_ref"`
	ContextSnapshot map[string]any `json:"context_snapshot"`
	TechContext     map[string]any `json:"tech_context"`
	ConsoleLogs     []any          `json:"console_logs"`
	NetworkErrors   []any          `json:"network_errors"`
	JSErrors        []any          `json:"js_errors"`
}

// PatchInput — что админ может менять у репорта.
// source_id = 0 снимает причину, b24_task_id = "" — ссылку на задачу.
type PatchInput struct {
	Status       *string `json:"status,omitempty"`
	SourceID     *int64  `json:"source_id,omitempty"`
	B24TaskID    *string `json:"b24_task_id,omitempty"`
	AdminComment *string `json:"admin_comment,omitempty"`
}

// ListFilter — фильтры списка для админки.
type ListFilter struct {
	Section  string
	Status   string
	Type     string
	UserID   int64
	DateFrom *time.Time
	DateTo   *time.Time
	Search   string
	Limit    int
	Offset   int
}

// Metrics — агрегаты для дашборда админки (за период, если он задан).
type Metrics struct {
	Total       int            `json:"total"`
	ByStatus    map[string]int `json:"by_status"`
	BySection   map[string]int `json:"by_section"`
	ByType      map[string]int `json:"by_type"`
	TopURLs     []URLCount     `json:"top_urls"`     // страницы, с которых жалуются чаще всего
	MTTRSeconds float64        `json:"mttr_seconds"` // среднее resolved_at - created_at среди resolved
}

type URLCount struct {
	URL   string `json:"url"`
	Count int    `json:"count"`
}

// ── Автоматический разбор (/internal/bugtracker) ────────────────────────────

// MRInput — POST /internal/bugtracker/report/{id}/mr.
// Причина (source_id) передаётся здесь, а не при выкате: её знает тот, кто
// разобрал дефект, а бот, закрывающий баг после мерджа, придумать её не может.
type MRInput struct {
	MRIID    int    `json:"mr_iid"`
	MRURL    string `json:"mr_url"`
	Summary  string `json:"summary"`
	SourceID int64  `json:"source_id"`
}

// ResolveInput — POST /internal/bugtracker/report/{id}/resolve.
type ResolveInput struct {
	SourceID int64  `json:"source_id"`
	Comment  string `json:"comment"`
}

// AutoCreateInput — POST /internal/bugtracker/report: находка мониторинга.
// Signature задаёт сама автоматика (например, «elk:plans_publish:…»): по ней
// повторная находка той же незакрытой проблемы не заводит новый баг.
type AutoCreateInput struct {
	Section     string `json:"section"`
	Type        string `json:"type"`
	Title       string `json:"title"`
	Description string `json:"description"`
	Steps       string `json:"steps"`
	URL         string `json:"url"`
	Signature   string `json:"signature"`
	By          string `json:"by"`
}
