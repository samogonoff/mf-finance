package plans

import (
	"errors"
	"fmt"
)

// Workflow согласования (SPEC §4, §12). Два трека (sales/production) + финал 3–4,
// зависимости WF-DEP. Машина состояний чистая (тестируема без БД); персист —
// StageStore. Назначение людей/стран — ABAC (plans_user_scope), полный конфигуратор
// маршрута — далее. Здесь: фиксированный маршрут схемы + переходы + SLA-сроки.

// StageDef — определение этапа маршрута.
type StageDef struct {
	Code       string
	Track      string // sales|production|final
	Name       string
	DueRD      int    // N-й рабочий день месяца (0 = спец-срок)
	DuePrev25  bool   // срок = 25-е предыдущего месяца (производство)
	DependsOn  []string
	IsApproval bool
}

// stageDefs — маршрут по схеме (этапы 1.1–1.6, 2.1–2.4, 3, 4).
func stageDefs() []StageDef {
	return []StageDef{
		{Code: "2.1", Track: "production", Name: "Минуты и объёмы производства", DuePrev25: true},
		{Code: "2.2", Track: "production", Name: "Согласование планов производства", DuePrev25: true, DependsOn: []string{"2.1"}, IsApproval: true},
		{Code: "1.1", Track: "sales", Name: "Заполнение бюджетов продаж ЦП", DueRD: 2},
		{Code: "1.2", Track: "sales", Name: "Согласование руководителем подразделения", DueRD: 3, DependsOn: []string{"1.1"}, IsApproval: true},
		{Code: "1.3", Track: "sales", Name: "Согласование директором направления", DueRD: 3, DependsOn: []string{"1.2"}, IsApproval: true},
		{Code: "2.3", Track: "production", Name: "Обновление бюджетов ЦЗ", DueRD: 3, DependsOn: []string{"2.2"}},
		{Code: "1.4", Track: "sales", Name: "Финальное согласование потока продаж", DueRD: 4, DependsOn: []string{"1.3"}, IsApproval: true},
		{Code: "2.4", Track: "production", Name: "Формирование бюджетов по ЮЛ (пр-во)", DueRD: 4, DependsOn: []string{"2.3"}},
		{Code: "1.5", Track: "sales", Name: "Обновление расходов в бюджетах ЦП", DueRD: 5, DependsOn: []string{"1.4"}},
		{Code: "1.6", Track: "sales", Name: "Формирование по ЮЛ и каналам", DueRD: 5, DependsOn: []string{"1.5", "2.4"}}, // WF-DEP-01
		{Code: "3", Track: "final", Name: "Согласование бюджетов ЮЛ", DueRD: 6, DependsOn: []string{"1.6"}, IsApproval: true}, // WF-DEP-02
		{Code: "4", Track: "final", Name: "Итоговое утверждение", DueRD: 6, DependsOn: []string{"3"}, IsApproval: true},
	}
}

func stageDefByCode(code string) (StageDef, bool) {
	for _, d := range stageDefs() {
		if d.Code == code {
			return d, true
		}
	}
	return StageDef{}, false
}

// StageState — состояние этапа экземпляра PL.
type StageState struct {
	Code      string   `json:"stage_code"`
	Track     string   `json:"track"`
	Name      string   `json:"name"`
	Status    string   `json:"status"` // pending|in_progress|completed|returned|blocked
	DueDate   string   `json:"due_at"`
	DependsOn []string `json:"depends_on"`
}

// initStages — стартовые этапы периода со сроками по календарю страны.
func initStages(year, month int, country string, seed []CountryCalendar) []StageState {
	cal := calendarFor(country, seed)
	out := make([]StageState, 0, len(stageDefs()))
	for _, d := range stageDefs() {
		due := ""
		if d.DuePrev25 {
			py, pm := year, month-1
			if pm == 0 {
				pm = 12
				py--
			}
			due = fmt.Sprintf("%04d-%02d-25", py, pm)
		} else if d.DueRD > 0 {
			if v, err := businessDayDate(year, month, d.DueRD, cal); err == nil {
				due = v
			}
		}
		out = append(out, StageState{
			Code: d.Code, Track: d.Track, Name: d.Name,
			Status: "pending", DueDate: due, DependsOn: d.DependsOn,
		})
	}
	return out
}

func statusOf(stages []StageState, code string) string {
	for _, s := range stages {
		if s.Code == code {
			return s.Status
		}
	}
	return ""
}

// depsCompleted — все ли зависимости этапа завершены.
func depsCompleted(def StageDef, stages []StageState) (string, bool) {
	for _, dep := range def.DependsOn {
		if statusOf(stages, dep) != "completed" {
			return dep, false
		}
	}
	return "", true
}

// applyStageAction применяет действие к этапу с проверкой зависимостей (WF-DEP).
// Действия: start, submit, approve, return (target — этап возврата).
func applyStageAction(stages []StageState, code, action, target string) ([]StageState, error) {
	def, ok := stageDefByCode(code)
	if !ok {
		return nil, fmt.Errorf("неизвестный этап %q", code)
	}
	out := make([]StageState, len(stages))
	copy(out, stages)
	idx := -1
	for i := range out {
		if out[i].Code == code {
			idx = i
		}
	}
	if idx < 0 {
		return nil, fmt.Errorf("этап %q не инициализирован", code)
	}

	switch action {
	case "start", "submit", "approve":
		if dep, ok := depsCompleted(def, out); !ok {
			return nil, fmt.Errorf("этап %s ждёт завершения %s (WF-DEP)", code, dep)
		}
		if action == "start" {
			out[idx].Status = "in_progress"
		} else {
			out[idx].Status = "completed"
		}
	case "return":
		if target == "" {
			return nil, errors.New("возврат требует целевой этап")
		}
		if _, ok := stageDefByCode(target); !ok {
			return nil, fmt.Errorf("неизвестный целевой этап %q", target)
		}
		out[idx].Status = "returned"
		for i := range out {
			if out[i].Code == target {
				out[i].Status = "in_progress"
			}
		}
	default:
		return nil, fmt.Errorf("неизвестное действие %q", action)
	}
	return out, nil
}
