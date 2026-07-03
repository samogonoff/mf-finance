package plans

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// Хранилище заданий процесса. Генерация раскрывает шаблоны этапа в задания на
// период с авто-исполнителем (ТОП ЦФО / Директор ЮЛ). Действия — через чистую
// машину applyTaskAction. Владелец этапа двигает этап, когда все задания done.

// Task — задание на этапе (с именами для UI).
type Task struct {
	ID           int64  `json:"id"`
	PlID         int64  `json:"pl_id"`
	StageCode    string `json:"stage_code"`
	FormCode     string `json:"form_code"`
	Title        string `json:"title"`
	CfoCodes     []int  `json:"cfo_codes"`
	CfoCount     int    `json:"cfo_count"`
	Role         string `json:"task_role"`
	PositionID   *int64 `json:"position_id"`
	LegalEntity  string `json:"legal_entity"`
	AssigneeID   *int64 `json:"assignee_user_id"`
	AssigneeName string `json:"assignee_name"`
	DelegateID   *int64 `json:"delegate_user_id"`
	DelegateName string `json:"delegate_name"`
	Status       string `json:"status"`
	Year         int    `json:"year,omitempty"`  // период карточки (в обзоре всех заданий)
	Month        int    `json:"month,omitempty"`
}

// TaskStore — доступ к заданиям.
type TaskStore struct {
	pool *pgxpool.Pool
	fact MpFactSource // источник факта МП (read-only) для формы
}

// NewTaskStore — конструктор. fact может быть nil (факт тогда пуст).
func NewTaskStore(pool *pgxpool.Pool, fact MpFactSource) *TaskStore {
	return &TaskStore{pool: pool, fact: fact}
}

type taskTemplate struct {
	ID        int64
	Stage     string
	Form      string
	Title     string
	Filter    CfoFilter
	Role      string
	GroupBy   string
}

func (s *TaskStore) templates(ctx context.Context) ([]taskTemplate, error) {
	rows, err := s.pool.Query(ctx, `
		SELECT id, stage_code, form_code, title, cfo_filter, task_role, group_by
		FROM pl_task_template ORDER BY sort_order, id`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := make([]taskTemplate, 0)
	for rows.Next() {
		var t taskTemplate
		var raw []byte
		if err := rows.Scan(&t.ID, &t.Stage, &t.Form, &t.Title, &raw, &t.Role, &t.GroupBy); err != nil {
			return nil, err
		}
		_ = json.Unmarshal(raw, &t.Filter)
		out = append(out, t)
	}
	return out, rows.Err()
}

// cfoRowLite — строка dir_cfo для генерации (код + ТОП + ЮЛ).
type cfoRowLite struct {
	Code        int
	PositionID  *int64
	HolderID    *int64
	LegalEntity string
}

// matchingCfo возвращает ЦФО dir_cfo под фильтр + их покрывающую должность (ТОП).
func (s *TaskStore) matchingCfo(ctx context.Context, f CfoFilter) ([]cfoRowLite, error) {
	conds := []string{"d.code='dir_cfo'", "COALESCE(r.external_id,'') NOT IN ('','0')"}
	args := []any{}
	add := func(field, val string) {
		if val != "" {
			args = append(args, val)
			conds = append(conds, fmt.Sprintf("COALESCE(r.payload_json->>'%s','')=$%d", field, len(args)))
		}
	}
	add("entity_type", f.EntityType)
	add("group_cfo1", f.GroupCFO1)
	add("group_cfo2", f.GroupCFO2)
	add("country", f.Country)
	add("legal_entity", f.LegalEntity)
	// Сегмент МП (large|small) — через справочник dir_marketplace по code_cfo.
	if f.Segment != "" {
		args = append(args, f.Segment)
		conds = append(conds, fmt.Sprintf(`r.external_id IN (
			SELECT mp.payload_json->>'code_cfo' FROM plans_directory_row mp
			JOIN plans_directory dm ON dm.id=mp.directory_id
			WHERE dm.code='dir_marketplace' AND mp.payload_json->>'segment'=$%d)`, len(args)))
	}
	rows, err := s.pool.Query(ctx, `
		SELECT (r.external_id)::int, cp.position_id, jp.holder_user_id, COALESCE(r.payload_json->>'legal_entity','')
		FROM plans_directory_row r JOIN plans_directory d ON d.id=r.directory_id
		LEFT JOIN plans_cfo_position cp ON cp.code_cfo = r.external_id
		LEFT JOIN plans_job_position jp ON jp.id = cp.position_id
		WHERE `+strings.Join(conds, " AND "), args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := make([]cfoRowLite, 0)
	for rows.Next() {
		var c cfoRowLite
		if err := rows.Scan(&c.Code, &c.PositionID, &c.HolderID, &c.LegalEntity); err != nil {
			return nil, err
		}
		out = append(out, c)
	}
	return out, rows.Err()
}

// leDirector — id директора ЮЛ (из dir_legal_entity.director_user_id).
func (s *TaskStore) leDirectors(ctx context.Context) map[string]*int64 {
	out := map[string]*int64{}
	rows, err := s.pool.Query(ctx, `
		SELECT payload_json->>'name', (payload_json->>'director_user_id')
		FROM plans_directory_row r JOIN plans_directory d ON d.id=r.directory_id
		WHERE d.code='dir_legal_entity'`)
	if err != nil {
		return out
	}
	defer rows.Close()
	for rows.Next() {
		var name string
		var uid *string
		if rows.Scan(&name, &uid) == nil && uid != nil && *uid != "" {
			var id int64
			if _, e := fmt.Sscan(*uid, &id); e == nil {
				out[name] = &id
			}
		}
	}
	return out
}

// Generate раскрывает шаблоны в задания на период (перегенерирует с нуля).
func (s *TaskStore) Generate(ctx context.Context, plID int64) (int, error) {
	tmpls, err := s.templates(ctx)
	if err != nil {
		return 0, err
	}
	directors := s.leDirectors(ctx)

	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return 0, err
	}
	defer tx.Rollback(ctx)
	// Сносим прежние НЕтронутые задания (pending) — сделанные сохраняем.
	if _, err := tx.Exec(ctx, `DELETE FROM pl_task WHERE pl_id=$1 AND status='pending'`, plID); err != nil {
		return 0, err
	}

	batch := &pgx.Batch{}
	n := 0
	queue := func(t taskTemplate, title string, codes []int, posID, assignee *int64, le string) {
		cj, _ := json.Marshal(codes)
		batch.Queue(`
			INSERT INTO pl_task (pl_id, stage_code, template_id, form_code, title, cfo_codes, task_role, position_id, legal_entity, assignee_user_id, status)
			VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,'pending')`,
			plID, t.Stage, t.ID, t.Form, title, cj, t.Role, posID, le, assignee)
		n++
	}

	for _, t := range tmpls {
		switch t.GroupBy {
		case "legal_entity":
			// Задание-согласование на каждое ЮЛ; исполнитель = директор ЮЛ.
			for le, dir := range directors {
				queue(t, t.Title+" · "+le, nil, nil, dir, le)
			}
		case "none":
			rows, _ := s.matchingCfo(ctx, t.Filter)
			codes := make([]int, 0, len(rows))
			for _, r := range rows {
				codes = append(codes, r.Code)
			}
			if len(codes) > 0 {
				queue(t, t.Title, codes, nil, nil, "")
			}
		default: // position
			rows, _ := s.matchingCfo(ctx, t.Filter)
			byPos := map[int64][]int{}
			var unassigned []int
			holderOf := map[int64]*int64{}
			for _, r := range rows {
				if r.PositionID != nil {
					byPos[*r.PositionID] = append(byPos[*r.PositionID], r.Code)
					holderOf[*r.PositionID] = r.HolderID
				} else {
					unassigned = append(unassigned, r.Code)
				}
			}
			for posID, codes := range byPos {
				p := posID
				queue(t, t.Title, codes, &p, holderOf[posID], "")
			}
			if len(unassigned) > 0 {
				queue(t, t.Title+" · без ТОПа", unassigned, nil, nil, "")
			}
		}
	}

	br := tx.SendBatch(ctx, batch)
	for i := 0; i < n; i++ {
		if _, err := br.Exec(); err != nil {
			br.Close()
			return 0, err
		}
	}
	if err := br.Close(); err != nil {
		return 0, err
	}
	if err := tx.Commit(ctx); err != nil {
		return 0, err
	}
	return n, nil
}

const taskSelect = `
SELECT t.id, t.pl_id, t.stage_code, t.form_code, t.title, t.cfo_codes, t.task_role,
       t.position_id, t.legal_entity, t.assignee_user_id,
       TRIM(COALESCE(au.last_name,'')||' '||COALESCE(au.name,'')),
       t.delegate_user_id, TRIM(COALESCE(du.last_name,'')||' '||COALESCE(du.name,'')), t.status
FROM pl_task t
LEFT JOIN users au ON au.id=t.assignee_user_id
LEFT JOIN users du ON du.id=t.delegate_user_id`

func scanTasks(rows pgx.Rows) ([]Task, error) {
	defer rows.Close()
	out := make([]Task, 0)
	for rows.Next() {
		var t Task
		var raw []byte
		if err := rows.Scan(&t.ID, &t.PlID, &t.StageCode, &t.FormCode, &t.Title, &raw, &t.Role,
			&t.PositionID, &t.LegalEntity, &t.AssigneeID, &t.AssigneeName,
			&t.DelegateID, &t.DelegateName, &t.Status); err != nil {
			return nil, err
		}
		_ = json.Unmarshal(raw, &t.CfoCodes)
		t.CfoCount = len(t.CfoCodes)
		out = append(out, t)
	}
	return out, rows.Err()
}

// ListByInstance — все задания периода (по этапам).
func (s *TaskStore) ListByInstance(ctx context.Context, plID int64) ([]Task, error) {
	rows, err := s.pool.Query(ctx, taskSelect+` WHERE t.pl_id=$1 ORDER BY t.stage_code, t.id`, plID)
	if err != nil {
		return nil, err
	}
	return scanTasks(rows)
}

// ListAll — ВСЕ задания всех карточек (для админа/админа планов) с периодом.
func (s *TaskStore) ListAll(ctx context.Context) ([]Task, error) {
	rows, err := s.pool.Query(ctx, `
		SELECT t.id, t.pl_id, t.stage_code, t.form_code, t.title, t.cfo_codes, t.task_role,
		       t.position_id, t.legal_entity, t.assignee_user_id,
		       TRIM(COALESCE(au.last_name,'')||' '||COALESCE(au.name,'')),
		       t.delegate_user_id, TRIM(COALESCE(du.last_name,'')||' '||COALESCE(du.name,'')), t.status,
		       COALESCE(pli.period_year,0), COALESCE(pli.period_month,0)
		FROM pl_task t
		LEFT JOIN users au ON au.id=t.assignee_user_id
		LEFT JOIN users du ON du.id=t.delegate_user_id
		LEFT JOIN pl_instance pli ON pli.id=t.pl_id
		ORDER BY pli.period_year DESC, pli.period_month DESC, t.pl_id, t.stage_code, t.id`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := make([]Task, 0)
	for rows.Next() {
		var t Task
		var raw []byte
		if err := rows.Scan(&t.ID, &t.PlID, &t.StageCode, &t.FormCode, &t.Title, &raw, &t.Role,
			&t.PositionID, &t.LegalEntity, &t.AssigneeID, &t.AssigneeName,
			&t.DelegateID, &t.DelegateName, &t.Status, &t.Year, &t.Month); err != nil {
			return nil, err
		}
		_ = json.Unmarshal(raw, &t.CfoCodes)
		t.CfoCount = len(t.CfoCodes)
		out = append(out, t)
	}
	return out, rows.Err()
}

// ListByUser — мои задания (я исполнитель или делегат).
func (s *TaskStore) ListByUser(ctx context.Context, userID int64) ([]Task, error) {
	rows, err := s.pool.Query(ctx, taskSelect+` WHERE t.assignee_user_id=$1 OR t.delegate_user_id=$1 ORDER BY t.stage_code, t.id`, userID)
	if err != nil {
		return nil, err
	}
	return scanTasks(rows)
}

// taskActorCan — права на действие (тонкие): admin — всё; исполнитель делегирует
// и работает и принимает; делегат — работает; владелец этапа — принимает/возвращает.
func taskActorCan(action string, actor int64, isAdmin bool, assignee, delegate, owner *int64) bool {
	if isAdmin {
		return true
	}
	is := func(p *int64) bool { return p != nil && *p == actor }
	switch action {
	case "delegate":
		return is(assignee)
	case "start", "submit":
		return is(assignee) || is(delegate)
	case "accept", "return", "reopen":
		return is(assignee) || is(owner)
	}
	return false
}

// Action применяет действие к заданию с проверкой прав. delegateUserID>0 для delegate.
func (s *TaskStore) Action(ctx context.Context, taskID, actorUserID int64, isAdmin bool, action string, delegateUserID int64) error {
	var status string
	var assignee, delegate, owner *int64
	err := s.pool.QueryRow(ctx, `
		SELECT t.status, t.assignee_user_id, t.delegate_user_id, si.owner_user_id
		FROM pl_task t
		LEFT JOIN pl_stage_instance si ON si.pl_id=t.pl_id AND si.stage_code=t.stage_code
		WHERE t.id=$1`, taskID).Scan(&status, &assignee, &delegate, &owner)
	if err != nil {
		return err
	}
	if !taskActorCan(action, actorUserID, isAdmin, assignee, delegate, owner) {
		return fmt.Errorf("нет прав на действие %q", action)
	}
	if action == "delegate" {
		if delegateUserID == 0 {
			return fmt.Errorf("нужен делегат")
		}
		next, err := applyTaskAction(status, action, true)
		if err != nil {
			return err
		}
		_, err = s.pool.Exec(ctx, `UPDATE pl_task SET delegate_user_id=$2, status=$3, updated_at=NOW() WHERE id=$1`, taskID, delegateUserID, next)
		return err
	}
	next, err := applyTaskAction(status, action, delegate != nil)
	if err != nil {
		return err
	}
	_, err = s.pool.Exec(ctx, `UPDATE pl_task SET status=$2, updated_at=NOW() WHERE id=$1`, taskID, next)
	return err
}

// --- Просмотр данных задания (read-only review для ответственного/согласующего) ---

// TaskDataRow — строка данных задания: ЦФО × статья со сценариями (ТЗ §7.4).
type TaskDataRow struct {
	CodeCFO     int      `json:"code_cfo"`
	NameCFO     string   `json:"name_cfo"`
	LineCode    int      `json:"line_code"`
	ExpenseName string   `json:"expense_name"`
	BlockType   string   `json:"block_type"`
	Fact        *float64 `json:"fact"`     // факт (read-only)
	Strategy    *float64 `json:"strategy"` // стратегия (read-only)
	Tactic      *float64 `json:"tactic"`   // тактика (ввод)
	Calc        *float64 `json:"calc"`     // расчёт CALC
	IsManual    bool     `json:"is_manual"`// ручная корректировка (ADJ-04)
	Reason      string   `json:"reason"`   // основание корректировки (ADJ-02)
	Original    *float64 `json:"original"` // значение до корректировки (diff ADJ-05)
}

// TaskData — данные задания: метаданные + строки + признак наличия данных.
type TaskData struct {
	Task    Task          `json:"task"`
	Rows    []TaskDataRow `json:"rows"`
	HasData bool          `json:"has_data"`
}

func (s *TaskStore) nameMap(ctx context.Context, dir, codeKey, nameKey string) map[int]string {
	out := map[int]string{}
	rows, err := s.pool.Query(ctx, `
		SELECT payload_json->>$2, payload_json->>$3
		FROM plans_directory_row r JOIN plans_directory d ON d.id=r.directory_id WHERE d.code=$1`, dir, codeKey, nameKey)
	if err != nil {
		return out
	}
	defer rows.Close()
	for rows.Next() {
		var code, name *string
		if rows.Scan(&code, &name) == nil && code != nil {
			var c int
			if _, e := fmt.Sscan(*code, &c); e == nil {
				if name != nil {
					out[c] = *name
				}
			}
		}
	}
	return out
}

// TaskData собирает срез данных задания: pl_metric по его ЦФО (или ЦФО его ЮЛ),
// факт/стратегия/тактика/расчёт + корректировки (для проверки промежуточных
// результатов ответственным/согласующим, ТЗ WF-05/ADJ-04/05).
func (s *TaskStore) TaskData(ctx context.Context, taskID int64) (TaskData, error) {
	var out TaskData
	var raw []byte
	t := &out.Task
	err := s.pool.QueryRow(ctx, taskSelect+` WHERE t.id=$1`, taskID).Scan(
		&t.ID, &t.PlID, &t.StageCode, &t.FormCode, &t.Title, &raw, &t.Role,
		&t.PositionID, &t.LegalEntity, &t.AssigneeID, &t.AssigneeName,
		&t.DelegateID, &t.DelegateName, &t.Status)
	if err != nil {
		return out, err
	}
	_ = json.Unmarshal(raw, &t.CfoCodes)
	t.CfoCount = len(t.CfoCodes)

	codes := t.CfoCodes
	// Approve-задание по ЮЛ: берём ЦФО этого ЮЛ из dir_cfo.
	if len(codes) == 0 && t.LegalEntity != "" {
		lr, _ := s.pool.Query(ctx, `
			SELECT (r.external_id)::int FROM plans_directory_row r JOIN plans_directory d ON d.id=r.directory_id
			WHERE d.code='dir_cfo' AND r.payload_json->>'legal_entity'=$1 AND COALESCE(r.external_id,'') NOT IN ('','0')`, t.LegalEntity)
		for lr.Next() {
			var c int
			if lr.Scan(&c) == nil {
				codes = append(codes, c)
			}
		}
		lr.Close()
	}
	if len(codes) == 0 {
		return out, nil
	}

	cfoNames := s.nameMap(ctx, "dir_cfo", "code_cfo", "name_cfo")
	plNames := s.nameMap(ctx, "dir_pl_line", "code_pl", "expense_name")

	rows, err := s.pool.Query(ctx, `
		SELECT m.profit_center, m.line_code, m.block_type,
		       m.amount_fact, m.amount_strategy, m.amount, m.amount_calc, m.is_manual,
		       a.reason, a.original_calculated
		FROM pl_metric m
		LEFT JOIN pl_adjustment a ON a.pl_id=m.pl_id AND a.profit_center=m.profit_center
		     AND a.line_code=m.line_code AND a.block_type=m.block_type
		WHERE m.pl_id=$1 AND m.profit_center = ANY($2)
		ORDER BY m.profit_center, m.line_code`, t.PlID, codes)
	if err != nil {
		return out, err
	}
	defer rows.Close()
	for rows.Next() {
		var r TaskDataRow
		var reason *string
		if err := rows.Scan(&r.CodeCFO, &r.LineCode, &r.BlockType,
			&r.Fact, &r.Strategy, &r.Tactic, &r.Calc, &r.IsManual, &reason, &r.Original); err != nil {
			return out, err
		}
		r.NameCFO = cfoNames[r.CodeCFO]
		r.ExpenseName = plNames[r.LineCode]
		if reason != nil {
			r.Reason = *reason
		}
		out.Rows = append(out.Rows, r)
	}
	out.HasData = len(out.Rows) > 0
	return out, rows.Err()
}

// --- Конструктор: шаблоны заданий этапа ---

// TaskTemplate — шаблон задания (для UI-конструктора).
type TaskTemplate struct {
	ID        int64     `json:"id"`
	StageCode string    `json:"stage_code"`
	FormCode  string    `json:"form_code"`
	Title     string    `json:"title"`
	Filter    CfoFilter `json:"cfo_filter"`
	Role      string    `json:"task_role"`
	GroupBy   string    `json:"group_by"`
	SortOrder int       `json:"sort_order"`
}

// ListTemplates — все шаблоны заданий.
func (s *TaskStore) ListTemplates(ctx context.Context) ([]TaskTemplate, error) {
	rows, err := s.pool.Query(ctx, `
		SELECT id, stage_code, form_code, title, cfo_filter, task_role, group_by, sort_order
		FROM pl_task_template ORDER BY stage_code, sort_order, id`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := make([]TaskTemplate, 0)
	for rows.Next() {
		var t TaskTemplate
		var raw []byte
		if err := rows.Scan(&t.ID, &t.StageCode, &t.FormCode, &t.Title, &raw, &t.Role, &t.GroupBy, &t.SortOrder); err != nil {
			return nil, err
		}
		_ = json.Unmarshal(raw, &t.Filter)
		out = append(out, t)
	}
	return out, rows.Err()
}

// UpsertTemplate создаёт/обновляет шаблон задания.
func (s *TaskStore) UpsertTemplate(ctx context.Context, t TaskTemplate) (int64, error) {
	fj, _ := json.Marshal(t.Filter)
	if t.GroupBy == "" {
		t.GroupBy = "position"
	}
	if t.Role == "" {
		t.Role = "fill"
	}
	if t.ID == 0 {
		var id int64
		err := s.pool.QueryRow(ctx, `
			INSERT INTO pl_task_template (stage_code, form_code, title, cfo_filter, task_role, group_by, sort_order)
			VALUES ($1,$2,$3,$4,$5,$6,$7) RETURNING id`,
			t.StageCode, t.FormCode, t.Title, fj, t.Role, t.GroupBy, t.SortOrder).Scan(&id)
		return id, err
	}
	_, err := s.pool.Exec(ctx, `
		UPDATE pl_task_template SET stage_code=$2, form_code=$3, title=$4, cfo_filter=$5,
			task_role=$6, group_by=$7, sort_order=$8 WHERE id=$1`,
		t.ID, t.StageCode, t.FormCode, t.Title, fj, t.Role, t.GroupBy, t.SortOrder)
	return t.ID, err
}

// DeleteTemplate удаляет шаблон.
func (s *TaskStore) DeleteTemplate(ctx context.Context, id int64) error {
	_, err := s.pool.Exec(ctx, `DELETE FROM pl_task_template WHERE id=$1`, id)
	return err
}

// StageOwner — владелец этапа (или nil).
func (s *TaskStore) StageOwner(ctx context.Context, plID int64, stageCode string) (*int64, error) {
	var owner *int64
	err := s.pool.QueryRow(ctx, `SELECT owner_user_id FROM pl_stage_instance WHERE pl_id=$1 AND stage_code=$2`, plID, stageCode).Scan(&owner)
	if err != nil {
		return nil, err
	}
	return owner, nil
}

// StageAllDone — все ли задания этапа в статусе done.
func (s *TaskStore) StageAllDone(ctx context.Context, plID int64, stageCode string) (bool, int, int, error) {
	var total, done int
	err := s.pool.QueryRow(ctx, `
		SELECT COUNT(*), COUNT(*) FILTER (WHERE status='done')
		FROM pl_task WHERE pl_id=$1 AND stage_code=$2`, plID, stageCode).Scan(&total, &done)
	if err != nil {
		return false, 0, 0, err
	}
	return total > 0 && total == done, done, total, nil
}

// StageUnassigned — число заданий этапа без исполнителя (задания-сироты: нет ТОПа
// на ЦФО → этап не сможет завершиться). Для понятного сообщения гейта.
func (s *TaskStore) StageUnassigned(ctx context.Context, plID int64, stageCode string) int {
	var n int
	_ = s.pool.QueryRow(ctx, `
		SELECT COUNT(*) FROM pl_task WHERE pl_id=$1 AND stage_code=$2 AND assignee_user_id IS NULL`,
		plID, stageCode).Scan(&n)
	return n
}

// SetAssignee переназначает исполнителя задания (админ; модель ТЗ — авто-ТОП
// переопределяемый). userID=0 снимает исполнителя. Сбрасывает делегата.
func (s *TaskStore) SetAssignee(ctx context.Context, taskID, userID int64) error {
	var u *int64
	if userID != 0 {
		u = &userID
	}
	_, err := s.pool.Exec(ctx, `
		UPDATE pl_task SET assignee_user_id=$2, delegate_user_id=NULL, updated_at=NOW() WHERE id=$1`,
		taskID, u)
	return err
}

// StageOwnerInfo — владелец этапа с именем.
type StageOwnerInfo struct {
	StageCode string `json:"stage_code"`
	UserID    *int64 `json:"owner_user_id"`
	Name      string `json:"owner_name"`
}

// StageOwners — владельцы всех этапов периода (для кокпита).
func (s *TaskStore) StageOwners(ctx context.Context, plID int64) ([]StageOwnerInfo, error) {
	rows, err := s.pool.Query(ctx, `
		SELECT si.stage_code, si.owner_user_id, TRIM(COALESCE(u.last_name,'')||' '||COALESCE(u.name,''))
		FROM pl_stage_instance si LEFT JOIN users u ON u.id=si.owner_user_id
		WHERE si.pl_id=$1 ORDER BY si.stage_code`, plID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := make([]StageOwnerInfo, 0)
	for rows.Next() {
		var o StageOwnerInfo
		if err := rows.Scan(&o.StageCode, &o.UserID, &o.Name); err != nil {
			return nil, err
		}
		out = append(out, o)
	}
	return out, rows.Err()
}

// SetOwner назначает владельца этапа (финансиста).
func (s *TaskStore) SetOwner(ctx context.Context, plID int64, stageCode string, ownerUserID int64) error {
	var owner *int64
	if ownerUserID != 0 {
		owner = &ownerUserID
	}
	_, err := s.pool.Exec(ctx, `UPDATE pl_stage_instance SET owner_user_id=$3 WHERE pl_id=$1 AND stage_code=$2`, plID, stageCode, owner)
	return err
}
