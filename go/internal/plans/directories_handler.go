package plans

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
)

// HTTP-ручки экрана «Справочники» (реестр+схема+синхронизация+кэш) и страницы
// «Пользователи и права» (каталог должностей + назначения). ТЗ §«UI справочников»,
// §«Разграничение прав». Все мутации — за ROLE_PLANS_ADMIN (роутинг в main.go).

func (h *Handler) syncableCodes() map[string]bool {
	out := map[string]bool{}
	if h.syncer != nil {
		for _, c := range h.syncer.Codes() {
			out[c] = true
		}
	}
	return out
}

// DirRegistry — GET /api/plans/dir. Реестр справочников с метаданными и схемой
// отображения (богатая версия для нового UI; суперсет старого DirList).
func (h *Handler) DirRegistry(w http.ResponseWriter, r *http.Request) {
	if h.dirRepo == nil {
		writeJSON(w, http.StatusOK, []DirMetaRich{})
		return
	}
	list, err := h.dirRepo.DirectoriesRich(r.Context(), h.syncableCodes())
	if err != nil {
		writeErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, list)
}

// DirRowsCached — GET /api/plans/dir/{code}/rows. Строки справочника через
// Redis-кэш (fallback → SQL). При отсутствии кэша — прямой SQL.
func (h *Handler) DirRowsCached(w http.ResponseWriter, r *http.Request) {
	code := r.PathValue("code")
	var rows []DirRow
	var err error
	if h.cache != nil {
		rows, err = h.cache.Rows(r.Context(), code)
	} else if h.dirRepo != nil {
		rows, err = h.dirRepo.Rows(r.Context(), code)
	}
	if err != nil {
		writeErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	// Живой ТОП у ЦФО = носитель покрывающей должности (overlay, переживает кэш/импорт).
	if code == "dir_cfo" && h.jobpos != nil {
		if top, e := h.jobpos.TopByCfo(r.Context()); e == nil && len(top) > 0 {
			for i := range rows {
				ext := fmt.Sprintf("%v", rows[i].Payload["code_cfo"])
				if t, ok := top[ext]; ok {
					rows[i].Payload["top"] = t.Holder
					rows[i].Payload["top_position"] = t.Title
				}
			}
		}
	}
	writeJSON(w, http.StatusOK, rows)
}

// DirSync — POST /api/plans/dir/{code}/sync. Ручной запуск синхронизации (DIR-03).
func (h *Handler) DirSync(w http.ResponseWriter, r *http.Request) {
	if h.syncer == nil {
		writeErr(w, http.StatusServiceUnavailable, "движок синхронизации не сконфигурирован")
		return
	}
	code := r.PathValue("code")
	by := "manual:" + itoa(h.prin(r).UserID)
	res, err := h.syncer.Sync(r.Context(), code, by)
	h.rec(r, "dir_sync", "directory", 0)
	if err != nil {
		// Возвращаем 200 с телом результата: статус/ошибка видны в UI журнала.
		writeJSON(w, http.StatusOK, res)
		return
	}
	writeJSON(w, http.StatusOK, res)
}

// DirWarm — POST /api/plans/dir/{code}/warm. Принудительный прогрев кэша.
func (h *Handler) DirWarm(w http.ResponseWriter, r *http.Request) {
	if h.cache == nil {
		writeErr(w, http.StatusServiceUnavailable, "кэш не сконфигурирован")
		return
	}
	code := r.PathValue("code")
	if err := h.cache.Warm(r.Context(), code); err != nil {
		writeErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	h.rec(r, "dir_warm", "directory", 0)
	writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
}

// DirSyncLog — GET /api/plans/dir/{code}/log. Журнал синхронизаций.
func (h *Handler) DirSyncLog(w http.ResponseWriter, r *http.Request) {
	if h.dirRepo == nil {
		writeJSON(w, http.StatusOK, []SyncLogEntry{})
		return
	}
	log, err := h.dirRepo.SyncLog(r.Context(), r.PathValue("code"), 20)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, log)
}

// DirVersions — GET /api/plans/dir/{code}/versions. История версий (DIR-02).
func (h *Handler) DirVersions(w http.ResponseWriter, r *http.Request) {
	if h.dirRepo == nil {
		writeJSON(w, http.StatusOK, []DirVersion{})
		return
	}
	list, err := h.dirRepo.Versions(r.Context(), r.PathValue("code"), 50)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, list)
}

// DirSettings — PUT /api/plans/dir/{code}/settings. {cache_ttl_seconds, stale_after_seconds}.
func (h *Handler) DirSettings(w http.ResponseWriter, r *http.Request) {
	if h.dirRepo == nil {
		writeErr(w, http.StatusServiceUnavailable, "нет хранилища справочников")
		return
	}
	code := r.PathValue("code")
	var body struct {
		CacheTTL   int `json:"cache_ttl_seconds"`
		StaleAfter int `json:"stale_after_seconds"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeErr(w, http.StatusBadRequest, "invalid json")
		return
	}
	if body.CacheTTL < 0 || body.StaleAfter < 0 {
		writeErr(w, http.StatusBadRequest, "значения TTL не могут быть отрицательными")
		return
	}
	if err := h.dirRepo.UpdateCacheSettings(r.Context(), code, body.CacheTTL, body.StaleAfter); err != nil {
		writeErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	if h.cache != nil {
		_ = h.cache.Invalidate(r.Context(), code)
	}
	h.rec(r, "dir_settings", "directory", 0)
	writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
}

// --- Каталог должностей ---

// PositionsList — GET /api/plans/positions.
func (h *Handler) PositionsList(w http.ResponseWriter, r *http.Request) {
	if h.positions == nil {
		writeJSON(w, http.StatusOK, []Position{})
		return
	}
	list, err := h.positions.List(r.Context())
	if err != nil {
		writeErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, list)
}

// PositionUpsert — PUT /api/plans/positions/{code}.
func (h *Handler) PositionUpsert(w http.ResponseWriter, r *http.Request) {
	if h.positions == nil {
		writeErr(w, http.StatusServiceUnavailable, "каталог должностей недоступен")
		return
	}
	var p Position
	if err := json.NewDecoder(r.Body).Decode(&p); err != nil {
		writeErr(w, http.StatusBadRequest, "invalid json")
		return
	}
	p.Code = r.PathValue("code")
	if p.Code == "" || p.Name == "" {
		writeErr(w, http.StatusBadRequest, "code и name обязательны")
		return
	}
	if err := h.positions.Upsert(r.Context(), p); err != nil {
		writeErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	h.rec(r, "position_upsert", "position", 0)
	writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
}

// PositionDelete — DELETE /api/plans/positions/{code}.
func (h *Handler) PositionDelete(w http.ResponseWriter, r *http.Request) {
	if h.positions == nil {
		writeErr(w, http.StatusServiceUnavailable, "каталог должностей недоступен")
		return
	}
	if err := h.positions.Delete(r.Context(), r.PathValue("code")); err != nil {
		writeErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	h.rec(r, "position_delete", "position", 0)
	writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
}

// --- Назначения должностей (страница «Пользователи и права») ---

// AssignmentsList — GET /api/plans/assignments.
func (h *Handler) AssignmentsList(w http.ResponseWriter, r *http.Request) {
	if h.scopeAdmin == nil {
		writeJSON(w, http.StatusOK, []ScopeAssignment{})
		return
	}
	list, err := h.scopeAdmin.ListAssignments(r.Context())
	if err != nil {
		writeErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, list)
}

// AssignmentDelete — DELETE /api/plans/assignments/{id}.
func (h *Handler) AssignmentDelete(w http.ResponseWriter, r *http.Request) {
	if h.scopeAdmin == nil {
		writeErr(w, http.StatusServiceUnavailable, "назначения недоступны")
		return
	}
	if err := h.scopeAdmin.DeleteAssignment(r.Context(), parseInt64(r.PathValue("id"))); err != nil {
		writeErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	h.rec(r, "assignment_delete", "scope", 0)
	writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
}

// --- Пользователи модуля (системные ТП-роли) ---

// PlanUsersList — GET /api/plans/users. Пользователи + их ТП-роли.
func (h *Handler) PlanUsersList(w http.ResponseWriter, r *http.Request) {
	if h.users == nil {
		writeJSON(w, http.StatusOK, []PlanUser{})
		return
	}
	list, err := h.users.List(r.Context())
	if err != nil {
		writeErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, list)
}

// PlanUserRoles — PUT /api/plans/users/{id}/roles. {plans_admin, plans_user}.
// Управляет ТОЛЬКО ТП-ролями; глобальный ROLE_ADMIN не выдаётся отсюда.
func (h *Handler) PlanUserRoles(w http.ResponseWriter, r *http.Request) {
	if h.users == nil {
		writeErr(w, http.StatusServiceUnavailable, "пользователи недоступны")
		return
	}
	var body struct {
		PlansAdmin bool `json:"plans_admin"`
		PlansUser  bool `json:"plans_user"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeErr(w, http.StatusBadRequest, "invalid json")
		return
	}
	id := parseInt64(r.PathValue("id"))
	if err := h.users.SetPlansRoles(r.Context(), id, body.PlansAdmin, body.PlansUser); err != nil {
		writeErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	h.rec(r, "plans_roles", "user", id)
	writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
}

// PositionStages — PUT /api/plans/positions/{code}/stages. {stages:[...]}.
func (h *Handler) PositionStages(w http.ResponseWriter, r *http.Request) {
	if h.positions == nil {
		writeErr(w, http.StatusServiceUnavailable, "каталог должностей недоступен")
		return
	}
	var body struct {
		Stages []string `json:"stages"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeErr(w, http.StatusBadRequest, "invalid json")
		return
	}
	if err := h.positions.SetStages(r.Context(), r.PathValue("code"), body.Stages); err != nil {
		writeErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	h.rec(r, "position_stages", "position", 0)
	writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
}

// --- Заместители ---

// DeputiesList — GET /api/plans/deputies.
func (h *Handler) DeputiesList(w http.ResponseWriter, r *http.Request) {
	if h.deputies == nil {
		writeJSON(w, http.StatusOK, []Deputy{})
		return
	}
	list, err := h.deputies.List(r.Context())
	if err != nil {
		writeErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, list)
}

// DeputyUpsert — PUT /api/plans/deputies. {principal_user_id, deputy_user_id, stage_code, note}.
func (h *Handler) DeputyUpsert(w http.ResponseWriter, r *http.Request) {
	if h.deputies == nil {
		writeErr(w, http.StatusServiceUnavailable, "замы недоступны")
		return
	}
	var body struct {
		PrincipalID int64  `json:"principal_user_id"`
		DeputyID    int64  `json:"deputy_user_id"`
		StageCode   string `json:"stage_code"`
		Note        string `json:"note"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeErr(w, http.StatusBadRequest, "invalid json")
		return
	}
	if body.PrincipalID == 0 || body.DeputyID == 0 {
		writeErr(w, http.StatusBadRequest, "нужны principal_user_id и deputy_user_id")
		return
	}
	if body.PrincipalID == body.DeputyID {
		writeErr(w, http.StatusBadRequest, "сотрудник не может быть собственным замом")
		return
	}
	if err := h.deputies.Upsert(r.Context(), body.PrincipalID, body.DeputyID, body.StageCode, body.Note, h.prin(r).UserID); err != nil {
		writeErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	h.rec(r, "deputy_upsert", "user", body.PrincipalID)
	writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
}

// DeputyDelete — DELETE /api/plans/deputies/{id}.
func (h *Handler) DeputyDelete(w http.ResponseWriter, r *http.Request) {
	if h.deputies == nil {
		writeErr(w, http.StatusServiceUnavailable, "замы недоступны")
		return
	}
	if err := h.deputies.Delete(r.Context(), parseInt64(r.PathValue("id"))); err != nil {
		writeErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	h.rec(r, "deputy_delete", "user", 0)
	writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
}

// UserAbsence — PUT /api/plans/users/{id}/absence. {status, until}.
func (h *Handler) UserAbsence(w http.ResponseWriter, r *http.Request) {
	if h.users == nil {
		writeErr(w, http.StatusServiceUnavailable, "пользователи недоступны")
		return
	}
	var body struct {
		Status string `json:"status"`
		Until  string `json:"until"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeErr(w, http.StatusBadRequest, "invalid json")
		return
	}
	id := parseInt64(r.PathValue("id"))
	if err := h.users.SetAbsence(r.Context(), id, body.Status, body.Until); err != nil {
		writeErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	h.rec(r, "user_absence", "user", id)
	writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
}

// PlanUsersImportB24 — POST /api/plans/users/import-b24. {b24_ids:[...]}.
// Догружает сотрудников из B24 по ID (даже не заходивших).
func (h *Handler) PlanUsersImportB24(w http.ResponseWriter, r *http.Request) {
	if h.b24 == nil || !h.b24.Enabled() {
		writeErr(w, http.StatusServiceUnavailable, "импорт B24 не настроен (B24_USERGET_WEBHOOK)")
		return
	}
	var body struct {
		B24IDs []int64 `json:"b24_ids"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeErr(w, http.StatusBadRequest, "invalid json")
		return
	}
	if len(body.B24IDs) == 0 {
		writeErr(w, http.StatusBadRequest, "нужен непустой b24_ids")
		return
	}
	res, err := h.b24.Import(r.Context(), body.B24IDs)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	h.rec(r, "users_import_b24", "user", int64(len(res)))
	writeJSON(w, http.StatusOK, res)
}

// OrgTree — GET /api/plans/org. Структура компании + покрытие ЦФО.
func (h *Handler) OrgTree(w http.ResponseWriter, r *http.Request) {
	if h.org == nil {
		writeJSON(w, http.StatusOK, OrgTree{})
		return
	}
	t, err := h.org.Tree(r.Context())
	if err != nil {
		writeErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, t)
}

// OrgSetResponsible — PUT /api/plans/org/responsible. {group, country, user_id, form_code}.
func (h *Handler) OrgSetResponsible(w http.ResponseWriter, r *http.Request) {
	if h.org == nil {
		writeErr(w, http.StatusServiceUnavailable, "структура недоступна")
		return
	}
	var body struct {
		Group    string `json:"group"`
		Country  string `json:"country"`
		UserID   int64  `json:"user_id"`
		FormCode string `json:"form_code"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeErr(w, http.StatusBadRequest, "invalid json")
		return
	}
	if body.Group == "" {
		writeErr(w, http.StatusBadRequest, "нужен group")
		return
	}
	if err := h.org.SetResponsible(r.Context(), body.Group, body.Country, body.UserID, body.FormCode); err != nil {
		writeErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	h.rec(r, "org_responsible", "org", body.UserID)
	writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
}

// PlanUsersSearch — GET /api/plans/users/search?q=. Локальные пользователи по фамилии.
func (h *Handler) PlanUsersSearch(w http.ResponseWriter, r *http.Request) {
	if h.users == nil {
		writeJSON(w, http.StatusOK, []PlanUser{})
		return
	}
	q := strings.TrimSpace(r.URL.Query().Get("q"))
	if len(q) < 2 {
		writeJSON(w, http.StatusOK, []PlanUser{})
		return
	}
	list, err := h.users.Search(r.Context(), q)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, list)
}

// PlanUserUpsert — POST /api/plans/users/upsert. {b24_id, name, last_name, email}.
// Прозрачная догрузка одного выбранного из B24 сотрудника. Возвращает {id, status}.
func (h *Handler) PlanUserUpsert(w http.ResponseWriter, r *http.Request) {
	if h.users == nil {
		writeErr(w, http.StatusServiceUnavailable, "пользователи недоступны")
		return
	}
	var body struct {
		B24ID    int64  `json:"b24_id"`
		Name     string `json:"name"`
		LastName string `json:"last_name"`
		Email    string `json:"email"`
		Domain   string `json:"domain"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeErr(w, http.StatusBadRequest, "invalid json")
		return
	}
	if body.B24ID == 0 {
		writeErr(w, http.StatusBadRequest, "нужен b24_id")
		return
	}
	id, status, err := h.users.UpsertByB24(r.Context(), body.B24ID, body.Name, body.LastName, body.Email, body.Domain)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	h.rec(r, "user_upsert", "user", id)
	writeJSON(w, http.StatusOK, map[string]any{"id": id, "status": status})
}

func itoa(n int64) string {
	if n == 0 {
		return "0"
	}
	neg := n < 0
	if neg {
		n = -n
	}
	var b [20]byte
	i := len(b)
	for n > 0 {
		i--
		b[i] = byte('0' + n%10)
		n /= 10
	}
	if neg {
		i--
		b[i] = '-'
	}
	return string(b[i:])
}
