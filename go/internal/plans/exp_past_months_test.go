package plans

import (
	"context"
	"strings"
	"testing"
)

func pastMonthsSvc(repo *payrollCalcRepoStub, sales map[int]map[int]float64) *ExpService {
	cards := NewCardService(payrollCalcCardStoreStub{card: Card{
		ID: 7, FormCode: TemplateExp, ScopeKey: "R-BY", Year: 2026, Month: 10,
		Currency: "BYN", Status: CardDraft, StepCode: "1.1",
	}})
	return NewExpService(repo, cards, nil).WithSalesPlan(payrollCalcSourceStub{sales: sales})
}

// Расчёт не трогает прошедшие месяцы ни в предпросмотре, ни в записи — иначе «Рассчитать» (по умолчанию
// на все месяцы) перезаписывало бы план месяцев, поля которых на форме закрыты.
func TestExpServiceCalc_SkipsPastMonths(t *testing.T) {
	base := 1000.0
	repo := &payrollCalcRepoStub{
		row: ExpRow{CodeCFO: 101, CodePL: ExpPLSalary, LegalEntity: "МАРК Формэль ООО", Values: []ExpValue{
			{Scenario: ExpScenarioFact, Month: 1, Amount: &base},
			{Scenario: ExpScenarioFact, Month: 10, Amount: &base},
		}},
		params: []ExpParamRow{
			{ParamCode: ExpParamPayrollShare, ScopeKind: "country", Value: 0.1},
			{ParamCode: ExpParamPayrollCap, ScopeKind: "country", Value: 1.06},
		},
	}
	svc := pastMonthsSvc(repo, map[int]map[int]float64{101: {1: 1000, 10: 1000}})

	prev, err := svc.Calc(context.Background(), adminP, 7, ExpCalcRequest{Op: ExpCalcPayroll, Preview: true})
	if err != nil {
		t.Fatal(err)
	}
	if len(prev.Diff) != 1 || prev.Diff[0].Month != 10 {
		t.Fatalf("предпросмотр должен показывать только месяцы с октября: %+v", prev.Diff)
	}
	if _, err := svc.Calc(context.Background(), adminP, 7, ExpCalcRequest{Op: ExpCalcPayroll}); err != nil {
		t.Fatal(err)
	}
	if len(repo.saved) != 1 || repo.saved[0].Month != 10 {
		t.Fatalf("записаться должен только октябрь: %+v", repo.saved)
	}
}

// Импорт Excel принимает только открытые месяцы; прошедшие пропускаются и объясняются одной строкой отчёта.
func TestExpServiceImport_SkipsPastMonths(t *testing.T) {
	repo := &payrollCalcRepoStub{row: ExpRow{CodeCFO: 101, CodePL: 24}}
	svc := pastMonthsSvc(repo, nil)

	header := []string{"code_cfo", "cfo", "code_pl", "expense", "group_pl", "group_cfo2", "legal_entity",
		"2026-01", "2026-02", "2026-03", "2026-04", "2026-05", "2026-06",
		"2026-07", "2026-08", "2026-09", "2026-10", "2026-11", "2026-12"}
	line := []string{"101", "Магазин", "24", "Заработная плата", "", "", "",
		"500", "", "", "", "", "", "", "", "", "700", "", ""}
	buf, err := writeXlsx("Расходы ЦП", header, [][]string{line})
	if err != nil {
		t.Fatal(err)
	}

	for _, preview := range []bool{true, false} {
		repo.saved = nil
		res, err := svc.ImportForm(context.Background(), adminP, 7, buf, preview)
		if err != nil {
			t.Fatal(err)
		}
		if len(res.Changes) != 1 || res.Changes[0].Month != 10 {
			t.Fatalf("preview=%v: изменения только за октябрь: %+v", preview, res.Changes)
		}
		var noted bool
		for _, is := range res.Issues {
			noted = noted || strings.Contains(is.Message, "прошедшие месяцы")
		}
		if !noted {
			t.Fatalf("preview=%v: отчёт должен объяснить пропуск прошедших месяцев: %+v", preview, res.Issues)
		}
		if !preview && (len(repo.saved) != 1 || repo.saved[0].Month != 10) {
			t.Fatalf("записаться должен только октябрь: %+v", repo.saved)
		}
	}
}