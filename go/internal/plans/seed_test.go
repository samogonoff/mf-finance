package plans

import "testing"

func TestMarketplaceSeed_LargeHasFourPlatforms(t *testing.T) {
	var large []MarketplaceRow
	for _, r := range MarketplaceSeed() {
		if r.Segment == "large" {
			large = append(large, r)
		}
	}
	if len(large) < 4 {
		t.Fatalf("large segment must have >=4 платформы, got %d", len(large))
	}
	want := map[int]bool{335: false, 336: false, 337: false, 954: false}
	for _, r := range large {
		if _, ok := want[r.CodeCFO]; ok {
			want[r.CodeCFO] = true
		}
	}
	for code, seen := range want {
		if !seen {
			t.Errorf("large segment must contain площадку code_cfo=%d", code)
		}
	}
}

func TestMarketplaceSeed_RequiredFields(t *testing.T) {
	rows := MarketplaceSeed()
	if len(rows) == 0 {
		t.Fatal("MarketplaceSeed() пуст")
	}
	for _, r := range rows {
		if r.CodeCFO == 0 || r.NameCFO == "" || r.Country == "" {
			t.Errorf("неполная строка площадки: %+v", r)
		}
		if r.Segment != "large" && r.Segment != "small" {
			t.Errorf("segment должен быть large|small, got %q (%+v)", r.Segment, r)
		}
		if r.GroupCFO != 250 && r.GroupCFO != 480 {
			t.Errorf("group_cfo должен быть 250|480, got %d (%+v)", r.GroupCFO, r)
		}
	}
}

func TestMarketplaceSeed_SmallHasRegional(t *testing.T) {
	byCode := map[int]MarketplaceRow{}
	for _, r := range MarketplaceSeed() {
		byCode[r.CodeCFO] = r
	}
	// Kaspi (KZ) и Uzmarket (UZ) — отличают small от чисто-RU large.
	if r, ok := byCode[338]; !ok || r.Segment != "small" || r.Country != "KZ" {
		t.Errorf("ожидался Kaspi code_cfo=338 segment=small country=KZ, got %+v", r)
	}
	if r, ok := byCode[339]; !ok || r.Segment != "small" || r.Country != "UZ" {
		t.Errorf("ожидался Uzmarket code_cfo=339 segment=small country=UZ, got %+v", r)
	}
}

func TestPLLineSeed_HasMPSalesCodes(t *testing.T) {
	codes := map[int]string{}
	for _, r := range PLLineSeed() {
		codes[r.CodePL] = r.Name
	}
	for _, code := range []int{1046, 1045, 1022, 1006, 8006} {
		if codes[code] == "" {
			t.Errorf("dir_pl_line должен содержать code_pl=%d с наименованием", code)
		}
	}
}
