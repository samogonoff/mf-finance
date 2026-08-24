package plans

import "testing"

// mpHasIssue — есть ли замечание МП с кодом (и опционально по площадке).
func mpHasIssue(list []MpIssue, code string, cfo int) bool {
	for _, i := range list {
		if i.Code == code && (cfo == 0 || i.CodeCFO == cfo) {
			return true
		}
	}
	return false
}

// Полностью заполненная площадка не даёт блокирующих замечаний.
func TestValidateMp_ValidPlatformPasses(t *testing.T) {
	cond := MpConditions{CodeCFO: 335, SppPct: 0.31, MarkupPct: 0.65, MarkupTotalPct: 1.7722,
		Currency: "RUB", Shares: conditionsWB().Shares}
	vals, _ := computeMpPlatformInverse(MpInverseInput{
		SalesManagerGross: 457_380_000, Conditions: conditionsToInput(cond, vatWB)})

	issues := ValidateMpForm(MpValidationInput{
		Year: 2026, Month: 6,
		Platforms:  []MarketplaceRow{{CodeCFO: 335, NameCFO: "Wildberries", Segment: "large", Country: "RU", LegalEntity: "TD Mark Formelle"}},
		Conditions: map[int]MpConditions{335: cond},
		Values:     map[int]map[string]float64{335: vals},
		CalcOwner:  map[[2]int]string{{51, 335}: TemplateMP, {52, 335}: TemplateMP, {54, 335}: TemplateMP},
	})
	if HasBlocking(issues) {
		for _, i := range issues {
			if i.Level == MpBlocking {
				t.Errorf("неожидаемое блокирующее замечание %s: %s", i.Code, i.Message)
			}
		}
	}
}

// МП-01: незаполненные условия блокируют отправку.
func TestValidateMp_MissingConditionsBlocks(t *testing.T) {
	issues := ValidateMpForm(MpValidationInput{
		Platforms: []MarketplaceRow{{CodeCFO: 336, NameCFO: "Lamoda", Segment: "large", Country: "RU", LegalEntity: "TD"}},
	})
	if !mpHasIssue(issues, "МП-01", 336) || !HasBlocking(issues) {
		t.Fatalf("ожидалось блокирующее МП-01, получено %+v", issues)
	}
}

// МП-03: сумма долей вместе с %СПП не может превышать 100 % выручки.
func TestValidateMp_ShareSumOverHundred(t *testing.T) {
	cond := MpConditions{CodeCFO: 337, SppPct: 0.60, MarkupPct: 0.5, MarkupTotalPct: 1.0,
		Shares: map[string]float64{BCostAgent: 0.30, BCostAds: 0.20}}
	issues := ValidateMpForm(MpValidationInput{
		Platforms:  []MarketplaceRow{{CodeCFO: 337, NameCFO: "Ozon", Segment: "large", Country: "RU", LegalEntity: "TD"}},
		Conditions: map[int]MpConditions{337: cond},
		Values:     map[int]map[string]float64{337: {BSalesManagerGross: 100}},
	})
	if !mpHasIssue(issues, "МП-03", 337) {
		t.Fatalf("ожидалось МП-03 (сумма долей > 100 %%), получено %+v", issues)
	}
}

// МП-02: отрицательная доля допустима только для компенсаций.
func TestValidateMp_NegativeShareOnlyForCompensation(t *testing.T) {
	bad := MpConditions{CodeCFO: 335, SppPct: 0.3, MarkupPct: 0.5, MarkupTotalPct: 1.0,
		Shares: map[string]float64{BCostAds: -0.01}}
	if err := validateConditions(bad); err == nil {
		t.Fatal("отрицательная доля рекламы должна быть отклонена")
	}
	ok := MpConditions{CodeCFO: 335, SppPct: 0.3, MarkupPct: 0.5, MarkupTotalPct: 1.0,
		Shares: map[string]float64{BCostOther: -0.0094}}
	if err := validateConditions(ok); err != nil {
		t.Fatalf("компенсация по статье 66 допустима: %v", err)
	}
}

// МП-10: валюта ввода обязана совпадать с валютой площадки.
func TestValidateMp_CurrencyMustMatchPlatform(t *testing.T) {
	cond := MpConditions{CodeCFO: 474, SppPct: 0.2, MarkupPct: 0.5, MarkupTotalPct: 1.0, Currency: "RUB"}
	issues := ValidateMpForm(MpValidationInput{
		Platforms:  []MarketplaceRow{{CodeCFO: 474, NameCFO: "Ozon KZ", Segment: "small", Country: "KZ", LegalEntity: "MF Kazakhstan"}},
		Conditions: map[int]MpConditions{474: cond},
		Values:     map[int]map[string]float64{474: {BSalesManagerGross: 1000}},
	})
	if !mpHasIssue(issues, "МП-10", 474) {
		t.Fatalf("ожидалось МП-10 (валюта площадки KZT), получено %+v", issues)
	}
}

// МП-08: если владелец расчёта статьи — другая форма, строка здесь read-only.
func TestValidateMp_DoubleCountOwnership(t *testing.T) {
	cond := MpConditions{CodeCFO: 335, SppPct: 0.31, MarkupPct: 0.65, MarkupTotalPct: 1.77,
		Currency: "RUB", Shares: map[string]float64{BCostFreight: 0.003}}
	issues := ValidateMpForm(MpValidationInput{
		Platforms:  []MarketplaceRow{{CodeCFO: 335, NameCFO: "Wildberries", Segment: "large", Country: "RU", LegalEntity: "TD"}},
		Conditions: map[int]MpConditions{335: cond},
		Values:     map[int]map[string]float64{335: {BSalesManagerGross: 100}},
		CalcOwner:  map[[2]int]string{{51, 335}: "TPL-CFO-EXP", {52, 335}: TemplateMP, {54, 335}: TemplateMP},
	})
	if !mpHasIssue(issues, "МП-08", 335) {
		t.Fatalf("ожидалось МП-08 (владелец статьи 51 — ЦЗ 32), получено %+v", issues)
	}
}

// МП-11: наименование статьи в форме расходится со справочником (конфликт кода 54).
func TestValidateMp_PLNameConflict(t *testing.T) {
	issues := ValidateMpForm(MpValidationInput{
		Platforms: []MarketplaceRow{{CodeCFO: 335, NameCFO: "Wildberries", Segment: "large", Country: "RU", LegalEntity: "TD"}},
		PLNames:   map[int]string{54: "Аренда помещений (стоянки)"},
	})
	if !mpHasIssue(issues, "МП-11", 335) {
		t.Fatalf("ожидалось МП-11 (конфликт кода 54), получено %+v", issues)
	}
}

// МП-W1/W2 требуют обоснования, но не блокируют: ввод сохраняется.
func TestValidateMp_WarningsNeedReasonNotBlocking(t *testing.T) {
	prev := MpConditions{CodeCFO: 335, SppPct: 0.25, MarkupPct: 0.65, MarkupTotalPct: 1.77,
		Shares: map[string]float64{BCostAds: 0.02}}
	cur := MpConditions{CodeCFO: 335, SppPct: 0.31, MarkupPct: 0.65, MarkupTotalPct: 1.77,
		Currency: "RUB", Shares: map[string]float64{BCostAds: 0.06}}
	vals, _ := computeMpPlatformInverse(MpInverseInput{
		SalesManagerGross: 1_000_000, Conditions: conditionsToInput(cur, 0.2036)})

	issues := ValidateMpForm(MpValidationInput{
		Platforms:      []MarketplaceRow{{CodeCFO: 335, NameCFO: "Wildberries", Segment: "large", Country: "RU", LegalEntity: "TD"}},
		Conditions:     map[int]MpConditions{335: cur},
		PrevConditions: map[int]MpConditions{335: prev},
		Values:         map[int]map[string]float64{335: vals},
		CalcOwner:      map[[2]int]string{{51, 335}: TemplateMP, {52, 335}: TemplateMP, {54, 335}: TemplateMP},
	})
	if !mpHasIssue(issues, "МП-W1", 335) || !mpHasIssue(issues, "МП-W2", 335) {
		t.Fatalf("ожидались МП-W1 и МП-W2, получено %+v", issues)
	}
	need := NeedsExplanation(issues)
	if len(need) == 0 {
		t.Fatal("предупреждения без обоснования должны требовать комментария")
	}
	// С обоснованием требование снимается.
	cur.ChangeReason = "пересогласовали пакет условий с площадкой"
	issues = ValidateMpForm(MpValidationInput{
		Platforms:      []MarketplaceRow{{CodeCFO: 335, NameCFO: "Wildberries", Segment: "large", Country: "RU", LegalEntity: "TD"}},
		Conditions:     map[int]MpConditions{335: cur},
		PrevConditions: map[int]MpConditions{335: prev},
		Values:         map[int]map[string]float64{335: vals},
		CalcOwner:      map[[2]int]string{{51, 335}: TemplateMP, {52, 335}: TemplateMP, {54, 335}: TemplateMP},
	})
	for _, i := range NeedsExplanation(issues) {
		if i.Code == "МП-W1" || i.Code == "МП-W2" {
			t.Fatalf("обоснование задано — %s не должно требовать пояснения", i.Code)
		}
	}
}

// МП-W7: плановые штрафы нулём при устойчивом факте — предупреждение.
func TestValidateMp_PenaltiesWarning(t *testing.T) {
	cond := MpConditions{CodeCFO: 337, SppPct: 0.27, MarkupPct: 0.65, MarkupTotalPct: 1.77, Currency: "RUB"}
	vals, _ := computeMpPlatformInverse(MpInverseInput{
		SalesManagerGross: 1_000_000, Conditions: conditionsToInput(cond, 0.2036)})
	issues := ValidateMpForm(MpValidationInput{
		Platforms:     []MarketplaceRow{{CodeCFO: 337, NameCFO: "Ozon", Segment: "large", Country: "RU", LegalEntity: "TD"}},
		Conditions:    map[int]MpConditions{337: cond},
		Values:        map[int]map[string]float64{337: vals},
		PenaltiesFact: map[int]float64{337: 150_000},
		CalcOwner:     map[[2]int]string{{51, 337}: TemplateMP, {52, 337}: TemplateMP, {54, 337}: TemplateMP},
	})
	if !mpHasIssue(issues, "МП-W7", 337) {
		t.Fatalf("ожидалось МП-W7 (штрафы 0 при факте), получено %+v", issues)
	}
}

// diff условий: согласующий видит изменение доли и порог обоснования (ТЗ §6.1).
func TestDiffConditions_ShowsChangesAndThreshold(t *testing.T) {
	prev := []MpConditions{{CodeCFO: 335, NameCFO: "Wildberries", SppPct: 0.25,
		Shares: map[string]float64{BCostAds: 0.04, BCostAgent: 0.05}}}
	next := []MpConditions{{CodeCFO: 335, NameCFO: "Wildberries", SppPct: 0.31,
		Shares: map[string]float64{BCostAds: 0.045, BCostAgent: 0.05}}}

	diffs := diffConditions(prev, next, DefaultMpThresholds())
	var spp, ads *MpConditionsDiff
	for i := range diffs {
		switch diffs[i].Field {
		case "spp_pct":
			spp = &diffs[i]
		case "share:" + BCostAds:
			ads = &diffs[i]
		}
	}
	if spp == nil || !spp.NeedsWhy {
		t.Fatalf("изменение %%СПП на 6 п.п. должно требовать обоснования: %+v", diffs)
	}
	if ads == nil || ads.NeedsWhy {
		t.Fatalf("изменение доли на 0,5 п.п. в пределах порога: %+v", ads)
	}
	// Неизменившиеся величины в diff не попадают.
	for _, d := range diffs {
		if d.Field == "share:"+BCostAgent {
			t.Fatalf("неизменившаяся доля не должна попадать в diff: %+v", d)
		}
	}
	if len(requireReasonFor(diffs)) != 1 {
		t.Fatalf("обоснования требует ровно одно изменение, получено %+v", requireReasonFor(diffs))
	}
}
