package plans

import (
	"errors"
	"fmt"
	"sort"
	"strings"
)

// Массовые операции формы «Розница» (ТЗ §5). Чистые функции: на входе строки,
// срезы и параметры — на выходе список изменений «было → будет». Запись в БД
// делает retail_service.go, поэтому весь §5 покрыт тестами без БД
// (retail_bulk_test.go).
//
// Три инварианта ТЗ §5, ради которых операции сделаны именно так:
//
//  1. ПРЕДПРОСМОТР У ВСЕХ. Любая операция сначала считает diff и счётчики и
//     только потом (при Preview=false) пишет. Функции ниже НИЧЕГО не пишут —
//     они возвращают diff, и он же применяется. Значит предпросмотр и применение
//     физически не могут разойтись.
//
//  2. MANUAL НЕ ПЕРЕЗАТИРАЕТСЯ. Ячейка с source=manual — это решение человека;
//     повторный прогон операции её не трогает и считает в ProtectedManual.
//     Сброс — ЯВНОЕ действие (ResetManual=true), а не побочный эффект.
//
//  3. РЕЗУЛЬТАТ — ОБЫЧНЫЕ ЗНАЧЕНИЯ. Операция кладёт число с source-признаком
//     (index_applied, distributed, …), а не формулу: дальше его можно править
//     руками, и правка станет manual.

// Коды массовых операций (ТЗ §5).
const (
	BulkCopyScenario = "copy_scenario"
	BulkDistribute   = "distribute"
	BulkSalesIndex   = "sales_index"
	BulkPayroll      = "payroll"
	BulkRent         = "rent"
)

// Области применения (ТЗ §5: «вся выборка / отфильтрованные строки / выделенные»).
const (
	BulkScopeAll      = "all"
	BulkScopeFiltered = "filtered"
	BulkScopeSelected = "selected"
)

// Источники копирования сценария (ТЗ §5).
const (
	CopyFromStrategy = "strategy"
	CopyFromTactic   = "tactic"
	CopyFromFact     = "fact"
)

// Базы индекса роста продаж (ТЗ §5). FactPrevMonth — дефолт по ТЗ.
const (
	IndexBaseFactPrevMonth = "fact_prev_month"
	IndexBaseFactPrevYear  = "fact_prev_year"
	IndexBaseApprovedPrev  = "approved_prev"
	IndexBaseStrategy      = "strategy"
)

// RetailBulkRequest — запрос массовой операции.
type RetailBulkRequest struct {
	Op      string `json:"op"`
	Scope   string `json:"scope"`   // all|filtered|selected
	Preview bool   `json:"preview"` // true → ничего не сохранять
	// CodeCFOs — строки области применения для filtered/selected. Для all не нужен.
	CodeCFOs []int `json:"code_cfos"`
	// Months — целевые месяцы. Пусто → все плановые месяцы экземпляра.
	Months []int `json:"months"`
	// ResetManual — явный сброс защиты ручных значений (ТЗ §5).
	ResetManual bool `json:"reset_manual"`

	// --- copy_scenario ---
	Source       string  `json:"source"`        // strategy|tactic|fact
	SourceYear   int     `json:"source_year"`   // год источника
	SourceMonth  int     `json:"source_month"`  // 0 → месяц-в-месяц
	Coefficient  float64 `json:"coefficient"`   // × k (0 → не применяется)
	PercentDelta float64 `json:"percent_delta"` // + x % (0.1 = +10 %)

	// --- distribute («Методика Е.В.») ---
	TargetTotal float64 `json:"target_total"` // целевой итог на область применения
	BaseMonths  []int   `json:"base_months"`  // месяцы факта, по которым берётся база

	// --- sales_index ---
	IndexBase string `json:"index_base"` // fact_prev_month (дефолт)|fact_prev_year|approved_prev|strategy

	// --- payroll ---
	// PayrollBase — база для ограничения ФОТ. ТЗ §5 её не называет, поэтому
	// выбирается явно; дефолт — стратегия (плановая величина того же месяца).
	PayrollBase string `json:"payroll_base"` // strategy|fact_prev_year|approved_prev
}

// RetailBulkDiff — одно изменение «было → будет» (ТЗ §5: предпросмотр).
type RetailBulkDiff struct {
	CodeCFO      int      `json:"code_cfo"`
	NameCFO      string   `json:"cfo"`
	Metric       string   `json:"metric"`
	Year         int      `json:"year"`
	Month        int      `json:"month"`
	Before       *float64 `json:"before"` // nil — ячейка была пустой
	After        float64  `json:"after"`
	BeforeSource string   `json:"before_source,omitempty"`
	AfterSource  string   `json:"after_source"`
	Note         string   `json:"note,omitempty"`
}

// RetailBulkResult — итог операции (и предпросмотра, и применения).
type RetailBulkResult struct {
	Op        string `json:"op"`
	Scope     string `json:"scope"`
	Preview   bool   `json:"preview"`
	RowsIn    int    `json:"rows_in"`          // строк в области применения
	Changed   int    `json:"changed"`          // ячеек изменится/изменено
	Protected int    `json:"protected_manual"` // ячеек защищено source=manual
	// SkippedNoBase — ячеек пропущено из-за отсутствия базы: индекс не
	// применяется к «новый»/«ххх» (ТЗ §5), у распределения нулевая база и т.п.
	SkippedNoBase int              `json:"skipped_no_base"`
	Diff          []RetailBulkDiff `json:"diff"`
	Notes         []string         `json:"notes,omitempty"`
}

// RetailBulkContext — данные, нужные операциям (собирает сервис).
type RetailBulkContext struct {
	Country    string
	Year       int
	Month      int
	PlanMonths []int
	Rows       []RetailRow
	Series     RetailSeries
	Params     RetailParamSet
	// RentPrevFact — факт аренды предыдущего периода по магазинам (ТЗ §5, этап 1:
	// «фикс = факт аренды предыдущего периода»). Отдельного источника аренды в
	// ТЗ §11 нет, поэтому берётся из значений metric='rent' предыдущего
	// экземпляра; пусто → операция ставит пометку, а не молча ноль.
	RentPrevFact map[int]float64
}

// ApplyRetailBulk — расчёт массовой операции. Возвращает diff и счётчики; НИЧЕГО
// не пишет. Один и тот же вызов обслуживает и preview, и применение — так
// предпросмотр гарантированно совпадает с результатом (ТЗ §5).
func ApplyRetailBulk(req RetailBulkRequest, ctx RetailBulkContext) (RetailBulkResult, error) {
	res := RetailBulkResult{Op: req.Op, Scope: req.Scope, Preview: req.Preview, Diff: []RetailBulkDiff{}}

	rows, err := bulkScopeRows(req, ctx.Rows)
	if err != nil {
		return res, err
	}
	res.RowsIn = len(rows)
	months := req.Months
	if len(months) == 0 {
		months = ctx.PlanMonths
	}
	if len(months) == 0 {
		return res, errors.New("не заданы целевые месяцы операции")
	}

	switch req.Op {
	case BulkCopyScenario:
		err = bulkCopyScenario(&res, req, ctx, rows, months)
	case BulkDistribute:
		err = bulkDistribute(&res, req, ctx, rows, months)
	case BulkSalesIndex:
		err = bulkSalesIndex(&res, req, ctx, rows, months)
	case BulkPayroll:
		err = bulkPayroll(&res, req, ctx, rows, months)
	case BulkRent:
		err = bulkRent(&res, req, ctx, rows, months)
	default:
		return res, fmt.Errorf("неизвестная массовая операция %q", req.Op)
	}
	if err != nil {
		return res, err
	}
	sortBulkDiff(res.Diff)
	res.Changed = len(res.Diff)
	return res, nil
}

// bulkScopeRows — строки области применения (ТЗ §5).
func bulkScopeRows(req RetailBulkRequest, all []RetailRow) ([]RetailRow, error) {
	switch req.Scope {
	case "", BulkScopeAll:
		return all, nil
	case BulkScopeFiltered, BulkScopeSelected:
		if len(req.CodeCFOs) == 0 {
			return nil, fmt.Errorf("для области %q нужен список магазинов (code_cfos)", req.Scope)
		}
		want := map[int]bool{}
		for _, c := range req.CodeCFOs {
			want[c] = true
		}
		out := make([]RetailRow, 0, len(want))
		for _, r := range all {
			if want[r.CodeCFO] {
				out = append(out, r)
			}
		}
		return out, nil
	}
	return nil, fmt.Errorf("неизвестная область применения %q", req.Scope)
}

// bulkSet — попытка записать значение в ячейку. Здесь реализован инвариант №2:
// source=manual не перезатирается без явного ResetManual, а защищённые ячейки
// считаются отдельно, чтобы пользователь видел, СКОЛЬКО его правок уцелело.
func bulkSet(res *RetailBulkResult, req RetailBulkRequest, row RetailRow,
	metric string, year, month int, value float64, source, note string) {
	before, existed := row.cellOfMetric(metric, year, month)
	if existed && before.Source == ValueManual && !req.ResetManual {
		res.Protected++
		return
	}
	var beforePtr *float64
	beforeSource := ""
	if existed {
		beforePtr, beforeSource = before.Amount, before.Source
	}
	// Значение не изменилось — в diff не попадает: предпросмотр должен показывать
	// то, что реально поменяется, а не всю сетку.
	if beforePtr != nil && *beforePtr == value && beforeSource == source && before.Note == note {
		return
	}
	res.Diff = append(res.Diff, RetailBulkDiff{
		CodeCFO: row.CodeCFO, NameCFO: row.NameCFO, Metric: metric,
		Year: year, Month: month, Before: beforePtr, After: value,
		BeforeSource: beforeSource, AfterSource: source, Note: note,
	})
}

// applyCoefficient — коэффициент операции: × k ИЛИ + x % (ТЗ §5).
func applyCoefficient(v float64, req RetailBulkRequest) float64 {
	if req.Coefficient != 0 {
		v *= req.Coefficient
	}
	if req.PercentDelta != 0 {
		v *= 1 + req.PercentDelta
	}
	return v
}

// bulkCopyScenario — «Копирование сценария» (ТЗ §5): источник = стратегия <год> |
// тактика <предыдущий период> | факт <период>, с коэффициентом.
func bulkCopyScenario(res *RetailBulkResult, req RetailBulkRequest, ctx RetailBulkContext,
	rows []RetailRow, months []int) error {
	var src map[RetailCellKey]float64
	var source string
	switch req.Source {
	case CopyFromStrategy:
		src, source = ctx.Series.Strategy, ValueFromStrategy
	case CopyFromTactic:
		src, source = ctx.Series.Approved, ValueFromPrevPer
	case CopyFromFact:
		src, source = ctx.Series.Fact, ValueFromPrevPer
	default:
		return fmt.Errorf("источник копирования должен быть %q, %q или %q", CopyFromStrategy, CopyFromTactic, CopyFromFact)
	}
	srcYear := req.SourceYear
	if srcYear == 0 {
		srcYear = ctx.Year
	}
	for _, row := range rows {
		for _, m := range months {
			srcMonth := m
			if req.SourceMonth != 0 {
				srcMonth = req.SourceMonth
			}
			base, ok := src[RetailCellKey{row.CodeCFO, srcYear, srcMonth}]
			if !ok {
				res.SkippedNoBase++
				continue
			}
			bulkSet(res, req, row, MetricSales, ctx.Year, m, applyCoefficient(base, req), source,
				fmt.Sprintf("копирование: %s %02d.%d", req.Source, srcMonth, srcYear))
		}
	}
	if res.SkippedNoBase > 0 {
		res.Notes = append(res.Notes,
			fmt.Sprintf("нет данных источника %q по %d ячейкам — они остались без изменений", req.Source, res.SkippedNoBase))
	}
	return nil
}

// bulkDistribute — «Распределение целевой суммы» («Методика Е.В.», ТЗ §5):
// доля строки = база строки / Σ база; план строки = доля × целевой итог.
// База — среднее по выбранным месяцам ФАКТА.
func bulkDistribute(res *RetailBulkResult, req RetailBulkRequest, ctx RetailBulkContext,
	rows []RetailRow, months []int) error {
	if req.TargetTotal == 0 {
		return errors.New("укажите целевой итог распределения")
	}
	baseMonths := req.BaseMonths
	if len(baseMonths) == 0 {
		return errors.New("укажите месяцы факта, по которым считается база распределения")
	}
	// База строки — среднее факта по выбранным месяцам. Усредняем по месяцам,
	// за которые факт есть: иначе магазин, открытый в середине базового окна,
	// получил бы заниженную долю.
	base := make(map[int]float64, len(rows))
	total := 0.0
	for _, row := range rows {
		sum, cnt := 0.0, 0
		for _, m := range baseMonths {
			if v, ok := retailFactAt(ctx.Series, row.CodeCFO, ctx.Year, m); ok {
				sum += v
				cnt++
			}
		}
		if cnt == 0 {
			res.SkippedNoBase++
			continue
		}
		b := sum / float64(cnt)
		base[row.CodeCFO] = b
		total += b
	}
	if total == 0 {
		return errors.New("сумма базы распределения равна нулю — распределять нечего")
	}
	// Целевой итог задан на ПЕРИОД целиком, поэтому делим его на число целевых
	// месяцев: иначе каждый месяц получил бы полный итог и «Итого за период»
	// оказалось бы в N раз больше заданного.
	perMonth := req.TargetTotal / float64(len(months))
	for _, row := range rows {
		b, ok := base[row.CodeCFO]
		if !ok {
			continue
		}
		share := b / total
		for _, m := range months {
			bulkSet(res, req, row, MetricSales, ctx.Year, m, share*perMonth, ValueDistributed,
				fmt.Sprintf("распределение: доля %.4f%% от целевого итога %.2f", share*100, req.TargetTotal))
		}
	}
	if res.SkippedNoBase > 0 {
		res.Notes = append(res.Notes,
			fmt.Sprintf("у %d магазинов нет факта за базовые месяцы — они исключены из распределения", res.SkippedNoBase))
	}
	return nil
}

// bulkSalesIndex — «Индекс роста продаж» (основной способ, ТЗ §5):
// Тактика[магазин,M] = База[магазин] × (1 + Индекс[магазин]).
//
// Индекс общий на страну, может быть отрицательным; переопределяется по городу /
// LFL-статусу / типу магазина / отдельному магазину — приоритет от частного к
// общему (RetailParamSet.Resolve). К магазинам «новый»/«ххх» индекс НЕ применяется.
func bulkSalesIndex(res *RetailBulkResult, req RetailBulkRequest, ctx RetailBulkContext,
	rows []RetailRow, months []int) error {
	baseKind := req.IndexBase
	if baseKind == "" {
		baseKind = IndexBaseFactPrevMonth // дефолт по ТЗ §5
	}
	noIndexRows := 0
	for _, row := range rows {
		// ТЗ §5: «к магазинам «новый»/«ххх» индекс не применяется» — базы роста нет.
		if retailNoBaseline(row.LFLEffective) {
			noIndexRows++
			continue
		}
		idx, ok := ctx.Params.Resolve(ParamSalesIndex, row, ctx.Country)
		if !ok {
			res.SkippedNoBase++
			continue
		}
		for _, m := range months {
			base, has := indexBaseValue(baseKind, ctx, row, m)
			if !has {
				res.SkippedNoBase++
				continue
			}
			bulkSet(res, req, row, MetricSales, ctx.Year, m, base*(1+idx), ValueIndexApplied,
				fmt.Sprintf("индекс %.2f%% на базу %s", idx*100, baseKind))
		}
	}
	if noIndexRows > 0 {
		res.Notes = append(res.Notes,
			fmt.Sprintf("%d магазинов со статусом «новый»/«ххх» пропущено: индекс роста к ним не применяется (ТЗ §5)", noIndexRows))
	}
	if res.SkippedNoBase > 0 {
		res.Notes = append(res.Notes,
			fmt.Sprintf("нет базы или индекса по %d ячейкам — они остались без изменений", res.SkippedNoBase))
	}
	return nil
}

// indexBaseValue — база индекса роста (ТЗ §5).
func indexBaseValue(kind string, ctx RetailBulkContext, row RetailRow, month int) (float64, bool) {
	switch kind {
	case IndexBaseFactPrevMonth:
		py, pm := retailPrevMonth(ctx.Year, month)
		v, ok := retailFactAt(ctx.Series, row.CodeCFO, py, pm)
		return v, ok
	case IndexBaseFactPrevYear:
		v, ok := retailFactAt(ctx.Series, row.CodeCFO, ctx.Year-1, month)
		return v, ok
	case IndexBaseApprovedPrev:
		v, ok := ctx.Series.Approved[RetailCellKey{row.CodeCFO, ctx.Year, month}]
		return v, ok
	case IndexBaseStrategy:
		v, ok := ctx.Series.Strategy[RetailCellKey{row.CodeCFO, ctx.Year, month}]
		return v, ok
	}
	return 0, false
}

// bulkPayroll — «ФОТ от продаж» (ТЗ §5):
// ФОТ[магазин,M] = MIN(План_продаж; База_для_ограничения × Порог) × Удельный_вес.
//
// Порог по умолчанию 106 % — «выше 106 % планы не выплачивают». Порог и удельный
// вес — ПАРАМЕТРЫ ПЕРИОДА, а не константы в формуле. При срабатывании порога
// строка помечается пояснением (ТЗ §5).
func bulkPayroll(res *RetailBulkResult, req RetailBulkRequest, ctx RetailBulkContext,
	rows []RetailRow, months []int) error {
	capped := 0
	for _, row := range rows {
		share, ok := ctx.Params.Resolve(ParamPayrollShare, row, ctx.Country)
		if !ok {
			res.SkippedNoBase++
			continue
		}
		threshold := ctx.Params.ResolveOr(ParamPayrollCap, row, ctx.Country, DefaultPayrollCap)
		for _, m := range months {
			plan := row.valueOf(ctx.Year, m)
			if plan == nil {
				res.SkippedNoBase++
				continue
			}
			limitBase, hasLimit := payrollLimitBase(req.PayrollBase, ctx, row, m)
			basis, note := *plan, ""
			if hasLimit {
				if limit := limitBase * threshold; limit < basis {
					basis = limit
					note = fmt.Sprintf("ФОТ ограничен порогом %.0f %%", threshold*100)
					capped++
				}
			}
			bulkSet(res, req, row, MetricPayroll, ctx.Year, m, basis*share, ValuePayrollShare, note)
		}
	}
	if capped > 0 {
		res.Notes = append(res.Notes, fmt.Sprintf("порог сработал по %d ячейкам", capped))
	}
	if res.SkippedNoBase > 0 {
		res.Notes = append(res.Notes,
			fmt.Sprintf("нет удельного веса ФОТ или плана продаж по %d ячейкам — ФОТ не посчитан", res.SkippedNoBase))
	}
	return nil
}

// payrollLimitBase — «База_для_ограничения» ФОТ. ТЗ §5 её источник не называет,
// поэтому он выбирается запросом; дефолт — стратегия того же месяца (плановая
// величина, с которой и сравнивают «выше 106 %»). Открытый вопрос к финблоку.
func payrollLimitBase(kind string, ctx RetailBulkContext, row RetailRow, month int) (float64, bool) {
	switch kind {
	case "", IndexBaseStrategy:
		v, ok := ctx.Series.Strategy[RetailCellKey{row.CodeCFO, ctx.Year, month}]
		return v, ok
	case IndexBaseFactPrevYear:
		return retailFactAt(ctx.Series, row.CodeCFO, ctx.Year-1, month)
	case IndexBaseApprovedPrev:
		v, ok := ctx.Series.Approved[RetailCellKey{row.CodeCFO, ctx.Year, month}]
		return v, ok
	}
	return 0, false
}

// bulkRent — «Аренда» (ТЗ §5):
// Аренда = Фикс_часть + MAX(0; Выручка − Порог_оборота) × Ставка_оборотной_части.
//
// Этап 1: фикс-часть = факт аренды предыдущего периода. Если параметров по
// магазину нет — ставим ТОЛЬКО факт предыдущего периода и помечаем строку
// (ТЗ §5), а не считаем оборотную часть по нулевым ставкам.
func bulkRent(res *RetailBulkResult, req RetailBulkRequest, ctx RetailBulkContext,
	rows []RetailRow, months []int) error {
	noParams := 0
	for _, row := range rows {
		fixed, hasFixed := ctx.RentPrevFact[row.CodeCFO]
		threshold, hasThreshold := ctx.Params.Resolve(ParamRentThresh, row, ctx.Country)
		rate, hasRate := ctx.Params.Resolve(ParamRentRate, row, ctx.Country)
		if !hasFixed && !hasRate {
			res.SkippedNoBase++
			continue
		}
		for _, m := range months {
			value, note := fixed, ""
			if !hasRate || !hasThreshold {
				// Этап 1 без параметров оборотной части: только перенос факта.
				note = "только факт аренды предыдущего периода: ставка/порог оборотной части не заданы"
				noParams++
			} else {
				revenue := 0.0
				if plan := row.valueOf(ctx.Year, m); plan != nil {
					revenue = *plan
				}
				if over := revenue - threshold; over > 0 {
					value += over * rate
				}
			}
			bulkSet(res, req, row, MetricRent, ctx.Year, m, value, ValueRentCarryover, note)
		}
	}
	if noParams > 0 {
		res.Notes = append(res.Notes,
			fmt.Sprintf("%d ячеек посчитаны только по факту предыдущего периода (нет ставки/порога оборотной части)", noParams))
	}
	if res.SkippedNoBase > 0 {
		res.Notes = append(res.Notes,
			fmt.Sprintf("у %d магазинов нет ни факта аренды предыдущего периода, ни ставки — аренда не посчитана", res.SkippedNoBase))
	}
	return nil
}

// sortBulkDiff — стабильный порядок diff: магазин, метрика, месяц.
func sortBulkDiff(d []RetailBulkDiff) {
	sort.SliceStable(d, func(i, j int) bool {
		if d[i].CodeCFO != d[j].CodeCFO {
			return d[i].CodeCFO < d[j].CodeCFO
		}
		if d[i].Metric != d[j].Metric {
			return d[i].Metric < d[j].Metric
		}
		return d[i].Month < d[j].Month
	})
}

// ValidateBulkRequest — проверка запроса до расчёта (понятная ошибка вместо
// пустого diff).
func ValidateBulkRequest(req RetailBulkRequest) error {
	switch req.Op {
	case BulkCopyScenario, BulkDistribute, BulkSalesIndex, BulkPayroll, BulkRent:
	case "":
		return errors.New("не указана операция (op)")
	default:
		return fmt.Errorf("неизвестная массовая операция %q", req.Op)
	}
	switch req.Scope {
	case "", BulkScopeAll, BulkScopeFiltered, BulkScopeSelected:
	default:
		return fmt.Errorf("неизвестная область применения %q", req.Scope)
	}
	if req.Op == BulkSalesIndex && req.IndexBase != "" {
		switch req.IndexBase {
		case IndexBaseFactPrevMonth, IndexBaseFactPrevYear, IndexBaseApprovedPrev, IndexBaseStrategy:
		default:
			return fmt.Errorf("неизвестная база индекса %q", req.IndexBase)
		}
	}
	if req.Op == BulkCopyScenario && strings.TrimSpace(req.Source) == "" {
		return errors.New("для копирования сценария укажите источник (strategy|tactic|fact)")
	}
	return nil
}
