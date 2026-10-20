package plans

import (
	"context"
	"strings"
	"testing"
)

// Ключ области карточки несёт вид формы: IM-BY — интернет-магазин, OPT — опт (одна форма на три страны).
func TestExpKindOf_IMAndOpt(t *testing.T) {
	for _, c := range []struct{ scope, kind, country string }{
		{"IM-BY", ExpKindIM, "BY"},
		{"im-by", ExpKindIM, "BY"},
		{"OPT", ExpKindOpt, ""},
		{"opt", ExpKindOpt, ""},
	} {
		if k, cn := expKindOf(c.scope); k != c.kind || cn != c.country {
			t.Errorf("%s → %s/%s, ожидали %s/%s", c.scope, k, cn, c.kind, c.country)
		}
	}
}

// Отбор источника: ИМ — по группе ЦФО 1, опт — по трём GroupCFO3 листа «ОПТ», группа ЦФО у опта любая.
func TestExpKindFilter(t *testing.T) {
	for _, c := range []struct {
		kind, g1 string
		g3       []string
	}{
		{ExpKindRetail, "3.Магазины", nil},
		{ExpKindMP, "4.Маркетплейсы", nil},
		{ExpKindIM, "5.Интернет-магазин", nil},
		{ExpKindOpt, "", []string{"ОПТ РБ", "ОПТ РФ", "ОПТ УЗ"}},
	} {
		g1, g3 := expKindFilter(c.kind)
		if g1 != c.g1 || strings.Join(g3, "|") != strings.Join(c.g3, "|") {
			t.Errorf("%s → %q/%v, ожидали %q/%v", c.kind, g1, g3, c.g1, c.g3)
		}
	}
	if !expKindInBYN(ExpKindOpt) || expKindInBYN(ExpKindRetail) || expKindInBYN(ExpKindIM) {
		t.Error("в BYN вводится только опт (несколько стран в одной форме)")
	}
}

func TestExpCostsSQL_IMAndOpt(t *testing.T) {
	im := expCostsSQL(ExpKindIM, "BY", []int{2025, 2026})
	if !strings.Contains(im, "group_cfo1 = '5.Интернет-магазин'") || strings.Contains(im, "group_cfo3") ||
		!strings.Contains(im, "toFloat64(amount) AS amount") {
		t.Errorf("запрос ИМ:\n%s", im)
	}
	opt := expCostsSQL(ExpKindOpt, "", []int{2026})
	for _, want := range []string{"group_cfo3 IN ('ОПТ РБ', 'ОПТ РФ', 'ОПТ УЗ')", "toFloat64(amount_byn) AS amount"} {
		if !strings.Contains(opt, want) {
			t.Errorf("в запросе опта нет %q:\n%s", want, opt)
		}
	}
	if strings.Contains(opt, "group_cfo1 =") || strings.Contains(opt, "country =") {
		t.Errorf("опт: группа ЦФО 1 и страна не отбираются:\n%s", opt)
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

// Фикстуры стенда: у ИМ один ЦФО, у опта — ЦФО всех трёх стран в одной форме.
func TestExpMockSource_IMAndOpt(t *testing.T) {
	im, err := (expMockSource{}).Costs(context.Background(), ExpKindIM, "BY", []int{2025, 2026})
	if err != nil || len(im) == 0 || im[0].CodeCFO != 380 {
		t.Fatalf("ИМ: %v, строк %d", err, len(im))
	}
	opt, err := (expMockSource{}).Costs(context.Background(), ExpKindOpt, "", []int{2025, 2026})
	if err != nil || len(opt) == 0 {
		t.Fatalf("опт: %v, строк %d", err, len(opt))
	}
	countryByCFO := map[int]string{}
	for _, c := range opt {
		countryByCFO[c.CodeCFO] = c.Country
	}
	for cfo, country := range map[int]string{35: "BY", 254: "BY", 303: "RU", 414: "UZ"} {
		if countryByCFO[cfo] != country {
			t.Errorf("опт: ЦФО %d должен быть в стране %s, получили %q", cfo, country, countryByCFO[cfo])
		}
	}
}

// Новые области (ИМ, опт) не заводятся в периодах, где расходы уже утверждены целиком: иначе пустые
// черновики откатили бы этап 1.5 из «завершён».
func TestFormClosedInPeriod(t *testing.T) {
	card := func(scope, status string) Card {
		return Card{FormCode: TemplateExp, ScopeKey: scope, Status: status}
	}
	if formClosedInPeriod(nil, TemplateExp) {
		t.Error("без карточек период не считается закрытым")
	}
	if !formClosedInPeriod([]Card{card("R-BY", CardApproved), card("MP-RU", CardPublished)}, TemplateExp) {
		t.Error("все карточки утверждены/опубликованы — период закрыт")
	}
	if formClosedInPeriod([]Card{card("R-BY", CardApproved), card("MP-RU", CardDraft)}, TemplateExp) {
		t.Error("есть черновик — период не закрыт")
	}
	// Скрытая флагом карточка отдела не мешает и не помогает.
	if !formClosedInPeriod([]Card{card("R-BY", CardApproved), card("CFO-31-BY", CardDraft)}, TemplateExp) {
		t.Error("скрытые карточки отделов не должны влиять")
	}
	// Карточки других форм не в счёт.
	if formClosedInPeriod([]Card{{FormCode: TemplateRetail, ScopeKey: "BY", Status: CardApproved}}, TemplateExp) {
		t.Error("карточки другой формы не должны считаться")
	}
	if !hasCardScope([]Card{card("OPT", CardDraft)}, TemplateExp, "OPT") || hasCardScope(nil, TemplateExp, "OPT") {
		t.Error("hasCardScope")
	}
	late := map[string]bool{}
	for _, f := range formRegistryAll() {
		for _, sc := range f.Scopes {
			if sc.Late {
				late[sc.Key] = true
			}
		}
	}
	if !late["IM-BY"] || !late["OPT"] || late["R-BY"] || late["MP-RU"] {
		t.Errorf("поздними должны быть ИМ и опт, а не прежние области: %v", late)
	}
}

// Справочник ЦФО опту не нужен (город/тип/РМ — атрибуты магазина): пустой ответ, а не ошибка.
func TestExpSourceCfo_OptSkipsDirectory(t *testing.T) {
	s := &expSource{tables: ExpTables{CfoTable: "FinDWH.dbo.[001 CodeCFO]"}}
	m, err := s.Cfo(context.Background(), ExpKindOpt, "")
	if err != nil || len(m) != 0 {
		t.Fatalf("справочник опта: %v, %d записей", err, len(m))
	}
}
