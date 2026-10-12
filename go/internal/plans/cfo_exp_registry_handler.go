package plans

import (
	"encoding/json"
	"github.com/jackc/pgx/v5/pgxpool"
	"net/http"
	"strconv"
	"strings"
)

// CfoExpenseRegistryHandler exposes the isolated manual-registry API.
type CfoExpenseRegistryHandler struct {
	svc  *CfoExpenseRegistryService
	prin PrincipalFunc
}

func NewCfoExpenseRegistryHandler(pool *pgxpool.Pool, cards CfoExpenseRegistryCards, scope ScopeStore, principal PrincipalFunc) *CfoExpenseRegistryHandler {
	return &CfoExpenseRegistryHandler{svc: NewCfoExpenseRegistryService(NewCfoExpenseRegistryStore(pool), cards, scope), prin: principal}
}
func (h *CfoExpenseRegistryHandler) p(r *http.Request) Principal {
	if h.prin == nil {
		return Principal{}
	}
	p, _ := h.prin(r)
	return p
}
func cfoRegistryID(w http.ResponseWriter, r *http.Request, key string) (int64, bool) {
	v, e := strconv.ParseInt(r.PathValue(key), 10, 64)
	if e != nil || v <= 0 {
		writeErr(w, http.StatusBadRequest, "invalid "+key)
		return 0, false
	}
	return v, true
}
func cfoRegistryErr(w http.ResponseWriter, e error) {
	msg := e.Error()
	code := http.StatusBadRequest
	if strings.Contains(msg, "среза") || strings.Contains(msg, "доступ") {
		code = http.StatusForbidden
	}
	if strings.Contains(msg, "не найден") || strings.Contains(msg, "не разрешён") {
		code = http.StatusNotFound
	}
	writeErr(w, code, msg)
}
func (h *CfoExpenseRegistryHandler) RegistryGet(w http.ResponseWriter, r *http.Request) {
	id, ok := cfoRegistryID(w, r, "cardId")
	if !ok {
		return
	}
	v, e := h.svc.Get(r.Context(), h.p(r), id, r.PathValue("sheet"))
	if e != nil {
		cfoRegistryErr(w, e)
		return
	}
	writeJSON(w, http.StatusOK, v)
}
func (h *CfoExpenseRegistryHandler) RegistrySave(w http.ResponseWriter, r *http.Request) {
	id, ok := cfoRegistryID(w, r, "cardId")
	if !ok {
		return
	}
	var row CfoExpenseRegistryRow
	if json.NewDecoder(r.Body).Decode(&row) != nil {
		writeErr(w, http.StatusBadRequest, "invalid json")
		return
	}
	v, e := h.svc.Save(r.Context(), h.p(r), id, r.PathValue("sheet"), row)
	if e != nil {
		cfoRegistryErr(w, e)
		return
	}
	writeJSON(w, http.StatusOK, map[string]int64{"id": v})
}
func (h *CfoExpenseRegistryHandler) RegistryDelete(w http.ResponseWriter, r *http.Request) {
	id, ok := cfoRegistryID(w, r, "cardId")
	if !ok {
		return
	}
	row, ok := cfoRegistryID(w, r, "rowId")
	if !ok {
		return
	}
	e := h.svc.Delete(r.Context(), h.p(r), id, r.PathValue("sheet"), row)
	if e != nil {
		cfoRegistryErr(w, e)
		return
	}
	writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
}
func (h *CfoExpenseRegistryHandler) RegistryCopy(w http.ResponseWriter, r *http.Request) {
	id, ok := cfoRegistryID(w, r, "cardId")
	if !ok {
		return
	}
	row, ok := cfoRegistryID(w, r, "rowId")
	if !ok {
		return
	}
	v, e := h.svc.Copy(r.Context(), h.p(r), id, r.PathValue("sheet"), row)
	if e != nil {
		cfoRegistryErr(w, e)
		return
	}
	writeJSON(w, http.StatusOK, map[string]int64{"id": v})
}
