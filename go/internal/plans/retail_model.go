package plans

import (
	"sort"
	"strings"
	"time"
)

// Типы формы «Тактический план продаж, Розница» (TPL-TO-RETAIL).
//
// Экземпляр формы = (страна, период) — ТЗ Розница §1. Строка = магазин, ключ
// строки — CodeCFO (§2). Ввод — только месячные ячейки плана продаж в
// НАЦИОНАЛЬНОЙ валюте (§3); всё остальное на экране — расчёт (§4) или атрибут
// магазина из справочника (§2, read-only).
//
// Здесь только данные. Формулы — retail_calc.go, массовые операции —
// retail_bulk.go, валидации — retail_validate.go, срезы согласования —
// retail_summary.go. Такое деление сделано осознанно: перечисленные файлы не
// знают ни про БД, ни про HTTP, и покрыты табличными тестами без БД.

// Источник значения ячейки (ТЗ §5: «результат — обычные редактируемые значения
// с source-признаком»). ValueManual защищён от перезаписи массовой операцией.
const (
	ValueManual        = "manual"
	ValueFromStrategy  = "from_strategy"
	ValueFromPrevPer   = "from_prev_period"
	ValueIndexApplied  = "index_applied"
	ValuePayrollShare  = "payroll_share"
	ValueRentCarryover = "rent_carryover"
	ValueDistributed   = "distributed"
	ValueImport        = "import"
)

// Метрика ячейки. Ввод пользователя (ТЗ §3) и валидации V-01..V-11 — только по
// MetricSales; MetricPayroll/MetricRent наполняются массовыми операциями §5
// («ФОТ от продаж», «Аренда»), формулы которых читают план продаж как вход.
// Подробнее о причинах — комментарий к колонке tp_value.metric (миграция 0035).
const (
	MetricSales   = "sales"
	MetricPayroll = "payroll"
	MetricRent    = "rent"
)

// LFL-статусы магазина (ТЗ §2, §4.4, §6). Значения приходят из
// [001 CodeCFO].LfLStatus; здесь — канонические коды, к которым приводим строку
// источника (retailLFLStatus).
//
// Почему это важно: для «нового» и «ххх» ТЗ §4 ЗАПРЕЩАЕТ считать LFL и % вып.
// («базы сравнения нет»), а §6 отключает предупреждения W-01/W-02 для «нового»
// и «до года». Значит статус — не декоративная метка, а переключатель расчёта.
const (
	LFLYes    = "lfl"     // LFL — сравнимый магазин
	LFLUnder1 = "under1y" // до года
	LFLNew    = "new"     // новый
	LFLNoID   = "xxx"     // «ххх» — магазин есть, KLIENT_ID ещё нет (10 новых точек РБ)
	LFLClosed = "closed"  // закрыт
)

// Коды параметров периода (ТЗ §5, §10). Хранятся в tp_period_param, а не в коде,
// потому что §5 требует переопределения по городу / LFL / типу / магазину.
const (
	ParamSalesIndex   = "sales_index"
	ParamPayrollShare = "payroll_share"
	ParamPayrollCap   = "payroll_cap"
	ParamRentShare    = "rent_share"
	ParamRentThresh   = "rent_turnover_threshold"
	ParamRentRate     = "rent_rate"
	ParamLFLWarn      = "lfl_warn"
	ParamLFMWarn      = "lfm_warn"
	ParamStrategyWarn = "strategy_warn"
)

// Области действия параметра периода — от частного к общему (ТЗ §5:
// «приоритет от частного к общему»). Порядок в retailParamScopeOrder и есть
// порядок разрешения.
const (
	ParamScopeStore     = "store"
	ParamScopeStoreType = "store_type"
	ParamScopeLFL       = "lfl"
	ParamScopeCity      = "city"
	ParamScopeCountry   = "country"
)

// retailParamScopeOrder — порядок поиска параметра: первое совпадение выигрывает.
func retailParamScopeOrder() []string {
	return []string{ParamScopeStore, ParamScopeStoreType, ParamScopeLFL, ParamScopeCity, ParamScopeCountry}
}

// Дефолты параметров периода. Живут в коде как ПОСЛЕДНИЙ фолбэк: если параметр
// не задан ни на одном уровне, форма всё равно считается, а в отчёте валидаций
// видно, что использован дефолт.
const (
	// «Выше 106 % планы не выплачивают» (ТЗ §5, ФОТ от продаж).
	DefaultPayrollCap = 1.06
	// Пороги предупреждений W-01/W-02/W-04 (ТЗ §6): дефолт 30 %.
	DefaultLFLWarn      = 0.30
	DefaultLFMWarn      = 0.30
	DefaultStrategyWarn = 0.30
)

// RetailStore — магазин из справочника dir_retail_store (ТЗ §11, источник
// FinDWH.dbo.[001 CodeCFO] WHERE GroupCFO1='Магазины'). Имена полей — по
// колонкам источника, чтобы сверка с BI шла без словаря переименований.
type RetailStore struct {
	CodeCFO    int     `json:"code_cfo"`
	KlientID   string  `json:"klient_id"`
	NameCFO    string  `json:"cfo"`         // CFO — название магазина
	GroupCFO1  string  `json:"group_cfo1"`  // 'Магазины'
	City       string  `json:"city"`        // GroupCFO2
	Country    string  `json:"country"`     // Country
	CodeFOX    string  `json:"code_fox"`    // CodeFOX
	Ploschad   float64 `json:"ploschad"`    // метраж
	StoreType  string  `json:"store_type"`  // TypeOfStore
	DateOpen   string  `json:"date_open"`   // YYYY-MM-DD (пусто = неизвестно)
	DateClose  string  `json:"date_close"`  // YYYY-MM-DD (пусто = не закрыт)
	Stage      string  `json:"stage"`       // StadiyaOfStore
	CompanyMF  string  `json:"company_mf"`  // ЮЛ
	Channel    string  `json:"channel"`     // Channel
	CFOold     string  `json:"cfo_old"`     // CFOold
	Category   string  `json:"category"`    // Category
	LFLStatus  string  `json:"lfl_status"`  // LfLStatus — приводится к каноническим кодам
	RegManager string  `json:"reg_manager"` // RegManager (права РМ, §9)
	Manager    string  `json:"manager"`     // Manager
	PLAnalytic string  `json:"pl_analytic"` // PLAnalyticCFO1
}

// RetailRow — строка формы: снапшот атрибутов магазина + ячейки по месяцам.
type RetailRow struct {
	ID        int64  `json:"id"`
	CodeCFO   int    `json:"code_cfo"`
	KlientID  string `json:"klient_id"`
	NameCFO   string `json:"cfo"`
	City      string `json:"city"`
	LFLStatus string `json:"lfl_status"` // из снапшота
	// LFLEffective — статус, применённый в расчёте: переопределение финансиста
	// (tp_lfl_override) поверх снапшота (ТЗ §3). Справочник не перезатирается.
	LFLEffective string            `json:"lfl_effective"`
	LFLOverride  bool              `json:"lfl_override"`
	LFLReason    string            `json:"lfl_reason,omitempty"`
	StoreType    string            `json:"store_type"`
	Category     string            `json:"category"`
	RegManager   string            `json:"reg_manager"`
	LegalEntity  string            `json:"legal_entity"`
	Ploschad     float64           `json:"ploschad"`
	DateOpen     string            `json:"date_open,omitempty"`
	DateClose    string            `json:"date_close,omitempty"`
	Stage        string            `json:"stage,omitempty"`
	CodeFOX      string            `json:"code_fox,omitempty"`
	Manager      string            `json:"manager,omitempty"`
	PLAnalytic   string            `json:"pl_analytic,omitempty"`
	Comment      string            `json:"comment"`
	RowVersion   int               `json:"row_version"`
	Values       []RetailValue     `json:"values"`
	Indicators   RetailIndicators  `json:"indicators"`
	Warnings     []ValidationIssue `json:"warnings,omitempty"`
}

// RetailValue — ячейка плана (строка × месяц). Amount=nil — ПУСТО: ТЗ V-01
// требует различать «не заполнено» и «явный ноль», поэтому указатель, а не 0.
type RetailValue struct {
	Metric    string     `json:"metric"` // sales | payroll | rent
	Year      int        `json:"year"`
	Month     int        `json:"month"`
	Amount    *float64   `json:"amount"`
	Source    string     `json:"source"`
	Note      string     `json:"note,omitempty"`
	Editable  bool       `json:"editable"`
	UpdatedAt *time.Time `json:"updated_at,omitempty"`
}

// RetailIndicators — расчётные показатели строки (ТЗ §4). Все — в нац. валюте.
// Указатели там, где ТЗ требует «пусто» вместо нуля: для «нового» и «ххх»
// LFL тактич. и % вып. НЕ рассчитываются (базы сравнения нет).
type RetailIndicators struct {
	FactPrevYearMonth *float64 `json:"fact_prev_year_month"` // Факт[M, Y−1]
	FactPrevMonth     *float64 `json:"fact_prev_month"`      // Факт[M−1, Y] (для января — Факт[12, Y−1])
	FactCurMonth      *float64 `json:"fact_cur_month"`       // Факт[M, Y]
	Strategy          *float64 `json:"strategy"`             // Страт[M, Y]
	TacticApproved    *float64 `json:"tactic_approved"`      // ранее утверждённая тактика
	Tactic            *float64 `json:"tactic"`               // Такт[M, Y] — то, что вводят
	PlanDonePct       *float64 `json:"plan_done_pct"`        // % вып. тактич. плана
	LFLTactic         *float64 `json:"lfl_tactic"`           // LFL тактич.
	LFMTactic         *float64 `json:"lfm_tactic"`           // LFM тактич.
	VsStrategyPct     *float64 `json:"vs_strategy_pct"`      // % к стратегии
	PeriodTotal       float64  `json:"period_total"`         // Итого за период
	AvgMonthFact      *float64 `json:"avg_month_fact"`       // Ср. мес. факт
	YearExpectation   float64  `json:"year_expectation"`     // Ожидание года
	YearExpVsPrevPct  *float64 `json:"year_exp_vs_prev_pct"` // Откл. ожидания к факту ПГ
}

// RetailFactCell — факт/стратегия/история по (магазин, год, месяц). Плоская
// структура, потому что все четыре источника ТЗ §11 отдают именно такой срез.
type RetailFactCell struct {
	CodeCFO int     `json:"code_cfo"`
	Year    int     `json:"year"`
	Month   int     `json:"month"`
	Amount  float64 `json:"amount"`
}

// RetailSeries — набор срезов, на которых считаются индикаторы. Ключ —
// (code_cfo, year, month); значения в НАЦ. валюте страны экземпляра.
type RetailSeries struct {
	Fact     map[RetailCellKey]float64 // факт продаж (выручка с НДС)
	Strategy map[RetailCellKey]float64 // стратегия
	Approved map[RetailCellKey]float64 // ранее утверждённая тактика (история плана)
	// ClosedMonths — закрытые месяцы года ИЗ КАЛЕНДАРЯ (ТЗ §4.4: признак закрытого
	// месяца ТОЛЬКО из календаря, не «по наличию чисел»).
	ClosedMonths map[int]bool
}

// RetailCellKey — ключ среза.
type RetailCellKey struct {
	CodeCFO int
	Year    int
	Month   int
}

// NewRetailSeries — пустой набор срезов (готов к заполнению, без nil-мап).
func NewRetailSeries() RetailSeries {
	return RetailSeries{
		Fact:         map[RetailCellKey]float64{},
		Strategy:     map[RetailCellKey]float64{},
		Approved:     map[RetailCellKey]float64{},
		ClosedMonths: map[int]bool{},
	}
}

// Put — добавить срез (суммируя: источник может отдавать несколько строк на
// один месяц — например, разные параметры стратегии).
func (s RetailSeries) put(m map[RetailCellKey]float64, cells []RetailFactCell) {
	for _, c := range cells {
		m[RetailCellKey{c.CodeCFO, c.Year, c.Month}] += c.Amount
	}
}

// AddFact / AddStrategy / AddApproved — наполнение срезов.
func (s RetailSeries) AddFact(cells []RetailFactCell)     { s.put(s.Fact, cells) }
func (s RetailSeries) AddStrategy(cells []RetailFactCell) { s.put(s.Strategy, cells) }
func (s RetailSeries) AddApproved(cells []RetailFactCell) { s.put(s.Approved, cells) }

// RetailParam — параметр периода (ТЗ §5, §10).
type RetailParam struct {
	ID         int64     `json:"id"`
	ScopeKind  string    `json:"scope_kind"`
	ScopeValue string    `json:"scope_value"`
	ParamCode  string    `json:"param_code"`
	Value      float64   `json:"value"`
	Note       string    `json:"note,omitempty"`
	SetBy      int64     `json:"set_by,omitempty"`
	SetByName  string    `json:"set_by_name,omitempty"`
	SetAt      time.Time `json:"set_at"`
}

// RetailParamSet — параметры экземпляра с разрешением по приоритету.
type RetailParamSet struct {
	Params []RetailParam `json:"params"`
}

// Resolve — значение параметра для конкретной строки: от частного к общему
// (ТЗ §5). Второй результат — false, если параметр не задан ни на одном уровне
// (вызывающий подставляет дефолт и знает, что это дефолт).
func (ps RetailParamSet) Resolve(code string, row RetailRow, country string) (float64, bool) {
	for _, kind := range retailParamScopeOrder() {
		want := paramScopeValue(kind, row, country)
		if want == "" {
			continue
		}
		for _, p := range ps.Params {
			if p.ParamCode == code && p.ScopeKind == kind && strings.EqualFold(p.ScopeValue, want) {
				return p.Value, true
			}
		}
	}
	// Страновой параметр мог быть задан с пустым scope_value («на всю форму»).
	for _, p := range ps.Params {
		if p.ParamCode == code && p.ScopeKind == ParamScopeCountry && p.ScopeValue == "" {
			return p.Value, true
		}
	}
	return 0, false
}

// ResolveOr — значение параметра с дефолтом.
func (ps RetailParamSet) ResolveOr(code string, row RetailRow, country string, def float64) float64 {
	if v, ok := ps.Resolve(code, row, country); ok {
		return v
	}
	return def
}

// paramScopeValue — значение измерения строки для данной области параметра.
func paramScopeValue(kind string, row RetailRow, country string) string {
	switch kind {
	case ParamScopeStore:
		return itoa(int64(row.CodeCFO))
	case ParamScopeStoreType:
		return row.StoreType
	case ParamScopeLFL:
		return row.LFLEffective
	case ParamScopeCity:
		return row.City
	case ParamScopeCountry:
		return country
	}
	return ""
}

// RetailForm — ответ GET /api/plans/retail/{cardId}/form.
type RetailForm struct {
	CardID      int64             `json:"card_id"`
	InstanceID  int64             `json:"instance_id"`
	FormCode    string            `json:"form_code"`
	Country     string            `json:"country"`
	LegalEntity string            `json:"legal_entity"`
	Currency    string            `json:"currency"`     // валюта ОТОБРАЖЕНИЯ
	NatCurrency string            `json:"nat_currency"` // валюта ВВОДА (нац., V-11)
	Year        int               `json:"year"`
	Month       int               `json:"month"`
	Months      []int             `json:"months"` // плановые месяцы (ТЗ §4: период)
	Editable    bool              `json:"editable"`
	Status      string            `json:"status"`
	StepCode    string            `json:"step_code"`
	Rows        []RetailRow       `json:"rows"`
	Params      []RetailParam     `json:"params"`
	Warnings    []ValidationIssue `json:"warnings,omitempty"`
	// FxRate — курс валюты отображения к нац. валюте. Пересчёт в BYN/USD —
	// ОТДЕЛЬНЫЙ СЛОЙ ОТОБРАЖЕНИЯ (ТЗ §4): индикаторы на нём не пересчитываются,
	// поэтому курс отдаётся явно, а проценты остаются теми же числами.
	FxRate float64 `json:"fx_rate"`
}

// retailPlanMonths — плановые месяцы экземпляра. Форма ведёт план от месяца
// карточки до конца года: ТЗ §4 требует «Итого за период» и «Ожидание года»
// (Σ факт закрытых + Σ тактика остальных) — то есть горизонт всегда до декабря.
func retailPlanMonths(month int) []int {
	if month < 1 || month > 12 {
		month = 1
	}
	out := make([]int, 0, 13-month)
	for m := month; m <= 12; m++ {
		out = append(out, m)
	}
	return out
}

// retailLFLStatus — приведение LfLStatus источника к каноническому коду.
// Источник пишет статусы по-русски и непоследовательно («ххх» — латиницей и
// кириллицей), поэтому сравнение по подстроке, а не по равенству.
func retailLFLStatus(raw string) string {
	s := strings.ToLower(strings.TrimSpace(raw))
	switch {
	case s == "":
		return ""
	case strings.Contains(s, "закр") || strings.Contains(s, "closed"):
		return LFLClosed
	case s == "ххх" || s == "xxx" || strings.Contains(s, "ххх") || strings.Contains(s, "xxx"):
		return LFLNoID
	case strings.Contains(s, "нов") || strings.Contains(s, "new"):
		return LFLNew
	case strings.Contains(s, "до года") || strings.Contains(s, "до 1 года") || strings.Contains(s, "under"):
		return LFLUnder1
	case strings.Contains(s, "lfl") || strings.Contains(s, "лфл") || s == "да" || s == "yes":
		return LFLYes
	}
	return s
}

// retailNoBaseline — у магазина НЕТ базы сравнения: для «нового» и «ххх»
// ТЗ §4 запрещает считать LFL тактич. и % вып. тактич. плана.
func retailNoBaseline(lfl string) bool {
	return lfl == LFLNew || lfl == LFLNoID
}

// retailWarnExempt — для «нового» и «до года» предупреждения W-01/W-02 не
// применяются (ТЗ §6).
func retailWarnExempt(lfl string) bool {
	return lfl == LFLNew || lfl == LFLUnder1 || lfl == LFLNoID
}

// retailRegManagerExcluded — служебные значения RegManager закрытых точек
// (ТЗ §9: «закрытые точки со значением RegManager 'Closed'/'n/a' исключать»).
func retailRegManagerExcluded(rm string) bool {
	s := strings.ToLower(strings.TrimSpace(rm))
	return s == "" || s == "closed" || s == "n/a" || s == "na" || s == "-"
}

// retailStoreClosedBefore — магазин закрыт ДО начала месяца (V-03/V-09).
// Пустая DateClose = не закрыт. Формат даты источника — YYYY-MM-DD (приводится
// в retail_source.go); нераспознанная дата трактуется как «не закрыт», чтобы
// мусор в НСИ не выбрасывал живой магазин из формы молча — это ловит V-09.
func retailStoreClosedBefore(dateClose string, year, month int) bool {
	d, ok := parseRetailDate(dateClose)
	if !ok {
		return false
	}
	start := time.Date(year, time.Month(month), 1, 0, 0, 0, 0, time.UTC)
	return d.Before(start)
}

// retailStoreOpenedAfter — магазин открывается ПОЗЖЕ начала месяца (W-05:
// «план на месяцы до DateOpen»).
func retailStoreOpenedAfter(dateOpen string, year, month int) bool {
	d, ok := parseRetailDate(dateOpen)
	if !ok {
		return false
	}
	start := time.Date(year, time.Month(month), 1, 0, 0, 0, 0, time.UTC)
	return d.After(start)
}

// retailDateMinYear — граница «дата не задана». В [001 CodeCFO] отсутствие даты
// записано НЕ пустым значением и не NULL, а сентинелом 1900-01-01: так помечены
// 196 из 206 действующих магазинов РБ (и 134 из 201 по остальным странам). Без
// этой отсечки V-09 читает сентинел как «закрыт в 1900 году» и выбрасывает из
// формы весь справочник — на проде форма открывалась пустой («0 из 0 магазинов»).
// Граница безопасна: самый ранний реальный DateOpen в справочнике — 2014-06-01,
// а дат закрытия раньше 2000 года, кроме сентинела, нет вовсе. Заодно
// отсекается Excel-сентинел 1899-12-30.
const retailDateMinYear = 2000

// parseRetailDate — дата атрибута магазина. Принимаем и YYYY-MM-DD, и
// YYYY-MM-DDTHH:MM:SSZ: источник MSSQL отдаёт datetime, а снапшот — date.
// Сентинел «даты нет» (год < retailDateMinYear) — это не дата: ok=false.
func parseRetailDate(s string) (time.Time, bool) {
	s = strings.TrimSpace(s)
	if s == "" {
		return time.Time{}, false
	}
	if len(s) > 10 {
		s = s[:10]
	}
	d, err := time.Parse("2006-01-02", s)
	if err != nil {
		return time.Time{}, false
	}
	if d.Year() < retailDateMinYear {
		return time.Time{}, false
	}
	return d, true
}

// retailDateOrEmpty — дата атрибута магазина к виду YYYY-MM-DD; сентинел и
// мусор → пустая строка. Нормализуем НА ГРАНИЦЕ (источник, мок, чтение
// снапшота из БД), потому что «закрыт» проверяется не только через
// retailStoreClosedBefore, но и сравнением DateClose != "" (§8 разрезы, V-03):
// оставь сентинел в поле — и все действующие магазины уедут в «закрытые».
func retailDateOrEmpty(s string) string {
	d, ok := parseRetailDate(s)
	if !ok {
		return ""
	}
	return d.Format("2006-01-02")
}

// retailSnapshotDate — дата снапшота строки, прочитанная из tp_row. Строки,
// записанные до отсечки сентинела, хранят date_close = 1900-01-01 — читаем их
// как «не задано», иначе магазин остаётся «закрытым» до следующего синка.
func retailSnapshotDate(t *time.Time) string {
	if t == nil || t.Year() < retailDateMinYear {
		return ""
	}
	return t.Format("2006-01-02")
}

// retailPrevMonth — предыдущий месяц с переходом через год (ТЗ §4: «для января —
// Факт[12, Y−1]»).
func retailPrevMonth(year, month int) (int, int) {
	if month <= 1 {
		return year - 1, 12
	}
	return year, month - 1
}

// sortRetailRows — стабильный порядок строк: город, затем код ЦФО. Так форма
// открывается в том же виде, в котором её согласовывали.
func sortRetailRows(rows []RetailRow) {
	sort.SliceStable(rows, func(i, j int) bool {
		if rows[i].City != rows[j].City {
			return rows[i].City < rows[j].City
		}
		return rows[i].CodeCFO < rows[j].CodeCFO
	})
}

// valueOf — план ПРОДАЖ строки за месяц (nil, если пусто). Индикаторы §4 считаются
// по продажам, поэтому это дефолтный доступ; ФОТ/аренда — через valueOfMetric.
func (r RetailRow) valueOf(year, month int) *float64 {
	return r.valueOfMetric(MetricSales, year, month)
}

// valueOfMetric — значение конкретной метрики за месяц (nil, если пусто).
// Пустая metric в ячейке трактуется как sales: так читаются строки, пришедшие
// до появления колонки metric.
func (r RetailRow) valueOfMetric(metric string, year, month int) *float64 {
	c, ok := r.cellOfMetric(metric, year, month)
	if !ok {
		return nil
	}
	return c.Amount
}

// cellOf — ячейка продаж за месяц вместе с source (для защиты manual, ТЗ §5).
func (r RetailRow) cellOf(year, month int) (RetailValue, bool) {
	return r.cellOfMetric(MetricSales, year, month)
}

// cellOfMetric — ячейка метрики за месяц.
func (r RetailRow) cellOfMetric(metric string, year, month int) (RetailValue, bool) {
	for i := range r.Values {
		m := r.Values[i].Metric
		if m == "" {
			m = MetricSales
		}
		if m == metric && r.Values[i].Year == year && r.Values[i].Month == month {
			return r.Values[i], true
		}
	}
	return RetailValue{Metric: metric, Year: year, Month: month}, false
}

// fptr — указатель на значение (для индикаторов, где nil означает «пусто»).
func fptr(v float64) *float64 { return &v }
