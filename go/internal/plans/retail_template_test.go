package plans

import "testing"

// Пример файла для импорта (ТЗ §5) обязан быть валидным входом самого импорта:
// подсказки пропускаются, строка-образец не пишется, реальные строки среза
// возвращают ровно свои текущие значения — загрузка примера «как есть» ничего
// не меняет.
func TestRetailImportTemplate_RoundTrip(t *testing.T) {
	v := 1000.0
	form := RetailForm{
		Country: "BY", Currency: "BYN", Year: 2026, Month: 7, Months: []int{7, 8},
		Rows: []RetailRow{
			{CodeCFO: 100, NameCFO: "Магазин 100", City: "Минск", Values: []RetailValue{
				{Metric: MetricSales, Year: 2026, Month: 7, Amount: &v},
			}},
			{CodeCFO: 101, NameCFO: "Магазин 101", City: "Брест"},
		},
	}
	header, rows := buildRetailTemplate(form)
	data, err := writeXlsx("Пример импорта", header, rows)
	if err != nil {
		t.Fatalf("writeXlsx: %v", err)
	}

	cells, issues, inFile, err := parseRetailImport(data, 2026, map[int]bool{7: true, 8: true},
		map[int]bool{100: true, 101: true}, 0)
	if err != nil {
		t.Fatalf("парсер должен принимать пример файла: %v", err)
	}
	if len(issues) != 0 {
		t.Fatalf("пример файла не должен давать проблем импорта: %+v", issues)
	}
	// В файле 2 содержательные строки: подсказки и образец не считаются.
	if inFile != len(form.Rows) {
		t.Errorf("строк в файле: got %d, want %d", inFile, len(form.Rows))
	}
	if len(cells) != 1 {
		t.Fatalf("к записи должна пойти одна заполненная ячейка, получено %d: %+v", len(cells), cells)
	}
	c := cells[0]
	if c.CodeCFO != 100 || c.Year != 2026 || c.Month != 7 || c.Amount == nil || *c.Amount != v {
		t.Errorf("значение примера уехало: %+v", c)
	}
}

// Колонки-месяцы примера — плановый период карточки, а не календарный год.
func TestRetailImportTemplate_MonthColumnsMatchPeriod(t *testing.T) {
	header, _ := buildRetailTemplate(RetailForm{Year: 2026, Months: []int{7, 8, 9}, Currency: "BYN"})
	want := []string{"code_cfo", "cfo", "city", "2026-07", "2026-08", "2026-09"}
	if len(header) != len(want) {
		t.Fatalf("заголовок: got %v, want %v", header, want)
	}
	for i := range want {
		if header[i] != want[i] {
			t.Fatalf("заголовок: got %v, want %v", header, want)
		}
	}
}
