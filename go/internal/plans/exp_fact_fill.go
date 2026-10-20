package plans

import (
	"context"
	"errors"
	"fmt"
	"log"
	"math"
)

// Подстановка плана из факта: «взять расходы прошлого месяца» или «расходы того же месяца прошлого года»
// (или другого года и месяца) как стартовое значение плана. Факт в форме подтягивается из источника сам и
// не вводится; здесь он только служит основой для плана, который потом правится руками.

// ExpCalcFactFill — вид расчёта: заполнить план фактом.
const ExpCalcFactFill = "fact_fill"

// Источник факта для подстановки.
const (
	ExpFillPrevMonth   = "prev_month"    // факт прошлого месяца (для января — декабрь прошлого года)
	ExpFillSameMonthLY = "same_month_ly" // факт того же календарного месяца прошлого года
	ExpFillYearMonth   = "ym"            // факт выбранных года и месяца
)

// Как раскладывать факт по плановым месяцам.
const (
	ExpFillModeSameMonth = "same_month" // каждый плановый месяц ← факт ТОГО ЖЕ месяца выбранного года
	ExpFillModeOneMonth  = "one_month"  // все плановые месяцы ← факт ОДНОГО выбранного месяца
)

// ExpFactFillParams — параметры подстановки.
type ExpFactFillParams struct {
	Source string   `json:"source"`
	Year   int      `json:"year"`  // для source = ym
	Month  int      `json:"month"` // для source = ym и режима one_month
	Mode   string   `json:"mode"`  // same_month | one_month (для ym; для остальных источников задаётся видом)
	Coef   *float64 `json:"coef"`  // множитель, по умолчанию 1
}

// ExpCalcFilter — отбор строк для расчёта (те же поля, что фильтры формы).
type ExpCalcFilter struct {
	Search      string `json:"search"`
	GroupPL     string `json:"group_pl"`
	CodePL      int    `json:"code_pl"`
	CodeCFO     int    `json:"code_cfo"`
	City        string `json:"city"`
	LegalEntity string `json:"legal_entity"`
	StoreType   string `json:"store_type"`
	RegManager  string `json:"reg_manager"`
}

func (f *ExpCalcFilter) toFilter() ExpFilter {
	if f == nil {
		return ExpFilter{}
	}
	return ExpFilter{Search: f.Search, GroupPL: f.GroupPL, CodePL: f.CodePL, CodeCFO: f.CodeCFO, City: f.City,
		LegalEntity: f.LegalEntity, StoreType: f.StoreType, RegManager: f.RegManager}
}

// expFactAt — факт строки за год и месяц: прошлый год лежит в сценарии fact, плановый — в fact_cur.
func expFactAt(r ExpRow, planYear, year, month int) (float64, bool) {
	scenario := ""
	switch year {
	case planYear - 1:
		scenario = ExpScenarioFact
	case planYear:
		scenario = ExpScenarioFactCur
	default:
		return 0, false
	}
	return expCellOf(r, scenario, month)
}

// expFillSource — из какого года и месяца берётся факт и как раскладывается по месяцам плана.
func expFillSource(p ExpFactFillParams, planYear, cardMonth int) (year, month int, mode string, err error) {
	switch p.Source {
	case ExpFillPrevMonth:
		if cardMonth <= 1 {
			return planYear - 1, 12, ExpFillModeOneMonth, nil
		}
		return planYear, cardMonth - 1, ExpFillModeOneMonth, nil
	case ExpFillSameMonthLY:
		return planYear - 1, 0, ExpFillModeSameMonth, nil
	case ExpFillYearMonth:
		if p.Year != planYear-1 && p.Year != planYear {
			return 0, 0, "", fmt.Errorf("факт есть за %d и %d годы", planYear-1, planYear)
		}
		mode = p.Mode
		if mode == "" {
			mode = ExpFillModeOneMonth
		}
		if mode != ExpFillModeSameMonth && mode != ExpFillModeOneMonth {
			return 0, 0, "", fmt.Errorf("неизвестный режим %q", p.Mode)
		}
		if mode == ExpFillModeOneMonth && (p.Month < 1 || p.Month > 12) {
			return 0, 0, "", fmt.Errorf("месяц вне диапазона: %d", p.Month)
		}
		return p.Year, p.Month, mode, nil
	}
	return 0, 0, "", fmt.Errorf("неизвестный источник факта %q", p.Source)
}

// expFactFillCells — раскладка подстановки в ячейки и предпросмотр «было → стало». Чистая функция: без БД,
// чтобы правила (откуда берётся факт, какие месяцы закрыты) проверялись тестом. Строки без факта в источнике
// не меняются, равные значения в отчёт не попадают.
func expFactFillCells(rows []ExpRow, planYear, cardMonth int, months []int, p ExpFactFillParams) ([]ExpCell, []ExpCalcDiff, int, error) {
	srcYear, srcMonth, mode, err := expFillSource(p, planYear, cardMonth)
	if err != nil {
		return nil, nil, 0, err
	}
	coef := 1.0
	if p.Coef != nil {
		coef = *p.Coef
	}
	if coef < 0 || math.IsNaN(coef) || math.IsInf(coef, 0) {
		return nil, nil, 0, fmt.Errorf("коэффициент должен быть неотрицательным числом")
	}
	var cells []ExpCell
	var diff []ExpCalcDiff
	touched := 0
	for _, r := range rows {
		changed := false
		for _, m := range months {
			from := srcMonth
			if mode == ExpFillModeSameMonth {
				from = m
			}
			v, ok := expFactAt(r, planYear, srcYear, from)
			if !ok {
				continue
			}
			amt := math.Round(v*coef*100) / 100
			before := expPlanOf(r, m)
			if before != nil && *before == amt {
				continue
			}
			note := fmt.Sprintf("факт %02d.%d", from, srcYear)
			if coef != 1 {
				note += fmt.Sprintf(" × %g", coef)
			}
			diff = append(diff, ExpCalcDiff{CodeCFO: r.CodeCFO, CodePL: r.CodePL, CFO: r.CFO, Month: m, Before: before, After: amt, Note: note})
			a := amt
			cells = append(cells, ExpCell{CodeCFO: r.CodeCFO, CodePL: r.CodePL, Month: m, Amount: &a, Source: ExpSourceCalc, Note: note})
			changed = true
		}
		if changed {
			touched++
		}
	}
	return cells, diff, touched, nil
}

// factFill — подстановка плана из факта по строкам формы (с отбором). Предпросмотр ничего не пишет;
// прошедшие месяцы плана не заполняются (по ним подтягивается факт, планировать их нельзя).
func (s *ExpService) factFill(ctx context.Context, p Principal, c Card, in ExpInstance, req ExpCalcRequest, res ExpCalcResult) (ExpCalcResult, error) {
	if req.Fill == nil {
		return res, errors.New("не указано, откуда брать факт")
	}
	months := req.Months
	if len(months) == 0 {
		months = allMonths()
	}
	months = expOpenMonths(c, months)
	if len(months) == 0 {
		return res, errors.New("все выбранные месяцы уже прошли: по ним подтягивается факт, планировать их нельзя")
	}
	rows, err := expAllRowsFiltered(ctx, s.repo, in.ID, req.Filter.toFilter())
	if err != nil {
		return res, err
	}
	cells, diff, touched, err := expFactFillCells(rows, in.PlanYear, c.Month, months, *req.Fill)
	if err != nil {
		return res, err
	}
	res.Rows, res.Changed, res.Diff = touched, len(diff), diff
	if res.Diff == nil {
		res.Diff = []ExpCalcDiff{}
	}
	if req.Preview || len(cells) == 0 {
		return res, nil
	}
	if _, err := s.saveValues(ctx, c, in, p, cells); err != nil {
		return res, err
	}
	log.Printf("plans/exp: план из факта карточки %d (%s): ячеек %d, строк %d", c.ID, req.Fill.Source, len(cells), touched)
	return res, nil
}
