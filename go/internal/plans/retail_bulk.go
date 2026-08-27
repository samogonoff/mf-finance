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

// Базы ограничения ФОТ (порог 106 %). ТЗ §5 базу не называл — это был открытый
// вопрос §12 п.12. Ответ финблока (27.08.2026): «Верхняя граница ФОТ 106 % от
// плана продаж на текущий месяц», поэтому дефолт — план текущего месяца, а
// прежние варианты остались как совместимость для закрытых периодов.
const PayrollBasePlanCurrent = "plan_current_month"

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
	// PayrollBase — база для ограничения ФОТ. Дефолт — план продаж текущего
	// месяца (ответ финблока §12 п.12); остальные варианты — совместимость.
	PayrollBase string `json:"payroll_base"` // plan_current_month|strategy|fact_prev_year|approved_prev

	// --- rent ---
	// RentTurnover — считать оборотную часть аренды. По ответу финблока
	// (§12 п.13) этап 1 — только факт аренды предыдущего месяца, поэтому
	// оборотная часть по умолчанию ВЫКЛЮЧЕНА и включается явно.
	RentTurnover bool `json:"rent_turnover"`
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
	// VatRate — эффективная ставка НДС страны из справочника dir_vat. Нужна ФОТ:
	// ввод формы — выручка С НДС, а удельный вес применяется к продажам БЕЗ НДС
	// (ответ финблока §12 п.12). 0 → берётся seed-ставка страны.
	VatRate float64
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
	// Ответ финблока (§12 п.11): для розницы индекс утверждается НА УРОВНЕ
	// СТРАНЫ, переопределения по городу/LFL/типу/магазину — только уточнение.
	// Работа на одних переопределениях — повод предупредить, а не запретить.
	if !ctx.Params.HasScope(ParamSalesIndex, ParamScopeCountry, ctx.Country) &&
		!ctx.Params.HasScope(ParamSalesIndex, ParamScopeCountry, "") {
		res.Notes = append(res.Notes,
			"страновое значение индекса не задано: по ответу финблока (§12 п.11) индекс роста утверждается на уровне страны, а переопределения — уточнение к нему")
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

// bulkPayroll — «ФОТ от продаж» (ТЗ §5 + ответ финблока §12 п.12 от 27.08.2026).
//
// ТЗ оставляло в формуле три неизвестных, и все три закрыты ответом финблока:
//  1. база порога 106 % — ПЛАН ПРОДАЖ ТЕКУЩЕГО МЕСЯЦА (не факт прошлого года);
//  2. удельный вес применяется к продажам БЕЗ НДС текущего периода — форма
//     вводит выручку С НДС, поэтому база делится на (1 + ставка НДС страны);
//  3. удельный вес задаётся ПО СТРАНЕ, а по магазинам «детализируется в
//     пропорции ср. факта за последний квартал».
//
// Отсюда расчёт в два шага на каждый месяц:
//
//	База  = MIN(Продажи_без_НДС(магазин, месяц); Порог × База_ограничения_без_НДС)
//	Фонд  = Σ уд.вес(магазин) × База(магазин)
//	Доля  = ср.факт_квартала(магазин) / Σ ср.факт_квартала
//	ФОТ   = Фонд × Доля
//
// Порог 106 % стоит на БАЗЕ НАЧИСЛЕНИЯ, как в формуле ТЗ §5
// (MIN(План_продаж; База × Порог)), а не на итоговой сумме ФОТ. Это важно:
// уд.вес — единицы процентов, поэтому порог на сумме ФОТ либо не сработал бы
// никогда (сравнение с оборотом), либо — с множителем уд.веса — зажал бы
// распределение в полосу ±6 % и обнулил бы саму «пропорцию ср. факта» из
// ответа финблока. С базой ограничения по умолчанию (план текущего месяца)
// порог не срабатывает — он и предназначен для случаев, когда база начисления
// не совпадает с планом (режимы совместимости: стратегия, факт прошлого года).
//
// Магазины без факта за последний закрытый квартал (новые точки) в фонд и в
// распределение не входят — им ФОТ считается напрямую от базы, с пометкой.
func bulkPayroll(res *RetailBulkResult, req RetailBulkRequest, ctx RetailBulkContext,
	rows []RetailRow, months []int) error {
	vat := retailVatRate(ctx)
	capped, direct := 0, 0

	// Пропорция детализации — одна на период (ср. факт последнего квартала),
	// а не своя на каждый месяц: детализируется удельный вес, а не сезонность.
	qbase := make(map[int]float64, len(rows))
	for _, row := range rows {
		qbase[row.CodeCFO] = payrollQuarterBase(ctx, row)
	}

	for _, m := range months {
		// Шаг 1: фонд страны на месяц и база распределения. В обоих участвуют
		// только строки с планом месяца И историей квартала.
		fund, baseTotal := 0.0, 0.0
		for _, row := range rows {
			share, hasShare := ctx.Params.Resolve(ParamPayrollShare, row, ctx.Country)
			base, hasBase, _ := payrollBase(req, ctx, row, m, vat)
			if !hasShare || !hasBase || qbase[row.CodeCFO] <= 0 {
				continue
			}
			fund += share * base
			baseTotal += qbase[row.CodeCFO]
		}

		// Шаг 2: раскладка фонда по магазинам в пропорции ср. факта квартала.
		for _, row := range rows {
			share, hasShare := ctx.Params.Resolve(ParamPayrollShare, row, ctx.Country)
			if !hasShare {
				res.SkippedNoBase++
				continue
			}
			base, hasBase, capNote := payrollBase(req, ctx, row, m, vat)
			if !hasBase {
				res.SkippedNoBase++
				continue
			}
			if capNote != "" {
				capped++
			}

			value, note := share*base, capNote
			if qbase[row.CodeCFO] > 0 && baseTotal > 0 {
				value = fund * qbase[row.CodeCFO] / baseTotal
			} else {
				direct++
				note = joinNotes(note, "нет факта за последний закрытый квартал: ФОТ посчитан напрямую от базы магазина")
			}
			bulkSet(res, req, row, MetricPayroll, ctx.Year, m, value, ValuePayrollShare, note)
		}
	}

	if len(res.Diff) > 0 {
		res.Notes = append(res.Notes, fmt.Sprintf(
			"удельный вес применён к продажам без НДС (ставка %.2f %%); фонд страны распределён пропорционально ср. факту за последний закрытый квартал (§12 п.12)",
			vat*100))
	}
	// Ответ финблока (§12 п.12): удельный вес устанавливается ПО СТРАНЕ. Как и с
	// индексом роста, работа на одних переопределениях допустима, но заметна.
	if len(res.Diff) > 0 &&
		!ctx.Params.HasScope(ParamPayrollShare, ParamScopeCountry, ctx.Country) &&
		!ctx.Params.HasScope(ParamPayrollShare, ParamScopeCountry, "") {
		res.Notes = append(res.Notes,
			"страновой удельный вес ФОТ не задан: по ответу финблока (§12 п.12) он устанавливается по каждой стране розницы, а переопределения — уточнение к нему")
	}
	if capped > 0 {
		res.Notes = append(res.Notes, fmt.Sprintf("порог базы начисления сработал по %d ячейкам", capped))
	}
	if direct > 0 {
		res.Notes = append(res.Notes, fmt.Sprintf(
			"%d ячеек посчитаны напрямую от базы магазина: нет факта за последний закрытый квартал", direct))
	}
	if res.SkippedNoBase > 0 {
		res.Notes = append(res.Notes,
			fmt.Sprintf("нет удельного веса ФОТ или плана продаж по %d ячейкам — ФОТ не посчитан", res.SkippedNoBase))
	}
	return nil
}

// payrollBase — база начисления ФОТ по магазину за месяц: продажи БЕЗ НДС с
// порогом ТЗ §5. Ввод формы — выручка С НДС (§11: эталон «Выручка с НДС»), а
// удельный вес по ответу финблока (§12 п.12) применяется к продажам без НДС.
// Третий результат — пояснение, если порог сработал (ТЗ §5 требует пометку).
func payrollBase(req RetailBulkRequest, ctx RetailBulkContext, row RetailRow,
	month int, vat float64) (float64, bool, string) {
	plan := row.valueOf(ctx.Year, month)
	if plan == nil {
		return 0, false, ""
	}
	base := *plan / (1 + vat)
	threshold := ctx.Params.ResolveOr(ParamPayrollCap, row, ctx.Country, DefaultPayrollCap)
	limitBase, hasLimit := payrollLimitBase(req.PayrollBase, ctx, row, month)
	if !hasLimit {
		return base, true, ""
	}
	if limit := threshold * limitBase / (1 + vat); limit < base {
		return limit, true, fmt.Sprintf("база ФОТ ограничена порогом %.0f %%", threshold*100)
	}
	return base, true, ""
}

// joinNotes — склейка пояснений к одной ячейке (порог + отсутствие истории).
func joinNotes(a, b string) string {
	switch {
	case a == "":
		return b
	case b == "":
		return a
	}
	return a + "; " + b
}

// retailVatRate — ставка НДС страны формы. Значение из справочника dir_vat
// подставляет сервис; если справочник недоступен, работаем на seed-ставках —
// форма не должна падать из-за НСИ (тот же принцип, что в RateBook).
func retailVatRate(ctx RetailBulkContext) float64 {
	if ctx.VatRate > 0 {
		return ctx.VatRate
	}
	return pickVat(VatSeed(), ctx.Country, 0)
}

// payrollQuarterBase — средний факт продаж магазина за последний закрытый
// квартал: база детализации удельного веса по магазинам (ответ §12 п.12).
// Закрытые месяцы берутся ТОЛЬКО из календаря (тот же инвариант, что у
// «ожидания года», ТЗ §4.4).
func payrollQuarterBase(ctx RetailBulkContext, row RetailRow) float64 {
	sum, n := 0.0, 0
	for _, ym := range retailLastClosedMonths(ctx, 3) {
		if v, ok := retailFactAt(ctx.Series, row.CodeCFO, ym[0], ym[1]); ok {
			sum += v
			n++
		}
	}
	if n == 0 {
		return 0
	}
	return sum / float64(n)
}

// retailLastClosedMonths — последние n закрытых месяцев перед периодом карточки.
// Месяцы прошлых лет закрыты по определению; месяцы года периода — только если
// календарь их закрыл. Ограничение по глубине не даёт уйти дальше прошлого года.
func retailLastClosedMonths(ctx RetailBulkContext, n int) [][2]int {
	out := make([][2]int, 0, n)
	y, m := ctx.Year, ctx.Month
	for i := 0; i < 24 && len(out) < n; i++ {
		y, m = retailPrevMonth(y, m)
		if y == ctx.Year && !ctx.Series.ClosedMonths[m] {
			continue
		}
		out = append(out, [2]int{y, m})
	}
	return out
}

// payrollLimitBase — «База_для_ограничения» ФОТ. По ответу финблока (§12 п.12)
// дефолт — план продаж ТЕКУЩЕГО месяца; прежние варианты оставлены для
// периодов, утверждённых до ответа, и выбираются полем запроса.
func payrollLimitBase(kind string, ctx RetailBulkContext, row RetailRow, month int) (float64, bool) {
	switch kind {
	case "", PayrollBasePlanCurrent:
		if plan := row.valueOf(ctx.Year, month); plan != nil {
			return *plan, true
		}
		return 0, false
	case IndexBaseStrategy:
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

// bulkRent — «Аренда» (ТЗ §5 + ответ финблока §12 п.13 от 27.08.2026).
//
// Ответ финблока: «применяем пока факт предыдущего месяца; надо будет
// перестроить расчёт от каждого условия договора там, где есть привязка».
// Поэтому оборотная часть по умолчанию НЕ считается: этап 1 — перенос факта
// аренды предыдущего месяца. Формула ТЗ
// Аренда = Фикс + MAX(0; Выручка − Порог_оборота) × Ставка
// осталась в коде и включается явным RentTurnover=true (пороги и ставки —
// параметры периода) — это переходный режим до этапа 2, где аренда считается от
// условий договора.
func bulkRent(res *RetailBulkResult, req RetailBulkRequest, ctx RetailBulkContext,
	rows []RetailRow, months []int) error {
	noParams, stage1 := 0, 0
	for _, row := range rows {
		fixed, hasFixed := ctx.RentPrevFact[row.CodeCFO]
		threshold, hasThreshold := ctx.Params.Resolve(ParamRentThresh, row, ctx.Country)
		rate, hasRate := ctx.Params.Resolve(ParamRentRate, row, ctx.Country)
		turnover := req.RentTurnover && hasThreshold && hasRate
		if !hasFixed && !turnover {
			res.SkippedNoBase++
			continue
		}
		for _, m := range months {
			value, note := fixed, ""
			switch {
			case turnover:
				revenue := 0.0
				if plan := row.valueOf(ctx.Year, m); plan != nil {
					revenue = *plan
				}
				if over := revenue - threshold; over > 0 {
					value += over * rate
				}
			case req.RentTurnover:
				// Оборотную часть запросили, но параметров магазина нет.
				note = "только факт аренды предыдущего месяца: ставка/порог оборотной части не заданы"
				noParams++
			default:
				note = "этап 1: факт аренды предыдущего месяца (расчёт от условий договора — этап 2, §12 п.13)"
				stage1++
			}
			bulkSet(res, req, row, MetricRent, ctx.Year, m, value, ValueRentCarryover, note)
		}
	}
	if stage1 > 0 {
		res.Notes = append(res.Notes, fmt.Sprintf(
			"%d ячеек — факт аренды предыдущего месяца: по ответу финблока (§12 п.13) оборотная часть на этапе 1 не считается", stage1))
	}
	if noParams > 0 {
		res.Notes = append(res.Notes,
			fmt.Sprintf("%d ячеек посчитаны только по факту предыдущего месяца (нет ставки/порога оборотной части)", noParams))
	}
	if res.SkippedNoBase > 0 {
		res.Notes = append(res.Notes,
			fmt.Sprintf("у %d магазинов нет ни факта аренды предыдущего месяца, ни ставки — аренда не посчитана", res.SkippedNoBase))
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
	if req.Op == BulkPayroll && req.PayrollBase != "" {
		switch req.PayrollBase {
		case PayrollBasePlanCurrent, IndexBaseStrategy, IndexBaseFactPrevYear, IndexBaseApprovedPrev:
		default:
			return fmt.Errorf("неизвестная база ограничения ФОТ %q", req.PayrollBase)
		}
	}
	if req.Op == BulkCopyScenario && strings.TrimSpace(req.Source) == "" {
		return errors.New("для копирования сценария укажите источник (strategy|tactic|fact)")
	}
	return nil
}
