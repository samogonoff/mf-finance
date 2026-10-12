package plans

import (
	"context"
	"errors"
	"fmt"
	"log"
)

// Массовые расчёты формы 1.5 (ТЗ §6) поверх чистых формул exp_calc.go.
//
// ТЗ §6.1 требует ПРЕДПРОСМОТР: «список строк с прежним и новым значением и
// число строк, где сработало ограничение». Поэтому расчёт и применение — один
// метод с флагом: два отдельных пути разошлись бы, и человек согласовывал бы не
// то, что запишется.

// ExpCalcDiff — одна строка предпросмотра расчёта.
type ExpCalcDiff struct {
	CodeCFO int      `json:"code_cfo"`
	CodePL  int      `json:"code_pl"`
	CFO     string   `json:"cfo,omitempty"`
	Month   int      `json:"month"`
	Before  *float64 `json:"before"`
	After   float64  `json:"after"`
	Note    string   `json:"note,omitempty"`
}

// ExpCalcResult — отчёт расчёта.
type ExpCalcResult struct {
	Op      string `json:"op"`
	Preview bool   `json:"preview"`
	Rows    int    `json:"rows"`
	Changed int    `json:"changed"`
	// Capped — в скольких строках сработало ограничение порогом (ТЗ §6.1).
	Capped int `json:"capped"`
	// NoBase — в скольких строках базы не нашлось: «порог не применён, нет базы».
	NoBase int           `json:"no_base"`
	Diff   []ExpCalcDiff `json:"diff"`
}

// Виды расчёта.
const (
	ExpCalcPayroll = "payroll"
	ExpCalcRent    = "rent"
)

// expRentCodePL — статьи аренды (ТЗ §6.2): 54 «Аренда помещений (стоянки)» и
// 55 «Аренда имущества». Одной метрикой они не раскладываются: это разные статьи
// справочника, и складывать их в одну строку PL нельзя.
var expRentCodePL = map[int]bool{54: true, 55: true}

// ExpCalcRequest — запрос расчёта.
type ExpCalcRequest struct {
	Op      string `json:"op"`
	Preview bool   `json:"preview"`
	// Months — охват по времени (ТЗ §7.7). Пусто — весь плановый год.
	Months []int `json:"months"`
}

// WithSalesPlan подключает план продаж — вход расчёта §6.1.
func (s *ExpService) WithSalesPlan(src ExpDataSource) *ExpService { s.sales = src; return s }

// Params — параметры периода формы (ТЗ §6.1).
func (s *ExpService) Params(ctx context.Context, p Principal, cardID int64) ([]ExpParamRow, error) {
	_, in, err := s.access(ctx, cardID)
	if err != nil {
		return nil, err
	}
	return s.repo.Params(ctx, in.ID)
}

// SaveParam — изменение параметра периода. По ТЗ §6.1 параметры меняет финансист;
// проверка права — общая для формы, поэтому здесь только состояние карточки.
func (s *ExpService) SaveParam(ctx context.Context, p Principal, cardID int64, row ExpParamRow) error {
	c, in, err := s.access(ctx, cardID)
	if err != nil {
		return err
	}
	if !cardEditable(c) {
		return errors.New("период закрыт: параметры не меняются")
	}
	if !p.PlansAdmin {
		return errors.New("параметры периода задаёт финансист")
	}
	switch row.ParamCode {
	case ExpParamPayrollShare, ExpParamPayrollCap:
	default:
		return fmt.Errorf("неизвестный параметр %q", row.ParamCode)
	}
	if row.ScopeKind == "" {
		row.ScopeKind = "country"
	}
	return s.repo.SaveParam(ctx, in.ID, row, p.UserID)
}

// Calc — массовый расчёт (ТЗ §6). Preview ничего не сохраняет.
func (s *ExpService) Calc(ctx context.Context, p Principal, cardID int64, req ExpCalcRequest) (ExpCalcResult, error) {
	res := ExpCalcResult{Op: req.Op, Preview: req.Preview, Diff: []ExpCalcDiff{}}
	switch req.Op {
	case ExpCalcPayroll, ExpCalcRent:
	default:
		return res, fmt.Errorf("неизвестный расчёт %q", req.Op)
	}
	c, in, err := s.access(ctx, cardID)
	if err != nil {
		return res, err
	}
	if !req.Preview && !cardEditable(c) {
		return res, errors.New("период закрыт на запись: расчёт недоступен")
	}
	if req.Op == ExpCalcPayroll && s.sales == nil {
		return res, errors.New("план продаж недоступен — расчёт §6.1 не выполняется")
	}

	rows, _, err := s.repo.Rows(ctx, in.ID, ExpFilter{Limit: 5000})
	if err != nil {
		return res, err
	}
	params, err := s.repo.Params(ctx, in.ID)
	if err != nil {
		return res, err
	}
	sales := map[int]map[int]float64{}
	if req.Op == ExpCalcPayroll {
		sales, err = s.sales.SalesPlan(ctx, in.Country, in.PlanYear)
		if err != nil {
			return res, fmt.Errorf("план продаж не прочитан: %w", err)
		}
	}

	months := req.Months
	if len(months) == 0 {
		months = allMonths()
	}
	share := expParamValue(params, ExpParamPayrollShare, "")
	capDefault := expParamValue(params, ExpParamPayrollCap, "")

	cells := make([]ExpCell, 0, 128)

	// §6.2 «Аренда» (ответ 37): расчёта от оборота нет — в эталоне помесячные
	// суммы. Заполняем ветку «по факту предыдущего периода», рядом в форме и так
	// стоит колонка факта. Ветка от оборота остаётся спящей в ExpRentPlan.
	if req.Op == ExpCalcRent {
		for _, r := range rows {
			if !expRentCodePL[r.CodePL] {
				continue
			}
			res.Rows++
			for _, m := range months {
				prev, hasPrev := expFactOf(r, m)
				got := ExpRentPlan(ExpRentInput{PrevFact: prev, HasPrev: hasPrev})
				if !got.FromPrev {
					continue
				}
				before := expPlanOf(r, m)
				if before != nil && *before == got.Amount {
					continue
				}
				res.Diff = append(res.Diff, ExpCalcDiff{
					CodeCFO: r.CodeCFO, CodePL: r.CodePL, CFO: r.CFO, Month: m,
					Before: before, After: got.Amount, Note: "по факту предыдущего периода",
				})
				amt := got.Amount
				cells = append(cells, ExpCell{
					CodeCFO: r.CodeCFO, CodePL: r.CodePL, Month: m,
					Amount: &amt, Source: ExpSourceCalc, Note: "по факту предыдущего периода",
				})
			}
		}
		res.Changed = len(res.Diff)
		if req.Preview {
			return res, nil
		}
		if _, err := s.repo.SaveValues(ctx, in.ID, cells, p.UserID, c.CurrentVersion); err != nil {
			return res, err
		}
		return res, nil
	}

	for _, r := range rows {
		// Расчёт §6.1 наполняет ЗАРПЛАТНУЮ статью пары по юридическому лицу
		// (ответ 40). Налоги от выплат по ответу 38 — самостоятельные статьи со
		// своими суммами, расчётом они не заполняются.
		want := ExpPLSalary
		if pair, ok := ExpPayrollPair(r.LegalEntity); ok {
			want = pair[0]
		}
		if r.CodePL != want {
			continue
		}
		res.Rows++
		rowShare := expParamValue(params, ExpParamPayrollShare, fmt.Sprint(r.CodeCFO))
		if rowShare == 0 {
			rowShare = share
		}
		// Нулевой удельный вес означает, что расчёт для строки не настроен.
		// При заданном весе нулевой результат — валидное значение (например,
		// при нулевом плане продаж) и должен заменить старую ячейку.
		if rowShare <= 0 {
			continue
		}
		rowCap := expParamValue(params, ExpParamPayrollCap, fmt.Sprint(r.CodeCFO))
		if rowCap == 0 {
			rowCap = capDefault
		}
		for _, m := range months {
			byMonth, hasCFO := sales[r.CodeCFO]
			plan, hasPlan := byMonth[m]
			// SalesPlan — разреженная карта: отсутствующий ЦФО или месяц
			// означает «данных нет», а не явный нулевой план. Явный ключ с
			// нулевым значением, напротив, должен пройти расчёт и заменить
			// прежнее значение нулём.
			if !hasCFO || !hasPlan {
				continue
			}
			base, hasBase := expFactOf(r, m)
			got := ExpPayrollFromSales(ExpPayrollInput{
				SalesPlan: plan, Base: base, HasBase: hasBase, Cap: rowCap, Share: rowShare,
			})
			before := expPlanOf(r, m)
			note := ""
			if got.Capped {
				res.Capped++
				note = "ФОТ ограничен порогом"
			}
			if got.NoBase {
				res.NoBase++
				note = "порог не применён, нет базы"
			}
			if before != nil && *before == got.Amount {
				continue
			}
			res.Diff = append(res.Diff, ExpCalcDiff{
				CodeCFO: r.CodeCFO, CodePL: r.CodePL, CFO: r.CFO, Month: m,
				Before: before, After: got.Amount, Note: note,
			})
			amt := got.Amount
			cells = append(cells, ExpCell{
				CodeCFO: r.CodeCFO, CodePL: r.CodePL, Month: m,
				Amount: &amt, Source: ExpSourceCalc, Note: note,
			})
		}
	}
	res.Changed = len(res.Diff)
	if req.Preview {
		return res, nil
	}
	if _, err := s.repo.SaveValues(ctx, in.ID, cells, p.UserID, c.CurrentVersion); err != nil {
		return res, err
	}
	log.Printf("plans/exp: расчёт %s карточки %d: строк %d, ограничено %d, без базы %d",
		req.Op, cardID, res.Changed, res.Capped, res.NoBase)
	return res, nil
}

// expParamValue — значение параметра: сначала переопределение по объекту, затем
// значение на страну (ТЗ §6.1).
func expParamValue(params []ExpParamRow, code, cfo string) float64 {
	for _, p := range params {
		if p.ParamCode != code {
			continue
		}
		if cfo != "" && p.ScopeKind == "cfo" && p.ScopeValue == cfo {
			return p.Value
		}
		if cfo == "" && p.ScopeKind != "cfo" {
			return p.Value
		}
	}
	return 0
}

// expFactOf — факт того же месяца прошлого года: база ограничения по умолчанию.
func expFactOf(r ExpRow, month int) (float64, bool) {
	for _, v := range r.Values {
		if v.Scenario == ExpScenarioFact && v.Month == month && v.Amount != nil {
			return *v.Amount, true
		}
	}
	return 0, false
}

// expPlanOf — текущее плановое значение ячейки (для предпросмотра «было → стало»).
func expPlanOf(r ExpRow, month int) *float64 {
	for _, v := range r.Values {
		if v.Scenario == ExpScenarioPlan && v.Month == month {
			return v.Amount
		}
	}
	return nil
}

func allMonths() []int {
	out := make([]int, 0, 12)
	for m := 1; m <= 12; m++ {
		out = append(out, m)
	}
	return out
}
