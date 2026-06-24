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

// T1: справочник код→ИНН. Table_Fin_PL.Компания хранит короткий код, ИНН там нет —
// резолвим через этот map. Коды и ИНН сняты с OLAP (vGLMFAddUSD).
func TestINNByCode(t *testing.T) {
	want := map[string]string{
		// ЮЛ из ТЗ, фигурирующие в PL-данных как Компания
		"MF":    "690591512",
		"F":     "690719790",
		"TDMF":  "6950135110",
		"MFTex": "5031159833",
		"MFT":   "9909349268",
		"PTIR":  "9731039708",
		"MFKaz": "141240004842",
		"MFUz":  "305554644",
		"BR":    "310170662",
		// ВГО-компании сверх 15 (нет в seed-списке, есть во флаге [ВГО]=1)
		"DR":  "692221084",
		"DR2": "693335015",
		"GP":  "190465888",
	}
	for code, inn := range want {
		got, ok := INNByCode(code)
		if !ok {
			t.Errorf("INNByCode(%q): код не найден в справочнике", code)
			continue
		}
		if got != inn {
			t.Errorf("INNByCode(%q) = %q, ожидался %q", code, got, inn)
		}
	}
	if _, ok := INNByCode("НЕТ_ТАКОГО"); ok {
		t.Errorf("INNByCode(неизвестный) должен вернуть ok=false")
	}
	if _, ok := INNByCode(""); ok {
		t.Errorf("INNByCode(\"\") должен вернуть ok=false")
	}
}

// EntityByCode даёт страну/имя/код — нужно finpl-репо для заполнения DebtRow по коду.
func TestEntityByCode(t *testing.T) {
	dr, ok := EntityByCode("DR")
	if !ok {
		t.Fatalf("EntityByCode(\"DR\"): не найдено")
	}
	if dr.Country != CountryRB {
		t.Errorf("DR.Country = %q, ожидалась %q (Дримдом — РБ)", dr.Country, CountryRB)
	}
	if dr.INN != "692221084" {
		t.Errorf("DR.INN = %q, ожидался 692221084", dr.INN)
	}
	mf, ok := EntityByCode("MF")
	if !ok || mf.Code != "MF" || mf.INN != "690591512" {
		t.Errorf("EntityByCode(\"MF\") = %+v, ok=%v", mf, ok)
	}
}

// Фаза 0 — без смены поведения: ВГО-компании сверх 15 (DR/DR2/GP) НЕ должны
// протекать в Entities()/OurINNs()/Level1, чтобы не расширить ВГО-фильтр текущего
// mssql-бэкенда и UI-фильтр до cutover (T9).
func TestExtraVGONotInSeedLists(t *testing.T) {
	extra := map[string]bool{"692221084": true, "693335015": true, "190465888": true}
	for _, inn := range OurINNs() {
		if extra[inn] {
			t.Errorf("OurINNs() не должен содержать ВГО-extra %q до cutover", inn)
		}
	}
	for _, e := range EntitiesLevel1() {
		if e.Code == "DR" || e.Code == "DR2" || e.Code == "GP" {
			t.Errorf("EntitiesLevel1() не должен содержать %q до cutover", e.Code)
		}
	}
}
