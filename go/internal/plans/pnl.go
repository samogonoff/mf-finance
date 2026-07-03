package plans

import "context"

// Сводное окно P&L (единое окно): по карточке периода — матрица ЦФО × статья со
// всеми сценариями (факт / стратегия / тактика / расчёт) + сравнение тактики с
// прошлым периодом (ТЗ §«Карточка PL», помесячный лист: plan_strategy/plan_tactic/
// fact_current/delta_pct). Read-only потребление того, что наполняют задания.

// PnlRow — строка сводного P&L: ЦФО × статья со сценариями и дельтами.
type PnlRow struct {
	CodeCFO     int      `json:"code_cfo"`
	NameCFO     string   `json:"name_cfo"`
	LineCode    int      `json:"line_code"`
	ExpenseName string   `json:"expense_name"`
	BlockType   string   `json:"block_type"`
	Fact        *float64 `json:"fact"`
	Strategy    *float64 `json:"strategy"`
	Tactic      *float64 `json:"tactic"`
	Calc        *float64 `json:"calc"`
	IsManual    bool     `json:"is_manual"`
	PrevTactic  *float64 `json:"prev_tactic"` // тактика прошлого периода (сравнение)
}

// PnlSummary — сводное окно карточки.
type PnlSummary struct {
	Year      int     `json:"year"`
	Month     int     `json:"month"`
	PrevYear  int     `json:"prev_year"`
	PrevMonth int     `json:"prev_month"`
	Rows      []PnlRow `json:"rows"`
}

// Pnl собирает сводную матрицу карточки + тактику прошлого периода.
func (s *TaskStore) Pnl(ctx context.Context, plID int64) (PnlSummary, error) {
	var out PnlSummary
	_ = s.pool.QueryRow(ctx, `SELECT period_year, period_month FROM pl_instance WHERE id=$1`, plID).
		Scan(&out.Year, &out.Month)

	cfoNames := s.nameMap(ctx, "dir_cfo", "code_cfo", "name_cfo")
	plNames := s.nameMap(ctx, "dir_pl_line", "code_pl", "expense_name")

	// Тактика прошлого периода (ближайшая карточка до текущей).
	var prevID int64
	_ = s.pool.QueryRow(ctx, `
		SELECT id, period_year, period_month FROM pl_instance
		WHERE (period_year, period_month) < ($1,$2)
		ORDER BY period_year DESC, period_month DESC LIMIT 1`, out.Year, out.Month).
		Scan(&prevID, &out.PrevYear, &out.PrevMonth)
	prevTac := map[[2]any]float64{}
	if prevID != 0 {
		pr, err := s.pool.Query(ctx, `
			SELECT profit_center, block_type, MAX(amount) FROM pl_metric
			WHERE pl_id=$1 AND amount IS NOT NULL GROUP BY profit_center, block_type`, prevID)
		if err == nil {
			for pr.Next() {
				var pc int
				var bt string
				var v *float64
				if pr.Scan(&pc, &bt, &v) == nil && v != nil {
					prevTac[[2]any{pc, bt}] = *v
				}
			}
			pr.Close()
		}
	}

	// Факт — живой оверлей из источника МП (в pl_metric не персистится): по
	// обоим сегментам, индекс по (code_cfo, code_pl=line_code).
	factIdx := map[[2]int]float64{}
	if s.fact != nil {
		for _, seg := range []string{"large", "small"} {
			if fr, e := s.fact.MpFact(ctx, out.Year, out.Month, seg); e == nil {
				for _, f := range fr {
					factIdx[[2]int{f.CodeCFO, f.CodePL}] = f.Amount
				}
			}
		}
	}

	rows, err := s.pool.Query(ctx, `
		SELECT profit_center, line_code, block_type,
		       MAX(amount_fact), MAX(amount_strategy), MAX(amount), MAX(amount_calc), bool_or(is_manual)
		FROM pl_metric WHERE pl_id=$1
		GROUP BY profit_center, line_code, block_type
		ORDER BY profit_center, line_code, block_type`, plID)
	if err != nil {
		return out, err
	}
	defer rows.Close()
	for rows.Next() {
		var r PnlRow
		if err := rows.Scan(&r.CodeCFO, &r.LineCode, &r.BlockType,
			&r.Fact, &r.Strategy, &r.Tactic, &r.Calc, &r.IsManual); err != nil {
			return out, err
		}
		r.NameCFO = cfoNames[r.CodeCFO]
		r.ExpenseName = plNames[r.LineCode]
		if r.Fact == nil {
			if v, ok := factIdx[[2]int{r.CodeCFO, r.LineCode}]; ok {
				vv := v
				r.Fact = &vv
			}
		}
		if v, ok := prevTac[[2]any{r.CodeCFO, r.BlockType}]; ok {
			vv := v
			r.PrevTactic = &vv
		}
		out.Rows = append(out.Rows, r)
	}
	return out, rows.Err()
}
