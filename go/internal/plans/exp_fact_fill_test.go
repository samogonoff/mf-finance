package plans

import (
	"context"
	"testing"
)

func fillRow(vals ...ExpValue) ExpRow {
	return ExpRow{CodeCFO: 101, CodePL: 61, CFO: "Магазин", Values: vals}
}

func fv(sc string, m int, v float64) ExpValue { return ExpValue{Scenario: sc, Month: m, Amount: &v} }

func TestExpFactFillCells_PrevMonthAndSameMonthLY(t *testing.T) {
	// План 2026, карточка ноября: прошлый месяц — октябрь 2026 (fact_cur), прошлый год — fact.
	row := fillRow(fv(ExpScenarioFactCur, 10, 100), fv(ExpScenarioFact, 11, 80), fv(ExpScenarioFact, 12, 90))
	months := []int{11, 12}

	cells, diff, touched, err := expFactFillCells([]ExpRow{row}, 2026, 11, months, ExpFactFillParams{Source: ExpFillPrevMonth})
	if err != nil || len(cells) != 2 || touched != 1 || diff[0].After != 100 || diff[1].After != 100 || diff[0].Note != "факт 10.2026" {
		t.Fatalf("прошлый месяц на все плановые месяцы: %+v %+v %v", cells, diff, err)
	}
	cells, _, _, err = expFactFillCells([]ExpRow{row}, 2026, 11, months, ExpFactFillParams{Source: ExpFillSameMonthLY})
	if err != nil || len(cells) != 2 || *cells[0].Amount != 80 || *cells[1].Amount != 90 {
		t.Fatalf("тот же месяц прошлого года помесячно: %+v %v", cells, err)
	}
}

func TestExpFactFillCells_JanuaryUsesDecemberOfPreviousYear(t *testing.T) {
	row := fillRow(fv(ExpScenarioFact, 12, 70))
	cells, _, _, err := expFactFillCells([]ExpRow{row}, 2026, 1, []int{1, 2}, ExpFactFillParams{Source: ExpFillPrevMonth})
	if err != nil || len(cells) != 2 || *cells[0].Amount != 70 {
		t.Fatalf("для января прошлый месяц — декабрь прошлого года: %+v %v", cells, err)
	}
}

func TestExpFactFillCells_YearMonthCoefAndSkips(t *testing.T) {
	plan := 110.0
	row := fillRow(fv(ExpScenarioFact, 5, 100), ExpValue{Scenario: ExpScenarioPlan, Month: 11, Amount: &plan})
	noFact := fillRow() // строка без факта не меняется
	coef := 1.1
	cells, diff, touched, err := expFactFillCells([]ExpRow{row, noFact}, 2026, 11, []int{11, 12},
		ExpFactFillParams{Source: ExpFillYearMonth, Year: 2025, Month: 5, Coef: &coef})
	// 100 × 1,1 = 110 — ноябрь уже равен (в отчёт не попадает), декабрь заполняется.
	if err != nil || len(cells) != 1 || cells[0].Month != 12 || *cells[0].Amount != 110 || touched != 1 || len(diff) != 1 {
		t.Fatalf("коэффициент, равные значения и строки без факта: %+v %+v %d %v", cells, diff, touched, err)
	}
	for _, bad := range []ExpFactFillParams{
		{Source: ExpFillYearMonth, Year: 2024, Month: 5},
		{Source: ExpFillYearMonth, Year: 2025, Month: 13},
		{Source: "unknown"},
		{Source: ExpFillPrevMonth, Coef: func() *float64 { v := -1.0; return &v }()},
	} {
		if _, _, _, err := expFactFillCells([]ExpRow{row}, 2026, 11, []int{11}, bad); err == nil {
			t.Errorf("параметры %+v должны отклоняться", bad)
		}
	}
	// Выбранный год — плановый: факт берётся из fact_cur.
	cur := fillRow(fv(ExpScenarioFactCur, 3, 55))
	cells, _, _, err = expFactFillCells([]ExpRow{cur}, 2026, 11, []int{11}, ExpFactFillParams{Source: ExpFillYearMonth, Year: 2026, Month: 3})
	if err != nil || len(cells) != 1 || *cells[0].Amount != 55 {
		t.Fatalf("факт планового года: %+v %v", cells, err)
	}
}

// Предпросмотр ничего не пишет; применение пишет только открытые месяцы (прошедшие закрыты).
func TestExpServiceCalc_FactFill(t *testing.T) {
	repo := &payrollCalcRepoStub{row: fillRow(fv(ExpScenarioFactCur, 9, 40))}
	svc := pastMonthsSvc(repo, nil) // карточка октября, годa 2026

	prev, err := svc.Calc(context.Background(), adminP, 7, ExpCalcRequest{Op: ExpCalcFactFill, Preview: true,
		Fill: &ExpFactFillParams{Source: ExpFillPrevMonth}})
	if err != nil || len(prev.Diff) != 3 || prev.Diff[0].Month != 10 || len(repo.saved) != 0 {
		t.Fatalf("предпросмотр: октябрь–декабрь, без записи: %+v %v saved=%v", prev, err, repo.saved)
	}
	if _, err := svc.Calc(context.Background(), adminP, 7, ExpCalcRequest{Op: ExpCalcFactFill,
		Fill: &ExpFactFillParams{Source: ExpFillPrevMonth}}); err != nil {
		t.Fatal(err)
	}
	if len(repo.saved) != 3 || repo.saved[0].Month != 10 || *repo.saved[0].Amount != 40 {
		t.Fatalf("запись: %+v", repo.saved)
	}
	if _, err := svc.Calc(context.Background(), adminP, 7, ExpCalcRequest{Op: ExpCalcFactFill, Months: []int{1, 2},
		Fill: &ExpFactFillParams{Source: ExpFillPrevMonth}}); err == nil {
		t.Fatal("прошедшие месяцы планировать нельзя")
	}
	if _, err := svc.Calc(context.Background(), adminP, 7, ExpCalcRequest{Op: ExpCalcFactFill}); err == nil {
		t.Fatal("без источника факта подстановка невозможна")
	}
}
