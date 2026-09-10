package plans

import (
	"context"
	"testing"
)

func f(v float64) *float64 { return &v }

// Пусто и ноль — разные вещи (ТЗ §7.8, валидация E-11). Итог считает только
// заполненные месяцы, иначе незаполненная строка выглядит как строка с нулём.
func TestExpRowTotals_EmptyIsNotZero(t *testing.T) {
	r := ExpRow{Values: []ExpValue{
		{Scenario: ExpScenarioPlan, Month: 1, Amount: f(100)},
		{Scenario: ExpScenarioPlan, Month: 2, Amount: f(0)},
		{Scenario: ExpScenarioPlan, Month: 3, Amount: nil},
		{Scenario: ExpScenarioFact, Month: 1, Amount: f(999)}, // факт в итог плана не входит
	}}
	if got := r.PlanTotal(); got != 100 {
		t.Fatalf("итог плана = %v, ожидали 100 (факт и пустые ячейки не считаются)", got)
	}
	if got := r.FilledMonths(); got != 2 {
		t.Fatalf("заполнено месяцев = %d, ожидали 2 (явный ноль заполнен, nil — нет)", got)
	}
}

// Порядок групп PL берётся из ТЗ §7.3, а не по алфавиту.
func TestSortExpRows_GroupOrderFromSpec(t *testing.T) {
	rows := []ExpRow{
		{GroupPL: "АРЕНДА", CodeCFO: 101, CodePL: 54},
		{GroupPL: "ВЫПЛАТЫ РАБОТНИКАМ", CodeCFO: 102, CodePL: 24},
		{GroupPL: "ВЫПЛАТЫ РАБОТНИКАМ", CodeCFO: 101, CodePL: 26},
		{GroupPL: "ЧТО-ТО НОВОЕ", CodeCFO: 101, CodePL: 99},
	}
	SortExpRows(ExpKindRetail, rows)
	if rows[0].GroupPL != "ВЫПЛАТЫ РАБОТНИКАМ" || rows[0].CodeCFO != 101 {
		t.Fatalf("первой должна идти первая группа ТЗ и меньший ЦФО, получили %+v", rows[0])
	}
	if rows[2].GroupPL != "АРЕНДА" {
		t.Fatalf("АРЕНДА по ТЗ идёт восьмой, до неё только ВЫПЛАТЫ, получили %+v", rows[2])
	}
	// Группа, которой в ТЗ нет, уходит в конец, а не роняет сортировку.
	if rows[3].GroupPL != "ЧТО-ТО НОВОЕ" {
		t.Fatalf("неизвестная группа должна быть последней, получили %+v", rows[3])
	}
}

// У маркетплейсов групп восемь, у розницы четырнадцать (ТЗ §7.3).
func TestExpGroupOrder_ByKind(t *testing.T) {
	if n := len(ExpGroupOrder(ExpKindRetail)); n != 14 {
		t.Fatalf("групп розницы %d, ТЗ §7.3 задаёт 14", n)
	}
	if n := len(ExpGroupOrder(ExpKindMP)); n != 8 {
		t.Fatalf("групп МП %d, ТЗ §7.3 задаёт 8", n)
	}
}

// Ключ области карточки несёт вид формы и страну.
func TestExpKindOf(t *testing.T) {
	for _, c := range []struct {
		scope, kind, country string
	}{
		{"R-BY", ExpKindRetail, "BY"},
		{"MP-RU", ExpKindMP, "RU"},
		{"MP-KZ", ExpKindMP, "KZ"},
		{"MP-UZ", ExpKindMP, "UZ"},
	} {
		k, cn := expKindOf(c.scope)
		if k != c.kind || cn != c.country {
			t.Fatalf("%s → %s/%s, ожидали %s/%s", c.scope, k, cn, c.kind, c.country)
		}
	}
}

// «Размножить до конца года» начинает с текущего месяца, «на год» — с января,
// «очистить» ставит пусто во все двенадцать (ТЗ §7.8).
func TestExpBulkCells(t *testing.T) {
	tail, err := expBulkCells(ExpBulkRequest{Action: ExpBulkSpreadTail, Month: 10, Amount: f(500)})
	if err != nil {
		t.Fatal(err)
	}
	if len(tail) != 3 || tail[0].Month != 10 || tail[2].Month != 12 {
		t.Fatalf("до конца года с октября — три месяца 10..12, получили %d: %+v", len(tail), tail)
	}

	year, err := expBulkCells(ExpBulkRequest{Action: ExpBulkSpreadYear, Amount: f(500)})
	if err != nil {
		t.Fatal(err)
	}
	if len(year) != 12 || year[0].Month != 1 {
		t.Fatalf("на год — двенадцать месяцев с января, получили %d", len(year))
	}

	clear, err := expBulkCells(ExpBulkRequest{Action: ExpBulkClearRow})
	if err != nil {
		t.Fatal(err)
	}
	if len(clear) != 12 {
		t.Fatalf("очистка строки — двенадцать ячеек, получили %d", len(clear))
	}
	for _, c := range clear {
		if c.Amount != nil {
			t.Fatalf("очистка обязана ставить ПУСТО, а не ноль: %+v", c)
		}
	}

	if _, err := expBulkCells(ExpBulkRequest{Action: ExpBulkSpreadYear}); err == nil {
		t.Fatal("размножение без значения должно быть ошибкой")
	}
	if _, err := expBulkCells(ExpBulkRequest{Action: "нет такого"}); err == nil {
		t.Fatal("неизвестное действие должно быть ошибкой")
	}
}

// Фикстуры дают строки обоих видов формы: без них форма не открывается без VPN.
func TestExpMockSource(t *testing.T) {
	src := NewExpSource(true, nil, ExpTables{})
	cells, err := src.Costs(context.Background(), ExpKindMP, "RU", []int{2025})
	if err != nil {
		t.Fatal(err)
	}
	if len(cells) == 0 {
		t.Fatal("фикстура МП пуста")
	}
	seen := map[[2]int]bool{}
	for _, c := range cells {
		if c.Month < 1 || c.Month > 12 {
			t.Fatalf("месяц вне диапазона: %+v", c)
		}
		if c.Scenario != "Факт" {
			t.Fatalf("фикстура обязана давать только факт, план — это ввод: %+v", c)
		}
		seen[[2]int{c.CodeCFO, c.CodePL}] = true
	}
	if len(seen) < 5 {
		t.Fatalf("пар «ЦФО × статья» в фикстуре %d — мало для проверки сетки", len(seen))
	}
	if _, err := src.Cfo(context.Background(), ExpKindRetail, "BY"); err != nil {
		t.Fatalf("справочник ЦФО фикстуры: %v", err)
	}
}

// Служебные коды PL и история до 2024 отбрасываются (ТЗ §2.1).
func TestExpDropRules(t *testing.T) {
	for _, code := range []int{50, 93, 1} {
		if !expDropCodePL[code] {
			t.Fatalf("код PL %d должен отбрасываться по ТЗ §2.1", code)
		}
	}
	if expDropCodePL[24] {
		t.Fatal("код PL 24 (зарплата) отбрасываться не должен")
	}
	if expCutoffYear != 2024 {
		t.Fatalf("отсечка %d, ТЗ §2.1 требует отбрасывать строки не позже 31.12.2023", expCutoffYear)
	}
	if expScenarioRename["Стратегический бюджет"] != "План" || expScenarioRename["Факт по платежам"] != "Факт" {
		t.Fatal("переименование сценариев не соответствует ТЗ §2.1 п.3")
	}
}
