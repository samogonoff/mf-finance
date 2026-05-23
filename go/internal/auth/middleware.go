package auth

import "net/http"

// RequireRole — middleware, требующее, чтобы аутентифицированный пользователь
// имел указанную роль (с учётом иерархии). Оборачивает RequireBearer.
func RequireRole(svc *Service, role string, next http.HandlerFunc) http.HandlerFunc {
	return RequireBearer(svc, func(w http.ResponseWriter, r *http.Request) {
		u := userFromCtx(r.Context())
		if !HasRole(u, role) {
			writeErr(w, http.StatusForbidden, "forbidden")
			return
		}
		next.ServeHTTP(w, r)
	})
}

// CurrentUser — экспортированный геттер пользователя из контекста.
// Используют сторонние пакеты-хендлеры.
func CurrentUser(r *http.Request) *User { return userFromCtx(r.Context()) }
