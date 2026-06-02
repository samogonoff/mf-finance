package etl

import (
	"database/sql"
	"strings"
	"testing"

	"github.com/company/finance-api/internal/reports/debt"
)

// vgoFilter строит ВГО-условие: ICO=1 ИЛИ контрагент ∈ наши ЮЛ.
func TestVGOFilter_clauseShape(t *testing.T) {
	clause, args := vgoFilter()

	if !strings.Contains(clause, "p.ICO = 1 OR") {
		t.Errorf("clause не содержит ICO-ветку: %q", clause)
	}
	if !strings.Contains(clause, "LTRIM(RTRIM(p.CounterpartyID)) IN (") {
		t.Errorf("clause не содержит TRIM'нутый IN-список контрагентов: %q", clause)
	}
	if !strings.HasPrefix(clause, " AND (") {
		t.Errorf("clause должен начинаться с ' AND (' для вставки после WHERE: %q", clause)
	}
	if len(args) != len(ourINNs) {
		t.Errorf("args = %d, want %d (по параметру на ИНН)", len(args), len(ourINNs))
	}
}

// ИНН подставляются только параметрами — ни одного значения в тексте SQL (анти-инъекция).
func TestVGOFilter_noRawINNInSQL(t *testing.T) {
	clause, args := vgoFilter()
	for _, inn := range ourINNs {
		if strings.Contains(clause, inn) {
			t.Errorf("clause содержит сырой ИНН %q — должно быть только @имя-параметра", inn)
		}
	}
	// Каждый аргумент — sql.NamedArg, и его @имя присутствует в clause.
	for _, a := range args {
		na, ok := a.(sql.NamedArg)
		if !ok {
			t.Fatalf("arg %#v не sql.NamedArg", a)
		}
		if !strings.Contains(clause, "@"+na.Name) {
			t.Errorf("clause не содержит плейсхолдер @%s", na.Name)
		}
	}
}

// Дубликат ourINNs в etl не должен разъезжаться с источником истины debt.OurINNs().
func TestVGOFilter_listMatchesDebtSeed(t *testing.T) {
	want := map[string]bool{}
	for _, inn := range debt.OurINNs() {
		want[inn] = true
	}
	got := map[string]bool{}
	for _, inn := range ourINNs {
		got[inn] = true
	}
	if len(got) != len(want) {
		t.Fatalf("ourINNs = %d ЮЛ, debt.OurINNs() = %d — списки разъехались", len(got), len(want))
	}
	for inn := range want {
		if !got[inn] {
			t.Errorf("ourINNs не содержит %q из debt.OurINNs()", inn)
		}
	}
}

// ВГО-контрагентом может быть ЮЛ любой страны, не только импортируемых РФ/РБ.
func TestVGOFilter_coversNonLevel1Entity(t *testing.T) {
	const mfKazakhstan = "141240004842" // КЗ — не в CountryByINN, но ВГО-контрагент
	found := false
	for _, inn := range ourINNs {
		if inn == mfKazakhstan {
			found = true
		}
	}
	if !found {
		t.Errorf("ourINNs не содержит КЗ-ЮЛ %q — ВГО с ним не распознается", mfKazakhstan)
	}
}
