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

// pnlNotDuplicateTotal — условие для запросов к pl_metric с алиасом m: итоговые строки
// (profit_center=0: скидки, уценки) у крупных и мелких МП свои, и в сводке они
// суммируются; строка без сегмента (сохранена до разделения или у смешанного
// задания) учитывается, только если своих строк по этой статье нет — иначе была бы
// посчитана дважды.
const pnlNotDuplicateTotal = `NOT (m.profit_center = 0 AND m.segment = '' AND EXISTS (
	SELECT 1 FROM pl_metric s
	 WHERE s.pl_id = m.pl_id AND s.template_code = m.template_code
	   AND s.profit_center = 0 AND s.line_code = m.line_code AND s.block_type = m.block_type
	   AND s.segment <> '' AND s.amount IS NOT NULL))`
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
			SELECT m.profit_center, m.block_type,
			       CASE WHEN m.profit_center = 0 THEN SUM(m.amount) ELSE MAX(m.amount) END
			  FROM pl_metric m
			 WHERE m.pl_id=$1 AND m.amount IS NOT NULL AND `+pnlNotDuplicateTotal+`
			 GROUP BY m.profit_center, m.block_type`, prevID)
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
		SELECT m.profit_center, m.line_code, m.block_type,
		       MAX(m.amount_fact), MAX(m.amount_strategy),
		       CASE WHEN m.profit_center = 0 THEN SUM(m.amount) ELSE MAX(m.amount) END,
		       MAX(m.amount_calc), bool_or(m.is_manual)
		FROM pl_metric m WHERE m.pl_id=$1 AND `+pnlNotDuplicateTotal+`
		GROUP BY m.profit_center, m.line_code, m.block_type
		ORDER BY m.profit_center, m.line_code, m.block_type`, plID)
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
