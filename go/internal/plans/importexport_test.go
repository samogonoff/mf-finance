package plans

import (
	"context"
	"testing"
)

func TestXlsx_GridRoundTrip(t *testing.T) {
	header := []string{"code_cfo", "name_cfo", "amount"}
	rows := [][]string{
		{"335", "Wildberries", "123.45"},
		{"337", "Ozon", "0"},
	}
	data, err := writeXlsx("TPL-MP", header, rows)
	if err != nil {
		t.Fatalf("writeXlsx: %v", err)
	}
	gotHeader, gotRows, err := readXlsx(data)
	if err != nil {
		t.Fatalf("readXlsx: %v", err)
	}
	if len(gotHeader) != 3 || gotHeader[0] != "code_cfo" || gotHeader[2] != "amount" {
		t.Errorf("заголовок не совпал: %v", gotHeader)
	}
	if len(gotRows) != 2 || gotRows[0][1] != "Wildberries" || gotRows[0][2] != "123.45" {
		t.Errorf("строки не совпали round-trip: %v", gotRows)
	}
}

func TestExportThenImport_PreservesTactic(t *testing.T) {
	store := newMemStore()
	svc := NewService(store, NewMockFactSource(), newMemScope())
	ctx := context.Background()

	// Завести тактику.
	save := SaveMpFormRequest{
		Segment: "large", Period: PeriodRef{Year: 2026, Month: 5},
		Rows: []SaveRow{{CodeCFO: 335, CodePL: 1046, BlockType: "sales_manager_price", Amount: 555000, IsManual: true, Comment: "set"}},
	}
	if _, err := svc.SaveMpForm(ctx, adminP, save, []byte(`{}`)); err != nil {
		t.Fatalf("seed save: %v", err)
	}

	// Экспорт → чтение обратно: значение тактики сохраняется (CHECKPOINT B).
	data, err := svc.ExportMpForm(ctx, adminP, 2026, 5, "large", "RUB")
	if err != nil {
		t.Fatalf("ExportMpForm: %v", err)
	}
	header, rows, err := readXlsx(data)
	if err != nil {
		t.Fatalf("readXlsx: %v", err)
	}
	ci := map[string]int{}
	for i, h := range header {
		ci[h] = i
	}
	var found bool
	for _, r := range rows {
		if r[ci["code_cfo"]] == "335" && r[ci["code_pl"]] == "1046" {
			found = true
			if r[ci["tactic"]] != "555000" {
				t.Errorf("тактика не сохранилась через xlsx: %q", r[ci["tactic"]])
			}
		}
	}
	if !found {
		t.Error("WB(335)/1046 не найден в экспорте")
	}
}

func TestParseImport_ValidWithReason(t *testing.T) {
	header := []string{"code_cfo", "code_pl", "block_type", "month", "amount", "reason"}
	rows := [][]string{{"337", "1046", "sales_manager_price", "5", "424242", "акция"}}
	data, _ := writeXlsx("TPL-MP", header, rows)
	imported, err := parseImport(data, "large", 2026, 5)
	if err != nil {
		t.Fatalf("parseImport валидного файла: %v", err)
	}
	if len(imported) != 1 || imported[0].CodeCFO != 337 || imported[0].Amount != 424242 || imported[0].Reason != "акция" {
		t.Errorf("импорт распарсен неверно: %+v", imported)
	}
}

func TestParseImport_RejectsUnknownCodePL(t *testing.T) {
	header := []string{"code_cfo", "code_pl", "block_type", "month", "amount", "reason"}
	rows := [][]string{{"335", "999999", "sales_manager_price", "5", "1", "x"}}
	data, _ := writeXlsx("TPL-MP", header, rows)
	if _, err := parseImport(data, "large", 2026, 5); err == nil {
		t.Error("ожидалась ошибка строки: неизвестный code_pl")
	}
}

func TestParseImport_RequiresReason(t *testing.T) {
	header := []string{"code_cfo", "code_pl", "block_type", "month", "amount", "reason"}
	rows := [][]string{{"335", "1046", "sales_manager_price", "5", "1", ""}}
	data, _ := writeXlsx("TPL-MP", header, rows)
	if _, err := parseImport(data, "large", 2026, 5); err == nil {
		t.Error("ожидалась ошибка: причина обязательна (TPL-06/ADJ-02)")
	}
}
