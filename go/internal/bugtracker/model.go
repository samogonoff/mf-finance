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
	allowedSections = map[string]struct{}{
		"finance": {}, "cost": {}, "operations": {}, "reports": {},
		"counterparties": {}, "analytics": {}, "account": {}, "admin": {}, "other": {},
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
	B24TaskID       *string        `json:"b24_task_id,omitempty"`
	CreatedAt       time.Time      `json:"created_at"`
	UpdatedAt       time.Time      `json:"updated_at"`
	ResolvedAt      *time.Time     `json:"resolved_at,omitempty"`
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
	Route           map[string]any `json:"route"`
	EntityRef       map[string]any `json:"entity_ref"`
	ContextSnapshot map[string]any `json:"context_snapshot"`
	TechContext     map[string]any `json:"tech_context"`
	ConsoleLogs     []any          `json:"console_logs"`
	NetworkErrors   []any          `json:"network_errors"`
	JSErrors        []any          `json:"js_errors"`
}

// PatchInput — что админ может менять у репорта.
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

// Metrics — агрегаты для дашборда админки.
type Metrics struct {
	Total       int            `json:"total"`
	ByStatus    map[string]int `json:"by_status"`
	BySection   map[string]int `json:"by_section"`
	ByType      map[string]int `json:"by_type"`
	MTTRSeconds float64        `json:"mttr_seconds"` // среднее resolved_at - created_at среди resolved
}
