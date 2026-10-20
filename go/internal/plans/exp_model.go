package plans

import (
	"sort"
	"time"
)

// Форма этапа 1.5 «Расходы центров прибыли» (TPL-CFO-EXP).
// ТЗ: docs/reports/plans/tz-stage-1.5-2026-09-09.md, план реализации:
// docs/reports/plans/stage-1.5-plan.md.
//
// Здесь только данные и чистые функции над ними: ни БД, ни HTTP. Хранилище —
// exp_store.go, сценарии — exp_service.go, источник строк — exp_source.go.

// TemplateExp — код формы. Совпадает с шаблоном задания этапа 1.5 из миграции
// 0027, поэтому у задания «Затраты ЦП» появляется кнопка «Форма».
const TemplateExp = "TPL-CFO-EXP"

// Вид формы: отбор строк из источника у них разный (ТЗ §2.1), оболочка общая.
const (
	ExpKindRetail = "retail" // 1.5-Р: Country='BY' и GroupCFO1='3.Магазины'
	ExpKindMP     = "mp"     // 1.5-МП: GroupCFO1='4.Маркетплейсы', страна по форме
	// ExpKindIM — интернет-магазин: GroupCFO1='5.Интернет-магазин' (лист «ИМ» файла PL 2025-2026).
	ExpKindIM = "im"
	// ExpKindOpt — опт: отбор по GroupCFO3 'ОПТ РБ' / 'ОПТ РФ' / 'ОПТ УЗ' (лист «ОПТ»), группа ЦФО любая.
	ExpKindOpt = "opt"
)

// Сценарии колонок формы.
//
// В эталоне их ТРИ, а не два (ответ Н1 от 10.09.2026): «Факт», «Стратегический
// бюджет» и «Тактика бюджет (таргеты)». Этап 1.5 заполняет третий — то есть
// ввод формы это ТАКТИКА, и публиковаться он должен под её сценарием, а не под
// сценарием стратегии. Стратегия планового года лежит рядом как ориентир: с ней
// сверяются при заполнении, менять её форма не может.
const (
	ExpScenarioPlan     = "plan"     // ввод формы = «Тактика бюджет (таргеты)»
	ExpScenarioFact     = "fact"     // факт прошлого года, read-only
	ExpScenarioStrategy = "strategy" // «Стратегический бюджет» планового года, read-only
	// ExpScenarioFactCur — факт ПЛАНОВОГО года (идёт на экран как «прошлый месяц» и «текущий
	// месяц» рядом с вводом), read-only. Факт прошлого года остаётся в ExpScenarioFact.
	ExpScenarioFactCur = "fact_cur"
)

// ExpScenarioReadOnly — сценарий не редактируется формой.
func ExpScenarioReadOnly(scenario string) bool {
	return scenario == ExpScenarioFact || scenario == ExpScenarioStrategy || scenario == ExpScenarioFactCur
}

// Источник значения ячейки — тот же словарь, что у розницы (tp_value.source),
// чтобы отчёты и журнал читались одинаково в обеих формах.
const (
	ExpSourceManual   = "manual"
	ExpSourceImport   = "import"
	ExpSourceSpread   = "spread"   // размножено на год / до конца года (ТЗ §7.8)
	ExpSourceRegistry = "registry" // подставлено реестром (ТЗ §5) — фаза Ф4
	ExpSourceCalc     = "calc"     // расчёт ТЗ §6 — фаза Ф5
	// ExpSourceBudget — подставлено из «Тактика бюджет (таргеты)» источника, как колонка «бюджет» листов
	// Excel: плановые месяцы там не вводятся, а читаются из данных. Правка человеком делает ячейку «manual».
	ExpSourceBudget = "budget"
)

// ExpInstance — экземпляр формы: одна карточка = одна страна одного вида формы.
type ExpInstance struct {
	ID          int64  `json:"id"`
	CardID      int64  `json:"card_id"`
	FormKind    string `json:"form_kind"`
	Country     string `json:"country"`
	PlanYear    int    `json:"plan_year"`
	Currency    string `json:"currency"`
	LegalEntity string `json:"legal_entity"`
}

// ExpValue — одна ячейка. Amount == nil означает «не заполнено»; ноль вводится
// явно (ТЗ §7.8) и от пустой ячейки отличается — на этом стоит валидация E-11.
type ExpValue struct {
	Scenario string   `json:"scenario"`
	Month    int      `json:"month"`
	Amount   *float64 `json:"amount"`
	Source   string   `json:"source"`
	Note     string   `json:"note,omitempty"`
}

// ExpRow — строка формы: пара «ЦФО × статья PL» с атрибутами и двумя дорожками
// значений (план и факт).
type ExpRow struct {
	ID      int64  `json:"id"`
	CodeCFO int    `json:"code_cfo"`
	CodePL  int    `json:"code_pl"`
	CFO     string `json:"cfo"`
	GroupPL string `json:"group_pl"`
	Expense string `json:"expense"`
	// GroupCFO2 — город у розницы, подгруппа у МП (ТЗ §7.1).
	GroupCFO1      string     `json:"group_cfo1"`
	GroupCFO2      string     `json:"group_cfo2"`
	LegalEntity    string     `json:"legal_entity"`
	CountryCompany string     `json:"country_company"`
	StoreType      string     `json:"store_type"`
	RegManager     string     `json:"reg_manager"`
	Comment        string     `json:"comment,omitempty"`
	Values         []ExpValue `json:"values"`
}

// PlanTotal — сумма плановых месяцев строки. Пустые ячейки в сумму не входят:
// пусто не ноль, и «итог 0» у незаполненной строки читался бы как заполненная.
func (r ExpRow) PlanTotal() float64 {
	var t float64
	for _, v := range r.Values {
		if v.Scenario == ExpScenarioPlan && v.Amount != nil {
			t += *v.Amount
		}
	}
	return t
}

// FilledMonths — сколько плановых месяцев заполнено (для E-11 и прогресса формы).
func (r ExpRow) FilledMonths() int {
	n := 0
	for _, v := range r.Values {
		if v.Scenario == ExpScenarioPlan && v.Amount != nil {
			n++
		}
	}
	return n
}

// ExpForm — форма целиком, как её отдаёт API.
type ExpForm struct {
	Instance ExpInstance `json:"instance"`
	Card     Card        `json:"card"`
	Rows     []ExpRow    `json:"rows"`
	// Total — сколько строк в форме всего (Rows может быть страницей, ТЗ §7.10).
	Total    int      `json:"total"`
	Offset   int      `json:"offset"`
	Groups   []string `json:"groups"` // порядок групп PL (ТЗ §7.3)
	ReadOnly bool     `json:"read_only"`
	// Sync — состояние загрузки состава строк из источника затрат. Процедура
	// отдаёт ~1 млн строк и работает минуты, поэтому загрузка идёт в фоне, а форма
	// открывается сразу с тем, что уже лежит в БД.
	Sync ExpSyncStatus `json:"sync"`
}

// Состояния загрузки из источника.
const (
	ExpSyncIdle    = "idle"    // не запускалась в этом процессе (или источник не подключён)
	ExpSyncRunning = "running" // идёт
	ExpSyncOK      = "ok"      // завершилась; SourceCells == 0 — источник ничего не вернул
	ExpSyncError   = "error"   // источник не прочитан, Error — причина
)

// ExpSyncStatus — состояние загрузки по экземпляру формы.
type ExpSyncStatus struct {
	State       string     `json:"state"`
	Error       string     `json:"error,omitempty"`
	SourceCells int        `json:"source_cells"` // ячеек в выдаче источника после отбора
	StartedAt   *time.Time `json:"started_at,omitempty"`
	FinishedAt  *time.Time `json:"finished_at,omitempty"`
	// SourceKind — откуда прочитаны данные: clickhouse (ночной снимок) | olap (процедура
	// напрямую); SourceAsOf — когда снимок залит (только для clickhouse).
	// Warning — загрузка прошла, но что-то не удалось (перезаливка снимка по кнопке): форма
	// показана по тому, что есть, а причина видна на экране.
	Warning    string     `json:"warning,omitempty"`
	// FactBasis — на чём построен факт рядом с вводом: пусто — по начислениям (ТЗ), "payments" —
	// по платежам (начислений в выдаче источника нет); экран подписывает колонки честно.
	FactBasis  string     `json:"fact_basis,omitempty"`
	SourceKind string     `json:"source_kind,omitempty"`
	SourceAsOf *time.Time `json:"source_as_of,omitempty"`
}

// ExpCell — правка одной ячейки (вход сохранения).
type ExpCell struct {
	CodeCFO int      `json:"code_cfo"`
	CodePL  int      `json:"code_pl"`
	Month   int      `json:"month"`
	Amount  *float64 `json:"amount"`
	Source  string   `json:"source"`
	Note    string   `json:"note"`
}

// ExpFilter — отбор строк (ТЗ §7.1) и порция (ТЗ §7.10).
type ExpFilter struct {
	Search      string
	GroupPL     string
	CodePL      int
	CodeCFO     int
	City        string
	LegalEntity string
	StoreType   string
	RegManager  string
	Offset      int
	Limit       int
	// Sort — колонка сортировки (ТЗ §7.3): атрибут или месяц вида "m07".
	// Пусто — порядок по умолчанию: группа PL, ЦФО, статья.
	Sort string
	// Desc — по убыванию. Три состояния ТЗ («возр./убыв./без сортировки»)
	// собираются на клиенте: пустой Sort и есть третье состояние.
	Desc bool
}

// expSortColumns — БЕЛЫЙ СПИСОК колонок сортировки. Имя приходит от клиента и
// попадает в ORDER BY, поэтому подстановка идёт только отсюда: собирать ORDER BY
// из внешней строки нельзя.
var expSortColumns = map[string]string{
	"code_cfo":     "code_cfo",
	"cfo":          "cfo",
	"code_pl":      "code_pl",
	"expense":      "expense",
	"group_pl":     "group_pl",
	"group_cfo2":   "group_cfo2",
	"legal_entity": "legal_entity",
	"store_type":   "store_type",
	"reg_manager":  "reg_manager",
}

// ExpSortMonth — номер месяца, если сортировка задана по месячной колонке
// ("m01"…"m12"). Второй результат false — сортировка не по месяцу.
func ExpSortMonth(sort string) (int, bool) {
	if len(sort) != 3 || sort[0] != 'm' {
		return 0, false
	}
	m := int(sort[1]-'0')*10 + int(sort[2]-'0')
	if m < 1 || m > 12 {
		return 0, false
	}
	return m, true
}

// expGroupOrderRetail — порядок групп PL формы 1.5-Р (ТЗ §7.3, четырнадцать групп).
// Порядок задаётся ТЗ, а не алфавитом: в эталоне он именно такой.
var expGroupOrderRetail = []string{
	"ВЫПЛАТЫ РАБОТНИКАМ", "ВСПОМОГАТЕЛЬНЫЕ РАСХОДЫ", "ПРОЧИЕ РАСХОДЫ", "ВЛОЖЕНИЯ",
	"АМОРТИЗАЦИЯ", "ОСНОВНЫЕ РАСХОДЫ", "ПРОЧИЕ ДОХОДЫ", "АРЕНДА", "НАЛОГИ",
	"БЕЗВОЗМЕЗДНАЯ ПОМОЩЬ", "РЕКЛАМА И МАРКЕТИНГ", "РЕЗУЛЬТАТЫ ИНВЕНТАРИЗАЦИИ",
	"ПРОЦЕНТЫ (Депозиты, вложения, ЦБ)", "ПРОЦЕНТЫ (займы, кредиты)",
}

// expGroupOrderMP — порядок групп PL формы 1.5-МП (ТЗ §7.3, восемь групп).
var expGroupOrderMP = []string{
	"ВЫПЛАТЫ РАБОТНИКАМ", "ВСПОМОГАТЕЛЬНЫЕ РАСХОДЫ", "ПРОЧИЕ РАСХОДЫ", "ВЛОЖЕНИЯ",
	"АМОРТИЗАЦИЯ", "ОСНОВНЫЕ РАСХОДЫ", "АРЕНДА", "РЕКЛАМА И МАРКЕТИНГ",
}

// ExpGroupOrder — порядок групп PL по виду формы.
func ExpGroupOrder(kind string) []string {
	if kind == ExpKindMP {
		return expGroupOrderMP
	}
	return expGroupOrderRetail
}

// expGroupRank — позиция группы в заданном порядке. Группа, которой в порядке
// нет, уходит в конец: источник живой, и новая группа не должна ронять сортировку.
func expGroupRank(order []string, group string) int {
	for i, g := range order {
		if g == group {
			return i
		}
	}
	return len(order)
}

// SortExpRows — порядок строк по перечню ТЗ §7.3 из кода.
func SortExpRows(kind string, rows []ExpRow) { SortExpRowsBy(ExpGroupOrder(kind), rows) }

// SortExpRowsBy — порядок строк формы: группа PL в порядке справочника
// (ответ 36), внутри — ЦФО, затем статья.
func SortExpRowsBy(order []string, rows []ExpRow) {
	sort.SliceStable(rows, func(i, j int) bool {
		ri, rj := expGroupRank(order, rows[i].GroupPL), expGroupRank(order, rows[j].GroupPL)
		if ri != rj {
			return ri < rj
		}
		if rows[i].GroupPL != rows[j].GroupPL {
			return rows[i].GroupPL < rows[j].GroupPL
		}
		if rows[i].CodeCFO != rows[j].CodeCFO {
			return rows[i].CodeCFO < rows[j].CodeCFO
		}
		return rows[i].CodePL < rows[j].CodePL
	})
}
