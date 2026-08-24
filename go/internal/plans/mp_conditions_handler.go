package plans

import (
	"encoding/json"
	"net/http"
)

// HTTP-контур реестра условий площадки, общих затрат и пересчёта расходной части.

// MpConditionsHandler — ручки формы МП по новому ТЗ.
type MpConditionsHandler struct {
	svc  *MpConditionsService
	prin PrincipalFunc
	rec  func(r *http.Request, action, entity string, id int64)
}

// NewMpConditionsHandler — конструктор.
func NewMpConditionsHandler(svc *MpConditionsService, prin PrincipalFunc) *MpConditionsHandler {
	return &MpConditionsHandler{svc: svc, prin: prin}
}

// WithAuditRecorder подключает журналирование запросов.
func (h *MpConditionsHandler) WithAuditRecorder(f func(r *http.Request, action, entity string, id int64)) *MpConditionsHandler {
	h.rec = f
	return h
}

func (h *MpConditionsHandler) principal(r *http.Request) Principal {
	if h.prin == nil {
		return Principal{}
	}
	p, _ := h.prin(r)
	return p
}

// ConditionsGet — GET /api/plans/mp/cards/{cardId}/conditions.
// Реестр условий периода: значения, diff к прошлому периоду и подсказки
// «фактическая доля прошлого месяца» (ТЗ §3.1 — в реестр они не переносятся).
func (h *MpConditionsHandler) ConditionsGet(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(w, r, "cardId")
	if !ok {
		return
	}
	view, err := h.svc.Conditions(r.Context(), id)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, view)
}

// ConditionsSave — PUT /api/plans/mp/cards/{cardId}/conditions.
// Тело: условия одной площадки. Обоснование обязательно при отклонении сверх
// порога от прошлого периода (ТЗ §6.1).
func (h *MpConditionsHandler) ConditionsSave(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(w, r, "cardId")
	if !ok {
		return
	}
	var body MpConditions
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeErr(w, http.StatusBadRequest, "invalid json")
		return
	}
	p := h.principal(r)
	saved, err := h.svc.SaveConditions(r.Context(), id, body, p.UserID)
	if err != nil {
		writeErr(w, http.StatusBadRequest, err.Error())
		return
	}
	if h.rec != nil {
		h.rec(r, "mp_conditions_save", "mp_conditions", saved.ID)
	}
	writeJSON(w, http.StatusOK, saved)
}

// ConditionsCopy — POST /api/plans/mp/cards/{cardId}/conditions/copy.
// «Условия копируются из предыдущего периода одним действием» (ТЗ §6.1).
func (h *MpConditionsHandler) ConditionsCopy(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(w, r, "cardId")
	if !ok {
		return
	}
	p := h.principal(r)
	n, err := h.svc.CopyConditionsFromPrev(r.Context(), id, p)
	if err != nil {
		writeErr(w, http.StatusBadRequest, err.Error())
		return
	}
	if h.rec != nil {
		h.rec(r, "mp_conditions_copy", "form_card", id)
	}
	writeJSON(w, http.StatusOK, map[string]any{"copied": n})
}

// Recalc — POST /api/plans/mp/cards/{cardId}/recalc?preview=1.
// Пересчёт расходной части от продаж и условий. preview=1 — предпросмотр без
// сохранения (обязателен при изменении условий, ТЗ §7.2).
func (h *MpConditionsHandler) Recalc(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(w, r, "cardId")
	if !ok {
		return
	}
	preview := r.URL.Query().Get("preview") == "1"
	p := h.principal(r)
	res, err := h.svc.Recalc(r.Context(), id, preview, p.UserID)
	if err != nil {
		writeErr(w, http.StatusBadRequest, err.Error())
		return
	}
	if h.rec != nil && !preview {
		h.rec(r, "mp_recalc", "form_card", id)
	}
	writeJSON(w, http.StatusOK, res)
}

// Validate — GET /api/plans/mp/cards/{cardId}/validate.
// Отчёт валидаций МП-01..11 / W1..W8 без сохранения: им гейтится кнопка
// «Отправить на согласование».
func (h *MpConditionsHandler) Validate(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(w, r, "cardId")
	if !ok {
		return
	}
	res, err := h.svc.Recalc(r.Context(), id, true, h.principal(r).UserID)
	if err != nil {
		writeErr(w, http.StatusBadRequest, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"issues":       res.Issues,
		"blocking":     res.Blocking,
		"needs_reason": NeedsExplanation(res.Issues),
		"can_submit":   !res.Blocking && len(NeedsExplanation(res.Issues)) == 0,
	})
}

// CommonCostsGet — GET /api/plans/mp/cards/{cardId}/common-costs.
// Общие затраты по МП: 7 групп статей PL (ТЗ §4.3).
func (h *MpConditionsHandler) CommonCostsGet(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(w, r, "cardId")
	if !ok {
		return
	}
	rows, spec, err := h.svc.CommonCosts(r.Context(), id)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	if rows == nil {
		rows = []MpCommonCost{}
	}
	writeJSON(w, http.StatusOK, map[string]any{"groups": spec, "values": rows})
}

// CommonCostsSave — PUT /api/plans/mp/cards/{cardId}/common-costs.
func (h *MpConditionsHandler) CommonCostsSave(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(w, r, "cardId")
	if !ok {
		return
	}
	var body struct {
		Values []MpCommonCost `json:"values"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeErr(w, http.StatusBadRequest, "invalid json")
		return
	}
	p := h.principal(r)
	if err := h.svc.SaveCommonCosts(r.Context(), id, body.Values, p.UserID); err != nil {
		writeErr(w, http.StatusBadRequest, err.Error())
		return
	}
	if h.rec != nil {
		h.rec(r, "mp_common_cost_save", "form_card", id)
	}
	rows, spec, _ := h.svc.CommonCosts(r.Context(), id)
	writeJSON(w, http.StatusOK, map[string]any{"groups": spec, "values": rows})
}
