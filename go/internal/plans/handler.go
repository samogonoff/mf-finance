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
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"
)

// parseInt64 — мягкий парсинг (0 при ошибке/пусто).
func parseInt64(s string) int64 {
	n, _ := strconv.ParseInt(s, 10, 64)
	return n
}

// auditCSV — сериализация событий аудита в CSV (AUD-04).
func auditCSV(events []AuditEvent) string {
	var b strings.Builder
	b.WriteString("ts,user_id,action,entity_type,entity_id,ip,correlation_id\n")
	for _, e := range events {
		fmt.Fprintf(&b, "%s,%d,%s,%s,%d,%s,%s\n",
			e.TS, e.UserID, e.Action, e.EntityType, e.EntityID, e.IP, e.CorrelationID)
	}
	return b.String()
}

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
	dir       DirSource
	fact      MpFactSource
	form      *Service
	principal PrincipalFunc
	audit     Auditor
}

// NewHandler — конструктор. dir — справочники (SeedSource в MVP), fact —
// read-only факт МП, form — сервис формы TPL-MP, principal — извлечение
// пользователя из запроса для ABAC, audit — журнал аудита (no-op при выключенном).
func NewHandler(dir DirSource, fact MpFactSource, form *Service, principal PrincipalFunc, audit Auditor) *Handler {
	if audit == nil {
		audit = noopAuditor{}
	}
	return &Handler{dir: dir, fact: fact, form: form, principal: principal, audit: audit}
}

// rec — fire-and-forget запись аудита (ошибки не валят операцию).
func (h *Handler) rec(r *http.Request, action, entityType string, entityID int64) {
	_ = h.audit.Record(r.Context(), AuditEvent{
		UserID:     h.prin(r).UserID,
		Action:     action,
		EntityType: entityType,
		EntityID:   entityID,
		IP:         clientIP(r),
	})
}

func clientIP(r *http.Request) string {
	if xf := r.Header.Get("X-Forwarded-For"); xf != "" {
		return xf
	}
	return r.RemoteAddr
}

// prin — Principal запроса; при отсутствии функции (часть юнит-тестов) — админ.
func (h *Handler) prin(r *http.Request) Principal {
	if h.principal == nil {
		return Principal{PlansAdmin: true}
	}
	p, _ := h.principal(r)
	return p
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
	form, err := h.form.MpForm(r.Context(), h.prin(r), year, month, segment, q.Get("currency"))
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
	plID, err := h.form.SaveMpForm(r.Context(), h.prin(r), req, raw)
	if err != nil {
		writeErr(w, http.StatusBadRequest, err.Error())
		return
	}
	h.rec(r, "save_form", "pl_instance", plID)
	writeJSON(w, http.StatusOK, map[string]int64{"pl_id": plID})
}

// MpCompute — GET /api/plans/mp/compute?year&month&segment&currency. Превью каскада
// CALC (производные показатели по площадкам).
func (h *Handler) MpCompute(w http.ResponseWriter, r *http.Request) {
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
	rows, err := h.form.ComputeMp(r.Context(), h.prin(r), year, month, segment, q.Get("currency"))
	if err != nil {
		writeErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, rows)
}

// FormulaOverride — PUT /api/plans/mp/formula. Переопределение формулы каскада
// per-срез (D11). Тело: {year, month, code, block_type, formula_expr, reason}.
func (h *Handler) FormulaOverride(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Year        int    `json:"year"`
		Month       int    `json:"month"`
		Code        string `json:"code"`
		BlockType   string `json:"block_type"`
		FormulaExpr string `json:"formula_expr"`
		Reason      string `json:"reason"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeErr(w, http.StatusBadRequest, "invalid json")
		return
	}
	if body.Year <= 0 || body.Month < 1 || body.Month > 12 {
		writeErr(w, http.StatusBadRequest, "invalid period")
		return
	}
	plID, err := h.form.SaveFormulaOverride(r.Context(), body.Year, body.Month, FormulaOverride{
		Code: body.Code, BlockType: body.BlockType, FormulaExpr: body.FormulaExpr, Reason: body.Reason,
	})
	if err != nil {
		writeErr(w, http.StatusBadRequest, err.Error())
		return
	}
	h.rec(r, "formula_override", "pl_instance", plID)
	writeJSON(w, http.StatusOK, map[string]int64{"pl_id": plID})
}

// AuditList — GET /api/plans/audit?user_id&entity_id&from&to&format=csv. Журнал
// аудита (роль ROLE_PLANS_ADMIN). format=csv → выгрузка CSV (AUD-04).
func (h *Handler) AuditList(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	f := AuditFilter{
		UserID:   parseInt64(q.Get("user_id")),
		EntityID: parseInt64(q.Get("entity_id")),
		From:     q.Get("from"),
		To:       q.Get("to"),
	}
	events, err := h.audit.List(r.Context(), f)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	if q.Get("format") == "csv" {
		w.Header().Set("Content-Type", "text/csv; charset=utf-8")
		w.Header().Set("Content-Disposition", "attachment; filename=\"plans-audit.csv\"")
		_, _ = w.Write([]byte(auditCSV(events)))
		return
	}
	writeJSON(w, http.StatusOK, events)
}

// MpExport — GET /api/plans/mp/export?year&month&segment&currency. Снимок формы в .xlsx.
func (h *Handler) MpExport(w http.ResponseWriter, r *http.Request) {
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
	data, err := h.form.ExportMpForm(r.Context(), h.prin(r), year, month, segment, q.Get("currency"))
	if err != nil {
		writeErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	fname := fmt.Sprintf("TPL-MP_%s_%d-%02d.xlsx", segment, year, month)
	w.Header().Set("Content-Type", "application/vnd.openxmlformats-officedocument.spreadsheetml.sheet")
	w.Header().Set("Content-Disposition", "attachment; filename=\""+fname+"\"")
	_, _ = w.Write(data)
}

// MpImport — POST /api/plans/mp/import?year&month&segment. Тело — .xlsx (editable
// колонки). Чужой ABAC/неизвестный код/пустая причина → отказ.
func (h *Handler) MpImport(w http.ResponseWriter, r *http.Request) {
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
	raw, err := io.ReadAll(io.LimitReader(r.Body, 16<<20))
	if err != nil {
		writeErr(w, http.StatusBadRequest, "read body")
		return
	}
	plID, err := h.form.ImportMpForm(r.Context(), h.prin(r), segment, year, month, raw)
	if err != nil {
		writeErr(w, http.StatusBadRequest, err.Error())
		return
	}
	h.rec(r, "import", "pl_instance", plID)
	writeJSON(w, http.StatusOK, map[string]int64{"pl_id": plID})
}

// CommentsList — GET /api/plans/instances/{id}/comments.
func (h *Handler) CommentsList(w http.ResponseWriter, r *http.Request) {
	plID, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil || plID <= 0 {
		writeErr(w, http.StatusBadRequest, "invalid id")
		return
	}
	list, err := h.form.Comments(r.Context(), plID)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, list)
}

// CommentCreate — POST /api/plans/instances/{id}/comments. Тело: {metric_ref, body}.
func (h *Handler) CommentCreate(w http.ResponseWriter, r *http.Request) {
	plID, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil || plID <= 0 {
		writeErr(w, http.StatusBadRequest, "invalid id")
		return
	}
	var c CommentInput
	if err := json.NewDecoder(r.Body).Decode(&c); err != nil {
		writeErr(w, http.StatusBadRequest, "invalid json")
		return
	}
	if c.Body == "" {
		writeErr(w, http.StatusBadRequest, "body required")
		return
	}
	id, err := h.form.AddComment(r.Context(), plID, c)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	h.rec(r, "comment", "pl_instance", plID)
	writeJSON(w, http.StatusOK, map[string]int64{"id": id})
}

// ScopeUpsert — PUT /api/plans/scope/{user_id}. Назначение ABAC-среза
// пользователю (роль ROLE_PLANS_ADMIN). Тело: role, code_cfo[], опц. stage/страна/ЮЛ.
func (h *Handler) ScopeUpsert(w http.ResponseWriter, r *http.Request) {
	userID, err := strconv.ParseInt(r.PathValue("user_id"), 10, 64)
	if err != nil || userID <= 0 {
		writeErr(w, http.StatusBadRequest, "invalid user_id")
		return
	}
	var body struct {
		Role        string `json:"role"`
		StageCode   string `json:"stage_code"`
		Country     string `json:"country"`
		LegalEntity string `json:"legal_entity"`
		CodeCFO     []int  `json:"code_cfo"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeErr(w, http.StatusBadRequest, "invalid json")
		return
	}
	if body.Role == "" {
		writeErr(w, http.StatusBadRequest, "role required")
		return
	}
	sc := UserScope{
		UserID: userID, Role: body.Role, StageCode: body.StageCode,
		Country: body.Country, LegalEntity: body.LegalEntity, CodeCFO: body.CodeCFO,
	}
	if err := h.form.AssignScope(r.Context(), sc); err != nil {
		writeErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	h.rec(r, "scope", "user", userID)
	writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
}

// InstancesList — GET /api/plans/instances. Список экземпляров PL.
func (h *Handler) InstancesList(w http.ResponseWriter, r *http.Request) {
	list, err := h.form.Instances(r.Context())
	if err != nil {
		writeErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, list)
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
	h.rec(r, "create_instance", "pl_instance", id)
	writeJSON(w, http.StatusOK, map[string]int64{"id": id})
}
