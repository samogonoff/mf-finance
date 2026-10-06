package plans

import "testing"

func TestScopedStepOwners_CardCountriesAndPlatforms(t *testing.T) {
	card := Card{FormCode: TemplateMP, ScopeKey: "large"}
	objects := []MpFormPlatform{{CodeCFO: 335, Country: "RU", LegalEntity: "RU-company"}}
	cases := []struct {
		name    string
		rules   []UserScope
		want    int64
		wantErr bool
	}{
		{"country scoped controller", []UserScope{{UserID: 7, StageCode: "fin", Country: "RU"}}, 7, false},
		{"other country excluded", []UserScope{{UserID: 7, StageCode: "fin", Country: "BY"}}, 0, false},
		{"other platform excluded", []UserScope{{UserID: 7, StageCode: "fin", CodeCFO: []int{999}}}, 0, false},
		{"rule dimensions not combined", []UserScope{{UserID: 7, StageCode: "fin", Country: "RU", CodeCFO: []int{999}}, {UserID: 7, StageCode: "fin", Country: "BY", CodeCFO: []int{335}}}, 0, false},
		{"no inferred step", []UserScope{{UserID: 7, Country: "RU"}}, 0, false},
		{"duplicate scopes one recipient", []UserScope{{UserID: 7, StageCode: "fin", Country: "RU"}, {UserID: 7, StageCode: "fin", CodeCFO: []int{335}}}, 7, false},
		{"ambiguous owner rejected", []UserScope{{UserID: 7, StageCode: "fin"}, {UserID: 8, StageCode: "fin"}}, 0, true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, err := resolveScopedStepOwners(map[string]int64{"1.3": 9}, card, objects, tc.rules)
			if (err != nil) != tc.wantErr {
				t.Fatalf("error=%v", err)
			}
			if tc.wantErr {
				return
			}
			if got["fin"] != tc.want || got["1.3"] != 9 {
				t.Fatalf("owners=%v", got)
			}
		})
	}
}

func TestScopedStepOwners_PeriodOwnerDisambiguates(t *testing.T) {
	base := map[string]int64{"1.3": 8}
	got, err := resolveScopedStepOwners(base, Card{Country: "BY"}, nil, []UserScope{{UserID: 7, StageCode: "1.3", Country: "BY"}, {UserID: 8, StageCode: "1.3", Country: "BY"}})
	if err != nil || got["1.3"] != 8 {
		t.Fatalf("owners=%v err=%v", got, err)
	}
	if base["1.3"] != 8 {
		t.Fatal("base mutated")
	}
}

func TestScopedStepOwners_MissingMPDirectoryDoesNotBroadenCountry(t *testing.T) {
	got, err := resolveScopedStepOwners(nil, Card{FormCode: TemplateMP, ScopeKey: "small"}, nil, []UserScope{{UserID: 7, StageCode: "fin", Country: "RU"}})
	if err != nil {
		t.Fatal(err)
	}
	if got["fin"] != 0 {
		t.Fatal("missing marketplace directory granted cross-country ownership")
	}
}

func TestScopedStepOwners_RequiresWholeCardCoverage(t *testing.T) {
	card := Card{FormCode: TemplateMP, ScopeKey: "large"}
	objects := []MpFormPlatform{{CodeCFO: 335, Country: "RU"}, {CodeCFO: 336, Country: "BY"}}
	partial := []UserScope{{UserID: 7, StageCode: "fin", CodeCFO: []int{335}}}
	got, err := resolveScopedStepOwners(map[string]int64{"fin": 7}, card, objects, partial)
	if err != nil || got["fin"] != 0 {
		t.Fatalf("partial owner accepted: %v %v", got, err)
	}
	complete := append(partial, UserScope{UserID: 7, StageCode: "fin", Country: "BY", CodeCFO: []int{336}})
	got, err = resolveScopedStepOwners(nil, card, objects, complete)
	if err != nil || got["fin"] != 7 {
		t.Fatalf("complete union rejected: %v %v", got, err)
	}
}

func TestScopedStepOwners_UnknownDimensionsDoNotMatch(t *testing.T) {
	for _, rule := range []UserScope{{Country: "RU"}, {LegalEntity: "company"}} {
		if ownerRuleAllows(rule, MpFormPlatform{CodeCFO: 335}) {
			t.Fatal("unknown dimension granted scoped owner")
		}
	}
}
