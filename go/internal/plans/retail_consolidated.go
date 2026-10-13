package plans

import (
	"context"
	"errors"
	"log"
	"net/http"
	"sort"
)

// Свод розницы «все четыре страны» (ТЗ доработок кабинета, п. 3.2).
//
// «Свод уровня «все четыре розницы» с раскрытием до страны, потом до магазина».
// Свод одной карточки (retail_summary.go) этого не даёт: он считает страну.
// Здесь — четыре карточки периода рядом: итог в BYN (общая валюта, нац.
// валюты стран несопоставимы), строка страны в BYN и нац. валюте, под ней —
// магазины в BYN. Пересчёт — теми же курсами по назначению показателя, что и
// форма (scaleMetricsFx), иначе свод разошёлся бы с карточками.
//
// Шлюз ФЗ 02.10 п. 18 (на утверждение розница уходит, только когда пройдены все
// четыре страны) живёт в CardService; свод только показывает, кого ждём.

// retailCountries — страны розницы в порядке вывода.
var retailCountries = []string{"BY", "RU", "KZ", "UZ"}

// RetailConsolidatedStore — магазин в своде (BYN).
type RetailConsolidatedStore struct {
	CodeCFO int                  `json:"code_cfo"`
	NameCFO string               `json:"cfo"`
	City    string               `json:"city"`
	LFL     string               `json:"lfl"`
	Metrics RetailSummaryMetrics `json:"metrics"`
}

// RetailConsolidatedCountry — страна в своде.
type RetailConsolidatedCountry struct {
	Country     string                    `json:"country"`
	CardID      int64                     `json:"card_id"`
	Title       string                    `json:"title"`
	Status      string                    `json:"status"`
	StepCode    string                    `json:"step_code"`
	NatCurrency string                    `json:"nat_currency"`
	Rows        int                       `json:"rows"`
	Nat         RetailSummaryMetrics      `json:"nat"`
	BYN         RetailSummaryMetrics      `json:"byn"`
	Stores      []RetailConsolidatedStore `json:"stores"`
	// Error — почему страны нет в своде (нет карточки, вне среза доступа).
	Error string `json:"error,omitempty"`
}

// RetailConsolidated — ответ GET /api/plans/retail/period/{plId}/consolidated.
type RetailConsolidated struct {
	PlID      int64                       `json:"pl_id"`
	Year      int                         `json:"year"`
	Month     int                         `json:"month"`
	Currency  string                      `json:"currency"`
	Total     RetailSummaryMetrics        `json:"total"`
	Countries []RetailConsolidatedCountry `json:"countries"`
	// Pending — страны, ещё не прошедшие до шага Дегтеревой: пока список не
	// пуст, розница на утверждение (1.4) не уходит.
	Pending []string `json:"pending"`
}

// recomputeRatios — проценты свода от агрегатов (как aggregateMetrics).
func (m *RetailSummaryMetrics) recomputeRatios() {
	m.TacticVsStrat = iferrorDiv(m.Tactic, m.Strategy)
	m.TacticVsPrevM = iferrorDiv(m.Tactic, m.FactPrevMonth)
	m.LFL = iferrorRatioMinus1(m.Tactic, m.FactPrevYear)
	m.TacticVsFactPY = iferrorDiv(m.Tactic, m.FactPrevYear)
	m.FactVsFactPY = iferrorDiv(m.FactCur, m.FactPrevYear)
	m.DoneVsStrategy = iferrorDiv(m.FactCur, m.Strategy)
	m.DoneVsTactic = iferrorDiv(m.FactCur, m.Tactic)
}

// sumMetrics — сумма денежных показателей в ОДНОЙ валюте + проценты заново.
func sumMetrics(list []RetailSummaryMetrics) RetailSummaryMetrics {
	var t RetailSummaryMetrics
	for _, m := range list {
		t.HasFactPrevYear = t.HasFactPrevYear || m.HasFactPrevYear
		t.HasFactPrevMonth = t.HasFactPrevMonth || m.HasFactPrevMonth
		t.HasStrategy = t.HasStrategy || m.HasStrategy
		t.FactPrevYear += m.FactPrevYear
		t.Strategy += m.Strategy
		t.Tactic += m.Tactic
		t.FactCur += m.FactCur
		t.FactPrevMonth += m.FactPrevMonth
		t.PeriodTotal += m.PeriodTotal
		t.YearExpectation += m.YearExpectation
	}
	t.recomputeRatios()
	return t
}

// consolidateCountry — строка страны и магазины из рассчитанных строк формы.
func consolidateCountry(rows []RetailRow, in RetailSummaryInput, toBYN RetailFxRates) (nat, byn RetailSummaryMetrics, stores []RetailConsolidatedStore) {
	nat = aggregateMetrics(rows, in)
	byn = scaleMetricsFx(nat, toBYN)
	byn.recomputeRatios() // курсы по назначению разные — проценты в BYN свои
	stores = make([]RetailConsolidatedStore, 0, len(rows))
	for _, r := range rows {
		m := scaleMetricsFx(aggregateMetrics([]RetailRow{r}, in), toBYN)
		m.recomputeRatios()
		stores = append(stores, RetailConsolidatedStore{
			CodeCFO: r.CodeCFO, NameCFO: r.NameCFO, City: r.City, LFL: r.LFLEffective, Metrics: m,
		})
	}
	sort.SliceStable(stores, func(i, j int) bool { return stores[i].Metrics.Tactic > stores[j].Metrics.Tactic })
	return nat, byn, stores
}

// Consolidated — свод четырёх стран периода. Страна вне среза пользователя
// остаётся в списке с пояснением, но в итог не входит: итог по чужим магазинам
// раскрыл бы то, что срез прячет.
func (s *RetailService) Consolidated(ctx context.Context, p Principal, plID int64) (RetailConsolidated, error) {
	cards, err := s.cards.Cards(ctx, plID)
	if err != nil {
		return RetailConsolidated{}, err
	}
	byCountry := map[string]Card{}
	for _, c := range cards {
		if c.FormCode == TemplateRetail {
			country := c.Country
			if country == "" {
				country = c.ScopeKey
			}
			byCountry[country] = c
		}
	}
	if len(byCountry) == 0 {
		return RetailConsolidated{}, errors.New("в периоде нет карточек розницы")
	}
	out := RetailConsolidated{PlID: plID, Currency: "BYN",
		Pending: missingRetailCountries(cards, 0, false)}
	totals := make([]RetailSummaryMetrics, 0, len(retailCountries))
	for _, country := range retailCountries {
		c, ok := byCountry[country]
		row := RetailConsolidatedCountry{Country: country}
		if !ok {
			row.Error = "карточка страны не заведена"
			out.Countries = append(out.Countries, row)
			continue
		}
		out.Year, out.Month = c.Year, c.Month
		row.CardID, row.Title, row.StepCode = c.ID, c.Title, c.StepCode
		row.Status = CardStatusLabel(c.Status, c.StepCode)
		a, err := s.access(ctx, c.ID, p)
		if err != nil {
			row.Error = err.Error()
			out.Countries = append(out.Countries, row)
			continue
		}
		row.NatCurrency = a.inst.Currency
		ser, klient, stores := s.series(ctx, c.Country, c.Year, c.Month)
		// Строки страны появляются синхронизацией со справочником при открытии
		// формы. Свод делает то же, что форма: иначе страна, чью форму ещё не
		// открывали, выглядела бы пустой при живых данных.
		if len(stores) > 0 && cardEditable(c) {
			if _, _, _, err := s.repo.SyncRows(ctx, a.inst, stores, klient, p.UserID); err != nil {
				log.Printf("plans retail: свод — синхронизация строк карточки %d: %v", c.ID, err)
			}
		}
		all, err := s.repo.Rows(ctx, a.inst.ID)
		if err != nil {
			return RetailConsolidated{}, err
		}
		visible := make([]RetailRow, 0, len(all))
		for _, r := range all {
			if a.allowsRow(r) {
				visible = append(visible, r)
			}
		}
		months := retailPlanMonths(c.Month)
		in := RetailSummaryInput{Country: country, NatCurrency: a.inst.Currency, Year: c.Year, Month: c.Month,
			PlanMonths: months, Series: ser}
		computed := ComputeRetailForm(visible, ser, c.Year, c.Month, months)
		toBYN := SameRetailFx(1)
		if a.inst.Currency != "BYN" && s.rates != nil {
			toBYN = s.retailFx(ctx, a.inst.Currency, "BYN", c.Year, c.Month)
		}
		row.Rows = len(computed)
		row.Nat, row.BYN, row.Stores = consolidateCountry(computed, in, toBYN)
		totals = append(totals, row.BYN)
		out.Countries = append(out.Countries, row)
	}
	out.Total = sumMetrics(totals)
	return out, nil
}

// ConsolidatedGet — GET /api/plans/retail/period/{plId}/consolidated.
func (h *RetailHandler) ConsolidatedGet(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(w, r, "plId")
	if !ok {
		return
	}
	res, err := h.svc.Consolidated(r.Context(), h.principal(r), id)
	if err != nil {
		retailErr(w, err)
		return
	}
	writeJSON(w, http.StatusOK, res)
}
