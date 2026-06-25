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
}

// NewHandler — конструктор. dir отдаёт справочники (SeedSource в MVP),
// fact — read-only факт МП (mock или online FinDWH).
func NewHandler(dir DirSource, fact MpFactSource) *Handler {
	return &Handler{dir: dir, fact: fact}
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
