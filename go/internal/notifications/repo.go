package notifications

import (
	"context"
	"encoding/json"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

type Repo struct{ pool *pgxpool.Pool }

func NewRepo(p *pgxpool.Pool) *Repo { return &Repo{pool: p} }

const cols = `id, user_id, title, message, type, is_read, created_at, read_at, data, object_type`

func scan(row pgx.Row) (*Notification, error) {
	var n Notification
	var dataJSON []byte
	var objType *string
	if err := row.Scan(&n.ID, &n.UserID, &n.Title, &n.Message, &n.Type, &n.IsRead,
		&n.CreatedAt, &n.ReadAt, &dataJSON, &objType); err != nil {
		return nil, err
	}
	if len(dataJSON) > 0 {
		_ = json.Unmarshal(dataJSON, &n.Data)
	}
	if n.Data == nil {
		n.Data = map[string]any{}
	}
	if objType != nil {
		n.ObjectType = *objType
	}
	return &n, nil
}

// Create — INSERT, возвращает созданное уведомление целиком (нужно сервису
// доставки в Б24, чтобы знать id и created_at).
func (r *Repo) Create(ctx context.Context, in Input) (*Notification, error) {
	if in.Type == "" {
		in.Type = TypeInfo
	}
	if in.Data == nil {
		in.Data = map[string]any{}
	}
	data, _ := json.Marshal(in.Data)
	var objType any
	if in.ObjectType != "" {
		objType = in.ObjectType
	}
	row := r.pool.QueryRow(ctx, `
		INSERT INTO notifications (user_id, title, message, type, data, object_type)
		VALUES ($1, $2, $3, $4, $5, $6)
		RETURNING `+cols,
		in.UserID, in.Title, in.Message, in.Type, data, objType)
	return scan(row)
}

// FindByUserPaginated — возвращает страницу уведомлений и общее число записей.
func (r *Repo) FindByUserPaginated(ctx context.Context, userID int64, unreadOnly bool, limit, offset int) ([]Notification, int, error) {
	if limit <= 0 || limit > 200 {
		limit = 50
	}
	cond := `user_id = $1`
	args := []any{userID}
	if unreadOnly {
		cond += ` AND is_read = FALSE`
	}

	var total int
	if err := r.pool.QueryRow(ctx, `SELECT COUNT(*) FROM notifications WHERE `+cond, args...).Scan(&total); err != nil {
		return nil, 0, err
	}

	args = append(args, limit, offset)
	q := `SELECT ` + cols + ` FROM notifications WHERE ` + cond +
		` ORDER BY created_at DESC LIMIT $2 OFFSET $3`
	rows, err := r.pool.Query(ctx, q, args...)
	if err != nil {
		return nil, 0, err
	}
	defer rows.Close()

	out := make([]Notification, 0, limit)
	for rows.Next() {
		n, err := scan(rows)
		if err != nil {
			return nil, 0, err
		}
		out = append(out, *n)
	}
	return out, total, rows.Err()
}

func (r *Repo) CountUnread(ctx context.Context, userID int64) (int, error) {
	var n int
	err := r.pool.QueryRow(ctx,
		`SELECT COUNT(*) FROM notifications WHERE user_id=$1 AND is_read=FALSE`, userID).Scan(&n)
	return n, err
}

// MarkAsRead — пометить одно своё уведомление. user_id в WHERE — защита от чужих id.
func (r *Repo) MarkAsRead(ctx context.Context, id, userID int64) error {
	_, err := r.pool.Exec(ctx,
		`UPDATE notifications SET is_read=TRUE, read_at=NOW()
		 WHERE id=$1 AND user_id=$2 AND is_read=FALSE`, id, userID)
	return err
}

func (r *Repo) MarkAllAsRead(ctx context.Context, userID int64) error {
	_, err := r.pool.Exec(ctx,
		`UPDATE notifications SET is_read=TRUE, read_at=NOW()
		 WHERE user_id=$1 AND is_read=FALSE`, userID)
	return err
}

// UpdateB24Status — фиксирует результат попытки доставки в Б24.
// Вызывается фоновой goroutine'ой в service.deliverToB24.
func (r *Repo) UpdateB24Status(ctx context.Context, id int64, ok bool, errMsg string) error {
	if ok {
		_, err := r.pool.Exec(ctx, `
			UPDATE notifications
			SET b24_sent_at = NOW(), b24_attempts = b24_attempts + 1, b24_last_error = NULL
			WHERE id=$1`, id)
		return err
	}
	_, err := r.pool.Exec(ctx, `
		UPDATE notifications
		SET b24_attempts = b24_attempts + 1, b24_last_error = $2
		WHERE id=$1`, id, errMsg)
	return err
}
