package plans

import (
	"context"
	"testing"
	"time"
)

// Подсказки к полям ввода (правки ТЗ 02.09.2026, часть 1 и МП §8/§9/§14).
// Главное свойство: подсказка живёт РЯДОМ со значением и никогда не попадает в
// само поле — иначе через одно сохранение она станет неотличима от решения
// планировщика.

func hintDir() fakeDirRows {
	return fakeDirRows{rows: []DirRow{
		{Payload: map[string]any{
			"code_cfo": 335, "spp_pct": 0.2179, "as_of": "2026-08",
			"source": "MP.dbo.WB_Price_SPP_Analysis × веса MP.dbo.sales (EAN)",
		}},
		{Payload: map[string]any{"code_cfo": 337, "amount": 154_000.0, "as_of": "2026-08"}},
	}}
}

func TestHintSource_SppFromDirectory(t *testing.T) {
	h := NewHintSource(hintDir())
	got, ok := h.SppHint(context.Background(), 335)
	if !ok {
		t.Fatal("подсказка %СПП по площадке 335 должна найтись")
	}
	if got.Value != 0.2179 || got.BlockType != BSPP || got.Kind != HintSpp {
		t.Fatalf("подсказка собрана неверно: %+v", got)
	}
	if got.Source == "" {
		t.Fatal("подсказка без происхождения бесполезна: пользователь не решит, доверять ли ей")
	}
	if got.AsOf != "2026-08" {
		t.Fatalf("период подсказки: got %q", got.AsOf)
	}
	if _, ok := h.SppHint(context.Background(), 999); ok {
		t.Fatal("для площадки без строки подсказки быть не должно")
	}
}

// У подсказки по штрафам оговорка обязательна: в источнике только габаритные
// штрафы Wildberries, а не общий реестр (правка МП §14).
func TestHintSource_PenaltyCarriesPartialityNote(t *testing.T) {
	h := NewHintSource(hintDir())
	got, ok := h.PenaltyHint(context.Background(), 337)
	if !ok {
		t.Fatal("подсказка по штрафам должна найтись")
	}
	if got.Value != 154_000 || got.BlockType != BCostPenalties {
		t.Fatalf("подсказка собрана неверно: %+v", got)
	}
	if got.Note == "" {
		t.Fatal("оговорка «часть, а не итог» обязательна — иначе подсказку примут за итог штрафов")
	}
}

// Справочника нет — подсказок нет, и это не ошибка: поля остаются полями ввода.
func TestHintSource_NilRepoIsSilent(t *testing.T) {
	h := NewHintSource(nil)
	if _, ok := h.SppHint(context.Background(), 335); ok {
		t.Fatal("без справочника подсказок быть не должно")
	}
	if _, ok := h.PenaltyHint(context.Background(), 335); ok {
		t.Fatal("без справочника подсказок быть не должно")
	}
}

// Подсказка считается за ПРОШЛЫЙ месяц: данные отчётного месяца готовы не раньше
// 20-го числа текущего (правка МП §12).
func TestHintPrevMonth(t *testing.T) {
	cases := []struct {
		now         time.Time
		year, month int
	}{
		{time.Date(2026, 9, 3, 12, 0, 0, 0, time.UTC), 2026, 8},
		{time.Date(2026, 1, 15, 0, 0, 0, 0, time.UTC), 2025, 12},
	}
	for _, c := range cases {
		y, m := hintPrevMonth(c.now)
		if y != c.year || m != c.month {
			t.Errorf("%s: got %04d-%02d, want %04d-%02d", c.now.Format("2006-01-02"), y, m, c.year, c.month)
		}
	}
}
