package plans

import (
	"context"
	"math"
	"testing"
)

// Нормативный НДС статьи (правки ТЗ 02.09.2026, часть 5). Проверяем на статьях,
// на которых аналитик подтвердил трактовку колонок VAT_*: если кто-то «упростит»
// НДС до одной ставки на страну, ломается именно здесь.

func vatEq(t *testing.T, got float64, ok bool, want float64, what string) {
	t.Helper()
	if !ok {
		t.Fatalf("%s: значение должно быть найдено в справочнике", what)
	}
	if math.Abs(got-want) > 1e-9 {
		t.Fatalf("%s: got %.4f, want %.4f", what, got, want)
	}
}

func TestNormativeVat_CoefficientBecomesRate(t *testing.T) {
	b := NewRateBook(nil) // без репозитория — работа на сиде
	ctx := context.Background()

	// Общие ставки: коэффициент 1,20 → 20 %, 1,22 → 22 % и так далее.
	got, ok := b.NormativeVat(ctx, 54, "BY")
	vatEq(t, got, ok, 0.20, "аренда помещений, BY")
	got, ok = b.NormativeVat(ctx, 54, "RU")
	vatEq(t, got, ok, 0.22, "аренда помещений, RU")
	got, ok = b.NormativeVat(ctx, 54, "KZ")
	vatEq(t, got, ok, 0.16, "аренда помещений, KZ")
	got, ok = b.NormativeVat(ctx, 54, "UZ")
	vatEq(t, got, ok, 0.12, "аренда помещений, UZ")
}

// Международные перевозки (50, 51, 93) НДС не облагаются: в справочнике по всем
// странам ровно 1,00. Это «ставка 0», а не «нет данных» — различать обязательно.
func TestNormativeVat_InternationalTransportIsZeroNotMissing(t *testing.T) {
	b := NewRateBook(nil)
	ctx := context.Background()
	for _, code := range []int{50, 51, 93} {
		for _, country := range []string{"BY", "RU", "KZ", "UZ", "TR"} {
			got, ok := b.NormativeVat(ctx, code, country)
			if !ok {
				t.Fatalf("статья %d/%s: значение обязано быть найдено (1,00 в справочнике)", code, country)
			}
			if got != 0 {
				t.Fatalf("статья %d/%s: перевозки НДС не облагаются, got %.4f", code, country, got)
			}
		}
	}
}

// Статья 49 (связь и интернет) в Беларуси — отдельная ставка 25 %.
func TestNormativeVat_TelecomBY25(t *testing.T) {
	b := NewRateBook(nil)
	got, ok := b.NormativeVat(context.Background(), 49, "BY")
	vatEq(t, got, ok, 0.25, "связь и интернет, BY")
}

// Статьи 62/63 — единицы по KZ, UZ, TR при 20/22 % по BY и RU.
func TestNormativeVat_ServicesZeroInKZUZTR(t *testing.T) {
	b := NewRateBook(nil)
	ctx := context.Background()
	for _, code := range []int{62, 63} {
		for _, country := range []string{"KZ", "UZ", "TR"} {
			got, ok := b.NormativeVat(ctx, code, country)
			if !ok || got != 0 {
				t.Fatalf("статья %d/%s: ожидалось 0 (коэффициент 1,00), got %.4f ok=%v", code, country, got, ok)
			}
		}
		got, ok := b.NormativeVat(ctx, code, "RU")
		vatEq(t, got, ok, 0.22, "статья по RU")
	}
	// Статья 68 — единица только по UZ.
	got, ok := b.NormativeVat(ctx, 68, "UZ")
	if !ok || got != 0 {
		t.Fatalf("приобретение имущества, UZ: ожидалось 0, got %.4f ok=%v", got, ok)
	}
	got, ok = b.NormativeVat(ctx, 68, "KZ")
	vatEq(t, got, ok, 0.16, "приобретение имущества, KZ")
}

// Нет статьи в справочнике — «нет значения», а не «ставка 0»: иначе проверка
// границей молча пропустит любую введённую ставку.
func TestNormativeVat_UnknownArticleReportsMissing(t *testing.T) {
	b := NewRateBook(nil)
	if got, ok := b.NormativeVat(context.Background(), 999999, "BY"); ok {
		t.Fatalf("неизвестная статья должна возвращать ok=false, got %.4f", got)
	}
}

// Нормативная ставка — верхняя граница проверки (ТЗ МП §2): эффективная ниже
// нормативной — норма, выше — ошибка ввода.
func TestVatExceedsNormative(t *testing.T) {
	b := NewRateBook(nil)
	ctx := context.Background()

	// Эффективная 19,6 % по РФ при нормативной 22 % — расхождение штатное.
	if over, norm := b.VatExceedsNormative(ctx, 54, "RU", 0.196); over {
		t.Fatalf("19,6 %% не должно считаться превышением нормативной %.4f", norm)
	}
	// Ровно нормативная — не превышение.
	if over, _ := b.VatExceedsNormative(ctx, 54, "RU", 0.22); over {
		t.Fatal("ставка, равная нормативной, не является превышением")
	}
	// Выше нормативной — ошибка ввода.
	over, norm := b.VatExceedsNormative(ctx, 54, "RU", 0.25)
	if !over {
		t.Fatal("ставка выше нормативной обязана помечаться как превышение")
	}
	if math.Abs(norm-0.22) > 1e-9 {
		t.Fatalf("в ответе должна быть нормативная ставка, got %.4f", norm)
	}
	// Статьи нет в справочнике → проверять нечем, превышения не объявляем.
	if over, _ := b.VatExceedsNormative(ctx, 999999, "RU", 0.99); over {
		t.Fatal("без нормативной ставки превышение объявлять нельзя")
	}
}
