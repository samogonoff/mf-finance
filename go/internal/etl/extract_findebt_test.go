package etl

import (
	"strings"
	"testing"
)

// extract должен читать FinDebt3, пиннить ВГО-контур, тащить все линзы CUR_FILTER
// как измерение (без пина) и агрегировать до бизнес-ключа договора × линза × валюта
// (совпадает с ключом ReplacingMergeTree fact_findebt_ccy).
func TestExtractFinDebtSQL(t *testing.T) {
	q := extractFinDebtSQL(FinDebtTables{Fin3FQN: "[Payments].[report].[FinDebt3]"})
	for _, want := range []string{
		"[Payments].[report].[FinDebt3]",
		"d.Folder = @grp",
		"d.[Date] >= @min",
		"SUM(ISNULL(d.SUM_D, 0))",
		"SUM(ISNULL(d.SUM_K, 0))",
		"ISNULL(d.Doc_Number, '')",
		"d.Delay",
		"d.Payment_Date",
		"d.DAY_DELAY",
		// Линза и валюта — измерения, тянутся в SELECT и в ключ (GROUP BY).
		"LTRIM(RTRIM(d.CUR_FILTER))",
		"LTRIM(RTRIM(ISNULL(d.Currency, '')))",
		"AS cur_filter",
		"AS currency",
		"AS sum_d",
		"AS sum_k",
		"GROUP BY",
	} {
		if !strings.Contains(q, want) {
			t.Errorf("extractFinDebtSQL не содержит %q", want)
		}
	}
	// CUR_FILTER больше НЕ пиннится (иначе теряем линзы) — параметра @cur нет.
	if strings.Contains(q, "@cur") {
		t.Errorf("extractFinDebtSQL не должен пиннить CUR_FILTER (@cur):\n%s", q)
	}
	// Срок/просрочку/описание берём MAX (не в ключ) — иначе ReplacingMergeTree
	// выкинет строки с одинаковым номером+датой (потеря суммы).
	if !strings.Contains(q, "MAX(CONVERT(INT, ISNULL(d.Delay, 0)))") {
		t.Errorf("delay должен агрегироваться MAX:\n%s", q)
	}
}

func TestFinDebtConstants(t *testing.T) {
	if finDebtVGOFolder != "ГРУППА КОМПАНИЙ" {
		t.Errorf("VGO folder = %q", finDebtVGOFolder)
	}
}
