package bugtracker

import (
	"encoding/json"
	"net/http"
	"strconv"
	"strings"
)

// Сервисный доступ к баг-трекеру для внешнего агента (Orca/Hermes).
// Эталон: MP src/Controller/Api/Core/CoreBugTrackerController.php.
//
// Зачем отдельно от /api/bugtracker (админка): та ручка ходит под Bearer-токеном
// ROLE_ADMIN, а у автоматизации нет пользователя — вход в кабинет только через
// B24 OAuth. Поэтому ручки живут под /internal/bugtracker/* за тем же
// internalapi.RequireToken (X-Internal-Token = INTERNAL_SERVICE_TOKEN; пустой
// токен → 503, fail-closed). Регистрация — в cmd/api/main.go.

func decodeOptional(r *http.Request, dst any) error {
	if r.ContentLength == 0 {
		return nil
	}
	return json.NewDecoder(r.Body).Decode(dst)
}

func writeList(w http.ResponseWriter, items []Report) {
	writeJSON(w, http.StatusOK, map[string]any{"data": items, "total": len(items)})
}

func queryInt(r *http.Request, key string, def int) int {
	if n, err := strconv.Atoi(r.URL.Query().Get(key)); err == nil {
		return n
	}
	return def
}

// InternalQueue — GET /internal/bugtracker/queue?limit=10&sections=plans,cost
func (h *Handler) InternalQueue(w http.ResponseWriter, r *http.Request) {
	var sections []string
	if v := r.URL.Query().Get("sections"); v != "" {
		sections = strings.Split(v, ",")
	}
	items, err := h.svc.Queue(r.Context(), queryInt(r, "limit", 10), sections)
	if err != nil {
		writeSvcErr(w, err)
		return
	}
	writeList(w, items)
}

// InternalGet — GET /internal/bugtracker/report/{id}: полный технический контекст.
func (h *Handler) InternalGet(w http.ResponseWriter, r *http.Request) {
	h.AdminGet(w, r)
}

// InternalSources — GET /internal/bugtracker/sources: активные причины, которыми
// агент помечает баг уже при разборе (вместе с MR).
func (h *Handler) InternalSources(w http.ResponseWriter, r *http.Request) {
	list, err := h.repo.ListSources(r.Context(), true)
	if err != nil {
		writeSvcErr(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"data": list})
}

// InternalCreate — POST /internal/bugtracker/report: находка мониторинга.
// 201 — заведён новый баг; 200 + duplicate=true — такая незакрытая проблема уже есть.
func (h *Handler) InternalCreate(w http.ResponseWriter, r *http.Request) {
	var in AutoCreateInput
	if err := json.NewDecoder(r.Body).Decode(&in); err != nil {
		writeErr(w, http.StatusBadRequest, "invalid body")
		return
	}
	rep, dup, err := h.svc.CreateAuto(r.Context(), in)
	if err != nil {
		writeSvcErr(w, err)
		return
	}
	status := http.StatusCreated
	if dup {
		status = http.StatusOK
	}
	writeJSON(w, status, map[string]any{"duplicate": dup, "data": rep})
}

// InternalClaim — POST /internal/bugtracker/report/{id}/claim {"by": "hermes"}.
// 409, если баг уже взят или уже не new.
func (h *Handler) InternalClaim(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(r, "id")
	if !ok {
		writeErr(w, http.StatusBadRequest, "invalid id")
		return
	}
	var body struct {
		By string `json:"by"`
	}
	if err := decodeOptional(r, &body); err != nil {
		writeErr(w, http.StatusBadRequest, "invalid body")
		return
	}
	rep, err := h.svc.Claim(r.Context(), id, body.By)
	if err != nil {
		writeSvcErr(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"data": rep})
}

// InternalMR — POST /internal/bugtracker/report/{id}/mr
// {"mr_iid": 123, "mr_url": "...", "summary": "что сделано", "source_id": 3}
func (h *Handler) InternalMR(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(r, "id")
	if !ok {
		writeErr(w, http.StatusBadRequest, "invalid id")
		return
	}
	var in MRInput
	if err := json.NewDecoder(r.Body).Decode(&in); err != nil {
		writeErr(w, http.StatusBadRequest, "invalid body")
		return
	}
	rep, err := h.svc.AttachMR(r.Context(), id, in)
	if err != nil {
		writeSvcErr(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"data": rep})
}

// InternalRelease — POST /internal/bugtracker/report/{id}/release {"reason": "..."}
func (h *Handler) InternalRelease(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(r, "id")
	if !ok {
		writeErr(w, http.StatusBadRequest, "invalid id")
		return
	}
	var body struct {
		Reason string `json:"reason"`
	}
	if err := decodeOptional(r, &body); err != nil {
		writeErr(w, http.StatusBadRequest, "invalid body")
		return
	}
	rep, err := h.svc.Release(r.Context(), id, body.Reason)
	if err != nil {
		writeSvcErr(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"data": rep})
}

// InternalResolve — POST /internal/bugtracker/report/{id}/resolve {"source_id": 3, "comment": "..."}
func (h *Handler) InternalResolve(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(r, "id")
	if !ok {
		writeErr(w, http.StatusBadRequest, "invalid id")
		return
	}
	var in ResolveInput
	if err := decodeOptional(r, &in); err != nil {
		writeErr(w, http.StatusBadRequest, "invalid body")
		return
	}
	rep, err := h.svc.Resolve(r.Context(), id, in)
	if err != nil {
		writeSvcErr(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"data": rep})
}

// InternalPendingMR — GET /internal/bugtracker/pending-mr?limit=20: «что ждёт выката».
func (h *Handler) InternalPendingMR(w http.ResponseWriter, r *http.Request) {
	items, err := h.svc.PendingMR(r.Context(), queryInt(r, "limit", 20))
	if err != nil {
		writeSvcErr(w, err)
		return
	}
	writeList(w, items)
}

// InternalByMR — GET /internal/bugtracker/by-mr/{iid}: какие баги закрывает MR
// (бот зовёт после успешного мерджа, чтобы закрыть их resolve'ом).
func (h *Handler) InternalByMR(w http.ResponseWriter, r *http.Request) {
	iid, ok := pathID(r, "iid")
	if !ok {
		writeErr(w, http.StatusBadRequest, "invalid iid")
		return
	}
	items, err := h.svc.ByMR(r.Context(), int(iid))
	if err != nil {
		writeSvcErr(w, err)
		return
	}
	writeList(w, items)
}
