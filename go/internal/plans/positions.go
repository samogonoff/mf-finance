package plans

import (
	"context"
	"encoding/json"

	"github.com/jackc/pgx/v5/pgxpool"
)

func unmarshalCodes(raw []byte, dst *[]int) error {
	if len(raw) == 0 {
		return nil
	}
	return json.Unmarshal(raw, dst)
}

// Каталог назначаемых должностей (ТЗ §«Разграничение прав»: системные роли —
// в auth/roles.go; назначаемые должности процесса — здесь, редактируемый
// справочник). Должность + ABAC-срез (этап×ЦФО×страна×ЮЛ) живут в
// plans_user_scope; этот каталог даёт список доступных должностей для выбора.

// Position — назначаемая должность процесса (привязана к этапам).
type Position struct {
	Code             string   `json:"code"`
	Name             string   `json:"name"`
	Description      string   `json:"description"`
	DefaultStageCode string   `json:"default_stage_code"`
	Track            string   `json:"track"`
	Kind             string   `json:"kind"` // filler|approver|coordinator|observer
	SortOrder        int      `json:"sort_order"`
	StageCodes       []string `json:"stage_codes"` // этапы процесса (plans_position_stage)
}

// PositionStore — CRUD каталога должностей.
type PositionStore struct{ pool *pgxpool.Pool }

// NewPositionStore — конструктор.
func NewPositionStore(pool *pgxpool.Pool) *PositionStore { return &PositionStore{pool: pool} }

// List — все должности по порядку отображения, с привязанными этапами.
func (s *PositionStore) List(ctx context.Context) ([]Position, error) {
	rows, err := s.pool.Query(ctx, `
		SELECT code, name, description, default_stage_code, track, COALESCE(kind,'filler'), sort_order
		FROM plans_position ORDER BY sort_order, code`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := make([]Position, 0)
	idx := map[string]int{}
	for rows.Next() {
		var p Position
		if err := rows.Scan(&p.Code, &p.Name, &p.Description, &p.DefaultStageCode, &p.Track, &p.Kind, &p.SortOrder); err != nil {
			return nil, err
		}
		p.StageCodes = []string{}
		idx[p.Code] = len(out)
		out = append(out, p)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	// Этапы должностей.
	sr, err := s.pool.Query(ctx, `SELECT position_code, stage_code FROM plans_position_stage ORDER BY stage_code`)
	if err != nil {
		return out, nil // должности без этапов — не фатально
	}
	defer sr.Close()
	for sr.Next() {
		var code, stage string
		if err := sr.Scan(&code, &stage); err != nil {
			return out, nil
		}
		if i, ok := idx[code]; ok {
			out[i].StageCodes = append(out[i].StageCodes, stage)
		}
	}
	return out, nil
}

// SetStages заменяет набор этапов должности (привязка к процессу).
func (s *PositionStore) SetStages(ctx context.Context, code string, stages []string) error {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	if _, err := tx.Exec(ctx, `DELETE FROM plans_position_stage WHERE position_code=$1`, code); err != nil {
		return err
	}
	for _, st := range stages {
		if st == "" {
			continue
		}
		if _, err := tx.Exec(ctx, `INSERT INTO plans_position_stage (position_code, stage_code) VALUES ($1,$2) ON CONFLICT DO NOTHING`, code, st); err != nil {
			return err
		}
	}
	return tx.Commit(ctx)
}

// Upsert создаёт/обновляет должность.
func (s *PositionStore) Upsert(ctx context.Context, p Position) error {
	kind := p.Kind
	if kind == "" {
		kind = "filler"
	}
	_, err := s.pool.Exec(ctx, `
		INSERT INTO plans_position (code, name, description, default_stage_code, track, kind, sort_order)
		VALUES ($1,$2,$3,$4,$5,$6,$7)
		ON CONFLICT (code) DO UPDATE SET
			name=EXCLUDED.name, description=EXCLUDED.description,
			default_stage_code=EXCLUDED.default_stage_code, track=EXCLUDED.track,
			kind=EXCLUDED.kind, sort_order=EXCLUDED.sort_order`,
		p.Code, p.Name, p.Description, p.DefaultStageCode, p.Track, kind, p.SortOrder)
	return err
}

// Delete удаляет должность из каталога.
func (s *PositionStore) Delete(ctx context.Context, code string) error {
	_, err := s.pool.Exec(ctx, `DELETE FROM plans_position WHERE code=$1`, code)
	return err
}

// --- Листинг назначений (для страницы «Пользователи и права») ---

// ScopeAssignment — назначение должности пользователю со срезом + имя/почта.
type ScopeAssignment struct {
	ID          int64  `json:"id"`
	UserID      int64  `json:"user_id"`
	UserName    string `json:"user_name"`
	UserEmail   string `json:"user_email"`
	Role        string `json:"role"`         // = Position.Code
	StageCode   string `json:"stage_code"`
	Country     string `json:"country"`
	LegalEntity string `json:"legal_entity"`
	CodeCFO     []int  `json:"code_cfo"`
}

// ListAssignments возвращает все назначения должностей со срезами (джойн users).
func (s *pgScopeStore) ListAssignments(ctx context.Context) ([]ScopeAssignment, error) {
	rows, err := s.pool.Query(ctx, `
		SELECT s.id, s.user_id, COALESCE(u.name,'') || ' ' || COALESCE(u.last_name,''),
		       COALESCE(u.email,''), s.role, s.stage_code, s.country, s.legal_entity, s.code_cfo
		FROM plans_user_scope s
		JOIN users u ON u.id = s.user_id
		ORDER BY u.last_name, u.name, s.role`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := make([]ScopeAssignment, 0)
	for rows.Next() {
		var a ScopeAssignment
		var raw []byte
		if err := rows.Scan(&a.ID, &a.UserID, &a.UserName, &a.UserEmail, &a.Role,
			&a.StageCode, &a.Country, &a.LegalEntity, &raw); err != nil {
			return nil, err
		}
		_ = unmarshalCodes(raw, &a.CodeCFO)
		out = append(out, a)
	}
	return out, rows.Err()
}

// DeleteAssignment удаляет одно назначение по id.
func (s *pgScopeStore) DeleteAssignment(ctx context.Context, id int64) error {
	_, err := s.pool.Exec(ctx, `DELETE FROM plans_user_scope WHERE id=$1`, id)
	return err
}
