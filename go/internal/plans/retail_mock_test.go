package plans

import "testing"

// Фикстура обязана давать объём контрольной выборки ТЗ (Приложение Б): РБ 191,
// РФ 74, KZ 45, UZ 6 действующих магазинов и 58+ закрытых — всего 375 строк.
//
// Это не косметика: на 18 магазинах не проверяются ни виртуализация сетки
// (НФТ §11), ни группировки свода, ни фильтры — любая реализация выглядит
// рабочей. Тест ловит случайное «упрощение» фикстуры.
func TestMockRetail_VolumesMatchAcceptanceSample(t *testing.T) {
	active := map[string]int{}
	closed := map[string]int{}
	noKlient := map[string]int{}
	for _, m := range mockRetailStores() {
		c := mockRetailCountryOf(m)
		if m.close != "" {
			closed[c]++
			continue
		}
		active[c]++
		if m.klient == "" {
			noKlient[c]++
		}
	}

	want := map[string]int{"BY": 191, "RU": 74, "KZ": 45, "UZ": 6}
	for country, n := range want {
		if active[country] != n {
			t.Errorf("%s: действующих магазинов %d, по ТЗ должно быть %d", country, active[country], n)
		}
	}
	totalClosed := 0
	for _, n := range closed {
		totalClosed += n
	}
	if totalClosed < 58 {
		t.Errorf("закрытых магазинов %d, по ТЗ не меньше 58", totalClosed)
	}
	total := totalClosed
	for _, n := range active {
		total += n
	}
	if total < 375 {
		t.Errorf("всего строк %d, контрольная выборка ТЗ — 375", total)
	}
	// «Ххх» (магазин есть, KLIENT_ID ещё нет) — ТЗ §1 п.6d: 10 таких точек в РБ.
	if noKlient["BY"] < 10 {
		t.Errorf("магазинов РБ без KLIENT_ID %d, по ТЗ 10", noKlient["BY"])
	}
}

// Фикстура режется страной: форма РБ не должна видеть магазины РФ (ТЗ §2.0 —
// экземпляр формы = страна).
func TestMockRetail_StoresScopedByCountry(t *testing.T) {
	src := NewMockRetailSource()
	for _, c := range []string{"BY", "RU", "KZ", "UZ"} {
		rows, err := src.Stores(t.Context(), c)
		if err != nil {
			t.Fatalf("%s: %v", c, err)
		}
		if len(rows) == 0 {
			t.Fatalf("%s: фикстура пуста", c)
		}
		le, _ := retailLegalEntity(c)
		for _, r := range rows {
			if r.Country != c {
				t.Fatalf("%s: пришёл магазин страны %s (ЦФО %d)", c, r.Country, r.CodeCFO)
			}
			// Одно реальное ЮЛ на страну (§4.2.1) — иначе V-08 не на чем проверить.
			if r.CompanyMF != le {
				t.Fatalf("%s: ЮЛ магазина %d = %q, ожидалось %q", c, r.CodeCFO, r.CompanyMF, le)
			}
		}
	}
}

// Факт приходит только по своей стране и только за месяцы, когда магазин работал.
func TestMockRetail_FactScopedAndBounded(t *testing.T) {
	src := NewMockRetailSource()
	rows, err := src.Fact(t.Context(), "KZ", []int{2025, 2026})
	if err != nil {
		t.Fatalf("факт KZ: %v", err)
	}
	if len(rows) == 0 {
		t.Fatal("факт KZ пуст")
	}
	kzCodes := map[int]bool{}
	for _, m := range mockRetailStores() {
		if mockRetailCountryOf(m) == "KZ" {
			kzCodes[m.code] = true
		}
	}
	for _, c := range rows {
		if !kzCodes[c.CodeCFO] {
			t.Fatalf("в факте KZ магазин чужой страны: ЦФО %d", c.CodeCFO)
		}
		if c.Year == 2026 && c.Month > mockRetailLastMonth(2026) {
			t.Fatalf("факт за незакрытый месяц: %d.%d", c.Month, c.Year)
		}
	}
}
