package plans

import (
	"context"
	"errors"
	"fmt"
	"strings"
)

// Сервис реестра условий площадки и расходной части МП: чтение/запись условий,
// копирование из предыдущего периода, diff для согласующего, пересчёт формы в
// inverse-режиме, общие затраты и валидации (ТЗ МП §6.1, §7, §10).

// MpConditionsService — прикладная логика условий площадки.
type MpConditionsService struct {
	store   MpConditionsStore
	metrics MetricStore
	cards   CardStore
	rates   *RateBook
	fact    MpFactSource
	scope   ScopeStore
	audit   AuditSink
}

// NewMpConditionsService — конструктор.
func NewMpConditionsService(store MpConditionsStore, metrics MetricStore, cards CardStore,
	rates *RateBook, fact MpFactSource, scope ScopeStore) *MpConditionsService {
	return &MpConditionsService{store: store, metrics: metrics, cards: cards,
		rates: rates, fact: fact, scope: scope}
}

// WithAudit подключает журнал модуля.
func (s *MpConditionsService) WithAudit(a AuditSink) *MpConditionsService { s.audit = a; return s }

// MpConditionsView — реестр условий с подсказками и diff к прошлому периоду.
type MpConditionsView struct {
	Period     PeriodRef          `json:"period"`
	Conditions []MpConditions     `json:"conditions"`
	Diff       []MpConditionsDiff `json:"diff"`
	// Hints — фактические доли прошлого периода: подсказка при заполнении.
	// ТЗ §3.1: значения долей за прошлые периоды остаются производными и в реестр
	// НЕ переносятся автоматически — только показываются.
	Hints    map[int]map[string]float64 `json:"hints"`
	Editable bool                       `json:"editable"`
	Card     *Card                      `json:"card,omitempty"`
}

// Conditions — реестр условий периода с diff и подсказками.
func (s *MpConditionsService) Conditions(ctx context.Context, cardID int64) (MpConditionsView, error) {
	card, err := s.cards.Card(ctx, cardID)
	if err != nil {
		return MpConditionsView{}, err
	}
	cur, err := s.store.Conditions(ctx, card.PlID, card.Year, card.Month)
	if err != nil {
		return MpConditionsView{}, err
	}
	segments := mpSegmentsOf(card)
	filtered := make([]MpConditions, 0, len(cur))
	for _, c := range cur {
		if c.Segment == "" || segments[c.Segment] {
			filtered = append(filtered, c)
		}
	}
	view := MpConditionsView{
		Period:     PeriodRef{Year: card.Year, Month: card.Month},
		Conditions: filtered,
		Editable:   cardEditable(card),
		Card:       &card,
	}

	// Условия прошлого периода: и для diff, и как подсказка «в прошлом месяце
	// фактическая доля была такой».
	prevY, prevM := prevMonth(card.Year, card.Month)
	if prevPl, err := s.metrics.EnsureInstance(ctx, prevY, prevM); err == nil {
		if prev, err := s.store.Conditions(ctx, prevPl, prevY, prevM); err == nil {
			view.Diff = diffConditions(prev, filtered, DefaultMpThresholds())
			view.Hints = map[int]map[string]float64{}
			for _, p := range prev {
				h := map[string]float64{"spp_pct": p.SppPct, "markup_pct": p.MarkupPct,
					"markup_total_pct": p.MarkupTotalPct}
				for b, v := range p.Shares {
					h["share:"+b] = v
				}
				view.Hints[p.CodeCFO] = h
			}
		}
	}
	return view, nil
}

// SaveConditions — сохранить условия площадки. Проверяет диапазоны (МП-02/03),
// требует обоснования при отклонении сверх порога и запрещает правку закрытого
// периода (ТЗ §7.2: «закрытый период не пересчитывается»).
func (s *MpConditionsService) SaveConditions(ctx context.Context, cardID int64, in MpConditions, actorID int64) (MpConditions, error) {
	card, err := s.cards.Card(ctx, cardID)
	if err != nil {
		return MpConditions{}, err
	}
	if !cardEditable(card) {
		return MpConditions{}, errors.New("период закрыт на запись: условия площадки не меняются")
	}
	in.PlID, in.Year, in.Month = card.PlID, card.Year, card.Month
	in.AuthorID = actorID
	if err := validateConditions(in); err != nil {
		return MpConditions{}, err
	}
	// Валюта ввода — валюта площадки (МП-10).
	attrs := map[int]MarketplaceRow{}
	for _, mp := range MarketplaceSeed() {
		attrs[mp.CodeCFO] = mp
	}
	mp, ok := attrs[in.CodeCFO]
	if !ok {
		return MpConditions{}, fmt.Errorf("площадка %d не найдена в справочнике", in.CodeCFO)
	}
	if want := platformCurrency(mp.Country); want != "" {
		if in.Currency == "" {
			in.Currency = want
		} else if in.Currency != want {
			return MpConditions{}, fmt.Errorf("валюта ввода должна быть %s (валюта площадки %s)", want, mp.NameCFO)
		}
	}

	// Обоснование обязательно, если условия отклонились от прошлого периода
	// сверх порога (ТЗ §6.1).
	prevY, prevM := prevMonth(card.Year, card.Month)
	if prevPl, err := s.metrics.EnsureInstance(ctx, prevY, prevM); err == nil {
		if prev, err := s.store.Conditions(ctx, prevPl, prevY, prevM); err == nil && len(prev) > 0 {
			need := requireReasonFor(diffConditions(prev, []MpConditions{in}, DefaultMpThresholds()))
			if len(need) > 0 && strings.TrimSpace(in.ChangeReason) == "" {
				return MpConditions{}, fmt.Errorf(
					"нужно обоснование изменения: %s (отклонение сверх порога к прошлому периоду)",
					describeDiffs(need))
			}
		}
	}

	saved, err := s.store.SaveConditions(ctx, in)
	if err != nil {
		return MpConditions{}, err
	}
	s.rec(ctx, actorID, "mp_conditions_save", "mp_conditions", saved.ID)
	return saved, nil
}

// CopyConditionsFromPrev — заполнить период условиями предыдущего одним действием.
func (s *MpConditionsService) CopyConditionsFromPrev(ctx context.Context, cardID int64, p Principal) (int, error) {
	card, err := s.cards.Card(ctx, cardID)
	if err != nil {
		return 0, err
	}
	if !cardEditable(card) {
		return 0, errors.New("период закрыт на запись")
	}
	prevY, prevM := prevMonth(card.Year, card.Month)
	fromPl, err := s.metrics.EnsureInstance(ctx, prevY, prevM)
	if err != nil {
		return 0, err
	}
	var allowed []int
	if !p.PlansAdmin && s.scope != nil {
		f, err := scopeFilterFor(ctx, s.scope, p)
		if err != nil {
			return 0, err
		}
		allowed = f.CodeCFOs()
		if len(allowed) == 0 && !f.Unrestricted() {
			return 0, errors.New("нет доступных площадок в вашем срезе")
		}
	}
	n, err := s.store.CopyConditions(ctx, fromPl, card.PlID, prevY, prevM, card.Year, card.Month, allowed)
	if err != nil {
		return 0, err
	}
	s.rec(ctx, p.UserID, "mp_conditions_copy", "form_card", cardID)
	return n, nil
}

// MpRecalcResult — результат пересчёта расходной части.
type MpRecalcResult struct {
	Period     PeriodRef                  `json:"period"`
	CalcMode   string                     `json:"calc_mode"`
	Platforms  map[int]map[string]float64 `json:"platforms"`
	Totals     MpFormTotals               `json:"totals"`
	Issues     []MpIssue                  `json:"issues"`
	Blocking   bool                       `json:"blocking"`
	CommonCost float64                    `json:"common_cost"`
	// Preview — расчёт без сохранения (обязательный предпросмотр diff при
	// изменении условий, ТЗ §7.2).
	Preview bool `json:"preview"`
}

// Recalc — пересчитать расходную часть карточки из условий и продаж.
// Порядок расчёта — как в ТЗ §7.2: справочники (НДС, курсы) → условия площадок →
// продажи → прямые затраты → маржа и PL по площадкам → общие затраты → PL формы.
func (s *MpConditionsService) Recalc(ctx context.Context, cardID int64, preview bool, actorID int64) (MpRecalcResult, error) {
	card, err := s.cards.Card(ctx, cardID)
	if err != nil {
		return MpRecalcResult{}, err
	}
	if !preview && !cardEditable(card) {
		return MpRecalcResult{}, errors.New("период закрыт: пересчёт утверждённой версии невозможен")
	}
	conds, err := s.store.Conditions(ctx, card.PlID, card.Year, card.Month)
	if err != nil {
		return MpRecalcResult{}, err
	}
	metrics, err := s.metrics.MetricsAll(ctx, card.PlID, card.Year, card.Month)
	if err != nil {
		return MpRecalcResult{}, err
	}

	segments := mpSegmentsOf(card)
	platformsInfo := make([]MarketplaceRow, 0)
	for _, mp := range MarketplaceSeed() {
		if segments[mp.Segment] {
			platformsInfo = append(platformsInfo, mp)
		}
	}
	// Продажи и ручные переопределения из сохранённой тактики.
	sales := map[int]float64{}
	overrides := map[int]map[string]float64{}
	for _, m := range metrics {
		if m.BlockType == BSalesManagerGross {
			sales[m.ProfitCenter] = m.Amount
			continue
		}
		if m.IsManual {
			if overrides[m.ProfitCenter] == nil {
				overrides[m.ProfitCenter] = map[string]float64{}
			}
			overrides[m.ProfitCenter][m.BlockType] = m.Amount
		}
	}

	condByCfo := map[int]MpConditions{}
	for _, c := range conds {
		condByCfo[c.CodeCFO] = c
	}
	values := map[int]map[string]float64{}
	for _, mp := range platformsInfo {
		cond, ok := condByCfo[mp.CodeCFO]
		if !ok {
			continue // условия не заданы — МП-01 отловит это в валидации
		}
		vat := 0.20
		if s.rates != nil {
			vat = s.rates.Vat(ctx, mp.Country, mp.CodeCFO)
		}
		out, log := computeMpPlatformInverse(MpInverseInput{
			SalesManagerGross: sales[mp.CodeCFO],
			Conditions:        conditionsToInput(cond, vat),
			Overrides:         overrides[mp.CodeCFO],
		})
		values[mp.CodeCFO] = out
		if !preview {
			// Лог расчёта по каждой вычисленной ячейке (ТЗ §7.2).
			_ = s.store.LogCalc(ctx, card.PlID, mp.CodeCFO, card.Year, card.Month,
				CalcInverse, cond.Version, log)
		}
	}

	// Общие затраты по МП (кроме прямых) — ввод по статьям PL, не от продаж.
	common, err := s.store.CommonCosts(ctx, card.PlID, card.ScopeKey, card.Year, card.Month)
	if err != nil {
		return MpRecalcResult{}, err
	}
	var commonSum float64
	for _, c := range common {
		commonSum += c.Amount
	}

	res := MpRecalcResult{
		Period: PeriodRef{Year: card.Year, Month: card.Month}, CalcMode: CalcInverse,
		Platforms: values, Totals: computeMpFormTotals(values, commonSum),
		CommonCost: commonSum, Preview: preview,
	}
	res.Issues = s.validate(ctx, card, platformsInfo, condByCfo, values)
	res.Blocking = HasBlocking(res.Issues)

	if !preview {
		if err := s.persist(ctx, card, values, actorID); err != nil {
			return res, err
		}
		s.rec(ctx, actorID, "mp_recalc", "form_card", cardID)
	}
	return res, nil
}

// validate — полный набор проверок формы: подтягивает владельцев расчёта,
// наименования статей справочника, факт прошлого месяца и штрафы.
func (s *MpConditionsService) validate(ctx context.Context, card Card, platforms []MarketplaceRow,
	conds map[int]MpConditions, values map[int]map[string]float64) []MpIssue {

	in := MpValidationInput{
		Year: card.Year, Month: card.Month, Platforms: platforms,
		Conditions: conds, Values: values,
		CalcOwner: map[[2]int]string{}, PLNames: plLineNames(),
		Thresholds: DefaultMpThresholds(),
	}
	// Владельцы расчёта логистических статей (ТЗ §8.1).
	for _, b := range []string{BCostFreight, BCostLogTransport, BCostLogWarehouse} {
		pl := blockToPL()[b]
		for _, mp := range platforms {
			if owner, ok, _ := s.store.CalcOwner(ctx, pl, mp.CodeCFO); ok {
				in.CalcOwner[[2]int{pl, mp.CodeCFO}] = owner
			}
		}
	}
	// Условия и факт прошлого периода — для предупреждений W1/W2/W5–W7.
	prevY, prevM := prevMonth(card.Year, card.Month)
	if prevPl, err := s.metrics.EnsureInstance(ctx, prevY, prevM); err == nil {
		if prev, err := s.store.Conditions(ctx, prevPl, prevY, prevM); err == nil {
			in.PrevConditions = map[int]MpConditions{}
			for _, c := range prev {
				in.PrevConditions[c.CodeCFO] = c
			}
		}
	}
	if s.fact != nil {
		if rows, err := s.fact.MpFact(ctx, prevY, prevM, card.ScopeKey); err == nil {
			in.FactPrev = map[int]map[string]float64{}
			in.PenaltiesFact = map[int]float64{}
			blocks := plToBlock()
			for _, r := range rows {
				if in.FactPrev[r.CodeCFO] == nil {
					in.FactPrev[r.CodeCFO] = map[string]float64{}
				}
				if b, ok := blocks[r.CodePL]; ok {
					in.FactPrev[r.CodeCFO][b] += r.Amount
				}
				if r.CodePL == mpPenaltyPL || r.CodePL == 66 {
					in.PenaltiesFact[r.CodeCFO] += r.Amount
				}
			}
		}
	}
	return ValidateMpForm(in)
}

// persist — записать пересчитанные суммы в тактику. Ручные значения сохраняются
// как есть (они уже пришли в расчёт через Overrides и помечены is_manual).
func (s *MpConditionsService) persist(ctx context.Context, card Card, values map[int]map[string]float64, actorID int64) error {
	spec := mpFormSpec()
	plOf := blockToPL()
	segOf := mpSegmentByCfo()
	rows := make([]MetricRow, 0, len(values)*len(spec))
	for cfo, vals := range values {
		for _, l := range spec {
			if l.Scope != ScopePlatform {
				continue
			}
			v, ok := vals[l.BlockType]
			if !ok {
				continue
			}
			rows = append(rows, MetricRow{
				LineCode: plOf[l.BlockType], BlockType: l.BlockType, ProfitCenter: cfo,
				Scenario: ScenarioTactic, Year: card.Year, Month: card.Month,
				Currency: firstNonEmpty(card.Currency, "RUB"), Amount: v,
			})
		}
		_ = segOf[cfo]
	}
	// Пишем по сегменту площадки, а не по сегменту задания (площадки large и
	// small могут жить в одной карточке).
	bySegment := map[string][]MetricRow{}
	for _, r := range rows {
		bySegment[segOf[r.ProfitCenter]] = append(bySegment[segOf[r.ProfitCenter]], r)
	}
	for segment, list := range bySegment {
		if err := s.metrics.UpsertMetrics(ctx, card.PlID, segment, list); err != nil {
			return err
		}
	}
	return nil
}

// CommonCosts — общие затраты по МП (7 групп статей, ТЗ §4.3).
func (s *MpConditionsService) CommonCosts(ctx context.Context, cardID int64) ([]MpCommonCost, []MpCommonCostGroup, error) {
	card, err := s.cards.Card(ctx, cardID)
	if err != nil {
		return nil, nil, err
	}
	rows, err := s.store.CommonCosts(ctx, card.PlID, card.ScopeKey, card.Year, card.Month)
	return rows, MpCommonCostSpec(), err
}

// SaveCommonCosts — сохранить общие затраты (ввод по статьям).
func (s *MpConditionsService) SaveCommonCosts(ctx context.Context, cardID int64, rows []MpCommonCost, actorID int64) error {
	card, err := s.cards.Card(ctx, cardID)
	if err != nil {
		return err
	}
	if !cardEditable(card) {
		return errors.New("период закрыт на запись")
	}
	allowed := map[int]bool{}
	for _, g := range MpCommonCostSpec() {
		for _, l := range g.Lines {
			allowed[l.CodePL] = true
		}
	}
	for i := range rows {
		if !allowed[rows[i].CodePL] {
			return fmt.Errorf("статья %d не входит в группы общих затрат МП", rows[i].CodePL)
		}
		rows[i].Year, rows[i].Month = card.Year, card.Month
		if rows[i].Segment == "" {
			rows[i].Segment = card.ScopeKey
		}
		if rows[i].Currency == "" {
			rows[i].Currency = firstNonEmpty(card.Currency, "RUB")
		}
	}
	if err := s.store.SaveCommonCosts(ctx, card.PlID, rows, actorID); err != nil {
		return err
	}
	s.rec(ctx, actorID, "mp_common_cost_save", "form_card", cardID)
	return nil
}

func (s *MpConditionsService) rec(ctx context.Context, userID int64, action, entity string, id int64) {
	if s.audit == nil || !s.audit.Enabled() {
		return
	}
	_ = s.audit.Record(ctx, AuditEvent{UserID: userID, Action: action, EntityType: entity, EntityID: id})
}

// describeDiffs — краткое описание изменений для сообщения об ошибке.
func describeDiffs(list []MpConditionsDiff) string {
	parts := make([]string, 0, len(list))
	for _, d := range list {
		parts = append(parts, fmt.Sprintf("%s: %.2f %% → %.2f %%", d.FieldName, d.Was*100, d.Now*100))
	}
	return strings.Join(parts, "; ")
}

// prevMonth — предыдущий календарный месяц.
func prevMonth(year, month int) (int, int) {
	if month <= 1 {
		return year - 1, 12
	}
	return year, month - 1
}

func firstNonEmpty(vals ...string) string {
	for _, v := range vals {
		if v != "" {
			return v
		}
	}
	return ""
}
