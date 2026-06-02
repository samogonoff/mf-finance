package debt

import (
	"strings"
	"testing"
)

// EntitiesLevel1 должен оставить только РФ+РБ юрлица — план счетов для остальных
// стран не подтверждён автором ТЗ (см. open-questions.md §A1).
func TestEntitiesLevel1_OnlyRfRb(t *testing.T) {
	got := EntitiesLevel1()
	if len(got) == 0 {
		t.Fatal("EntitiesLevel1 пуст")
	}
	for _, e := range got {
		if e.Country != CountryRF && e.Country != CountryRB {
			t.Errorf("EntitiesLevel1 содержит юрлицо вне РФ/РБ: %s (%s)", e.Name, e.Country)
		}
	}
}

// AccountsLevel1 должен содержать общие счета (Country == "") и счета РФ/РБ.
// КЗ/УЗ-счета — вне Level 1 MVP.
func TestAccountsLevel1_NoKzUz(t *testing.T) {
	got := AccountsLevel1()
	if len(got) == 0 {
		t.Fatal("AccountsLevel1 пуст")
	}
	for _, a := range got {
		if a.Country == CountryKZ || a.Country == CountryUZ {
			t.Errorf("AccountsLevel1 содержит КЗ/УЗ-счёт: %s (%s, %s)", a.Code, a.Country, a.Name)
		}
	}
	// Должны увидеть хотя бы один общий счёт (Country == "") — например, 62.
	found62 := false
	for _, a := range got {
		if a.Code == "62" && a.Country == "" {
			found62 = true
			break
		}
	}
	if !found62 {
		t.Error("AccountsLevel1 не содержит общий счёт 62 (РФ/РБ дебиторка)")
	}
}

// Полный список Entities() должен оставаться без изменений (15 юрлиц всех стран).
// Level 1 фильтр не должен мутировать seed.
func TestEntities_FullListUnchanged(t *testing.T) {
	all := Entities()
	if len(all) != 15 {
		t.Errorf("Entities() = %d, want 15 (полный список ГК МФ)", len(all))
	}
}

// OurINNs питает ВГО-фильтр импорта (bootstrap/incremental): строка считается
// ВГО, если контрагент входит в этот список. Источник истины — Entities().
func TestOurINNs_matchesEntities(t *testing.T) {
	got := OurINNs()

	if len(got) != len(Entities()) {
		t.Fatalf("OurINNs() len = %d, want %d (по одному ИНН на ЮЛ)", len(got), len(Entities()))
	}

	seen := map[string]bool{}
	for _, inn := range got {
		if inn == "" {
			t.Errorf("OurINNs() содержит пустой ИНН")
		}
		if inn != strings.TrimSpace(inn) {
			t.Errorf("OurINNs() содержит непротримленный ИНН %q", inn)
		}
		if seen[inn] {
			t.Errorf("OurINNs() содержит дубль ИНН %q", inn)
		}
		seen[inn] = true
	}

	for _, e := range Entities() {
		if !seen[strings.TrimSpace(e.INN)] {
			t.Errorf("OurINNs() не содержит ИНН %q (%s)", e.INN, e.Name)
		}
	}
}

// ПТИР (#4) должен попадать в список — это корень проблемы «расчёты есть, данных нет».
func TestOurINNs_includesPTIR(t *testing.T) {
	const ptir = "9731039708"
	for _, inn := range OurINNs() {
		if inn == ptir {
			return
		}
	}
	t.Fatalf("OurINNs() не содержит ИНН ПТИР %q", ptir)
}
