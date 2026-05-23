// Package users — админский CRUD над таблицей users.
// Эталон: MP src/Controller/Api/Admin/UserController.php.
package users

import (
	"encoding/json"
	"errors"
	"net/http"
	"strconv"
	"strings"

	"github.com/company/finance-api/internal/auth"
)

type Handler struct {
	repo   *auth.UserRepo
	svc    *auth.Service
	tokens *auth.TokenRepo
}

func NewHandler(repo *auth.UserRepo, svc *auth.Service, tokens *auth.TokenRepo) *Handler {
	return &Handler{repo: repo, svc: svc, tokens: tokens}
}

// userAdminDTO — расширенный DTO для админки (с is_blocked и notify_via_b24,
// которых нет в публичном /api/auth/me).
type userAdminDTO struct {
	ID                      int64    `json:"id"`
	Email                   string   `json:"email"`
	Name                    string   `json:"name"`
	LastName                string   `json:"last_name"`
	Roles                   []string `json:"roles"`           // прямые роли, как в БД
	EffectiveRoles          []string `json:"effective_roles"` // с учётом ExpandRoles
	IsBlocked               bool     `json:"is_blocked"`
	NotifyViaB24            bool     `json:"notify_via_b24"`
	WelcomeNotificationSent bool     `json:"welcome_notification_sent"`
	CreatedAt               string   `json:"created_at"`
}

func toAdminDTO(u *auth.User) userAdminDTO {
	return userAdminDTO{
		ID:                      u.ID,
		Email:                   u.Email,
		Name:                    u.Name,
		LastName:                u.LastName,
		Roles:                   u.Roles,
		EffectiveRoles:          auth.ExpandRoles(u.Roles),
		IsBlocked:               u.IsBlocked,
		NotifyViaB24:            u.NotifyViaB24,
		WelcomeNotificationSent: u.WelcomeNotificationSent,
		CreatedAt:               u.CreatedAt.Format("2006-01-02T15:04:05Z07:00"),
	}
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

func writeErr(w http.ResponseWriter, status int, msg string) {
	writeJSON(w, status, map[string]string{"error": msg})
}

// List — GET /api/admin/users?email=&name=&is_blocked=&limit=&offset=
func (h *Handler) List(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	f := auth.ListFilter{
		Email: q.Get("email"),
		Name:  q.Get("name"),
	}
	if v := q.Get("is_blocked"); v != "" {
		b := v == "1" || strings.EqualFold(v, "true")
		f.IsBlocked = &b
	}
	if v := q.Get("limit"); v != "" {
		if n, err := strconv.Atoi(v); err == nil {
			f.Limit = n
		}
	}
	if v := q.Get("offset"); v != "" {
		if n, err := strconv.Atoi(v); err == nil {
			f.Offset = n
		}
	}

	list, total, err := h.repo.List(r.Context(), f)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	out := make([]userAdminDTO, 0, len(list))
	for i := range list {
		out = append(out, toAdminDTO(&list[i]))
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"data":  out,
		"total": total,
	})
}

func parseIDFromPath(path, prefix string) (int64, error) {
	rest := strings.TrimPrefix(path, prefix)
	rest = strings.TrimSuffix(rest, "/")
	// rest = "{id}" или "{id}/something"
	idStr, _, _ := strings.Cut(rest, "/")
	return strconv.ParseInt(idStr, 10, 64)
}

// Block — POST /api/admin/users/{id}/block
func (h *Handler) Block(w http.ResponseWriter, r *http.Request) { h.setBlocked(w, r, true) }

// Unblock — POST /api/admin/users/{id}/unblock
func (h *Handler) Unblock(w http.ResponseWriter, r *http.Request) { h.setBlocked(w, r, false) }

func (h *Handler) setBlocked(w http.ResponseWriter, r *http.Request, blocked bool) {
	id, err := parseIDFromPath(r.URL.Path, "/api/admin/users/")
	if err != nil {
		writeErr(w, http.StatusBadRequest, "invalid id")
		return
	}
	me := auth.CurrentUser(r)
	if blocked && me != nil && me.ID == id {
		writeErr(w, http.StatusBadRequest, "cannot block self")
		return
	}
	if err := h.repo.SetBlocked(r.Context(), id, blocked); err != nil {
		writeErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	// При блокировке — стираем все живые токены пользователя.
	if blocked && h.tokens != nil {
		_ = h.tokens.RevokeAll(r.Context(), id)
	}
	writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
}

// UpdateRoles — PUT /api/admin/users/{id}/roles  body: {"roles":["ROLE_..."]}
func (h *Handler) UpdateRoles(w http.ResponseWriter, r *http.Request) {
	id, err := parseIDFromPath(r.URL.Path, "/api/admin/users/")
	if err != nil {
		writeErr(w, http.StatusBadRequest, "invalid id")
		return
	}
	var body struct {
		Roles []string `json:"roles"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeErr(w, http.StatusBadRequest, "invalid body")
		return
	}
	roles := auth.FilterAllowed(body.Roles)

	// Защита: админ не может снять с себя ROLE_ADMIN.
	me := auth.CurrentUser(r)
	if me != nil && me.ID == id {
		if !containsRole(roles, auth.RoleAdmin) {
			writeErr(w, http.StatusBadRequest, "cannot remove ROLE_ADMIN from self")
			return
		}
	}

	if err := h.repo.UpdateRoles(r.Context(), id, roles); err != nil {
		if errors.Is(err, errors.New("")) { // placeholder
			writeErr(w, http.StatusInternalServerError, err.Error())
			return
		}
		writeErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	u, err := h.repo.ByID(r.Context(), id)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, toAdminDTO(u))
}

// AllowedRoles — GET /api/admin/users/roles → список ролей, которые админ может присвоить.
// Удобно фронту, чтобы не дублировать список.
func (h *Handler) AllowedRoles(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, map[string]any{"roles": auth.Allowed})
}

func containsRole(rs []string, r string) bool {
	for _, x := range rs {
		if x == r {
			return true
		}
	}
	return false
}
