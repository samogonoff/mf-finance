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
	// articleFact — факт по статьям для первичного заполнения долей (mp_conditions_seed.go).
	articleFact MpArticleFactSource
}

// NewMpConditionsService — конструктор.
func NewMpConditionsService(store MpConditionsStore, metrics MetricStore, cards CardStore,
	rates *RateBook, fact MpFactSource, scope ScopeStore) *MpConditionsService {
	return &MpConditionsService{store: store, metrics: metrics, cards: cards,
		rates: rates, fact: fact, scope: scope}
}

// WithAudit подключает журнал модуля.
func (s *MpConditionsService) WithAudit(a AuditSink) *MpConditionsService { s.audit = a; return s }

// WithArticleFact подключает факт по статьям («Заполнить доли из факта»).
func (s *MpConditionsService) WithArticleFact(src MpArticleFactSource) *MpConditionsService {
	s.articleFact = src
	return s
}

func (s *MpConditionsService) userScope(ctx context.Context, p Principal) (ScopeFilter, error) {
	if p.PlansAdmin {
		return NewScopeFilter(true, nil), nil
	}
	full, ok := s.scope.(FullScopeStore)
	if !ok || p.UserID == 0 {
		return ScopeFilter{}, errors.New("срез доступа недоступен")
	}
	rules, err := full.UserScopes(ctx, p.UserID)
	if err != nil {
		return ScopeFilter{}, err
	}
	return NewScopeFilter(false, rules), nil
}

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
func (s *MpConditionsService) Conditions(ctx context.Context, cardID int64, actor Principal) (MpConditionsView, error) {
	card, err := s.cards.Card(ctx, cardID)
	if err != nil {
		return MpConditionsView{}, err
	}
	scope, err := s.userScope(ctx, actor)
	if err != nil {
		return MpConditionsView{}, err
	}
	visible := map[int]bool{}
	for _, mp := range MarketplaceSeed() {
		if mpSegmentsOf(card)[mp.Segment] && mpSaveScopeAllows(scope, card.StepCode, mp.CodeCFO) {
			visible[mp.CodeCFO] = true
		}
	}
	if len(visible) == 0 {
		return MpConditionsView{}, errors.New("карточка вне вашего среза доступа")
	}
	cur, err := s.store.Conditions(ctx, card.PlID, card.Year, card.Month)
	if err != nil {
		return MpConditionsView{}, err
	}
	segments := mpSegmentsOf(card)
	filtered := make([]MpConditions, 0, len(cur))
	for _, c := range cur {
		if visible[c.CodeCFO] && (c.Segment == "" || segments[c.Segment]) {
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
			visiblePrev := make([]MpConditions, 0)
			for _, c := range prev {
				if visible[c.CodeCFO] {
					visiblePrev = append(visiblePrev, c)
				}
			}
			view.Diff = diffConditions(visiblePrev, filtered, DefaultMpThresholds())
			view.Hints = map[int]map[string]float64{}
			for _, p := range visiblePrev {
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
func (s *MpConditionsService) SaveConditions(ctx context.Context, cardID int64, in MpConditions, actor Principal) (MpConditions, error) {
	card, err := s.cards.Card(ctx, cardID)
	if err != nil {
		return MpConditions{}, err
	}
	if !cardEditable(card) {
		return MpConditions{}, errors.New("период закрыт на запись: условия площадки не меняются")
	}
	in.PlID, in.Year, in.Month, in.CardID = card.PlID, card.Year, card.Month, card.ID
	in.AuthorID = actor.UserID
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
	if !mpSegmentsOf(card)[mp.Segment] {
		return MpConditions{}, fmt.Errorf("площадка %d вне сегмента карточки", in.CodeCFO)
	}
	scope, err := s.userScope(ctx, actor)
	if err != nil {
		return MpConditions{}, err
	}
	if !mpSaveScopeAllows(scope, card.StepCode, in.CodeCFO) {
		return MpConditions{}, errors.New("площадка вне вашего среза доступа")
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
	s.rec(ctx, actor.UserID, "mp_conditions_save", "mp_conditions", saved.ID)
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
	filter, err := s.userScope(ctx, p)
	if err != nil {
		return 0, err
	}
	allowed := make([]int, 0)
	for _, mp := range MarketplaceSeed() {
		if mpSegmentsOf(card)[mp.Segment] && mpSaveScopeAllows(filter, card.StepCode, mp.CodeCFO) {
			allowed = append(allowed, mp.CodeCFO)
		}
	}
	if len(allowed) == 0 {
		return 0, errors.New("нет доступных площадок в вашем срезе")
	}
	n, err := s.store.CopyConditions(ctx, card.ID, fromPl, card.PlID, prevY, prevM, card.Year, card.Month, allowed)
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
func (s *MpConditionsService) Recalc(ctx context.Context, cardID int64, preview bool, actor Principal) (MpRecalcResult, error) {
	card, err := s.cards.Card(ctx, cardID)
	if err != nil {
		return MpRecalcResult{}, err
	}
	if !preview && !cardEditable(card) {
		return MpRecalcResult{}, errors.New("период закрыт: пересчёт утверждённой версии невозможен")
	}
	scope, err := s.userScope(ctx, actor)
	if err != nil {
		return MpRecalcResult{}, err
	}
	// Пересчёт «от условий» — это inverse-направление (ТЗ §3.1). В legacy-режиме
	// суммы приходят из источника и пересчитывать их от долей нельзя: получилось бы
	// молчаливое переписывание чисел другим алгоритмом. Режим включается на
	// карточке (PUT /api/plans/cards/{id}/calc-mode).
	if card.CalcMode != CalcInverse && !cardEditable(card) {
		return MpRecalcResult{}, errors.New(
			"карточка считается в режиме «от сумм»: пересчёт расходной части от условий " +
				"доступен после включения режима «от условий» (инверсия) на карточке")
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
		if segments[mp.Segment] && mpSaveScopeAllows(scope, card.StepCode, mp.CodeCFO) {
			platformsInfo = append(platformsInfo, mp)
		}
	}
	if len(platformsInfo) == 0 {
		return MpRecalcResult{}, errors.New("карточка вне вашего среза доступа")
	}
	// Ввод формы: продажи и %СПП. Сохранённые ручные суммы старых периодов
	// не становятся входами нового каскада.
	sales := map[int]float64{}
	spp := map[int]float64{}
	for _, m := range metrics {
		if m.BlockType == BSalesManagerGross {
			sales[m.ProfitCenter] = m.Amount
			continue
		}
		if m.BlockType == BSPP {
			spp[m.ProfitCenter] = m.Amount
		}
	}

	condByCfo := map[int]MpConditions{}
	for _, c := range conds {
		condByCfo[c.CodeCFO] = c
	}
	// Базис НДС — тот же, что в форме: отношение плановых сумм площадки, ставка
	// справочника фолбэком. Иначе предпросмотр пересчёта показывал бы суммы,
	// которых форма не даёт.
	planSegments := make([]string, 0, 2)
	for _, mp := range platformsInfo {
		planSegments = append(planSegments, mp.Segment)
	}
	planLayer := mpPlanByBlock(ctx, s.fact, card.Year, card.Month, planSegments)
	prevPY, prevPM := prevMonth(card.Year, card.Month)
	planPrevLayer := mpPlanByBlock(ctx, s.fact, prevPY, prevPM, planSegments)

	values := map[int]map[string]float64{}
	basisByCfo := map[int]MpVatBasis{}
	normByCfo := map[int]float64{}
	for _, mp := range platformsInfo {
		cond := condByCfo[mp.CodeCFO] // без условий статьи пусты; МП-01 блокирует отправку
		vat := 0.20
		if s.rates != nil {
			vat = s.rates.Vat(ctx, mp.Country, mp.CodeCFO)
		}
		basis := mpVatBasis(planLayer, planPrevLayer, mp.CodeCFO, card.Year, card.Month, vat)
		basisByCfo[mp.CodeCFO] = basis
		// Норматив удержаний — из справочника с периодом действия; сид остаётся
		// фолбэком (mpNormShareOf).
		norm := mpNormShareOf(mp.CodeCFO, card.Year, card.Month)
		if s.rates != nil {
			if v, ok := s.rates.NormShare(ctx, mp.CodeCFO, card.Year, card.Month); ok {
				norm = v
			}
		}
		normByCfo[mp.CodeCFO] = norm
		input := conditionsToInputNorm(cond, basis, norm)
		if v, ok := spp[mp.CodeCFO]; ok {
			input.SppPct = v
		}
		out, log := computeMpPlatformInverse(MpInverseInput{
			SalesManagerGross: sales[mp.CodeCFO],
			Conditions:        input,
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
		Platforms: values, Totals: computeMpFormTotalsMode(values, commonSum, mpPLWithCommon(card)),
		CommonCost: commonSum, Preview: preview,
	}
	res.Issues = s.validate(ctx, card, platformsInfo, condByCfo, values, basisByCfo, normByCfo)
	res.Blocking = HasBlocking(res.Issues)

	if !preview {
		if err := s.persist(ctx, card, values, actor.UserID); err != nil {
			return res, err
		}
		s.rec(ctx, actor.UserID, "mp_recalc", "form_card", cardID)
	}
	return res, nil
}

// validate — полный набор проверок формы: подтягивает владельцев расчёта,
// наименования статей справочника, факт прошлого месяца и штрафы.
func (s *MpConditionsService) validate(ctx context.Context, card Card, platforms []MarketplaceRow,
	conds map[int]MpConditions, values map[int]map[string]float64,
	vat map[int]MpVatBasis, norms map[int]float64) []MpIssue {

	in := MpValidationInput{
		Year: card.Year, Month: card.Month, Platforms: platforms,
		Conditions: conds, Values: values, VatBasis: vat, Norms: norms,
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
				if r.CodePL == mpPenaltyFactKey || r.CodePL == 66 {
					in.PenaltiesFact[r.CodeCFO] += r.Amount
				}
			}
		}
	}
	return ValidateMpForm(in)
}

// persist — записать пересчитанные суммы в тактику.
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
	if pg, ok := s.metrics.(*pgStore); ok {
		tx, err := pg.pool.Begin(ctx)
		if err != nil {
			return err
		}
		defer tx.Rollback(ctx)
		if err := lockEditableMpCardTx(ctx, tx, card.ID); err != nil {
			return err
		}
		for segment, list := range bySegment {
			for _, m := range list {
				_, err := tx.Exec(ctx, `INSERT INTO pl_metric (pl_id,template_code,segment,line_code,block_type,
					profit_center,country,scenario,period_year,period_month,currency,amount,is_manual)
					VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,false)
					ON CONFLICT (pl_id,template_code,segment,line_code,block_type,profit_center,scenario,period_year,period_month,currency)
					DO UPDATE SET amount=EXCLUDED.amount,is_manual=false`, card.PlID, TemplateMP, segment,
					m.LineCode, m.BlockType, m.ProfitCenter, m.Country, m.Scenario, m.Year, m.Month, m.Currency, m.Amount)
				if err != nil {
					return err
				}
			}
		}
		// Удаляем старые суммы статей, доли которых больше нет.
		for cfo, vals := range values {
			missing := make([]string, 0)
			for _, block := range mpCostBlocks {
				if _, present := vals[block]; !present {
					missing = append(missing, block)
				}
			}
			if len(missing) == 0 {
				continue
			}
			if _, err := tx.Exec(ctx, `DELETE FROM pl_metric
				WHERE pl_id=$1 AND template_code=$2 AND profit_center=$3
				  AND period_year=$4 AND period_month=$5 AND scenario=$6
				  AND block_type=ANY($7)`, card.PlID, TemplateMP, cfo,
				card.Year, card.Month, ScenarioTactic, missing); err != nil {
				return err
			}
		}
		if err := touchMpCardTx(ctx, tx, card.ID); err != nil {
			return err
		}
		return tx.Commit(ctx)
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
	if err != nil {
		return rows, MpCommonCostSpec(), err
	}
	return s.withPlanCommonCosts(ctx, card, rows), MpCommonCostSpec(), nil
}

// withPlanCommonCosts — статьи, которых нет во вводе, добираются ИЗ ПЛАНА того же
// месяца. В прототипе общие затраты тактики не пересчитываются и не вводятся:
// колонка тактики ссылается на план один в один (=BF202). Введённое значение
// плану не уступает — оно остаётся переопределением.
func (s *MpConditionsService) withPlanCommonCosts(ctx context.Context, card Card, rows []MpCommonCost) []MpCommonCost {
	if s.fact == nil {
		return rows
	}
	planRows, err := s.fact.MpStrategy(ctx, card.Year, card.Month, card.ScopeKey)
	if err != nil || len(planRows) == 0 {
		return rows
	}
	byPL := map[int]float64{}
	for _, r := range planRows {
		byPL[r.CodePL] += r.Amount
	}
	has := map[int]bool{}
	for _, r := range rows {
		has[r.CodePL] = true
	}
	for _, g := range MpCommonCostSpec() {
		for _, l := range g.Lines {
			amount, ok := byPL[l.CodePL]
			if has[l.CodePL] || !ok || amount == 0 {
				continue
			}
			rows = append(rows, MpCommonCost{
				Segment: card.ScopeKey, CodePL: l.CodePL, Name: l.Name, Group: g.Group,
				Year: card.Year, Month: card.Month,
				Currency: firstNonEmpty(card.Currency, "RUB"),
				Amount:   amount, Source: "plan",
			})
		}
	}
	return rows
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
	if err := s.store.SaveCommonCosts(ctx, card.ID, card.PlID, rows, actorID); err != nil {
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
