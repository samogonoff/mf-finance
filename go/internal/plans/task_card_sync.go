package plans

import (
	"context"
	"encoding/json"
	"strings"
)

// Согласование заданий-заполнения с состоянием карточек форм.
//
// Задание этапа 1.1 значит «заполни форму». Критерий выполнения у него ровно
// один — форма ОТПРАВЛЕНА на согласование. До сих пор статус задания жил
// отдельно: человек заполнял форму, отправлял её, а задание оставалось «в
// работе», либо наоборот — задание помечали «готово» при пустой форме. На
// таймлайне и в кокпите это выглядело как рассинхрон процесса, чем и было.
//
// Здесь статус задания выводится из карточек, которые оно покрывает:
//
//	все карточки ушли с шага заполнения → done
//	хотя бы одна вернулась на заполнение → returned (человеку снова работать)
//
// Обратной операции «карточка следует за заданием» нет и быть не должно:
// источник истины — форма и её маршрут, а задание это наряд на работу.

// taskFillStage — этап, на котором задания означают «заполнить форму».
const taskFillStage = "1.1"

// SyncFillTasks — привести задания заполнения периода в соответствие с карточками.
//
// Возвращает число изменённых заданий. Ошибку чтения шаблонов не считаем
// фатальной для процесса — переход карточки уже состоялся, поэтому вызывающий
// логирует и идёт дальше.
func (s *TaskStore) SyncFillTasks(ctx context.Context, plID int64, cards []Card) (int, error) {
	if s.pool == nil || plID == 0 || len(cards) == 0 {
		return 0, nil
	}
	// Задания заполнения периода вместе с фильтром своего шаблона: из фильтра
	// берётся, какие карточки задание покрывает (страна у розницы, сегмент у МП).
	rows, err := s.pool.Query(ctx, `
		SELECT t.id, t.form_code, t.status, COALESCE(tpl.cfo_filter, '{}'::jsonb)
		  FROM pl_task t
		  LEFT JOIN pl_task_template tpl ON tpl.id = t.template_id
		 WHERE t.pl_id = $1 AND t.stage_code = $2`, plID, taskFillStage)
	if err != nil {
		return 0, err
	}
	type fillTask struct {
		id     int64
		form   string
		status string
		filter CfoFilter
	}
	var tasks []fillTask
	for rows.Next() {
		var ft fillTask
		var raw []byte
		if err := rows.Scan(&ft.id, &ft.form, &ft.status, &raw); err != nil {
			rows.Close()
			return 0, err
		}
		_ = json.Unmarshal(raw, &ft.filter)
		tasks = append(tasks, ft)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return 0, err
	}

	changed := 0
	for _, ft := range tasks {
		covered := cardsCoveredByTask(cards, ft.form, ft.filter)
		if len(covered) == 0 {
			continue // задание ни одной карточке не соответствует — не трогаем
		}
		want := fillTaskStatus(covered)
		if want == "" || want == ft.status {
			continue
		}
		// Статус задания меняем прямым UPDATE, а не через applyTaskAction:
		// это не действие человека, а следствие состояния формы, и переходы
		// «pending → done» здесь законны (заполнить и отправить можно, не нажимая
		// «взять в работу»).
		if _, err := s.pool.Exec(ctx,
			`UPDATE pl_task SET status = $2, updated_at = NOW() WHERE id = $1`, ft.id, want); err != nil {
			return changed, err
		}
		s.logTaskEvent(ctx, ft.id, "sync_from_card", 0, nil, ft.status, want,
			"состояние формы: "+fillTaskReason(covered), nil)
		changed++
	}
	return changed, nil
}

// cardsCoveredByTask — какие карточки покрывает задание заполнения.
//
// Розница: одно задание на страну (фильтр country) — карточка со scope_key той
// же страны. МП: одно задание на все площадки (segment=all) — обе карточки,
// large и small. Конкретный сегмент в фильтре — только своя карточка.
func cardsCoveredByTask(cards []Card, formCode string, f CfoFilter) []Card {
	scope := strings.TrimSpace(f.Country)
	if scope == "" {
		scope = strings.TrimSpace(f.Segment)
	}
	var out []Card
	for _, c := range cards {
		if c.FormCode != formCode {
			continue
		}
		switch {
		case scope == "" || scope == "all":
			out = append(out, c) // задание покрывает все карточки формы
		case strings.EqualFold(scope, c.ScopeKey):
			out = append(out, c)
		}
	}
	return out
}

// fillTaskStatus — статус задания по состоянию его карточек. Пустая строка —
// «оставить как есть» (форма ещё заполняется, и статус задания решает человек:
// pending, in_progress или review у делегата).
func fillTaskStatus(covered []Card) string {
	allPassed := true
	returned := false
	for _, c := range covered {
		if !cardPassedStep(c, taskFillStage) {
			allPassed = false
		}
		if c.Status == CardReturned {
			returned = true
		}
	}
	switch {
	case returned:
		// Форму вернули на доработку — задание снова в работе у исполнителя.
		return TaskReturned
	case allPassed:
		return TaskDone
	default:
		return ""
	}
}

// fillTaskReason — что написать в журнал события задания.
func fillTaskReason(covered []Card) string {
	parts := make([]string, 0, len(covered))
	for _, c := range covered {
		parts = append(parts, c.Title+" — "+CardStatusLabel(c.Status, c.StepCode))
	}
	return strings.Join(parts, "; ")
}
