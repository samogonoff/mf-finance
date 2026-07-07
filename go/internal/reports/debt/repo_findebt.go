package debt

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"

	_ "github.com/microsoft/go-mssqldb"
)

// findebtRepo — источник DEBT_BACKEND=findebt. Читает готовые отчётные вьюхи
// расчётного слоя Payments, которые аналитик сверил с 1С копейка-в-копейку
// (см. docs/reports/debt/findebt-verification.md):
//
//   - [Payments].[report].[FinDebt1] — суточный снэпшот остатка ДЗ/КЗ.
//     Классификация ДЗ/КЗ уже сделана (DEBT_FILTER), счёт заполнен (Acc),
//     корзины просрочки посчитаны (SUM30/60/90/999).
//   - [Payments].[report].[FinDebt3] — документная детализация (Doc_Number,
//     Payment_Date, DAY_DELAY, SUM_D/SUM_K). Агрегируется ровно в FinDebt1.
//
// Отличие от premaster/glmf/finpl: здесь мы НЕ считаем свёртку сами — берём
// готовые числа чужого ETL. Это осознанный компромисс (потеря контроля над
// цифрами ради точного совпадения с 1С); риски — см. payments-source-map.md §3.
//
// Маппинг на DebtRow не требует правок модели/UI: FinDebt даёт те же
// (компания → контрагент → счёт) остатки Opening/Turnover/Closing.
type findebtRepo struct {
	db      *sql.DB
	fin1FQN string // [Payments].[report].[FinDebt1]
	fin3FQN string // [Payments].[report].[FinDebt3]
}

// findebtVGOChannel — значение Channel (в FinDebt1) / Folder (в FinDebt3),
// маркирующее внутригрупповой контур. Проверено: только это написание несёт
// ВГО-баланс; варианты-опечатки пустые (findebt-verification.md §4). Это данные
// отчётного движка, не пользовательский ввод — держим константой, не env.
const findebtVGOChannel = "ГРУППА КОМПАНИЙ"

// Линза суммы (CUR_FILTER) больше НЕ пиннится — выбирается фильтром (f.Lens,
// нормализуется normLens, дефолт «В валюте договора»). CUR_FILTER даёт три
// представления одной суммы; фильтр по одной линзе снимает задвоение ×3. См.
// lens.go и docs/reports/debt/findebt-verification.md.

// findebtSkipFilter — служебное значение DEBT_FILTER, которое НЕ баланс, а строки
// оборотов «оплаты и отгрузки»; отсекаем явно.
const findebtSkipFilter = "Отображать платежи и отгр."

// NewFinDebtRepo строит репо поверх уже открытого MSSQL-пула к OLAP-серверу
// (вьюхи FinDebt* живут в отдельной БД Payments на ТОМ ЖЕ сервере, что и
// Premaster1C — обращаемся трёхчастным именем). db==nil → (nil, nil): сигнал
// main.go, что live-репо не настроено (как у premaster/finpl).
func NewFinDebtRepo(db *sql.DB, database, schema, fin1, fin3 string) (PremasterRepo, error) {
	if db == nil {
		return nil, nil
	}
	for k, v := range map[string]string{"database": database, "schema": schema, "fin1_table": fin1, "fin3_table": fin3} {
		if !identRe.MatchString(v) {
			return nil, fmt.Errorf("debt.findebt: invalid %s %q (must match %s)", k, v, identRe.String())
		}
	}
	return &findebtRepo{
		db:      db,
		fin1FQN: "[" + database + "].[" + schema + "].[" + fin1 + "]",
		fin3FQN: "[" + database + "].[" + schema + "].[" + fin3 + "]",
	}, nil
}

// findebtRow — сырая строка свода из FinDebt1: остатки ДЗ/КЗ на снэпшот закрытия
// и открытия по (организация, контрагент, счёт). Знак FinDebt: ДЗ положительна,
// КЗ отрицательна.
type findebtRow struct {
	Company    string
	CompanyINN string
	Partner    string
	PartnerINN string
	Acc        string
	Currency   string // native-валюта строки (FinDebt1.Currency)
	OpenDZ     float64
	OpenKZ     float64
	CloseDZ    float64
	CloseKZ    float64
}

// Report — свод ДЗ/КЗ ВГО на снэпшот DateTo (закрытие) и DateFrom (открытие).
//
// Границы периода — «последний доступный снэпшот» (записка аналитика §3):
//
//	closeDate = MAX(Date) WHERE Date <= @DateTo   (остаток на конец периода)
//	openDate  = MAX(Date) WHERE Date <  @DateFrom (остаток до начала периода)
//	turnover  = closing − opening
//
// EntityINNs опционален (в отличие от premaster): ВГО-фильтр + дата уже узкие,
// full scan не грозит. Пусто → все ВГО-организации.
func (r *findebtRepo) Report(ctx context.Context, f Filters) ([]DebtRow, error) {
	if f.DateTo.IsZero() {
		return nil, errors.New("debt.findebt.Report: date_to required")
	}

	lens := normLens(f.Lens)
	args := []any{
		sql.Named("grp", findebtVGOChannel),
		sql.Named("cur", lens),
		sql.Named("skip", findebtSkipFilter),
		sql.Named("dfrom", asMSSQLDate(f.DateFrom)),
		sql.Named("dto", asMSSQLDate(f.DateTo)),
	}

	// В линзе «В валюте договора» суммы в native-валюте — группируем ПО валюте, иначе
	// смешали бы валюты в одном итоге. В линзах BYN/USD сумма уже пересчитана —
	// валюту в ключ не берём (агрегируем через неё), подпись ставит lensCurrency.
	curGroupClause := ""
	if lens == LensContract {
		curGroupClause = ", LTRIM(RTRIM(f.Currency))"
	}

	// Фильтр организаций (по УНП нашего ЮЛ). Опционален.
	orgClause := ""
	if len(f.EntityINNs) > 0 {
		ph, a := bindIN(args, "i", f.EntityINNs)
		args = a
		orgClause = " AND LTRIM(RTRIM(f.UNPOrg)) IN (" + strings.Join(ph, ",") + ")"
	}

	// Фильтр счетов — по КОРНЮ счёта (UI шлёт коды-корни 60/62/76, а FinDebt.Acc —
	// субсчёт 62.4.1). Опционален.
	accClause := ""
	if len(f.Accounts) > 0 {
		ph, a := bindIN(args, "a", f.Accounts)
		args = a
		accClause = " AND " + accRootSQL("LTRIM(RTRIM(f.Acc))") + " IN (" + strings.Join(ph, ",") + ")"
	}

	// bounds: обе граничные даты одним под-запросом; CROSS JOIN к 1-строчному CTE.
	// Date IN (close, NULL) корректно вырождается в Date=close (NULL в IN игнорится),
	// поэтому при отсутствии открывающего снэпшота opening просто = 0.
	q := fmt.Sprintf(`
WITH bounds AS (
    SELECT
        (SELECT MAX([Date]) FROM %[1]s WITH (NOLOCK) WHERE [Date] <= @dto   AND Channel = @grp) AS close_dt,
        (SELECT MAX([Date]) FROM %[1]s WITH (NOLOCK) WHERE [Date] <  @dfrom AND Channel = @grp) AS open_dt
)
SELECT
    MAX(f.Organisation)                                                          AS company,
    LTRIM(RTRIM(f.UNPOrg))                                                       AS company_inn,
    MAX(f.Contragent)                                                            AS partner,
    LTRIM(RTRIM(f.UNP))                                                          AS partner_inn,
    LTRIM(RTRIM(f.Acc))                                                          AS acc,
    MAX(LTRIM(RTRIM(f.Currency)))                                                AS native_currency,
    SUM(CASE WHEN f.[Date] = b.open_dt  AND f.DEBT_FILTER = N'Дебиторская'  THEN f.[SUM] ELSE 0 END) AS open_dz,
    SUM(CASE WHEN f.[Date] = b.open_dt  AND f.DEBT_FILTER = N'Кредиторская' THEN f.[SUM] ELSE 0 END) AS open_kz,
    SUM(CASE WHEN f.[Date] = b.close_dt AND f.DEBT_FILTER = N'Дебиторская'  THEN f.[SUM] ELSE 0 END) AS close_dz,
    SUM(CASE WHEN f.[Date] = b.close_dt AND f.DEBT_FILTER = N'Кредиторская' THEN f.[SUM] ELSE 0 END) AS close_kz
FROM %[1]s f WITH (NOLOCK)
CROSS JOIN bounds b
WHERE f.[Date] IN (b.close_dt, b.open_dt)
  AND f.Channel = @grp
  AND f.CUR_FILTER = @cur
  AND f.DEBT_FILTER <> @skip%[2]s%[3]s
GROUP BY LTRIM(RTRIM(f.UNPOrg)), LTRIM(RTRIM(f.UNP)), LTRIM(RTRIM(f.Acc))%[4]s
HAVING ABS(SUM(CASE WHEN f.[Date] = b.close_dt AND f.DEBT_FILTER = N'Дебиторская'  THEN f.[SUM] ELSE 0 END)) > 0.005
    OR ABS(SUM(CASE WHEN f.[Date] = b.close_dt AND f.DEBT_FILTER = N'Кредиторская' THEN f.[SUM] ELSE 0 END)) > 0.005
    OR ABS(SUM(CASE WHEN f.[Date] = b.open_dt  AND f.DEBT_FILTER = N'Дебиторская'  THEN f.[SUM] ELSE 0 END)) > 0.005
    OR ABS(SUM(CASE WHEN f.[Date] = b.open_dt  AND f.DEBT_FILTER = N'Кредиторская' THEN f.[SUM] ELSE 0 END)) > 0.005
ORDER BY company, partner, acc`, r.fin1FQN, orgClause, accClause, curGroupClause)

	rows, err := r.db.QueryContext(ctx, q, args...)
	if err != nil {
		return nil, fmt.Errorf("debt.findebt.Report: query: %w", err)
	}
	defer rows.Close()

	raw := make([]findebtRow, 0, 256)
	for rows.Next() {
		var rr findebtRow
		if err := rows.Scan(&rr.Company, &rr.CompanyINN, &rr.Partner, &rr.PartnerINN, &rr.Acc,
			&rr.Currency, &rr.OpenDZ, &rr.OpenKZ, &rr.CloseDZ, &rr.CloseKZ); err != nil {
			return nil, fmt.Errorf("debt.findebt.Report: scan: %w", err)
		}
		raw = append(raw, rr)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("debt.findebt.Report: rows: %w", err)
	}
	return buildFinDebtReport(raw, lens), nil
}

// buildFinDebtReport разворачивает сырые остатки FinDebt в DebtRow.
// КЗ во вьюхе отрицательна → переворачиваем в «положительный долг» (наша конвенция).
// Account несёт ПОЛНЫЙ субсчёт (напр. «62.4.1») — он же ключ drill-down в FinDebt3.
func buildFinDebtReport(raw []findebtRow, lens string) []DebtRow {
	out := make([]DebtRow, 0, len(raw))
	for _, rr := range raw {
		country := countryOfINN(rr.CompanyINN) // "" для незнакомого УНП — не теряем строку
		root := AccountRoot(rr.Acc)
		row := DebtRow{
			Country:     country,
			Company:     strings.TrimSpace(rr.Company),
			CompanyINN:  rr.CompanyINN,
			Partner:     strings.TrimSpace(rr.Partner),
			PartnerINN:  rr.PartnerINN,
			Account:     rr.Acc, // полный субсчёт = ключ drilldown
			AccountName: accountNameFor(country, root),
			Subaccount:  rr.Acc,
			Currency:    lensCurrency(lens, rr.Currency),
			OpeningDZ:   rr.OpenDZ,
			OpeningKZ:   -rr.OpenKZ,
			ClosingDZ:   rr.CloseDZ,
			ClosingKZ:   -rr.CloseKZ,
			TurnoverDZ:  rr.CloseDZ - rr.OpenDZ,
			TurnoverKZ:  (-rr.CloseKZ) - (-rr.OpenKZ),
		}
		out = append(out, row)
	}
	return out
}

// Drilldown — документная детализация из FinDebt3 на снэпшот закрытия (DateTo).
// Фильтр по (организация, контрагент, полный счёт). Договор в FinDebt3 не хранится
// (документы висят прямо под счётом) → q.Contract игнорируется. Просрочка берётся
// готовой из вьюхи (Payment_Date, DAY_DELAY).
func (r *findebtRepo) Drilldown(ctx context.Context, q DrilldownQuery) ([]DocumentRow, error) {
	if q.CompanyINN == "" || q.PartnerINN == "" || q.Account == "" {
		return nil, errors.New("debt.findebt.Drilldown: company_inn/partner_inn/account required")
	}
	if q.DateTo.IsZero() {
		return nil, errors.New("debt.findebt.Drilldown: date_to required")
	}

	args := []any{
		sql.Named("grp", findebtVGOChannel),
		sql.Named("cur", normLens(q.Lens)),
		sql.Named("company", strings.TrimSpace(q.CompanyINN)),
		sql.Named("partner", strings.TrimSpace(q.PartnerINN)),
		sql.Named("acc", strings.TrimSpace(q.Account)),
		sql.Named("dto", asMSSQLDate(q.DateTo)),
	}

	query := fmt.Sprintf(`
SELECT d.Doc_Date, d.Doc_Number, d.Doc_Description, d.Payment_Date, d.DAY_DELAY, d.SUM_D, d.SUM_K
FROM %[1]s d WITH (NOLOCK)
WHERE d.[Date] = (SELECT MAX([Date]) FROM %[1]s WITH (NOLOCK) WHERE [Date] <= @dto AND Folder = @grp)
  AND LTRIM(RTRIM(d.UNPOrg)) = @company
  AND LTRIM(RTRIM(d.UNP))    = @partner
  AND LTRIM(RTRIM(d.Acc))    = @acc
  AND d.Folder = @grp
  AND d.CUR_FILTER = @cur
  AND (d.SUM_D <> 0 OR d.SUM_K <> 0)
ORDER BY d.Doc_Date, d.Doc_Number`, r.fin3FQN)

	rows, err := r.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("debt.findebt.Drilldown: query: %w", err)
	}
	defer rows.Close()

	out := make([]DocumentRow, 0, 64)
	for rows.Next() {
		var (
			docDate, payDate sql.NullTime
			docNum, docDesc  sql.NullString
			dayDelay         sql.NullInt64
			sumD, sumK       float64
		)
		if err := rows.Scan(&docDate, &docNum, &docDesc, &payDate, &dayDelay, &sumD, &sumK); err != nil {
			return nil, fmt.Errorf("debt.findebt.Drilldown: scan: %w", err)
		}
		// Тип документа = сторона (ДЗ/КЗ) — по ненулевой сумме. Уходит в TransGroup
		// для группировки на фронте (byTrans), и в DocKind как подпись.
		kind := "Документ"
		if sumD != 0 {
			kind = "Дебиторская"
		} else if sumK != 0 {
			kind = "Кредиторская"
		}
		dr := DocumentRow{
			DocNumber:   nz(docNum),
			DocKind:     kind,
			TransGroup:  kind,
			Amount:      absF(sumD) + absF(sumK),
			Description: nz(docDesc),
			DZChange:    sumD,
			KZChange:    -sumK, // КЗ во вьюхе отрицательна → в положительный долг
		}
		if docDate.Valid {
			dr.DocDate = docDate.Time
		}
		if payDate.Valid {
			dr.PaymentDueDate = payDate.Time
		}
		if dayDelay.Valid {
			dr.OverdueDays = int(dayDelay.Int64)
		}
		out = append(out, dr)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("debt.findebt.Drilldown: rows: %w", err)
	}
	return out, nil
}

// absF — модуль float64 без импорта math (мелочь, чтобы не тащить пакет).
func absF(v float64) float64 {
	if v < 0 {
		return -v
	}
	return v
}
