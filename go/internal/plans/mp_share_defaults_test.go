package plans

import (
	"context"
	"errors"
	"math"
	"testing"
)

// Доля — статья ÷ |1006|; чужие ЦФО не участвуют; доли, которые реестр не
// примет (сторно вне компенсаций), в умолчание не попадают.
func TestComputeArticleShares(t *testing.T) {
	rows := []MpArticleFactRow{
		{CodeCFO: 335, CodePL: 1006, Amount: 4000}, // стратегия — выручка с плюсом
		{CodeCFO: 335, CodePL: 13, Amount: 400},
		{CodeCFO: 335, CodePL: 64, Amount: 500},
		{CodeCFO: 335, CodePL: 52, Amount: -40}, // сторно — не берём
		{CodeCFO: 335, CodePL: 66, Amount: -20}, // компенсация — можно
		{CodeCFO: 337, CodePL: 13, Amount: 9999},
	}
	sh := computeArticleShares(rows, 335)
	want := map[string]float64{BCostAds: 0.1, BCostAgent: 0.125, BCostOther: -0.005}
	if len(sh) != len(want) {
		t.Fatalf("доли %v, ждали %v", sh, want)
	}
	for b, v := range want {
		if math.Abs(sh[b]-v) > 1e-9 {
			t.Errorf("%s = %v, ждали %v", b, sh[b], v)
		}
	}
	// Факт: выручка с минусом — база по модулю.
	sh = computeArticleShares([]MpArticleFactRow{{CodeCFO: 1, CodePL: 1006, Amount: -1000}, {CodeCFO: 1, CodePL: 13, Amount: 50}}, 1)
	if math.Abs(sh[BCostAds]-0.05) > 1e-9 {
		t.Errorf("база по модулю: %v", sh)
	}
	if len(computeArticleShares(rows, 954)) != 0 {
		t.Error("площадка без продаж должна остаться без долей")
	}
}

// Ручная доля старше стратегической; статья без обеих в каскад не попадает.
func TestEffectiveSharesManualOverDefault(t *testing.T) {
	c := MpConditions{
		Shares:        map[string]float64{BCostAds: 0.09},
		DefaultShares: map[string]float64{BCostAds: 0.058, BCostAgent: 0.0768},
	}
	eff := c.EffectiveShares()
	if eff[BCostAds] != 0.09 || eff[BCostAgent] != 0.0768 {
		t.Fatalf("эффективные доли %v", eff)
	}
	if _, ok := eff[BCostFreight]; ok {
		t.Fatal("доля без ручного и стратегического значения должна отсутствовать")
	}
	if c.Shares[BCostAgent] != 0 {
		t.Fatal("умолчание не должно попадать в ручные доли")
	}
	in := conditionsToInputNorm(c, MpVatBasis{Ratio: 1.2}, 0)
	if in.Shares[BCostAgent] != 0.0768 {
		t.Fatalf("каскад должен считать от стратегической доли: %v", in.Shares)
	}
}

type fakeArticles struct {
	rows  []MpArticleFactRow
	err   error
	calls int
}

func (f *fakeArticles) SourceName() string { return "fake" }
func (f *fakeArticles) MpArticles(context.Context, string, int, int, []int, []int) ([]MpArticleFactRow, error) {
	f.calls++
	return f.rows, f.err
}

// Площадка без строки реестра получает пустые условия с долями по умолчанию;
// существующая сохраняет ручные значения; источник читается один раз (кэш).
func TestShareDefaultsAttach(t *testing.T) {
	src := &fakeArticles{rows: []MpArticleFactRow{
		{CodeCFO: 335, CodePL: 1006, Amount: 1000}, {CodeCFO: 335, CodePL: 13, Amount: 58},
		{CodeCFO: 337, CodePL: 1006, Amount: 1000}, {CodeCFO: 337, CodePL: 64, Amount: 95},
	}}
	d := NewMpShareDefaults(src, "")
	platforms := []MarketplaceRow{{CodeCFO: 335, NameCFO: "Wildberries", Segment: "large", Country: "RU"},
		{CodeCFO: 337, NameCFO: "Ozon", Segment: "large", Country: "RU"}}
	existing := map[int]MpConditions{337: {ID: 7, CodeCFO: 337, SppPct: 0.3, Shares: map[string]float64{BCostAds: 0.08}}}

	got := d.Attach(context.Background(), existing, platforms, 2026, 11)
	wb := got[335]
	if wb.ID != 0 || wb.SppPct != 0 || wb.Currency != "RUB" || wb.DefaultShares[BCostAds] != 0.058 {
		t.Fatalf("синтетические условия WB: %+v", wb)
	}
	if wb.DefaultSource != "стратегия «НовСтрат» 11.2026" {
		t.Errorf("подпись источника %q", wb.DefaultSource)
	}
	oz := got[337]
	if oz.ID != 7 || oz.Shares[BCostAds] != 0.08 || oz.DefaultShares[BCostAgent] != 0.095 {
		t.Fatalf("условия Ozon: %+v", oz)
	}
	if _, touched := existing[335]; touched {
		t.Fatal("Attach не должен менять входную карту")
	}
	d.Attach(context.Background(), existing, platforms, 2026, 11)
	if src.calls != 1 {
		t.Errorf("источник прочитан %d раз, ждали 1 (кэш)", src.calls)
	}
}

// Недоступный источник и nil-провайдер не роняют форму: условия как были.
func TestShareDefaultsSourceDown(t *testing.T) {
	d := NewMpShareDefaults(&fakeArticles{err: errors.New("TDS EOF")}, "НовСтрат")
	in := map[int]MpConditions{335: {CodeCFO: 335}}
	got := d.Attach(context.Background(), in, []MarketplaceRow{{CodeCFO: 335}}, 2026, 11)
	if len(got) != 1 || got[335].DefaultShares != nil {
		t.Fatalf("при недоступном источнике умолчаний быть не должно: %+v", got)
	}
	var nilD *MpShareDefaults
	if got := nilD.Attach(context.Background(), in, nil, 2026, 11); len(got) != 1 {
		t.Fatal("nil-провайдер должен вернуть условия без изменений")
	}
}
