package auth

import (
	"bytes"
	"context"
	"crypto/sha256"
	"crypto/subtle"
	_ "embed"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/url"
	"regexp"
	"strings"
	"time"
)

// Public digests travel inside the normal API binary; no runtime secret or DB issuance.
//
//go:embed technical_readonly_tokens.json
var technicalTokenJSON []byte

//go:embed technical_readonly_routes.json
var technicalRouteJSON []byte

type technicalToken struct {
	SHA256       string `json:"sha256"`
	Name         string `json:"name"`
	SubjectEmail string `json:"subject_email"`
	ExpiresAt    string `json:"expires_at"`
}

type technicalRoute struct {
	Pattern       string   `json:"pattern"`
	Handler       string   `json:"handler"`
	PathRegex     string   `json:"path_regex"`
	QueryKeys     []string `json:"query_keys"`
	ExcludedPaths []string `json:"excluded_paths,omitempty"`
	path          *regexp.Regexp
}

type technicalContextKey struct{}
type technicalBoundKey struct{}
type technicalRequest struct{ handler, name string }

// TechnicalReadonly reports a trusted request-local marker, never a client header or role.
func TechnicalReadonly(ctx context.Context) bool {
	_, ok := ctx.Value(technicalContextKey{}).(technicalRequest)
	return ok
}

// TechnicalReadonlyName exposes only the matched public name for log attribution.
// The private context key prevents client headers from supplying this provenance.
func TechnicalReadonlyName(ctx context.Context) string {
	if technical, ok := ctx.Value(technicalContextKey{}).(technicalRequest); ok {
		return technical.name
	}
	return ""
}

// TechnicalReadHandler binds the reviewed handler to its exact mux pattern.
// A newly registered handler using ordinary RequireBearer remains closed to technical tokens.
func TechnicalReadHandler(pattern, handler string, next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if technical, ok := r.Context().Value(technicalContextKey{}).(technicalRequest); ok {
			if r.Pattern != pattern || technical.handler != handler {
				writeErr(w, http.StatusForbidden, "technical read handler denied")
				return
			}
			r = r.WithContext(context.WithValue(r.Context(), technicalBoundKey{}, true))
		}
		next(w, r)
	}
}

// ResolveTechnicalSubject must only SELECT an existing user; it must never log in/upsert.
type ResolveTechnicalSubject func(context.Context, string) (*User, error)

type TechnicalReadonlyGate struct {
	tokens  []technicalToken
	routes  map[string]technicalRoute
	resolve ResolveTechnicalSubject
	now     func() time.Time
}

func strictJSON(raw []byte, target any) error {
	d := json.NewDecoder(bytes.NewReader(raw))
	d.DisallowUnknownFields()
	if err := d.Decode(target); err != nil {
		return err
	}
	if err := d.Decode(new(any)); err != io.EOF {
		return errors.New("trailing JSON")
	}
	return nil
}

func NewTechnicalReadonlyGate(resolve ResolveTechnicalSubject) (*TechnicalReadonlyGate, error) {
	return NewTechnicalReadonlyGateFromRecords(technicalTokenJSON, resolve)
}

// NewTechnicalReadonlyGateFromRecords validates public records against the embedded
// read policy. The API uses NewTechnicalReadonlyGate with its embedded registry;
// this explicit constructor also permits isolated integration fixtures.
func NewTechnicalReadonlyGateFromRecords(records []byte, resolve ResolveTechnicalSubject) (*TechnicalReadonlyGate, error) {
	return newTechnicalReadonlyGate(records, technicalRouteJSON, resolve, time.Now)
}

func newTechnicalReadonlyGate(tokens, routes []byte, resolve ResolveTechnicalSubject, now func() time.Time) (*TechnicalReadonlyGate, error) {
	g := &TechnicalReadonlyGate{resolve: resolve, now: now, routes: make(map[string]technicalRoute)}
	if err := strictJSON(tokens, &g.tokens); err != nil {
		return nil, err
	}
	if len(g.tokens) > 64 || resolve == nil {
		return nil, errors.New("invalid technical configuration")
	}
	names, digests := map[string]bool{}, map[string]bool{}
	for _, t := range g.tokens {
		if !regexp.MustCompile(`^[a-f0-9]{64}$`).MatchString(t.SHA256) || !regexp.MustCompile(`^[A-Za-z0-9_-]{1,80}$`).MatchString(t.Name) || names[t.Name] || digests[t.SHA256] || !validSubjectEmail(t.SubjectEmail) {
			return nil, errors.New("invalid technical record")
		}
		if _, err := time.Parse(time.RFC3339, t.ExpiresAt); err != nil {
			return nil, errors.New("invalid technical expiry")
		}
		names[t.Name], digests[t.SHA256] = true, true
	}
	var manifest []technicalRoute
	if err := strictJSON(routes, &manifest); err != nil {
		return nil, err
	}
	for _, route := range manifest {
		if !strings.HasPrefix(route.Pattern, "GET /api/") || route.Handler == "" || g.routes[route.Pattern].Handler != "" || !strings.HasPrefix(route.PathRegex, "^") || !strings.HasSuffix(route.PathRegex, "$") {
			return nil, errors.New("invalid technical route")
		}
		var err error
		route.path, err = regexp.Compile(route.PathRegex)
		if err != nil {
			return nil, errors.New("invalid technical path")
		}
		g.routes[route.Pattern] = route
	}
	return g, nil
}

func validSubjectEmail(s string) bool {
	return len(s) <= 254 && strings.TrimSpace(s) == s && strings.Count(s, "@") == 1 && !strings.ContainsAny(s, " \t\r\n\x00") && !strings.HasPrefix(s, "@") && !strings.HasSuffix(s, "@")
}

// Detect the reserved namespace even in malformed/duplicate authorization headers.
func hasTechnicalCredential(r *http.Request) bool {
	for _, h := range r.Header.Values("Authorization") {
		if strings.Contains(strings.ToLower(h), "finance_ro_") {
			return true
		}
	}
	return false
}

var technicalBearerRE = regexp.MustCompile(`^Bearer finance_ro_[a-f0-9]{64}$`)

func (g *TechnicalReadonlyGate) authenticate(r *http.Request) (*User, string, error) {
	h := r.Header.Values("Authorization")
	if len(h) != 1 || !technicalBearerRE.MatchString(h[0]) {
		return nil, "", errors.New("invalid technical bearer")
	}
	digest := sha256.Sum256([]byte(strings.TrimPrefix(h[0], "Bearer ")))
	for _, record := range g.tokens {
		want, _ := hex.DecodeString(record.SHA256)
		if subtle.ConstantTimeCompare(want, digest[:]) != 1 {
			continue
		}
		expiry, _ := time.Parse(time.RFC3339, record.ExpiresAt)
		if !g.now().Before(expiry) {
			return nil, "", errors.New("expired technical bearer")
		}
		u, err := g.resolve(r.Context(), record.SubjectEmail)
		if err != nil || u == nil || u.ID <= 0 || u.IsBlocked || !strings.EqualFold(u.Email, record.SubjectEmail) {
			return nil, "", errors.New("unavailable technical subject")
		}
		copy := *u
		copy.Roles = append([]string(nil), u.Roles...)
		if u.B24ID != nil {
			id := *u.B24ID
			copy.B24ID = &id
		}
		if u.B24Domain != nil {
			domain := *u.B24Domain
			copy.B24Domain = &domain
		}
		return &copy, record.Name, nil
	}
	return nil, "", errors.New("unknown technical bearer")
}

func canonicalTechnicalRequest(r *http.Request, route technicalRoute) bool {
	if r.Method != http.MethodGet && r.Method != http.MethodHead {
		return false
	}
	if r.ContentLength != 0 || len(r.TransferEncoding) != 0 {
		return false
	}
	if r.URL == nil || r.URL.IsAbs() || r.URL.Host != "" || r.URL.Opaque != "" || r.URL.Fragment != "" || r.URL.RawPath != "" || r.URL.ForceQuery {
		return false
	}
	path := r.URL.Path
	if strings.ContainsAny(path, "%\\?#") || strings.Contains(path, "//") || !route.path.MatchString(path) {
		return false
	}
	for _, excluded := range route.ExcludedPaths {
		if path == excluded {
			return false
		}
	}
	// RequestURI is the original target, checked before ServeMux's cleaning redirects.
	if r.RequestURI != r.URL.RequestURI() {
		return false
	}
	if len(r.URL.RawQuery) > 8192 {
		return false
	}
	query, err := url.ParseQuery(r.URL.RawQuery)
	if err != nil {
		return false
	}
	keys := map[string]bool{}
	for _, key := range route.QueryKeys {
		keys[key] = true
	}
	for key, values := range query {
		if !keys[key] || len(values) != 1 {
			return false
		}
		if strings.Contains(strings.ToLower(values[0]), "finance_ro_") {
			return false
		}
		for _, c := range values[0] {
			if c < 0x20 || c == 0x7f {
				return false
			}
		}
	}
	// One canonical query representation; reject aliases, duplicates, semicolons and ambiguity.
	return query.Encode() == r.URL.RawQuery
}

// Middleware must be OUTSIDE CORS, auditing and ServeMux: deny before effects or redirects.
// Ordinary sessions and X-API-Key /v1 requests keep their existing path unchanged.
func (g *TechnicalReadonlyGate) Middleware(mux *http.ServeMux, next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !hasTechnicalCredential(r) {
			next.ServeHTTP(w, r)
			return
		}
		if len(r.Header.Values("Authorization")) != 1 || !technicalBearerRE.MatchString(r.Header.Get("Authorization")) {
			writeErr(w, http.StatusUnauthorized, "invalid technical credential")
			return
		}
		_, pattern := mux.Handler(r)
		route, ok := g.routes[pattern]
		if !ok || !canonicalTechnicalRequest(r, route) {
			writeErr(w, http.StatusForbidden, "technical read route denied")
			return
		}
		u, name, err := g.authenticate(r)
		if err != nil {
			writeErr(w, http.StatusUnauthorized, "invalid technical credential")
			return
		}
		// No downstream logger or handler needs the credential. Also reject an
		// attempt to copy it into a logged header (request ID, page, etc.).
		bearer := strings.TrimPrefix(r.Header.Get("Authorization"), "Bearer ")
		for key, values := range r.Header {
			if strings.EqualFold(key, "Authorization") {
				continue
			}
			for _, value := range values {
				if strings.Contains(value, bearer) {
					writeErr(w, http.StatusForbidden, "credential metadata denied")
					return
				}
			}
		}
		r = r.Clone(r.Context())
		r.Header.Del("Authorization")
		ctx := ContextWithUser(r.Context(), u)
		ctx = context.WithValue(ctx, technicalContextKey{}, technicalRequest{handler: route.Handler, name: name})
		next.ServeHTTP(w, r.WithContext(ctx))
	})
}
