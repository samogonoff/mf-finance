package debt

import (
	"database/sql"
	"strings"
	"testing"
)

const ptirINN = "9731039708"

// vgoCHClause — безусловный ВГО-фильтр для ClickHouse-отчёта: ico=1 ИЛИ
// контрагент ∈ наши ЮЛ. Значения инлайнятся (как EntityINNs в repo_clickhouse).
func TestVGOCHClause(t *testing.T) {
	c := vgoCHClause()
	if !strings.Contains(c, "ico = 1 OR counterparty_id IN (") {
		t.Errorf("CH clause без union-ветки: %q", c)
	}
	if !strings.HasPrefix(c, " AND (") {
		t.Errorf("CH clause должен начинаться с ' AND (': %q", c)
	}
	// ПТИР должен присутствовать инлайном (в кавычках) — иначе его ico=0 строки отрежутся.
	if !strings.Contains(c, "'"+ptirINN+"'") {
		t.Errorf("CH clause не содержит ПТИР %q: %q", ptirINN, c)
	}
}

// vgoMSSQLClause — тот же фильтр для прямого premaster-отчёта, но параметризованный.
func TestVGOMSSQLClause(t *testing.T) {
	c, args := vgoMSSQLClause()
	if !strings.Contains(c, "ICO = 1 OR LTRIM(RTRIM(CounterpartyID)) IN (") {
		t.Errorf("MSSQL clause без union-ветки: %q", c)
	}
	if len(args) != len(OurINNs()) {
		t.Fatalf("args = %d, want %d", len(args), len(OurINNs()))
	}
	for _, inn := range OurINNs() {
		if strings.Contains(c, inn) {
			t.Errorf("MSSQL clause содержит сырой ИНН %q — должны быть только параметры", inn)
		}
	}
	for _, a := range args {
		na, ok := a.(sql.NamedArg)
		if !ok {
			t.Fatalf("arg %#v не sql.NamedArg", a)
		}
		if !strings.Contains(c, "@"+na.Name) {
			t.Errorf("clause не содержит @%s", na.Name)
		}
	}
}
