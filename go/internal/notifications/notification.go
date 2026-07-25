// Package notifications — in-app уведомления (один общий поток).
// Эталон: MP src/Entity/Notification.php, упрощённая версия.
package notifications

import "time"

// Type — допустимые значения колонки notifications.type.
const (
	TypeInfo    = "info"
	TypeSuccess = "success"
	TypeWarning = "warning"
	TypeError   = "error"
)

// ObjectType — служебные метки (для аналитики и фильтров на админке).
const (
	ObjWelcome      = "welcome"
	ObjBugReportNew = "bug_report_new"
	ObjCostPriceSet = "cost_price_set"
	ObjPlanTask     = "plan_task" // задание модуля «Тактические планы»
)

// Notification — строка из таблицы notifications (см. миграции 0003, 0005).
type Notification struct {
	ID         int64          `json:"id"`
	UserID     int64          `json:"user_id"`
	Title      string         `json:"title"`
	Message    string         `json:"message"`
	Type       string         `json:"type"`
	IsRead     bool           `json:"is_read"`
	CreatedAt  time.Time      `json:"created_at"`
	ReadAt     *time.Time     `json:"read_at,omitempty"`
	Data       map[string]any `json:"data"`
	ObjectType string         `json:"object_type,omitempty"`
}

// Input — что передаёт вызывающая сторона при создании.
type Input struct {
	UserID     int64
	Title      string
	Message    string
	Type       string // если пусто — info
	Data       map[string]any
	ObjectType string
}
