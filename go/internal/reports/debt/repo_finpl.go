package debt

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"time"

	_ "github.com/microsoft/go-mssqldb"
)

// finPLRepo — источник DEBT_BACKEND=finpl. Первоисточник — каноническая месячная
// ОПУ-витрина Table_Fin_PL (FinDWH.dbo, тот же OLAP, что Premaster1C; наполняется
// процедурой Update_Table_Fin_PL «по свежим алгоритмам»). См. SPEC.md.
//
// На этом срезе (T4) репо отдаёт ТОЛЬКО выручочную часть DebtRow (RevenuePeriod/
// RevenueLastMonth) из Table_Fin_PL; ДЗ/КЗ-сальдо, договор, срок/просрочка и
// PartnerINN/менеджер/канал дотянет Premaster в композиции (T7). Drilldown — T8.
//
// Решения (SPEC §3, probe [002 CodePL]):
//   - выручка = строки с GroupPL='ПРОДАЖИ' (16 кодов продаж по каналам);
//   - валюта = USD-консолидация (колонка AmountUSD), единая для всей ГК.
//     Чтобы перейти на функциональную валюту юрлица — поменять finPLRevenueColumn
//     на "Amount" (нац.) и Currency на CurrencyForCountry(ent.Country) в builder.
//   - ВГО — безусловный [ВГО]=1 (флаг канонично посчитан апстримом).
type finPLRepo struct {
	db       *sql.DB
	tableFQN string    // [FinDWH].[dbo].[Table_Fin_PL]
	minMonth time.Time // нижняя граница периода (DEBT_FINPL_MIN_MONTH)
}

// finPLRevenueColumn / finPLRevenueCurrency — какая из 4 amount-колонок витрины
// и под какой валютой идёт в выручку. Вынесено в одно место для лёгкого переключения
// (см. комментарий к finPLRepo). По умолчанию USD-консолидация.
const finPLRevenueColumn = "AmountUSD"

func finPLRevenueCurrency() string { return "USD" }

// NewFinPLRepo строит репо поверх уже открытого MSSQL-пула к OLAP (тот же *sql.DB,
// что и premaster — БД одна). Валидирует идентификаторы (анти-инъекция через env).
// db==nil → (nil, nil): сигнал, что live-источник не настроен (как у premaster).
func NewFinPLRepo(db *sql.DB, database, schema, table, minMonth string) (*finPLRepo, error) {
	if db == nil {
		return nil, nil
	}
	for k, v := range map[string]string{"database": database, "schema": schema, "table": table} {
		if !identRe.MatchString(v) {
			return nil, fmt.Errorf("debt.finpl: invalid %s %q (must match %s)", k, v, identRe.String())
		}
	}
	return &finPLRepo{
		db:       db,
		tableFQN: "[" + database + "].[" + schema + "].[" + table + "]",
		minMonth: parseFinPLMinMonth(minMonth),
	}, nil
}

// finplRevRow — сырая строка свёртки выручки из Table_Fin_PL.
// RevenuePeriod — сумма за весь период; RevenueLastMonth — сумма за последний
// календарный месяц периода (отдельный показатель ТЗ).
type finplRevRow struct {
	CompanyCode      string         // [Компания] — короткий код юрлица
	Country          string         // витринная страна 'BY'/'RU'/'KZ'/'UZ'
	PartnerName      sql.NullString // CounterpartyName
	RevenuePeriod    float64
	RevenueLastMonth float64
}

// Report — свёртка выручки ВГО по (Компания, страна, контрагент) за месяцы периода.
func (r *finPLRepo) Report(ctx context.Context, f Filters) ([]DebtRow, error) {
	if f.DateTo.IsZero() {
		return nil, errors.New("debt.finpl.Report: date_to required")
	}
	from := finPLMonthFrom(f.DateFrom, r.minMonth)
	to := finPLMonthTo(f.DateTo)
	lastMonth := monthStart(f.DateTo)

	args := []any{
		sql.Named("from", from),
		sql.Named("to", to),
		sql.Named("last", lastMonth),
	}

	// Фильтр юрлиц: выбор приходит как ИНН, витрина хранит коды → транслируем.
	var companyClause string
	if codes := innsToFinPLCodes(f.EntityINNs); len(codes) > 0 {
		ph := make([]string, len(codes))
		for i, c := range codes {
			name := fmt.Sprintf("co%d", i)
			ph[i] = "@" + name
			args = append(args, sql.Named(name, c))
		}
		companyClause = " AND [Компания] IN (" + strings.Join(ph, ",") + ")"
	}

	q := fmt.Sprintf(`
SELECT [Компания]                                                         AS company_code,
       Country,
       CounterpartyName,
       SUM(%[1]s)                                                         AS rev_period,
       SUM(CASE WHEN [Month] >= @last THEN %[1]s ELSE 0 END)             AS rev_last
FROM %[2]s
WHERE [ВГО] = 1
  AND GroupPL = N'ПРОДАЖИ'
  AND [Month] BETWEEN @from AND @to%[3]s
GROUP BY [Компания], Country, CounterpartyName
HAVING ABS(SUM(%[1]s)) > 0.005`,
		finPLRevenueColumn, r.tableFQN, companyClause)

	rows, err := r.db.QueryContext(ctx, q, args...)
	if err != nil {
		return nil, fmt.Errorf("debt.finpl.Report: query: %w", err)
	}
	defer rows.Close()

	raw := make([]finplRevRow, 0, 256)
	for rows.Next() {
		var rr finplRevRow
		if err := rows.Scan(&rr.CompanyCode, &rr.Country, &rr.PartnerName, &rr.RevenuePeriod, &rr.RevenueLastMonth); err != nil {
			return nil, fmt.Errorf("debt.finpl.Report: scan: %w", err)
		}
		raw = append(raw, rr)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("debt.finpl.Report: rows: %w", err)
	}
	return buildFinPLReport(raw), nil
}

// Drilldown — полноценная месячная PL-детализация будет в T8. Пока не реализована
// (в композиции с Premaster drilldown идёт через mssql-репо — см. wiring T6).
func (r *finPLRepo) Drilldown(ctx context.Context, q DrilldownQuery) ([]DocumentRow, error) {
	return nil, errors.New("debt.finpl.Drilldown: not implemented (use composite/mssql backend)")
}

// buildFinPLReport раскладывает сырую свёртку выручки в DebtRow: резолвит код
// компании в ИНН/имя/страну (EntityByCode), проставляет валюту и выручку. ДЗ/КЗ и
// прочее остаются нулевыми/пустыми — их дополняет Premaster (T7).
func buildFinPLReport(raw []finplRevRow) []DebtRow {
	out := make([]DebtRow, 0, len(raw))
	for _, rr := range raw {
		row := DebtRow{Currency: finPLRevenueCurrency()}
		if ent, ok := EntityByCode(rr.CompanyCode); ok {
			row.Company = ent.Name
			row.CompanyINN = ent.INN
			row.Country = ent.Country
		} else {
			// Незнакомый код не теряем: показываем код, страну берём из витрины.
			row.Company = rr.CompanyCode
			row.Country = countryFromFinPL(rr.Country)
		}
		if rr.PartnerName.Valid {
			row.Partner = strings.TrimSpace(rr.PartnerName.String)
		}
		row.RevenuePeriod = rr.RevenuePeriod
		row.RevenueLastMonth = rr.RevenueLastMonth
		out = append(out, row)
	}
	return out
}

// countryFromFinPL — витринный код страны ('BY'/'RU'/…) → доменный Country (РБ/РФ/…).
func countryFromFinPL(c string) Country {
	switch strings.ToUpper(strings.TrimSpace(c)) {
	case "BY":
		return CountryRB
	case "RU":
		return CountryRF
	case "KZ":
		return CountryKZ
	case "UZ":
		return CountryUZ
	default:
		return Country(strings.TrimSpace(c))
	}
}

// innsToFinPLCodes — выбор юрлиц (ИНН) → коды Table_Fin_PL.Компания. Неизвестные
// ИНН (без кода в справочнике) отбрасываются. Дубли схлопываются.
func innsToFinPLCodes(inns []string) []string {
	byINN := make(map[string]string, len(codeIndex))
	for code, e := range codeIndex {
		byINN[e.INN] = code
	}
	out := make([]string, 0, len(inns))
	seen := make(map[string]bool, len(inns))
	for _, inn := range inns {
		if code, ok := byINN[strings.TrimSpace(inn)]; ok && !seen[code] {
			seen[code] = true
			out = append(out, code)
		}
	}
	return out
}
