package bugtracker

import (
	"strings"
	"testing"
)

// baseInput — минимальный валидный CreateInput для теста сигнатуры.
func baseInput() CreateInput {
	return CreateInput{
		Section:     "operations",
		Type:        TypeBug,
		Title:       "Дата пропадает",
		Description: "После применения фильтра дата сбрасывается",
		Route: map[string]any{
			"url":  "http://finance.local/operations?from=2026-01-01",
			"path": "/operations",
		},
	}
}

func TestComputeSignature_IsDeterministic(t *testing.T) {
	a := computeSignature(baseInput())
	b := computeSignature(baseInput())
	if a != b {
		t.Errorf("computeSignature must be deterministic\n  a = %q\n  b = %q", a, b)
	}
}

func TestComputeSignature_IsSHA256Hex(t *testing.T) {
	got := computeSignature(baseInput())
	if len(got) != 64 {
		t.Errorf("signature must be 64-char hex (SHA256), got %d chars: %q", len(got), got)
	}
	for _, c := range got {
		if !((c >= '0' && c <= '9') || (c >= 'a' && c <= 'f')) {
			t.Errorf("signature must be lowercase hex, found %q in %q", c, got)
			break
		}
	}
}

func TestComputeSignature_ChangesWithSection(t *testing.T) {
	a := computeSignature(baseInput())
	in := baseInput()
	in.Section = "reports"
	b := computeSignature(in)
	if a == b {
		t.Errorf("signature must depend on section")
	}
}

func TestComputeSignature_ChangesWithURL(t *testing.T) {
	a := computeSignature(baseInput())
	in := baseInput()
	in.Route = map[string]any{"url": "http://finance.local/reports"}
	b := computeSignature(in)
	if a == b {
		t.Errorf("signature must depend on route.url")
	}
}

func TestComputeSignature_ChangesWithTitle(t *testing.T) {
	a := computeSignature(baseInput())
	in := baseInput()
	in.Title = "Совсем другой заголовок"
	b := computeSignature(in)
	if a == b {
		t.Errorf("signature must depend on title")
	}
}

func TestComputeSignature_ChangesWithDescription(t *testing.T) {
	a := computeSignature(baseInput())
	in := baseInput()
	in.Description = "Другое описание"
	b := computeSignature(in)
	if a == b {
		t.Errorf("signature must depend on description")
	}
}

func TestComputeSignature_IsCaseInsensitive(t *testing.T) {
	a := computeSignature(baseInput())
	in := baseInput()
	in.Section = strings.ToUpper(in.Section)
	in.Title = strings.ToUpper(in.Title)
	in.Description = strings.ToUpper(in.Description)
	in.Route = map[string]any{"url": strings.ToUpper(in.Route["url"].(string))}
	b := computeSignature(in)
	if a != b {
		t.Errorf("signature must be case-insensitive\n  lower = %q\n  upper = %q", a, b)
	}
}

func TestComputeSignature_IgnoresEdgeWhitespaceInTitleAndDescription(t *testing.T) {
	a := computeSignature(baseInput())
	in := baseInput()
	in.Title = "   " + in.Title + "   "
	in.Description = "\n\t" + in.Description + "\n"
	b := computeSignature(in)
	if a != b {
		t.Errorf("signature must ignore leading/trailing whitespace in title and description")
	}
}

// Документация по дизайну дедупа: два разных пользователя, один и тот же баг
// → сигнатура одинаковая, но FindRecentDuplicate смотрит по user_id.
// Поэтому сигнатура НЕ зависит от user_id (мы не знаем юзера в computeSignature).
func TestComputeSignature_DoesNotDependOnUser(t *testing.T) {
	// computeSignature не принимает user_id вовсе — проверка-документация:
	// если кто-то добавит userID в подпись, эти тесты ловить не будут, и это
	// сломает поведение FindRecentDuplicate (там фильтр по user_id отдельный).
	// Этот тест существует как живая документация контракта.
	got := computeSignature(baseInput())
	if got == "" {
		t.Fatalf("sanity check")
	}
}

func TestSignature_DiffersBetweenDifferentReports(t *testing.T) {
	// Реальные кейсы: два независимых баг-репорта должны иметь разные сигнатуры.
	rep1 := CreateInput{
		Section:     "cost",
		Title:       "Не загружаются цены",
		Description: "При выборе бренд-менеджера таблица пустая",
		Route:       map[string]any{"url": "http://finance.local/cost"},
	}
	rep2 := CreateInput{
		Section:     "operations",
		Title:       "Экспорт CSV падает",
		Description: "При нажатии на «Экспорт CSV» падает 500",
		Route:       map[string]any{"url": "http://finance.local/operations"},
	}
	if computeSignature(rep1) == computeSignature(rep2) {
		t.Errorf("two distinct reports must produce distinct signatures")
	}
}
