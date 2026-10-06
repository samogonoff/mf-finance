package plans

import "testing"

func TestTaskDataScopeFiltersRowsAndMetadata(t *testing.T) {
	value := 12.0
	data := TaskData{Task: Task{CfoCodes: []int{335, 999}, CfoCount: 2, FormCode: TemplateMP},
		Rows: []TaskDataRow{{CodeCFO: 335, Tactic: &value}, {CodeCFO: 999, Tactic: &value}, {CodeCFO: 0, Tactic: &value}}, HasData: true}
	objects := map[int]taskDataObject{335: {Country: "RU", LegalEntity: "R"}, 999: {Country: "BY", LegalEntity: "B"}}
	got, err := filterTaskDataByScope(data, objects, NewScopeFilter(false, []UserScope{{Country: "RU", CodeCFO: []int{335, 999}}}))
	if err != nil {
		t.Fatal(err)
	}
	if len(got.Rows) != 1 || got.Rows[0].CodeCFO != 335 || got.Task.CfoCount != 1 || len(got.Task.CfoCodes) != 1 || got.Task.CfoCodes[0] != 335 {
		t.Fatalf("hidden task data leaked: %+v", got)
	}
	if len(data.Rows) != 3 || len(data.Task.CfoCodes) != 2 {
		t.Fatal("input mutated")
	}
}

func TestTaskDataScopeFailsClosedOnUnknownCFOAndMissingDimensions(t *testing.T) {
	data := TaskData{Task: Task{CfoCodes: []int{335}}, Rows: []TaskDataRow{{CodeCFO: 335}}}
	rule := NewScopeFilter(false, []UserScope{{Country: "RU"}})
	if _, err := filterTaskDataByScope(data, nil, rule); err == nil {
		t.Fatal("unknown CFO authorized")
	}
	if _, err := filterTaskDataByScope(data, map[int]taskDataObject{335: {}}, rule); err == nil {
		t.Fatal("missing country authorized")
	}
}

func TestTaskDataScopeAllowsAggregateOnlyForFullTask(t *testing.T) {
	data := TaskData{Task: Task{CfoCodes: []int{335, 999}}, Rows: []TaskDataRow{{CodeCFO: 0}}}
	objects := map[int]taskDataObject{335: {Country: "RU"}, 999: {Country: "BY"}}
	got, err := filterTaskDataByScope(data, objects, NewScopeFilter(false, []UserScope{{CodeCFO: []int{335, 999}}}))
	if err != nil || len(got.Rows) != 1 {
		t.Fatalf("full scope should see aggregate: %+v %v", got, err)
	}
}
