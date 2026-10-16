package config

import (
	"os"
	"testing"
)

// FinDebt — единственный источник отчёта «Задолженность ВГО». Дефолты env.
func TestLoad_FinDebtDefaults(t *testing.T) {
	for _, k := range []string{"DEBT_BACKEND", "MSSQL_FINDEBT_SCHEMA", "MSSQL_FINDEBT1_TABLE", "MSSQL_FINDEBT3_TABLE", "MSSQL_PAYMENTS_DB"} {
		t.Setenv(k, "") // гарантируем «не задано» → дефолт
		os.Unsetenv(k)
	}
	cfg := Load()
	if cfg.DebtBackend != "findebt" {
		t.Errorf("DebtBackend default = %q, want findebt", cfg.DebtBackend)
	}
	if cfg.DebtFinDebtSchema != "report" {
		t.Errorf("DebtFinDebtSchema default = %q, want report", cfg.DebtFinDebtSchema)
	}
	if cfg.DebtFinDebt1Table != "FinDebt1" || cfg.DebtFinDebt3Table != "FinDebt3" {
		t.Errorf("FinDebt tables = %q/%q, want FinDebt1/FinDebt3", cfg.DebtFinDebt1Table, cfg.DebtFinDebt3Table)
	}
	if cfg.PremasterPaymentsDatabase != "Payments" {
		t.Errorf("PremasterPaymentsDatabase default = %q, want Payments", cfg.PremasterPaymentsDatabase)
	}
}

// Явно заданные значения переопределяют дефолты.
func TestLoad_FinDebtOverride(t *testing.T) {
	t.Setenv("DEBT_BACKEND", "findebt-live")
	t.Setenv("MSSQL_FINDEBT1_TABLE", "FinDebt1_test")
	cfg := Load()
	if cfg.DebtBackend != "findebt-live" {
		t.Errorf("DebtBackend = %q", cfg.DebtBackend)
	}
	if cfg.DebtFinDebt1Table != "FinDebt1_test" {
		t.Errorf("DebtFinDebt1Table = %q", cfg.DebtFinDebt1Table)
	}
}

// История плана розницы (Checks.dbo.plan_saler_st*) лежит на SRV-SQL, а не на
// OLAP: без префикса linked server запрос с OLAP падает и KLIENT_ID пуст.
func TestLoad_RetailPlanHistoryViaLinkedServer(t *testing.T) {
	for _, k := range []string{"PLANS_RETAIL_PLAN_HISTORY_TABLE", "PLANS_RETAIL_PLAN_HISTORY_NEW_TABLE"} {
		t.Setenv(k, "")
		os.Unsetenv(k)
	}
	cfg := Load()
	if cfg.PlansRetailPlanHistTable != "[SRV-SQL].Checks.dbo.plan_saler_st" {
		t.Errorf("PlansRetailPlanHistTable default = %q", cfg.PlansRetailPlanHistTable)
	}
	if cfg.PlansRetailPlanHistNew != "[SRV-SQL].Checks.dbo.plan_saler_st_new_stores" {
		t.Errorf("PlansRetailPlanHistNew default = %q", cfg.PlansRetailPlanHistNew)
	}
}

func TestLoad_RetailCountryGate(t *testing.T) {
	for _, tc := range []struct {
		value string
		want  bool
	}{{"", true}, {"1", true}, {"0", false}} {
		t.Run(tc.value, func(t *testing.T) {
			t.Setenv("PLANS_RETAIL_COUNTRY_GATE", tc.value)
			if got := Load().PlansRetailCountryGate; got != tc.want {
				t.Fatalf("gate=%v want=%v", got, tc.want)
			}
		})
	}
}
