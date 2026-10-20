package plans

import (
	"context"
	"strings"
	"testing"
)

// Ключ области несёт вид формы: IM-BY — интернет-магазин, OPT-xx — опт по странам листа «ОПТ».
func TestExpKindOf_IMAndOpt(t *testing.T) {
	for _, c := range []struct{ scope, kind, country string }{
		{"IM-BY", ExpKindIM, "BY"},
		{"im-by", ExpKindIM, "BY"},
		{"OPT-BY", ExpKindOpt, "BY"},
		{"OPT-RU", ExpKindOpt, "RU"},
		{"OPT-UZ", ExpKindOpt, "UZ"},
	} {
		if k, cn := expKindOf(c.scope); k != c.kind || cn != c.country {
			t.Errorf("%s → %s/%s, ожидали %s/%s", c.scope, k, cn, c.kind, c.country)
		}
	}
}

// Отбор источника: ИМ — по группе ЦФО 1, опт — по GroupCFO3 страны листа, группа ЦФО у опта любая.
func TestExpKindFilter(t *testing.T) {
	for _, c := range []struct{ kind, country, g1, g3 string }{
		{ExpKindRetail, "BY", "3.Магазины", ""},
		{ExpKindMP, "RU", "4.Маркетплейсы", ""},
		{ExpKindIM, "BY", "5.Интернет-магазин", ""},
		{ExpKindOpt, "BY", "", "ОПТ РБ"},
		{ExpKindOpt, "RU", "", "ОПТ РФ"},
		{ExpKindOpt, "UZ", "", "ОПТ УЗ"},
		{ExpKindOpt, "KZ", "", "!"}, // на листе «ОПТ» Казахстана нет: строк быть не должно
	} {
		g1, g3 := expKindFilter(c.kind, c.country)
		if g1 != c.g1 || g3 != c.g3 {
			t.Errorf("%s/%s → %q/%q, ожидали %q/%q", c.kind, c.country, g1, g3, c.g1, c.g3)
		}
	}
}

func TestExpCostsSQL_IMAndOpt(t *testing.T) {
	im := expCostsSQL(ExpKindIM, "BY", []int{2025, 2026})
	if !strings.Contains(im, "group_cfo1 = '5.Интернет-магазин'") || strings.Contains(im, "group_cfo3") {
		t.Errorf("запрос ИМ:\n%s", im)
	}
	opt := expCostsSQL(ExpKindOpt, "RU", []int{2026})
	if !strings.Contains(opt, "group_cfo3 = 'ОПТ РФ'") || !strings.Contains(opt, "country = 'RU'") || strings.Contains(opt, "group_cfo1 =") {
		t.Errorf("запрос опта: группа ЦФО 1 не должна отбираться, GroupCFO3 — должна:\n%s", opt)
	}
	if kz := expCostsSQL(ExpKindOpt, "KZ", nil); !strings.Contains(kz, "group_cfo3 = '!'") {
		t.Errorf("страна без листа опта должна давать пустой отбор:\n%s", kz)
	}
}

// Листы «ОПТ» и «ИМ» берут те же коды PL в те же управленческие группы, что и «Розница».
func TestExpPnlDefaults_IMAndOpt(t *testing.T) {
	for _, kind := range []string{ExpKindIM, ExpKindOpt} {
		m := expApplyDefaultPnlGroups(kind, map[int]string{})
		if m[24] != "Заработная плата" || m[54] != "Аренда" {
			t.Errorf("%s: группы розницы не применены: 24→%q, 54→%q", kind, m[24], m[54])
		}
		if order := expPnlOrder(kind); len(order) == 0 || order[0] != "Заработная плата" {
			t.Errorf("%s: порядок групп не розничный: %v", kind, order)
		}
	}
	if m := expApplyDefaultPnlGroups(ExpKindMP, map[int]string{}); len(m) != 0 {
		t.Errorf("МП не должна получать розничные группы: %v", m)
	}
}

// Фикстуры стенда: у ИМ один ЦФО, у опта — ЦФО страны; строки идут под своей группой.
func TestExpMockSource_IMAndOpt(t *testing.T) {
	for _, c := range []struct {
		kind, country string
		cfo           int
	}{
		{ExpKindIM, "BY", 380},
		{ExpKindOpt, "BY", 35},
		{ExpKindOpt, "RU", 303},
		{ExpKindOpt, "UZ", 414},
	} {
		cells, err := (expMockSource{}).Costs(context.Background(), c.kind, c.country, []int{2025, 2026})
		if err != nil || len(cells) == 0 {
			t.Fatalf("%s/%s: %v, строк %d", c.kind, c.country, err, len(cells))
		}
		found := false
		for _, cell := range cells {
			if cell.Country != c.country {
				t.Fatalf("%s/%s: строка чужой страны %+v", c.kind, c.country, cell)
			}
			if cell.CodeCFO == c.cfo {
				found = true
			}
		}
		if !found {
			t.Errorf("%s/%s: нет ЦФО %d", c.kind, c.country, c.cfo)
		}
	}
}

// Справочник ЦФО опту не нужен (город/тип/РМ — атрибуты магазина): пустой ответ, а не ошибка.
func TestExpSourceCfo_OptSkipsDirectory(t *testing.T) {
	s := &expSource{tables: ExpTables{CfoTable: "FinDWH.dbo.[001 CodeCFO]"}}
	m, err := s.Cfo(context.Background(), ExpKindOpt, "BY")
	if err != nil || len(m) != 0 {
		t.Fatalf("справочник опта: %v, %d записей", err, len(m))
	}
}
