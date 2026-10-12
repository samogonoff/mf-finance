package plans

import (
	"context"
	"testing"
)

type payrollCalcRepoStub struct {
	ExpRepo
	row      ExpRow
	params   []ExpParamRow
	saved    []ExpCell
	instance ExpInstance
}

func (s *payrollCalcRepoStub) EnsureInstance(_ context.Context, in ExpInstance, _ int64) (ExpInstance, error) {
	if s.instance.ID != 0 {
		return s.instance, nil
	}
	in.ID = 1
	s.instance = in
	return in, nil
}

func (s *payrollCalcRepoStub) Rows(context.Context, int64, ExpFilter) ([]ExpRow, int, error) {
	return []ExpRow{s.row}, 1, nil
}

func (s *payrollCalcRepoStub) Params(context.Context, int64) ([]ExpParamRow, error) {
	return s.params, nil
}

func (s *payrollCalcRepoStub) SaveValues(_ context.Context, _ int64, cells []ExpCell, _ int64, _ int) (int, error) {
	s.saved = append([]ExpCell(nil), cells...)
	return len(cells), nil
}

type payrollCalcCardStoreStub struct {
	CardStore
	card Card
}

func (s payrollCalcCardStoreStub) Card(context.Context, int64) (Card, error) { return s.card, nil }

type payrollCalcSourceStub struct {
	ExpDataSource
	sales map[int]map[int]float64
}

func (s payrollCalcSourceStub) SalesPlan(context.Context, string, int) (map[int]map[int]float64, error) {
	return s.sales, nil
}

func TestExpServiceCalc_PersistsZeroWhenSalesPlanIsZero(t *testing.T) {
	old := 100.0
	base := 1000.0
	repo := &payrollCalcRepoStub{
		row: ExpRow{CodeCFO: 101, CodePL: ExpPLSalary, Values: []ExpValue{
			{Scenario: ExpScenarioFact, Month: 1, Amount: &base},
			{Scenario: ExpScenarioPlan, Month: 1, Amount: &old},
		}},
		params: []ExpParamRow{
			{ParamCode: ExpParamPayrollShare, ScopeKind: "country", Value: 0.1},
			{ParamCode: ExpParamPayrollCap, ScopeKind: "country", Value: 1.06},
		},
	}
	cards := NewCardService(payrollCalcCardStoreStub{card: Card{
		ID: 7, FormCode: TemplateExp, ScopeKey: "R-BY", Year: 2026,
		Currency: "BYN", Status: CardDraft, StepCode: "1.1",
	}})
	svc := NewExpService(repo, cards, nil).WithSalesPlan(payrollCalcSourceStub{
		sales: map[int]map[int]float64{101: {1: 0}},
	})

	got, err := svc.Calc(context.Background(), adminP, 7, ExpCalcRequest{
		Op: ExpCalcPayroll, Months: []int{1}, Preview: false,
	})
	if err != nil {
		t.Fatal(err)
	}
	if got.Changed != 1 || len(repo.saved) != 1 {
		t.Fatalf("нулевой результат должен заменить старое значение: result=%+v saved=%+v", got, repo.saved)
	}
	if repo.saved[0].Amount == nil || *repo.saved[0].Amount != 0 {
		t.Fatalf("ожидался явный ноль, получили %+v", repo.saved[0])
	}
}

func TestExpServiceCalc_SkipsMissingSalesPlan(t *testing.T) {
	old := 100.0
	base := 1000.0
	repo := &payrollCalcRepoStub{
		row: ExpRow{CodeCFO: 101, CodePL: ExpPLSalary, Values: []ExpValue{
			{Scenario: ExpScenarioFact, Month: 1, Amount: &base},
			{Scenario: ExpScenarioPlan, Month: 1, Amount: &old},
		}},
		params: []ExpParamRow{
			{ParamCode: ExpParamPayrollShare, ScopeKind: "country", Value: 0.1},
			{ParamCode: ExpParamPayrollCap, ScopeKind: "country", Value: 1.06},
		},
	}
	cards := NewCardService(payrollCalcCardStoreStub{card: Card{
		ID: 7, FormCode: TemplateExp, ScopeKey: "R-BY", Year: 2026,
		Currency: "BYN", Status: CardDraft, StepCode: "1.1",
	}})
	svc := NewExpService(repo, cards, nil).WithSalesPlan(payrollCalcSourceStub{
		sales: map[int]map[int]float64{},
	})

	got, err := svc.Calc(context.Background(), adminP, 7, ExpCalcRequest{
		Op: ExpCalcPayroll, Months: []int{1}, Preview: false,
	})
	if err != nil {
		t.Fatal(err)
	}
	if got.Changed != 0 || len(repo.saved) != 0 {
		t.Fatalf("отсутствующий план продаж не должен очищать старое значение: result=%+v saved=%+v", got, repo.saved)
	}
}

// ТЗ §6.1: порог ограничивает БАЗУ НАЧИСЛЕНИЯ, а не сумму выплат.
// «Выше 106 % никому не могут выплатить планы».
func TestExpPayrollFromSales_CapLimitsBaseNotAmount(t *testing.T) {
	// План 1 000, база 800, порог 1,06 → предел базы 848 < 1 000, значит
	// начисляем от 848, а не от плана.
	got := ExpPayrollFromSales(ExpPayrollInput{
		SalesPlan: 1000, Base: 800, HasBase: true, Cap: 1.06, Share: 0.1,
	})
	if !near(got.Amount, 84.8) {
		t.Fatalf("ФОТ %v, ожидали 84,80 = MIN(1000; 800×1,06) × 0,1", got.Amount)
	}
	if !got.Capped {
		t.Fatal("сработавшее ограничение обязано быть видно: строка подсвечивается")
	}

	// План ниже предела — ограничение не срабатывает.
	got = ExpPayrollFromSales(ExpPayrollInput{
		SalesPlan: 700, Base: 800, HasBase: true, Cap: 1.06, Share: 0.1,
	})
	if !near(got.Amount, 70) || got.Capped {
		t.Fatalf("ФОТ %v (capped=%v), ожидали 70 без ограничения", got.Amount, got.Capped)
	}
}

// Базы по магазину нет — ограничение не применяется, считаем от плана, и строка
// помечается отдельно (ТЗ §6.1: «строка помечается „порог не применён, нет базы“»).
func TestExpPayrollFromSales_NoBase(t *testing.T) {
	got := ExpPayrollFromSales(ExpPayrollInput{SalesPlan: 1000, HasBase: false, Cap: 1.06, Share: 0.1})
	if !near(got.Amount, 100) {
		t.Fatalf("ФОТ %v, без базы считаем от плана: 1000 × 0,1", got.Amount)
	}
	if !got.NoBase {
		t.Fatal("строка без базы обязана помечаться — это не то же самое, что «порог не сработал»")
	}
	if got.Capped {
		t.Fatal("без базы ограничение применяться не может")
	}
}

// Порог по умолчанию — 1,06 (ТЗ §6.1).
func TestExpPayrollFromSales_DefaultCap(t *testing.T) {
	if ExpPayrollCapDefault != 1.06 {
		t.Fatalf("порог по умолчанию %v, ТЗ §6.1 задаёт 1,06", ExpPayrollCapDefault)
	}
	got := ExpPayrollFromSales(ExpPayrollInput{SalesPlan: 1000, Base: 800, HasBase: true, Share: 0.1})
	if !near(got.Amount, 84.8) {
		t.Fatalf("без явного порога должен применяться 1,06: %v", got.Amount)
	}
}

// Удельный вес не задан — считать нечего, и подставлять ноль как «расход равен
// нулю» нельзя: ноль в форме означает явный ввод.
func TestExpPayrollFromSales_NoShare(t *testing.T) {
	got := ExpPayrollFromSales(ExpPayrollInput{SalesPlan: 1000, Base: 800, HasBase: true})
	if got.Amount != 0 || got.Capped || got.NoBase {
		t.Fatalf("без удельного веса результата быть не должно: %+v", got)
	}
}

// ТЗ §6.2 с уточнением ответа 37: расчёт от оборота — СПЯЩАЯ ветка, срабатывает
// только при заполненных пороге и ставке. Иначе берётся факт предыдущего периода.
func TestExpRentPlan(t *testing.T) {
	// Справочника нет — факт предыдущего периода, строка помечается.
	got := ExpRentPlan(ExpRentInput{PrevFact: 700, HasPrev: true, SalesPlan: 100000})
	if !near(got.Amount, 700) || !got.FromPrev {
		t.Fatalf("без договора ожидали факт прошлого месяца с пометкой: %+v", got)
	}

	// Договор задан — фиксированная часть плюс переменная сверх порога.
	got = ExpRentPlan(ExpRentInput{
		HasContract: true, FixedPart: 500, Threshold: 4000, Rate: 0.05, SalesPlan: 5000,
	})
	if !near(got.Amount, 550) { // 500 + (5000 − 4000) × 0,05
		t.Fatalf("аренда по договору %v, ожидали 550", got.Amount)
	}

	// Оборот ниже порога — переменной части нет, минус не уходит.
	got = ExpRentPlan(ExpRentInput{
		HasContract: true, FixedPart: 500, Threshold: 4000, Rate: 0.05, SalesPlan: 1000,
	})
	if !near(got.Amount, 500) {
		t.Fatalf("ниже порога ожидали только фиксированную часть: %v", got.Amount)
	}

	// Ни договора, ни факта — пусто, а не ноль.
	if got := ExpRentPlan(ExpRentInput{SalesPlan: 1000}); got.Amount != 0 || got.FromPrev {
		t.Fatalf("без данных результата быть не должно: %+v", got)
	}
}
