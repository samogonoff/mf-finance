package debt

import "testing"

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
