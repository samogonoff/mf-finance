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
		"d.[Date] >= CONVERT(date, @min, 112)",
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

// Корзины просрочки FinDebt3 тянутся в extract тем же GROUP BY (суммой).
func TestExtractFinDebtSQL_AgingBuckets(t *testing.T) {
	q := extractFinDebtSQL(FinDebtTables{Fin3FQN: "[Payments].[report].[FinDebt3]"})
	for _, col := range []string{"SUM30_D", "SUM60_D", "SUM90_D", "SUM999_D", "SUM30_K", "SUM60_K", "SUM90_K", "SUM999_K"} {
		if !strings.Contains(q, "SUM(ISNULL(d."+col+", 0))") {
			t.Errorf("extract не суммирует %s", col)
		}
	}
	for _, alias := range []string{"AS sum30_d", "AS sum999_d", "AS sum30_k", "AS sum999_k"} {
		if !strings.Contains(q, alias) {
			t.Errorf("нет алиаса %q", alias)
		}
	}
}

// Оборотный extract: только ВГО-канал и служебный DEBT_FILTER, свёртка каналов в
// зерно (снэпшот × ЮЛ × контрагент × счёт × линза × валюта), без пина линзы.
func TestExtractFinDebtTurnoverSQL(t *testing.T) {
	q := extractFinDebtTurnoverSQL(FinDebtTables{Fin1FQN: "[Payments].[report].[FinDebt1]"})
	for _, want := range []string{
		"[Payments].[report].[FinDebt1]",
		"f.Channel = @grp",
		"f.DEBT_FILTER = N'Отображать платежи и отгр.'",
		"f.[Date] >= CONVERT(date, @min, 112)",
		"SUM(ISNULL(f.D_Wholesales, 0))", "SUM(ISNULL(f.K_Wholesales, 0))",
		"SUM(ISNULL(f.D_Payment, 0))", "SUM(ISNULL(f.K_Payment, 0))",
		"LTRIM(RTRIM(f.CUR_FILTER))", "AS cur_filter", "AS acc_root", "AS date_from",
		"GROUP BY",
	} {
		if !strings.Contains(q, want) {
			t.Errorf("extractFinDebtTurnoverSQL не содержит %q", want)
		}
	}
	if strings.Contains(q, "@cur") {
		t.Errorf("линза не должна пиниться:\n%s", q)
	}
	// Channel/Employe не в ключе — каналы сворачиваются суммой.
	if strings.Contains(q[strings.Index(q, "GROUP BY"):], "Channel") {
		t.Errorf("Channel не должен быть в GROUP BY")
	}
}
