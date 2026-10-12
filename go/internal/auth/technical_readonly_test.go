package auth

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

const fixtureToken = "finance_ro_0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef"

var fixtureNow = time.Date(2026, 10, 10, 12, 0, 0, 0, time.UTC)

func fixtureRecords(expiry time.Time) []byte {
	digest := sha256.Sum256([]byte(fixtureToken))
	raw, _ := json.Marshal([]technicalToken{{SHA256: hex.EncodeToString(digest[:]), Name: "test", SubjectEmail: "reader@example.test", ExpiresAt: expiry.Format(time.RFC3339)}})
	return raw
}
func fixtureGate(t *testing.T, raw []byte, resolve ResolveTechnicalSubject) *TechnicalReadonlyGate {
	t.Helper()
	g, err := newTechnicalReadonlyGate(raw, technicalRouteJSON, resolve, func() time.Time { return fixtureNow })
	if err != nil {
		t.Fatal(err)
	}
	return g
}

func TestTechnicalReadonlyValidPreservesIdentityAndStripsCredential(t *testing.T) {
	original := &User{ID: 77, Email: "reader@example.test", Roles: []string{RoleFinanceAdmin}}
	g := fixtureGate(t, fixtureRecords(fixtureNow.Add(time.Hour)), func(_ context.Context, email string) (*User, error) {
		if email != original.Email {
			t.Fatal(email)
		}
		return original, nil
	})
	mux := http.NewServeMux()
	called := 0
	mux.HandleFunc("GET /api/reports/debt/report", TechnicalReadHandler("GET /api/reports/debt/report", "debt.Report", func(w http.ResponseWriter, r *http.Request) {
		called++
		u := CurrentUser(r)
		if u == original || u.ID != original.ID || !HasRole(u, RoleFinanceAdmin) || !TechnicalReadonly(r.Context()) || TechnicalReadonlyName(r.Context()) != "test" || r.Header.Get("Authorization") != "" {
			t.Fatal("technical identity or credential boundary violated")
		}
		u.Roles[0] = RoleAdmin
		w.WriteHeader(http.StatusNoContent)
	}))
	for _, method := range []string{"GET", "HEAD"} {
		r := httptest.NewRequest(method, "/api/reports/debt/report?date_from=2026-01-01&date_to=2026-01-31", nil)
		r.Header.Set("Authorization", "Bearer "+fixtureToken)
		w := httptest.NewRecorder()
		g.Middleware(mux, mux).ServeHTTP(w, r)
		if w.Code != http.StatusNoContent {
			t.Fatalf("%s: %d", method, w.Code)
		}
	}
	if called != 2 || original.Roles[0] != RoleFinanceAdmin {
		t.Fatal("subject was mutated")
	}
}

func TestTechnicalReadonlyDeniesWritesUnknownPathsAndAliasesBeforeResolve(t *testing.T) {
	g := fixtureGate(t, fixtureRecords(fixtureNow.Add(time.Hour)), func(context.Context, string) (*User, error) {
		t.Fatal("denied request resolved subject")
		return nil, nil
	})
	mux := http.NewServeMux()
	mux.HandleFunc("GET /api/reports/debt/report", func(http.ResponseWriter, *http.Request) { t.Fatal("handler reached") })
	cases := []struct{ method, target string }{
		{"POST", "/api/reports/debt/report"}, {"PUT", "/api/reports/debt/report"}, {"GET", "/api/unknown"},
		{"GET", "/api/reports/debt/report/"}, {"GET", "/api//reports/debt/report"},
		{"GET", "/api/reports/debt/report?date_from=2026-01-01&date_from=2026-01-02"},
		{"GET", "/api/reports/debt/report?unknown=1"}, {"GET", "/api/reports/debt/report?"},
		{"GET", "http://example.test/api/reports/debt/report"},
	}
	for _, c := range cases {
		r := httptest.NewRequest(c.method, c.target, nil)
		r.Header.Set("Authorization", "Bearer "+fixtureToken)
		w := httptest.NewRecorder()
		g.Middleware(mux, http.HandlerFunc(func(http.ResponseWriter, *http.Request) { t.Fatal("downstream reached") })).ServeHTTP(w, r)
		if w.Code != http.StatusForbidden {
			t.Fatalf("%s %s: got %d", c.method, c.target, w.Code)
		}
	}
}

func TestTechnicalReadonlyCredentialsFailClosedAndDoesNotFallback(t *testing.T) {
	g := fixtureGate(t, fixtureRecords(fixtureNow.Add(time.Hour)), func(context.Context, string) (*User, error) { return nil, errors.New("missing") })
	mux := http.NewServeMux()
	mux.HandleFunc("GET /api/reports/debt/report", func(http.ResponseWriter, *http.Request) { t.Fatal("handler reached") })
	for _, header := range []string{"Bearer finance_ro_short", "Basic " + fixtureToken, "bearer " + fixtureToken, "Bearer finance_ro_" + strings.Repeat("f", 64)} {
		r := httptest.NewRequest("GET", "/api/reports/debt/report", nil)
		r.Header.Set("Authorization", header)
		w := httptest.NewRecorder()
		g.Middleware(mux, mux).ServeHTTP(w, r)
		if w.Code != http.StatusUnauthorized || strings.Contains(w.Body.String(), fixtureToken) {
			t.Fatalf("%q: %d", header, w.Code)
		}
	}
}

func TestTechnicalReadonlyConfigValidation(t *testing.T) {
	for _, raw := range []string{`{}`, `[ {"sha256":"bad"} ]`, string(fixtureRecords(fixtureNow.Add(time.Hour))) + ` []`, `[{"token":"finance_ro_secret"}]`} {
		if _, err := newTechnicalReadonlyGate([]byte(raw), technicalRouteJSON, func(context.Context, string) (*User, error) { return nil, nil }, time.Now); err == nil {
			t.Fatal("accepted invalid config")
		}
	}
	if _, err := newTechnicalReadonlyGate(technicalTokenJSON, technicalRouteJSON, func(context.Context, string) (*User, error) { return nil, nil }, time.Now); err != nil {
		t.Fatal(err)
	}
}
