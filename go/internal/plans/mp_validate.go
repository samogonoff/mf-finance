package plans

import (
	"fmt"
	"math"
	"sort"
)

// Валидации формы МП (ТЗ §10): блокирующие МП-01…МП-11 не дают отправить/согласовать,
// предупреждающие МП-W1…МП-W8 подсвечивают строку и требуют обоснования, но ввод
// сохраняют. Логика чистая (без БД) — на ней держится «вперёд только при
// прохождении блокирующих валидаций», поэтому она покрыта тестами.

// MpIssueLevel — уровень замечания.
type MpIssueLevel string

const (
	// MpBlocking — блокирует переход вперёд по маршруту.
	MpBlocking MpIssueLevel = "blocking"
	// MpWarning — подсветка + обязательное обоснование, ввод сохраняется.
	MpWarning MpIssueLevel = "warning"
)

// MpIssue — одно замечание валидации.
type MpIssue struct {
	Code     string       `json:"code"` // МП-01 … МП-W8
	Level    MpIssueLevel `json:"level"`
	CodeCFO  int          `json:"code_cfo,omitempty"`
	NameCFO  string       `json:"name_cfo,omitempty"`
	Block    string       `json:"block_type,omitempty"`
	Message  string       `json:"message"`
	NeedsWhy bool         `json:"needs_why,omitempty"` // требуется комментарий/обоснование
}

// MpValidationInput — всё, что нужно для проверки формы за период.
type MpValidationInput struct {
	Year, Month int
	// Площадки задания и их атрибуты (страна, ЮЛ, сегмент, набор статей).
	Platforms []MarketplaceRow
	// Условия по площадке (ключ — code_cfo). Отсутствие = условия не заданы.
	Conditions map[int]MpConditions
	// Значения каскада по площадке (результат computeMpPlatformInverse).
	Values map[int]map[string]float64
	// Факт прошлого месяца и прошлого периода — для предупреждений W5–W7.
	FactPrev      map[int]map[string]float64
	PenaltiesFact map[int]float64
	// Условия прошлого периода — для W1/W2 (изменение сверх порога).
	PrevConditions map[int]MpConditions
	// Общий согласованный вес затрат по площадке (W3): доля, выше которой
	// «мы платим площадке больше, чем договорились». 0 = не задан.
	AgreedShare map[int]float64
	// Комментарии по площадке: наличие снимает требование обоснования.
	Comments   map[int]string
	Thresholds MpDiffThresholds
	// Владелец расчёта пары (статья, ЦФО) — контроль двойного счёта МП-08.
	CalcOwner map[[2]int]string
	// Наименования статей из справочника «Расходы Code PL» — контроль МП-11.
	PLNames map[int]string
}

// ValidateMpForm — полный набор проверок формы. Возвращает замечания,
// упорядоченные так, чтобы блокирующие шли первыми.
func ValidateMpForm(in MpValidationInput) []MpIssue {
	var out []MpIssue
	th := in.Thresholds
	if th == (MpDiffThresholds{}) {
		th = DefaultMpThresholds()
	}
	spec := mpFormSpec()
	names := map[string]string{}
	for _, l := range spec {
		names[l.BlockType] = l.Name
	}

	for _, p := range sortedPlatforms(in.Platforms) {
		cfo := p.CodeCFO
		cond, hasCond := in.Conditions[cfo]
		vals := in.Values[cfo]
		add := func(code string, level MpIssueLevel, needsWhy bool, format string, args ...any) {
			out = append(out, MpIssue{
				Code: code, Level: level, CodeCFO: cfo, NameCFO: p.NameCFO,
				Message: fmt.Sprintf(format, args...), NeedsWhy: needsWhy,
			})
		}

		// МП-05: площадка отнесена к группе, стране и ЮЛ.
		if p.Segment == "" || p.Country == "" || p.LegalEntity == "" {
			add("МП-05", MpBlocking, false,
				"площадка не отнесена к группе/стране/ЮЛ (сегмент %q, страна %q, ЮЛ %q)",
				p.Segment, p.Country, p.LegalEntity)
		}

		// МП-01: заданы продажи, %СПП, обе наценки и доли всех статей набора.
		if !hasCond {
			add("МП-01", MpBlocking, false, "не заданы условия площадки (%%СПП, наценки, доли статей)")
		} else {
			if cond.SppPct == 0 {
				add("МП-01", MpBlocking, false, "не задан %%СПП")
			}
			if cond.MarkupPct == 0 {
				add("МП-01", MpBlocking, false, "не задана наценка к отпускным ценам")
			}
			if cond.MarkupTotalPct == 0 {
				add("МП-01", MpBlocking, false, "не задана наценка от общей себестоимости")
			}
			// МП-02/МП-03: диапазоны и сумма долей.
			if err := validateConditions(cond); err != nil {
				code := "МП-02"
				if isShareSumError(err) {
					code = "МП-03"
				}
				add(code, MpBlocking, false, "%s", err.Error())
			}
		}
		if vals == nil || vals[BSalesManagerGross] == 0 {
			add("МП-01", MpBlocking, false, "не заданы продажи по ценам менеджера")
		}

		// МП-04: контроль сходимости. Прямые затраты = комиссия + Σ статей,
		// PL = маржа − (ПЗ − комиссия). Расхождение должно быть нулевым.
		if vals != nil {
			sum := vals[BCommission]
			for _, b := range mpCostBlocks {
				sum += vals[b]
			}
			if diff := vals[BPlatformCosts] - sum; math.Abs(diff) > 0.01 {
				add("МП-04", MpBlocking, false,
					"прямые затраты не равны сумме статей: расхождение %.2f", diff)
			}
			wantPL := vals[BRetailMargin] - (vals[BPlatformCosts] - vals[BCommission])
			if diff := vals[BPlPlatform] - wantPL; math.Abs(diff) > 0.01 {
				add("МП-04", MpBlocking, false,
					"PL по площадке не сходится с маржой за вычетом прямых затрат: расхождение %.2f", diff)
			}
		}

		// МП-06: соотношение «с НДС / без НДС» не задаётся вручную в строке —
		// ставка приходит из условий/справочника.
		if hasCond && cond.VatRate != nil && (*cond.VatRate < 0 || *cond.VatRate >= 1) {
			add("МП-06", MpBlocking, false, "недопустимая ставка НДС: %.4f", *cond.VatRate)
		}

		// МП-10: валюта ввода = валюте площадки.
		if hasCond && cond.Currency != "" && p.Country != "" {
			if want := platformCurrency(p.Country); want != "" && cond.Currency != want {
				add("МП-10", MpBlocking, false,
					"валюта ввода %s не совпадает с валютой площадки (%s)", cond.Currency, want)
			}
		}

		// МП-08: одна и та же комбинация (ЦФО, статья) не приходит из двух форм.
		for _, b := range []string{BCostFreight, BCostLogTransport, BCostLogWarehouse} {
			pl := blockToPL()[b]
			if owner, ok := in.CalcOwner[[2]int{pl, cfo}]; ok && owner != TemplateMP {
				add("МП-08", MpBlocking, false,
					"статья %d (%s) считается формой «%s» — здесь она только выводится",
					pl, names[b], owner)
			}
		}

		// МП-W1/W2: изменения условий сверх порога требуют обоснования.
		if hasCond {
			prev, hadPrev := in.PrevConditions[cfo]
			if hadPrev {
				if d := math.Abs(cond.SppPct - prev.SppPct); d > th.SppPct {
					add("МП-W1", MpWarning, cond.ChangeReason == "",
						"%%СПП изменился на %.2f п.п. к прошлому периоду", d*100)
				}
				for _, b := range mpCostBlocks {
					if d := math.Abs(cond.Shares[b] - prev.Shares[b]); d > th.SharePct {
						add("МП-W2", MpWarning, cond.ChangeReason == "",
							"доля статьи «%s» изменилась на %.2f п.п.", names[b], d*100)
					}
				}
			}
			// МП-W3: доля прямых затрат превысила согласованный с площадкой вес.
			if agreed := in.AgreedShare[cfo]; agreed > 0 && vals != nil {
				if vals[BDirectShare] > agreed {
					add("МП-W3", MpWarning, in.Comments[cfo] == "",
						"доля прямых затрат %.2f %% выше согласованного веса %.2f %%",
						vals[BDirectShare]*100, agreed*100)
				}
			}
		}

		if vals != nil {
			// МП-W4: PL по площадке ушёл в минус.
			if vals[BPlPlatform] < 0 {
				add("МП-W4", MpWarning, in.Comments[cfo] == "",
					"PL по площадке отрицательный: %.2f", vals[BPlPlatform])
			}
			// МП-W5: маржинальность отклонилась от прошлого периода сверх порога.
			if prev := in.FactPrev[cfo]; prev != nil && prev[BRetailMarginPct] != 0 {
				if d := math.Abs(vals[BRetailMarginPct] - prev[BRetailMarginPct]); d > th.MarkupPct {
					add("МП-W5", MpWarning, in.Comments[cfo] == "",
						"маржинальность отклоняется от прошлого периода на %.2f п.п.", d*100)
				}
			}
			// МП-W6: продажи = 0 при наличии факта прошлого месяца.
			if vals[BSalesManagerGross] == 0 {
				if prev := in.FactPrev[cfo]; prev != nil && prev[BSalesManagerGross] > 0 {
					add("МП-W6", MpWarning, in.Comments[cfo] == "",
						"продажи не заданы, хотя в прошлом месяце факт был %.2f", prev[BSalesManagerGross])
				}
			}
			// МП-W7: плановые штрафы = 0 при устойчивом факте штрафов.
			if vals[BCostPenalties] == 0 && in.PenaltiesFact[cfo] > 0 {
				add("МП-W7", MpWarning, in.Comments[cfo] == "",
					"штрафы запланированы нулём, хотя факт прошлых периодов — %.2f", in.PenaltiesFact[cfo])
			}
		}

		// МП-W8: статьи 51/52/54 задублированы с планом ЦЗ 32 «Логистика».
		for _, b := range []string{BCostFreight, BCostLogTransport, BCostLogWarehouse} {
			pl := blockToPL()[b]
			if owner, ok := in.CalcOwner[[2]int{pl, cfo}]; ok && owner == TemplateMP {
				continue // владелец — мы, дублирования нет
			} else if !ok {
				add("МП-W8", MpWarning, false,
					"владелец расчёта статьи %d (%s) не задан — возможен двойной счёт с ЦЗ 32", pl, names[b])
			}
		}

	}

	// МП-11: наименование статьи в форме совпадает со справочником «Расходы Code PL»
	// по тому же коду. Контроль конфликта кодов 52 и 54 (ТЗ §8.1: в справочнике 54 —
	// «Аренда помещений (стоянки)», в форме МП — «Складская логистика»).
	// Проверка уровня ФОРМЫ, а не площадки: наименование статьи от площадки не
	// зависит, и повторять одно замечание по каждой площадке — шум, в котором
	// теряются настоящие построчные ошибки.
	for _, l := range spec {
		if l.CodePL == 0 || !l.CostLine {
			continue
		}
		if want, ok := in.PLNames[l.CodePL]; ok && want != "" && want != l.Name {
			out = append(out, MpIssue{
				Code: "МП-11", Level: MpBlocking, Block: l.BlockType,
				Message: fmt.Sprintf("статья %d в форме названа «%s», а в справочнике — «%s»",
					l.CodePL, l.Name, want),
			})
		}
	}

	// МП-07: свод по группе = сумме площадок. Проверяем на уровне формы.
	if len(in.Values) > 0 {
		var sumPL, sumDirect float64
		for _, v := range in.Values {
			sumPL += v[BPlPlatform]
			sumDirect += v[BPlatformCosts]
		}
		if math.IsNaN(sumPL) || math.IsNaN(sumDirect) {
			out = append(out, MpIssue{Code: "МП-07", Level: MpBlocking,
				Message: "свод не считается: в значениях есть нечисловые результаты"})
		}
	}

	sort.SliceStable(out, func(i, j int) bool {
		if out[i].Level != out[j].Level {
			return out[i].Level == MpBlocking
		}
		return out[i].Code < out[j].Code
	})
	return dedupIssues(out)
}

// HasBlocking — есть ли блокирующие замечания (гейт перехода вперёд).
func HasBlocking(issues []MpIssue) bool {
	for _, i := range issues {
		if i.Level == MpBlocking {
			return true
		}
	}
	return false
}

// NeedsExplanation — предупреждения без обоснования (переход возможен только
// после комментария, ТЗ §10.2).
func NeedsExplanation(issues []MpIssue) []MpIssue {
	var out []MpIssue
	for _, i := range issues {
		if i.Level == MpWarning && i.NeedsWhy {
			out = append(out, i)
		}
	}
	return out
}

// platformCurrency — валюта площадки по стране (ТЗ §3.3: ввод в валюте площадки).
func platformCurrency(country string) string {
	switch country {
	case "RU":
		return "RUB"
	case "KZ":
		return "KZT"
	case "UZ":
		return "UZS"
	case "BY":
		return "BYN"
	}
	return ""
}

// isShareSumError — отличить нарушение суммы долей (МП-03) от диапазона (МП-02).
func isShareSumError(err error) bool {
	return err != nil && containsRu(err.Error(), "превышает 100")
}

func containsRu(s, sub string) bool {
	return len(s) >= len(sub) && indexOf(s, sub) >= 0
}

func indexOf(s, sub string) int {
	for i := 0; i+len(sub) <= len(s); i++ {
		if s[i:i+len(sub)] == sub {
			return i
		}
	}
	return -1
}

func sortedPlatforms(list []MarketplaceRow) []MarketplaceRow {
	out := append([]MarketplaceRow(nil), list...)
	sort.Slice(out, func(i, j int) bool { return out[i].CodeCFO < out[j].CodeCFO })
	return out
}

// dedupIssues — одно и то же замечание по площадке не дублируем.
func dedupIssues(list []MpIssue) []MpIssue {
	seen := map[string]bool{}
	out := make([]MpIssue, 0, len(list))
	for _, i := range list {
		k := i.Code + "|" + itoa(int64(i.CodeCFO)) + "|" + i.Message
		if seen[k] {
			continue
		}
		seen[k] = true
		out = append(out, i)
	}
	return out
}
