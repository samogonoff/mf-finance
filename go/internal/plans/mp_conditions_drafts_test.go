package plans

import (
	"math"
	"testing"
)

func draftsBase() (map[int]MpConditions, []MarketplaceRow) {
	base := map[int]MpConditions{
		335: {CodeCFO: 335, SppPct: .10, MarkupPct: .50, MarkupTotalPct: .20, Currency: "RUB",
			Shares: map[string]float64{BCostAgent: .05}},
	}
	return base, []MarketplaceRow{{CodeCFO: 335}, {CodeCFO: 336}}
}

// Черновик перекрывает значения сохранённых условий, но не трогает НДС/валюту и
// сам исходный реестр: предпросмотр ничего не меняет.
func TestApplyConditionDrafts_OverridesWithoutMutatingBase(t *testing.T) {
	base, plats := draftsBase()
	out, err := applyConditionDrafts(base, []MpConditions{{
		CodeCFO: 335, SppPct: .20, MarkupPct: .60, MarkupTotalPct: .30,
		Shares: map[string]float64{BCostAgent: .10},
	}}, plats)
	if err != nil {
		t.Fatal(err)
	}
	got := out[335]
	if got.SppPct != .20 || got.MarkupPct != .60 || got.MarkupTotalPct != .30 || got.Shares[BCostAgent] != .10 {
		t.Fatalf("черновик не применился: %+v", got)
	}
	if got.Currency != "RUB" {
		t.Fatalf("валюта сохранённых условий потеряна: %q", got.Currency)
	}
	if base[335].SppPct != .10 || base[335].Shares[BCostAgent] != .05 {
		t.Fatalf("исходные условия изменены предпросмотром: %+v", base[335])
	}
}

// Площадка без сохранённых условий получает их из черновика (первое заполнение).
func TestApplyConditionDrafts_NewPlatform(t *testing.T) {
	base, plats := draftsBase()
	out, err := applyConditionDrafts(base, []MpConditions{{CodeCFO: 336, SppPct: .3, MarkupPct: 1}}, plats)
	if err != nil {
		t.Fatal(err)
	}
	if out[336].SppPct != .3 || out[336].MarkupPct != 1 {
		t.Fatalf("условия новой площадки: %+v", out[336])
	}
	if _, ok := base[336]; ok {
		t.Fatal("исходный реестр получил площадку из черновика")
	}
}

// Площадка вне среза/сегмента и значения вне диапазона отклоняются так же, как
// при сохранении.
func TestApplyConditionDrafts_Rejects(t *testing.T) {
	base, plats := draftsBase()
	cases := map[string]MpConditions{
		"вне среза":          {CodeCFO: 999, SppPct: .1},
		"спп ≥ 100 %":        {CodeCFO: 335, SppPct: 1.2},
		"доли + спп > 1":     {CodeCFO: 335, SppPct: .6, Shares: map[string]float64{BCostAgent: .5}},
		"отрицательная доля": {CodeCFO: 335, SppPct: .1, Shares: map[string]float64{BCostAgent: -.1}},
	}
	for name, draft := range cases {
		if _, err := applyConditionDrafts(base, []MpConditions{draft}, plats); err == nil {
			t.Errorf("%s: черновик должен быть отклонён", name)
		}
	}
}

// Проверка МП-01 «не задан %СПП» смотрит на тот же СПП, что и расчёт: введённый
// в форме перекрывает реестр; нулевой СПП формы реестр не затирает.
func TestConditionsWithFormSpp(t *testing.T) {
	base, _ := draftsBase()
	base[336] = MpConditions{CodeCFO: 336} // условия есть, СПП в реестре не задан
	out := conditionsWithFormSpp(base, map[int]float64{335: .25, 336: .30, 337: .4})
	if out[335].SppPct != .25 {
		t.Errorf("СПП формы должен перекрывать реестр: %v", out[335].SppPct)
	}
	if out[336].SppPct != .30 {
		t.Errorf("СПП формы должен закрывать «не задан %%СПП»: %v", out[336].SppPct)
	}
	if _, ok := out[337]; ok {
		t.Error("СПП формы не должен создавать условия площадки, которой нет в реестре (МП-01 «не заданы условия»)")
	}
	if base[335].SppPct != .10 {
		t.Error("исходные условия изменены")
	}
	zero := conditionsWithFormSpp(base, map[int]float64{335: 0})
	if zero[335].SppPct != .10 {
		t.Errorf("нулевой СПП формы затёр СПП реестра: %v", zero[335].SppPct)
	}
}

// Правка доли в условиях меняет расходную часть пропорционально: это то, что
// видит предпросмотр с черновиком и что пишет пересчёт.
func TestDraftChangesCostPart(t *testing.T) {
	base, plats := draftsBase()
	calc := func(c MpConditions) float64 {
		out, _ := computeMpPlatformInverse(MpInverseInput{
			SalesManagerGross: 1_000_000, Conditions: conditionsToInput(c, vatBasisFromRate(.2)),
		})
		return out[BCostAgent]
	}
	before := calc(base[335])
	drafted, err := applyConditionDrafts(base, []MpConditions{{
		CodeCFO: 335, SppPct: .10, MarkupPct: .50, MarkupTotalPct: .20,
		Shares: map[string]float64{BCostAgent: .10},
	}}, plats)
	if err != nil {
		t.Fatal(err)
	}
	after := calc(drafted[335])
	if before <= 0 {
		t.Fatalf("расходная часть должна считаться от условий: %v", before)
	}
	if math.Abs(after-2*before) > 1e-6*before {
		t.Fatalf("доля ×2 должна дать статью ×2: было %v, стало %v", before, after)
	}
}
