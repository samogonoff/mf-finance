package debt

import "testing"

// T8: матчер CounterpartyName → ИНН для ВГО-контрагентов. Table_Fin_PL хранит
// контрагента только именем; чтобы revenue-строки группировались с ДЗ/КЗ (по ИНН)
// и были адресуемы в drilldown — резолвим имя в ИНН по справочнику наблюдаемых
// ВГО-имён (сняты с OLAP vGLMFAddUSD, ICO=1).
func TestVGOCounterpartyINNByName(t *testing.T) {
	cases := map[string]string{
		"Формэль":                "690719790",
		"Марк Формэль":           "690591512",
		"Марк Формэль ТД ООО, РФ": "6950135110",
		"Дримдом, ООО":           "692221084",
		"ПТИР ООО":               "9731039708",
	}
	for name, inn := range cases {
		if got, ok := vgoCounterpartyINNByName(name); !ok || got != inn {
			t.Errorf("vgoCounterpartyINNByName(%q) = %q,%v; want %q", name, got, ok, inn)
		}
	}
	// Внешний/неизвестный контрагент (например Летникова без ИНН) — не матчится.
	if _, ok := vgoCounterpartyINNByName("Летникова Екатерина Геннадьевна (аренда квартиры)"); ok {
		t.Error("неизвестное имя не должно матчиться")
	}
	if _, ok := vgoCounterpartyINNByName(""); ok {
		t.Error("пустое имя не должно матчиться")
	}
}

// buildFinPLReport проставляет PartnerINN, когда имя контрагента распознано.
func TestBuildFinPLReport_setsPartnerINN(t *testing.T) {
	got := buildFinPLReport([]finplRevRow{
		{CompanyCode: "MF", Country: "BY", PartnerName: nstr("Формэль"), RevenuePeriod: 10},
	})
	if got[0].PartnerINN != "690719790" {
		t.Errorf("PartnerINN = %q, want 690719790", got[0].PartnerINN)
	}
}

// codeByINN — обратный резолв для drilldown (company_inn → код Компании витрины).
func TestCodeByINN(t *testing.T) {
	if c, ok := codeByINN("690591512"); !ok || c != "MF" {
		t.Errorf("codeByINN(690591512) = %q,%v; want MF", c, ok)
	}
	if c, ok := codeByINN("692221084"); !ok || c != "DR" {
		t.Errorf("codeByINN(DR-inn) = %q,%v; want DR", c, ok)
	}
	if _, ok := codeByINN("999"); ok {
		t.Error("неизвестный ИНН не должен резолвиться в код")
	}
}
