package plans

import (
	"encoding/json"
	"io"
	"net/http"
	"strconv"
	"strings"
)

// HTTP-контур формы «Розница» (ТЗ §12). Ручки тонкие: разбор запроса, вызов
// сервиса, единая обработка ошибок. Права проверяет сервис (там же ABAC/РМ §9) —
// дублировать проверку в хендлере нельзя, иначе они разойдутся.

// RetailHandler — ручки формы.
type RetailHandler struct {
	svc  *RetailService
	prin PrincipalFunc
	rec  func(r *http.Request, action, entity string, id int64)
}

// NewRetailHandler — конструктор.
func NewRetailHandler(svc *RetailService, prin PrincipalFunc) *RetailHandler {
	return &RetailHandler{svc: svc, prin: prin}
}

// WithAuditRecorder подключает журналирование запросов (как в CardHandler).
func (h *RetailHandler) WithAuditRecorder(f func(r *http.Request, action, entity string, id int64)) *RetailHandler {
	h.rec = f
	return h
}

func (h *RetailHandler) principal(r *http.Request) Principal {
	if h.prin == nil {
		return Principal{}
	}
	p, _ := h.prin(r)
	return p
}

func (h *RetailHandler) audit(r *http.Request, action string, id int64) {
	if h.rec != nil {
		h.rec(r, action, "form_card", id)
	}
}

// retailErr — единая трансляция ошибок сервиса в HTTP-код. Отказ по доступу — 403
// (сообщения сервиса про «срез доступа»), закрытый период и валидации — 400,
// отсутствие карточки — 404.
func retailErr(w http.ResponseWriter, err error) {
	msg := err.Error()
	switch {
	case strings.Contains(msg, "среза доступа") || strings.Contains(msg, "доступно финансисту") ||
		strings.Contains(msg, "доступно только финансисту") || strings.Contains(msg, "задаёт финансист"):
		writeErr(w, http.StatusForbidden, msg)
	case strings.Contains(msg, "no rows") || strings.Contains(msg, "не найдена"):
		writeErr(w, http.StatusNotFound, "карточка формы не найдена")
	default:
		writeErr(w, http.StatusBadRequest, msg)
	}
}

// FormGet — GET /api/plans/retail/{cardId}/form?currency=BYN|USD
func (h *RetailHandler) FormGet(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(w, r, "cardId")
	if !ok {
		return
	}
	form, err := h.svc.Form(r.Context(), h.principal(r), id, r.URL.Query().Get("currency"))
	if err != nil {
		retailErr(w, err)
		return
	}
	writeJSON(w, http.StatusOK, form)
}

// FormSave — PUT /api/plans/retail/{cardId}/form
// Тело: {cells: [{code_cfo, metric, year, month, amount, source, note}], comments: [...]}.
func (h *RetailHandler) FormSave(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(w, r, "cardId")
	if !ok {
		return
	}
	var req RetailSaveRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeErr(w, http.StatusBadRequest, "invalid json")
		return
	}
	if len(req.Cells) == 0 && len(req.Comments) == 0 {
		writeErr(w, http.StatusBadRequest, "нечего сохранять: нет ни ячеек, ни комментариев")
		return
	}
	form, err := h.svc.SaveForm(r.Context(), h.principal(r), id, req)
	if err != nil {
		retailErr(w, err)
		return
	}
	h.audit(r, "retail_form_save", id)
	writeJSON(w, http.StatusOK, form)
}

// BulkApply — POST /api/plans/retail/{cardId}/bulk
// При preview=true НИЧЕГО не сохраняется: ответ — diff «было → будет» и счётчики
// (сколько ячеек изменится, сколько защищено manual) — ТЗ §5.
func (h *RetailHandler) BulkApply(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(w, r, "cardId")
	if !ok {
		return
	}
	var req RetailBulkRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeErr(w, http.StatusBadRequest, "invalid json")
		return
	}
	res, err := h.svc.Bulk(r.Context(), h.principal(r), id, req)
	if err != nil {
		retailErr(w, err)
		return
	}
	if !req.Preview {
		h.audit(r, "retail_bulk_"+req.Op, id)
	}
	writeJSON(w, http.StatusOK, res)
}

// ParamsGet — GET /api/plans/retail/{cardId}/params
func (h *RetailHandler) ParamsGet(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(w, r, "cardId")
	if !ok {
		return
	}
	list, err := h.svc.Params(r.Context(), h.principal(r), id)
	if err != nil {
		retailErr(w, err)
		return
	}
	if list == nil {
		list = []RetailParam{}
	}
	writeJSON(w, http.StatusOK, list)
}

// ParamsSave — PUT /api/plans/retail/{cardId}/params. Тело: массив параметров
// либо {params: [...]} — принимаем оба вида, чтобы фронт не подстраивался.
func (h *RetailHandler) ParamsSave(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(w, r, "cardId")
	if !ok {
		return
	}
	raw, err := io.ReadAll(r.Body)
	if err != nil {
		writeErr(w, http.StatusBadRequest, "не удалось прочитать тело запроса")
		return
	}
	var params []RetailParam
	if err := json.Unmarshal(raw, &params); err != nil {
		var wrapped struct {
			Params []RetailParam `json:"params"`
		}
		if err2 := json.Unmarshal(raw, &wrapped); err2 != nil {
			writeErr(w, http.StatusBadRequest, "invalid json")
			return
		}
		params = wrapped.Params
	}
	if len(params) == 0 {
		writeErr(w, http.StatusBadRequest, "не переданы параметры периода")
		return
	}
	list, err := h.svc.SaveParams(r.Context(), h.principal(r), id, params)
	if err != nil {
		retailErr(w, err)
		return
	}
	h.audit(r, "retail_params_save", id)
	writeJSON(w, http.StatusOK, list)
}

// LFLOverride — PUT /api/plans/retail/{cardId}/lfl-override
// Тело: {code_cfo, lfl_status, reason}. Пустой lfl_status снимает переопределение.
func (h *RetailHandler) LFLOverride(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(w, r, "cardId")
	if !ok {
		return
	}
	var body struct {
		CodeCFO   int    `json:"code_cfo"`
		LFLStatus string `json:"lfl_status"`
		Reason    string `json:"reason"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeErr(w, http.StatusBadRequest, "invalid json")
		return
	}
	if body.CodeCFO == 0 {
		writeErr(w, http.StatusBadRequest, "не указан code_cfo магазина")
		return
	}
	if err := h.svc.SetLFLOverride(r.Context(), h.principal(r), id, body.CodeCFO, body.LFLStatus, body.Reason); err != nil {
		retailErr(w, err)
		return
	}
	h.audit(r, "retail_lfl_override", id)
	form, err := h.svc.Form(r.Context(), h.principal(r), id, "")
	if err != nil {
		retailErr(w, err)
		return
	}
	writeJSON(w, http.StatusOK, form)
}

// SummaryGet — GET /api/plans/retail/{cardId}/summary (экран согласования, §7/§8).
func (h *RetailHandler) SummaryGet(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(w, r, "cardId")
	if !ok {
		return
	}
	sum, err := h.svc.Summary(r.Context(), h.principal(r), id)
	if err != nil {
		retailErr(w, err)
		return
	}
	writeJSON(w, http.StatusOK, sum)
}

// ValidateGet — GET /api/plans/retail/{cardId}/validate (отчёт V-*/W-*, §6).
func (h *RetailHandler) ValidateGet(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(w, r, "cardId")
	if !ok {
		return
	}
	rep, err := h.svc.Validate(r.Context(), h.principal(r), id)
	if err != nil {
		retailErr(w, err)
		return
	}
	writeJSON(w, http.StatusOK, rep)
}

// Export — GET /api/plans/retail/{cardId}/export?currency=
func (h *RetailHandler) Export(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(w, r, "cardId")
	if !ok {
		return
	}
	data, name, err := h.svc.ExportForm(r.Context(), h.principal(r), id, r.URL.Query().Get("currency"))
	if err != nil {
		retailErr(w, err)
		return
	}
	w.Header().Set("Content-Type", "application/vnd.openxmlformats-officedocument.spreadsheetml.sheet")
	w.Header().Set("Content-Disposition", `attachment; filename="`+name+`"`)
	w.Header().Set("Content-Length", strconv.Itoa(len(data)))
	_, _ = w.Write(data)
}

// retailImportMaxBytes — предел размера загружаемого файла. 375 магазинов × 12
// месяцев в минимальном xlsx — сотни килобайт; 16 МБ с запасом, но не «сколько
// пришлют».
const retailImportMaxBytes = 16 << 20

// Import — POST /api/plans/retail/{cardId}/import (multipart: file, либо тело=xlsx).
func (h *RetailHandler) Import(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(w, r, "cardId")
	if !ok {
		return
	}
	var data []byte
	if strings.HasPrefix(r.Header.Get("Content-Type"), "multipart/") {
		if err := r.ParseMultipartForm(retailImportMaxBytes); err != nil {
			writeErr(w, http.StatusBadRequest, "не удалось разобрать multipart-форму")
			return
		}
		f, _, err := r.FormFile("file")
		if err != nil {
			writeErr(w, http.StatusBadRequest, "в форме нет файла (поле file)")
			return
		}
		defer f.Close()
		data, err = io.ReadAll(io.LimitReader(f, retailImportMaxBytes))
		if err != nil {
			writeErr(w, http.StatusBadRequest, "не удалось прочитать файл")
			return
		}
	} else {
		var err error
		data, err = io.ReadAll(io.LimitReader(r.Body, retailImportMaxBytes))
		if err != nil {
			writeErr(w, http.StatusBadRequest, "не удалось прочитать тело запроса")
			return
		}
	}
	if len(data) == 0 {
		writeErr(w, http.StatusBadRequest, "пустой файл")
		return
	}
	res, err := h.svc.ImportForm(r.Context(), h.principal(r), id, data)
	if err != nil {
		retailErr(w, err)
		return
	}
	h.audit(r, "retail_import", id)
	writeJSON(w, http.StatusOK, res)
}
