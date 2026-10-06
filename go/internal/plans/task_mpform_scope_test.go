package plans

import "testing"

func TestMpSaveScopeRequiresOneCompleteRule(t *testing.T) {
	scope := NewScopeFilter(false, []UserScope{
		{StageCode: "fill", CodeCFO: []int{335}, Country: "KZ"},
		{StageCode: "approve", CodeCFO: []int{335}, Country: "RU"},
		{StageCode: "fill", CodeCFO: []int{337}, Country: "RU", LegalEntity: "TD Mark Formelle"},
	})
	if mpSaveScopeAllows(scope, "fill", 335) {
		t.Fatal("country from one rule and stage from another must not authorize Wildberries")
	}
	if !mpSaveScopeAllows(scope, "fill", 337) {
		t.Fatal("matching CFO, country, legal entity and stage should authorize Ozon")
	}
	if mpSaveScopeAllows(scope, "fill", 475) {
		t.Fatal("another segment must remain outside scope")
	}
	if mpSaveScopeAllows(scope, "fill", 999999) {
		t.Fatal("unknown marketplace must remain outside scope")
	}
}
