package auth

import (
	"context"
	"encoding/json"
	"errors"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// User — финансовый пользователь. Поля копируем из MP, но без MP-специфики.
type User struct {
	ID        int64     `json:"id"`
	Email     string    `json:"email"`
	Name      string    `json:"name"`
	LastName  string    `json:"-"`
	Roles     []string  `json:"roles"`
	B24ID     *int64    `json:"-"`
	B24Domain *string   `json:"-"`
	IsBlocked bool      `json:"-"`
	CreatedAt time.Time `json:"-"`
	UpdatedAt time.Time `json:"-"`
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

// UserRepo инкапсулирует работу с таблицей users (см. миграцию 0001).
type UserRepo struct{ pool *pgxpool.Pool }

func NewUserRepo(p *pgxpool.Pool) *UserRepo { return &UserRepo{pool: p} }

// FindOrCreateByB24 — атомарный upsert по email. Если юзер новый — создаём
// с дефолтной ролью ROLE_FINANCE; если существует — обновляем имя/B24-связку.
func (r *UserRepo) FindOrCreateByB24(ctx context.Context, in B24CallbackPayload) (*User, error) {
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback(ctx)

	row := tx.QueryRow(ctx, `SELECT id, email, name, last_name, roles, b24_id, b24_domain, is_blocked
		FROM users WHERE lower(email) = lower($1) LIMIT 1`, in.Email)
	var u User
	var rolesJSON []byte
	err = row.Scan(&u.ID, &u.Email, &u.Name, &u.LastName, &rolesJSON, &u.B24ID, &u.B24Domain, &u.IsBlocked)

	switch {
	case errors.Is(err, pgx.ErrNoRows):
		roles := []string{"ROLE_USER", "ROLE_FINANCE"}
		rb, _ := json.Marshal(roles)
		err = tx.QueryRow(ctx, `INSERT INTO users(email, name, last_name, roles, b24_id, b24_domain, password)
			VALUES ($1, $2, $3, $4, $5, $6, '') RETURNING id`,
			in.Email, in.Name, in.LastName, rb, in.B24ID, in.B24Domain).Scan(&u.ID)
		if err != nil {
			return nil, err
		}
		u.Email = in.Email
		u.Name = in.Name
		u.LastName = in.LastName
		u.Roles = roles
		u.B24ID = in.B24ID
		u.B24Domain = strPtr(in.B24Domain)
	case err != nil:
		return nil, err
	default:
		_ = json.Unmarshal(rolesJSON, &u.Roles)
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
	return &u, tx.Commit(ctx)
}

func (r *UserRepo) ByID(ctx context.Context, id int64) (*User, error) {
	var u User
	var rolesJSON []byte
	err := r.pool.QueryRow(ctx, `SELECT id, email, name, last_name, roles, b24_id, b24_domain, is_blocked
		FROM users WHERE id=$1`, id).
		Scan(&u.ID, &u.Email, &u.Name, &u.LastName, &rolesJSON, &u.B24ID, &u.B24Domain, &u.IsBlocked)
	if err != nil {
		return nil, err
	}
	_ = json.Unmarshal(rolesJSON, &u.Roles)
	return &u, nil
}

func strPtr(s string) *string {
	if s == "" {
		return nil
	}
	return &s
}
