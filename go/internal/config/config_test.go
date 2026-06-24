package config

import (
	"os"
	"testing"
)

// finpl-источник отчёта «Задолженность ВГО» — новые env с дефолтами.
// Дефолт DEBT_BACKEND остаётся mssql (premaster); finpl — явный opt-in до сверки.
func TestLoad_FinPLDefaults(t *testing.T) {
	for _, k := range []string{"MSSQL_FINPL_TABLE", "DEBT_FINPL_MIN_MONTH", "DEBT_BACKEND"} {
		t.Setenv(k, "") // гарантируем «не задано» → дефолт
		os.Unsetenv(k)
	}
	cfg := Load()
	if cfg.DebtFinPLTable != "Table_Fin_PL" {
		t.Errorf("DebtFinPLTable default = %q, want Table_Fin_PL", cfg.DebtFinPLTable)
	}
	if cfg.DebtFinPLMinMonth != "2025-01-01" {
		t.Errorf("DebtFinPLMinMonth default = %q, want 2025-01-01", cfg.DebtFinPLMinMonth)
	}
	if cfg.DebtBackend != "mssql" {
		t.Errorf("DebtBackend default = %q, want mssql (finpl — opt-in до сверки)", cfg.DebtBackend)
	}
}

// Явно заданные значения переопределяют дефолты.
func TestLoad_FinPLOverride(t *testing.T) {
	t.Setenv("MSSQL_FINPL_TABLE", "Table_Fin_PL_test")
	t.Setenv("DEBT_FINPL_MIN_MONTH", "2024-06-01")
	t.Setenv("DEBT_BACKEND", "finpl")
	cfg := Load()
	if cfg.DebtFinPLTable != "Table_Fin_PL_test" {
		t.Errorf("DebtFinPLTable = %q", cfg.DebtFinPLTable)
	}
	if cfg.DebtFinPLMinMonth != "2024-06-01" {
		t.Errorf("DebtFinPLMinMonth = %q", cfg.DebtFinPLMinMonth)
	}
	if cfg.DebtBackend != "finpl" {
		t.Errorf("DebtBackend = %q", cfg.DebtBackend)
	}
}
