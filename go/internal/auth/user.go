package auth

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// User — финансовый пользователь. Поля копируем из MP, но без MP-специфики.
type User struct {
	ID                      int64     `json:"id"`
	Email                   string    `json:"email"`
	Name                    string    `json:"name"`
	LastName                string    `json:"-"`
	Roles                   []string  `json:"roles"`
	B24ID                   *int64    `json:"-"`
	B24Domain               *string   `json:"-"`
	IsBlocked               bool      `json:"-"`
	WelcomeNotificationSent bool      `json:"-"`
	NotifyViaB24            bool      `json:"-"`
	CreatedAt               time.Time `json:"-"`
	UpdatedAt               time.Time `json:"-"`
}

func (u User) DisplayName() string {
	if u.Name == "" && u.LastName == "" {
		return u.Email
	}
	if u.LastName == "" {
		return u.Name
	}
	return u.Name + " " + u.LastName
}

// UserRepo инкапсулирует работу с таблицей users (см. миграции 0001/0002/0005).
type UserRepo struct{ pool *pgxpool.Pool }

func NewUserRepo(p *pgxpool.Pool) *UserRepo { return &UserRepo{pool: p} }

const userColumns = `id, email, name, last_name, roles, b24_id, b24_domain,
	is_blocked, welcome_notification_sent, notify_via_b24, created_at, updated_at`

func scanUser(row pgx.Row) (*User, error) {
	var u User
	var rolesJSON []byte
	err := row.Scan(&u.ID, &u.Email, &u.Name, &u.LastName, &rolesJSON, &u.B24ID, &u.B24Domain,
		&u.IsBlocked, &u.WelcomeNotificationSent, &u.NotifyViaB24, &u.CreatedAt, &u.UpdatedAt)
	if err != nil {
		return nil, err
	}
	_ = json.Unmarshal(rolesJSON, &u.Roles)
	if u.Roles == nil {
		u.Roles = []string{}
	}
	return &u, nil
}

// FindOrCreateByB24 — атомарный upsert по email. Новый юзер создаётся с пустым
// набором прикладных ролей; ROLE_USER добавляется на выдаче токена через
// ExpandRoles. Прикладные роли (ROLE_COST_USER и т.п.) присваивает админ.
func (r *UserRepo) FindOrCreateByB24(ctx context.Context, in B24CallbackPayload) (*User, error) {
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback(ctx)

	row := tx.QueryRow(ctx, `SELECT `+userColumns+` FROM users
		WHERE lower(email) = lower($1) LIMIT 1`, in.Email)
	u, err := scanUser(row)

	switch {
	case errors.Is(err, pgx.ErrNoRows):
		rb, _ := json.Marshal([]string{})
		var id int64
		err = tx.QueryRow(ctx, `INSERT INTO users(email, name, last_name, roles, b24_id, b24_domain, password)
			VALUES ($1, $2, $3, $4, $5, $6, '') RETURNING id`,
			in.Email, in.Name, in.LastName, rb, in.B24ID, in.B24Domain).Scan(&id)
		if err != nil {
			return nil, err
		}
		// Перечитываем — нужны дефолты колонок (notify_via_b24=true, created_at и т.п.).
		u, err = scanUser(tx.QueryRow(ctx, `SELECT `+userColumns+` FROM users WHERE id=$1`, id))
		if err != nil {
			return nil, err
		}
	case err != nil:
		return nil, err
	default:
		_, err = tx.Exec(ctx, `UPDATE users SET name=$2, last_name=$3, b24_id=$4, b24_domain=$5, updated_at=NOW()
			WHERE id=$1`, u.ID, in.Name, in.LastName, in.B24ID, in.B24Domain)
		if err != nil {
			return nil, err
		}
		u.Name = in.Name
		u.LastName = in.LastName
		u.B24ID = in.B24ID
		u.B24Domain = strPtr(in.B24Domain)
	}

	if u.IsBlocked {
		return nil, errors.New("user is blocked")
	}
	return u, tx.Commit(ctx)
}

func (r *UserRepo) ByID(ctx context.Context, id int64) (*User, error) {
	return scanUser(r.pool.QueryRow(ctx, `SELECT `+userColumns+` FROM users WHERE id=$1`, id))
}

// ListFilter — фильтры админского списка пользователей.
type ListFilter struct {
	Email     string
	Name      string
	IsBlocked *bool
	Limit     int
	Offset    int
}

// List возвращает страницу пользователей и общее число записей под фильтром.
func (r *UserRepo) List(ctx context.Context, f ListFilter) ([]User, int, error) {
	conds := []string{"1=1"}
	args := []any{}
	if f.Email != "" {
		args = append(args, "%"+strings.ToLower(f.Email)+"%")
		conds = append(conds, "lower(email) LIKE $"+itoa(len(args)))
	}
	if f.Name != "" {
		args = append(args, "%"+strings.ToLower(f.Name)+"%")
		i := itoa(len(args))
		conds = append(conds, "(lower(name) LIKE $"+i+" OR lower(last_name) LIKE $"+i+")")
	}
	if f.IsBlocked != nil {
		args = append(args, *f.IsBlocked)
		conds = append(conds, "is_blocked = $"+itoa(len(args)))
	}
	where := strings.Join(conds, " AND ")

	var total int
	if err := r.pool.QueryRow(ctx, `SELECT COUNT(*) FROM users WHERE `+where, args...).Scan(&total); err != nil {
		return nil, 0, err
	}

	limit := f.Limit
	if limit <= 0 || limit > 200 {
		limit = 50
	}
	args = append(args, limit, f.Offset)
	q := `SELECT ` + userColumns + ` FROM users WHERE ` + where +
		` ORDER BY id DESC LIMIT $` + itoa(len(args)-1) + ` OFFSET $` + itoa(len(args))

	rows, err := r.pool.Query(ctx, q, args...)
	if err != nil {
		return nil, 0, err
	}
	defer rows.Close()

	users := make([]User, 0, limit)
	for rows.Next() {
		u, err := scanUser(rows)
		if err != nil {
			return nil, 0, err
		}
		users = append(users, *u)
	}
	return users, total, rows.Err()
}

// UpdateRoles — переписывает прикладные роли пользователя. Вход должен быть
// уже отфильтрован FilterAllowed.
func (r *UserRepo) UpdateRoles(ctx context.Context, id int64, roles []string) error {
	rb, _ := json.Marshal(roles)
	_, err := r.pool.Exec(ctx, `UPDATE users SET roles=$2, updated_at=NOW() WHERE id=$1`, id, rb)
	return err
}

// SetBlocked — выставляет/снимает флаг блокировки.
func (r *UserRepo) SetBlocked(ctx context.Context, id int64, blocked bool) error {
	_, err := r.pool.Exec(ctx, `UPDATE users SET is_blocked=$2, updated_at=NOW() WHERE id=$1`, id, blocked)
	return err
}

// SetWelcomeSent — выставляет welcome_notification_sent=TRUE.
// Вызывается из notifications.Service.CreateWelcome после успешного создания.
func (r *UserRepo) SetWelcomeSent(ctx context.Context, id int64) error {
	_, err := r.pool.Exec(ctx, `UPDATE users SET welcome_notification_sent=TRUE WHERE id=$1`, id)
	return err
}

// UpdateNotifyViaB24 — переключает per-user флаг дублирования в Б24.
func (r *UserRepo) UpdateNotifyViaB24(ctx context.Context, id int64, on bool) error {
	_, err := r.pool.Exec(ctx, `UPDATE users SET notify_via_b24=$2, updated_at=NOW() WHERE id=$1`, id, on)
	return err
}

func strPtr(s string) *string {
	if s == "" {
		return nil
	}
	return &s
}

// itoa — мини-helper для построения $N плейсхолдеров без зависимости от strconv-аллокаций.
func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	var buf [12]byte
	i := len(buf)
	for n > 0 {
		i--
		buf[i] = byte('0' + n%10)
		n /= 10
	}
	return string(buf[i:])
}
