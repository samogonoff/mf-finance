package plans

import (
	"context"
	"encoding/json"

	"github.com/jackc/pgx/v5/pgxpool"
)

// pgScopeStore — pgx-реализация ScopeStore (таблица plans_user_scope).
type pgScopeStore struct{ pool *pgxpool.Pool }

// NewPgScopeStore — конструктор.
func NewPgScopeStore(pool *pgxpool.Pool) *pgScopeStore { return &pgScopeStore{pool: pool} }

func (s *pgScopeStore) UserCodeCFOs(ctx context.Context, userID int64) ([]int, error) {
	rows, err := s.pool.Query(ctx, `SELECT code_cfo FROM plans_user_scope WHERE user_id = $1`, userID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	seen := map[int]struct{}{}
	out := make([]int, 0)
	for rows.Next() {
		var raw []byte
		if err := rows.Scan(&raw); err != nil {
			return nil, err
		}
		var codes []int
		if err := json.Unmarshal(raw, &codes); err != nil {
			return nil, err
		}
		for _, c := range codes {
			if _, ok := seen[c]; !ok {
				seen[c] = struct{}{}
				out = append(out, c)
			}
		}
	}
	return out, rows.Err()
}

func (s *pgScopeStore) UpsertScope(ctx context.Context, sc UserScope) error {
	codes, err := json.Marshal(sc.CodeCFO)
	if err != nil {
		return err
	}
	_, err = s.pool.Exec(ctx, `
		INSERT INTO plans_user_scope (user_id, role, stage_code, country, legal_entity, code_cfo, updated_at)
		VALUES ($1, $2, $3, $4, $5, $6, NOW())
		ON CONFLICT (user_id, role, stage_code, country, legal_entity)
		DO UPDATE SET code_cfo = EXCLUDED.code_cfo, updated_at = NOW()`,
		sc.UserID, sc.Role, sc.StageCode, sc.Country, sc.LegalEntity, codes)
	return err
}
