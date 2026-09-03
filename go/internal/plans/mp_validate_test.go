package plans

import "testing"

// mpValidateFixture — заполненная площадка WB на июнь 2026 (общий вход для
// проверок правок 02.09.2026: владельцы статей и расхождение %СПП с фактом).
func mpValidateFixture() MpValidationInput {
	cond := MpConditions{CodeCFO: 335, SppPct: 0.31, MarkupPct: 0.65, MarkupTotalPct: 1.7722,
		Currency: "RUB", Shares: conditionsWB().Shares}
	vals, _ := computeMpPlatformInverse(MpInverseInput{
		SalesManagerGross: 457_380_000, Conditions: conditionsToInput(cond, vatWB)})
	return MpValidationInput{
		Year: 2026, Month: 6,
		Platforms:  []MarketplaceRow{{CodeCFO: 335, NameCFO: "Wildberries", Segment: "large", Country: "RU", LegalEntity: "TD Mark Formelle"}},
		Conditions: map[int]MpConditions{335: cond},
		Values:     map[int]map[string]float64{335: vals},
		CalcOwner:  map[[2]int]string{{51, 335}: TemplateMP, {52, 335}: TemplateMP, {54, 335}: TemplateMP},
	}
}

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

// МП-11 только предупреждает (правка ТЗ МП §17 от 28.08.2026): справочник
// авторитетен, расходятся подписи, а не коды, поэтому отправку это не
// останавливает. Замечание уровня формы: наименование от площадки не зависит.
func TestValidateMp_PLNameConflict(t *testing.T) {
	issues := ValidateMpForm(MpValidationInput{
		Platforms: []MarketplaceRow{
			{CodeCFO: 335, NameCFO: "Wildberries", Segment: "large", Country: "RU", LegalEntity: "TD"},
			{CodeCFO: 336, NameCFO: "Lamoda", Segment: "large", Country: "RU", LegalEntity: "TD"},
		},
		PLNames: map[int]string{
			52: "Грузоперевозки развоз и перемещение", // совпадает с формой после правки §16
			54: "Аренда помещений (стоянки)",          // совпадает с формой после правки §16
			58: "Банковские расходы / эквайринг",      // та же статья, другая формулировка
		},
	})
	var blocking, warning int
	for _, i := range issues {
		if i.Code != "МП-11" {
			continue
		}
		if i.CodeCFO != 0 {
			t.Errorf("МП-11 не должно привязываться к площадке, получено ЦФО %d", i.CodeCFO)
		}
		switch i.Level {
		case MpBlocking:
			blocking++
		case MpWarning:
			warning++
			if i.Block != BCostAcquiring {
				t.Errorf("предупреждение ожидалось по статье 58, получено %q", i.Block)
			}
		}
	}
	if blocking != 0 {
		t.Errorf("МП-11 больше не блокирует отправку, получено %d блокирующих: %+v", blocking, issues)
	}
	if warning != 1 {
		t.Fatalf("ожидалось одно предупреждение (код 58), получено %d: %+v", warning, issues)
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

// Владелец статьи 51 — ЦЗ 32 (правка ТЗ 02.09.2026, часть 2 / МП §24): в форме МП
// строка только выводится индикатором, поэтому «владелец не МП» не должен
// блокировать отправку — иначе форма заблокирована всегда.
func TestValidate_Article51ForeignOwnerIsInfoNotBlocking(t *testing.T) {
	in := mpValidateFixture()
	in.CalcOwner = map[[2]int]string{
		{51, 335}: "TPL-CFO-EXP", // логисты, ЦЗ 32 — так и должно быть
		{52, 335}: TemplateMP,
		{54, 335}: TemplateMP,
	}
	issues := ValidateMpForm(in)

	var found bool
	for _, i := range issues {
		if i.Code != "МП-08" {
			continue
		}
		found = true
		if i.Level != MpInfo {
			t.Fatalf("статья 51 у ЦЗ 32: ожидался уровень info, got %q (%s)", i.Level, i.Message)
		}
		if i.NeedsWhy {
			t.Fatal("информационное замечание не должно требовать обоснования")
		}
	}
	if !found {
		t.Fatal("замечание МП-08 по статье 51 должно присутствовать (строка только выводится)")
	}
	for _, i := range issues {
		if i.Level == MpBlocking && i.Code == "МП-08" {
			t.Fatal("владелец статьи 51 вне формы МП не должен блокировать отправку")
		}
	}
}

// Статьи 52 и 54 принадлежат МП: если их считает другая форма — это по-прежнему
// блокирующий двойной счёт.
func TestValidate_Articles52And54ForeignOwnerStillBlocks(t *testing.T) {
	in := mpValidateFixture()
	in.CalcOwner = map[[2]int]string{{52, 335}: "TPL-CFO-EXP", {54, 335}: TemplateMP}
	blocking := 0
	for _, i := range ValidateMpForm(in) {
		if i.Code == "МП-08" && i.Level == MpBlocking {
			blocking++
		}
	}
	if blocking == 0 {
		t.Fatal("статья 52 у чужой формы — двойной счёт, отправка должна блокироваться")
	}
}

// Расхождение введённого %СПП с фактическим: информируем и подсвечиваем, но не
// блокируем и обоснования не требуем (правка ТЗ, часть 2 / МП §23).
func TestValidate_SppVsFactWarnsWithoutJustification(t *testing.T) {
	in := mpValidateFixture()
	in.SppFact = map[int]float64{335: 0.20} // введено 0,31 — расхождение 11 п.п.
	issues := ValidateMpForm(in)
	var got *MpIssue
	for i := range issues {
		if issues[i].Code == "МП-W9" {
			got = &issues[i]
		}
	}
	if got == nil {
		t.Fatal("расхождение %СПП с фактическим обязано попадать в замечания (МП-W9)")
	}
	if got.Level != MpWarning {
		t.Fatalf("уровень: got %q, want warning (подсветка)", got.Level)
	}
	if got.NeedsWhy {
		t.Fatal("ТЗ прямо говорит «комментарий не требовать»")
	}
	for _, i := range NeedsExplanation(issues) {
		if i.Code == "МП-W9" {
			t.Fatal("МП-W9 не должно попадать в список требующих обоснования")
		}
	}
}

// Совпадение с фактом (в пределах порога) замечания не даёт.
func TestValidate_SppMatchingFactIsSilent(t *testing.T) {
	in := mpValidateFixture()
	in.SppFact = map[int]float64{335: 0.31}
	for _, i := range ValidateMpForm(in) {
		if i.Code == "МП-W9" {
			t.Fatalf("при совпадении с фактом замечания быть не должно: %s", i.Message)
		}
	}
}
