package auth

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"
)

type Handler struct{ svc *Service }

func NewHandler(s *Service) *Handler { return &Handler{svc: s} }

type b24Resp struct {
	AccessToken  string  `json:"access_token"`
	RefreshToken string  `json:"refresh_token"`
	ExpiresIn    int64   `json:"expires_in"`
	User         userDTO `json:"user"`
}

type userDTO struct {
	ID    int64    `json:"id"`
	Email string   `json:"email"`
	Name  string   `json:"name"`
	Roles []string `json:"roles"`
}

func toDTO(u *User) userDTO {
	return userDTO{ID: u.ID, Email: u.Email, Name: u.DisplayName(), Roles: ExpandRoles(u.Roles)}
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

func writeErr(w http.ResponseWriter, status int, msg string) {
	writeJSON(w, status, map[string]string{"error": msg})
}

func (h *Handler) B24Callback(w http.ResponseWriter, r *http.Request) {
	var p B24CallbackPayload
	if err := json.NewDecoder(r.Body).Decode(&p); err != nil {
		writeErr(w, http.StatusBadRequest, "invalid body")
		return
	}
	sess, err := h.svc.HandleB24Callback(r.Context(), p)
	if err != nil {
		writeErr(w, http.StatusUnauthorized, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, b24Resp{
		AccessToken: sess.Access, RefreshToken: sess.Refresh,
		ExpiresIn: sess.ExpiresIn, User: toDTO(sess.User),
	})
}

func (h *Handler) Refresh(w http.ResponseWriter, r *http.Request) {
	var body struct {
		RefreshToken string `json:"refresh_token"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil || body.RefreshToken == "" {
		writeErr(w, http.StatusBadRequest, "refresh_token required")
		return
	}
	sess, err := h.svc.RefreshSession(r.Context(), body.RefreshToken)
	if err != nil {
		writeErr(w, http.StatusUnauthorized, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"access_token":  sess.Access,
		"refresh_token": sess.Refresh,
		"expires_in":    sess.ExpiresIn,
	})
}

type ctxKey int

const ctxKeyUser ctxKey = iota

// AuthObserver — необязательный наблюдатель успешной аутентификации. Ставится
// один раз при старте (cmd/api/logging.go) и служит одной цели: донести user_id
// до внешнего access-лога. Просто прочитать пользователя из контекста снаружи
// нельзя — RequireBearer кладёт *User в КЛОН запроса, и объемлющий middleware
// его уже не видит; поэтому пользователь «поднимается» наверх через изменяемый
// конверт, положенный в контекст до маршрутизации. nil → никто не слушает.
var AuthObserver func(ctx context.Context, userID int64)

// RequireBearer — middleware, которое читает Authorization: Bearer <token>,
// валидирует через сервис и кладёт *User в контекст.
func RequireBearer(svc *Service, next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		h := r.Header.Get("Authorization")
		const p = "Bearer "
		if !strings.HasPrefix(h, p) {
			writeErr(w, http.StatusUnauthorized, "missing bearer")
			return
		}
		token := strings.TrimSpace(strings.TrimPrefix(h, p))
		u, err := svc.AuthenticateBearer(r.Context(), token)
		if err != nil || u == nil {
			writeErr(w, http.StatusUnauthorized, "invalid token")
			return
		}
		if AuthObserver != nil {
			AuthObserver(r.Context(), u.ID)
		}
		ctx := context.WithValue(r.Context(), ctxKeyUser, u)
		ctx = context.WithValue(ctx, ctxKey(99), token)
		next.ServeHTTP(w, r.WithContext(ctx))
	}
}

func userFromCtx(ctx context.Context) *User {
	u, _ := ctx.Value(ctxKeyUser).(*User)
	return u
}

// UserFromCtx — публичный аксессор к *User, положенному в context middleware'ом
// RequireBearer. Возвращает (nil, false) если контекст без юзера. Используется
// другими доменами (например, reports/debt) для определения user_id.
func UserFromCtx(ctx context.Context) (*User, bool) {
	u, ok := ctx.Value(ctxKeyUser).(*User)
	return u, ok
}

func tokenFromCtx(ctx context.Context) string {
	t, _ := ctx.Value(ctxKey(99)).(string)
	return t
}

func (h *Handler) Me(w http.ResponseWriter, r *http.Request) {
	u := userFromCtx(r.Context())
	if u == nil {
		writeErr(w, http.StatusUnauthorized, "no session")
		return
	}
	writeJSON(w, http.StatusOK, toDTO(u))
}

func (h *Handler) Logout(w http.ResponseWriter, r *http.Request) {
	if t := tokenFromCtx(r.Context()); t != "" {
		_ = h.svc.Logout(r.Context(), t)
	}
	writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
}
