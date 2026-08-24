package plans

import (
	"fmt"
	"sort"
	"strings"
)

// Валидации формы «Розница» (ТЗ §6): блокирующие V-01..V-11 и предупреждающие
// W-01..W-06. Чистые функции без БД — покрыты табличными тестами
// (retail_validate_test.go).
//
// Различие блокирующих и предупреждающих принципиальное и заложено в тип:
//   - V-* блокируют ОТПРАВКУ формы на согласование (Blocking=true). Ввод при этом
//     сохраняется: пользователь должен видеть, что именно не так, а не терять
//     работу на 375 строках.
//   - W-* ввод не блокируют, но требуют комментария к строке (V-07). Поэтому
//     W-проверки возвращают код и строку, а V-07 проверяет, что у строки с
//     непустым набором W-предупреждений есть комментарий.

// ValidationIssue — одна сработавшая проверка.
type ValidationIssue struct {
	Code     string  `json:"code"` // V-01 … W-06
	Blocking bool    `json:"blocking"`
	CodeCFO  int     `json:"code_cfo,omitempty"` // 0 — проверка уровня формы
	Month    int     `json:"month,omitempty"`    // 0 — проверка не привязана к месяцу
	Message  string  `json:"message"`
	Value    float64 `json:"value,omitempty"` // сработавшее значение (для порогов)
}

// RetailValidationReport — отчёт GET /api/plans/retail/{cardId}/validate.
type RetailValidationReport struct {
	CardID    int64             `json:"card_id"`
	Blocking  []ValidationIssue `json:"blocking"`
	Warnings  []ValidationIssue `json:"warnings"`
	CanSubmit bool              `json:"can_submit"`
	// Reconciliations — контрольные сверки согласующего (ТЗ §7): все должны быть 0.
	Reconciliations []RetailReconciliation `json:"reconciliations"`
}

// RetailValidationInput — всё, что нужно проверкам. Собирается сервисом, чтобы
// сами проверки остались чистыми.
type RetailValidationInput struct {
	Country     string
	LegalEntity string
	NatCurrency string
	Year        int
	Month       int
	PlanMonths  []int
	Rows        []RetailRow
	Series      RetailSeries
	Params      RetailParamSet
	// ValueLimit — верхняя граница значения ячейки (V-02, «настраивается по
	// стране»). 0 → границы нет.
	ValueLimit float64
	// DirectoryCodes — активные CodeCFO из источника (V-04: «есть в справочнике»,
	// и полнота справочника для §7). nil → проверка пропускается: отсутствие связи
	// с источником не должно выдавать ложное «магазина нет в справочнике».
	DirectoryCodes map[int]bool
}

// ValidateRetailForm — полный набор проверок ТЗ §6.
func ValidateRetailForm(in RetailValidationInput) (blocking, warnings []ValidationIssue) {
	blocking = make([]ValidationIssue, 0)
	warnings = make([]ValidationIssue, 0)

	add := func(dst *[]ValidationIssue, code string, block bool, cfo, month int, val float64, format string, args ...any) {
		*dst = append(*dst, ValidationIssue{
			Code: code, Blocking: block, CodeCFO: cfo, Month: month, Value: val,
			Message: fmt.Sprintf(format, args...),
		})
	}

	// ---- проверки уровня формы ----

	// V-11: валюта всех значений = нац. валюте страны. Ввод физически идёт в одной
	// валюте (валюта экземпляра), поэтому проверяем согласованность экземпляра и
	// реестра форм: рассинхрон означает, что карточку создали не тем шаблоном.
	if nat := retailCurrency(in.Country); nat != "" && !strings.EqualFold(nat, in.NatCurrency) {
		add(&blocking, "V-11", true, 0, 0, 0,
			"валюта формы %q не совпадает с национальной валютой страны %s (%s)", in.NatCurrency, in.Country, nat)
	}

	// V-08: все строки на ОДНОМ ЮЛ страны.
	wantLE, haveLE := retailLegalEntity(in.Country)
	seenLE := map[string]bool{}
	for _, r := range in.Rows {
		if le := strings.TrimSpace(r.LegalEntity); le != "" {
			seenLE[le] = true
		}
	}
	if len(seenLE) > 1 {
		add(&blocking, "V-08", true, 0, 0, float64(len(seenLE)),
			"строки формы принадлежат разным ЮЛ (%s): у страны %s должно быть одно ЮЛ",
			strings.Join(sortedKeys(seenLE), ", "), in.Country)
	} else if haveLE && len(seenLE) == 1 && !seenLE[wantLE] {
		add(&blocking, "V-08", true, 0, 0, 0,
			"ЮЛ строк (%s) не совпадает с ЮЛ страны %s (%s)", sortedKeys(seenLE)[0], in.Country, wantLE)
	}

	// V-04 (уникальность): CodeCFO и непустой KLIENT_ID не дублируются.
	seenCFO := map[int]int{}
	seenKlient := map[string]int{}
	for _, r := range in.Rows {
		seenCFO[r.CodeCFO]++
		if k := strings.TrimSpace(r.KlientID); k != "" {
			seenKlient[k]++
		}
	}
	for code, n := range seenCFO {
		if n > 1 {
			add(&blocking, "V-04", true, code, 0, float64(n), "CodeCFO %d встречается %d раз", code, n)
		}
	}
	for k, n := range seenKlient {
		if n > 1 {
			add(&blocking, "V-04", true, 0, 0, float64(n), "KLIENT_ID %s встречается %d раз", k, n)
		}
	}

	// ---- построчные проверки ----

	for _, r := range in.Rows {
		rowWarn := make([]ValidationIssue, 0, 4)
		warn := func(code string, month int, val float64, format string, args ...any) {
			rowWarn = append(rowWarn, ValidationIssue{
				Code: code, Blocking: false, CodeCFO: r.CodeCFO, Month: month, Value: val,
				Message: fmt.Sprintf(format, args...),
			})
		}

		// V-04 (наличие): CodeCFO заполнен и есть в справочнике.
		if r.CodeCFO == 0 {
			add(&blocking, "V-04", true, 0, 0, 0, "строка «%s» без CodeCFO", r.NameCFO)
		} else if in.DirectoryCodes != nil && !in.DirectoryCodes[r.CodeCFO] {
			add(&blocking, "V-04", true, r.CodeCFO, 0, 0,
				"магазина %d нет в справочнике активных магазинов", r.CodeCFO)
		}

		// V-09: в форму не попали магазины, закрытые до начала периода.
		if retailStoreClosedBefore(r.DateClose, in.Year, in.Month) {
			add(&blocking, "V-09", true, r.CodeCFO, 0, 0,
				"магазин %d закрыт %s — до начала периода %d-%02d, в форме ввода его быть не должно",
				r.CodeCFO, r.DateClose, in.Year, in.Month)
		}

		closedRow := r.LFLEffective == LFLClosed || r.DateClose != ""

		for _, m := range in.PlanMonths {
			cell, filled := r.cellOf(in.Year, m)
			amount := 0.0
			if filled && cell.Amount != nil {
				amount = *cell.Amount
			}

			// V-01: все ячейки целевых месяцев заполнены. «Пусто ≠ 0»: для нуля
			// нужен явный ввод, поэтому проверяем наличие ячейки, а не значение.
			if !filled || cell.Amount == nil {
				// Для закрытого магазина пустая ячейка — это норма (V-03).
				if !retailStoreClosedBefore(r.DateClose, in.Year, m) {
					add(&blocking, "V-01", true, r.CodeCFO, m, 0,
						"магазин %d: не заполнен план за %02d.%d (пусто ≠ 0 — для нуля введите 0)",
						r.CodeCFO, m, in.Year)
				}
				continue
			}

			// V-02: значение ≥ 0 и в допустимом диапазоне.
			if amount < 0 {
				add(&blocking, "V-02", true, r.CodeCFO, m, amount,
					"магазин %d, %02d.%d: план отрицательный (%.2f)", r.CodeCFO, m, in.Year, amount)
			} else if in.ValueLimit > 0 && amount > in.ValueLimit {
				add(&blocking, "V-02", true, r.CodeCFO, m, amount,
					"магазин %d, %02d.%d: план %.2f выше допустимой границы %.2f для страны %s",
					r.CodeCFO, m, in.Year, amount, in.ValueLimit, in.Country)
			}

			// V-03: по закрытому магазину план пустой или 0.
			if retailStoreClosedBefore(r.DateClose, in.Year, m) && amount != 0 {
				add(&blocking, "V-03", true, r.CodeCFO, m, amount,
					"магазин %d закрыт %s, но на %02d.%d стоит план %.2f",
					r.CodeCFO, r.DateClose, m, in.Year, amount)
			}

			// V-10: нет значений вне планового периода — проверяется ниже по всем
			// ячейкам строки (здесь мы идём только по плановым месяцам).

			// ---- предупреждения по месяцу карточки (ТЗ §6) ----
			if m != in.Month {
				continue
			}
			factPrevYear, hasFactPrevYear := retailFactAt(in.Series, r.CodeCFO, in.Year-1, m)
			py, pm := retailPrevMonth(in.Year, m)
			factPrevMonth, _ := retailFactAt(in.Series, r.CodeCFO, py, pm)
			strategy := in.Series.Strategy[RetailCellKey{r.CodeCFO, in.Year, m}]

			lflWarn := in.Params.ResolveOr(ParamLFLWarn, r, in.Country, DefaultLFLWarn)
			lfmWarn := in.Params.ResolveOr(ParamLFMWarn, r, in.Country, DefaultLFMWarn)
			stratWarn := in.Params.ResolveOr(ParamStrategyWarn, r, in.Country, DefaultStrategyWarn)

			// W-01/W-02 не применяются к «новый»/«до года»/«ххх» (ТЗ §6).
			if !retailWarnExempt(r.LFLEffective) {
				if lfl := iferrorRatioMinus1(amount, factPrevYear); factPrevYear != 0 && absf(lfl) > lflWarn {
					warn("W-01", m, lfl, "LFL тактич. %.1f%% превышает порог %.1f%% (LFL-статус %q)",
						lfl*100, lflWarn*100, r.LFLEffective)
				}
				if lfm := iferrorRatioMinus1(amount, factPrevMonth); factPrevMonth != 0 && absf(lfm) > lfmWarn {
					warn("W-02", m, lfm, "LFM тактич. %.1f%% превышает порог %.1f%%", lfm*100, lfmWarn*100)
				}
			}
			// W-03: план = 0 при непустом факте того же месяца прошлого года.
			if amount == 0 && hasFactPrevYear && factPrevYear != 0 {
				warn("W-03", m, factPrevYear,
					"план на %02d.%d = 0, хотя факт %02d.%d был %.2f", m, in.Year, m, in.Year-1, factPrevYear)
			}
			// W-04: отклонение тактики от стратегии больше порога.
			if strategy != 0 {
				if d := iferrorRatioMinus1(amount, strategy); absf(d) > stratWarn {
					warn("W-04", m, d, "отклонение от стратегии %.1f%% превышает порог %.1f%%", d*100, stratWarn*100)
				}
			}
			// W-05: план на месяцы до DateOpen.
			if amount != 0 && retailStoreOpenedAfter(r.DateOpen, in.Year, m) {
				warn("W-05", m, amount, "план на %02d.%d стоит до даты открытия магазина (%s)", m, in.Year, r.DateOpen)
			}
			// W-06: магазин активен в справочнике, но факта за прошедшие месяцы года нет.
			if !closedRow && in.Month > 1 {
				hasAny := false
				for pmn := 1; pmn < in.Month; pmn++ {
					if _, ok := retailFactAt(in.Series, r.CodeCFO, in.Year, pmn); ok {
						hasAny = true
						break
					}
				}
				if !hasAny {
					warn("W-06", 0, 0, "магазин %d активен, но факта за прошедшие месяцы %d нет", r.CodeCFO, in.Year)
				}
			}
		}

		// V-10: значения вне планового периода. Ячейка за месяц/год, которого нет
		// в плановом периоде — это чаще всего импорт «не туда»; молча складывать
		// такое в форму нельзя, иначе «Итого за период» не сойдётся со свод́ом.
		inPlan := map[int]bool{}
		for _, m := range in.PlanMonths {
			inPlan[m] = true
		}
		for _, v := range r.Values {
			if v.Metric != "" && v.Metric != MetricSales {
				continue // ФОТ/аренда — производные статьи, V-10 про план продаж
			}
			if v.Year != in.Year || !inPlan[v.Month] {
				add(&blocking, "V-10", true, r.CodeCFO, v.Month, 0,
					"магазин %d: значение за %02d.%d вне планового периода", r.CodeCFO, v.Month, v.Year)
			}
		}

		// V-07: обязательный комментарий при сработавшем предупреждении.
		if len(rowWarn) > 0 && strings.TrimSpace(r.Comment) == "" {
			add(&blocking, "V-07", true, r.CodeCFO, 0, float64(len(rowWarn)),
				"магазин %d: сработало предупреждений — %d, нужен комментарий к строке", r.CodeCFO, len(rowWarn))
		}
		warnings = append(warnings, rowWarn...)
	}

	sortIssues(blocking)
	sortIssues(warnings)
	return blocking, warnings
}

// RetailRowWarnings — предупреждения одной строки: тот же расчёт, что в
// ValidateRetailForm, но для подсветки ячеек в сетке ввода (ТЗ §12: ответ формы
// содержит «сработавшие предупреждения»).
func RetailRowWarnings(row RetailRow, in RetailValidationInput) []ValidationIssue {
	one := in
	one.Rows = []RetailRow{row}
	_, warnings := ValidateRetailForm(one)
	return warnings
}

// sortIssues — стабильный порядок: код проверки, затем магазин и месяц. Отчёт
// должен читаться одинаково при каждом запросе.
func sortIssues(list []ValidationIssue) {
	sort.SliceStable(list, func(i, j int) bool {
		if list[i].Code != list[j].Code {
			return list[i].Code < list[j].Code
		}
		if list[i].CodeCFO != list[j].CodeCFO {
			return list[i].CodeCFO < list[j].CodeCFO
		}
		return list[i].Month < list[j].Month
	})
}

func sortedKeys(m map[string]bool) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

func absf(v float64) float64 {
	if v < 0 {
		return -v
	}
	return v
}
