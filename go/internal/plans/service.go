package plans

import (
	"context"
	"errors"
)

// Service — бизнес-логика формы TPL-MP (VS3): сборка матрицы (факт + тактика)
// и сохранение тактики с полным снимком. См. docs/reports/plans/SPEC.md §9, §13.
type Service struct {
	store MetricStore
	fact  MpFactSource
	scope ScopeStore
}

// NewService — конструктор.
func NewService(store MetricStore, fact MpFactSource, scope ScopeStore) *Service {
	return &Service{store: store, fact: fact, scope: scope}
}

// allowedFor — ABAC-набор разрешённых code_cfo (nil для админа — без фильтра).
func (s *Service) allowedFor(ctx context.Context, p Principal) (map[int]bool, error) {
	if p.PlansAdmin {
		return nil, nil
	}
	codes, err := s.scope.UserCodeCFOs(ctx, p.UserID)
	if err != nil {
		return nil, err
	}
	return allowedSet(codes), nil
}

// AssignScope — назначение ABAC-среза пользователю (админ процессов).
func (s *Service) AssignScope(ctx context.Context, sc UserScope) error {
	return s.scope.UpsertScope(ctx, sc)
}

// EnsureInstance — id экземпляра PL на период (создаёт при отсутствии).
func (s *Service) EnsureInstance(ctx context.Context, year, month int) (int64, error) {
	return s.store.EnsureInstance(ctx, year, month)
}

// Instances — список экземпляров PL (для списка/дашборда).
func (s *Service) Instances(ctx context.Context) ([]InstanceSummary, error) {
	return s.store.ListInstances(ctx)
}

// Stages — этапы экземпляра PL; при отсутствии инициализирует по маршруту схемы
// со сроками по календарю страны.
func (s *Service) Stages(ctx context.Context, plID int64, year, month int, country string) ([]StageState, error) {
	stages, err := s.store.Stages(ctx, plID)
	if err != nil {
		return nil, err
	}
	if len(stages) == 0 {
		stages = initStages(year, month, country, CalendarSeed())
		if err := s.store.StagesInit(ctx, plID, stages); err != nil {
			return nil, err
		}
	}
	return stages, nil
}

// StageAction — действие WF-03 (start/submit/approve/return) с проверкой
// зависимостей (WF-DEP); согласование пишет лист (pl_approval).
func (s *Service) StageAction(ctx context.Context, p Principal, plID int64, year, month int, country, code, action, target string) ([]StageState, error) {
	stages, err := s.Stages(ctx, plID, year, month, country)
	if err != nil {
		return nil, err
	}
	next, err := applyStageAction(stages, code, action, target)
	if err != nil {
		return nil, err
	}
	if err := s.store.StagesSave(ctx, plID, next); err != nil {
		return nil, err
	}
	if action == "approve" || action == "return" {
		_ = s.store.RecordApproval(ctx, plID, code, p.UserID, action, "")
	}
	return next, nil
}

// MpForm собирает форму: read-only факт (OLAP/FinDWH) + сохранённая тактика,
// отфильтрованную по ABAC-срезу пользователя.
func (s *Service) MpForm(ctx context.Context, p Principal, year, month int, segment, currency string) (MpForm, error) {
	if currency == "" {
		currency = "RUB"
	}
	allowed, err := s.allowedFor(ctx, p)
	if err != nil {
		return MpForm{}, err
	}
	plID, err := s.store.EnsureInstance(ctx, year, month)
	if err != nil {
		return MpForm{}, err
	}
	// Факт — read-only обогащение из OLAP/FinDWH. Его недоступность (вьюха не
	// заведена, VPN недоступен и т.п.) НЕ должна блокировать ввод тактики:
	// деградируем до пустого факта. Ошибку видно на выделенном /mp/fact.
	factRows, _ := s.fact.MpFact(ctx, year, month, segment)
	tactic, err := s.store.Metrics(ctx, plID, segment, year, month)
	if err != nil {
		return MpForm{}, err
	}
	// Пересчёт в валюту шапки (факт mock — RUB; тактика — в своей валюте).
	factM := convertMetrics(factToMetrics(factRows), currency)
	tac := convertMetrics(tactic, currency)
	form := buildMpForm(segment, year, month, currency, factM, tac)
	return applyScope(form, allowed, p.PlansAdmin), nil
}

// SaveMpForm валидирует ABAC-срез + редактируемость, сохраняет тактику и снимок.
// rawPayload — исходное тело запроса (для form_submission.json_payload).
func (s *Service) SaveMpForm(ctx context.Context, p Principal, req SaveMpFormRequest, rawPayload []byte) (int64, error) {
	allowed, err := s.allowedFor(ctx, p)
	if err != nil {
		return 0, err
	}
	if err := checkScopeRows(req, allowed, p.PlansAdmin); err != nil {
		return 0, err
	}
	metrics, err := metricsFromRequest(req)
	if err != nil {
		return 0, err
	}
	plID, err := s.store.EnsureInstance(ctx, req.Period.Year, req.Period.Month)
	if err != nil {
		return 0, err
	}
	if err := s.store.UpsertMetrics(ctx, plID, req.Segment, metrics); err != nil {
		return 0, err
	}
	if err := s.store.SaveAdjustments(ctx, plID, adjustmentsFromRequest(req)); err != nil {
		return 0, err
	}
	if err := s.store.SaveSubmission(ctx, plID, rawPayload); err != nil {
		return 0, err
	}
	return plID, nil
}

// vatDefault — провизорная ставка НДС каскада (Q4b: уточнить по странам/разрезу).
const vatDefault = 0.20

// ComputeMp — превью каскада CALC (D11): по каждой площадке считает производные
// показатели из тактики (или факта) через формулы calc_rule. Override per-срез —
// следующий срез; здесь overrides пуст (только дефолтные формулы).
func (s *Service) ComputeMp(ctx context.Context, p Principal, year, month int, segment, currency string) ([]ComputedRow, error) {
	plID, err := s.store.EnsureInstance(ctx, year, month)
	if err != nil {
		return nil, err
	}
	overrides, err := s.store.FormulaOverrides(ctx, plID)
	if err != nil {
		return nil, err
	}
	form, err := s.MpForm(ctx, p, year, month, segment, currency)
	if err != nil {
		return nil, err
	}
	// Собрать sales (1046) и cost (8006) по площадке: тактика, иначе факт.
	type sc struct{ sales, cost float64 }
	byCFO := map[int]*sc{}
	name := map[int]string{}
	for _, b := range form.Blocks {
		for _, r := range b.Rows {
			if _, ok := byCFO[r.CodeCFO]; !ok {
				byCFO[r.CodeCFO] = &sc{}
				name[r.CodeCFO] = r.NameCFO
			}
			val := r.Fact
			if r.Tactic != nil {
				val = *r.Tactic
			}
			switch b.CodePL {
			case 1046:
				byCFO[r.CodeCFO].sales = val
			case 8006:
				byCFO[r.CodeCFO].cost = val
			}
		}
	}
	formulas := resolveFormulas(CalcRuleSeed(), overrides)
	out := make([]ComputedRow, 0, len(byCFO))
	for _, p := range form.Platforms {
		v := byCFO[p.CodeCFO]
		if v == nil {
			continue
		}
		vars := map[string]float64{"sales": v.sales, "cost": v.cost, "vat": vatDefault}
		out = append(out, ComputedRow{
			CodeCFO: p.CodeCFO,
			NameCFO: name[p.CodeCFO],
			Values:  computeCascade(vars, formulas),
		})
	}
	return out, nil
}

// SaveFormulaOverride — переопределение формулы каскада per-срез (D11). Требует
// причину (как ADJ-02) и компилируемое выражение (проверяется пробным Eval).
func (s *Service) SaveFormulaOverride(ctx context.Context, year, month int, ov FormulaOverride) (int64, error) {
	if ov.Code == "" {
		return 0, errors.New("code обязателен")
	}
	if ov.Reason == "" {
		return 0, errors.New("причина обязательна (D11/ADJ-02)")
	}
	// Проверка компиляции формулы на пробных переменных каскада.
	probe := map[string]float64{"sales": 1, "cost": 1, "vat": vatDefault}
	if _, err := Eval(ov.FormulaExpr, probe); err != nil {
		return 0, errors.New("формула не компилируется: " + err.Error())
	}
	plID, err := s.store.EnsureInstance(ctx, year, month)
	if err != nil {
		return 0, err
	}
	return plID, s.store.UpsertOverride(ctx, plID, ov)
}

// AddComment — комментарий к экземпляру PL по id (COM-01).
func (s *Service) AddComment(ctx context.Context, plID int64, c CommentInput) (int64, error) {
	return s.store.AddComment(ctx, plID, c)
}

// Comments — комментарии экземпляра PL по id.
func (s *Service) Comments(ctx context.Context, plID int64) ([]Comment, error) {
	return s.store.Comments(ctx, plID)
}

// factToMetrics приводит read-only факт к MetricRow для сборки формы.
func factToMetrics(fact []FactRow) []MetricRow {
	out := make([]MetricRow, 0, len(fact))
	for _, f := range fact {
		out = append(out, MetricRow{
			LineCode:     f.CodePL,
			ProfitCenter: f.CodeCFO,
			Year:         f.Year,
			Month:        f.Month,
			Currency:     f.Currency,
			Scenario:     f.Scenario,
			Amount:       f.Amount,
		})
	}
	return out
}
