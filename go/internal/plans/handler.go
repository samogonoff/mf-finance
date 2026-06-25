// Package plans — модуль «Тактические планы» (Profit & Loss).
// Спецификация: docs/reports/plans/SPEC.md. План: docs/reports/plans/plan.md.
//
// VS0 — каркас (health за ROLE_PLANS_USER). VS1 — справочники TPL-MP
// (dir_marketplace/dir_pl_line/dir_cfo) из seed-данных. Доменная логика формы
// и ABAC — следующими срезами VS3+.
package plans

import (
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strconv"
)

// atoiPositive парсит положительное целое из query-параметра.
func atoiPositive(s string) (int, error) {
	n, err := strconv.Atoi(s)
	if err != nil {
		return 0, err
	}
	if n <= 0 {
		return 0, errors.New("must be positive")
	}
	return n, nil
}

// Handler — HTTP-ручки модуля тактических планов.
type Handler struct {
	dir  DirSource
	fact MpFactSource
	form *Service
}

// NewHandler — конструктор. dir — справочники (SeedSource в MVP), fact —
// read-only факт МП, form — сервис формы TPL-MP (запись тактики; nil в части
// тестов, не задействующих форму).
func NewHandler(dir DirSource, fact MpFactSource, form *Service) *Handler {
	return &Handler{dir: dir, fact: fact, form: form}
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

func writeErr(w http.ResponseWriter, status int, msg string) {
	writeJSON(w, status, map[string]string{"error": msg})
}

// Health — GET /api/plans/health. Проверка, что модуль смонтирован и доступен
// носителю роли ROLE_PLANS_USER (гейт навешивается в main.go).
func (h *Handler) Health(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
}

// Directories — GET /api/plans/directories. Реестр справочников модуля.
func (h *Handler) Directories(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, h.dir.Directories())
}

// DirectoryRows — GET /api/plans/directories/{code}/rows. Строки справочника.
func (h *Handler) DirectoryRows(w http.ResponseWriter, r *http.Request) {
	code := r.PathValue("code")
	rows, err := h.dir.Rows(code)
	if err != nil {
		writeErr(w, http.StatusNotFound, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, rows)
}

// MpFact — GET /api/plans/mp/fact?year&month&segment. Read-only факт МП
// (mock или online FinDWH). Дефолт сегмента — large.
func (h *Handler) MpFact(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	year, err := atoiPositive(q.Get("year"))
	if err != nil {
		writeErr(w, http.StatusBadRequest, "invalid year")
		return
	}
	month, err := atoiPositive(q.Get("month"))
	if err != nil || month < 1 || month > 12 {
		writeErr(w, http.StatusBadRequest, "invalid month")
		return
	}
	segment := q.Get("segment")
	if segment == "" {
		segment = "large"
	}
	rows, err := h.fact.MpFact(r.Context(), year, month, segment)
	if err != nil {
		writeErr(w, http.StatusBadGateway, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, rows)
}

// MpFormGet — GET /api/plans/mp/form?year&month&segment&currency. Матрица формы
// (факт read-only + сохранённая тактика).
func (h *Handler) MpFormGet(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	year, err := atoiPositive(q.Get("year"))
	if err != nil {
		writeErr(w, http.StatusBadRequest, "invalid year")
		return
	}
	month, err := atoiPositive(q.Get("month"))
	if err != nil || month < 1 || month > 12 {
		writeErr(w, http.StatusBadRequest, "invalid month")
		return
	}
	segment := q.Get("segment")
	if segment == "" {
		segment = "large"
	}
	form, err := h.form.MpForm(r.Context(), year, month, segment, q.Get("currency"))
	if err != nil {
		writeErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, form)
}

// MpFormSave — PUT /api/plans/mp/form. Сохраняет editable-ячейки тактики +
// полный снимок формы. Тело — SaveMpFormRequest (SPEC §9.7).
func (h *Handler) MpFormSave(w http.ResponseWriter, r *http.Request) {
	raw, err := io.ReadAll(io.LimitReader(r.Body, 8<<20))
	if err != nil {
		writeErr(w, http.StatusBadRequest, "read body")
		return
	}
	var req SaveMpFormRequest
	if err := json.Unmarshal(raw, &req); err != nil {
		writeErr(w, http.StatusBadRequest, "invalid json: "+err.Error())
		return
	}
	plID, err := h.form.SaveMpForm(r.Context(), req, raw)
	if err != nil {
		writeErr(w, http.StatusBadRequest, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]int64{"pl_id": plID})
}

// CreateInstance — POST /api/plans/instances. Создаёт/возвращает экземпляр PL
// на период. Тело: {"year":2026,"month":6}.
func (h *Handler) CreateInstance(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Year  int `json:"year"`
		Month int `json:"month"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeErr(w, http.StatusBadRequest, "invalid json")
		return
	}
	if body.Year <= 0 || body.Month < 1 || body.Month > 12 {
		writeErr(w, http.StatusBadRequest, "invalid period")
		return
	}
	id, err := h.form.EnsureInstance(r.Context(), body.Year, body.Month)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]int64{"id": id})
}
