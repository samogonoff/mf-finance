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
func NewFinPLRepo(db *sql.DB, database, schema, table, minMonth string) (PremasterRepo, error) {
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

// Drilldown — месячная PL-детализация выручки из Table_Fin_PL «согласно PL»
// (SPEC §5.1): строки месяца за период по компании (и контрагенту, если ИНН
// резолвится в имя витрины). Возвращает DocumentRow как PL-строку месяца
// (DocDate=Month, DocKind=GroupPL, Description=OperationDescription, Amount=USD).
// Дневной premaster-drilldown здесь НЕ используется — это путь для ДЗ/КЗ-строк.
func (r *finPLRepo) Drilldown(ctx context.Context, q DrilldownQuery) ([]DocumentRow, error) {
	code, ok := codeByINN(q.CompanyINN)
	if !ok {
		return nil, fmt.Errorf("debt.finpl.Drilldown: unknown company_inn %q", q.CompanyINN)
	}
	from := finPLMonthFrom(q.DateFrom, r.minMonth)
	to := finPLMonthTo(q.DateTo)
	args := []any{
		sql.Named("code", code),
		sql.Named("from", from),
		sql.Named("to", to),
	}
	// Контрагент опционален: если partner_inn резолвится в имя витрины — фильтруем,
	// иначе отдаём PL-детализацию на уровне компании.
	partnerClause := ""
	if name, ok := vgoNameByINN(q.PartnerINN); ok {
		partnerClause = " AND CounterpartyName = @partner"
		args = append(args, sql.Named("partner", name))
	}

	query := fmt.Sprintf(`
SELECT [Month], DocName1C, OperationDescription, GroupPL, Dr_Cr, SUM(%[1]s) AS amt
FROM %[2]s
WHERE [ВГО] = 1
  AND GroupPL = N'ПРОДАЖИ'
  AND [Компания] = @code
  AND [Month] BETWEEN @from AND @to%[3]s
GROUP BY [Month], DocName1C, OperationDescription, GroupPL, Dr_Cr
HAVING ABS(SUM(%[1]s)) > 0.005
ORDER BY [Month]`,
		finPLRevenueColumn, r.tableFQN, partnerClause)

	rows, err := r.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("debt.finpl.Drilldown: query: %w", err)
	}
	defer rows.Close()

	out := make([]DocumentRow, 0, 64)
	for rows.Next() {
		var month time.Time
		var docName, opDesc, groupPL, drCr sql.NullString
		var amt float64
		if err := rows.Scan(&month, &docName, &opDesc, &groupPL, &drCr, &amt); err != nil {
			return nil, fmt.Errorf("debt.finpl.Drilldown: scan: %w", err)
		}
		out = append(out, DocumentRow{
			DocDate:     month,
			DocNumber:   nz(docName),
			DocKind:     nz(groupPL),
			TransGroup:  nz(drCr),
			Amount:      amt,
			Description: nz(opDesc),
		})
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("debt.finpl.Drilldown: rows: %w", err)
	}
	return out, nil
}

// nz — строка из NullString или "".
func nz(s sql.NullString) string {
	if s.Valid {
		return strings.TrimSpace(s.String)
	}
	return ""
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
			// Резолвим ИНН контрагента по имени — чтобы revenue-строка
			// группировалась с ДЗ/КЗ той же пары и была адресуема в drilldown.
			if inn, ok := vgoCounterpartyINNByName(row.Partner); ok {
				row.PartnerINN = inn
			}
		}
		row.RevenuePeriod = rr.RevenuePeriod
		row.RevenueLastMonth = rr.RevenueLastMonth
		out = append(out, row)
	}
	return out
}

// finplComposite — источник DEBT_BACKEND=finpl целиком: Report сливает выручку из
// Table_Fin_PL (rev) с ДЗ/КЗ-сальдо/договором/просрочкой из Premaster (debt).
// Drilldown идёт в Premaster (mssql) — до T8. См. SPEC §5, docs/reports/debt/finpl-merge.md.
//
// Closing-сальдо ДЗ/КЗ берётся premaster'ом на конец периода (f.DateTo) — как в
// текущем mssql-отчёте; finpl даёт месячную выручку за тот же период.
type finplComposite struct {
	rev  PremasterRepo // finpl (Table_Fin_PL) — выручка
	debt PremasterRepo // premaster (Premaster1C) — ДЗ/КЗ, договор, просрочка, drilldown
}

// NewFinPLComposite собирает полный finpl-источник. debt может быть nil (тогда
// отчёт будет только выручочный, без ДЗ/КЗ; drilldown вернёт ошибку).
func NewFinPLComposite(rev, debt PremasterRepo) PremasterRepo {
	return &finplComposite{rev: rev, debt: debt}
}

func (c *finplComposite) Report(ctx context.Context, f Filters) ([]DebtRow, error) {
	revRows, err := c.rev.Report(ctx, f)
	if err != nil {
		return nil, err
	}
	var debtRows []DebtRow
	if c.debt != nil {
		debtRows, err = c.debt.Report(ctx, f)
		if err != nil {
			return nil, err
		}
	}
	return mergeFinPLPremaster(revRows, debtRows), nil
}

// Drilldown маршрутизирует по наличию счёта: revenue-строка finpl приходит без
// счёта (Account=="") → месячная PL-детализация из Table_Fin_PL; ДЗ/КЗ-строка
// (Account задан) → premaster (документная детализация по договору).
func (c *finplComposite) Drilldown(ctx context.Context, q DrilldownQuery) ([]DocumentRow, error) {
	if strings.TrimSpace(q.Account) == "" {
		return c.rev.Drilldown(ctx, q)
	}
	if c.debt == nil {
		return nil, errors.New("debt.finpl.Drilldown: no premaster backend configured")
	}
	return c.debt.Drilldown(ctx, q)
}

// mergeFinPLPremaster сливает выручку finpl и ДЗ/КЗ premaster без двойного счёта:
//   - у premaster-строк обнуляем выручку (её канонический источник теперь finpl) и
//     оставляем только те, где есть ДЗ/КЗ-сальдо (чисто-выручочные строки выкидываем);
//   - добавляем revenue-строки finpl как есть.
// Строки остаются плоскими — UI группирует по (Компания→Контрагент→Счёт→Договор→Валюта).
func mergeFinPLPremaster(rev, debt []DebtRow) []DebtRow {
	out := make([]DebtRow, 0, len(rev)+len(debt))
	for _, r := range debt {
		r.RevenuePeriod = 0
		r.RevenueLastMonth = 0
		if hasBalance(r) {
			out = append(out, r)
		}
	}
	out = append(out, rev...)
	return out
}

// hasBalance — есть ли в строке ненулевое ДЗ/КЗ-сальдо (вход/оборот/исход).
func hasBalance(r DebtRow) bool {
	return r.OpeningDZ != 0 || r.OpeningKZ != 0 ||
		r.TurnoverDZ != 0 || r.TurnoverKZ != 0 ||
		r.ClosingDZ != 0 || r.ClosingKZ != 0
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
