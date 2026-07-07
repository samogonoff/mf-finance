package plans

import (
	"context"
	"fmt"
	"strings"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// Должности (job positions) — настоящие должности с носителем (IT-Директор =
// Серяков). Плоские: 1 должность = 1 носитель. Замы носителя — из plans_deputy.
// Должность покрывает набор ЦФО (plans_cfo_position) и является их ТОПом.

// JobPosition — должность с носителем, замом и числом покрытых ЦФО.
type JobPosition struct {
	ID           int64  `json:"id"`
	Title        string `json:"title"`
	Kind         string `json:"kind"` // regular | financier | founder
	HolderUserID *int64 `json:"holder_user_id"`
	HolderName   string `json:"holder_name"`
	Description  string `json:"description"`
	CfoCount     int    `json:"cfo_count"`
	DeputyName   string `json:"deputy_name"`   // глобальный зам носителя
	StageDeputs  int    `json:"stage_deputies"`// число замов по этапам
}

// JobPositionStore — CRUD должностей + маппинг ЦФО.
type JobPositionStore struct{ pool *pgxpool.Pool }

// NewJobPositionStore — конструктор.
func NewJobPositionStore(pool *pgxpool.Pool) *JobPositionStore { return &JobPositionStore{pool: pool} }

// List — должности с носителем, замом и счётчиком покрытых ЦФО.
func (s *JobPositionStore) List(ctx context.Context) ([]JobPosition, error) {
	rows, err := s.pool.Query(ctx, `
		SELECT jp.id, jp.title, COALESCE(jp.kind,'regular'), jp.holder_user_id,
		       TRIM(COALESCE(u.last_name,'')||' '||COALESCE(u.name,'')), jp.description,
		       (SELECT COUNT(*) FROM plans_cfo_position cp WHERE cp.position_id=jp.id),
		       COALESCE((SELECT TRIM(COALESCE(du.last_name,'')||' '||COALESCE(du.name,''))
		                 FROM plans_deputy d JOIN users du ON du.id=d.deputy_user_id
		                 WHERE d.principal_user_id=jp.holder_user_id AND d.stage_code='' LIMIT 1),''),
		       COALESCE((SELECT COUNT(*) FROM plans_deputy d
		                 WHERE d.principal_user_id=jp.holder_user_id AND d.stage_code<>''),0)
		FROM plans_job_position jp
		LEFT JOIN users u ON u.id=jp.holder_user_id
		ORDER BY jp.sort_order, jp.title`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := make([]JobPosition, 0)
	for rows.Next() {
		var p JobPosition
		if err := rows.Scan(&p.ID, &p.Title, &p.Kind, &p.HolderUserID, &p.HolderName, &p.Description,
			&p.CfoCount, &p.DeputyName, &p.StageDeputs); err != nil {
			return nil, err
		}
		out = append(out, p)
	}
	return out, rows.Err()
}

// Upsert создаёт (id=0) или обновляет должность.
func (s *JobPositionStore) Upsert(ctx context.Context, id int64, title, kind string, holderUserID int64, desc string) (int64, error) {
	var holder *int64
	if holderUserID != 0 {
		holder = &holderUserID
	}
	if kind == "" {
		kind = "regular"
	}
	if id == 0 {
		var newID int64
		err := s.pool.QueryRow(ctx, `
			INSERT INTO plans_job_position (title, kind, holder_user_id, description)
			VALUES ($1,$2,$3,$4) RETURNING id`, title, kind, holder, desc).Scan(&newID)
		return newID, err
	}
	_, err := s.pool.Exec(ctx, `
		UPDATE plans_job_position SET title=$2, kind=$3, holder_user_id=$4, description=$5 WHERE id=$1`,
		id, title, kind, holder, desc)
	return id, err
}

// Delete удаляет должность (снимает покрытие ЦФО каскадом).
func (s *JobPositionStore) Delete(ctx context.Context, id int64) error {
	_, err := s.pool.Exec(ctx, `DELETE FROM plans_job_position WHERE id=$1`, id)
	return err
}

// CfoCodes — коды ЦФО, покрытые должностью.
func (s *JobPositionStore) CfoCodes(ctx context.Context, positionID int64) ([]string, error) {
	rows, err := s.pool.Query(ctx, `SELECT code_cfo FROM plans_cfo_position WHERE position_id=$1 ORDER BY code_cfo`, positionID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := make([]string, 0)
	for rows.Next() {
		var c string
		if rows.Scan(&c) == nil {
			out = append(out, c)
		}
	}
	return out, rows.Err()
}

// AssignCfo привязывает коды ЦФО к должности. ЗАПРЕТ ПЕРЕСЕЧЕНИЯ: если ЦФО уже
// закреплён за ДРУГОЙ должностью — отказ (сначала снять с прежней). Один ЦФО — один ТОП.
func (s *JobPositionStore) AssignCfo(ctx context.Context, positionID int64, codes []string) error {
	if len(codes) == 0 {
		return nil
	}
	// Конфликты: те же коды на других должностях.
	confRows, err := s.pool.Query(ctx, `
		SELECT cp.code_cfo, jp.title FROM plans_cfo_position cp
		JOIN plans_job_position jp ON jp.id = cp.position_id
		WHERE cp.code_cfo = ANY($1) AND cp.position_id <> $2`, codes, positionID)
	if err == nil {
		conflicts := []string{}
		for confRows.Next() {
			var code, title string
			if confRows.Scan(&code, &title) == nil {
				conflicts = append(conflicts, fmt.Sprintf("ЦФО %s → «%s»", code, title))
			}
		}
		confRows.Close()
		if len(conflicts) > 0 {
			return fmt.Errorf("пересечение ЦФО (уже закреплены): %s. Снимите их с прежней должности.", strings.Join(conflicts, "; "))
		}
	}
	batch := &pgx.Batch{}
	for _, c := range codes {
		c = strings.TrimSpace(c)
		if c == "" {
			continue
		}
		batch.Queue(`
			INSERT INTO plans_cfo_position (code_cfo, position_id, updated_at)
			VALUES ($1,$2,NOW())
			ON CONFLICT (code_cfo) DO UPDATE SET position_id=$2, updated_at=NOW()`, c, positionID)
	}
	br := s.pool.SendBatch(ctx, batch)
	defer br.Close()
	for range codes {
		if _, err := br.Exec(); err != nil {
			return err
		}
	}
	return nil
}

// UnassignCfo снимает покрытие ЦФО с должности.
func (s *JobPositionStore) UnassignCfo(ctx context.Context, code string) error {
	_, err := s.pool.Exec(ctx, `DELETE FROM plans_cfo_position WHERE code_cfo=$1`, code)
	return err
}

// CfoFilter — критерии массового назначения по разрезам dir_cfo (пустые игнорируются).
// Segment (large|small) — сегмент МП из dir_marketplace (для разделения задач МП).
type CfoFilter struct {
	EntityType  string `json:"entity_type"`
	GroupCFO1   string `json:"group_cfo1"`
	GroupCFO2   string `json:"group_cfo2"`
	Country     string `json:"country"`
	LegalEntity string `json:"legal_entity"`
	Segment     string `json:"segment"`
}

// AssignByFilter привязывает к должности все ЦФО dir_cfo, подходящие под фильтр.
// Возвращает число затронутых.
func (s *JobPositionStore) AssignByFilter(ctx context.Context, positionID int64, f CfoFilter) (int, error) {
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
	if f.Segment != "" {
		args = append(args, f.Segment)
		conds = append(conds, fmt.Sprintf(`r.external_id IN (
			SELECT mp.payload_json->>'code_cfo' FROM plans_directory_row mp
			JOIN plans_directory dm ON dm.id=mp.directory_id
			WHERE dm.code='dir_marketplace' AND mp.payload_json->>'segment'=$%d)`, len(args)))
	}
	if len(args) == 0 {
		return 0, fmt.Errorf("укажите хотя бы один критерий фильтра")
	}
	rows, err := s.pool.Query(ctx, `
		SELECT COALESCE(r.external_id,'')
		FROM plans_directory_row r JOIN plans_directory d ON d.id=r.directory_id
		WHERE `+strings.Join(conds, " AND "), args...)
	if err != nil {
		return 0, err
	}
	var codes []string
	for rows.Next() {
		var c string
		if rows.Scan(&c) == nil && c != "" {
			codes = append(codes, c)
		}
	}
	rows.Close()
	if err := s.AssignCfo(ctx, positionID, codes); err != nil {
		return 0, err
	}
	return len(codes), nil
}

// cfoTop — ТОП ЦФО (имя носителя + должность) для оверлея dir_cfo.
type cfoTop struct{ Title, Holder string }

// TopByCfo — карта code_cfo → {должность, носитель} (живая связь).
func (s *JobPositionStore) TopByCfo(ctx context.Context) (map[string]cfoTop, error) {
	rows, err := s.pool.Query(ctx, `
		SELECT cp.code_cfo, jp.title, TRIM(COALESCE(u.last_name,'')||' '||COALESCE(u.name,''))
		FROM plans_cfo_position cp
		JOIN plans_job_position jp ON jp.id=cp.position_id
		LEFT JOIN users u ON u.id=jp.holder_user_id`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := map[string]cfoTop{}
	for rows.Next() {
		var code, title, holder string
		if rows.Scan(&code, &title, &holder) == nil {
			out[code] = cfoTop{Title: title, Holder: holder}
		}
	}
	return out, rows.Err()
}
