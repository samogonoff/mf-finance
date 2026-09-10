package plans

import (
	"encoding/json"
	"net/http"
	"strconv"
	"strings"
)

// HTTP-контур формы 1.5. Ручки тонкие: разбор запроса, вызов сервиса, единая
// трансляция ошибок. Права проверяет сервис — дублировать нельзя, разойдутся.

// ExpHandler — ручки формы расходов ЦП.
type ExpHandler struct {
	svc  *ExpService
	prin PrincipalFunc
	rec  func(r *http.Request, action, entity string, id int64)
}

// NewExpHandler — конструктор.
func NewExpHandler(svc *ExpService, prin PrincipalFunc) *ExpHandler {
	return &ExpHandler{svc: svc, prin: prin}
}

// WithAuditRecorder подключает журналирование запросов.
func (h *ExpHandler) WithAuditRecorder(f func(r *http.Request, action, entity string, id int64)) *ExpHandler {
	h.rec = f
	return h
}

func (h *ExpHandler) principal(r *http.Request) Principal {
	if h.prin == nil {
		return Principal{}
	}
	p, _ := h.prin(r)
	return p
}

func (h *ExpHandler) audit(r *http.Request, action string, id int64) {
	if h.rec != nil {
		h.rec(r, action, "form_card", id)
	}
}

// expErr — трансляция ошибок сервиса в HTTP-код, как в retailErr.
func expErr(w http.ResponseWriter, err error) {
	msg := err.Error()
	switch {
	case strings.Contains(msg, "среза доступа") || strings.Contains(msg, "доступно финансисту"):
		writeErr(w, http.StatusForbidden, msg)
	case strings.Contains(msg, "no rows") || strings.Contains(msg, "не найдена") ||
		strings.Contains(msg, "не относится к форме"):
		writeErr(w, http.StatusNotFound, msg)
	default:
		writeErr(w, http.StatusBadRequest, msg)
	}
}

// expFilterFrom — отбор из query (ТЗ §7.1) и порция (ТЗ §7.10).
func expFilterFrom(r *http.Request) ExpFilter {
	q := r.URL.Query()
	atoi := func(k string) int { v, _ := strconv.Atoi(q.Get(k)); return v }
	return ExpFilter{
		Search:      strings.TrimSpace(q.Get("search")),
		GroupPL:     q.Get("group_pl"),
		CodePL:      atoi("code_pl"),
		CodeCFO:     atoi("code_cfo"),
		City:        q.Get("city"),
		LegalEntity: q.Get("legal_entity"),
		StoreType:   q.Get("store_type"),
		RegManager:  q.Get("reg_manager"),
		Offset:      atoi("offset"),
		Limit:       atoi("limit"),
	}
}

// FormGet — GET /api/plans/exp/{cardId}/form
func (h *ExpHandler) FormGet(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(w, r, "cardId")
	if !ok {
		return
	}
	form, err := h.svc.Form(r.Context(), h.principal(r), id, expFilterFrom(r))
	if err != nil {
		expErr(w, err)
		return
	}
	writeJSON(w, http.StatusOK, form)
}

// FormSave — PUT /api/plans/exp/{cardId}/form
// Тело: {cells: [{code_cfo, code_pl, month, amount, source, note}], comments: [...]}.
func (h *ExpHandler) FormSave(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(w, r, "cardId")
	if !ok {
		return
	}
	var body struct {
		Cells    []ExpCell `json:"cells"`
		Comments []struct {
			CodeCFO int    `json:"code_cfo"`
			CodePL  int    `json:"code_pl"`
			Comment string `json:"comment"`
		} `json:"comments"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeErr(w, http.StatusBadRequest, "invalid json")
		return
	}
	p := h.principal(r)
	n, err := h.svc.SaveCells(r.Context(), p, id, body.Cells)
	if err != nil {
		expErr(w, err)
		return
	}
	for _, c := range body.Comments {
		if err := h.svc.SaveComment(r.Context(), p, id, c.CodeCFO, c.CodePL, c.Comment); err != nil {
			expErr(w, err)
			return
		}
	}
	h.audit(r, "exp_form_save", id)
	writeJSON(w, http.StatusOK, map[string]int{"changed": n})
}

// BulkApply — POST /api/plans/exp/{cardId}/bulk (ТЗ §7.8).
func (h *ExpHandler) BulkApply(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(w, r, "cardId")
	if !ok {
		return
	}
	var req ExpBulkRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeErr(w, http.StatusBadRequest, "invalid json")
		return
	}
	n, err := h.svc.Bulk(r.Context(), h.principal(r), id, req)
	if err != nil {
		expErr(w, err)
		return
	}
	h.audit(r, "exp_bulk_"+req.Action, id)
	writeJSON(w, http.StatusOK, map[string]int{"changed": n})
}

// TotalsGet — GET /api/plans/exp/{cardId}/totals (ТЗ §7.6).
func (h *ExpHandler) TotalsGet(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(w, r, "cardId")
	if !ok {
		return
	}
	t, err := h.svc.Totals(r.Context(), h.principal(r), id)
	if err != nil {
		expErr(w, err)
		return
	}
	writeJSON(w, http.StatusOK, t)
}

// RegistryGet — GET /api/plans/exp/{cardId}/registry/{kind} (ТЗ §5).
func (h *ExpHandler) RegistryGet(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(w, r, "cardId")
	if !ok {
		return
	}
	reg, err := h.svc.Registry(r.Context(), h.principal(r), id, r.PathValue("kind"))
	if err != nil {
		expErr(w, err)
		return
	}
	writeJSON(w, http.StatusOK, reg)
}

// RegistrySave — PUT /api/plans/exp/{cardId}/registry/{kind}. Тело — строка
// реестра; id = 0 создаёт новую (ТЗ §5.5).
func (h *ExpHandler) RegistrySave(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(w, r, "cardId")
	if !ok {
		return
	}
	var row ExpRegRow
	if err := json.NewDecoder(r.Body).Decode(&row); err != nil {
		writeErr(w, http.StatusBadRequest, "invalid json")
		return
	}
	row.Kind = r.PathValue("kind")
	rowID, err := h.svc.SaveRegRow(r.Context(), h.principal(r), id, row)
	if err != nil {
		expErr(w, err)
		return
	}
	h.audit(r, "exp_reg_save_"+row.Kind, id)
	writeJSON(w, http.StatusOK, map[string]int64{"id": rowID})
}

// RegistryCopy — POST /api/plans/exp/{cardId}/registry/{kind}/{rowId}/copy.
func (h *ExpHandler) RegistryCopy(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(w, r, "cardId")
	if !ok {
		return
	}
	rowID, ok := pathID(w, r, "rowId")
	if !ok {
		return
	}
	newID, err := h.svc.CopyRegRow(r.Context(), h.principal(r), id, rowID)
	if err != nil {
		expErr(w, err)
		return
	}
	h.audit(r, "exp_reg_copy", id)
	writeJSON(w, http.StatusOK, map[string]int64{"id": newID})
}

// RegistryDelete — DELETE /api/plans/exp/{cardId}/registry/{kind}/{rowId}.
func (h *ExpHandler) RegistryDelete(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(w, r, "cardId")
	if !ok {
		return
	}
	rowID, ok := pathID(w, r, "rowId")
	if !ok {
		return
	}
	if err := h.svc.DeleteRegRow(r.Context(), h.principal(r), id, rowID); err != nil {
		expErr(w, err)
		return
	}
	h.audit(r, "exp_reg_delete", id)
	writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
}
