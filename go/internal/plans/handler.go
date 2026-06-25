// Package plans — модуль «Тактические планы» (Profit & Loss).
// Спецификация: docs/reports/plans/SPEC.md. План: docs/reports/plans/plan.md.
//
// VS0 (каркас): пакет регистрируется в cmd/api/main.go и отдаёт health-ручку
// за middleware ROLE_PLANS_USER. Доменная логика (форма TPL-MP, справочники,
// ABAC) добавляется следующими вертикальными срезами VS1+.
package plans

import (
	"encoding/json"
	"net/http"
)

// Handler — HTTP-ручки модуля тактических планов.
type Handler struct{}

// NewHandler — конструктор. По мере роста модуля сюда придут service/repo
// (паттерн internal/reports/debt).
func NewHandler() *Handler { return &Handler{} }

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

// Health — GET /api/plans/health. Проверка, что модуль смонтирован и доступен
// носителю роли ROLE_PLANS_USER (гейт навешивается в main.go).
func (h *Handler) Health(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
}
