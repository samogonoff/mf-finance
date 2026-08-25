package plans

import (
	"context"
	"fmt"
	"log"
	"strings"
	"time"
)

// Журнал действий по заданию (pl_task_event, миграция 0037) — прозрачность
// передачи работы: кто, кому, когда, зачем и к какому сроку.
//
// Зачем отдельный журнал, если есть plans_audit_event: аудит модуля отвечает на
// вопрос «кто и что менял» в целом, а здесь нужна ИСТОРИЯ ОДНОГО ЗАДАНИЯ,
// которую видит сам исполнитель прямо в карточке — без прав аудитора.

// parseTaskDue — срок из запроса. Пусто → без срока; иначе RFC3339 или YYYY-MM-DD.
func parseTaskDue(raw string) (*time.Time, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return nil, nil
	}
	for _, layout := range []string{time.RFC3339, "2006-01-02T15:04", "2006-01-02"} {
		if t, err := time.Parse(layout, raw); err == nil {
			return &t, nil
		}
	}
	return nil, fmt.Errorf("не разобрать срок %q: ожидается дата вида 2026-06-02", raw)
}

// logTaskEvent — записать действие в журнал. Ошибка журнала не отменяет само
// действие (работа уже переведена), но и молчать о ней нельзя: пропавшая запись
// означает дыру в истории передач.
func (s *TaskStore) logTaskEvent(ctx context.Context, taskID int64, action string,
	actorID int64, targetID *int64, statusFrom, statusTo, comment string, due *time.Time) {

	var actor any
	if actorID != 0 {
		actor = actorID
	}
	var target any
	if targetID != nil && *targetID != 0 {
		target = *targetID
	}
	if _, err := s.pool.Exec(ctx, `
		INSERT INTO pl_task_event (task_id, action, actor_id, target_id, status_from, status_to, comment, due_at)
		VALUES ($1,$2,$3,$4,$5,$6,$7,$8)`,
		taskID, action, actor, target, statusFrom, statusTo, comment, due); err != nil {
		log.Printf("plans: событие задания %d (%s) не записано в журнал: %v", taskID, action, err)
	}
}

// TaskEvents — история одного задания в хронологическом порядке.
func (s *TaskStore) TaskEvents(ctx context.Context, taskID int64) ([]TaskEvent, error) {
	rows, err := s.pool.Query(ctx, `
		SELECT e.id, e.action, e.actor_id,
		       TRIM(COALESCE(au.last_name,'') || ' ' || COALESCE(au.name,'')),
		       e.target_id,
		       TRIM(COALESCE(tu.last_name,'') || ' ' || COALESCE(tu.name,'')),
		       e.status_from, e.status_to, e.comment, e.due_at, e.created_at
		  FROM pl_task_event e
		  LEFT JOIN users au ON au.id = e.actor_id
		  LEFT JOIN users tu ON tu.id = e.target_id
		 WHERE e.task_id = $1
		 ORDER BY e.created_at, e.id`, taskID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := make([]TaskEvent, 0)
	for rows.Next() {
		var e TaskEvent
		if err := rows.Scan(&e.ID, &e.Action, &e.ActorID, &e.ActorName, &e.TargetID,
			&e.TargetName, &e.StatusFrom, &e.StatusTo, &e.Comment, &e.DueAt, &e.CreatedAt); err != nil {
			return nil, err
		}
		out = append(out, e)
	}
	return out, rows.Err()
}

// TaskActionLabel — человеческая формулировка действия для истории.
// Списки заданий читают исполнители, а не разработчики: «delegate» им ничего
// не говорит, а «передал(а) в работу» — говорит.
func TaskActionLabel(action string) string {
	switch action {
	case "start":
		return "взял(а) в работу"
	case "delegate":
		return "передал(а) в работу"
	case "submit":
		return "сдал(а)"
	case "accept":
		return "принял(а)"
	case "return":
		return "вернул(а) на доработку"
	case "reopen":
		return "переоткрыл(а)"
	case "assign":
		return "назначил(а) исполнителя"
	default:
		return action
	}
}
