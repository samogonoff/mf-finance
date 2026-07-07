package plans

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
)

// HTTP-ручки движка заданий процесса. Чтение — ROLE_PLANS_USER; генерация/назначение
// владельца — ROLE_PLANS_ADMIN; действия по заданию — участник (state-machine
// гарантирует валидные переходы; тонкие права — следующим срезом).

// TasksList — GET /api/plans/instances/{id}/tasks. Задания периода по этапам.
func (h *Handler) TasksList(w http.ResponseWriter, r *http.Request) {
	if h.tasks == nil {
		writeJSON(w, http.StatusOK, []Task{})
		return
	}
	list, err := h.tasks.ListByInstance(r.Context(), parseInt64(r.PathValue("id")))
	if err != nil {
		writeErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, list)
}

// TasksGenerate — POST /api/plans/instances/{id}/tasks/generate. Раскрыть шаблоны.
func (h *Handler) TasksGenerate(w http.ResponseWriter, r *http.Request) {
	if h.tasks == nil {
		writeErr(w, http.StatusServiceUnavailable, "движок заданий недоступен")
		return
	}
	n, err := h.tasks.Generate(r.Context(), parseInt64(r.PathValue("id")))
	if err != nil {
		writeErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	h.rec(r, "tasks_generate", "instance", parseInt64(r.PathValue("id")))
	writeJSON(w, http.StatusOK, map[string]int{"generated": n})
}

// TaskDataView — GET /api/plans/tasks/{taskId}/data. Срез данных задания
// (факт/стратегия/тактика/расчёт + корректировки) для проверки на этапе.
func (h *Handler) TaskDataView(w http.ResponseWriter, r *http.Request) {
	if h.tasks == nil {
		writeJSON(w, http.StatusOK, TaskData{})
		return
	}
	data, err := h.tasks.TaskData(r.Context(), parseInt64(r.PathValue("taskId")))
	if err != nil {
		writeErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, data)
}

// MpTaskFormGet — GET /api/plans/tasks/{taskId}/mp-form. Структура формы МП задания.
func (h *Handler) MpTaskFormGet(w http.ResponseWriter, r *http.Request) {
	if h.tasks == nil {
		writeErr(w, http.StatusServiceUnavailable, "движок недоступен")
		return
	}
	f, err := h.tasks.MpFormData(r.Context(), parseInt64(r.PathValue("taskId")))
	if err != nil {
		writeErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, f)
}

// MpTaskFormExport — GET /api/plans/tasks/{taskId}/mp-form/export. .xlsx формы (TPL-06).
func (h *Handler) MpTaskFormExport(w http.ResponseWriter, r *http.Request) {
	if h.tasks == nil {
		writeErr(w, http.StatusServiceUnavailable, "движок недоступен")
		return
	}
	taskID := parseInt64(r.PathValue("taskId"))
	data, err := h.tasks.MpFormExport(r.Context(), taskID)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	w.Header().Set("Content-Type", "application/vnd.openxmlformats-officedocument.spreadsheetml.sheet")
	w.Header().Set("Content-Disposition", fmt.Sprintf(`attachment; filename="TPL-MP_task%d.xlsx"`, taskID))
	_, _ = w.Write(data)
}

// MpTaskFormImport — POST /api/plans/tasks/{taskId}/mp-form/import. Тело — .xlsx.
func (h *Handler) MpTaskFormImport(w http.ResponseWriter, r *http.Request) {
	if h.tasks == nil {
		writeErr(w, http.StatusServiceUnavailable, "движок недоступен")
		return
	}
	data, _ := io.ReadAll(io.LimitReader(r.Body, 10<<20))
	prin := h.prin(r)
	n, err := h.tasks.MpFormImport(r.Context(), parseInt64(r.PathValue("taskId")), prin.UserID, prin.PlansAdmin, data)
	if err != nil {
		writeErr(w, http.StatusBadRequest, err.Error())
		return
	}
	h.rec(r, "mp_form_import", "task", parseInt64(r.PathValue("taskId")))
	writeJSON(w, http.StatusOK, map[string]int{"imported": n})
}

// MpTaskFormSave — PUT /api/plans/tasks/{taskId}/mp-form. {rows:[...]}.
func (h *Handler) MpTaskFormSave(w http.ResponseWriter, r *http.Request) {
	if h.tasks == nil {
		writeErr(w, http.StatusServiceUnavailable, "движок недоступен")
		return
	}
	raw, _ := io.ReadAll(io.LimitReader(r.Body, 1<<20))
	var body struct {
		Rows []MpSaveRow `json:"rows"`
	}
	if err := json.Unmarshal(raw, &body); err != nil {
		writeErr(w, http.StatusBadRequest, "invalid json")
		return
	}
	prin := h.prin(r)
	if err := h.tasks.SaveMpForm(r.Context(), parseInt64(r.PathValue("taskId")), prin.UserID, prin.PlansAdmin, body.Rows, raw); err != nil {
		writeErr(w, http.StatusBadRequest, err.Error())
		return
	}
	h.rec(r, "mp_form_save", "task", parseInt64(r.PathValue("taskId")))
	writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
}

// PnlView — GET /api/plans/instances/{id}/pnl. Сводное окно (все сценарии + сравнение).
func (h *Handler) PnlView(w http.ResponseWriter, r *http.Request) {
	if h.tasks == nil {
		writeJSON(w, http.StatusOK, PnlSummary{})
		return
	}
	res, err := h.tasks.Pnl(r.Context(), parseInt64(r.PathValue("id")))
	if err != nil {
		writeErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, res)
}

// StrategyImport — POST /api/plans/instances/{id}/strategy/import. Тело — .xlsx.
func (h *Handler) StrategyImport(w http.ResponseWriter, r *http.Request) {
	if h.tasks == nil {
		writeErr(w, http.StatusServiceUnavailable, "движок недоступен")
		return
	}
	data, _ := io.ReadAll(io.LimitReader(r.Body, 10<<20))
	n, err := h.tasks.ImportStrategy(r.Context(), parseInt64(r.PathValue("id")), data)
	if err != nil {
		writeErr(w, http.StatusBadRequest, err.Error())
		return
	}
	h.rec(r, "strategy_import", "instance", parseInt64(r.PathValue("id")))
	writeJSON(w, http.StatusOK, map[string]int{"imported": n})
}

// TaskAssign — PUT /api/plans/tasks/{taskId}/assignee. {user_id}. Переназначить
// исполнителя (админ планов). Разблокирует задания-сироты без перегенерации.
func (h *Handler) TaskAssign(w http.ResponseWriter, r *http.Request) {
	if h.tasks == nil {
		writeErr(w, http.StatusServiceUnavailable, "движок недоступен")
		return
	}
	var body struct {
		UserID int64 `json:"user_id"`
	}
	_ = json.NewDecoder(io.LimitReader(r.Body, 1<<16)).Decode(&body)
	if err := h.tasks.SetAssignee(r.Context(), parseInt64(r.PathValue("taskId")), body.UserID); err != nil {
		writeErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	h.rec(r, "task_assign", "task", parseInt64(r.PathValue("taskId")))
	writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
}

// MyTasks — GET /api/plans/tasks/mine. Задания, где я исполнитель или делегат.
func (h *Handler) MyTasks(w http.ResponseWriter, r *http.Request) {
	if h.tasks == nil {
		writeJSON(w, http.StatusOK, []Task{})
		return
	}
	list, err := h.tasks.ListByUser(r.Context(), h.prin(r).UserID)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, list)
}

// AllTasks — GET /api/plans/tasks/all. ВСЕ задания всех карточек (админ планов).
func (h *Handler) AllTasks(w http.ResponseWriter, r *http.Request) {
	if h.tasks == nil {
		writeJSON(w, http.StatusOK, []Task{})
		return
	}
	list, err := h.tasks.ListAll(r.Context())
	if err != nil {
		writeErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, list)
}

// TaskAction — POST /api/plans/tasks/{taskId}/action. {action, delegate_user_id?}.
func (h *Handler) TaskAction(w http.ResponseWriter, r *http.Request) {
	if h.tasks == nil {
		writeErr(w, http.StatusServiceUnavailable, "движок заданий недоступен")
		return
	}
	var body struct {
		Action         string `json:"action"`
		DelegateUserID int64  `json:"delegate_user_id"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeErr(w, http.StatusBadRequest, "invalid json")
		return
	}
	taskID := parseInt64(r.PathValue("taskId"))
	prin := h.prin(r)
	if err := h.tasks.Action(r.Context(), taskID, prin.UserID, prin.PlansAdmin, body.Action, body.DelegateUserID); err != nil {
		writeErr(w, http.StatusBadRequest, err.Error())
		return
	}
	h.rec(r, "task_"+body.Action, "task", taskID)
	writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
}

// --- Конструктор шаблонов ---

// TaskTemplatesList — GET /api/plans/task-templates.
func (h *Handler) TaskTemplatesList(w http.ResponseWriter, r *http.Request) {
	if h.tasks == nil {
		writeJSON(w, http.StatusOK, []TaskTemplate{})
		return
	}
	list, err := h.tasks.ListTemplates(r.Context())
	if err != nil {
		writeErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, list)
}

// TaskTemplateUpsert — PUT /api/plans/task-templates.
func (h *Handler) TaskTemplateUpsert(w http.ResponseWriter, r *http.Request) {
	if h.tasks == nil {
		writeErr(w, http.StatusServiceUnavailable, "движок недоступен")
		return
	}
	var t TaskTemplate
	if err := json.NewDecoder(r.Body).Decode(&t); err != nil {
		writeErr(w, http.StatusBadRequest, "invalid json")
		return
	}
	if t.StageCode == "" || t.FormCode == "" || t.Title == "" {
		writeErr(w, http.StatusBadRequest, "stage_code, form_code, title обязательны")
		return
	}
	id, err := h.tasks.UpsertTemplate(r.Context(), t)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	h.rec(r, "task_template_upsert", "template", id)
	writeJSON(w, http.StatusOK, map[string]int64{"id": id})
}

// TaskTemplateDelete — DELETE /api/plans/task-templates/{id}.
func (h *Handler) TaskTemplateDelete(w http.ResponseWriter, r *http.Request) {
	if h.tasks == nil {
		writeErr(w, http.StatusServiceUnavailable, "движок недоступен")
		return
	}
	if err := h.tasks.DeleteTemplate(r.Context(), parseInt64(r.PathValue("id"))); err != nil {
		writeErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	h.rec(r, "task_template_delete", "template", 0)
	writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
}

// StageOwnerSet — PUT /api/plans/instances/{id}/stages/{code}/owner. {user_id}.
func (h *Handler) StageOwnerSet(w http.ResponseWriter, r *http.Request) {
	if h.tasks == nil {
		writeErr(w, http.StatusServiceUnavailable, "движок заданий недоступен")
		return
	}
	var body struct {
		UserID int64 `json:"user_id"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeErr(w, http.StatusBadRequest, "invalid json")
		return
	}
	plID := parseInt64(r.PathValue("id"))
	if err := h.tasks.SetOwner(r.Context(), plID, r.PathValue("code"), body.UserID); err != nil {
		writeErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	h.rec(r, "stage_owner", "instance", plID)
	writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
}

// StageOwnersList — GET /api/plans/instances/{id}/stage-owners.
func (h *Handler) StageOwnersList(w http.ResponseWriter, r *http.Request) {
	if h.tasks == nil {
		writeJSON(w, http.StatusOK, []StageOwnerInfo{})
		return
	}
	list, err := h.tasks.StageOwners(r.Context(), parseInt64(r.PathValue("id")))
	if err != nil {
		writeErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, list)
}

// StageReadiness — GET /api/plans/instances/{id}/stages/{code}/readiness.
// Готовность этапа к продвижению (все ли задания done) — для владельца этапа.
func (h *Handler) StageReadiness(w http.ResponseWriter, r *http.Request) {
	if h.tasks == nil {
		writeJSON(w, http.StatusOK, map[string]any{"ready": false})
		return
	}
	ready, done, total, err := h.tasks.StageAllDone(r.Context(), parseInt64(r.PathValue("id")), r.PathValue("code"))
	if err != nil {
		writeErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"ready": ready, "done": done, "total": total})
}
