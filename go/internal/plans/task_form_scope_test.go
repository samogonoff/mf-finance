package plans

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestMpTaskFormScope_FiltersJSONAndExport(t *testing.T) {
	f := MpTaskForm{
		Platforms:  []MpFormPlatform{{CodeCFO: 335, Name: "allowed", Country: "RU", Segment: "large"}, {CodeCFO: 999, Name: "SECRET", Country: "BY", Segment: "small"}},
		Cells:      []MpFormCell{{CodeCFO: 335, BlockType: BSalesManagerGross}, {CodeCFO: 999, BlockType: BSalesManagerGross}},
		Conditions: map[int]MpConditions{335: {CodeCFO: 335}, 999: {CodeCFO: 999}},
		Hints:      []FieldHint{{CodeCFO: 335}, {CodeCFO: 999}},
		VatByCfo:   map[int]MpVatBasis{335: {Ratio: 1.2}, 999: {Ratio: 1.12}}, VatMissing: []int{999},
		Task: Task{CfoCodes: []int{335, 999}, CfoCount: 2}, Lines: mpFormSpec(),
	}
	got, err := filterMpTaskForm(f, NewScopeFilter(false, []UserScope{{Country: "RU", CodeCFO: []int{335, 999}}}))
	if err != nil {
		t.Fatal(err)
	}
	if len(got.Platforms) != 1 || len(got.Cells) != 1 || len(got.Conditions) != 1 || len(got.Hints) != 1 || len(got.VatByCfo) != 1 || len(got.VatMissing) != 0 || got.Task.CfoCount != 1 || got.Segment != "large" {
		t.Fatalf("scope leak: %+v", got)
	}
	data, err := mpFormExportData(got)
	if err != nil {
		t.Fatal(err)
	}
	wb, err := openXLSX(data)
	if err != nil {
		t.Fatal(err)
	}
	rows, err := wb.Sheet("МП")
	if err != nil {
		t.Fatal(err)
	}
	for _, row := range rows {
		if strings.Contains(strings.Join(row, "|"), "SECRET") {
			t.Fatal("Excel leaks hidden platform")
		}
	}
	if len(f.Cells) != 2 || len(f.Conditions) != 2 {
		t.Fatal("input mutated")
	}
}

func TestMpTaskFormScope_NoRulesCannotRead(t *testing.T) {
	_, err := filterMpTaskForm(MpTaskForm{Platforms: []MpFormPlatform{{CodeCFO: 335}}}, NewScopeFilter(false, nil))
	if err == nil {
		t.Fatal("empty access rules allowed data")
	}
}

type failedMpScope struct{}

func (failedMpScope) UserCodeCFOs(context.Context, int64) ([]int, error) {
	return nil, errors.New("unavailable")
}
func (failedMpScope) UpsertScope(context.Context, UserScope) error { return nil }
func TestMpTaskFormScope_FailsClosed(t *testing.T) {
	h := &Handler{principal: func(*http.Request) (Principal, bool) { return Principal{UserID: 2}, true }, form: &Service{scope: failedMpScope{}}}
	_, err := h.scopedMpForm(httptest.NewRequest("GET", "/", nil), MpTaskForm{Platforms: []MpFormPlatform{{CodeCFO: 335}}})
	if err == nil {
		t.Fatal("failed scope lookup allowed data")
	}
}
