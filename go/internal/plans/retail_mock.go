package plans

import (
	"context"
	"math"
	"sort"
)

// Фикстуры формы «Розница» (PLANS_MOCK=1 или нет соединения с MSSQL).
//
// Зачем нужны: онлайн-источники ТЗ §11 живут за корп-VPN (10.10.6.15), а колонки
// таблицы факта (sales_and_COGG_from_FOX_offline_retail) ещё не подтверждены
// пробой. Без фикстур ни форму нельзя открыть в dev, ни прогнать тесты сервиса.
//
// Состав. Ядро — 18 магазинов РБ с РЕАЛЬНЫМИ CodeCFO и адресами из dir_cfo
// (миграция 0028), поэтому строки формы совпадают со справочником ЦФО. Набор
// специально разнородный, чтобы включались все ветки расчёта:
//   - 13 магазинов LFL (есть факт 2025 и 2026 → LFL/LFM считаются);
//   - 2 «до года» (открыты в 2025-09 → W-01/W-02 к ним не применяются, §6);
//   - 2 «новых» (открыты 2026-03, KLIENT_ID есть → LFL и % вып. не считаются, §4);
//   - 1 «ххх» (KLIENT_ID нет — та самая ситуация 10 новых магазинов РБ, §2);
//   - 1 закрытый (DateClose 2026-02 → V-03/V-09 наблюдаемы).
//
// Поверх ядра генерируются синтетические магазины ДО ОБЪЁМА ТЗ (Приложение Б):
// РБ 191, РФ 74, KZ 45, UZ 6 действующих плюс 58 закрытых — всего 375 строк.
// Без этого объёма нельзя проверить ни виртуализацию сетки (НФТ §11: «375 строк
// × 12 месяцев — виртуализация обязательна»), ни поведение фильтров и сводов на
// реальном количестве строк: на 18 магазинах любая реализация выглядит рабочей.
// Генерация ДЕТЕРМИНИРОВАННАЯ (никакого random): одинаковый mock на каждом
// запуске — иначе тесты и сверки чисел стали бы плавающими.
//
// Факт — в национальной валюте страны, помесячно 2025-01…2026-06.

// mockRetailStore — магазин фикстуры вместе с сезонным профилем факта.
type mockRetailStore struct {
	code      int
	name      string
	city      string
	klient    string
	lfl       string
	storeType string
	category  string
	rm        string
	area      float64
	open      string
	close     string
	// base — «средний месяц» 2025 года в национальной валюте; факт получается
	// умножением на сезонный коэффициент месяца и годовой уплотнитель.
	base float64
	// country — BY|RU|KZ|UZ. У магазинов ядра пусто, что читается как BY
	// (см. mockRetailCountryOf): ядро — реальные ЦФО РБ из dir_cfo.
	country string
}

// mockRetailStores — полный набор фикстуры: ядро РБ + синтетика до объёма ТЗ.
func mockRetailStores() []mockRetailStore {
	out := mockRetailCoreStores()
	return append(out, mockRetailGeneratedStores()...)
}

// mockRetailCoreStores — 18 магазинов РБ (CodeCFO и адреса — из dir_cfo, 0028).
func mockRetailCoreStores() []mockRetailStore {
	return []mockRetailStore{
		{100, "б-р Шевченко, 9", "Минск", "1641", LFLYes, "стрит", "B", "Смолер О.В.", 133.7, "2019-04-12", "", 168000, "BY"},
		{113, "ТЦ Манеж, Вильнюсское шоссе, 1", "Полоцк", "1669", LFLYes, "ТЦ", "A", "Смолер О.В.", 148.1, "2019-08-01", "", 152000, "BY"},
		{114, "ул.Ленина, 57", "Береза", "2246", LFLYes, "стрит", "A", "Смолер О.В.", 124.1, "2020-03-14", "", 121000, "BY"},
		{115, "м-н Первомайский, 3", "Светлогорск", "1676", LFLYes, "стрит", "B", "Смолер О.В.", 164, "2019-11-02", "", 134000, "BY"},
		{117, "ул.Ленина, 13", "Кобрин", "1721", LFLYes, "стрит", "B", "Смолер О.В.", 165.8, "2020-06-20", "", 129000, "BY"},
		{119, "ул.Сумченко, 63", "Осиповичи", "1794", LFLYes, "стрит", "B", "Ковалёва И.П.", 112.3, "2020-09-05", "", 118000, "BY"},
		{121, "пр-т Ленина, 22", "Жодино", "1824", LFLYes, "стрит", "A", "Ковалёва И.П.", 141.6, "2021-02-13", "", 146000, "BY"},
		{122, "ТЦ Простор, Каменногорская, 3", "Минск", "2023", LFLYes, "ТЦ", "B", "Смолер О.В.", 126.4, "2021-05-22", "", 158000, "BY"},
		{123, "ТЦ DANAMOL, П.Мстиславца, 4", "Минск", "2083", LFLYes, "ТЦ", "A", "Смолер О.В.", 283.7, "2021-09-18", "", 287000, "BY"},
		{124, "ТЦ ГРИН, ул.Притыцкого, 156", "Минск", "2358", LFLYes, "ТЦ", "A", "Смолер О.В.", 411.4, "2022-03-05", "", 418000, "BY"},
		{127, "ул.Ленина, 53", "Рогачев", "2092", LFLYes, "стрит", "B", "Ковалёва И.П.", 162.6, "2022-06-11", "", 124000, "BY"},
		{128, "ТЦ Планета GREEN, Островского, 5", "Могилев", "2093", LFLYes, "ТЦ", "B", "Ковалёва И.П.", 180, "2022-08-27", "", 171000, "BY"},
		{131, "ул.Замковая, 19", "Витебск", "2150", LFLYes, "стрит", "A", "Ковалёва И.П.", 401.6, "2023-04-08", "", 264000, "BY"},
		// «до года» — открыты 2025-09: факт есть не за все месяцы, W-01/W-02 не применяются (§6).
		{133, "ул.Мицкевича, 104Б", "Новогрудок", "2312", LFLUnder1, "стрит", "B", "Ковалёва И.П.", 95.2, "2025-09-12", "", 96000, "BY"},
		{134, "ул. М. Октябрьской, 73", "Лиозно", "2177", LFLUnder1, "стрит", "C", "Ковалёва И.П.", 126.2, "2025-09-26", "", 88000, "BY"},
		// «новые» — открыты 2026-03: LFL тактич. и % вып. не рассчитываются (§4).
		{136, "ул.Водопьянова 19", "Вилейка", "2277", LFLNew, "стрит", "A", "Ковалёва И.П.", 207.8, "2026-03-02", "", 0, "BY"},
		// «ххх» — магазин есть, KLIENT_ID ещё нет (§2: 10 новых магазинов РБ).
		{330, "ТЦ Экспобел, Дражня", "Минск", "", LFLNoID, "ТЦ", "B", "Смолер О.В.", 190.5, "2026-04-15", "", 0, "BY"},
		// Закрытый — DateClose до начала периода: V-03/V-09.
		{130, "ТЦ ГРИН, Шабаны", "Минск", "2098", LFLClosed, "ТЦ", "B", "Closed", 79, "2021-07-01", "2026-02-28", 74000, "BY"},
	}
}

// mockRetailSeason — сезонный профиль розницы: летний спад и декабрьский пик.
// Коэффициенты к «среднему месяцу», сумма ≈ 12.
func mockRetailSeason() [13]float64 {
	return [13]float64{0,
		0.86, 0.78, 0.95, 0.97, 1.02, 0.93,
		0.88, 1.05, 1.08, 1.06, 1.10, 1.32,
	}
}

// mockRetailYearFactor — годовой рост оборота: 2026 к 2025 ≈ +11 %.
func mockRetailYearFactor(year int) float64 {
	switch year {
	case 2025:
		return 1.0
	case 2026:
		return 1.11
	}
	return 1.0
}

// mockRetailLastMonth — последний месяц, за который есть ФАКТ. 2026 отфакчен по
// июнь: так в форме на июль 2026 наблюдаемы и «ср. мес. факт» (§4), и «ожидание
// года» (§4.4) — часть месяцев закрыта, часть плановая.
func mockRetailLastMonth(year int) int {
	switch {
	case year < 2026:
		return 12
	case year == 2026:
		return 6
	}
	return 0
}

// MockRetailSource — фикстуры источников розницы.
type MockRetailSource struct{}

// NewMockRetailSource — конструктор.
func NewMockRetailSource() *MockRetailSource { return &MockRetailSource{} }

// Stores — магазины фикстуры страны. Формы независимы по странам (ТЗ §2.0),
// поэтому и фикстура режется страной: запрос BY не должен видеть магазины РФ.
func (s *MockRetailSource) Stores(_ context.Context, country string) ([]RetailStore, error) {
	all := mockRetailStores()
	out := make([]RetailStore, 0, len(all))
	for _, m := range all {
		c := mockRetailCountryOf(m)
		if country != "" && country != c {
			continue
		}
		le, _ := retailLegalEntity(c)
		stage := "действующий"
		if m.close != "" {
			stage = "закрыт"
		}
		out = append(out, RetailStore{
			CodeCFO: m.code, KlientID: m.klient, NameCFO: m.name, GroupCFO1: "Магазины",
			City: m.city, Country: c, CodeFOX: "FOX" + itoa(int64(m.code)),
			Ploschad: m.area, StoreType: m.storeType, DateOpen: m.open, DateClose: m.close,
			Stage: stage, CompanyMF: le, Channel: "Розница", Category: m.category,
			LFLStatus: m.lfl, RegManager: m.rm, Manager: m.rm,
			PLAnalytic: "Розница " + c,
		})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].CodeCFO < out[j].CodeCFO })
	return out, nil
}

// Fact — факт продаж (выручка с НДС, BYN) по сезонному профилю. Магазин не даёт
// факта за месяцы до открытия и после закрытия — иначе W-05/W-06 и «ср. мес.
// факт» тестировались бы на невозможных данных.
func (s *MockRetailSource) Fact(_ context.Context, country string, years []int) ([]RetailFactCell, error) {
	season := mockRetailSeason()
	out := make([]RetailFactCell, 0, 4096)
	for _, m := range mockRetailStores() {
		if country != "" && country != mockRetailCountryOf(m) {
			continue
		}
		if m.base == 0 {
			continue // новые магазины и «ххх» факта ещё не имеют
		}
		for _, y := range years {
			last := mockRetailLastMonth(y)
			for mo := 1; mo <= last; mo++ {
				if retailStoreOpenedAfter(m.open, y, mo) {
					continue
				}
				if m.close != "" && retailStoreClosedBefore(m.close, y, mo) {
					continue
				}
				amt := m.base * season[mo] * mockRetailYearFactor(y)
				out = append(out, RetailFactCell{
					CodeCFO: m.code, Year: y, Month: mo,
					Amount: math.Round(amt*100) / 100,
				})
			}
		}
	}
	return out, nil
}

// Strategy — стратегия года: факт прошлого года × 1.15 (демо). Онлайн источник —
// Budgeting.dbo.VFORMTOLOADPLAN; в mock отдельного среза нет, но колонка
// «План стратегический» (§4) должна быть наблюдаема, как и «% к стратегии».
func (s *MockRetailSource) Strategy(_ context.Context, country string, year int) ([]RetailFactCell, error) {
	season := mockRetailSeason()
	out := make([]RetailFactCell, 0, 2048)
	for _, m := range mockRetailStores() {
		if country != "" && country != mockRetailCountryOf(m) {
			continue
		}
		if m.base == 0 || m.close != "" {
			continue
		}
		for mo := 1; mo <= 12; mo++ {
			amt := m.base * season[mo] * mockRetailYearFactor(year) * 1.15
			out = append(out, RetailFactCell{
				CodeCFO: m.code, Year: year, Month: mo, Amount: math.Round(amt*100) / 100,
			})
		}
	}
	return out, nil
}

// PlanHistory — ранее утверждённая тактика: стратегия × 0.97 (демо), плюс
// соответствие CodeCFO → KLIENT_ID (в [001 CodeCFO] такой колонки нет, §2).
func (s *MockRetailSource) PlanHistory(ctx context.Context, country string, year int) ([]RetailFactCell, map[int]string, error) {
	strat, err := s.Strategy(ctx, country, year)
	if err != nil {
		return nil, nil, err
	}
	out := make([]RetailFactCell, 0, len(strat))
	for _, c := range strat {
		c.Amount = math.Round(c.Amount*0.97*100) / 100
		out = append(out, c)
	}
	klient := map[int]string{}
	for _, m := range mockRetailStores() {
		if m.klient != "" {
			klient[m.code] = m.klient
		}
	}
	return out, klient, nil
}
