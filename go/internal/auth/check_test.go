package auth

import (
	"context"
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"
)

// Хендлер Check вызывается ТОЛЬКО после RequireBearer, поэтому в тестах
// пользователя кладём в контекст руками — сервис (Redis/PG) здесь не нужен.
func requestWithUser(target string, u *User) *http.Request {
	r := httptest.NewRequest(http.MethodGet, target, nil)
	if u == nil {
		return r
	}
	return r.WithContext(context.WithValue(r.Context(), ctxKeyUser, u))
}

func TestCheck_OKPassesIdentityInHeaders(t *testing.T) {
	u := &User{ID: 42, Email: "i.ivanov@mf.ru", Name: "Иван", LastName: "Иванов",
		Roles: []string{RoleBudgetAdmin}}
	w := httptest.NewRecorder()

	(&Handler{}).Check(w, requestWithUser("/api/auth/check?role="+RoleBudgetUser, u))

	if w.Code != http.StatusOK {
		t.Fatalf("want 200, got %d", w.Code)
	}
	if got := w.Header().Get("X-Auth-User-Id"); got != "42" {
		t.Errorf("X-Auth-User-Id = %q, want %q", got, "42")
	}
	// Контур раскодирует заголовок urllib.parse.unquote — пробел обязан
	// приехать как %20, иначе в шапке будет «Иван+Иванов».
	name := w.Header().Get("X-Auth-Name")
	if name != "%D0%98%D0%B2%D0%B0%D0%BD%20%D0%98%D0%B2%D0%B0%D0%BD%D0%BE%D0%B2" {
		t.Errorf("X-Auth-Name = %q, want percent-encoded «Иван Иванов» с %%20", name)
	}
	if decoded, err := url.PathUnescape(name); err != nil || decoded != "Иван Иванов" {
		t.Errorf("X-Auth-Name must decode back to «Иван Иванов», got %q (err %v)", decoded, err)
	}
	// Роли уходят развёрнутыми: контур делает простую проверку вхождения.
	if got := w.Header().Get("X-Auth-Roles"); got != "ROLE_BUDGET,ROLE_BUDGET_ADMIN,ROLE_USER" {
		t.Errorf("X-Auth-Roles = %q, want развёрнутый набор", got)
	}
}

func TestCheck_AsciiHeadersOnly(t *testing.T) {
	u := &User{ID: 1, Email: "пользователь@mf.ru", Name: "Пётр", Roles: []string{RoleBudgetUser}}
	w := httptest.NewRecorder()

	(&Handler{}).Check(w, requestWithUser("/api/auth/check?role="+RoleBudgetUser, u))

	for _, h := range []string{"X-Auth-Email", "X-Auth-Name"} {
		for _, b := range []byte(w.Header().Get(h)) {
			if b > 0x7f || b == '\r' || b == '\n' {
				t.Fatalf("%s содержит не-ASCII или перевод строки: %q", h, w.Header().Get(h))
			}
		}
	}
}

func TestCheck_ForbiddenWhenRoleMissing(t *testing.T) {
	u := &User{ID: 7, Email: "a@mf.ru", Roles: []string{RoleCostUser}}
	w := httptest.NewRecorder()

	(&Handler{}).Check(w, requestWithUser("/api/auth/check?role="+RoleBudgetUser, u))

	if w.Code != http.StatusForbidden {
		t.Fatalf("want 403, got %d", w.Code)
	}
	// nginx пропускает наружу 401/403 как есть; identity при отказе не отдаём.
	if w.Header().Get("X-Auth-User-Id") != "" {
		t.Errorf("при отказе identity-заголовков быть не должно")
	}
}

func TestCheck_UnknownRoleIsForbiddenNotAllowed(t *testing.T) {
	// Опечатка в конфиге гейта не должна открывать раздел всем подряд.
	u := &User{ID: 7, Email: "a@mf.ru", Roles: []string{RoleAdmin}}
	w := httptest.NewRecorder()

	(&Handler{}).Check(w, requestWithUser("/api/auth/check?role=ROLE_BUDGE", u))

	if w.Code != http.StatusForbidden {
		t.Fatalf("want 403 для неизвестной роли, got %d", w.Code)
	}
}

func TestCheck_EmptyRoleChecksSessionOnly(t *testing.T) {
	u := &User{ID: 5, Email: "b@mf.ru", Roles: []string{}}
	w := httptest.NewRecorder()

	(&Handler{}).Check(w, requestWithUser("/api/auth/check", u))

	if w.Code != http.StatusOK {
		t.Fatalf("без role ручка проверяет только живость сессии, got %d", w.Code)
	}
	if w.Header().Get("X-Auth-Roles") != RoleUser {
		t.Errorf("X-Auth-Roles = %q, want %q", w.Header().Get("X-Auth-Roles"), RoleUser)
	}
}

func TestCheck_NoUserIsUnauthorized(t *testing.T) {
	w := httptest.NewRecorder()

	(&Handler{}).Check(w, requestWithUser("/api/auth/check?role="+RoleBudgetUser, nil))

	if w.Code != http.StatusUnauthorized {
		t.Fatalf("want 401, got %d", w.Code)
	}
}

func TestSetIdentityHeaders_NilUserIsNoop(t *testing.T) {
	h := http.Header{}
	SetIdentityHeaders(h, nil)
	if len(h) != 0 {
		t.Errorf("SetIdentityHeaders(_, nil) не должен ничего писать, got %v", h)
	}
}
