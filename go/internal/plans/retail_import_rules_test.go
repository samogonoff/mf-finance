package plans

import "testing"

// Строки-итоги источника не должны попадать в справочник магазинов: их CodeCFO
// заполнен, и каждый магазин попадал в сумму дважды (правка 08.09.2026 п. 34).
func TestIsRetailTotalRow(t *testing.T) {
	cases := []struct {
		name  string
		store RetailStore
		want  bool
	}{
		{"итог по стране", RetailStore{Country: "Розница РБ Итог"}, true},
		{"итог по статусу", RetailStore{LFLStatus: "LFL Итог"}, true},
		{"итог в нижнем регистре", RetailStore{Country: "розница уз итог"}, true},
		{"обычный магазин", RetailStore{Country: "BY", LFLStatus: "LFL"}, false},
		// «Итог» только суффиксом: магазин с таким словом внутри названия страны
		// или статуса выбрасывать нельзя.
		{"слово внутри", RetailStore{Country: "Итоговая область"}, false},
	}
	for _, c := range cases {
		if got := isRetailTotalRow(c.store); got != c.want {
			t.Errorf("%s: got %v, want %v", c.name, got, c.want)
		}
	}
}

// У группы «Розница закрыты» в колонке страны стоит название группы: страну
// берём по коду ЦФО, статус ставим «закрыт» (правка 08.09.2026 п. 35).
func TestRetailStoreCountryAndLFL_ClosedGroup(t *testing.T) {
	closed := RetailStore{CodeCFO: 512, GroupCFO1: "Розница закрыты", Country: "Розница закрыты", LFLStatus: "lfl"}
	cfo := map[int]string{512: "RU"}

	if got := retailStoreCountry(closed, cfo); got != "RU" {
		t.Errorf("страна закрытого магазина: got %q, want RU", got)
	}
	if got := retailStoreLFL(closed); got != LFLClosed {
		t.Errorf("статус закрытого магазина: got %q, want %q", got, LFLClosed)
	}

	// Справочника нет — страну не выдумываем, оставляем как пришла.
	if got := retailStoreCountry(closed, map[int]string{}); got != "Розница закрыты" {
		t.Errorf("без справочника страна не должна меняться: %q", got)
	}
	// Обычный магазин правило не трогает.
	usual := RetailStore{CodeCFO: 100, GroupCFO1: "3.Магазины", Country: "BY", LFLStatus: LFLYes}
	if got := retailStoreCountry(usual, cfo); got != "BY" {
		t.Errorf("страна обычного магазина изменена: %q", got)
	}
	if got := retailStoreLFL(usual); got != LFLYes {
		t.Errorf("статус обычного магазина изменён: %q", got)
	}
}
