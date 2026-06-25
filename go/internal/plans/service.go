package plans

import "context"

// Service — бизнес-логика формы TPL-MP (VS3): сборка матрицы (факт + тактика)
// и сохранение тактики с полным снимком. См. docs/reports/plans/SPEC.md §9, §13.
type Service struct {
	store MetricStore
	fact  MpFactSource
}

// NewService — конструктор.
func NewService(store MetricStore, fact MpFactSource) *Service {
	return &Service{store: store, fact: fact}
}

// EnsureInstance — id экземпляра PL на период (создаёт при отсутствии).
func (s *Service) EnsureInstance(ctx context.Context, year, month int) (int64, error) {
	return s.store.EnsureInstance(ctx, year, month)
}

// MpForm собирает форму: read-only факт (OLAP/FinDWH) + сохранённая тактика.
func (s *Service) MpForm(ctx context.Context, year, month int, segment, currency string) (MpForm, error) {
	if currency == "" {
		currency = "RUB"
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
	return buildMpForm(segment, year, month, currency, factToMetrics(factRows), tactic), nil
}

// SaveMpForm валидирует и сохраняет editable-ячейки тактики + полный снимок формы.
// rawPayload — исходное тело запроса (для form_submission.json_payload).
func (s *Service) SaveMpForm(ctx context.Context, req SaveMpFormRequest, rawPayload []byte) (int64, error) {
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
	if err := s.store.SaveSubmission(ctx, plID, rawPayload); err != nil {
		return 0, err
	}
	return plID, nil
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
