package plans

import (
	"context"
	"errors"
	"net/http"
	"sort"
	"strings"
)

// Свод расходов розницы «все четыре страны» — расходный аналог свода продаж
// (retail_consolidated.go): четыре карточки формы 1.5 (R-BY, R-RU, R-KZ, R-UZ) периода рядом.
// Строки — управленческие группы расходов (как на листе «Розница» Excel) с раскрытием до статьи;
// колонки — страны в BYN (общая валюта: национальные валюты стран несопоставимы), итог и три
// колонки факта (тот же месяц прошлого года, прошлый месяц, текущий месяц) — итого по всем странам.
//
// Пересчёт в BYN — теми же курсами по назначению, что у свода продаж (retailFx): тактика — курс
// факта ПРЕДЫДУЩЕГО месяца, факт — курс своего месяца. Окно плана — как на форме расходов:
// месяц плана, «месяц–декабрь» или весь год.

// Окна свода (то же, что «Период формы» на форме расходов).
const (
	ExpWindowMonth = "month"
	ExpWindowTail  = "tail"
	ExpWindowYear  = "year"
)

// ExpConsCountry — страна в своде.
type ExpConsCountry struct {
	Country     string  `json:"country"`
	CardID      int64   `json:"card_id,omitempty"`
	Title       string  `json:"title,omitempty"`
	Status      string  `json:"status,omitempty"`
	NatCurrency string  `json:"nat_currency,omitempty"`
	Rows        int     `json:"rows"`
	Rate        float64 `json:"rate"` // курс тактики к BYN (1 для BYN; 0, если курса нет)
	Plan        float64 `json:"plan"` // план страны за окно, BYN
	// NoRate — курса валюты страны к BYN нет: суммы страны в свод не входят.
	NoRate bool `json:"no_rate,omitempty"`
	// Error — почему страны нет в своде (нет карточки, ошибка чтения).
	Error string `json:"error,omitempty"`
}

// ExpConsAmounts — суммы строки свода в BYN.
type ExpConsAmounts struct {
	Plan      map[string]float64 `json:"plan"` // страна → план за окно
	PlanTotal float64            `json:"plan_total"`
	FactLY    float64            `json:"fact_ly"`   // факт того же месяца прошлого года
	FactPrev  float64            `json:"fact_prev"` // факт прошлого месяца
	FactCur   float64            `json:"fact_cur"`  // факт текущего месяца плана (на момент загрузки)
}

func newExpConsAmounts() ExpConsAmounts { return ExpConsAmounts{Plan: map[string]float64{}} }

func (a *ExpConsAmounts) add(o ExpConsAmounts) {
	for c, v := range o.Plan {
		a.Plan[c] += v
	}
	a.PlanTotal += o.PlanTotal
	a.FactLY += o.FactLY
	a.FactPrev += o.FactPrev
	a.FactCur += o.FactCur
}

// ExpConsLine — статья в группе.
type ExpConsLine struct {
	CodePL  int    `json:"code_pl"`
	Expense string `json:"expense"`
	ExpConsAmounts
}

// ExpConsGroup — управленческая группа расходов.
type ExpConsGroup struct {
	Group      string        `json:"group"`
	Unassigned bool          `json:"unassigned,omitempty"`
	Lines      []ExpConsLine `json:"lines"`
	ExpConsAmounts
}

// Каналы свода расходов: розница (4 карточки по странам), интернет-магазин и опт (по одной карточке).
const (
	ExpChannelRetail = "retail"
	ExpChannelIM     = "im"
	ExpChannelOpt    = "opt"
)

// expOptCountries — страны опта в порядке вывода (колонки свода): совпадают с GroupCFO3 листа «ОПТ».
var expOptCountries = []string{"BY", "RU", "UZ"}

// ExpConsolidated — ответ GET /api/plans/exp-consolidated/{plId}.
type ExpConsolidated struct {
	// Channel — канал свода: retail, im или opt.
	Channel   string           `json:"channel"`
	PlID      int64            `json:"pl_id"`
	Year      int              `json:"year"`
	Month     int              `json:"month"`
	Window    string           `json:"window"`
	FromMonth int              `json:"from_month"`
	ToMonth   int              `json:"to_month"`
	Currency  string           `json:"currency"`
	Countries []ExpConsCountry `json:"countries"`
	Groups    []ExpConsGroup   `json:"groups"`
	Total     ExpConsAmounts   `json:"total"`
	// FactBasis — "payments": факт построен по платежам (начислений в источнике нет).
	FactBasis string `json:"fact_basis,omitempty"`
	// Missing — страны, по которым карточки расходов нет: итог неполный.
	Missing []string `json:"missing,omitempty"`
	// Syncing — загрузка из источника по какой-то карточке ещё идёт: данные неполные, страница повторяет запрос.
	Syncing bool `json:"syncing,omitempty"`
	// Warnings — пояснения к неполному итогу (страна не прочитана, нет курса, загрузка не удалась, карточка закрыта и пуста).
	Warnings []string `json:"warnings,omitempty"`
	// AbsentChannels — каналы общего свода, карточки которых в периоде нет (штатная ситуация, не сбой).
	AbsentChannels []string `json:"absent_channels,omitempty"`
}

// expWindowMonths — месяцы окна: month → [m, m], tail → [m, 12], year → [1, 12].
func expWindowMonths(window string, month int) (from, to int, normalized string) {
	if month < 1 || month > 12 {
		month = 1
	}
	switch window {
	case ExpWindowMonth:
		return month, month, ExpWindowMonth
	case ExpWindowYear:
		return 1, 12, ExpWindowYear
	default:
		return month, 12, ExpWindowTail
	}
}

// expFxToBYN — курсы к BYN по назначению колонки (зеркало retailFx свода продаж).
// NoRate — курса валюты страны к BYN нет: суммы страны не пересчитать, и в свод они не входят.
type expFxToBYN struct {
	Tactic, FactLY, FactPrev, FactCur float64
	NoRate                            bool
}

func sameExpFx(r float64) expFxToBYN { return expFxToBYN{Tactic: r, FactLY: r, FactPrev: r, FactCur: r} }

// expConsInput — одна страна на входе сборки свода.
type expConsInput struct {
	Country string
	Card    Card
	Inst    ExpInstance
	Rows    []ExpRow
	Fx      expFxToBYN
}

// buildExpConsolidated — чистая сборка свода из строк карточек: суммирует план окна и факт
// периодов сравнения, переводит в BYN и раскладывает по группам и статьям. Вынесена из сервиса,
// чтобы правила (окно, курсы, группы, порядок) проверялись тестом без БД.
func buildExpConsolidated(plID int64, year, month int, window string, in []expConsInput, pnlByCode map[int]string, order []string) ExpConsolidated {
	from, to, win := expWindowMonths(window, month)
	out := ExpConsolidated{Channel: ExpChannelRetail, PlID: plID, Year: year, Month: month, Window: win, FromMonth: from, ToMonth: to, Currency: "BYN"}
	prevM, prevScenario := month-1, ExpScenarioFactCur
	if month <= 1 {
		prevM, prevScenario = 12, ExpScenarioFact // январь: прошлый месяц — декабрь прошлого года
	}

	type key struct {
		group string
		code  int
	}
	lines := map[key]*ExpConsLine{}
	names := map[int]string{}
	for _, c := range in {
		for _, r := range c.Rows {
			if r.Expense != "" {
				names[r.CodePL] = r.Expense
			}
			g := pnlByCode[r.CodePL]
			if g == "" {
				g = ExpSummaryUnassigned
			}
			k := key{g, r.CodePL}
			l := lines[k]
			if l == nil {
				l = &ExpConsLine{CodePL: r.CodePL, ExpConsAmounts: newExpConsAmounts()}
				lines[k] = l
			}
			// Нет курса к BYN — сумму страны не пересчитать, а складывать нац. валюту с BYN нельзя
			// (итог завышался бы в десятки–тысячи раз). Статья остаётся в списке без сумм.
			if c.Fx.NoRate {
				continue
			}
			var plan float64
			for _, v := range r.Values {
				if v.Scenario == ExpScenarioPlan && v.Amount != nil && v.Month >= from && v.Month <= to {
					plan += *v.Amount
				}
			}
			plan *= c.Fx.Tactic
			l.Plan[c.Country] += plan
			l.PlanTotal += plan
			if v, ok := expCellOf(r, ExpScenarioFact, month); ok {
				l.FactLY += v * c.Fx.FactLY
			}
			if v, ok := expCellOf(r, prevScenario, prevM); ok {
				l.FactPrev += v * c.Fx.FactPrev
			}
			if v, ok := expCellOf(r, ExpScenarioFactCur, month); ok {
				l.FactCur += v * c.Fx.FactCur
			}
		}
	}

	byGroup := map[string]*ExpConsGroup{}
	for k, l := range lines {
		l.Expense = names[l.CodePL]
		g := byGroup[k.group]
		if g == nil {
			g = &ExpConsGroup{Group: k.group, Unassigned: k.group == ExpSummaryUnassigned, ExpConsAmounts: newExpConsAmounts()}
			byGroup[k.group] = g
		}
		g.Lines = append(g.Lines, *l)
		g.add(l.ExpConsAmounts)
	}
	out.Total = newExpConsAmounts()
	pos := map[string]int{}
	for i, g := range order {
		pos[g] = i
	}
	for _, g := range byGroup {
		sort.SliceStable(g.Lines, func(a, b int) bool { return g.Lines[a].CodePL < g.Lines[b].CodePL })
		out.Total.add(g.ExpConsAmounts)
		out.Groups = append(out.Groups, *g)
	}
	sort.SliceStable(out.Groups, func(a, b int) bool {
		ga, gb := out.Groups[a], out.Groups[b]
		if ga.Unassigned != gb.Unassigned {
			return !ga.Unassigned
		}
		pa, oka := pos[ga.Group]
		pb, okb := pos[gb.Group]
		if oka != okb {
			return oka
		}
		if oka && okb {
			return pa < pb
		}
		return ga.Group < gb.Group
	})
	for _, c := range in {
		row := ExpConsCountry{Country: c.Country, CardID: c.Card.ID, Title: c.Card.Title,
			Status: CardStatusLabel(c.Card.Status, c.Card.StepCode), NatCurrency: c.Inst.Currency,
			Rows: len(c.Rows), Rate: c.Fx.Tactic, Plan: out.Total.Plan[c.Country], NoRate: c.Fx.NoRate}
		out.Countries = append(out.Countries, row)
	}
	return out
}

// expCellOf — заполненное значение строки по сценарию и месяцу.
func expCellOf(r ExpRow, scenario string, month int) (float64, bool) {
	for _, v := range r.Values {
		if v.Scenario == scenario && v.Month == month && v.Amount != nil {
			return *v.Amount, true
		}
	}
	return 0, false
}

// WithRates подключает справочник курсов: пересчёт стран в BYN для свода расходов.
func (s *ExpService) WithRates(r *RateBook) *ExpService { s.rates = r; return s }

// expFx — курсы страны к BYN по назначению колонки; без справочника или в BYN — единица.
func (s *ExpService) expFx(ctx context.Context, nat string, year, month int) expFxToBYN {
	if nat == "" || nat == "BYN" {
		return sameExpFx(1)
	}
	prevY, prevM := prevMonth(year, month)
	if s.rates == nil || !s.rates.FxKnown(ctx, nat, prevY, prevM) {
		return expFxToBYN{NoRate: true}
	}
	conv := func(y, m int) float64 { return s.rates.ConvertFor(ctx, 1, nat, "BYN", y, m, FxScenarioFact) }
	return expFxToBYN{Tactic: conv(prevY, prevM), FactLY: conv(year-1, month), FactPrev: conv(prevY, prevM), FactCur: conv(year, month)}
}

// RetailConsolidated — свод расходов розницы по четырём странам периода.
func (s *ExpService) RetailConsolidated(ctx context.Context, p Principal, plID int64, window string) (ExpConsolidated, error) {
	cards, err := s.cards.Cards(ctx, plID)
	if err != nil {
		return ExpConsolidated{}, err
	}
	byCountry := map[string]Card{}
	for _, c := range cards {
		if c.FormCode != TemplateExp {
			continue
		}
		if kind, country := expKindOf(c.ScopeKey); kind == ExpKindRetail && strings.HasPrefix(c.ScopeKey, "R-") {
			byCountry[country] = c
		}
	}
	if len(byCountry) == 0 {
		return ExpConsolidated{}, expNoCardErr{"в периоде нет карточек расходов розницы"}
	}
	pnlByCode := map[int]string{}
	if m, err := s.pnlGroups(ctx); err == nil {
		pnlByCode = m
	}
	pnlByCode = expApplyDefaultPnlGroups(ExpKindRetail, pnlByCode)

	var (
		inputs  []expConsInput
		missing []string
		year    int
		month   int
		errs    = map[string]string{}
	)
	var loads []svodLoad
	for _, country := range retailCountries {
		c, ok := byCountry[country]
		if !ok {
			missing = append(missing, country)
			continue
		}
		year, month = c.Year, c.Month
		card, inst, err := s.access(ctx, c.ID)
		if err != nil {
			errs[country] = err.Error()
			continue
		}
		loads = append(loads, svodLoad{Label: country, Card: card, Inst: inst})
	}
	// Свод сам запускает загрузку из источника для открытых карточек (как открытие формы): за период, форму которого
	// не открывали, он иначе пуст.
	syncing, syncWarnings := s.loadForSvod(ctx, loads)
	var warnings []string
	warnings = append(warnings, syncWarnings...)
	for _, l := range loads {
		rows, err := expAllRows(ctx, s.repo, l.Inst.ID)
		if err != nil {
			errs[l.Label] = err.Error()
			continue
		}
		if len(rows) == 0 && !cardEditable(l.Card) {
			warnings = append(warnings, svodEmptyClosedWarning(l.Label))
		}
		inputs = append(inputs, expConsInput{Country: l.Label, Card: l.Card, Inst: l.Inst, Rows: rows, Fx: s.expFx(ctx, l.Inst.Currency, l.Card.Year, l.Card.Month)})
	}
	out := buildExpConsolidated(plID, year, month, window, inputs, pnlByCode, expRetailPnlOrder)
	out.Missing = missing
	out.Syncing = syncing
	out.Warnings = warnings
	// Страны без чтения (нет карточки, ошибка) остаются в списке с пояснением.
	have := map[string]bool{}
	for _, c := range out.Countries {
		have[c.Country] = true
	}
	var full []ExpConsCountry
	for _, country := range retailCountries {
		if have[country] {
			for _, c := range out.Countries {
				if c.Country == country {
					full = append(full, c)
				}
			}
			continue
		}
		row := ExpConsCountry{Country: country}
		if e, ok := errs[country]; ok {
			row.Error = e
		} else {
			row.Error = "карточка расходов страны не заведена"
		}
		full = append(full, row)
	}
	out.Countries = full
	for _, c := range inputs {
		if s.syncStatusWithBasis(ctx, c.Inst.ID).FactBasis == ExpFactBasisPayments {
			out.FactBasis = ExpFactBasisPayments
		}
	}
	return out, nil
}

// expNoCardErr — в периоде нет карточки канала. Отличается от ошибки чтения и от отказа в доступе: общий свод
// показывает такой канал нейтрально («нет карточки»), а не как сбой.
type expNoCardErr struct{ msg string }

func (e expNoCardErr) Error() string { return e.msg }

// WithScope подключает срез доступа: свод опта показывает суммы карточки, доступной не всем.
func (s *ExpService) WithScope(st ScopeStore) *ExpService { s.scope = st; return s }

// splitOptRows раскладывает строки карточки опта по странам их ЦФО (CountryCompany). Строки без
// известной страны попадают в отдельную колонку «—», чтобы итог не терял суммы.
func splitOptRows(rows []ExpRow) map[string][]ExpRow {
	out := map[string][]ExpRow{}
	known := map[string]bool{}
	for _, c := range expOptCountries {
		known[c] = true
	}
	for _, r := range rows {
		c := strings.ToUpper(strings.TrimSpace(r.CountryCompany))
		if !known[c] {
			c = "—"
		}
		out[c] = append(out[c], r)
	}
	return out
}

// ChannelConsolidated — свод расходов канала периода. Розница — по четырём карточкам стран;
// интернет-магазин — одна карточка IM-BY; опт — одна карточка OPT, колонки — страны ЦФО. ИМ и опт
// вводятся в BYN, пересчёта курсами нет.
func (s *ExpService) ChannelConsolidated(ctx context.Context, p Principal, plID int64, channel, window string) (ExpConsolidated, error) {
	var scopeKey string
	switch channel {
	case "", ExpChannelRetail:
		return s.RetailConsolidated(ctx, p, plID, window)
	case ExpChannelAll:
		return s.AllChannelsConsolidated(ctx, p, plID, window)
	case ExpChannelIM:
		scopeKey = "IM-BY"
	case ExpChannelOpt:
		scopeKey = "OPT"
	default:
		return ExpConsolidated{}, errors.New("неизвестный канал свода расходов")
	}
	cards, err := s.cards.Cards(ctx, plID)
	if err != nil {
		return ExpConsolidated{}, err
	}
	var card *Card
	for i := range cards {
		if cards[i].FormCode == TemplateExp && cards[i].ScopeKey == scopeKey {
			card = &cards[i]
			break
		}
	}
	if card == nil {
		return ExpConsolidated{}, expNoCardErr{"в периоде нет карточки расходов этого канала"}
	}
	if s.scope != nil {
		f, err := scopeFilterFor(ctx, s.scope, p)
		if err != nil {
			return ExpConsolidated{}, err
		}
		if !f.AllowsCard(*card) {
			return ExpConsolidated{}, errors.New("карточка вне вашего среза доступа")
		}
	}
	c, inst, err := s.access(ctx, card.ID)
	if err != nil {
		return ExpConsolidated{}, err
	}
	label := "Интернет-магазин"
	if channel == ExpChannelOpt {
		label = "Опт"
	}
	syncing, warnings := s.loadForSvod(ctx, []svodLoad{{Label: label, Card: c, Inst: inst}})
	rows, err := expAllRows(ctx, s.repo, inst.ID)
	if err != nil {
		return ExpConsolidated{}, err
	}
	if len(rows) == 0 && !cardEditable(c) {
		warnings = append(warnings, svodEmptyClosedWarning(label))
	}
	pnlByCode := map[int]string{}
	if m, err := s.pnlGroups(ctx); err == nil {
		pnlByCode = m
	}
	pnlByCode = expApplyDefaultPnlGroups(ExpKindRetail, pnlByCode)

	fx := sameExpFx(1)
	var inputs []expConsInput
	if channel == ExpChannelIM {
		inputs = []expConsInput{{Country: "BY", Card: c, Inst: inst, Rows: rows, Fx: fx}}
	} else {
		by := splitOptRows(rows)
		for _, country := range append(append([]string{}, expOptCountries...), "—") {
			if country == "—" && len(by[country]) == 0 {
				continue
			}
			inputs = append(inputs, expConsInput{Country: country, Card: c, Inst: inst, Rows: by[country], Fx: fx})
		}
	}
	out := buildExpConsolidated(plID, c.Year, c.Month, window, inputs, pnlByCode, expRetailPnlOrder)
	out.Channel = channel
	out.Syncing = syncing
	out.Warnings = warnings
	if s.syncStatusWithBasis(ctx, inst.ID).FactBasis == ExpFactBasisPayments {
		out.FactBasis = ExpFactBasisPayments
	}
	return out, nil
}

// ConsolidatedGet — GET /api/plans/exp-consolidated/{plId}?window=month|tail|year&channel=retail|im|opt.
func (h *ExpHandler) ConsolidatedGet(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(w, r, "plId")
	if !ok {
		return
	}
	res, err := h.svc.ChannelConsolidated(r.Context(), h.principal(r), id, r.URL.Query().Get("channel"), r.URL.Query().Get("window"))
	if err != nil {
		expErr(w, err)
		return
	}
	writeJSON(w, http.StatusOK, res)
}
