// Package plans — модуль «Тактические планы» (Profit & Loss).
// Спецификация: docs/reports/plans/SPEC.md. План: docs/reports/plans/plan.md.
//
// VS0 — каркас (health за ROLE_PLANS_USER). VS1 — справочники TPL-MP
// (dir_marketplace/dir_pl_line/dir_cfo) из seed-данных. Доменная логика формы
// и ABAC — следующими срезами VS3+.
package plans

import (
	"encoding/json"
	"net/http"
)

// Handler — HTTP-ручки модуля тактических планов.
type Handler struct {
	dir DirSource
}

// NewHandler — конструктор. dir отдаёт справочники (SeedSource в MVP).
func NewHandler(dir DirSource) *Handler { return &Handler{dir: dir} }

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
