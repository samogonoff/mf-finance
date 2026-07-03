package debt

import (
	"context"
	"encoding/json"
	"errors"

	"github.com/jackc/pgx/v5/pgxpool"
)

// FiltersRepo — CRUD сохранённых пресетов фильтров (таблица debt_saved_filters).
type FiltersRepo struct{ pool *pgxpool.Pool }

func NewFiltersRepo(p *pgxpool.Pool) *FiltersRepo { return &FiltersRepo{pool: p} }

// List возвращает пресеты пользователя в порядке возрастания имени.
func (r *FiltersRepo) List(ctx context.Context, userID int64) ([]SavedFilter, error) {
	rows, err := r.pool.Query(ctx, `
		SELECT id, user_id, name, payload, created_at, updated_at
		FROM debt_saved_filters
		WHERE user_id = $1
		ORDER BY name ASC`, userID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	out := make([]SavedFilter, 0)
	for rows.Next() {
		var f SavedFilter
		var payload []byte
		if err := rows.Scan(&f.ID, &f.UserID, &f.Name, &payload, &f.CreatedAt, &f.UpdatedAt); err != nil {
			return nil, err
		}
		if err := json.Unmarshal(payload, &f.Payload); err != nil {
			return nil, err
		}
		out = append(out, f)
	}
	return out, rows.Err()
}

// Create вставляет новый пресет.
func (r *FiltersRepo) Create(ctx context.Context, userID int64, name string, payload FilterPayload) (*SavedFilter, error) {
	if name == "" {
		return nil, errors.New("name required")
	}
	pb, err := json.Marshal(payload)
	if err != nil {
		return nil, err
	}
	var f SavedFilter
	f.UserID = userID
	f.Name = name
	f.Payload = payload
	var p []byte
	err = r.pool.QueryRow(ctx, `
		INSERT INTO debt_saved_filters(user_id, name, payload)
		VALUES ($1, $2, $3)
		RETURNING id, payload, created_at, updated_at`,
		userID, name, pb,
	).Scan(&f.ID, &p, &f.CreatedAt, &f.UpdatedAt)
	if err != nil {
		return nil, err
	}
	_ = json.Unmarshal(p, &f.Payload)
	return &f, nil
}

// Delete удаляет пресет, принадлежащий пользователю.
// Если пресет не найден или принадлежит другому юзеру — возвращает ErrNotFound.
var ErrNotFound = errors.New("not found")

func (r *FiltersRepo) Delete(ctx context.Context, userID, id int64) error {
	tag, err := r.pool.Exec(ctx, `DELETE FROM debt_saved_filters WHERE id = $1 AND user_id = $2`, id, userID)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return ErrNotFound
	}
	return nil
}
