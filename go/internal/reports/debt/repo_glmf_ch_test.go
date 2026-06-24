package debt

import (
	"strings"
	"testing"
)

// T2/T3: SQL-билдеры glmfCHRepo. Числовая корректность — на CHECKPOINT B (нужен CH).
func TestGLMFSaldoQuery(t *testing.T) {
	q := glmfSaldoQuery([]string{"6950135110"}, "2026-01-01", "2026-01-31")
	for _, frag := range []string{
		"finance.fact_glmf", "amt_withvat_byn", "dr_acc", "cr_acc",
		"sumIf", "company_id IN ('6950135110')",
		"dr_acc_root IN", "cr_acc_root IN", // фильтр только ДЗ/КЗ-счетов
		"finance.dim_contract", "contract_name", // договор = уровень группировки (T5)
		"GROUP BY", "FORMAT JSONEachRow",
	} {
		if !strings.Contains(q, frag) {
			t.Errorf("glmfSaldoQuery не содержит %q", frag)
		}
	}
}

func TestGLMFDrilldownQuery(t *testing.T) {
	q := glmfDrilldownQuery("6950135110", "141240004842", "62", "ДП-1 от 01.01", "2026-01-31")
	for _, frag := range []string{"finance.fact_glmf", "finance.dim_contract",
		"company_id = '6950135110'", "counterparty_id = '141240004842'", "doc_id", "FORMAT JSONEachRow"} {
		if !strings.Contains(q, frag) {
			t.Errorf("glmfDrilldownQuery не содержит %q", frag)
		}
	}
}

func TestGLMFRevenueQuery(t *testing.T) {
	q := glmfRevenueQuery([]string{"6950135110"}, "2026-01-01", "2026-01-31", "2026-01-01")
	for _, frag := range []string{
		"finance.fact_glmf", "amt_wovat_byn",
		"country = 'РБ'", "62.1", "90.1.1", // выручка по корреспонденции
		"GROUP BY company_id, counterparty_id", "FORMAT JSONEachRow",
	} {
		if !strings.Contains(q, frag) {
			t.Errorf("glmfRevenueQuery не содержит %q", frag)
		}
	}
}

// ДЗ/КЗ-корни для фильтра берутся из chart (все страны, KindDZ+KindKZ).
func TestDebtAccountRoots(t *testing.T) {
	roots := debtAccountRoots()
	set := map[string]bool{}
	for _, r := range roots {
		set[r] = true
	}
	for _, want := range []string{"62", "60", "76", "1210", "3310", "6000"} {
		if !set[want] {
			t.Errorf("debtAccountRoots не содержит %q", want)
		}
	}
	// выручочные счета (90/6010/9010) — НЕ должны попадать в ДЗ/КЗ-корни
	for _, no := range []string{"90", "6010", "9010"} {
		if set[no] {
			t.Errorf("debtAccountRoots не должен содержать выручочный %q", no)
		}
	}
}
