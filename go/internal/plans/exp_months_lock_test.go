package plans

import "testing"

// Прошедшие месяцы плана закрыты от записи: по ним подтягивается факт.
func TestExpDropPastMonths(t *testing.T) {
	cells := []ExpCell{{Month: 1}, {Month: 9}, {Month: 10}, {Month: 12}}

	got := expDropPastMonths(Card{Month: 10}, cells)
	if len(got) != 2 || got[0].Month != 10 || got[1].Month != 12 {
		t.Fatalf("карточка октября: остаются октябрь и декабрь, получили %+v", got)
	}
	if len(expDropPastMonths(Card{Month: 1}, cells)) != 4 {
		t.Fatal("карточка января: ничего не отбрасывается")
	}
	if len(expDropPastMonths(Card{}, cells)) != 4 {
		t.Fatal("месяц карточки не задан: ничего не отбрасывается")
	}
	if len(cells) != 4 || cells[0].Month != 1 {
		t.Fatal("исходный срез не должен меняться")
	}
}

// Массовые действия в виде «весь год» не должны задевать прошедшие месяцы.
func TestExpBulkYearSkipsPastMonths(t *testing.T) {
	amt := 5.0
	cells, err := expBulkCells(ExpBulkRequest{Action: ExpBulkSpreadYear, CodeCFO: 1, CodePL: 2, Amount: &amt})
	if err != nil {
		t.Fatal(err)
	}
	got := expDropPastMonths(Card{Month: 11}, cells)
	if len(got) != 2 || got[0].Month != 11 {
		t.Fatalf("ожидали ноябрь–декабрь, получили %+v", got)
	}
}