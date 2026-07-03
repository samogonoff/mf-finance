package plans

import (
	"encoding/json"
	"net/http"
)

// HTTP-ручки должностей (Должности): CRUD + назначение покрытия ЦФО пикером и
// массово по фильтру. Все мутации — за ROLE_PLANS_ADMIN (роутинг в main.go).

// JobPositionsList — GET /api/plans/jobpos.
func (h *Handler) JobPositionsList(w http.ResponseWriter, r *http.Request) {
	if h.jobpos == nil {
		writeJSON(w, http.StatusOK, []JobPosition{})
		return
	}
	list, err := h.jobpos.List(r.Context())
	if err != nil {
		writeErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, list)
}

// JobPositionUpsert — PUT /api/plans/jobpos. {id?, title, holder_user_id, description}.
func (h *Handler) JobPositionUpsert(w http.ResponseWriter, r *http.Request) {
	if h.jobpos == nil {
		writeErr(w, http.StatusServiceUnavailable, "должности недоступны")
		return
	}
	var body struct {
		ID           int64  `json:"id"`
		Title        string `json:"title"`
		Kind         string `json:"kind"`
		HolderUserID int64  `json:"holder_user_id"`
		Description  string `json:"description"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeErr(w, http.StatusBadRequest, "invalid json")
		return
	}
	if body.Title == "" {
		writeErr(w, http.StatusBadRequest, "title обязателен")
		return
	}
	id, err := h.jobpos.Upsert(r.Context(), body.ID, body.Title, body.Kind, body.HolderUserID, body.Description)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	h.rec(r, "jobpos_upsert", "jobpos", id)
	writeJSON(w, http.StatusOK, map[string]int64{"id": id})
}

// JobPositionDelete — DELETE /api/plans/jobpos/{id}.
func (h *Handler) JobPositionDelete(w http.ResponseWriter, r *http.Request) {
	if h.jobpos == nil {
		writeErr(w, http.StatusServiceUnavailable, "должности недоступны")
		return
	}
	if err := h.jobpos.Delete(r.Context(), parseInt64(r.PathValue("id"))); err != nil {
		writeErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	h.rec(r, "jobpos_delete", "jobpos", 0)
	writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
}

// JobPositionCfo — GET /api/plans/jobpos/{id}/cfo. Коды ЦФО должности.
func (h *Handler) JobPositionCfo(w http.ResponseWriter, r *http.Request) {
	if h.jobpos == nil {
		writeJSON(w, http.StatusOK, []string{})
		return
	}
	codes, err := h.jobpos.CfoCodes(r.Context(), parseInt64(r.PathValue("id")))
	if err != nil {
		writeErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, codes)
}

// JobPositionAssignCfo — PUT /api/plans/jobpos/{id}/cfo. {codes:[...]} (пикер) или {filter:{...}}.
func (h *Handler) JobPositionAssignCfo(w http.ResponseWriter, r *http.Request) {
	if h.jobpos == nil {
		writeErr(w, http.StatusServiceUnavailable, "должности недоступны")
		return
	}
	id := parseInt64(r.PathValue("id"))
	var body struct {
		Codes  []string   `json:"codes"`
		Filter *CfoFilter `json:"filter"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeErr(w, http.StatusBadRequest, "invalid json")
		return
	}
	n := len(body.Codes)
	var err error
	if body.Filter != nil {
		n, err = h.jobpos.AssignByFilter(r.Context(), id, *body.Filter)
	} else {
		err = h.jobpos.AssignCfo(r.Context(), id, body.Codes)
	}
	if err != nil {
		writeErr(w, http.StatusBadRequest, err.Error())
		return
	}
	if h.cache != nil {
		_ = h.cache.Invalidate(r.Context(), "dir_cfo")
	}
	h.rec(r, "jobpos_assign_cfo", "jobpos", id)
	writeJSON(w, http.StatusOK, map[string]int{"assigned": n})
}

// JobPositionUnassignCfo — DELETE /api/plans/jobpos/cfo/{code}. Снять покрытие ЦФО.
func (h *Handler) JobPositionUnassignCfo(w http.ResponseWriter, r *http.Request) {
	if h.jobpos == nil {
		writeErr(w, http.StatusServiceUnavailable, "должности недоступны")
		return
	}
	if err := h.jobpos.UnassignCfo(r.Context(), r.PathValue("code")); err != nil {
		writeErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	if h.cache != nil {
		_ = h.cache.Invalidate(r.Context(), "dir_cfo")
	}
	writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
}
