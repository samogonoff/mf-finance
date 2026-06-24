package debt

import (
	"encoding/json"
	"errors"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"
)

// UserIDFromCtxFunc — поставляется из cmd/api/main.go (обёртка над auth.UserFromCtx),
// чтобы пакет debt не зависел напрямую от пакета auth.
type UserIDFromCtxFunc func(r *http.Request) (int64, bool)

// Handler — HTTP-ручки отчёта.
type Handler struct {
	svc    *Service
	repo   *FiltersRepo
	userOf UserIDFromCtxFunc
}

func NewHandler(svc *Service, repo *FiltersRepo, userOf UserIDFromCtxFunc) *Handler {
	return &Handler{svc: svc, repo: repo, userOf: userOf}
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

func writeErr(w http.ResponseWriter, status int, msg string) {
	writeJSON(w, status, map[string]string{"error": msg})
}

// FilterOptions — GET /api/reports/debt/filter-options.
func (h *Handler) FilterOptions(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, h.svc.FilterOptions())
}

// Report — GET /api/reports/debt/report?date_from&date_to&entity_inns&accounts&currencies.
// Параметры списками передаются как CSV или повторяющиеся ?entity_inns=a&entity_inns=b.
func (h *Handler) Report(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	from, err := parseDate(q.Get("date_from"))
	if err != nil {
		writeErr(w, http.StatusBadRequest, "invalid date_from: "+err.Error())
		return
	}
	to, err := parseDate(q.Get("date_to"))
	if err != nil {
		writeErr(w, http.StatusBadRequest, "invalid date_to: "+err.Error())
		return
	}
	f := Filters{
		DateFrom:   from,
		DateTo:     to,
		EntityINNs: collectList(q, "entity_inns"),
		Accounts:   collectList(q, "accounts"),
		Currencies: collectList(q, "currencies"),
		OnlyICO:    parseBoolDefault(q.Get("only_ico"), true),
	}
	resp, err := h.svc.Report(r.Context(), f)
	if err != nil {
		writeErr(w, http.StatusBadRequest, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, resp)
}

// Drilldown — GET /api/reports/debt/drilldown?... .
func (h *Handler) Drilldown(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	from, err := parseDate(q.Get("date_from"))
	if err != nil {
		writeErr(w, http.StatusBadRequest, "invalid date_from")
		return
	}
	to, err := parseDate(q.Get("date_to"))
	if err != nil {
		writeErr(w, http.StatusBadRequest, "invalid date_to")
		return
	}
	dq := DrilldownQuery{
		CompanyINN: q.Get("company_inn"),
		PartnerINN: q.Get("partner_inn"),
		Account:    q.Get("account"),
		Contract:   q.Get("contract"),
		Currency:   q.Get("currency"),
		DateFrom:   from,
		DateTo:     to,
	}
	// Обязателен company_inn. partner_inn/account опциональны: ДЗ/КЗ-строки
	// (premaster) всегда несут оба, а revenue-строки finpl приходят без счёта
	// (Account="") и иногда без распознанного контрагента — для них композит
	// отдаёт месячную PL-детализацию (см. finplComposite.Drilldown).
	if dq.CompanyINN == "" {
		writeErr(w, http.StatusBadRequest, "company_inn is required")
		return
	}
	docs, err := h.svc.Drilldown(r.Context(), dq)
	if err != nil {
		writeErr(w, http.StatusBadRequest, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, docs)
}

// SavedFilters — GET → список, POST → создать, DELETE /{id} → удалить.
// Здесь один handler через DELETE по query-param ?id= — упрощает роутинг ServeMux.
func (h *Handler) ListSavedFilters(w http.ResponseWriter, r *http.Request) {
	uid, ok := h.userOf(r)
	if !ok {
		writeErr(w, http.StatusUnauthorized, "no session")
		return
	}
	out, err := h.repo.List(r.Context(), uid)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, out)
}

func (h *Handler) CreateSavedFilter(w http.ResponseWriter, r *http.Request) {
	uid, ok := h.userOf(r)
	if !ok {
		writeErr(w, http.StatusUnauthorized, "no session")
		return
	}
	var body struct {
		Name    string        `json:"name"`
		Payload FilterPayload `json:"payload"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeErr(w, http.StatusBadRequest, "invalid body")
		return
	}
	if strings.TrimSpace(body.Name) == "" {
		writeErr(w, http.StatusBadRequest, "name required")
		return
	}
	f, err := h.repo.Create(r.Context(), uid, body.Name, body.Payload)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusCreated, f)
}

func (h *Handler) DeleteSavedFilter(w http.ResponseWriter, r *http.Request) {
	uid, ok := h.userOf(r)
	if !ok {
		writeErr(w, http.StatusUnauthorized, "no session")
		return
	}
	idStr := r.URL.Query().Get("id")
	id, err := strconv.ParseInt(idStr, 10, 64)
	if err != nil || id <= 0 {
		writeErr(w, http.StatusBadRequest, "invalid id")
		return
	}
	if err := h.repo.Delete(r.Context(), uid, id); err != nil {
		if errors.Is(err, ErrNotFound) {
			writeErr(w, http.StatusNotFound, "not found")
			return
		}
		writeErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// helpers

func parseDate(s string) (time.Time, error) {
	s = strings.TrimSpace(s)
	if s == "" {
		return time.Time{}, errors.New("required")
	}
	// принимаем ISO-8601 (YYYY-MM-DD) — UI отправляет именно так
	return time.Parse("2006-01-02", s)
}

// parseBoolDefault разбирает "1"/"true"/"yes" → true, "0"/"false"/"no" → false,
// пустое значение → def. Используется для query-параметров с дефолтом.
func parseBoolDefault(s string, def bool) bool {
	switch strings.ToLower(strings.TrimSpace(s)) {
	case "":
		return def
	case "1", "true", "yes", "on":
		return true
	case "0", "false", "no", "off":
		return false
	}
	return def
}

func collectList(q url.Values, key string) []string {
	out := []string{}
	for _, v := range q[key] {
		for _, p := range strings.Split(v, ",") {
			p = strings.TrimSpace(p)
			if p != "" {
				out = append(out, p)
			}
		}
	}
	return out
}
