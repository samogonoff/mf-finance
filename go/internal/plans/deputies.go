package plans

import (
	"context"

	"github.com/jackc/pgx/v5/pgxpool"
)

// Заместители (замы). Глобальный зам (stage_code='') подхватывает весь функционал
// отсутствующего; зам в разрезе этапа — только по конкретному этапу. Активируется,
// когда у principal проставлена метка отсутствия (users.absence_status, позже из B24).

// Deputy — запись о заместителе с именами обеих сторон (для UI).
type Deputy struct {
	ID            int64  `json:"id"`
	PrincipalID   int64  `json:"principal_user_id"`
	PrincipalName string `json:"principal_name"`
	DeputyID      int64  `json:"deputy_user_id"`
	DeputyName    string `json:"deputy_name"`
	StageCode     string `json:"stage_code"` // ''=глобально
	Note          string `json:"note"`
}

// DeputyStore — CRUD заместителей.
type DeputyStore struct{ pool *pgxpool.Pool }

// NewDeputyStore — конструктор.
func NewDeputyStore(pool *pgxpool.Pool) *DeputyStore { return &DeputyStore{pool: pool} }

const deputySelect = `
SELECT d.id, d.principal_user_id,
       TRIM(COALESCE(pu.last_name,'') || ' ' || COALESCE(pu.name,'')),
       d.deputy_user_id,
       TRIM(COALESCE(du.last_name,'') || ' ' || COALESCE(du.name,'')),
       d.stage_code, d.note
FROM plans_deputy d
JOIN users pu ON pu.id = d.principal_user_id
JOIN users du ON du.id = d.deputy_user_id`

func scanDeputies(rows interface {
	Next() bool
	Scan(...any) error
	Err() error
}) ([]Deputy, error) {
	out := make([]Deputy, 0)
	for rows.Next() {
		var d Deputy
		if err := rows.Scan(&d.ID, &d.PrincipalID, &d.PrincipalName, &d.DeputyID, &d.DeputyName, &d.StageCode, &d.Note); err != nil {
			return nil, err
		}
		out = append(out, d)
	}
	return out, rows.Err()
}

// List — все замы (для страницы прав).
func (s *DeputyStore) List(ctx context.Context) ([]Deputy, error) {
	rows, err := s.pool.Query(ctx, deputySelect+` ORDER BY pu.last_name, d.stage_code`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return scanDeputies(rows)
}

// Upsert назначает/меняет зама (глобально или по этапу). UNIQUE(principal, stage).
func (s *DeputyStore) Upsert(ctx context.Context, principal, deputy int64, stage, note string, by int64) error {
	_, err := s.pool.Exec(ctx, `
		INSERT INTO plans_deputy (principal_user_id, deputy_user_id, stage_code, note, updated_by, updated_at)
		VALUES ($1,$2,$3,$4,$5,NOW())
		ON CONFLICT (principal_user_id, stage_code)
		DO UPDATE SET deputy_user_id=EXCLUDED.deputy_user_id, note=EXCLUDED.note,
		              updated_by=EXCLUDED.updated_by, updated_at=NOW()`,
		principal, deputy, stage, note, by)
	return err
}

// Delete снимает зама.
func (s *DeputyStore) Delete(ctx context.Context, id int64) error {
	_, err := s.pool.Exec(ctx, `DELETE FROM plans_deputy WHERE id=$1`, id)
	return err
}

// ActiveDeputy возвращает id активного зама для отсутствующего пользователя на
// этапе: сперва per-stage, затем глобальный; 0 если зама нет. Используется
// движком процесса для авто-подмены функционала (когда principal отсутствует).
func (s *DeputyStore) ActiveDeputy(ctx context.Context, principal int64, stage string) (int64, error) {
	var id int64
	err := s.pool.QueryRow(ctx, `
		SELECT deputy_user_id FROM plans_deputy
		WHERE principal_user_id=$1 AND stage_code IN ($2, '')
		ORDER BY (stage_code = '') ASC
		LIMIT 1`, principal, stage).Scan(&id)
	if err != nil {
		return 0, nil // нет зама — не ошибка
	}
	return id, nil
}
