package plans

import (
	"encoding/json"
	"net/http"
	"strconv"
)

// LeHandler — HTTP свода «Бюджет по ЮЛ».
type LeHandler struct {
	svc  *LeSvodService
	prin PrincipalFunc
}

// NewLeHandler — конструктор.
func NewLeHandler(svc *LeSvodService, prin PrincipalFunc) *LeHandler {
	return &LeHandler{svc: svc, prin: prin}
}

func (h *LeHandler) principal(r *http.Request) Principal {
	if h.prin == nil {
		return Principal{}
	}
	p, _ := h.prin(r)
	return p
}

// Get — GET /api/plans/le-svod/{plId}?window=month|tail|year.
func (h *LeHandler) Get(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(w, r, "plId")
	if !ok {
		return
	}
	res, err := h.svc.Build(r.Context(), id, r.URL.Query().Get("window"))
	if err != nil {
		expErr(w, err)
		return
	}
	writeJSON(w, http.StatusOK, res)
}

type leAdjustBody struct {
	LegalEntity string  `json:"legal_entity"`
	LineKey     string  `json:"line_key"`
	AmountBYN   float64 `json:"amount_byn"`
	Reason      string  `json:"reason"`
}

// Adjust — POST /api/plans/le-svod/{plId}/adjust: корректировка итога (админ процессов).
func (h *LeHandler) Adjust(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(w, r, "plId")
	if !ok {
		return
	}
	p := h.principal(r)
	if !p.PlansAdmin {
		writeErr(w, http.StatusForbidden, "корректировать итоги свода может администратор процессов")
		return
	}
	var body leAdjustBody
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeErr(w, http.StatusBadRequest, "некорректное тело запроса")
		return
	}
	adjID, err := h.svc.Adjust(r.Context(), id, LeAdjust{
		LegalEntity: body.LegalEntity, LineKey: body.LineKey, AmountBYN: body.AmountBYN, Reason: body.Reason, UserID: p.UserID,
	})
	if err != nil {
		expErr(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"id": adjID})
}

// Revoke — DELETE /api/plans/le-svod/{plId}/adjust/{id}?reason=: снять корректировку.
func (h *LeHandler) Revoke(w http.ResponseWriter, r *http.Request) {
	plID, ok := pathID(w, r, "plId")
	if !ok {
		return
	}
	adjID, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil || adjID <= 0 {
		writeErr(w, http.StatusBadRequest, "invalid id")
		return
	}
	p := h.principal(r)
	if !p.PlansAdmin {
		writeErr(w, http.StatusForbidden, "снимать корректировки может администратор процессов")
		return
	}
	if err := h.svc.RevokeAdjust(r.Context(), plID, adjID, p.UserID, r.URL.Query().Get("reason")); err != nil {
		expErr(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true})
}
