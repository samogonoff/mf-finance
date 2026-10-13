package plans

import "testing"

// ТЗ доработок п. 4.3/4.2: в схеме только включённые шаги по порядку, с
// исполнителем; согласующий шаг без исполнителя — пробел.
func TestBuildRouteScheme(t *testing.T) {
	route := []RouteStep{
		{StepCode: "1.3", StepName: "Директор направления", SortOrder: 30, Kind: StepApprove, Enabled: true},
		{StepCode: "1.1", StepName: "Заполнение", SortOrder: 10, Kind: StepFill, Enabled: true},
		{StepCode: "1.2", StepName: "Руководитель", SortOrder: 20, Kind: StepApprove, Enabled: false},
		{StepCode: "fin", StepName: "Финансовый контролёр", SortOrder: 25, Kind: StepApprove, Enabled: true},
		{StepCode: "1.4", StepName: "Утверждение", SortOrder: 40, Kind: StepFinal, Enabled: true},
	}
	c := Card{ID: 7, FormCode: TemplateRetail, ScopeKey: "RU", StepCode: "fin", Status: CardOnApproval}
	got := buildRouteScheme(c, route, map[string]int64{"1.3": 5, "1.4": 9}, map[int64]string{5: "Дегтерева Е.В.", 9: "Сипарова С.Г."})

	codes := []string{}
	for _, s := range got.Steps {
		codes = append(codes, s.StepCode)
	}
	if len(codes) != 4 || codes[0] != "1.1" || codes[1] != "fin" || codes[2] != "1.3" || codes[3] != "1.4" {
		t.Fatalf("порядок/состав шагов: %v", codes)
	}
	if got.Gaps != 1 || !got.Steps[1].Gap || !got.Steps[1].Current {
		t.Errorf("пробел должен быть на шаге контролёра: %+v", got.Steps[1])
	}
	if got.Steps[0].Gap {
		t.Error("шаг заполнения без исполнителя — не пробел маршрута")
	}
	if got.Steps[2].OwnerName != "Дегтерева Е.В." {
		t.Errorf("исполнитель 1.3: %+v", got.Steps[2])
	}
}
