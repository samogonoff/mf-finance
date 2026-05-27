package bugtracker

import (
	"encoding/json"
	"mime/multipart"
	"net/http"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/company/finance-api/internal/auth"
)

type Handler struct {
	svc     *Service
	repo    *Repo
	storage *Storage
}

func NewHandler(svc *Service, repo *Repo, storage *Storage) *Handler {
	return &Handler{svc: svc, repo: repo, storage: storage}
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

func writeErr(w http.ResponseWriter, status int, msg string) {
	writeJSON(w, status, map[string]string{"error": msg})
}

// CreateReport — POST /api/bugtracker/report (multipart: payload + screenshots[])
func (h *Handler) CreateReport(w http.ResponseWriter, r *http.Request) {
	// multipart — лимит 25 МБ на форму суммарно.
	if err := r.ParseMultipartForm(25 << 20); err != nil {
		writeErr(w, http.StatusBadRequest, "invalid multipart: "+err.Error())
		return
	}
	payload := r.FormValue("payload")
	if payload == "" {
		writeErr(w, http.StatusBadRequest, "payload required")
		return
	}
	var in CreateInput
	if err := json.Unmarshal([]byte(payload), &in); err != nil {
		writeErr(w, http.StatusBadRequest, "invalid payload json")
		return
	}

	var userID *int64
	if u, ok := auth.UserFromCtx(r.Context()); ok && u != nil {
		id := u.ID
		userID = &id
	}

	var files []*multipart.FileHeader
	if r.MultipartForm != nil {
		files = r.MultipartForm.File["screenshots"]
	}

	rep, dup, err := h.svc.Create(r.Context(), userID, in, files)
	if err != nil {
		writeErr(w, http.StatusBadRequest, err.Error())
		return
	}
	status := http.StatusCreated
	if dup {
		status = http.StatusOK
	}
	writeJSON(w, status, map[string]any{
		"id":           rep.ID,
		"deduplicated": dup,
		"section":      rep.Section,
		"status":       rep.Status,
	})
}

// ── Admin ───────────────────────────────────────────────────────────────────

// AdminList — GET /api/bugtracker/list
func (h *Handler) AdminList(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	f := ListFilter{
		Section: q.Get("section"),
		Status:  q.Get("status"),
		Type:    q.Get("type"),
		Search:  q.Get("search"),
	}
	if v := q.Get("user_id"); v != "" {
		if n, err := strconv.ParseInt(v, 10, 64); err == nil {
			f.UserID = n
		}
	}
	if v := q.Get("date_from"); v != "" {
		if t, err := time.Parse(time.RFC3339, v); err == nil {
			f.DateFrom = &t
		} else if t, err := time.Parse("2006-01-02", v); err == nil {
			f.DateFrom = &t
		}
	}
	if v := q.Get("date_to"); v != "" {
		if t, err := time.Parse(time.RFC3339, v); err == nil {
			f.DateTo = &t
		} else if t, err := time.Parse("2006-01-02", v); err == nil {
			end := t.Add(24 * time.Hour)
			f.DateTo = &end
		}
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
	items, total, err := h.repo.List(r.Context(), f)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"data": items, "total": total})
}

// AdminGet — GET /api/bugtracker/report/{id}
func (h *Handler) AdminGet(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		writeErr(w, http.StatusBadRequest, "invalid id")
		return
	}
	rep, err := h.repo.Get(r.Context(), id)
	if err != nil {
		writeErr(w, http.StatusNotFound, "not found")
		return
	}
	writeJSON(w, http.StatusOK, rep)
}

// AdminPatch — PATCH /api/bugtracker/report/{id}
func (h *Handler) AdminPatch(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		writeErr(w, http.StatusBadRequest, "invalid id")
		return
	}
	var in PatchInput
	if err := json.NewDecoder(r.Body).Decode(&in); err != nil {
		writeErr(w, http.StatusBadRequest, "invalid body")
		return
	}
	if in.Status != nil && !IsValidStatus(*in.Status) {
		writeErr(w, http.StatusBadRequest, "invalid status")
		return
	}
	if err := h.repo.Patch(r.Context(), id, in); err != nil {
		writeErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	rep, _ := h.repo.Get(r.Context(), id)
	writeJSON(w, http.StatusOK, rep)
}

// AdminDelete — DELETE /api/bugtracker/report/{id}
func (h *Handler) AdminDelete(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		writeErr(w, http.StatusBadRequest, "invalid id")
		return
	}
	if err := h.repo.Delete(r.Context(), id); err != nil {
		writeErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
}

// AdminMetrics — GET /api/bugtracker/metrics
func (h *Handler) AdminMetrics(w http.ResponseWriter, r *http.Request) {
	m, err := h.repo.Metrics(r.Context())
	if err != nil {
		writeErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, m)
}

// ── Sources ─────────────────────────────────────────────────────────────────

// SourcesList — GET /api/bugtracker/sources?active=1
func (h *Handler) SourcesList(w http.ResponseWriter, r *http.Request) {
	active := r.URL.Query().Get("active") == "1"
	list, err := h.repo.ListSources(r.Context(), active)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, list)
}

// SourcesCreate — POST /api/bugtracker/sources
func (h *Handler) SourcesCreate(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Name string `json:"name"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil || strings.TrimSpace(body.Name) == "" {
		writeErr(w, http.StatusBadRequest, "name required")
		return
	}
	s, err := h.repo.CreateSource(r.Context(), strings.TrimSpace(body.Name))
	if err != nil {
		writeErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusCreated, s)
}

// SourcesPatch — PATCH /api/bugtracker/sources/{id}
func (h *Handler) SourcesPatch(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		writeErr(w, http.StatusBadRequest, "invalid id")
		return
	}
	var body struct {
		Name     *string `json:"name,omitempty"`
		IsActive *bool   `json:"is_active,omitempty"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeErr(w, http.StatusBadRequest, "invalid body")
		return
	}
	if err := h.repo.PatchSource(r.Context(), id, body.Name, body.IsActive); err != nil {
		writeErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
}

// ── Раздача скриншотов ──────────────────────────────────────────────────────

// ServeUpload — GET /uploads/bugtracker/{id}/{file}
// Открыт без auth: ссылки в админских уведомлениях и админской UI должны
// открываться по прямому URL. Защита — длинные UUID-имена файлов.
func (h *Handler) ServeUpload(w http.ResponseWriter, r *http.Request) {
	// Path: /uploads/bugtracker/{id}/{file}
	parts := strings.Split(strings.TrimPrefix(r.URL.Path, "/uploads/bugtracker/"), "/")
	if len(parts) != 2 || parts[0] == "" || parts[1] == "" {
		http.NotFound(w, r)
		return
	}
	id, err := strconv.ParseInt(parts[0], 10, 64)
	if err != nil {
		http.NotFound(w, r)
		return
	}
	// Не позволяем выйти из каталога.
	name := filepath.Base(parts[1])
	f, err := h.storage.Open(id, name)
	if err != nil {
		http.NotFound(w, r)
		return
	}
	defer f.Close()
	w.Header().Set("Cache-Control", "private, max-age=600")
	http.ServeContent(w, r, name, time.Time{}, f)
}
