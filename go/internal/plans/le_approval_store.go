package plans

import (
	"context"
	"errors"
	"time"

	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
)

type pgLeApproval struct{ pool *pgxpool.Pool }

// NewLeApprovalRepo — хранилище списков согласующих и решений этапов 3 и 4.
func NewLeApprovalRepo(pool *pgxpool.Pool) LeApprovalRepo { return &pgLeApproval{pool: pool} }

const leUserName = `TRIM(COALESCE(u.last_name,'')||' '||COALESCE(u.name,''))`

func (r *pgLeApproval) Approvers(ctx context.Context) ([]LeApprover, error) {
	rows, err := r.pool.Query(ctx, `
SELECT a.stage_code, a.legal_entity, a.user_id, `+leUserName+`
  FROM pl_final_approver a LEFT JOIN users u ON u.id = a.user_id
 ORDER BY a.stage_code, a.legal_entity, a.user_id`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []LeApprover
	for rows.Next() {
		var a LeApprover
		if err := rows.Scan(&a.StageCode, &a.LegalEntity, &a.UserID, &a.UserName); err != nil {
			return nil, err
		}
		out = append(out, a)
	}
	return out, rows.Err()
}

func (r *pgLeApproval) Policies(ctx context.Context) ([]LePolicy, error) {
	rows, err := r.pool.Query(ctx, `SELECT stage_code, legal_entity, require_all FROM pl_final_policy ORDER BY stage_code, legal_entity`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []LePolicy
	for rows.Next() {
		var p LePolicy
		if err := rows.Scan(&p.StageCode, &p.LegalEntity, &p.RequireAll); err != nil {
			return nil, err
		}
		out = append(out, p)
	}
	return out, rows.Err()
}

func (r *pgLeApproval) SetApprovers(ctx context.Context, stage, legal string, userIDs []int64, requireAll bool, actor int64) error {
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	if _, err := tx.Exec(ctx, `DELETE FROM pl_final_approver WHERE stage_code=$1 AND legal_entity=$2`, stage, legal); err != nil {
		return err
	}
	for _, id := range userIDs {
		if _, err := tx.Exec(ctx, `INSERT INTO pl_final_approver (stage_code, legal_entity, user_id, created_by) VALUES ($1,$2,$3,$4)`,
			stage, legal, id, actor); err != nil {
			return err
		}
	}
	if _, err := tx.Exec(ctx, `
INSERT INTO pl_final_policy (stage_code, legal_entity, require_all, updated_by) VALUES ($1,$2,$3,$4)
ON CONFLICT (stage_code, legal_entity) DO UPDATE SET require_all = EXCLUDED.require_all, updated_by = EXCLUDED.updated_by, updated_at = NOW()`,
		stage, legal, requireAll, actor); err != nil {
		return err
	}
	return tx.Commit(ctx)
}

func (r *pgLeApproval) Decisions(ctx context.Context, plID int64) ([]LeDecision, error) {
	rows, err := r.pool.Query(ctx, `
SELECT d.id, d.pl_id, d.stage_code, d.legal_entity, d.user_id, `+leUserName+`, d.decision, d.comment, d.created_at, d.revoked, d.revoke_reason
  FROM pl_final_decision d LEFT JOIN users u ON u.id = d.user_id
 WHERE d.pl_id = $1 ORDER BY d.id`, plID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []LeDecision
	for rows.Next() {
		var d LeDecision
		if err := rows.Scan(&d.ID, &d.PlID, &d.StageCode, &d.LegalEntity, &d.UserID, &d.UserName, &d.Decision, &d.Comment, &d.CreatedAt, &d.Revoked, &d.RevokeNote); err != nil {
			return nil, err
		}
		out = append(out, d)
	}
	return out, rows.Err()
}

func (r *pgLeApproval) AddDecision(ctx context.Context, d LeDecision) (int64, error) {
	var id int64
	// Повторное согласие того же человека заменяет предыдущее активное, чтобы в журнале не копились дубли.
	if d.Decision == LeApprove {
		if _, err := r.pool.Exec(ctx, `
UPDATE pl_final_decision SET revoked = TRUE, revoked_at = NOW(), revoke_reason = 'заменено новым согласием'
 WHERE pl_id=$1 AND stage_code=$2 AND legal_entity=$3 AND user_id=$4 AND decision='approve' AND NOT revoked`,
			d.PlID, d.StageCode, d.LegalEntity, d.UserID); err != nil {
			return 0, err
		}
	}
	err := r.pool.QueryRow(ctx, `
INSERT INTO pl_final_decision (pl_id, stage_code, legal_entity, user_id, decision, comment)
VALUES ($1,$2,$3,$4,$5,$6) RETURNING id`,
		d.PlID, d.StageCode, d.LegalEntity, d.UserID, d.Decision, d.Comment).Scan(&id)
	return id, err
}

func (r *pgLeApproval) RevokeDecisions(ctx context.Context, plID int64, stage, legal string, allLegals bool, reason string) (int, error) {
	const base = `UPDATE pl_final_decision SET revoked = TRUE, revoked_at = NOW(), revoke_reason = $3
	               WHERE pl_id=$1 AND stage_code=$2 AND decision='approve' AND NOT revoked`
	var tag pgconn.CommandTag
	var err error
	if allLegals {
		tag, err = r.pool.Exec(ctx, base, plID, stage, reason)
	} else {
		tag, err = r.pool.Exec(ctx, base+` AND legal_entity = $4`, plID, stage, reason, legal)
	}
	if err != nil {
		return 0, err
	}
	return int(tag.RowsAffected()), nil
}

// leLockWait — сколько ждём блокировку периода, прежде чем отказать («период занят»).
const leLockWait = 30 * time.Second

// Lock — блокировка периода (advisory, на отдельном соединении) на время операции согласования. Соединение пула во время
// ожидания НЕ удерживается: ожидающие пробуют pg_try_advisory_lock и между попытками возвращают соединение, иначе при
// нескольких согласующих пул заканчивался бы (держатель лока тоже ходит в БД другими соединениями) и вставал весь API.
func (r *pgLeApproval) Lock(ctx context.Context, plID int64) (func(), error) {
	return r.lock(ctx, plID, leLockWait)
}

// LockPatient — Lock без предела ожидания: прерывается только отменой ctx.
func (r *pgLeApproval) LockPatient(ctx context.Context, plID int64) (func(), error) {
	return r.lock(ctx, plID, 0)
}

// lock: wait <= 0 — ждать до отмены ctx.
func (r *pgLeApproval) lock(ctx context.Context, plID int64, wait time.Duration) (func(), error) {
	deadline := time.Now().Add(wait)
	for {
		conn, err := r.pool.Acquire(ctx)
		if err != nil {
			return nil, err
		}
		var got bool
		if err := conn.QueryRow(ctx, `SELECT pg_try_advisory_lock(hashtext('le_approval'), $1::int)`, plID).Scan(&got); err != nil {
			conn.Release()
			return nil, err
		}
		if got {
			return func() {
				bg, cancel := context.WithTimeout(context.Background(), 5*time.Second)
				defer cancel()
				var ok bool
				if err := conn.QueryRow(bg, `SELECT pg_advisory_unlock(hashtext('le_approval'), $1::int)`, plID).Scan(&ok); err != nil || !ok {
					// Не удалось снять лок: соединение с висящим сессионным локом нельзя возвращать в пул — закрываем его
					// (лок исчезнет вместе с сессией), иначе период остался бы заблокирован навсегда.
					_ = conn.Hijack().Close(bg)
					return
				}
				conn.Release()
			}, nil
		}
		conn.Release()
		if wait > 0 && time.Now().After(deadline) {
			return nil, errors.New("период обрабатывается другим пользователем: повторите через несколько секунд")
		}
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-time.After(75 * time.Millisecond):
		}
	}
}

// OpenPeriods — периоды с завершённым этапом 1.6: на них могут решаться этапы 3 и 4.
func (r *pgLeApproval) OpenPeriods(ctx context.Context) ([]int64, error) {
	rows, err := r.pool.Query(ctx, `SELECT pl_id FROM pl_stage_instance WHERE stage_code = '1.6' AND status = 'completed' ORDER BY pl_id`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []int64
	for rows.Next() {
		var id int64
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		out = append(out, id)
	}
	return out, rows.Err()
}
