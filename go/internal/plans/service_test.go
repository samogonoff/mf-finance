package plans

import (
	"context"
	"testing"
)

// memStore — in-memory MetricStore для round-trip тестов без БД.
type memStore struct {
	nextID        int64
	nextCommentID int64
	instances     map[[2]int]int64
	metrics       map[int64][]storedMetric
	subs          map[int64][][]byte
	adjustments   map[int64][]AdjustmentRow
	comments      map[int64][]Comment
	overrides     map[int64]map[string]string
	stages        map[int64][]StageState
	approvals     map[int64][]string
}

type storedMetric struct {
	segment string
	row     MetricRow
}

func newMemStore() *memStore {
	return &memStore{
		instances:   map[[2]int]int64{},
		metrics:     map[int64][]storedMetric{},
		subs:        map[int64][][]byte{},
		adjustments: map[int64][]AdjustmentRow{},
		comments:    map[int64][]Comment{},
		overrides:   map[int64]map[string]string{},
		stages:      map[int64][]StageState{},
		approvals:   map[int64][]string{},
	}
}

func (m *memStore) EnsureInstance(_ context.Context, year, month int) (int64, error) {
	k := [2]int{year, month}
	if id, ok := m.instances[k]; ok {
		return id, nil
	}
	m.nextID++
	m.instances[k] = m.nextID
	return m.nextID, nil
}

func (m *memStore) ListInstances(_ context.Context) ([]InstanceSummary, error) {
	out := make([]InstanceSummary, 0, len(m.instances))
	for k, id := range m.instances {
		out = append(out, InstanceSummary{
			ID: id, PeriodYear: k[0], PeriodMonth: k[1],
			Status: "in_progress", MetricCount: len(m.metrics[id]),
		})
	}
	return out, nil
}

func (m *memStore) Metrics(_ context.Context, plID int64, segment string, year, month int) ([]MetricRow, error) {
	var out []MetricRow
	for _, s := range m.metrics[plID] {
		if s.segment == segment && s.row.Year == year && s.row.Month == month && s.row.Scenario == ScenarioTactic {
			out = append(out, s.row)
		}
	}
	return out, nil
}

func (m *memStore) UpsertMetrics(_ context.Context, plID int64, segment string, rows []MetricRow) error {
	key := func(s string, r MetricRow) string {
		return s + "|" + r.Scenario + "|" + r.Currency
	}
	for _, r := range rows {
		replaced := false
		for i, ex := range m.metrics[plID] {
			if ex.row.LineCode == r.LineCode && ex.row.BlockType == r.BlockType &&
				ex.row.ProfitCenter == r.ProfitCenter && ex.row.Year == r.Year &&
				ex.row.Month == r.Month && key(ex.segment, ex.row) == key(segment, r) {
				m.metrics[plID][i] = storedMetric{segment, r}
				replaced = true
				break
			}
		}
		if !replaced {
			m.metrics[plID] = append(m.metrics[plID], storedMetric{segment, r})
		}
	}
	return nil
}

func (m *memStore) SaveSubmission(_ context.Context, plID int64, payload []byte) error {
	m.subs[plID] = append(m.subs[plID], payload)
	return nil
}

func (m *memStore) SaveAdjustments(_ context.Context, plID int64, adj []AdjustmentRow) error {
	m.adjustments[plID] = append(m.adjustments[plID], adj...)
	return nil
}

func (m *memStore) AddComment(_ context.Context, plID int64, c CommentInput) (int64, error) {
	m.nextCommentID++
	m.comments[plID] = append(m.comments[plID], Comment{
		ID: m.nextCommentID, MetricRef: c.MetricRef, Body: c.Body, Status: "open",
	})
	return m.nextCommentID, nil
}

func (m *memStore) Comments(_ context.Context, plID int64) ([]Comment, error) {
	return m.comments[plID], nil
}

func (m *memStore) FormulaOverrides(_ context.Context, plID int64) (map[string]string, error) {
	if m.overrides[plID] == nil {
		return map[string]string{}, nil
	}
	return m.overrides[plID], nil
}

func (m *memStore) UpsertOverride(_ context.Context, plID int64, ov FormulaOverride) error {
	if m.overrides[plID] == nil {
		m.overrides[plID] = map[string]string{}
	}
	m.overrides[plID][ov.Code] = ov.FormulaExpr
	return nil
}

func (m *memStore) StagesInit(_ context.Context, plID int64, stages []StageState) error {
	if m.stages[plID] == nil {
		m.stages[plID] = append([]StageState(nil), stages...)
	}
	return nil
}

func (m *memStore) Stages(_ context.Context, plID int64) ([]StageState, error) {
	return m.stages[plID], nil
}

func (m *memStore) StagesSave(_ context.Context, plID int64, stages []StageState) error {
	m.stages[plID] = append([]StageState(nil), stages...)
	return nil
}

func (m *memStore) RecordApproval(_ context.Context, plID int64, code string, userID int64, decision, le string) error {
	m.approvals[plID] = append(m.approvals[plID], code+":"+decision)
	return nil
}

// memScopeStore — in-memory ScopeStore для ABAC-тестов.
type memScopeStore struct{ codes map[int64][]int }

func newMemScope() *memScopeStore { return &memScopeStore{codes: map[int64][]int{}} }

func (m *memScopeStore) UserCodeCFOs(_ context.Context, userID int64) ([]int, error) {
	return m.codes[userID], nil
}

func (m *memScopeStore) UpsertScope(_ context.Context, sc UserScope) error {
	m.codes[sc.UserID] = sc.CodeCFO
	return nil
}

// adminP — Principal с обходом ABAC (для не-ABAC тестов).
var adminP = Principal{PlansAdmin: true}

func TestService_SaveAndLoad_RoundTrip(t *testing.T) {
	store := newMemStore()
	svc := NewService(store, NewMockFactSource(), newMemScope())
	ctx := context.Background()

	req := SaveMpFormRequest{
		Segment: "large",
		Period:  PeriodRef{Year: 2026, Month: 5},
		Header:  SaveHeader{Currency: "RUB", Scenario: ScenarioTactic},
		Rows: []SaveRow{
			{CodeCFO: 335, CodePL: 1046, BlockType: "sales_manager_price", Amount: 999000, IsManual: true, Comment: "round-trip"},
		},
	}
	raw := []byte(`{"template_code":"TPL-MP"}`)
	plID, err := svc.SaveMpForm(ctx, adminP, req, raw)
	if err != nil {
		t.Fatalf("SaveMpForm: %v", err)
	}
	if plID == 0 {
		t.Fatal("plID == 0")
	}
	if len(store.subs[plID]) != 1 {
		t.Errorf("снимок form_submission не сохранён: %d", len(store.subs[plID]))
	}

	form, err := svc.MpForm(ctx, adminP, 2026, 5, "large", "RUB")
	if err != nil {
		t.Fatalf("MpForm: %v", err)
	}
	var checked bool
	for _, b := range form.Blocks {
		if b.CodePL != 1046 {
			continue
		}
		if !b.Editable {
			t.Errorf("блок 1046 должен быть editable")
		}
		for _, r := range b.Rows {
			if r.CodeCFO != 335 {
				continue
			}
			checked = true
			if r.Tactic == nil || *r.Tactic != 999000 {
				t.Errorf("тактика WB не сохранилась round-trip: %v", r.Tactic)
			}
			if r.Fact < 357034569 || r.Fact > 357034571 {
				t.Errorf("факт WB должен подтянуться read-only: %.2f", r.Fact)
			}
		}
	}
	if !checked {
		t.Fatal("строка WB(335) в блоке 1046 не найдена")
	}
}

func TestMpForm_ABAC_SmallUserCannotSeeLarge(t *testing.T) {
	scope := newMemScope()
	scope.codes[42] = []int{338, 339} // small-площадки (Kaspi, Uzmarket)
	svc := NewService(newMemStore(), NewMockFactSource(), scope)
	ctx := context.Background()

	form, err := svc.MpForm(ctx, Principal{UserID: 42}, 2026, 5, "large", "RUB")
	if err != nil {
		t.Fatalf("MpForm: %v", err)
	}
	if len(form.Platforms) != 0 {
		t.Errorf("small-юзер не должен видеть large-площадки, got %d", len(form.Platforms))
	}
	for _, b := range form.Blocks {
		if len(b.Rows) != 0 {
			t.Errorf("блок %d не должен содержать строк для чужого сегмента", b.CodePL)
		}
	}
}

func TestMpForm_ABAC_AllowedPlatformVisible(t *testing.T) {
	scope := newMemScope()
	scope.codes[7] = []int{335} // только Wildberries
	svc := NewService(newMemStore(), NewMockFactSource(), scope)

	form, err := svc.MpForm(context.Background(), Principal{UserID: 7}, 2026, 5, "large", "RUB")
	if err != nil {
		t.Fatalf("MpForm: %v", err)
	}
	if len(form.Platforms) != 1 || form.Platforms[0].CodeCFO != 335 {
		t.Errorf("должна остаться только площадка 335, got %+v", form.Platforms)
	}
}

func TestSaveMpForm_ABAC_RejectsForeignPlatform(t *testing.T) {
	scope := newMemScope()
	scope.codes[7] = []int{335} // разрешён только WB
	svc := NewService(newMemStore(), NewMockFactSource(), scope)

	req := SaveMpFormRequest{
		Segment: "large",
		Period:  PeriodRef{Year: 2026, Month: 5},
		Rows:    []SaveRow{{CodeCFO: 337, CodePL: 1046, BlockType: "sales_manager_price", Amount: 1}}, // Ozon — чужой
	}
	if _, err := svc.SaveMpForm(context.Background(), Principal{UserID: 7}, req, []byte(`{}`)); err == nil {
		t.Error("ожидалась ошибка: запись в чужую площадку (337) вне среза")
	}
}

func TestSaveMpForm_ABAC_AllowsOwnPlatform(t *testing.T) {
	scope := newMemScope()
	scope.codes[7] = []int{335}
	svc := NewService(newMemStore(), NewMockFactSource(), scope)

	req := SaveMpFormRequest{
		Segment: "large",
		Period:  PeriodRef{Year: 2026, Month: 5},
		Rows:    []SaveRow{{CodeCFO: 335, CodePL: 1046, BlockType: "sales_manager_price", Amount: 100}},
	}
	if _, err := svc.SaveMpForm(context.Background(), Principal{UserID: 7}, req, []byte(`{}`)); err != nil {
		t.Errorf("запись в свою площадку (335) должна проходить: %v", err)
	}
}

func TestSaveMpForm_ManualRequiresReason(t *testing.T) {
	svc := NewService(newMemStore(), NewMockFactSource(), newMemScope())
	req := SaveMpFormRequest{
		Segment: "large",
		Period:  PeriodRef{Year: 2026, Month: 5},
		Rows:    []SaveRow{{CodeCFO: 335, CodePL: 1046, BlockType: "sales_manager_price", Amount: 1, IsManual: true}}, // без Comment
	}
	if _, err := svc.SaveMpForm(context.Background(), adminP, req, []byte(`{}`)); err == nil {
		t.Error("ожидалась ошибка: причина корректировки обязательна (ADJ-02)")
	}
}

func TestSaveMpForm_ManualWithReason_PersistsAdjustment(t *testing.T) {
	store := newMemStore()
	svc := NewService(store, NewMockFactSource(), newMemScope())
	req := SaveMpFormRequest{
		Segment: "large",
		Period:  PeriodRef{Year: 2026, Month: 5},
		Rows: []SaveRow{{
			CodeCFO: 335, CodePL: 1046, BlockType: "sales_manager_price",
			Amount: 123, IsManual: true, Comment: "акция",
		}},
	}
	plID, err := svc.SaveMpForm(context.Background(), adminP, req, []byte(`{}`))
	if err != nil {
		t.Fatalf("SaveMpForm: %v", err)
	}
	adj := store.adjustments[plID]
	if len(adj) != 1 || adj[0].Reason != "акция" || adj[0].AdjustedValue != 123 {
		t.Errorf("корректировка не записана: %+v", adj)
	}
	// ADJ-04: ячейка помечена Manual в форме.
	form, _ := svc.MpForm(context.Background(), adminP, 2026, 5, "large", "RUB")
	for _, b := range form.Blocks {
		for _, r := range b.Rows {
			if b.CodePL == 1046 && r.CodeCFO == 335 && !r.Manual {
				t.Error("ячейка корректировки должна быть помечена Manual (ADJ-04)")
			}
		}
	}
}

func TestStages_LazyInitAndAction(t *testing.T) {
	store := newMemStore()
	svc := NewService(store, NewMockFactSource(), newMemScope())
	ctx := context.Background()
	plID, _ := store.EnsureInstance(ctx, 2026, 6)

	// Первый запрос инициализирует этапы.
	stages, err := svc.Stages(ctx, plID, 2026, 6, "RU")
	if err != nil {
		t.Fatalf("Stages: %v", err)
	}
	if len(stages) != len(stageDefs()) {
		t.Fatalf("ожидалось %d этапов", len(stageDefs()))
	}
	// Действие submit на 1.1 сохраняется.
	next, err := svc.StageAction(ctx, adminP, plID, 2026, 6, "RU", "1.1", "submit", "")
	if err != nil {
		t.Fatalf("StageAction: %v", err)
	}
	if findStage(next, "1.1").Status != "completed" {
		t.Error("1.1 должен стать completed")
	}
	// Перезагрузка возвращает сохранённый статус.
	reload, _ := svc.Stages(ctx, plID, 2026, 6, "RU")
	if findStage(reload, "1.1").Status != "completed" {
		t.Error("статус 1.1 должен сохраниться")
	}
}

func TestStageAction_ApprovalRecorded(t *testing.T) {
	store := newMemStore()
	svc := NewService(store, NewMockFactSource(), newMemScope())
	ctx := context.Background()
	plID, _ := store.EnsureInstance(ctx, 2026, 6)
	_, _ = svc.Stages(ctx, plID, 2026, 6, "RU")
	_, _ = svc.StageAction(ctx, adminP, plID, 2026, 6, "RU", "1.1", "submit", "")
	if _, err := svc.StageAction(ctx, adminP, plID, 2026, 6, "RU", "1.2", "approve", ""); err != nil {
		t.Fatalf("approve 1.2: %v", err)
	}
	if len(store.approvals[plID]) == 0 {
		t.Error("согласование должно попасть в лист (pl_approval)")
	}
}

func TestInstances_ListAfterSave(t *testing.T) {
	store := newMemStore()
	svc := NewService(store, NewMockFactSource(), newMemScope())
	ctx := context.Background()

	req := SaveMpFormRequest{
		Segment: "large", Period: PeriodRef{Year: 2026, Month: 6},
		Rows: []SaveRow{{CodeCFO: 335, CodePL: 1046, BlockType: "sales_manager_price", Amount: 10, IsManual: true, Comment: "c"}},
	}
	if _, err := svc.SaveMpForm(ctx, adminP, req, []byte(`{}`)); err != nil {
		t.Fatalf("save: %v", err)
	}
	list, err := svc.Instances(ctx)
	if err != nil {
		t.Fatalf("Instances: %v", err)
	}
	var found bool
	for _, s := range list {
		if s.PeriodYear == 2026 && s.PeriodMonth == 6 {
			found = true
			if s.MetricCount < 1 {
				t.Errorf("ожидался metric_count >= 1, got %d", s.MetricCount)
			}
		}
	}
	if !found {
		t.Error("экземпляр 2026-06 не появился в списке")
	}
}

func TestComments_AddAndList(t *testing.T) {
	store := newMemStore()
	svc := NewService(store, NewMockFactSource(), newMemScope())
	ctx := context.Background()
	plID, _ := store.EnsureInstance(ctx, 2026, 5)

	if _, err := svc.AddComment(ctx, plID, CommentInput{MetricRef: "335:1046", Body: "проверить"}); err != nil {
		t.Fatalf("AddComment: %v", err)
	}
	list, err := svc.Comments(ctx, plID)
	if err != nil {
		t.Fatalf("Comments: %v", err)
	}
	if len(list) != 1 || list[0].Body != "проверить" || list[0].Status != "open" {
		t.Errorf("комментарий не сохранился: %+v", list)
	}
}

func TestMetricsFromRequest_RejectsForeignCFO(t *testing.T) {
	req := SaveMpFormRequest{
		Segment: "large",
		Period:  PeriodRef{Year: 2026, Month: 5},
		Rows:    []SaveRow{{CodeCFO: 338, CodePL: 1046, BlockType: "sales_manager_price", Amount: 1}}, // 338 — small (Kaspi)
	}
	if _, err := metricsFromRequest(req); err == nil {
		t.Error("ожидалась ошибка: code_cfo вне сегмента")
	}
}

func TestMetricsFromRequest_RejectsNonEditableBlock(t *testing.T) {
	req := SaveMpFormRequest{
		Segment: "large",
		Period:  PeriodRef{Year: 2026, Month: 5},
		Rows:    []SaveRow{{CodeCFO: 335, CodePL: 1045, BlockType: "sales_manager_price_net", Amount: 1}},
	}
	if _, err := metricsFromRequest(req); err == nil {
		t.Error("ожидалась ошибка: блок не редактируется (расчётный)")
	}
}

func TestMetricsFromRequest_ForcesTacticScenario(t *testing.T) {
	req := SaveMpFormRequest{
		Segment: "large",
		Period:  PeriodRef{Year: 2026, Month: 5},
		Header:  SaveHeader{Scenario: "Факт"}, // попытка подменить сценарий
		Rows:    []SaveRow{{CodeCFO: 335, CodePL: 1046, BlockType: "sales_manager_price", Amount: 5}},
	}
	rows, err := metricsFromRequest(req)
	if err != nil {
		t.Fatalf("unexpected: %v", err)
	}
	if rows[0].Scenario != ScenarioTactic {
		t.Errorf("сценарий должен быть зафиксирован тактикой, got %q", rows[0].Scenario)
	}
}
