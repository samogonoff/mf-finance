package plans

import (
	"encoding/json"
	"net/http"
)

// LeApprovalHandler — HTTP согласования ЮЛ (этап 3) и итогового утверждения (этап 4).
type LeApprovalHandler struct {
	svc  *LeApprovalService
	prin PrincipalFunc
}

// NewLeApprovalHandler — конструктор.
func NewLeApprovalHandler(svc *LeApprovalService, prin PrincipalFunc) *LeApprovalHandler {
	return &LeApprovalHandler{svc: svc, prin: prin}
}

func (h *LeApprovalHandler) principal(r *http.Request) Principal {
	if h.prin == nil {
		return Principal{}
	}
	p, _ := h.prin(r)
	return p
}

// Status — GET /api/plans/le-approval/{plId}.
func (h *LeApprovalHandler) Status(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(w, r, "plId")
	if !ok {
		return
	}
	res, err := h.svc.Status(r.Context(), id, h.principal(r))
	if err != nil {
		expErr(w, err)
		return
	}
	writeJSON(w, http.StatusOK, res)
}

type leDecideBody struct {
	StageCode   string `json:"stage_code"`
	LegalEntity string `json:"legal_entity"`
	Decision    string `json:"decision"`
	Comment     string `json:"comment"`
}

// Decide — POST /api/plans/le-approval/{plId}/decide: согласовать или вернуть (только назначенный согласующий).
func (h *LeApprovalHandler) Decide(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(w, r, "plId")
	if !ok {
		return
	}
	var body leDecideBody
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeErr(w, http.StatusBadRequest, "некорректное тело запроса")
		return
	}
	if err := h.svc.Decide(r.Context(), id, h.principal(r), body.StageCode, body.LegalEntity, body.Decision, body.Comment); err != nil {
		expErr(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true})
}

// Config — GET /api/plans/le-approvers: списки согласующих и политики (настройка, администратор).
func (h *LeApprovalHandler) Config(w http.ResponseWriter, r *http.Request) {
	res, err := h.svc.Config(r.Context())
	if err != nil {
		expErr(w, err)
		return
	}
	writeJSON(w, http.StatusOK, res)
}

type leApproversBody struct {
	StageCode   string  `json:"stage_code"`
	LegalEntity string  `json:"legal_entity"`
	UserIDs     []int64 `json:"user_ids"`
	RequireAll  bool    `json:"require_all"`
}

// SetApprovers — PUT /api/plans/le-approvers: заменить список согласующих области и политику.
func (h *LeApprovalHandler) SetApprovers(w http.ResponseWriter, r *http.Request) {
	var body leApproversBody
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeErr(w, http.StatusBadRequest, "некорректное тело запроса")
		return
	}
	p := h.principal(r)
	if err := h.svc.SetApprovers(r.Context(), p, body.StageCode, body.LegalEntity, body.UserIDs, body.RequireAll); err != nil {
		if !p.PlansAdmin {
			writeErr(w, http.StatusForbidden, err.Error())
			return
		}
		expErr(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true})
}
