package plans

import (
	"context"
	"sort"
)

// Свод формы этапа 1.5 (ТЗ §8) и его свёртка в укрупнённые группы P&L (§12.3).
//
// Эталон свода — лист «результат ввода в BYN»: строки `GroupPL + Expense +
// CodePL`, колонки — сценарий × месяц. Здесь то же самое, только сценариев три
// (находка Н1): факт прошлого года, стратегия планового и тактика — то, что
// заполняет форма.
//
// Второй уровень («код PL → укрупнённая группа P&L», ответ 36) применяется тем
// же справочником, что ведёт НСИ. Код без второго уровня попадает в группу
// «Не распределено» и подсвечивается — ошибкой это не считается и публикацию не
// блокирует, так прямо сказано в ответе.

// ExpSummaryUnassigned — подгруппа для кодов PL без второго уровня справочника.
const ExpSummaryUnassigned = "Не распределено"

// ExpSummaryRow — строка свода.
type ExpSummaryRow struct {
	GroupPL  string `json:"group_pl"`
	Expense  string `json:"expense"`
	CodePL   int    `json:"code_pl"`
	PnlGroup string `json:"pnl_group"`
	// ByScenario — сценарий → месяц → сумма.
	ByScenario map[string]map[int]float64 `json:"by_scenario"`
	Totals     map[string]float64         `json:"totals"`
}

// ExpSummaryGroup — свёртка по укрупнённой группе P&L (ТЗ §12.3).
type ExpSummaryGroup struct {
	PnlGroup string          `json:"pnl_group"`
	ByMonth  map[int]float64 `json:"by_month"`
	Total    float64         `json:"total"`
	CodesPL  []int           `json:"codes_pl"`
	// Unassigned — группа собрана из кодов без второго уровня справочника.
	Unassigned bool `json:"unassigned,omitempty"`
}

// ExpSummary — ответ GET /api/plans/exp/{cardId}/summary.
type ExpSummary struct {
	CardID   int64             `json:"card_id"`
	PlanYear int               `json:"plan_year"`
	Currency string            `json:"currency"`
	Rows     []ExpSummaryRow   `json:"rows"`
	Groups   []ExpSummaryGroup `json:"groups"`
	Recon    []ExpRecon        `json:"reconciliations"`
}

// Summary — свод формы и контрольные сверки к нему.
func (s *ExpService) Summary(ctx context.Context, p Principal, cardID int64) (ExpSummary, error) {
	c, in, err := s.access(ctx, cardID)
	if err != nil {
		return ExpSummary{}, err
	}
	rows, _, err := s.repo.Rows(ctx, in.ID, ExpFilter{Limit: 5000})
	if err != nil {
		return ExpSummary{}, err
	}
	pnlByCode := map[int]string{}
	if m, err := s.pnlGroups(ctx); err == nil {
		pnlByCode = m
	}
	// Группы НСИ дополняются умолчаниями вида формы (розница — как на листе «Розница» Excel).
	pnlByCode = expApplyDefaultPnlGroups(in.FormKind, pnlByCode)

	// Строки свода: ключ ТЗ §8 — группа PL + статья + код PL.
	type key struct {
		group, expense string
		codePL         int
	}
	idx := map[key]int{}
	out := ExpSummary{CardID: c.ID, PlanYear: in.PlanYear, Currency: in.Currency}
	for _, r := range rows {
		k := key{r.GroupPL, r.Expense, r.CodePL}
		i, ok := idx[k]
		if !ok {
			pnl := pnlByCode[r.CodePL]
			if pnl == "" {
				pnl = ExpSummaryUnassigned
			}
			out.Rows = append(out.Rows, ExpSummaryRow{
				GroupPL: r.GroupPL, Expense: r.Expense, CodePL: r.CodePL, PnlGroup: pnl,
				ByScenario: map[string]map[int]float64{}, Totals: map[string]float64{},
			})
			i = len(out.Rows) - 1
			idx[k] = i
		}
		for _, v := range r.Values {
			if v.Amount == nil {
				continue
			}
			row := &out.Rows[i]
			if row.ByScenario[v.Scenario] == nil {
				row.ByScenario[v.Scenario] = map[int]float64{}
			}
			row.ByScenario[v.Scenario][v.Month] += *v.Amount
			row.Totals[v.Scenario] += *v.Amount
		}
	}
	sort.SliceStable(out.Rows, func(a, b int) bool {
		if out.Rows[a].GroupPL != out.Rows[b].GroupPL {
			return out.Rows[a].GroupPL < out.Rows[b].GroupPL
		}
		return out.Rows[a].CodePL < out.Rows[b].CodePL
	})

	out.Groups = ExpFoldToPnlGroupsOrdered(out.Rows, expPnlOrder(in.FormKind))
	out.Recon = ExpReconcile(ExpReconInput{Rows: rows, Registries: s.registriesFor(ctx, in)})
	return out, nil
}

// ExpFoldToPnlGroups — свёртка свода в укрупнённые группы P&L (ТЗ §12.3).
// Сворачивается ТАКТИКА (то, что заполняет форма): свод этапа 1.6 строится по
// плановым числам, а факт и стратегия лежат рядом для сравнения.
func ExpFoldToPnlGroups(rows []ExpSummaryRow) []ExpSummaryGroup {
	return ExpFoldToPnlGroupsOrdered(rows, nil)
}

// ExpFoldToPnlGroupsOrdered — то же с заданным порядком групп (для розницы — как в Excel).
func ExpFoldToPnlGroupsOrdered(rows []ExpSummaryRow, order []string) []ExpSummaryGroup {
	byGroup := map[string]*ExpSummaryGroup{}
	for _, r := range rows {
		g, ok := byGroup[r.PnlGroup]
		if !ok {
			g = &ExpSummaryGroup{
				PnlGroup:   r.PnlGroup,
				ByMonth:    map[int]float64{},
				Unassigned: r.PnlGroup == ExpSummaryUnassigned,
			}
			byGroup[r.PnlGroup] = g
		}
		g.CodesPL = append(g.CodesPL, r.CodePL)
		for m, v := range r.ByScenario[ExpScenarioPlan] {
			g.ByMonth[m] += v
			g.Total += v
		}
	}
	out := make([]ExpSummaryGroup, 0, len(byGroup))
	for _, g := range byGroup {
		sort.Ints(g.CodesPL)
		out = append(out, *g)
	}
	// «Не распределено» всегда последней: это остаток, а не подгруппа наравне.
	sortPnlGroups(out, order)
	return out
}

// pnlGroups — второй уровень справочника статей: код PL → укрупнённая группа.
func (s *ExpService) pnlGroups(ctx context.Context) (map[int]string, error) {
	type grouper interface {
		PnlGroups(ctx context.Context) (map[int]string, error)
	}
	g, ok := s.repo.(grouper)
	if !ok {
		return nil, nil
	}
	return g.PnlGroups(ctx)
}
