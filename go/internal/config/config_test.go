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
