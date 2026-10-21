package plans

import (
	"context"
	"errors"
	"strings"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

type pgLeAdjust struct{ pool *pgxpool.Pool }

// NewLeAdjustRepo — хранилище корректировок свода по ЮЛ в PG.
func NewLeAdjustRepo(pool *pgxpool.Pool) LeAdjustRepo { return &pgLeAdjust{pool: pool} }

func (r *pgLeAdjust) List(ctx context.Context, plID int64) ([]LeAdjust, error) {
	rows, err := r.pool.Query(ctx, `
SELECT a.id, a.pl_id, a.legal_entity, a.line_key, a.amount_byn::float8, a.reason, a.user_id,
       TRIM(COALESCE(u.last_name,'')||' '||COALESCE(u.name,'')), a.created_at, a.revoked, a.revoked_at, a.revoke_reason
  FROM pl_le_adjust a
  LEFT JOIN users u ON u.id = a.user_id
 WHERE a.pl_id = $1
 ORDER BY a.id`, plID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []LeAdjust
	for rows.Next() {
		var a LeAdjust
		if err := rows.Scan(&a.ID, &a.PlID, &a.LegalEntity, &a.LineKey, &a.AmountBYN, &a.Reason, &a.UserID,
			&a.UserName, &a.CreatedAt, &a.Revoked, &a.RevokedAt, &a.RevokeNote); err != nil {
			return nil, err
		}
		out = append(out, a)
	}
	return out, rows.Err()
}

func (r *pgLeAdjust) Add(ctx context.Context, a LeAdjust) (int64, error) {
	var id int64
	err := r.pool.QueryRow(ctx, `
INSERT INTO pl_le_adjust (pl_id, legal_entity, line_key, amount_byn, reason, user_id)
VALUES ($1,$2,$3,$4,$5,$6) RETURNING id`,
		a.PlID, a.LegalEntity, a.LineKey, a.AmountBYN, a.Reason, a.UserID).Scan(&id)
	return id, err
}

func (r *pgLeAdjust) Revoke(ctx context.Context, plID, id, userID int64, reason string) (LeAdjust, error) {
	var a LeAdjust
	err := r.pool.QueryRow(ctx, `
UPDATE pl_le_adjust SET revoked = TRUE, revoked_by = $3, revoked_at = NOW(), revoke_reason = $4
 WHERE id = $2 AND pl_id = $1 AND NOT revoked
 RETURNING id, pl_id, legal_entity, line_key, amount_byn::float8, reason, user_id, created_at, revoked`,
		plID, id, userID, strings.TrimSpace(reason)).
		Scan(&a.ID, &a.PlID, &a.LegalEntity, &a.LineKey, &a.AmountBYN, &a.Reason, &a.UserID, &a.CreatedAt, &a.Revoked)
	if errors.Is(err, pgx.ErrNoRows) {
		return LeAdjust{}, errors.New("корректировка не найдена или уже снята")
	}
	return a, err
}
