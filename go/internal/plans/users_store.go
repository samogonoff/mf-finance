package plans

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/jackc/pgx/v5/pgxpool"
)

// Модульный доступ к пользователям для страницы «Пользователи и права».
// Глобальная админка /api/admin/users закрыта под ROLE_ADMIN; «Администратору
// Тактических планов» (ROLE_PLANS_ADMIN) нужен СВОЙ список, чтобы выдавать
// системные роли модуля и назначать должности. Здесь он может управлять только
// ТП-ролями (ROLE_PLANS_USER/ROLE_PLANS_ADMIN) — глобальный ROLE_ADMIN не трогает.

const (
	rolePlansUser  = "ROLE_PLANS_USER"
	rolePlansAdmin = "ROLE_PLANS_ADMIN"
	roleAdmin      = "ROLE_ADMIN"
)

// PlanUser — пользователь с его прямыми ролями и меткой отсутствия (для страницы прав).
type PlanUser struct {
	ID            int64    `json:"id"`
	Name          string   `json:"name"`
	LastName      string   `json:"last_name"`
	Email         string   `json:"email"`
	Roles         []string `json:"roles"`
	PlansAdmin    bool     `json:"plans_admin"`
	PlansUser     bool     `json:"plans_user"`
	GlobalAdmin   bool     `json:"global_admin"`
	AbsenceStatus string   `json:"absence_status"` // ''|vacation|sick|dismissed
	AbsenceUntil  string   `json:"absence_until"`  // YYYY-MM-DD или ''
}

// UsersStore — чтение users + точечная правка ТП-ролей.
type UsersStore struct{ pool *pgxpool.Pool }

// NewUsersStore — конструктор.
func NewUsersStore(pool *pgxpool.Pool) *UsersStore { return &UsersStore{pool: pool} }

// List возвращает незаблокированных пользователей с их прямыми ролями.
func (s *UsersStore) List(ctx context.Context) ([]PlanUser, error) {
	rows, err := s.pool.Query(ctx, `
		SELECT id, COALESCE(name,''), COALESCE(last_name,''), COALESCE(email,''), COALESCE(roles,'[]'::jsonb),
		       COALESCE(absence_status,''), COALESCE(to_char(absence_until,'YYYY-MM-DD'),'')
		FROM users
		WHERE COALESCE(is_blocked,false) = false
		ORDER BY last_name, name`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := make([]PlanUser, 0)
	for rows.Next() {
		var u PlanUser
		var raw []byte
		if err := rows.Scan(&u.ID, &u.Name, &u.LastName, &u.Email, &raw, &u.AbsenceStatus, &u.AbsenceUntil); err != nil {
			return nil, err
		}
		_ = json.Unmarshal(raw, &u.Roles)
		for _, r := range u.Roles {
			switch r {
			case rolePlansAdmin:
				u.PlansAdmin = true
			case rolePlansUser:
				u.PlansUser = true
			case roleAdmin:
				u.GlobalAdmin = true
			}
		}
		out = append(out, u)
	}
	return out, rows.Err()
}

// SetPlansRoles выставляет ТП-роли пользователю, СОХРАНЯЯ все прочие роли
// (cost/finance/admin не затрагиваются). plansAdmin → добавляет ROLE_PLANS_ADMIN;
// plansUser → ROLE_PLANS_USER (admin его и так разворачивает, но храним явно).
func (s *UsersStore) SetPlansRoles(ctx context.Context, id int64, plansAdmin, plansUser bool) error {
	var raw []byte
	if err := s.pool.QueryRow(ctx, `SELECT COALESCE(roles,'[]'::jsonb) FROM users WHERE id=$1`, id).Scan(&raw); err != nil {
		return err
	}
	var roles []string
	_ = json.Unmarshal(raw, &roles)

	// Убираем ТП-роли, оставляем остальные.
	kept := roles[:0:0]
	for _, r := range roles {
		if r != rolePlansAdmin && r != rolePlansUser {
			kept = append(kept, r)
		}
	}
	if plansAdmin {
		kept = append(kept, rolePlansAdmin)
	} else if plansUser {
		kept = append(kept, rolePlansUser)
	}
	next, _ := json.Marshal(kept)
	_, err := s.pool.Exec(ctx, `UPDATE users SET roles=$2 WHERE id=$1`, id, next)
	return err
}

// UpsertByB24 догружает/обновляет пользователя по B24-id (импорт сотрудников,
// ещё не заходивших). Ищет по b24_id, затем по email. Возвращает статус
// "created"|"updated". Прикладные роли НЕ трогает (их назначают отдельно).
func (s *UsersStore) UpsertByB24(ctx context.Context, b24id int64, name, lastName, email, domain string) (int64, string, error) {
	if email == "" {
		email = fmt.Sprintf("b24-%d@import.local", b24id)
	}
	var id int64
	err := s.pool.QueryRow(ctx, `SELECT id FROM users WHERE b24_id=$1 OR lower(email)=lower($2) LIMIT 1`, b24id, email).Scan(&id)
	if err != nil {
		// Новый пользователь: рандом-пароль, базовые роли ROLE_USER + ROLE_PLANS_USER
		// (Участник модуля — даём автоматом при добавлении).
		e := s.pool.QueryRow(ctx, `
			INSERT INTO users (email, password, name, last_name, roles, b24_id, b24_domain)
			VALUES ($1, '', $2, $3, '["ROLE_USER","ROLE_PLANS_USER"]'::jsonb, $4, $5) RETURNING id`,
			email, name, lastName, b24id, domain).Scan(&id)
		if e != nil {
			return 0, "", e
		}
		return id, "created", nil
	}
	// Существующий: обновляем имя/b24 и ДОБАВЛЯЕМ ROLE_PLANS_USER, если нет
	// (добавление в систему = уровень Участник).
	_, e := s.pool.Exec(ctx, `
		UPDATE users SET b24_id=$2, b24_domain=$3,
			name=CASE WHEN COALESCE(name,'')='' THEN $4 ELSE name END,
			last_name=CASE WHEN COALESCE(last_name,'')='' THEN $5 ELSE last_name END,
			roles=CASE WHEN COALESCE(roles,'[]'::jsonb) @> '["ROLE_PLANS_USER"]' THEN roles
			           ELSE COALESCE(roles,'[]'::jsonb) || '["ROLE_PLANS_USER"]'::jsonb END
		WHERE id=$1`, id, b24id, domain, name, lastName)
	return id, "updated", e
}

// Search ищет пользователей по фамилии/имени (для пикера директора/ТОПа/замов).
func (s *UsersStore) Search(ctx context.Context, q string) ([]PlanUser, error) {
	rows, err := s.pool.Query(ctx, `
		SELECT id, COALESCE(name,''), COALESCE(last_name,''), COALESCE(email,'')
		FROM users
		WHERE COALESCE(is_blocked,false)=false
		  AND (lower(last_name) LIKE lower($1) OR lower(name) LIKE lower($1) OR lower(email) LIKE lower($1))
		ORDER BY last_name, name LIMIT 20`, q+"%")
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := make([]PlanUser, 0)
	for rows.Next() {
		var u PlanUser
		if err := rows.Scan(&u.ID, &u.Name, &u.LastName, &u.Email); err != nil {
			return nil, err
		}
		out = append(out, u)
	}
	return out, rows.Err()
}

// SetAbsence выставляет метку отсутствия пользователя (ручной ввод; позже —
// автоматически из B24). Пустой status снимает метку. При наличии — глобально
// во всех интерфейсах и активирует зама (DeputyStore.ActiveDeputy).
func (s *UsersStore) SetAbsence(ctx context.Context, id int64, status, until string) error {
	var untilArg any
	if until != "" {
		untilArg = until
	}
	_, err := s.pool.Exec(ctx, `
		UPDATE users SET absence_status=$2, absence_until=$3, absence_synced_at=NOW() WHERE id=$1`,
		id, status, untilArg)
	return err
}
