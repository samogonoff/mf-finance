package plans

// Движок CALC (D11, SPEC §11) — этап 2, первый срез. Формулы каскада TPL-MP —
// ДАННЫЕ (calc_rule), переопределяемые per-срез (pl_formula_override), приоритет
// override > seed. Значения вычисляет безопасный Eval. Точные выражения каскада
// — провизорные (Q4b: снять у автора прототипа); правятся конфигом, не кодом.

// FormulaOverride — per-срез переопределение формулы финансистом (D11).
type FormulaOverride struct {
	Code         string `json:"code"`
	BlockType    string `json:"block_type"`
	FormulaExpr  string `json:"formula_expr"`
	Reason       string `json:"reason"`
	ScopeCodeCFO *int   `json:"scope_code_cfo,omitempty"`
}

// CalcRule — правило расчёта (строка calc_rule).
type CalcRule struct {
	Code        string `json:"code"`
	TemplateCode string `json:"template_code"`
	FormulaExpr string `json:"formula_expr"`
	Version     int    `json:"version"`
}

// CalcRuleSeed — провизорные формулы каскада TPL-MP (Q4b). Переменные:
//   sales — продажи по ценам менеджера с НДС (1046)
//   cost  — себестоимость по отпускным ценам (8006)
//   vat   — ставка НДС (константа разреза)
func CalcRuleSeed() []CalcRule {
	return []CalcRule{
		{Code: "sales_net", TemplateCode: TemplateMP, Version: 1, FormulaExpr: "sales / (1 + vat)"},
		{Code: "gross_margin", TemplateCode: TemplateMP, Version: 1, FormulaExpr: "sales - cost"},
		{Code: "markup_pct", TemplateCode: TemplateMP, Version: 1, FormulaExpr: "(sales - cost) / cost"},
	}
}

// resolveFormulas — итоговый набор формул: override (per-срез) поверх seed.
func resolveFormulas(seed []CalcRule, overrides map[string]string) map[string]string {
	out := make(map[string]string, len(seed))
	for _, r := range seed {
		out[r.Code] = r.FormulaExpr
	}
	for code, expr := range overrides {
		if expr != "" {
			out[code] = expr
		}
	}
	return out
}

// computeCascade считает все формулы по переменным. Правила с ошибкой вычисления
// (деление на ноль, неизвестная переменная) пропускаются — без них, не падая.
func computeCascade(vars map[string]float64, formulas map[string]string) map[string]float64 {
	out := make(map[string]float64, len(formulas))
	for code, expr := range formulas {
		v, err := Eval(expr, vars)
		if err != nil {
			continue
		}
		out[code] = v
	}
	return out
}
