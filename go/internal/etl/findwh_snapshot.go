package etl

import (
	"context"
	"fmt"
	"strconv"
	"strings"
)

// Полный снимок для небольших таблиц (Table_Fin_PL ~3 млн строк, ExRates ~20 тыс.):
// заливаем во временную копию и атомарно подменяем EXCHANGE TABLES. Аналитик
// никогда не видит пустую или полузалитую таблицу; при ошибке остаётся вчерашняя.

// finDWHSnapshot — что и куда снимать.
type finDWHSnapshot struct {
	Target string    // finance.findwh_<таблица>
	Cols   []colSpec // колонки в порядке SELECT
	Query  string    // SELECT из MSSQL, отдаёт Cols в том же порядке
}

// tableFinPLCols — dbo.Table_Fin_PL → finance.findwh_table_fin_pl.
var tableFinPLCols = []colSpec{
	{"BlockNum", "block_num", kInt},
	{"Dr_Cr", "dr_cr", kStr},
	{"DrAcc", "dr_acc", kStr},
	{"CrAcc", "cr_acc", kStr},
	{"DocName1C", "doc_name_1c", kStr},
	{"CodePL", "code_pl", kStr},
	{"Country", "country", kStr},
	{"Month", "month", kDate},
	{"Компания", "company", kStr},
	{"Корр-ка", "counterparty", kStr},
	{"ВГО", "vgo", kInt},
	{"OperationDescription", "operation_description", kStr},
	{"CounterpartyName", "counterparty_name", kStr},
	{"GroupPL", "group_pl", kStr},
	{"GroupPL_new", "group_pl_new", kStr},
	{"AmountBYN", "amount_byn", kDec},
	{"Amount", "amount", kDec},
	{"AmountUSD", "amount_usd", kDec},
	{"AmountRUB", "amount_rub", kDec},
	{"Scenario", "scenario", kStr},
	{"CodeCFO", "code_cfo", kStr},
	{"CFO", "cfo", kStr},
	{"GroupCFO1", "group_cfo1", kStr},
	{"GroupCFO2", "group_cfo2", kStr},
	{"GroupCFO3", "group_cfo3", kStr},
	{"Блок", "block_name", kStr},
	{"Метод.нюансы", "method_nuances", kInt},
	{"КлючСвязи", "link_key", kStr},
	{"Для отчета_скрипт", "for_report_script", kStr},
	{"Для отчета", "for_report", kStr},
	{"Expense", "expense", kStr},
	{"CF_item", "cf_item", kStr},
	{"LFL_LISA", "lfl_lisa", kStr},
	{"discount_amount_vat_BYN", "discount_amount_vat_byn", kDec},
	{"markdown_amount_vat_BYN", "markdown_amount_vat_byn", kDec},
}

func tableFinPLSnapshot(opts FinDWHOpts) finDWHSnapshot {
	return finDWHSnapshot{
		Target: "finance.findwh_table_fin_pl",
		Cols:   tableFinPLCols,
		Query:  "SELECT " + selectList(tableFinPLCols) + " FROM " + opts.fqn("Table_Fin_PL"),
	}
}

// exRatesTables — страновые таблицы курса: суффикс → значение колонки country.
// dbo.ExRates (без суффикса) — country ”. ExRatesBY сейчас пуста, но
// spExRatesBY её наполняет — берём, чтобы не потерять, когда заработает.
var exRatesTables = []string{"", "BY", "RU", "KZ", "UZ", "TR"}

// exRatesCols — колонки после UNION ALL: первой идёт country (литерал).
var exRatesCols = []colSpec{
	{"country", "country", kStr},
	{"DATA", "data", kDate},
	{"KURS_BANK", "kurs_bank", kDec},
	{"VALUTA_ID", "valuta_id", kInt},
}

func exRatesSnapshot(opts FinDWHOpts) finDWHSnapshot {
	parts := make([]string, len(exRatesTables))
	for i, sfx := range exRatesTables {
		parts[i] = "SELECT N'" + sfx + "' AS [country], " + selectList(exRatesCols[1:]) +
			" FROM " + opts.fqn("ExRates"+sfx)
	}
	return finDWHSnapshot{
		Target: "finance.findwh_ex_rates",
		Cols:   exRatesCols,
		Query:  strings.Join(parts, "\nUNION ALL\n"),
	}
}

// runFinDWHSnapshot — снимок в <target>__new, сверка числа строк, EXCHANGE.
func runFinDWHSnapshot(ctx context.Context, deps Deps, ch *chClient, s finDWHSnapshot, batch int) (int64, error) {
	stage := s.Target + "__new"
	if err := ch.exec(ctx, "DROP TABLE IF EXISTS "+stage); err != nil {
		return 0, fmt.Errorf("drop stage: %w", err)
	}
	if err := ch.exec(ctx, "CREATE TABLE "+stage+" AS "+s.Target); err != nil {
		return 0, fmt.Errorf("create stage: %w", err)
	}
	defer func() { _ = ch.exec(context.WithoutCancel(ctx), "DROP TABLE IF EXISTS "+stage) }()

	rows, err := deps.MSSQL.QueryContext(ctx, s.Query)
	if err != nil {
		return 0, fmt.Errorf("mssql query: %w", err)
	}
	n, err := streamToCH(ctx, ch, rows, s.Cols, stage, batch)
	rows.Close()
	if err != nil {
		return n, err
	}

	got, err := ch.queryString(ctx, "SELECT count() FROM "+stage)
	if err != nil {
		return n, fmt.Errorf("count stage: %w", err)
	}
	if got != strconv.FormatInt(n, 10) {
		return n, fmt.Errorf("в %s %s строк, прочитано из MSSQL %d — подмену не делаем", stage, got, n)
	}
	if err := ch.exec(ctx, "EXCHANGE TABLES "+stage+" AND "+s.Target); err != nil {
		return n, fmt.Errorf("exchange: %w", err)
	}
	return n, nil
}
