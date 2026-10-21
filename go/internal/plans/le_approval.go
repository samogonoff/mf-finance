package plans

import (
	"context"
	"errors"
	"fmt"
	"log"
	"sort"
	"strings"
	"time"
)

// Согласование бюджетов ЮЛ (этап 3) и итоговое утверждение (этап 4) на экране «Бюджет по ЮЛ».
//
// Этап 3 — по каждому ЮЛ свой список согласующих, этап 4 — один список на период. У списка политика: «нужны все» или
// «достаточно одного» (настраивается). Этап завершается сам, когда выполнены все списки этапа. Возврат любым согласующим
// отправляет период назад (с этапа 3 — на 1.6, с этапа 4 — на 3) с обязательной причиной. Корректировка итога ЮЛ снимает
// согласия по этому ЮЛ и итоговое утверждение: согласовывали другие цифры.

// Решения согласующих.
const (
	LeApprove = "approve"
	LeReturn  = "return"
)

// LeApprover — согласующий списка (этап 3: ЮЛ; этап 4: legal_entity пусто).
type LeApprover struct {
	StageCode   string `json:"stage_code"`
	LegalEntity string `json:"legal_entity"`
	UserID      int64  `json:"user_id"`
	UserName    string `json:"user_name,omitempty"`
}

// LePolicy — политика списка: RequireAll — согласие каждого, иначе достаточно одного.
type LePolicy struct {
	StageCode   string `json:"stage_code"`
	LegalEntity string `json:"legal_entity"`
	RequireAll  bool   `json:"require_all"`
}

// LeDecision — решение согласующего.
type LeDecision struct {
	ID          int64     `json:"id"`
	PlID        int64     `json:"pl_id"`
	StageCode   string    `json:"stage_code"`
	LegalEntity string    `json:"legal_entity"`
	UserID      int64     `json:"user_id"`
	UserName    string    `json:"user_name,omitempty"`
	Decision    string    `json:"decision"`
	Comment     string    `json:"comment"`
	CreatedAt   time.Time `json:"created_at"`
	Revoked     bool      `json:"revoked"`
	RevokeNote  string    `json:"revoke_reason,omitempty"`
}

// LeApprovalRepo — хранилище настроек и решений.
type LeApprovalRepo interface {
	Approvers(ctx context.Context) ([]LeApprover, error)
	Policies(ctx context.Context) ([]LePolicy, error)
	// SetApprovers заменяет список согласующих области и её политику.
	SetApprovers(ctx context.Context, stage, legal string, userIDs []int64, requireAll bool, actor int64) error
	Decisions(ctx context.Context, plID int64) ([]LeDecision, error)
	AddDecision(ctx context.Context, d LeDecision) (int64, error)
	// RevokeDecisions снимает согласия этапа: по одному ЮЛ или по всем (allLegals); возвращает число снятых.
	RevokeDecisions(ctx context.Context, plID int64, stage, legal string, allLegals bool, reason string) (int, error)
	// Lock берёт блокировку периода на время операции согласования: решения и завершение этапов по одному периоду идут по одному
	// (иначе два согласующих, решающих одновременно, оба завершили бы этап). unlock обязателен.
	Lock(ctx context.Context, plID int64) (unlock func(), err error)
	// OpenPeriods — периоды, у которых завершён этап 1.6 (кандидаты на завершение этапов 3 и 4).
	OpenPeriods(ctx context.Context) ([]int64, error)
}

// LeApproverState — согласующий и его решение.
type LeApproverState struct {
	UserID   int64  `json:"user_id"`
	UserName string `json:"user_name,omitempty"`
	Approved bool   `json:"approved"`
	Comment  string `json:"comment,omitempty"`
	// Mine — это текущий пользователь.
	Mine bool `json:"mine,omitempty"`
}

// LeScopeStatus — состояние одного списка согласующих.
type LeScopeStatus struct {
	StageCode   string            `json:"stage_code"`
	LegalEntity string            `json:"legal_entity"`
	RequireAll  bool              `json:"require_all"`
	Approvers   []LeApproverState `json:"approvers"`
	NoApprovers bool              `json:"no_approvers"`
	Satisfied   bool              `json:"satisfied"`
	// CanDecide — текущий пользователь может принять решение (он в списке и предпосылки этапа выполнены).
	CanDecide bool `json:"can_decide"`
}

// evalLeScope — состояние списка: согласия считаются только у тех, кто сейчас в списке.
func evalLeScope(stage, legal string, approvers []LeApprover, requireAll bool, decisions []LeDecision, me int64) LeScopeStatus {
	st := LeScopeStatus{StageCode: stage, LegalEntity: legal, RequireAll: requireAll}
	approved := map[int64]LeDecision{}
	for _, d := range decisions {
		if d.StageCode == stage && d.LegalEntity == legal && d.Decision == LeApprove && !d.Revoked {
			approved[d.UserID] = d
		}
	}
	n := 0
	for _, a := range approvers {
		if a.StageCode != stage || a.LegalEntity != legal {
			continue
		}
		d, ok := approved[a.UserID]
		st.Approvers = append(st.Approvers, LeApproverState{UserID: a.UserID, UserName: a.UserName, Approved: ok, Comment: d.Comment, Mine: a.UserID == me})
		if ok {
			n++
		}
	}
	st.NoApprovers = len(st.Approvers) == 0
	switch {
	case st.NoApprovers:
	case requireAll:
		st.Satisfied = n == len(st.Approvers)
	default:
		st.Satisfied = n >= 1
	}
	sort.SliceStable(st.Approvers, func(i, j int) bool { return st.Approvers[i].UserName < st.Approvers[j].UserName })
	return st
}

// LeApprovalStatus — ответ экрана согласования.
type LeApprovalStatus struct {
	PlID int64 `json:"pl_id"`
	// Stages — состояние этапов 1.6, 3, 4 (pending|in_progress|completed|returned).
	Stages map[string]string `json:"stages"`
	// Stage3 — по одному списку на ЮЛ свода; Stage4 — общий список.
	Stage3 []LeScopeStatus `json:"stage3"`
	Stage4 LeScopeStatus   `json:"stage4"`
	// Decisions — журнал решений (включая снятые).
	Decisions []LeDecision `json:"decisions"`
	// CanConfigure — пользователь может менять списки согласующих (администратор процессов).
	CanConfigure bool `json:"can_configure"`
}

// LeApprovalService — решения согласующих, настройки списков и проверки перед движением этапов 3 и 4.
type LeApprovalService struct {
	repo   LeApprovalRepo
	svod   *LeSvodService
	flow   *Service
	cards  *CardService
	stages func(ctx context.Context, plID int64) ([]StageState, error)
}

// NewLeApprovalService — конструктор.
func NewLeApprovalService(repo LeApprovalRepo, svod *LeSvodService, flow *Service, cards *CardService,
	stages func(ctx context.Context, plID int64) ([]StageState, error)) *LeApprovalService {
	return &LeApprovalService{repo: repo, svod: svod, flow: flow, cards: cards, stages: stages}
}

func (s *LeApprovalService) stageStatuses(ctx context.Context, plID int64) map[string]string {
	out := map[string]string{}
	if s.stages == nil {
		return out
	}
	if st, err := s.stages(ctx, plID); err == nil {
		for _, x := range st {
			out[x.Code] = x.Status
		}
	}
	return out
}

func policyOf(policies []LePolicy, stage, legal string) bool {
	for _, p := range policies {
		if p.StageCode == stage && p.LegalEntity == legal {
			return p.RequireAll
		}
	}
	return true // по умолчанию нужны все
}

// legalsOf — ЮЛ свода, по которым нужно согласие (колонка «без ЮЛ» согласующих не имеет и показывается замечанием готовности).
func (s *LeApprovalService) legalsOf(ctx context.Context, plID int64) ([]string, error) {
	sv, err := s.svod.Build(ctx, plID, ExpWindowMonth)
	if err != nil {
		return nil, err
	}
	var out []string
	for _, lg := range sv.Legals {
		if lg != leNoEntity {
			out = append(out, lg)
		}
	}
	return out, nil
}

// Status — состояние согласования для текущего пользователя.
func (s *LeApprovalService) Status(ctx context.Context, plID int64, p Principal) (LeApprovalStatus, error) {
	legals, err := s.legalsOf(ctx, plID)
	if err != nil {
		return LeApprovalStatus{}, err
	}
	approvers, err := s.repo.Approvers(ctx)
	if err != nil {
		return LeApprovalStatus{}, err
	}
	policies, err := s.repo.Policies(ctx)
	if err != nil {
		return LeApprovalStatus{}, err
	}
	decisions, err := s.repo.Decisions(ctx, plID)
	if err != nil {
		return LeApprovalStatus{}, err
	}
	stages := s.stageStatuses(ctx, plID)
	out := LeApprovalStatus{PlID: plID, Stages: stages, Decisions: decisions, CanConfigure: p.PlansAdmin}
	open3 := stages["1.6"] == "completed" && stages["3"] != "completed"
	for _, lg := range legals {
		sc := evalLeScope("3", lg, approvers, policyOf(policies, "3", lg), decisions, p.UserID)
		sc.CanDecide = open3 && leMine(sc)
		out.Stage3 = append(out.Stage3, sc)
	}
	out.Stage4 = evalLeScope("4", "", approvers, policyOf(policies, "4", ""), decisions, p.UserID)
	out.Stage4.CanDecide = stages["3"] == "completed" && stages["4"] != "completed" && leMine(out.Stage4)
	return out, nil
}

func leMine(sc LeScopeStatus) bool {
	for _, a := range sc.Approvers {
		if a.Mine {
			return true
		}
	}
	return false
}

// Decide — решение согласующего. approve: этап завершается сам, когда выполнены все списки этапа. return: период
// возвращается назад, согласия снимаются, причина обязательна.
func (s *LeApprovalService) Decide(ctx context.Context, plID int64, p Principal, stage, legal, decision, comment string) error {
	comment = strings.TrimSpace(comment)
	legal = strings.TrimSpace(legal)
	if stage != "3" && stage != "4" {
		return errors.New("решение принимается на этапах 3 и 4")
	}
	if stage == "4" {
		legal = ""
	} else if legal == "" {
		return errors.New("укажите юридическое лицо")
	}
	if decision != LeApprove && decision != LeReturn {
		return fmt.Errorf("неизвестное решение %q", decision)
	}
	if decision == LeReturn && comment == "" {
		return errors.New("возврат без причины невозможен")
	}
	unlock, err := s.repo.Lock(ctx, plID)
	if err != nil {
		return err
	}
	defer unlock()
	stages := s.stageStatuses(ctx, plID)
	prereq, prereqName := "1.6", "1.6"
	if stage == "4" {
		prereq, prereqName = "3", "3"
	}
	if stages[prereq] != "completed" {
		return fmt.Errorf("этап %s ещё не завершён: решение по этапу %s принимается после него", prereqName, stage)
	}
	if stages[stage] == "completed" {
		return fmt.Errorf("этап %s уже завершён: чтобы изменить решение, период нужно вернуть", stage)
	}
	approvers, err := s.repo.Approvers(ctx)
	if err != nil {
		return err
	}
	allowed := false
	for _, a := range approvers {
		if a.StageCode == stage && a.LegalEntity == legal && a.UserID == p.UserID {
			allowed = true
		}
	}
	if !allowed {
		if stage == "3" {
			return fmt.Errorf("вы не назначены согласующим по ЮЛ «%s»", legal)
		}
		return errors.New("вы не назначены согласующим итогового утверждения")
	}
	year, month, err := s.periodOf(ctx, plID)
	if err != nil {
		return err
	}

	if decision == LeReturn {
		// Сначала двигаем этап: если движение не удалось, ничего не записано и согласия не сняты. Хук возврата для этого вызова
		// отключён — снятие согласий здесь точнее (по одному ЮЛ).
		target := "1.6"
		if stage == "4" {
			target = "3"
		}
		skip := context.WithValue(ctx, leReturnSkipKey{}, true)
		if _, err := s.flow.StageAction(skip, p, plID, year, month, "BY", stage, "return", target, comment); err != nil {
			return err
		}
		if _, err := s.repo.AddDecision(ctx, LeDecision{PlID: plID, StageCode: stage, LegalEntity: legal, UserID: p.UserID, Decision: LeReturn, Comment: comment}); err != nil {
			return err
		}
		reason := "возврат: " + comment
		if stage == "3" {
			_, err := s.repo.RevokeDecisions(ctx, plID, "3", legal, false, reason)
			return err
		}
		// Возврат итогового утверждения на этап 3: согласия этапа 4 и прежние согласия по ЮЛ недействительны.
		if _, err := s.repo.RevokeDecisions(ctx, plID, "4", "", true, reason); err != nil {
			return err
		}
		_, err := s.repo.RevokeDecisions(ctx, plID, "3", "", true, reason)
		return err
	}

	if _, err := s.repo.AddDecision(ctx, LeDecision{PlID: plID, StageCode: stage, LegalEntity: legal, UserID: p.UserID, Decision: LeApprove, Comment: comment}); err != nil {
		return err
	}
	return s.complete(ctx, plID, p, year, month, stage, comment)
}

// leReturnSkipKey — признак «это возврат из Decide»: хук возврата этапа не снимает согласия повторно.
type leReturnSkipKey struct{}

// complete завершает этап, если списки выполнены и он ещё не завершён (повторный вызов безопасен). Вызывать под Lock.
func (s *LeApprovalService) complete(ctx context.Context, plID int64, p Principal, year, month int, stage, comment string) error {
	stages := s.stageStatuses(ctx, plID)
	prereq := "1.6"
	if stage == "4" {
		prereq = "3"
	}
	if stages[prereq] != "completed" || stages[stage] == "completed" {
		return nil
	}
	done, _, err := s.stageSatisfied(ctx, plID, stage)
	if err != nil || !done {
		return err
	}
	_, err = s.flow.StageAction(ctx, p, plID, year, month, "BY", stage, "approve", "", comment)
	return err
}

// tryComplete пробует завершить этапы 3 и 4 периода (после смены списков или правила). Под Lock.
func (s *LeApprovalService) tryComplete(ctx context.Context, plID int64, p Principal) error {
	year, month, err := s.periodOf(ctx, plID)
	if err != nil {
		return err
	}
	for _, stage := range []string{"3", "4"} {
		if err := s.complete(ctx, plID, p, year, month, stage, ""); err != nil {
			return err
		}
	}
	return nil
}

// RecheckOpen после смены списков или правила проверяет открытые периоды: если условия этапа уже выполнены (убрали
// последнего несогласовавшего, правило «все» заменили на «один»), этап завершается, не дожидаясь нового согласия.
func (s *LeApprovalService) RecheckOpen(ctx context.Context, p Principal) {
	ids, err := s.repo.OpenPeriods(ctx)
	if err != nil {
		log.Printf("plans/le-approval: периоды для перепроверки не прочитаны: %v", err)
		return
	}
	for _, id := range ids {
		unlock, err := s.repo.Lock(ctx, id)
		if err != nil {
			log.Printf("plans/le-approval: период %d не заблокирован для перепроверки: %v", id, err)
			continue
		}
		if err := s.tryComplete(ctx, id, p); err != nil {
			log.Printf("plans/le-approval: период %d: этап не завершён после смены списков: %v", id, err)
		}
		unlock()
	}
}

// LockPeriod берёт блокировку периода (решения, корректировки и возвраты по одному периоду идут по одному).
func (s *LeApprovalService) LockPeriod(ctx context.Context, plID int64) (func(), error) {
	return s.repo.Lock(ctx, plID)
}

// OnStageReturn вызывается после возврата этапа вручную (таймлайн): любой возврат делает недействительным итоговое утверждение
// и согласия ЮЛ — согласовывали другие цифры. Возврат из Decide отключает хук (там согласия снимаются точнее).
func (s *LeApprovalService) OnStageReturn(ctx context.Context, plID int64, code, target string) error {
	if v, _ := ctx.Value(leReturnSkipKey{}).(bool); v || target == "4" {
		return nil
	}
	unlock, err := s.repo.Lock(ctx, plID)
	if err != nil {
		return err
	}
	defer unlock()
	reason := "возврат этапа " + code + " на этап " + target
	if _, err := s.repo.RevokeDecisions(ctx, plID, "4", "", true, reason); err != nil {
		return err
	}
	_, err = s.repo.RevokeDecisions(ctx, plID, "3", "", true, reason)
	return err
}

func (s *LeApprovalService) periodOf(ctx context.Context, plID int64) (int, int, error) {
	cards, err := s.cards.Cards(ctx, plID)
	if err != nil {
		return 0, 0, err
	}
	if len(cards) == 0 {
		return 0, 0, errors.New("в периоде нет карточек форм")
	}
	return cards[0].Year, cards[0].Month, nil
}

// stageSatisfied — выполнены ли все списки этапа; pending — человекочитаемый перечень невыполненных.
func (s *LeApprovalService) stageSatisfied(ctx context.Context, plID int64, stage string) (bool, []string, error) {
	approvers, err := s.repo.Approvers(ctx)
	if err != nil {
		return false, nil, err
	}
	policies, err := s.repo.Policies(ctx)
	if err != nil {
		return false, nil, err
	}
	decisions, err := s.repo.Decisions(ctx, plID)
	if err != nil {
		return false, nil, err
	}
	var pending []string
	if stage == "4" {
		sc := evalLeScope("4", "", approvers, policyOf(policies, "4", ""), decisions, 0)
		if !sc.Satisfied {
			pending = append(pending, scopeNote("итоговое утверждение", sc))
		}
		return sc.Satisfied, pending, nil
	}
	legals, err := s.legalsOf(ctx, plID)
	if err != nil {
		return false, nil, err
	}
	if len(legals) == 0 {
		return false, []string{"в своде нет юридических лиц"}, nil
	}
	for _, lg := range legals {
		sc := evalLeScope("3", lg, approvers, policyOf(policies, "3", lg), decisions, 0)
		if !sc.Satisfied {
			pending = append(pending, scopeNote(lg, sc))
		}
	}
	return len(pending) == 0, pending, nil
}

func scopeNote(name string, sc LeScopeStatus) string {
	if sc.NoApprovers {
		return name + " (не назначены согласующие)"
	}
	if sc.RequireAll {
		n := 0
		for _, a := range sc.Approvers {
			if a.Approved {
				n++
			}
		}
		return fmt.Sprintf("%s (согласовано %d из %d)", name, n, len(sc.Approvers))
	}
	return name + " (нужно согласие хотя бы одного)"
}

// Gate — проверка перед ручным завершением этапов 3 и 4 (общая точка Service.WithStageGate): этап завершается только
// решениями согласующих. Возврат и «взять в работу» не проверяются.
func (s *LeApprovalService) Gate(ctx context.Context, plID int64, code, action string) error {
	if (code != "3" && code != "4") || (action != "submit" && action != "approve") {
		return nil
	}
	ok, pending, err := s.stageSatisfied(ctx, plID, code)
	if err != nil {
		return err
	}
	if ok {
		return nil
	}
	return fmt.Errorf("этап %s завершается решениями согласующих на экране «Бюджет по ЮЛ». Ещё не согласовано: %s", code, leListTitles(pending, 6))
}

// OnAdjust вызывается после корректировки итога ЮЛ: согласия по этому ЮЛ и итоговое утверждение снимаются.
func (s *LeApprovalService) OnAdjust(ctx context.Context, plID int64, legal string) error {
	reason := "корректировка итога ЮЛ «" + legal + "»"
	if _, err := s.repo.RevokeDecisions(ctx, plID, "3", legal, false, reason); err != nil {
		return fmt.Errorf("не удалось снять согласия ЮЛ: %w", err)
	}
	if _, err := s.repo.RevokeDecisions(ctx, plID, "4", "", true, reason); err != nil {
		return fmt.Errorf("не удалось снять итоговое утверждение: %w", err)
	}
	return nil
}

// IsApprover — пользователь согласующий этапа (по ЮЛ для этапа 3).
func (s *LeApprovalService) IsApprover(ctx context.Context, stage, legal string, userID int64) bool {
	approvers, err := s.repo.Approvers(ctx)
	if err != nil {
		return false
	}
	for _, a := range approvers {
		if a.StageCode == stage && a.UserID == userID && (stage == "4" || a.LegalEntity == legal) {
			return true
		}
	}
	return false
}

// Config — списки согласующих и политики (для настройки).
type LeApprovalConfig struct {
	Approvers []LeApprover `json:"approvers"`
	Policies  []LePolicy   `json:"policies"`
}

// Config возвращает все списки и политики.
func (s *LeApprovalService) Config(ctx context.Context) (LeApprovalConfig, error) {
	a, err := s.repo.Approvers(ctx)
	if err != nil {
		return LeApprovalConfig{}, err
	}
	p, err := s.repo.Policies(ctx)
	return LeApprovalConfig{Approvers: a, Policies: p}, err
}

// SetApprovers задаёт список согласующих области (этап 3 — ЮЛ, этап 4 — весь период) и политику «все / один».
func (s *LeApprovalService) SetApprovers(ctx context.Context, p Principal, stage, legal string, userIDs []int64, requireAll bool) error {
	if !p.PlansAdmin {
		return errors.New("списки согласующих настраивает администратор процессов")
	}
	if stage != "3" && stage != "4" {
		return errors.New("списки настраиваются для этапов 3 и 4")
	}
	legal = strings.TrimSpace(legal)
	if stage == "4" {
		legal = ""
	} else if legal == "" {
		return errors.New("укажите юридическое лицо")
	}
	seen := map[int64]bool{}
	var ids []int64
	for _, id := range userIDs {
		if id > 0 && !seen[id] {
			seen[id] = true
			ids = append(ids, id)
		}
	}
	if err := s.repo.SetApprovers(ctx, stage, legal, ids, requireAll, p.UserID); err != nil {
		return err
	}
	s.RecheckOpen(ctx, p)
	return nil
}
