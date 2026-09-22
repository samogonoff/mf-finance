package plans

import (
	"encoding/json"
	"io"
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
		Sort:        q.Get("sort"),
		Desc:        q.Get("dir") == "desc",
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

// ValidateGet — GET /api/plans/exp/{cardId}/validate (ТЗ §8 и §9).
func (h *ExpHandler) ValidateGet(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(w, r, "cardId")
	if !ok {
		return
	}
	rep, err := h.svc.Validate(r.Context(), h.principal(r), id)
	if err != nil {
		expErr(w, err)
		return
	}
	writeJSON(w, http.StatusOK, rep)
}

// Export — GET /api/plans/exp/{cardId}/export (ТЗ §7.9). Выгружается ТЕКУЩАЯ
// выборка: те же фильтры, что у формы.
func (h *ExpHandler) Export(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(w, r, "cardId")
	if !ok {
		return
	}
	buf, name, err := h.svc.ExportForm(r.Context(), h.principal(r), id, expFilterFrom(r))
	if err != nil {
		expErr(w, err)
		return
	}
	h.audit(r, "exp_export", id)
	expXlsx(w, name, buf)
}

// ImportTemplate — GET /api/plans/exp/{cardId}/import-template (ТЗ §7.9).
func (h *ExpHandler) ImportTemplate(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(w, r, "cardId")
	if !ok {
		return
	}
	buf, name, err := h.svc.ImportTemplate(r.Context(), h.principal(r), id)
	if err != nil {
		expErr(w, err)
		return
	}
	expXlsx(w, name, buf)
}

// expXlsx — отдача .xlsx файлом.
func expXlsx(w http.ResponseWriter, name string, buf []byte) {
	w.Header().Set("Content-Type", "application/vnd.openxmlformats-officedocument.spreadsheetml.sheet")
	w.Header().Set("Content-Disposition", `attachment; filename="`+name+`"`)
	_, _ = w.Write(buf)
}

// Import — POST /api/plans/exp/{cardId}/import?preview=1 (ТЗ §7.9).
// Предпросмотр обязателен по ТЗ, поэтому это отдельный режим одной ручки, а не
// отдельный эндпоинт: разойтись они не должны.
func (h *ExpHandler) Import(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(w, r, "cardId")
	if !ok {
		return
	}
	data, err := io.ReadAll(http.MaxBytesReader(w, r.Body, 32<<20))
	if err != nil {
		writeErr(w, http.StatusBadRequest, "файл не прочитан")
		return
	}
	preview := r.URL.Query().Get("preview") == "1"
	res, err := h.svc.ImportForm(r.Context(), h.principal(r), id, data, preview)
	if err != nil {
		expErr(w, err)
		return
	}
	if !preview {
		h.audit(r, "exp_import", id)
	}
	writeJSON(w, http.StatusOK, res)
}

// SummaryGet — GET /api/plans/exp/{cardId}/summary (ТЗ §8 и §12.3).
func (h *ExpHandler) SummaryGet(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(w, r, "cardId")
	if !ok {
		return
	}
	sum, err := h.svc.Summary(r.Context(), h.principal(r), id)
	if err != nil {
		expErr(w, err)
		return
	}
	writeJSON(w, http.StatusOK, sum)
}

// ParamsGet — GET /api/plans/exp/{cardId}/params (ТЗ §6.1).
func (h *ExpHandler) ParamsGet(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(w, r, "cardId")
	if !ok {
		return
	}
	rows, err := h.svc.Params(r.Context(), h.principal(r), id)
	if err != nil {
		expErr(w, err)
		return
	}
	writeJSON(w, http.StatusOK, rows)
}

// ParamsSave — PUT /api/plans/exp/{cardId}/params.
func (h *ExpHandler) ParamsSave(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(w, r, "cardId")
	if !ok {
		return
	}
	var row ExpParamRow
	if err := json.NewDecoder(r.Body).Decode(&row); err != nil {
		writeErr(w, http.StatusBadRequest, "invalid json")
		return
	}
	if err := h.svc.SaveParam(r.Context(), h.principal(r), id, row); err != nil {
		expErr(w, err)
		return
	}
	h.audit(r, "exp_param_save", id)
	writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
}

// CalcApply — POST /api/plans/exp/{cardId}/calc (ТЗ §6). preview=true в теле
// ничего не сохраняет и возвращает предпросмотр.
func (h *ExpHandler) CalcApply(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(w, r, "cardId")
	if !ok {
		return
	}
	var req ExpCalcRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeErr(w, http.StatusBadRequest, "invalid json")
		return
	}
	res, err := h.svc.Calc(r.Context(), h.principal(r), id, req)
	if err != nil {
		expErr(w, err)
		return
	}
	if !req.Preview {
		h.audit(r, "exp_calc_"+req.Op, id)
	}
	writeJSON(w, http.StatusOK, res)
}
