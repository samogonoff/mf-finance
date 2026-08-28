package plans

import (
	"errors"
	"fmt"
	"strings"
	"time"
)

// Карточка формы — общая оболочка процесса для всех форм комплекта (ТЗ МП §2,
// ТЗ Розница §2). Единица маршрута — форма, а не этап периода: возврат по мелким
// МП не блокирует крупные, возврат по одной стране розницы не блокирует остальные.
//
// Здесь только чистая логика переходов (без БД) — на ней держится корректность
// статусов, поэтому она отделена от card_store.go/card_service.go и покрыта
// тестами (card_test.go).

// Статусы карточки. Составные метки ТЗ (on_approval_1_2, returned_to_1_1)
// собираются из пары status+step_code — см. CardStatusLabel.
const (
	CardDraft         = "draft"
	CardOnApproval    = "on_approval"
	CardReturned      = "returned"
	CardApproved      = "approved"
	CardPublished     = "published"
	CardPublishFailed = "publish_failed"
	CardArchived      = "archived"
)

// Виды шагов маршрута.
const (
	StepFill    = "fill"    // заполнение (1.1)
	StepApprove = "approve" // согласование (финансист, 1.2, 1.3)
	StepFinal   = "final"   // утверждение (1.4) — закрывает период и триггерит публикацию
)

// Режимы расчёта карточки: legacy — действующий каскад «суммы → доли»,
// inverse — направление ТЗ МП §3.1 «доли/наценки → суммы».
const (
	CalcLegacy  = "legacy"
	CalcInverse = "inverse"
)

// RouteStep — шаг маршрута формы (plans_form_route). Данные, не код: шаг
// «Финансист» между 1.2 и 1.3 включается флагом Enabled (ТЗ МП §2.1).
type RouteStep struct {
	FormCode         string `json:"form_code"`
	StepCode         string `json:"step_code"`
	StepName         string `json:"step_name"`
	SortOrder        int    `json:"sort_order"`
	Kind             string `json:"kind"`
	Responsible      string `json:"responsible"`
	PositionCode     string `json:"position_code"`
	DueRD            int    `json:"due_rd"`
	Enabled          bool   `json:"enabled"`
	SkipIfSameUser   bool   `json:"skip_if_same_user"`
	PublishOnApprove bool   `json:"publish_on_approve"`
}

// Card — карточка формы в периоде.
type Card struct {
	ID             int64          `json:"id"`
	PlID           int64          `json:"pl_id"`
	FormCode       string         `json:"form_code"`
	ScopeKey       string         `json:"scope_key"`
	Title          string         `json:"title"`
	Year           int            `json:"year"`
	Month          int            `json:"month"`
	Country        string         `json:"country"`
	LegalEntity    string         `json:"legal_entity"`
	Currency       string         `json:"currency"`
	CalcMode       string         `json:"calc_mode"`
	Status         string         `json:"status"`
	StepCode       string         `json:"step_code"`
	StatusLabel    string         `json:"status_label"`
	CurrentVersion int            `json:"current_version"`
	FxSnapshot     map[string]any `json:"fx_snapshot,omitempty"`
	Locked         bool           `json:"locked"`
	DueAt          *time.Time     `json:"due_at,omitempty"`
	CreatedBy      int64          `json:"created_by"`
	UpdatedAt      time.Time      `json:"updated_at"`
}

// CardActionInput — запрос на переход по маршруту.
type CardActionInput struct {
	Action     string `json:"action"`      // submit|approve|return|reopen|archive
	TargetStep string `json:"target_step"` // обязателен при return
	Comment    string `json:"comment"`     // обязателен при return и reopen
	ActorID    int64  `json:"-"`
	// StepOwner — кто назначен на шаг (для skip_if_same_user). Ключ — step_code.
	StepOwner map[string]int64 `json:"-"`
}

// CardTransition — результат перехода: новое состояние + побочные требования.
type CardTransition struct {
	Status      string          `json:"status"`
	StepCode    string          `json:"step_code"`
	Locked      bool            `json:"locked"`
	VersionBump bool            `json:"version_bump"`
	Publish     bool            `json:"publish"`     // сработал шаг с publish_on_approve
	RevokeFrom  string          `json:"revoke_from"` // с какого шага аннулировать согласования
	Approvals   []ApprovalWrite `json:"approvals"`   // что записать в лист согласования
}

// ApprovalWrite — запись, которую нужно положить в card_approval.
type ApprovalWrite struct {
	StepCode   string `json:"step_code"`
	Decision   string `json:"decision"`
	TargetStep string `json:"target_step"`
	Comment    string `json:"comment"`
	UserID     int64  `json:"user_id"`
}

// CardStatusLabel — человекочитаемая метка ТЗ: on_approval_1.2 / returned_to_1.1.
func CardStatusLabel(status, step string) string {
	switch status {
	case CardOnApproval:
		return "on_approval_" + step
	case CardReturned:
		return "returned_to_" + step
	default:
		return status
	}
}

// enabledRoute — включённые шаги маршрута по порядку.
func enabledRoute(route []RouteStep) []RouteStep {
	out := make([]RouteStep, 0, len(route))
	for _, s := range route {
		if s.Enabled {
			out = append(out, s)
		}
	}
	// Порядок задаётся sort_order; сортировка вставками — набор из 4–6 шагов.
	for i := 1; i < len(out); i++ {
		for j := i; j > 0 && out[j].SortOrder < out[j-1].SortOrder; j-- {
			out[j], out[j-1] = out[j-1], out[j]
		}
	}
	return out
}

// stepIndex — позиция шага в маршруте (−1, если шаг выключен/неизвестен).
func stepIndex(route []RouteStep, code string) int {
	for i, s := range route {
		if s.StepCode == code {
			return i
		}
	}
	return -1
}

// firstStep — первый включённый шаг (шаг заполнения).
func firstStep(route []RouteStep) (RouteStep, bool) {
	if len(route) == 0 {
		return RouteStep{}, false
	}
	return route[0], true
}

// applyCardAction — чистый переход по маршруту. Правила ТЗ:
//   - вперёд можно только с шага, на котором карточка стоит;
//   - возврат требует целевого шага (строго раньше текущего) и комментария;
//   - при возврате согласования от целевого шага и выше аннулируются;
//   - утверждение на шаге final закрывает карточку на запись и триггерит публикацию;
//   - шаг с skip_if_same_user проходится автоматически, если его владелец —
//     тот же человек, что инициировал переход (вырожденное самосогласование).
func applyCardAction(c Card, allSteps []RouteStep, in CardActionInput) (CardTransition, error) {
	route := enabledRoute(allSteps)
	if len(route) == 0 {
		return CardTransition{}, errors.New("маршрут формы не настроен")
	}
	action := strings.TrimSpace(in.Action)
	comment := strings.TrimSpace(in.Comment)

	switch action {
	case "submit", "approve":
		if c.Status == CardApproved || c.Status == CardPublished || c.Status == CardArchived {
			return CardTransition{}, errors.New("период закрыт: изменения только через reopen с причиной")
		}
		idx := stepIndex(route, c.StepCode)
		if idx < 0 {
			return CardTransition{}, fmt.Errorf("шаг %q не входит в маршрут формы", c.StepCode)
		}
		cur := route[idx]
		if action == "submit" && cur.Kind != StepFill {
			return CardTransition{}, errors.New("«отправить» доступно только на шаге заполнения")
		}
		if action == "approve" && cur.Kind == StepFill {
			return CardTransition{}, errors.New("на шаге заполнения используется «отправить»")
		}

		tr := CardTransition{}
		tr.Approvals = append(tr.Approvals, ApprovalWrite{
			StepCode: cur.StepCode, Decision: action, Comment: comment, UserID: in.ActorID,
		})
		// Утверждение на финальном шаге закрывает карточку.
		if cur.Kind == StepFinal && action == "approve" {
			tr.Status = CardApproved
			tr.StepCode = cur.StepCode
			tr.Locked = true
			tr.VersionBump = true
			tr.Publish = cur.PublishOnApprove
			return tr, nil
		}
		// Иначе двигаемся вперёд, пропуская вырожденное самосогласование.
		next := idx + 1
		for next < len(route) {
			s := route[next]
			if s.SkipIfSameUser && in.ActorID != 0 {
				owner := in.StepOwner[s.StepCode]
				if owner == 0 || owner == in.ActorID {
					tr.Approvals = append(tr.Approvals, ApprovalWrite{
						StepCode: s.StepCode, Decision: "auto_skipped", UserID: in.ActorID,
						Comment: "согласовано автоматически: совпадение исполнителя и согласующего",
					})
					next++
					continue
				}
			}
			break
		}
		if next >= len(route) {
			// Все шаги пройдены (маршрут без final) — считаем карточку утверждённой.
			tr.Status = CardApproved
			tr.StepCode = route[len(route)-1].StepCode
			tr.Locked = true
			tr.VersionBump = true
			tr.Publish = route[len(route)-1].PublishOnApprove
			return tr, nil
		}
		tr.Status = CardOnApproval
		tr.StepCode = route[next].StepCode
		tr.VersionBump = true
		// Если следующий шаг — финальный с автопубликацией, публикация случится
		// только по его approve, не сейчас.
		return tr, nil

	case "return":
		if comment == "" {
			return CardTransition{}, errors.New("возврат без комментария невозможен")
		}
		if in.TargetStep == "" {
			return CardTransition{}, errors.New("укажите шаг, на который возвращаете")
		}
		curIdx := stepIndex(route, c.StepCode)
		tgtIdx := stepIndex(route, in.TargetStep)
		if tgtIdx < 0 {
			return CardTransition{}, fmt.Errorf("шаг %q не входит в маршрут формы", in.TargetStep)
		}
		if curIdx >= 0 && tgtIdx >= curIdx {
			return CardTransition{}, errors.New("возврат возможен только на предыдущий шаг")
		}
		if c.Status == CardApproved || c.Status == CardPublished {
			return CardTransition{}, errors.New("утверждённый период возвращают через reopen с причиной")
		}
		return CardTransition{
			Status:      CardReturned,
			StepCode:    in.TargetStep,
			VersionBump: true,
			RevokeFrom:  in.TargetStep,
			Approvals: []ApprovalWrite{{
				StepCode: c.StepCode, Decision: "return",
				TargetStep: in.TargetStep, Comment: comment, UserID: in.ActorID,
			}},
		}, nil

	case "reopen":
		if comment == "" {
			return CardTransition{}, errors.New("переоткрытие периода требует причины")
		}
		if c.Status != CardApproved && c.Status != CardPublished && c.Status != CardPublishFailed {
			return CardTransition{}, errors.New("переоткрывать можно только утверждённый период")
		}
		first, _ := firstStep(route)
		return CardTransition{
			Status:      CardDraft,
			StepCode:    first.StepCode,
			Locked:      false,
			VersionBump: true,
			RevokeFrom:  first.StepCode,
			Approvals: []ApprovalWrite{{
				StepCode: c.StepCode, Decision: "reopen", Comment: comment, UserID: in.ActorID,
			}},
		}, nil

	case "archive":
		return CardTransition{
			Status: CardArchived, StepCode: c.StepCode, Locked: true,
			Approvals: []ApprovalWrite{{StepCode: c.StepCode, Decision: "archive", Comment: comment, UserID: in.ActorID}},
		}, nil
	}
	return CardTransition{}, fmt.Errorf("неизвестное действие %q", action)
}

// cardEditable — можно ли править значения карточки. После утверждения период
// закрыт на запись ДЛЯ ВСЕХ, включая автора (ТЗ Розница §2.4, МП §2.3).
func cardEditable(c Card) bool {
	if c.Locked {
		return false
	}
	switch c.Status {
	case CardDraft, CardReturned:
		return true
	default:
		return false
	}
}
