package plans

import (
	"encoding/json"
	"net/http"
	"strconv"
)

// HTTP-контур карточек форм: состояние, переходы, лист согласования, версии,
// публикация (dry-run/запись), маршрут и маппинг. Общий для МП и розницы.

// CardHandler — ручки процесса карточек.
type CardHandler struct {
	svc     *CardService
	scope   ScopeStore
	prin    PrincipalFunc
	presets *PresetStore
	rec     func(r *http.Request, action, entity string, id int64)
}

// NewCardHandler — конструктор.
func NewCardHandler(svc *CardService, scope ScopeStore, prin PrincipalFunc) *CardHandler {
	return &CardHandler{svc: svc, scope: scope, prin: prin}
}

// WithPresets подключает пресеты представлений (ТЗ Розница §4.6).
func (h *CardHandler) WithPresets(p *PresetStore) *CardHandler { h.presets = p; return h }

// WithAuditRecorder подключает журналирование запросов (как в Handler).
func (h *CardHandler) WithAuditRecorder(f func(r *http.Request, action, entity string, id int64)) *CardHandler {
	h.rec = f
	return h
}

func (h *CardHandler) principal(r *http.Request) Principal {
	if h.prin == nil {
		return Principal{}
	}
	p, _ := h.prin(r)
	return p
}

func (h *CardHandler) audit(r *http.Request, action, entity string, id int64) {
	if h.rec != nil {
		h.rec(r, action, entity, id)
	}
}

// filter — ABAC-фильтр текущего пользователя.
func (h *CardHandler) filter(r *http.Request) (ScopeFilter, error) {
	p := h.principal(r)
	if h.scope == nil {
		return NewScopeFilter(p.PlansAdmin, nil), nil
	}
	return scopeFilterFor(r.Context(), h.scope, p)
}

// CardsList — GET /api/plans/instances/{id}/cards. Карточки периода (создаёт
// отсутствующие: карточка — часть периода, а не отдельная сущность на создание).
func (h *CardHandler) CardsList(w http.ResponseWriter, r *http.Request) {
	plID, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil || plID <= 0 {
		writeErr(w, http.StatusBadRequest, "invalid id")
		return
	}
	p := h.principal(r)
	cards, err := h.svc.EnsureCards(r.Context(), plID, p.UserID)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	f, err := h.filter(r)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	// Видимость карточек — по ABAC (страна/ЮЛ). Скрываем чужие, но не 403:
	// список карточек периода — навигация, а не данные.
	out := make([]Card, 0, len(cards))
	for _, c := range cards {
		if f.AllowsCard(c) {
			out = append(out, c)
		}
	}
	writeJSON(w, http.StatusOK, out)
}

// CardGet — GET /api/plans/cards/{cardId}: состояние + маршрут + лист + версии.
func (h *CardHandler) CardGet(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(w, r, "cardId")
	if !ok {
		return
	}
	c, err := h.svc.Card(r.Context(), id)
	if err != nil {
		writeErr(w, http.StatusNotFound, "карточка не найдена")
		return
	}
	f, err := h.filter(r)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	if !f.AllowsCard(c) {
		writeErr(w, http.StatusForbidden, "карточка вне вашего среза доступа")
		return
	}
	route, _ := h.svc.Route(r.Context(), c.FormCode)
	approvals, _ := h.svc.Approvals(r.Context(), id)
	versions, _ := h.svc.Versions(r.Context(), id)
	if approvals == nil {
		approvals = []CardApproval{}
	}
	if versions == nil {
		versions = []CardVersion{}
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"card":      c,
		"route":     route,
		"approvals": approvals,
		"versions":  versions,
		"editable":  cardEditable(c),
	})
}

// CardAction — POST /api/plans/cards/{cardId}/action.
// Тело: {action, target_step, comment}.
func (h *CardHandler) CardAction(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(w, r, "cardId")
	if !ok {
		return
	}
	var body CardActionInput
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeErr(w, http.StatusBadRequest, "invalid json")
		return
	}
	p := h.principal(r)
	body.ActorID = p.UserID

	c, err := h.svc.Card(r.Context(), id)
	if err != nil {
		writeErr(w, http.StatusNotFound, "карточка не найдена")
		return
	}
	f, err := h.filter(r)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	if !f.AllowsCard(c) {
		writeErr(w, http.StatusForbidden, "карточка вне вашего среза доступа")
		return
	}
	// Право действовать на шаге: reopen/archive — только админ процессов.
	if body.Action == "reopen" || body.Action == "archive" {
		if !p.PlansAdmin {
			writeErr(w, http.StatusForbidden, "переоткрытие периода доступно администратору процессов или финансисту")
			return
		}
	} else if !f.AllowsStep(c.StepCode) {
		writeErr(w, http.StatusForbidden, "шаг "+c.StepCode+" вне вашего среза доступа")
		return
	}

	next, err := h.svc.Action(r.Context(), id, body)
	if err != nil {
		writeErr(w, http.StatusBadRequest, err.Error())
		return
	}
	h.audit(r, "card_"+body.Action, "form_card", id)
	writeJSON(w, http.StatusOK, next)
}

// CardVersionPayload — GET /api/plans/cards/{cardId}/versions/{version}.
// Снимок версии: по нему строится diff «что изменилось с предыдущей версии».
func (h *CardHandler) CardVersionPayload(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(w, r, "cardId")
	if !ok {
		return
	}
	v, err := strconv.ParseInt(r.PathValue("version"), 10, 64)
	if err != nil || v <= 0 {
		writeErr(w, http.StatusBadRequest, "invalid version")
		return
	}
	raw, err := h.svc.VersionPayload(r.Context(), id, v)
	if err != nil {
		writeErr(w, http.StatusNotFound, "версия не найдена")
		return
	}
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	if len(raw) == 0 {
		raw = []byte(`{}`)
	}
	_, _ = w.Write(raw)
}

// CardCalcMode — PUT /api/plans/cards/{cardId}/calc-mode. Тело: {"mode":"inverse"}.
// Переключение направления расчёта (ТЗ МП §3.1) — только админ процессов.
func (h *CardHandler) CardCalcMode(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(w, r, "cardId")
	if !ok {
		return
	}
	var body struct {
		Mode string `json:"mode"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeErr(w, http.StatusBadRequest, "invalid json")
		return
	}
	p := h.principal(r)
	if err := h.svc.SetCalcMode(r.Context(), id, body.Mode, p.UserID); err != nil {
		writeErr(w, http.StatusBadRequest, err.Error())
		return
	}
	h.audit(r, "card_calc_mode", "form_card", id)
	c, _ := h.svc.Card(r.Context(), id)
	writeJSON(w, http.StatusOK, c)
}

// CardPublish — POST /api/plans/cards/{cardId}/publish?mode=dry_run|write.
// dry-run доступен согласующему: это инструмент подтверждения маппинга у BI.
func (h *CardHandler) CardPublish(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(w, r, "cardId")
	if !ok {
		return
	}
	mode := r.URL.Query().Get("mode")
	if mode == "" {
		mode = PublishModeDryRun
	}
	p := h.principal(r)
	if mode == PublishModeWrite && !p.PlansAdmin {
		writeErr(w, http.StatusForbidden, "запись в приёмник доступна администратору процессов или финансисту")
		return
	}
	res, err := h.svc.Publish(r.Context(), id, mode, p.UserID)
	if err != nil {
		// Результат отдаём даже при ошибке: в нём отчёт, что именно не сошлось.
		writeJSON(w, http.StatusOK, res)
		return
	}
	h.audit(r, "card_publish_"+mode, "form_card", id)
	writeJSON(w, http.StatusOK, res)
}

// CardPublishLog — GET /api/plans/cards/{cardId}/publish-log.
func (h *CardHandler) CardPublishLog(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(w, r, "cardId")
	if !ok {
		return
	}
	list, err := h.svc.PublishLog(r.Context(), id)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	if list == nil {
		list = []PublishLogItem{}
	}
	writeJSON(w, http.StatusOK, list)
}

// FormRoute — GET /api/plans/forms/{formCode}/route.
func (h *CardHandler) FormRoute(w http.ResponseWriter, r *http.Request) {
	code := r.PathValue("formCode")
	steps, err := h.svc.Route(r.Context(), code)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	if steps == nil {
		steps = []RouteStep{}
	}
	writeJSON(w, http.StatusOK, steps)
}

// FormRouteSave — PUT /api/plans/forms/{formCode}/route. Тело: шаг маршрута.
// Так включается шаг «Финансист» — данными, без правки кода (ТЗ МП §2.1).
func (h *CardHandler) FormRouteSave(w http.ResponseWriter, r *http.Request) {
	var st RouteStep
	if err := json.NewDecoder(r.Body).Decode(&st); err != nil {
		writeErr(w, http.StatusBadRequest, "invalid json")
		return
	}
	st.FormCode = r.PathValue("formCode")
	p := h.principal(r)
	if err := h.svc.SaveRouteStep(r.Context(), st, p.UserID); err != nil {
		writeErr(w, http.StatusBadRequest, err.Error())
		return
	}
	h.audit(r, "route_step_save", "plans_form_route", 0)
	steps, _ := h.svc.Route(r.Context(), st.FormCode)
	writeJSON(w, http.StatusOK, steps)
}

// PublishMappings — GET /api/plans/forms/{formCode}/publish-mapping.
func (h *CardHandler) PublishMappings(w http.ResponseWriter, r *http.Request) {
	list, err := h.svc.Mappings(r.Context(), r.PathValue("formCode"))
	if err != nil {
		writeErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	if list == nil {
		list = []PublishMapping{}
	}
	writeJSON(w, http.StatusOK, list)
}

// PublishMappingSave — PUT /api/plans/forms/{formCode}/publish-mapping.
// Ответы BI по §12 (какой Параметр, агрегат/детализация, КодPL) ложатся сюда.
func (h *CardHandler) PublishMappingSave(w http.ResponseWriter, r *http.Request) {
	var m PublishMapping
	if err := json.NewDecoder(r.Body).Decode(&m); err != nil {
		writeErr(w, http.StatusBadRequest, "invalid json")
		return
	}
	m.FormCode = r.PathValue("formCode")
	p := h.principal(r)
	if err := h.svc.SaveMapping(r.Context(), m, p.UserID); err != nil {
		writeErr(w, http.StatusBadRequest, err.Error())
		return
	}
	h.audit(r, "publish_mapping_save", "publish_mapping", m.ID)
	list, _ := h.svc.Mappings(r.Context(), m.FormCode)
	writeJSON(w, http.StatusOK, list)
}

// Forms — GET /api/plans/forms. Реестр форм с их областями (карточками).
func (h *CardHandler) Forms(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, FormRegistry())
}

// pathID — числовой параметр пути с единой обработкой ошибки.
func pathID(w http.ResponseWriter, r *http.Request, name string) (int64, bool) {
	id, err := strconv.ParseInt(r.PathValue(name), 10, 64)
	if err != nil || id <= 0 {
		writeErr(w, http.StatusBadRequest, "invalid "+name)
		return 0, false
	}
	return id, true
}
