package etl

import (
	"strings"
	"testing"
)

// extract должен читать FinDebt3, пиннить ВГО-контур + белрублёвую линзу и
// агрегировать до бизнес-ключа договора (совпадает с ключом ReplacingMergeTree).
func TestExtractFinDebtSQL(t *testing.T) {
	q := extractFinDebtSQL(FinDebtTables{Fin3FQN: "[Payments].[report].[FinDebt3]"})
	for _, want := range []string{
		"[Payments].[report].[FinDebt3]",
		"d.Folder = @grp",
		"d.CUR_FILTER = @cur",
		"d.[Date] >= @min",
		"SUM(ISNULL(d.SUM_D, 0))",
		"SUM(ISNULL(d.SUM_K, 0))",
		"ISNULL(d.Doc_Number, '')",
		"d.Delay",
		"d.Payment_Date",
		"d.DAY_DELAY",
		"GROUP BY",
	} {
		if !strings.Contains(q, want) {
			t.Errorf("extractFinDebtSQL не содержит %q", want)
		}
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
	if finDebtCurFilter != "В бел. рублях" {
		t.Errorf("cur filter = %q", finDebtCurFilter)
	}
}
