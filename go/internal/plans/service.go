package plans

import "context"

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
	factRows, err := s.fact.MpFact(ctx, year, month, segment)
	if err != nil {
		return MpForm{}, err
	}
	tactic, err := s.store.Metrics(ctx, plID, segment, year, month)
	if err != nil {
		return MpForm{}, err
	}
	form := buildMpForm(segment, year, month, currency, factToMetrics(factRows), tactic)
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
