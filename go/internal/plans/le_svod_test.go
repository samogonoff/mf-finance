package plans

import (
	"context"
	"math"
	"strings"
	"testing"
)

func leNear(a, b float64) bool { return math.Abs(a-b) < 0.005 }

func leRow(t *testing.T, s LeSvod, key string) LeSvodRow {
	t.Helper()
	for _, r := range s.Rows {
		if r.Key == key {
			return r
		}
	}
	t.Fatalf("в своде нет строки %q: %+v", key, s.Rows)
	return LeSvodRow{}
}

func TestNormLegalEntity(t *testing.T) {
	for in, want := range map[string]string{
		"TDMF": "TD Mark Formelle", " tdmf ": "TD Mark Formelle", "MFKaz": "MF Kazakhstan", "MFT": "MF Tex",
		"F": "F", "": leNoEntity, "  ": leNoEntity, "Неизвестное ЮЛ": "Неизвестное ЮЛ",
	} {
		if got := NormLegalEntity(in); got != want {
			t.Errorf("NormLegalEntity(%q) = %q, ждали %q", in, got, want)
		}
	}
}

// Расчёт + корректировки: итог строки и ЮЛ = расчёт + активные корректировки, снятая не считается, итоговые строки
// и отношение расходов к продажам пересчитываются, ЮЛ без суммы расчёта, но с корректировкой, получает колонку.
func TestBuildLeSvod_AdjustmentsAndTotals(t *testing.T) {
	in := leSvodInput{
		PlID: 1, Year: 2026, Month: 10, Window: "month", From: 10, To: 10,
		Amounts: []leAmount{
			{Legal: "F", Key: "sales:Розница РБ", Label: "Розница РБ", BYN: 1000},
			{Legal: "TD Mark Formelle", Key: "sales:Маркетплейсы", Label: "Маркетплейсы", BYN: 3000},
			{Legal: "F", Key: "exp:Аренда", Label: "Аренда", BYN: 100},
			{Legal: "F", Key: "exp:Заработная плата", Label: "Заработная плата", BYN: 50},
			{Legal: leNoEntity, Key: "exp:Аренда", Label: "Аренда", BYN: 7},
		},
		Adjustments: []LeAdjust{
			{ID: 1, LegalEntity: "F", LineKey: "sales:Розница РБ", AmountBYN: -200, Reason: "возврат"},
			{ID: 2, LegalEntity: "F", LineKey: "sales:Розница РБ", AmountBYN: 999, Reason: "ошибка", Revoked: true},
			{ID: 3, LegalEntity: "MF IT", LineKey: "exp:Расходы на ИТ", AmountBYN: 30, Reason: "новая статья"},
		},
		GroupOrder: []string{"Заработная плата", "Аренда"},
	}
	s := buildLeSvod(in)

	if got := strings.Join(s.Legals, "|"); got != "F|MF IT|TD Mark Formelle|"+leNoEntity {
		t.Fatalf("колонки ЮЛ: %s", got)
	}
	r := leRow(t, s, "sales:Розница РБ")
	if !leNear(r.Calc["F"], 1000) || !leNear(r.Adj["F"], -200) || !leNear(r.Total["F"], 800) || !leNear(r.TotAll, 800) {
		t.Errorf("корректировка продаж: %+v", r)
	}
	if st := leRow(t, s, leKeySalesTotal); !leNear(st.TotAll, 3800) || !leNear(st.Total["F"], 800) || !leNear(st.AdjAll, -200) {
		t.Errorf("итого продажи: %+v", st)
	}
	// Строка, появившаяся только из корректировки, видна и учтена в итоге расходов.
	if r := leRow(t, s, "exp:Расходы на ИТ"); !leNear(r.Total["MF IT"], 30) || !r.Editable {
		t.Errorf("строка из корректировки: %+v", r)
	}
	et := leRow(t, s, leKeyExpTotal)
	if !leNear(et.TotAll, 187) || !leNear(et.Total["F"], 150) {
		t.Errorf("итого расходы: %+v", et)
	}
	// Порядок групп расходов: сначала по порядку листа Excel, затем остальные по алфавиту.
	var order []string
	for _, row := range s.Rows {
		if row.Section == LeSectionExp {
			order = append(order, row.Label)
		}
	}
	if strings.Join(order, "|") != "Заработная плата|Аренда|Расходы на ИТ" {
		t.Errorf("порядок групп: %v", order)
	}
	// Отношение: у ЮЛ F расходы 150 к продажам 800; у MF IT продаж нет — отношения нет.
	ratio := leRow(t, s, leKeyRatio)
	if ratio.Ratio["F"] == nil || !leNear(*ratio.Ratio["F"], 18.75) || ratio.Ratio["MF IT"] != nil {
		t.Errorf("отношение расходов к продажам: %+v", ratio.Ratio)
	}
	if ratio.RatioAll == nil || !leNear(*ratio.RatioAll, 187.0/3800*100) {
		t.Errorf("общее отношение: %v", ratio.RatioAll)
	}
	// Снятая корректировка не попадает в активные.
	if len(s.Adjustments) != 2 {
		t.Errorf("активных корректировок %d, ждали 2", len(s.Adjustments))
	}
	// Итоговые строки корректировать нельзя.
	if leRow(t, s, leKeySalesTotal).Editable || leRow(t, s, leKeyExpTotal).Editable {
		t.Error("итоговые строки не редактируются")
	}
}

func TestValidateLeAdjust(t *testing.T) {
	ok := LeAdjust{LegalEntity: "F", LineKey: "exp:Аренда", AmountBYN: 5, Reason: "уточнение"}
	if err := validateLeAdjust(ok); err != nil {
		t.Fatal(err)
	}
	for name, mut := range map[string]func(*LeAdjust){
		"без ЮЛ":      func(a *LeAdjust) { a.LegalEntity = " " },
		"без причины": func(a *LeAdjust) { a.Reason = "  " },
		"ноль":        func(a *LeAdjust) { a.AmountBYN = 0 },
		"чужой ключ":  func(a *LeAdjust) { a.LineKey = "total:exp" },
	} {
		a := ok
		mut(&a)
		if err := validateLeAdjust(a); err == nil {
			t.Errorf("%s: ждали ошибку", name)
		}
	}
}

var _ = context.Background
