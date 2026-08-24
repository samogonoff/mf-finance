package plans

import "testing"

// Правило по стране: доступ к ЦФО своей страны, отказ по чужой (ТЗ Розница §9).
func TestScopeFilter_CountryRule(t *testing.T) {
	f := NewScopeFilter(false, []UserScope{{Country: "BY", LegalEntity: "F"}})

	if !f.Allows(100, "BY", "F") {
		t.Fatal("магазин своей страны должен быть доступен")
	}
	if f.Allows(105, "RU", "TDMF") {
		t.Fatal("магазин чужой страны должен быть недоступен")
	}
	if !f.Unrestricted() {
		t.Fatal("правило без списка ЦФО не ограничивает набор ЦФО внутри страны")
	}
}

// Правило по списку площадок: чужая площадка недоступна даже в своей стране.
func TestScopeFilter_CfoRule(t *testing.T) {
	f := NewScopeFilter(false, []UserScope{{Country: "RU", CodeCFO: []int{335, 336}}})

	if !f.Allows(335, "RU", "") {
		t.Fatal("своя площадка должна быть доступна")
	}
	if f.Allows(337, "RU", "") {
		t.Fatal("площадка вне списка должна быть недоступна")
	}
	if f.Unrestricted() {
		t.Fatal("правило со списком ЦФО ограничивает набор")
	}
	if got := f.CodeCFOs(); len(got) != 2 {
		t.Fatalf("набор ЦФО: %v", got)
	}
}

// Разные правила складываются: large по площадкам + вся розница РБ.
func TestScopeFilter_RulesUnion(t *testing.T) {
	f := NewScopeFilter(false, []UserScope{
		{Country: "RU", CodeCFO: []int{335}},
		{Country: "BY"},
	})
	if !f.Allows(335, "RU", "") || !f.Allows(100, "BY", "F") {
		t.Fatal("объединение правил должно давать доступ к обоим объектам")
	}
	if f.Allows(954, "RU", "") {
		t.Fatal("площадка вне правил RU недоступна")
	}
}

// Право на шаг: правило без stage_code разрешает любой шаг, с кодом — только его.
func TestScopeFilter_StepRule(t *testing.T) {
	any := NewScopeFilter(false, []UserScope{{Country: "BY"}})
	if !any.AllowsStep("1.2") {
		t.Fatal("правило без указания этапа разрешает любой шаг")
	}
	only := NewScopeFilter(false, []UserScope{{Country: "BY", StageCode: "1.1"}})
	if !only.AllowsStep("1.1") || only.AllowsStep("1.3") {
		t.Fatal("правило с этапом ограничивает только им")
	}
}

// Админ обходит ABAC; пустой срез не даёт прав.
func TestScopeFilter_AdminAndEmpty(t *testing.T) {
	if !NewScopeFilter(true, nil).Allows(999, "XX", "YY") {
		t.Fatal("админ планов обходит ABAC")
	}
	if NewScopeFilter(false, nil).Allows(335, "RU", "") {
		t.Fatal("пустой срез не даёт прав")
	}
}

// Доступ к карточке формы — по стране и ЮЛ карточки.
func TestScopeFilter_AllowsCard(t *testing.T) {
	f := NewScopeFilter(false, []UserScope{{Country: "KZ", LegalEntity: "MFKaz"}})
	if !f.AllowsCard(Card{Country: "KZ", LegalEntity: "MFKaz"}) {
		t.Fatal("карточка своей страны доступна")
	}
	if f.AllowsCard(Card{Country: "UZ", LegalEntity: "MFUz"}) {
		t.Fatal("карточка чужой страны недоступна")
	}
}
