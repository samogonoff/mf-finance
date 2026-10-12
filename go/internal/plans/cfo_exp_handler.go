package plans

import (
	"encoding/json"
	"io"
	"net/http"
	"strconv"
	"strings"
)

type CfoExpenseHandler struct {
	svc  *CfoExpenseService
	prin PrincipalFunc
}

func cfoExpenseErr(w http.ResponseWriter, err error) {
	code := http.StatusBadRequest
	msg := err.Error()
	if strings.Contains(msg, "среза") || strings.Contains(msg, "доступ") {
		code = http.StatusForbidden
	}
	if strings.Contains(msg, "not CFO") || strings.Contains(msg, "invalid CFO") {
		code = http.StatusNotFound
	}
	writeErr(w, code, msg)
}

func NewCfoExpenseHandler(s *CfoExpenseService, p PrincipalFunc) *CfoExpenseHandler {
	return &CfoExpenseHandler{svc: s, prin: p}
}
func (h *CfoExpenseHandler) p(r *http.Request) Principal {
	if h.prin == nil {
		return Principal{}
	}
	p, _ := h.prin(r)
	return p
}
func cfoID(w http.ResponseWriter, r *http.Request) (int64, bool) {
	id, e := strconv.ParseInt(r.PathValue("cardId"), 10, 64)
	if e != nil {
		writeErr(w, 400, "invalid cardId")
		return 0, false
	}
	return id, true
}
func (h *CfoExpenseHandler) FormGet(w http.ResponseWriter, r *http.Request) {
	id, ok := cfoID(w, r)
	if !ok {
		return
	}
	q := r.URL.Query()
	f := CfoExpenseFilter{Search: q.Get("search"), GroupPL: q.Get("group_pl"), CountryCompany: q.Get("country_company"), LegalEntity: q.Get("legal_entity"), Sort: q.Get("sort"), Desc: q.Get("dir") == "desc"}
	f.CodePL, _ = strconv.Atoi(q.Get("code_pl"))
	f.CodeCFO, _ = strconv.Atoi(q.Get("code_cfo"))
	f.Offset, _ = strconv.Atoi(q.Get("offset"))
	f.Limit, _ = strconv.Atoi(q.Get("limit"))
	v, e := h.svc.Form(r.Context(), h.p(r), id, f)
	if e != nil {
		cfoExpenseErr(w, e)
		return
	}
	writeJSON(w, 200, v)
}
func (h *CfoExpenseHandler) FormSave(w http.ResponseWriter, r *http.Request) {
	id, ok := cfoID(w, r)
	if !ok {
		return
	}
	var b struct {
		Cells    []CfoExpenseCell    `json:"cells"`
		Comments []CfoExpenseComment `json:"comments"`
	}
	if json.NewDecoder(r.Body).Decode(&b) != nil {
		writeErr(w, 400, "invalid json")
		return
	}
	n, e := h.svc.Save(r.Context(), h.p(r), id, b.Cells, b.Comments)
	if e != nil {
		cfoExpenseErr(w, e)
		return
	}
	writeJSON(w, 200, map[string]any{"saved": n})
}
func (h *CfoExpenseHandler) Export(w http.ResponseWriter, r *http.Request) {
	id, ok := cfoID(w, r)
	if !ok {
		return
	}
	q := r.URL.Query()
	f := CfoExpenseFilter{Search: q.Get("search"), GroupPL: q.Get("group_pl"), CountryCompany: q.Get("country_company"), LegalEntity: q.Get("legal_entity"), Sort: q.Get("sort"), Desc: q.Get("dir") == "desc"}
	f.CodePL, _ = strconv.Atoi(q.Get("code_pl"))
	f.CodeCFO, _ = strconv.Atoi(q.Get("code_cfo"))
	f.Offset, _ = strconv.Atoi(q.Get("offset"))
	f.Limit, _ = strconv.Atoi(q.Get("limit"))
	b, name, e := h.svc.Export(r.Context(), h.p(r), id, f)
	if e != nil {
		cfoExpenseErr(w, e)
		return
	}
	w.Header().Set("Content-Type", "application/vnd.openxmlformats-officedocument.spreadsheetml.sheet")
	w.Header().Set("Content-Disposition", `attachment; filename="`+name+`"`)
	_, _ = w.Write(b)
}
func (h *CfoExpenseHandler) Import(w http.ResponseWriter, r *http.Request) {
	id, ok := cfoID(w, r)
	if !ok {
		return
	}
	if err := r.ParseMultipartForm(32 << 20); err != nil {
		writeErr(w, http.StatusBadRequest, "invalid multipart form")
		return
	}
	f, _, err := r.FormFile("file")
	if err != nil {
		writeErr(w, http.StatusBadRequest, "file is required")
		return
	}
	defer f.Close()
	b, err := io.ReadAll(f)
	if err != nil {
		writeErr(w, http.StatusBadRequest, "file cannot be read")
		return
	}
	preview := r.URL.Query().Get("preview") == "1"
	result, err := h.svc.Import(r.Context(), h.p(r), id, b, preview)
	if err != nil {
		cfoExpenseErr(w, err)
		return
	}
	writeJSON(w, http.StatusOK, result)
}
func (h *CfoExpenseHandler) Validate(w http.ResponseWriter, r *http.Request) {
	id, ok := cfoID(w, r)
	if !ok {
		return
	}
	x, e := h.svc.Validate(r.Context(), h.p(r), id)
	if e != nil {
		cfoExpenseErr(w, e)
		return
	}
	writeJSON(w, 200, map[string]any{"issues": x})
}
func (h *CfoExpenseHandler) Bulk(w http.ResponseWriter, r *http.Request) {
	id, ok := cfoID(w, r)
	if !ok {
		return
	}
	var b struct {
		Op   string         `json:"op"`
		Cell CfoExpenseCell `json:"cell"`
	}
	if json.NewDecoder(r.Body).Decode(&b) != nil {
		writeErr(w, 400, "invalid json")
		return
	}
	n, e := h.svc.Bulk(r.Context(), h.p(r), id, b.Cell, b.Op)
	if e != nil {
		cfoExpenseErr(w, e)
		return
	}
	writeJSON(w, 200, map[string]any{"saved": n})
}
func (h *CfoExpenseHandler) Routes(mux *http.ServeMux) {
	mux.HandleFunc("/api/plans/cfo-exp/", func(w http.ResponseWriter, r *http.Request) {
		if strings.HasSuffix(r.URL.Path, "/form") && r.Method == "GET" {
			h.FormGet(w, r)
			return
		}
		if strings.HasSuffix(r.URL.Path, "/form") && r.Method == "PUT" {
			h.FormSave(w, r)
			return
		}
		if strings.HasSuffix(r.URL.Path, "/validate") && r.Method == "GET" {
			h.Validate(w, r)
			return
		}
		if strings.HasSuffix(r.URL.Path, "/bulk") && r.Method == "POST" {
			h.Bulk(w, r)
			return
		}
		if strings.HasSuffix(r.URL.Path, "/export") && r.Method == "GET" {
			h.Export(w, r)
			return
		}
		if strings.HasSuffix(r.URL.Path, "/import") && r.Method == "POST" {
			h.Import(w, r)
			return
		}
		writeErr(w, 404, "not found")
	})
}
