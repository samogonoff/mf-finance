package plans

import "fmt"

// Генератор синтетических магазинов фикстуры — добор ядра до объёма ТЗ
// (Приложение Б): РБ 191, РФ 74, KZ 45, UZ 6 действующих и 58 закрытых.
//
// Почему объём важен: НФТ ТЗ §11 требуют виртуализации сетки («375 строк × 12
// месяцев»), а на 18 магазинах ядра она не проверяется — все строки помещаются в
// окно рендера, и нерабочая виртуализация выглядит как рабочая. Тот же объём
// нужен фильтрам, разрезам свода (§8) и контрольным сверкам.
//
// Генерация детерминированная: коды, названия и суммы выводятся из индекса, без
// random. Иначе mock менялся бы между запусками, и сверки чисел поплыли бы.

// mockRetailCountryPlan — сколько магазинов какой страны должно быть в фикстуре.
type mockRetailCountryPlan struct {
	country string
	// active — действующих магазинов (ТЗ, Приложение Б).
	active int
	// closed — закрытых: они попадают в форму только при включённом
	// переключателе «показывать закрытые» и питают V-03/V-09.
	closed int
	// noKlient — магазинов без KLIENT_ID (статус «ххх»): ТЗ §1 п.6d.
	noKlient int
	// baseFrom/baseTo — «средний месяц» в национальной валюте: у KZT и UZS
	// порядок сумм принципиально другой, и на нём проверяется форматирование.
	baseFrom, baseTo float64
	codeFrom         int
	cityPrefix       string
}

// mockRetailCountryPlans — целевой состав фикстуры по странам.
// Числа действующих — из контрольной выборки ТЗ; закрытые распределены по
// странам так, чтобы в сумме дать 58.
func mockRetailCountryPlans() []mockRetailCountryPlan {
	return []mockRetailCountryPlan{
		{country: "BY", active: 191, closed: 30, noKlient: 10, baseFrom: 88000, baseTo: 420000, codeFrom: 1400, cityPrefix: "РБ"},
		{country: "RU", active: 74, closed: 18, noKlient: 8, baseFrom: 1_900_000, baseTo: 9_400_000, codeFrom: 2400, cityPrefix: "РФ"},
		{country: "KZ", active: 45, closed: 8, noKlient: 4, baseFrom: 9_000_000, baseTo: 41_000_000, codeFrom: 3400, cityPrefix: "KZ"},
		{country: "UZ", active: 6, closed: 2, noKlient: 0, baseFrom: 900_000_000, baseTo: 2_400_000_000, codeFrom: 4400, cityPrefix: "UZ"},
	}
}

// mockRetailCityNames — города по странам. Список короткий и повторяется с
// номером: фильтр «по городу» должен группировать по несколько магазинов,
// иначе группировка свода (§8) не проверяется.
func mockRetailCityNames(country string) []string {
	switch country {
	case "RU":
		return []string{"Москва", "Санкт-Петербург", "Воронеж", "Казань", "Екатеринбург", "Ростов-на-Дону"}
	case "KZ":
		return []string{"Алматы", "Астана", "Шымкент", "Караганда"}
	case "UZ":
		return []string{"Ташкент", "Самарканд"}
	default:
		return []string{"Минск", "Гомель", "Брест", "Витебск", "Гродно", "Могилев", "Бобруйск", "Барановичи"}
	}
}

// mockRetailManagers — РМ по странам: права РМ (§9) проверяются только тогда,
// когда у одного менеджера НЕСКОЛЬКО магазинов, а у формы — несколько менеджеров.
func mockRetailManagers(country string) []string {
	switch country {
	case "RU":
		return []string{"Антипова О.В.", "Левин А.С."}
	case "KZ":
		return []string{"Качановская Е.П."}
	case "UZ":
		return []string{"Мурашко Ф.И."}
	default:
		return []string{"Смолер О.В.", "Ковалёва И.П."}
	}
}

// mockRetailGeneratedStores — добор фикстуры до объёма ТЗ.
func mockRetailGeneratedStores() []mockRetailStore {
	types := []string{"стрит", "ТЦ", "распродажа", "косметика"}
	cats := []string{"A", "B", "C"}
	var out []mockRetailStore

	for _, p := range mockRetailCountryPlans() {
		cities := mockRetailCityNames(p.country)
		managers := mockRetailManagers(p.country)
		// Сколько ядро уже даёт по этой стране — синтетикой добираем остаток.
		coreActive := 0
		if p.country == "BY" {
			for _, m := range mockRetailCoreStores() {
				if !mockRetailClosed(m) {
					coreActive++
				}
			}
		}
		need := p.active - coreActive
		if need < 0 {
			need = 0
		}

		for i := 0; i < need+p.closed; i++ {
			closed := i >= need
			code := p.codeFrom + i
			city := cities[i%len(cities)]
			// «Средний месяц» распределяем по диапазону страны равномерно —
			// суммы получаются разными, но воспроизводимыми.
			span := p.baseTo - p.baseFrom
			base := p.baseFrom
			if need+p.closed > 1 {
				base += span * float64(i) / float64(need+p.closed-1)
			}

			st := mockRetailStore{
				code:      code,
				country:   p.country,
				name:      fmt.Sprintf("%s · точка %d", city, i+1),
				city:      city,
				klient:    fmt.Sprintf("%d", 9000+code),
				lfl:       LFLYes,
				storeType: types[i%len(types)],
				category:  cats[i%len(cats)],
				rm:        managers[i%len(managers)],
				area:      float64(80 + (i*7)%320),
				open:      "2019-05-06",
				base:      base,
			}
			switch {
			case closed:
				// Закрытые: RegManager у них в источнике 'Closed'/'n/a' (§9),
				// и это значение должно исключаться при разборе прав.
				st.lfl, st.rm, st.close = LFLClosed, "Closed", "2026-01-31"
			case i >= need-p.noKlient && p.noKlient > 0:
				// «ххх»: магазина ещё нет в 1С, KLIENT_ID пустой, факта нет —
				// к таким не применяется индекс роста и не считаются LFL/% вып.
				st.lfl, st.klient, st.base, st.open = LFLNoID, "", 0, "2026-04-01"
			case i%17 == 3:
				// «до года» — открыт в сентябре прошлого года.
				st.lfl, st.open = LFLUnder1, "2025-09-15"
			case i%23 == 5:
				// «новый» — открыт в марте текущего.
				st.lfl, st.open, st.base = LFLNew, "2026-03-02", 0
			}
			out = append(out, st)
		}
	}
	return out
}

// mockRetailCurrency — национальная валюта страны фикстуры (ТЗ §4.3: ввод и факт
// живут в валюте страны, а не в BYN).
func mockRetailCurrency(country string) string {
	switch country {
	case "RU":
		return "RUB"
	case "KZ":
		return "KZT"
	case "UZ":
		return "UZS"
	default:
		return "BYN"
	}
}

// mockRetailCountryOf — страна магазина фикстуры (у ядра поле пустое — это РБ).
func mockRetailCountryOf(m mockRetailStore) string {
	if m.country == "" {
		return "BY"
	}
	return m.country
}
