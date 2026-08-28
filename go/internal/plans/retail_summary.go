package plans

import (
	"fmt"
	"sort"
)

// Экран согласования формы «Розница»: разрезы и показатели (ТЗ §8) + контрольные
// сверки согласующего (ТЗ §7). Чистые функции без БД — покрыты тестами
// (retail_summary_test.go).
//
// Почему сверки живут здесь, а не в валидациях: V-*/W-* отвечают на вопрос «можно
// ли отправить форму», а сверки §7 — на вопрос «сходится ли она с источниками».
// Согласующий смотрит именно на вторые, и все четыре ОБЯЗАНЫ давать 0; ненулевое
// значение отдаётся с расшифровкой по строкам, иначе искать расхождение на 375
// магазинах невозможно.

// Ключи обязательных разрезов ТЗ §8: «Розница <страна> → в т.ч. LFL / до года /
// новые / закрыты».
var retailSectionOrder = []struct {
	Key   string
	Title string
	Match func(RetailRow) bool
}{
	{"lfl", "в т.ч. LFL", func(r RetailRow) bool { return r.LFLEffective == LFLYes }},
	{"under1y", "в т.ч. до года", func(r RetailRow) bool { return r.LFLEffective == LFLUnder1 }},
	{"new", "в т.ч. новые", func(r RetailRow) bool {
		return r.LFLEffective == LFLNew || r.LFLEffective == LFLNoID
	}},
	{"closed", "в т.ч. закрыты", func(r RetailRow) bool {
		return r.LFLEffective == LFLClosed || r.DateClose != ""
	}},
}

// Переключаемые дополнительные группировки (ТЗ §8). Считаем все сразу: их пять,
// данные уже в памяти, и так UI переключает разрез без повторного запроса.
var retailGroupDims = []struct {
	Key   string
	Title string
	Value func(RetailRow) string
}{
	{"city", "Город", func(r RetailRow) string { return r.City }},
	{"reg_manager", "РМ", func(r RetailRow) string { return r.RegManager }},
	{"store_type", "Тип магазина", func(r RetailRow) string { return r.StoreType }},
	{"legal_entity", "ЮЛ", func(r RetailRow) string { return r.LegalEntity }},
	{"category", "Категория", func(r RetailRow) string { return r.Category }},
}

// RetailSummaryMetrics — 11 показателей месяца (ТЗ §8). Денежные — в валюте
// блока, процентные — отношения (от валюты не зависят).
type RetailSummaryMetrics struct {
	FactPrevYear   float64 `json:"fact_prev_year"`       // 1. факт M прошлого года
	Strategy       float64 `json:"strategy"`             // 2. план стратегический M
	Tactic         float64 `json:"tactic"`               // 3. план тактический M
	TacticVsStrat  float64 `json:"tactic_vs_strategy"`   // 4. % тактика/стратегия
	TacticVsPrevM  float64 `json:"tactic_vs_prev_month"` // 5. тактика M к факту M−1
	FactCur        float64 `json:"fact_cur"`             // 6. факт M текущего года
	LFL            float64 `json:"lfl"`                  // 7. LFL (тактика к факту ПГ − 1)
	TacticVsFactPY float64 `json:"tactic_vs_fact_py"`    // 8. % тактика/факт ПГ
	FactVsFactPY   float64 `json:"fact_vs_fact_py"`      // 9. % факт/факт ПГ
	DoneVsStrategy float64 `json:"done_vs_strategy"`     // 10. % выполнения (факт/стратегия)
	DoneVsTactic   float64 `json:"done_vs_tactic"`       // 11. % выполнения (факт/тактика)
	// Ниже — денежные итоги для сокращённых валютных блоков (ТЗ §8).
	PeriodTotal     float64 `json:"period_total"`
	YearExpectation float64 `json:"year_expectation"`
	FactPrevMonth   float64 `json:"fact_prev_month"`
}

// RetailSummarySection — один разрез: сколько строк и показатели.
type RetailSummarySection struct {
	Key     string               `json:"key"`
	Title   string               `json:"title"`
	Rows    int                  `json:"rows"`
	Metrics RetailSummaryMetrics `json:"metrics"`
}

// RetailSummaryGroup — дополнительная группировка (город/РМ/тип/ЮЛ/категория).
type RetailSummaryGroup struct {
	Key      string                 `json:"key"`
	Title    string                 `json:"title"`
	Sections []RetailSummarySection `json:"sections"`
}

// RetailCurrencyBlock — валютный блок (ТЗ §8: нац. валюта — все 11 показателей,
// BYN и USD — сокращённый набор из 6).
type RetailCurrencyBlock struct {
	Currency string `json:"currency"`
	// Full=true — показывать все 11 показателей; false — сокращённый набор.
	Full bool `json:"full"`
	// Fields — какие именно показатели показывать. Отдаём списком, а не «на
	// усмотрение фронта»: сокращённый набор — требование ТЗ, и его состав должен
	// быть один и тот же в UI, в экспорте и в тесте.
	Fields   []string               `json:"fields"`
	FxRate   float64                `json:"fx_rate"`
	Total    RetailSummaryMetrics   `json:"total"`
	Sections []RetailSummarySection `json:"sections"`
}

// retailFullFields — все 11 показателей ТЗ §8 в порядке вывода.
func retailFullFields() []string {
	return []string{
		"fact_prev_year", "strategy", "tactic", "tactic_vs_strategy", "tactic_vs_prev_month",
		"fact_cur", "lfl", "tactic_vs_fact_py", "fact_vs_fact_py", "done_vs_strategy", "done_vs_tactic",
	}
}

// retailShortFields — сокращённый набор из 6 для блоков BYN и USD (ТЗ §8).
//
// ТЗ состав шести не перечисляет. Берём ДЕНЕЖНЫЕ величины: пересчёт в BYN/USD —
// слой отображения (§4), и проценты в нём те же самые числа, что в нац. блоке, —
// дублировать их значит показать согласующему одно и то же трижды. Состав
// подтвердить у аналитика (см. отчёт: открытые вопросы).
func retailShortFields() []string {
	return []string{"fact_prev_year", "strategy", "tactic", "fact_cur", "period_total", "year_expectation"}
}

// RetailSummary — ответ GET /api/plans/retail/{cardId}/summary.
type RetailSummary struct {
	CardID          int64                  `json:"card_id"`
	Country         string                 `json:"country"`
	NatCurrency     string                 `json:"nat_currency"`
	Year            int                    `json:"year"`
	Month           int                    `json:"month"`
	Title           string                 `json:"title"`
	Rows            int                    `json:"rows"`
	Blocks          []RetailCurrencyBlock  `json:"blocks"`
	Groups          []RetailSummaryGroup   `json:"groups"`
	Reconciliations []RetailReconciliation `json:"reconciliations"`
}

// RetailReconciliation — контрольная сверка §7. OK=true ⇔ Diff = 0.
type RetailReconciliation struct {
	Code    string              `json:"code"`
	Title   string              `json:"title"`
	Left    float64             `json:"left"`
	Right   float64             `json:"right"`
	Diff    float64             `json:"diff"`
	OK      bool                `json:"ok"`
	Skipped bool                `json:"skipped,omitempty"` // источник недоступен
	Note    string              `json:"note,omitempty"`
	Details []RetailReconDetail `json:"details,omitempty"` // расшифровка по строкам
}

// RetailReconDetail — строка расшифровки ненулевой сверки.
type RetailReconDetail struct {
	CodeCFO int     `json:"code_cfo"`
	NameCFO string  `json:"cfo"`
	Left    float64 `json:"left"`
	Right   float64 `json:"right"`
	Diff    float64 `json:"diff"`
	Note    string  `json:"note,omitempty"`
}

// retailReconEps — допуск сверки. Значения хранятся с копейками (NUMERIC(18,2)),
// поэтому расхождение меньше копейки — это арифметика float, а не ошибка данных.
const retailReconEps = 0.01

// ComputeRetailSummary — экран согласования (ТЗ §8).
func ComputeRetailSummary(in RetailSummaryInput) RetailSummary {
	rows := ComputeRetailForm(in.Rows, in.Series, in.Year, in.Month, in.PlanMonths)
	out := RetailSummary{
		CardID:      in.CardID,
		Country:     in.Country,
		NatCurrency: in.NatCurrency,
		Year:        in.Year,
		Month:       in.Month,
		Title:       "Розница " + in.Country,
		Rows:        len(rows),
	}

	// Валютные блоки: нац. валюта (все 11) + BYN и USD (сокращённые), ТЗ §8.
	// Нац. валюта BY — BYN, поэтому дубль блока не создаём.
	blocks := []struct {
		cur  string
		full bool
	}{{in.NatCurrency, true}}
	for _, c := range []string{"BYN", "USD"} {
		if c != in.NatCurrency {
			blocks = append(blocks, struct {
				cur  string
				full bool
			}{c, false})
		}
	}
	for _, b := range blocks {
		rate := 1.0
		if r, ok := in.FxRates[b.cur]; ok && r != 0 {
			rate = r
		}
		block := RetailCurrencyBlock{Currency: b.cur, Full: b.full, FxRate: rate}
		if b.full {
			block.Fields = retailFullFields()
		} else {
			block.Fields = retailShortFields()
		}
		block.Total = scaleMetrics(aggregateMetrics(rows, in), rate)
		for _, sec := range retailSectionOrder {
			part := filterRows(rows, sec.Match)
			block.Sections = append(block.Sections, RetailSummarySection{
				Key: sec.Key, Title: sec.Title, Rows: len(part),
				Metrics: scaleMetrics(aggregateMetrics(part, in), rate),
			})
		}
		out.Blocks = append(out.Blocks, block)
	}

	// Дополнительные группировки — в нац. валюте: их назначение «где именно
	// расхождение», а для этого пересчёт в BYN/USD ничего не добавляет.
	for _, dim := range retailGroupDims {
		groups := map[string][]RetailRow{}
		for _, r := range rows {
			v := dim.Value(r)
			if v == "" {
				v = "(не указано)"
			}
			groups[v] = append(groups[v], r)
		}
		keys := make([]string, 0, len(groups))
		for k := range groups {
			keys = append(keys, k)
		}
		sort.Strings(keys)
		g := RetailSummaryGroup{Key: dim.Key, Title: dim.Title}
		for _, k := range keys {
			g.Sections = append(g.Sections, RetailSummarySection{
				Key: k, Title: k, Rows: len(groups[k]),
				Metrics: aggregateMetrics(groups[k], in),
			})
		}
		out.Groups = append(out.Groups, g)
	}

	out.Reconciliations = ComputeRetailReconciliations(rows, in)
	return out
}

// RetailSummaryInput — вход экрана согласования и сверок.
type RetailSummaryInput struct {
	CardID      int64
	Country     string
	NatCurrency string
	Year        int
	Month       int
	PlanMonths  []int
	Rows        []RetailRow
	Series      RetailSeries
	// FxRates — курс нац. валюты к валюте блока (сколько единиц валюты блока в
	// одной единице нац. валюты). Считается RateBook'ом снаружи.
	FxRates map[string]float64
	// SourceFactTotal — итог факта ИЗ ИСТОЧНИКА за период по всей стране
	// (сверка §7 №2). nil → источник недоступен, сверка помечается skipped:
	// «нет данных» и «расхождение 0» — разные ответы согласующему.
	SourceFactTotal *float64
	// DirectoryActiveCount — число активных CodeCFO в источнике (сверка §7 №4).
	// −1 → источник недоступен.
	DirectoryActiveCount int
}

// filterRows — подмножество строк по признаку разреза.
func filterRows(rows []RetailRow, match func(RetailRow) bool) []RetailRow {
	out := make([]RetailRow, 0, len(rows))
	for _, r := range rows {
		if match(r) {
			out = append(out, r)
		}
	}
	return out
}

// aggregateMetrics — 11 показателей §8 по набору строк. Проценты считаются ОТ
// АГРЕГАТОВ (Σ тактика / Σ стратегия), а не как среднее построчных процентов:
// среднее процентов по 191 магазину — величина без экономического смысла.
func aggregateMetrics(rows []RetailRow, in RetailSummaryInput) RetailSummaryMetrics {
	var m RetailSummaryMetrics
	for _, r := range rows {
		ind := r.Indicators
		m.FactPrevYear += deref(ind.FactPrevYearMonth)
		m.Strategy += deref(ind.Strategy)
		m.Tactic += deref(ind.Tactic)
		m.FactCur += deref(ind.FactCurMonth)
		m.FactPrevMonth += deref(ind.FactPrevMonth)
		m.PeriodTotal += ind.PeriodTotal
		m.YearExpectation += ind.YearExpectation
	}
	m.TacticVsStrat = iferrorDiv(m.Tactic, m.Strategy)
	m.TacticVsPrevM = iferrorDiv(m.Tactic, m.FactPrevMonth)
	m.LFL = iferrorRatioMinus1(m.Tactic, m.FactPrevYear)
	m.TacticVsFactPY = iferrorDiv(m.Tactic, m.FactPrevYear)
	m.FactVsFactPY = iferrorDiv(m.FactCur, m.FactPrevYear)
	m.DoneVsStrategy = iferrorDiv(m.FactCur, m.Strategy)
	m.DoneVsTactic = iferrorDiv(m.FactCur, m.Tactic)
	return m
}

// scaleMetrics — пересчёт денежных показателей в валюту блока. Проценты НЕ
// пересчитываются (ТЗ §4: «индикаторы на слое отображения не пересчитываются»).
func scaleMetrics(m RetailSummaryMetrics, rate float64) RetailSummaryMetrics {
	if rate == 1 || rate == 0 {
		return m
	}
	m.FactPrevYear *= rate
	m.Strategy *= rate
	m.Tactic *= rate
	m.FactCur *= rate
	m.FactPrevMonth *= rate
	m.PeriodTotal *= rate
	m.YearExpectation *= rate
	return m
}

// ComputeRetailReconciliations — четыре контрольные сверки согласующего (ТЗ §7).
// Все должны давать 0; ненулевые отдаются с расшифровкой по строкам.
func ComputeRetailReconciliations(rows []RetailRow, in RetailSummaryInput) []RetailReconciliation {
	out := make([]RetailReconciliation, 0, 4)

	// §7 №1: свод тактики = сумма строк. Свод берём из агрегата показателей §8
	// (то, что видит согласующий), сумму строк — прямым обходом ячеек месяца.
	agg := aggregateMetrics(rows, in)
	sumRows := 0.0
	for _, r := range rows {
		if v := r.valueOf(in.Year, in.Month); v != nil {
			sumRows += *v
		}
	}
	out = append(out, recon("R-01", "Свод тактики = сумма строк формы", agg.Tactic, sumRows, nil))

	// §7 №2: свод по ЦФО = итог факта из источника за тот же период.
	if in.SourceFactTotal == nil {
		out = append(out, RetailReconciliation{
			Code: "R-02", Title: "Свод факта по ЦФО = итог источника", Skipped: true,
			Note: "источник факта недоступен — сверку выполнить нельзя (это не «расхождение 0»)",
		})
	} else {
		out = append(out, recon("R-02", "Свод факта по ЦФО = итог источника", agg.FactCur, *in.SourceFactTotal, nil))
	}

	// §7 №3: свод = данные мастер-представления. Мастер-представление экрана
	// согласования — разбивка «в т.ч. LFL / до года / новые / закрыты» (§8):
	// она обязана покрывать все строки без пересечений, иначе итог сходится, а
	// разрезы — нет, и согласующий видит противоречивые числа.
	sections := 0.0
	covered := map[int]int{}
	for _, sec := range retailSectionOrder {
		part := filterRows(rows, sec.Match)
		sections += aggregateMetrics(part, in).Tactic
		for _, r := range part {
			covered[r.CodeCFO]++
		}
	}
	details := make([]RetailReconDetail, 0)
	for _, r := range rows {
		switch covered[r.CodeCFO] {
		case 1:
			// норма
		case 0:
			details = append(details, RetailReconDetail{
				CodeCFO: r.CodeCFO, NameCFO: r.NameCFO, Left: deref(r.Indicators.Tactic),
				Note: fmt.Sprintf("LFL-статус %q не попал ни в один разрез", r.LFLEffective),
			})
		default:
			details = append(details, RetailReconDetail{
				CodeCFO: r.CodeCFO, NameCFO: r.NameCFO, Left: deref(r.Indicators.Tactic),
				Note: fmt.Sprintf("строка учтена в %d разрезах", covered[r.CodeCFO]),
			})
		}
	}
	out = append(out, recon("R-03", "Свод = данные мастер-представления (разрезы §8)", agg.Tactic, sections, details))

	// §7 №4: полнота справочника = (активных CodeCFO в источнике) − (строк формы).
	if in.DirectoryActiveCount < 0 {
		out = append(out, RetailReconciliation{
			Code: "R-04", Title: "Полнота справочника магазинов", Skipped: true,
			Note: "справочник магазинов недоступен — число активных CodeCFO неизвестно",
		})
	} else {
		out = append(out, recon("R-04", "Полнота справочника магазинов",
			float64(in.DirectoryActiveCount), float64(len(rows)), nil))
	}
	return out
}

// recon — сверка с допуском в копейку.
func recon(code, title string, left, right float64, details []RetailReconDetail) RetailReconciliation {
	diff := left - right
	r := RetailReconciliation{
		Code: code, Title: title, Left: left, Right: right,
		Diff: diff, OK: absf(diff) < retailReconEps,
	}
	if !r.OK || len(details) > 0 {
		r.Details = details
	}
	return r
}

func deref(p *float64) float64 {
	if p == nil {
		return 0
	}
	return *p
}
