package notifications

import (
	"encoding/json"
	"net/http"
	"strconv"
	"strings"

	"github.com/company/finance-api/internal/auth"
)

type Handler struct {
	svc  *Service
	repo *Repo
}

func NewHandler(svc *Service, repo *Repo) *Handler { return &Handler{svc: svc, repo: repo} }

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

func writeErr(w http.ResponseWriter, status int, msg string) {
	writeJSON(w, status, map[string]string{"error": msg})
}

// List — GET /api/notifications?limit=&offset=&unread_only=
func (h *Handler) List(w http.ResponseWriter, r *http.Request) {
	u, _ := auth.UserFromCtx(r.Context())
	if u == nil {
		writeErr(w, http.StatusUnauthorized, "no session")
		return
	}
	q := r.URL.Query()
	limit, _ := strconv.Atoi(q.Get("limit"))
	offset, _ := strconv.Atoi(q.Get("offset"))
	unread := q.Get("unread_only") == "1" || strings.EqualFold(q.Get("unread_only"), "true")

	items, total, err := h.repo.FindByUserPaginated(r.Context(), u.ID, unread, limit, offset)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"data":  items,
		"total": total,
	})
}

// UnreadCount — GET /api/notifications/unread-count
func (h *Handler) UnreadCount(w http.ResponseWriter, r *http.Request) {
	u, _ := auth.UserFromCtx(r.Context())
	if u == nil {
		writeErr(w, http.StatusUnauthorized, "no session")
		return
	}
	n, err := h.repo.CountUnread(r.Context(), u.ID)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]int{"count": n})
}

// Read — POST /api/notifications/{id}/read
func (h *Handler) Read(w http.ResponseWriter, r *http.Request) {
	u, _ := auth.UserFromCtx(r.Context())
	if u == nil {
		writeErr(w, http.StatusUnauthorized, "no session")
		return
	}
	idStr := r.PathValue("id")
	id, err := strconv.ParseInt(idStr, 10, 64)
	if err != nil {
		writeErr(w, http.StatusBadRequest, "invalid id")
		return
	}
	if err := h.repo.MarkAsRead(r.Context(), id, u.ID); err != nil {
		writeErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
}

// ReadAll — POST /api/notifications/read-all
func (h *Handler) ReadAll(w http.ResponseWriter, r *http.Request) {
	u, _ := auth.UserFromCtx(r.Context())
	if u == nil {
		writeErr(w, http.StatusUnauthorized, "no session")
		return
	}
	if err := h.repo.MarkAllAsRead(r.Context(), u.ID); err != nil {
		writeErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
}

// GetAccountSettings — GET /api/account/notification-settings
func (h *Handler) GetAccountSettings(w http.ResponseWriter, r *http.Request) {
	u, _ := auth.UserFromCtx(r.Context())
	if u == nil {
		writeErr(w, http.StatusUnauthorized, "no session")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"notify_via_b24":      u.NotifyViaB24,
		"b24_delivery_active": h.svc.B24Enabled(),
		"has_b24_id":          u.B24ID != nil,
	})
}

// PatchAccountSettings — PATCH /api/account/notification-settings
// body: { "notify_via_b24": bool }
func (h *Handler) PatchAccountSettings(usersRepo *auth.UserRepo) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		u, _ := auth.UserFromCtx(r.Context())
		if u == nil {
			writeErr(w, http.StatusUnauthorized, "no session")
			return
		}
		var body struct {
			NotifyViaB24 *bool `json:"notify_via_b24"`
		}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			writeErr(w, http.StatusBadRequest, "invalid body")
			return
		}
		if body.NotifyViaB24 != nil {
			if err := usersRepo.UpdateNotifyViaB24(r.Context(), u.ID, *body.NotifyViaB24); err != nil {
				writeErr(w, http.StatusInternalServerError, err.Error())
				return
			}
		}
		writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
	}
}
