package plans

import "testing"

// Состав розницы по правилу ALL_view (правка ТЗ Розница §11). Проверяем ровно те
// ЦФО, которые аналитик назвал: на них ломается «свой список магазинов».

func TestRetailScope_RuleFromTZ(t *testing.T) {
	sc := NewRetailScope(RetailScopeSeed())

	// Магазин группы «3.Магазины» — розница.
	if !sc.Includes(101, "3.Магазины") {
		t.Fatal("магазин группы 3.Магазины должен входить в розницу")
	}
	// В справочнике та же группа называется «Магазины» (без номера) — это одно и
	// то же значение, сравнение не должно зависеть от префикса (ТЗ §6 п.1).
	if !sc.Includes(101, "Магазины") {
		t.Fatal("группа «Магазины» и «3.Магазины» — одно и то же")
	}
	// Восемь ЦФО-отделов входят в розницу, хотя их группа — не магазины.
	for _, cfo := range []int{36, 102, 203, 205, 304, 305, 412, 435} {
		if !sc.Includes(cfo, "2.Отделы") {
			t.Fatalf("ЦФО-отдел %d обязан входить в розницу (ALL_view)", cfo)
		}
	}
	// Исключения: интернет-магазин и техническая запись переезда.
	if sc.Includes(40, "3.Магазины") {
		t.Fatal("ЦФО 40 (интернет-магазин) не розница даже в группе магазинов")
	}
	if sc.Includes(722, "3.Магазины") {
		t.Fatal("ЦФО 722 (переезд магазина в Солигорске) обязан исключаться")
	}
	// Прочие группы — не розница.
	if sc.Includes(250, "4.Маркетплейсы") {
		t.Fatal("маркетплейсы не входят в состав розницы")
	}
}

// Исключение сильнее включения: если один и тот же ЦФО попал и в отделы, и в
// исключения, он не розница. Иначе правило зависело бы от порядка строк.
func TestRetailScope_ExcludeWinsOverInclude(t *testing.T) {
	sc := NewRetailScope([]RetailScopeRule{
		{Kind: ScopeIncludeCFO, Value: "40"},
		{Kind: ScopeExcludeCFO, Value: "40"},
	})
	if sc.Includes(40, "") {
		t.Fatal("исключение должно перебивать включение")
	}
}

// Правило берётся из данных: добавление ЦФО-отдела не требует релиза.
func TestRetailScope_RuleIsData(t *testing.T) {
	sc := NewRetailScope(append(RetailScopeSeed(),
		RetailScopeRule{Kind: ScopeIncludeCFO, Value: "999", Note: "новый отдел"}))
	if !sc.Includes(999, "2.Отделы") {
		t.Fatal("новый ЦФО-отдел из справочника должен входить в состав")
	}
}

// Отбор по статусу справочника, а не по наличию факта (правка §12): закрытая
// точка видна как закрытая, а новая (без факта) остаётся в списке.
func TestRetailStoreClosed_ByDirectoryStatus(t *testing.T) {
	cases := []struct {
		name string
		st   RetailStore
		want bool
	}{
		{"дата закрытия заполнена", RetailStore{DateClose: "2026-03-31"}, true},
		{"стадия Closed", RetailStore{Stage: "Closed"}, true},
		{"стадия «Закрыт»", RetailStore{Stage: "Закрыт"}, true},
		{"РМ = Closed", RetailStore{RegManager: "Closed"}, true},
		{"новая точка без факта", RetailStore{Stage: "Открыт", DateOpen: "2026-09-01"}, false},
		{"действующий магазин", RetailStore{Stage: "Работает", RegManager: "Иванов"}, false},
	}
	for _, c := range cases {
		if got := retailStoreClosed(c.st); got != c.want {
			t.Errorf("%s: got %v, want %v", c.name, got, c.want)
		}
	}
}

// Метрики формы (правка ТЗ 02.09.2026 Розница §30): аренда стала полем ввода, и
// статей аренды ДВЕ — 54 «Аренда помещений (стоянки)» и 55 «Аренда имущества».
// Одна метрика на две статьи не разложится при публикации.
func TestRetailMetrics_RentHasTwoArticles(t *testing.T) {
	if RetailMetricCodePL(MetricRent) != 54 {
		t.Fatalf("аренда помещений — статья 54, got %d", RetailMetricCodePL(MetricRent))
	}
	if RetailMetricCodePL(MetricRentAssets) != 55 {
		t.Fatalf("аренда имущества — статья 55, got %d", RetailMetricCodePL(MetricRentAssets))
	}
	if RetailMetricCodePL(MetricPayroll) != 24 {
		t.Fatalf("ФОТ — статья 24, got %d", RetailMetricCodePL(MetricPayroll))
	}
	// Продажи статьёй затрат не являются: код у них зависит от страны и канала и
	// приходит из маппинга публикации, а не из этой таблицы.
	if RetailMetricCodePL(MetricSales) != 0 {
		t.Fatalf("для продаж код статьи здесь не задаётся, got %d", RetailMetricCodePL(MetricSales))
	}
}

// Неизвестная метрика отклоняется: строка в tp_value с чужой метрикой не
// находится ни отчётом, ни массовой операцией.
func TestRetailMetricKnown(t *testing.T) {
	// Ответ 35 (10.09.2026): ФОТ и аренда переехали на этап 1.5 и убраны из формы
	// продаж полностью, а не сделаны read-only. В форме осталась одна метрика.
	for _, m := range []string{"", MetricSales} {
		if !RetailMetricKnown(m) {
			t.Errorf("метрика %q должна быть известна форме", m)
		}
	}
	for _, m := range []string{"rent2", "SALES", "прочее"} {
		if RetailMetricKnown(m) {
			t.Errorf("метрика %q не должна приниматься", m)
		}
	}
	// Переехавшие метрики отклоняются с ОСМЫСЛЕННЫМ поводом: «неизвестная
	// метрика» про переезд ничего не говорит.
	for _, m := range []string{MetricPayroll, MetricRent, MetricRentAssets} {
		if RetailMetricKnown(m) {
			t.Errorf("метрика %q убрана из формы продаж (ответ 35)", m)
		}
		if !RetailMetricMoved(m) {
			t.Errorf("метрика %q должна опознаваться как переехавшая на этап 1.5", m)
		}
	}
}

// ТЗ доработок п. 3.1: интернет-магазин (40) и техзапись 722 не попадают в
// строки формы, даже если источник отдаёт их в группе магазинов.
func TestRetailFormStoresExcludesOnlineShop(t *testing.T) {
	stores := []RetailStore{
		{CodeCFO: 101, GroupCFO1: "Магазины"},
		{CodeCFO: 40, GroupCFO1: "Магазины", NameCFO: "Интернет-магазин"},
		{CodeCFO: 722, GroupCFO1: "Магазины"},
	}
	got := retailFormStores(stores, NewRetailScope(RetailScopeSeed()))
	if len(got) != 1 || got[0].CodeCFO != 101 {
		t.Fatalf("в форме должны остаться только магазины: %+v", got)
	}
	// Разошлись названия групп — форма не пустеет, исключения действуют.
	odd := []RetailStore{{CodeCFO: 101, GroupCFO1: "Shops"}, {CodeCFO: 40, GroupCFO1: "Shops"}}
	got = retailFormStores(odd, NewRetailScope(RetailScopeSeed()))
	if len(got) != 1 || got[0].CodeCFO != 101 {
		t.Fatalf("фолбэк должен оставить магазины без исключённых ЦФО: %+v", got)
	}
}
