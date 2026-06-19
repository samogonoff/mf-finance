package debt

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"regexp"
	"strings"
	"time"

	_ "github.com/microsoft/go-mssqldb"
)

// PremasterTables — куда репо ходит за данными. Имена идентификаторов
// подставляются в SQL напрямую (нельзя параметризовать), поэтому валидируются
// строгим regex.
type PremasterTables struct {
	Database          string // FinDWH
	Schema            string // dbo
	Main              string // Premaster1C — главная таблица проводок
	ObjectsTable      string // Objects — справочник DocID → имя документа (LEFT JOIN в Drilldown)
	CounterpartyTable string // Counterparty1C — имя/канал/менеджер контрагента (LEFT JOIN в Report); "" → не джойнить

	// Payments-витрина (опционально). Кросс-БД на ТОМ ЖЕ OLAP-сервере.
	// Docs несёт PaymentDate/Delay → просрочка в drill-down (мост
	// Premaster1C.DocID = Payments.dbo.Docs.ID). Если PaymentsDatabase пуст —
	// джойн отключён, payment_due_date/overdue_days не заполняются.
	PaymentsDatabase string // Payments
	DocsSchema       string // dbo
	DocsTable        string // Docs
}

var identRe = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_]*$`)

func (t PremasterTables) Validate() error {
	for k, v := range map[string]string{
		"database":       t.Database,
		"schema":         t.Schema,
		"main_table":     t.Main,
		"objects_table":  t.ObjectsTable,
	} {
		if !identRe.MatchString(v) {
			return fmt.Errorf("debt.PremasterTables: invalid %s %q (must match %s)", k, v, identRe.String())
		}
	}
	// Counterparty1C — опционально; валидируем, только если задан.
	if t.CounterpartyTable != "" && !identRe.MatchString(t.CounterpartyTable) {
		return fmt.Errorf("debt.PremasterTables: invalid counterparty_table %q (must match %s)", t.CounterpartyTable, identRe.String())
	}
	// Docs — опционально; валидируем только если витрина Payments сконфигурирована.
	if t.DocsConfigured() {
		for k, v := range map[string]string{
			"payments_database": t.PaymentsDatabase,
			"docs_schema":       t.DocsSchema,
			"docs_table":        t.DocsTable,
		} {
			if !identRe.MatchString(v) {
				return fmt.Errorf("debt.PremasterTables: invalid %s %q (must match %s)", k, v, identRe.String())
			}
		}
	}
	return nil
}

// DocsConfigured — включён ли кросс-БД джойн к Payments.Docs (по непустому имени БД).
func (t PremasterTables) DocsConfigured() bool { return t.PaymentsDatabase != "" }

func (t PremasterTables) MainFQN() string    { return "[" + t.Database + "].[" + t.Schema + "].[" + t.Main + "]" }
func (t PremasterTables) ObjectsFQN() string { return "[" + t.Database + "].[" + t.Schema + "].[" + t.ObjectsTable + "]" }
func (t PremasterTables) CounterpartyFQN() string {
	return "[" + t.Database + "].[" + t.Schema + "].[" + t.CounterpartyTable + "]"
}
func (t PremasterTables) DocsFQN() string {
	return "[" + t.PaymentsDatabase + "].[" + t.DocsSchema + "].[" + t.DocsTable + "]"
}

// premasterRepo — реализация PremasterRepo через MSSQL Premaster1C на OLAP-сервере.
// Логика и шаблон SQL — см. docs/reports/debt/schema-draft.md (§ 4.4, § 8a.2).
//
// Главный запрос (Report) делает один UNION ALL поверх индекса
// (CompanyID, Date) и считает «raw signed-сальдо» по корню счёта:
//
//	opening = Σ Dr − Σ Cr  где Date < @DateFrom
//	turnover = то же по Date BETWEEN @DateFrom AND @DateTo
//	closing = opening + turnover
//
// Дальше service.go разворачивает signed_sum → OpeningDZ/KZ/Revenue
// через chart_of_accounts (для дебиторских счетов: positive=ДЗ; для кредиторских —
// flip знака, чтобы наш долг показывался как положительная сумма КЗ).
type premasterRepo struct {
	db              *sql.DB
	mainFQN         string // [FinDWH].[dbo].[Premaster1C]
	objectsFQN      string // [FinDWH].[dbo].[Objects]
	counterpartyFQN string // [FinDWH].[dbo].[Counterparty1C]; "" → имя/канал/менеджер не обогащаем
	docsFQN         string // [Payments].[dbo].[Docs]; "" → просрочка не заполняется
}

// NewPremasterRepo открывает MSSQL-пул к OLAP-серверу (Premaster1C — таблица в FinDWH).
// При пустых учётках возвращает (nil, nil) — это сигнал main.go, что
// live-репо не настроено (UI должен оставаться в режиме DEBT_MOCK=1).
// port — числовая строка ("1433" дефолт); если в server уже есть ":port",
// можно передать port="" и драйвер сам распарсит host:port.
func NewPremasterRepo(server, port, database, user, password string) (*sql.DB, error) {
	if server == "" || user == "" || password == "" {
		return nil, nil
	}
	host := server
	if port != "" && !strings.Contains(server, ":") {
		host = server + ":" + port
	}
	dsn := fmt.Sprintf(
		"sqlserver://%s:%s@%s?database=%s&encrypt=true&trustservercertificate=true&app+name=finance-api",
		user, password, host, database,
	)
	db, err := sql.Open("sqlserver", dsn)
	if err != nil {
		return nil, fmt.Errorf("debt: open mssql: %w", err)
	}
	db.SetMaxOpenConns(4)
	db.SetMaxIdleConns(2)
	return db, nil
}

// WrapPremasterRepo — фабрика, упрощающая wiring в main.go.
// tables валидируется заранее — невалидный идентификатор тут же возвращает ошибку,
// чтобы не получить SQL-инъекцию через env (теоретически).
func WrapPremasterRepo(db *sql.DB, tables PremasterTables) (PremasterRepo, error) {
	if db == nil {
		return nil, nil
	}
	if err := tables.Validate(); err != nil {
		return nil, err
	}
	repo := &premasterRepo{
		db:         db,
		mainFQN:    tables.MainFQN(),
		objectsFQN: tables.ObjectsFQN(),
	}
	if tables.CounterpartyTable != "" {
		repo.counterpartyFQN = tables.CounterpartyFQN()
	}
	if tables.DocsConfigured() {
		repo.docsFQN = tables.DocsFQN()
	}
	return repo, nil
}

// rawRow — что отдаёт SQL: signed-сальдо по (CompanyID, CounterpartyID, account_root).
// Знак: для Dr-движения = +amt, для Cr-движения = −amt.
//
// LastMonthSigned — signed-сальдо только за последний календарный месяц периода
// [DateFrom; DateTo]. Используется для RevenueLastMonth (отдельный показатель в ТЗ).
// Для non-revenue счетов не используется и не должно влиять на DZ/KZ-показатели.
type rawRow struct {
	CompanyID       string
	CounterpartyID  sql.NullString // NULL → внутренние операции, отфильтруем
	AccountRoot     string
	OpeningSigned   float64
	TurnoverSigned  float64
	LastMonthSigned float64
	ClosingSigned   float64

	// Из Counterparty1C (LEFT JOIN после агрегации; NULL, если джойн отключён/не сматчился).
	PartnerName sql.NullString // CounterpartyName1C — имя контрагента
	Channel     sql.NullString // канал продаж
	Manager     sql.NullString // менеджер пары
}

// Report — основной запрос. Берёт сырые проводки Premaster1C по выбранным
// юрлицам, считает signed-сальдо по корню счёта, отдаёт сервису для разворота
// в DebtRow.
//
// Фильтры:
//   - f.EntityINNs — CompanyID IN (...). Обязателен (иначе full scan 200M).
//   - f.DateFrom/DateTo — границы периода для opening/turnover.
//   - f.Accounts — белый список корней счетов (если пуст — собираем из chart_of_accounts по странам выбранных юрлиц).
//
// Возврат — плоский []DebtRow (имена/страну/категорию счёта проставит service).
func (r *premasterRepo) Report(ctx context.Context, f Filters) ([]DebtRow, error) {
	if len(f.EntityINNs) == 0 {
		return nil, errors.New("debt.premaster.Report: entity_inns is required (no scan without company filter)")
	}
	if f.DateTo.IsZero() {
		return nil, errors.New("debt.premaster.Report: date_to required")
	}

	// 1. По выбранным INN определяем страны и собираем счета, которые нас интересуют.
	countries := countriesForINNs(f.EntityINNs)
	if len(countries) == 0 {
		return nil, fmt.Errorf("debt.premaster.Report: no known companies in entity_inns=%v", f.EntityINNs)
	}
	accountRoots := f.Accounts
	if len(accountRoots) == 0 {
		accountRoots = AccountsForCountries(countries)
	}
	if len(accountRoots) == 0 {
		return nil, fmt.Errorf("debt.premaster.Report: no accounts configured for countries %v (chart_of_accounts pending)", countries)
	}

	// 2. Собираем именованные параметры для IN-clause.
	args := []any{}
	innParams, args := bindIN(args, "i", f.EntityINNs)
	// accRoot фильтр работает по началу строки счёта: чтобы 62.01/62.02/62 все попали под root '62',
	// используем LIKE на root + конец строки или точка. Простой и понятный путь — IN по полным root + LIKE 'root.%'.
	// Здесь: добавляем для каждого root один параметр; в WHERE используем LEFT(DrAcc, …) = @p ИЛИ DrAcc LIKE @p+'.%%'.
	// Чтобы избежать вычислений с LEFT в WHERE (плохо для индекса), фильтруем БЕЗ ограничения по счетам в WHERE —
	// и считаем только нужные корни в CASE на стороне Go. Premaster по (CompanyID, Date) уже узкий.
	//
	// Альтернатива: для рабочих счётов узких (60.*, 62.*, 76.*, 90.*) — можно фильтровать `DrAcc LIKE '60%' OR ...`,
	// но это набор хрупких хардкодов. Оставим в v1 без фильтра — service отбросит KindOther.

	dateFromParam := "@dfrom"
	dateToParam := "@dto"
	dateLastMonthParam := "@dlast"
	args = append(args, sql.Named("dfrom", asMSSQLDate(f.DateFrom)))
	args = append(args, sql.Named("dto", asMSSQLDate(endOfDay(f.DateTo))))
	args = append(args, sql.Named("dlast", asMSSQLDate(startOfLastMonth(f.DateTo))))

	// ВГО-фильтр БЕЗУСЛОВНЫЙ: отчёт всегда внутригрупповой (union ico=1 / наш
	// контрагент) — то же определение, что в импорте. См. vgoMSSQLClause.
	// @vgoN переиспользуются в обоих WHERE (Dr и Cr) одного запроса.
	icoClause, vgoArgs := vgoMSSQLClause()
	args = append(args, vgoArgs...)

	// 3. SQL: UNION ALL Dr/Cr, GROUP BY (CompanyID, CounterpartyID, root).
	//    `Coalesce(CounterpartyID, '')` чтобы NULL не терялся в GROUP BY (мы потом отбросим).
	//    Корень счёта — `LEFT(acc, CHARINDEX('.', acc + '.') - 1)`: `+ '.'` гарантирует, что
	//    точка есть, даже для счёта без точки (`'9010'` → `'9010.' `→ position=5 → LEFT(4)).
	//
	//    WITH (NOLOCK) — обязательно по контракту на shared OLAP.
	//
	//    Обогащение Counterparty1C (имя/канал/менеджер) — LEFT JOIN ПОСЛЕ агрегации
	//    (на маленьком результате, не на 200M проводок). Опционально: при пустом
	//    counterpartyFQN отдаём NULL-колонки, и BuildReport падает на seed/ИНН как раньше.
	cpCols := ", CAST(NULL AS nvarchar(512)) AS partner_name, CAST(NULL AS nvarchar(256)) AS channel, CAST(NULL AS nvarchar(256)) AS manager"
	cpJoin := ""
	if r.counterpartyFQN != "" {
		cpCols = ", C.CounterpartyName1C AS partner_name, C.Channel AS channel, C.Manager AS manager"
		cpJoin = "LEFT JOIN " + r.counterpartyFQN + " C WITH (NOLOCK) ON LTRIM(RTRIM(C.UNP)) = LTRIM(RTRIM(agg.CounterpartyID))"
	}

	q := fmt.Sprintf(`
WITH src AS (
    SELECT CompanyID, ISNULL(CounterpartyID,'') AS CounterpartyID,
           LEFT(DrAcc, CHARINDEX('.', DrAcc + '.') - 1) AS acc_root,
           AmountWithVATCurrency AS amt,
           CAST(1 AS smallint) AS sign_dr,
           [Date]
    FROM %[6]s WITH (NOLOCK)
    WHERE CompanyID IN (%[1]s) AND [Date] <= %[2]s%[5]s
    UNION ALL
    SELECT CompanyID, ISNULL(CounterpartyID,''),
           LEFT(CrAcc, CHARINDEX('.', CrAcc + '.') - 1),
           AmountWithVATCurrency,
           CAST(-1 AS smallint),
           [Date]
    FROM %[6]s WITH (NOLOCK)
    WHERE CompanyID IN (%[1]s) AND [Date] <= %[2]s%[5]s
), agg AS (
    SELECT
        CompanyID,
        NULLIF(CounterpartyID,'') AS CounterpartyID,
        acc_root,
        SUM(CASE WHEN [Date] <  %[3]s THEN amt * sign_dr ELSE 0 END) AS opening_signed,
        SUM(CASE WHEN [Date] >= %[3]s THEN amt * sign_dr ELSE 0 END) AS turnover_signed,
        SUM(CASE WHEN [Date] >= %[4]s THEN amt * sign_dr ELSE 0 END) AS last_month_signed,
        SUM(amt * sign_dr) AS closing_signed
    FROM src
    GROUP BY CompanyID, CounterpartyID, acc_root
    HAVING ABS(SUM(amt * sign_dr)) > 0.005    -- отбросить строки с нулевым сальдо
        OR ABS(SUM(CASE WHEN [Date] <  %[3]s THEN amt * sign_dr ELSE 0 END)) > 0.005
        OR ABS(SUM(CASE WHEN [Date] >= %[3]s THEN amt * sign_dr ELSE 0 END)) > 0.005
)
SELECT agg.CompanyID, agg.CounterpartyID, agg.acc_root,
       agg.opening_signed, agg.turnover_signed, agg.last_month_signed, agg.closing_signed%[7]s
FROM agg
%[8]s
`, strings.Join(innParams, ","), dateToParam, dateFromParam, dateLastMonthParam, icoClause, r.mainFQN, cpCols, cpJoin)

	rows, err := r.db.QueryContext(ctx, q, args...)
	if err != nil {
		return nil, fmt.Errorf("debt.premaster.Report: query: %w", err)
	}
	defer rows.Close()

	raw := make([]rawRow, 0, 256)
	for rows.Next() {
		var rr rawRow
		if err := rows.Scan(&rr.CompanyID, &rr.CounterpartyID, &rr.AccountRoot,
			&rr.OpeningSigned, &rr.TurnoverSigned, &rr.LastMonthSigned, &rr.ClosingSigned,
			&rr.PartnerName, &rr.Channel, &rr.Manager); err != nil {
			return nil, fmt.Errorf("debt.premaster.Report: scan: %w", err)
		}
		raw = append(raw, rr)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("debt.premaster.Report: rows: %w", err)
	}

	return BuildReport(raw), nil
}

// drillRow — что вернёт SQL для drill-down: проводка с распознанными именами.
type drillRow struct {
	Date                 time.Time
	DocID                string
	RwNm                 int64
	DrAcc                string
	CrAcc                string
	Amount               float64
	ObjectsName          string         // из Objects.Name (NULL → ""), FQN — см. premasterRepo.objectsFQN
	Mapping              sql.NullString
	TransDescription     sql.NullString
	OperationDescription sql.NullString

	// Из Payments.Docs (LEFT JOIN по DocID; все NULL, если джойн отключён/не сматчился).
	DocBaseDate    sql.NullTime  // Docs.Date — дата документа-основания
	DocPaymentDate sql.NullTime  // Docs.PaymentDate — плановая дата оплаты (= срок)
	DocDelay       sql.NullInt64 // Docs.Delay — отсрочка в днях
}

// Drilldown — детализация по документам внутри (CompanyINN, PartnerINN, Account, Period).
// Игнорирует q.Contract и q.Currency в M2 (нет resolver субконто и нет колонки валюты
// в Premaster — см. M4/M5). UI должен дёргать с пустыми Contract/Currency.
func (r *premasterRepo) Drilldown(ctx context.Context, q DrilldownQuery) ([]DocumentRow, error) {
	if q.CompanyINN == "" || q.PartnerINN == "" || q.Account == "" {
		return nil, errors.New("debt.premaster.Drilldown: company_inn/partner_inn/account required")
	}
	if q.DateTo.IsZero() {
		return nil, errors.New("debt.premaster.Drilldown: date_to required")
	}

	args := []any{
		sql.Named("company", q.CompanyINN),
		sql.Named("partner", q.PartnerINN),
		sql.Named("acc", AccountRoot(q.Account)),
		sql.Named("dfrom", asMSSQLDate(q.DateFrom)),
		sql.Named("dto", asMSSQLDate(endOfDay(q.DateTo))),
	}

	// `LEFT(... , CHARINDEX('.', acc + '.') - 1)` — корень счёта (см. Report).
	// LEFT JOIN Objects по DocID — даёт читаемое имя документа без regex (приоритет в ResolveDoc).
	// Drill-down показывает все проводки, формирующие сальдо НА @dto,
	// без нижней границы по дате. @dfrom хранится в DrilldownQuery, но в SQL
	// не используется — он нужен, если в будущем добавим режим «только за период».
	// Сейчас семантика: «что сложило задолженность к концу периода» — полная история.
	_ = q.DateFrom

	// Опциональный кросс-БД джойн к Payments.Docs за датой оплаты/отсрочкой.
	// LEFT JOIN: если не сконфигурирован или ключ не сматчился — drill-down
	// возвращает проводки как раньше, просто без payment_due_date/overdue_days.
	docsCols := ", CAST(NULL AS date) AS doc_base_date, CAST(NULL AS date) AS doc_payment_date, CAST(NULL AS int) AS doc_delay"
	docsJoin := ""
	if r.docsFQN != "" {
		docsCols = ", D.[Date] AS doc_base_date, D.PaymentDate AS doc_payment_date, D.Delay AS doc_delay"
		docsJoin = "LEFT JOIN " + r.docsFQN + " D WITH (NOLOCK) ON D.ID = A.DocID"
	}

	q1 := fmt.Sprintf(`
SELECT A.[Date], CONVERT(nvarchar(max), A.DocID, 1) AS DocID, A.RwNm,
       A.DrAcc, A.CrAcc, A.AmountWithVATCurrency,
       ISNULL(F.[Name], '') AS objects_name,
       A.Mapping, A.TransDescription, A.OperationDescription%[3]s
FROM %[1]s A WITH (NOLOCK)
LEFT JOIN %[2]s F ON A.DocID = F.ID
%[4]s
WHERE A.CompanyID = @company
  AND A.CounterpartyID = @partner
  AND (   LEFT(A.DrAcc, CHARINDEX('.', A.DrAcc + '.') - 1) = @acc
       OR LEFT(A.CrAcc, CHARINDEX('.', A.CrAcc + '.') - 1) = @acc )
  AND A.[Date] <= @dto
ORDER BY A.[Date], A.DocID, A.RwNm`, r.mainFQN, r.objectsFQN, docsCols, docsJoin)

	rows, err := r.db.QueryContext(ctx, q1, args...)
	if err != nil {
		return nil, fmt.Errorf("debt.premaster.Drilldown: query: %w", err)
	}
	defer rows.Close()

	raw := make([]drillRow, 0, 64)
	for rows.Next() {
		var d drillRow
		if err := rows.Scan(&d.Date, &d.DocID, &d.RwNm, &d.DrAcc, &d.CrAcc, &d.Amount,
			&d.ObjectsName, &d.Mapping, &d.TransDescription, &d.OperationDescription,
			&d.DocBaseDate, &d.DocPaymentDate, &d.DocDelay); err != nil {
			return nil, fmt.Errorf("debt.premaster.Drilldown: scan: %w", err)
		}
		raw = append(raw, d)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("debt.premaster.Drilldown: rows: %w", err)
	}

	// Определяем страну юрлица — нужно для классификации DZ/KZ по счёту.
	country := countryOfINN(q.CompanyINN)
	return BuildDrilldown(raw, country, AccountRoot(q.Account), q.DateTo), nil
}

// countryOfINN — страна нашего юрлица; пустая если CompanyINN не наш.
func countryOfINN(inn string) Country {
	for _, e := range Entities() {
		if e.INN == inn {
			return e.Country
		}
	}
	return ""
}

// bindIN — добавляет позиционные параметры для IN-clause и возвращает их
// текстовые имена (`@in0,@in1,...`) для подстановки в SQL.
func bindIN(args []any, prefix string, values []string) ([]string, []any) {
	names := make([]string, len(values))
	for i, v := range values {
		name := fmt.Sprintf("%s%d", prefix, i)
		names[i] = "@" + name
		args = append(args, sql.Named(name, v))
	}
	return names, args
}

// asMSSQLDate — конвертирует time.Time в строку 'YYYY-MM-DD' для безопасной
// передачи как datetime в MSSQL (go-mssqldb для именованных параметров
// корректно сериализует time.Time, но мы явно отдаём дату без часов).
func asMSSQLDate(t time.Time) time.Time {
	return time.Date(t.Year(), t.Month(), t.Day(), 0, 0, 0, 0, time.UTC)
}

// endOfDay — конец суток для DateTo (включительно по дню).
func endOfDay(t time.Time) time.Time {
	return time.Date(t.Year(), t.Month(), t.Day(), 23, 59, 59, 0, time.UTC)
}

// startOfLastMonth — первое число календарного месяца, в котором находится t.
// Для t = 2026-04-15 → 2026-04-01. Используется для RevenueLastMonth.
func startOfLastMonth(t time.Time) time.Time {
	return time.Date(t.Year(), t.Month(), 1, 0, 0, 0, 0, time.UTC)
}

// countriesForINNs — определяет уникальные страны по списку наших ИНН/УНП
// через хардкод-справочник Entities() (seed.go). Неизвестные ИНН игнорируются.
func countriesForINNs(inns []string) []Country {
	ent := Entities()
	byINN := make(map[string]Country, len(ent))
	for _, e := range ent {
		byINN[e.INN] = e.Country
	}
	seen := map[Country]struct{}{}
	for _, inn := range inns {
		if c, ok := byINN[strings.TrimSpace(inn)]; ok {
			seen[c] = struct{}{}
		}
	}
	out := make([]Country, 0, len(seen))
	for c := range seen {
		out = append(out, c)
	}
	return out
}
