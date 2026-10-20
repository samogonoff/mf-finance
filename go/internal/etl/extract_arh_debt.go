package etl

import (
	"database/sql"
	"fmt"
	"regexp"
	"strings"
)

// Поток ARH (DEBT_BACKEND=arh): остатки ДЗ/КЗ строятся из СЫРЫХ таблиц OLAP —
// [Payments].[dbo].[Debt_arh] + [Docs] — а не из готовых вьюх FinDebt1/3.
// Правила пересборки сверены на живом OLAP с FinDebt1 (docs/reports/debt/arh-runbook.md):
//   - остаток на дату снэпшота Debt_arh.Date (datetime 23:59:59 → сравниваем по дню);
//   - знак строки Sum: плюс = ДЗ, минус = КЗ (поле DtKt не используется);
//   - корзины Sum30/60/90/999 суммируются по тому же знаку, что и Sum строки;
//   - ВГО-контур остатков: Chnl или Folder = 'ГРУППА КОМПАНИЙ' (см. arhVGOCond);
//   - документ/договор/срок: Debt_arh.DocID → Docs.ID, родитель-договор — вторым
//     присоединением Docs по ParentID; Docs содержит дубли ID (нулевая запись) —
//     поэтому Docs читается через подзапрос с дедупом по ID (ROW_NUMBER).
//
// Зерно выгрузки = ключ дедупа ReplacingMergeTree fact_arh_debt: дата × ЮЛ × контрагент
// × счёт × DocID × валюта. Остальное (имя, № документа, срок) — MAX() внутри ключа.

// ArhTables — FQN сырых таблиц OLAP.
type ArhTables struct {
	DebtFQN       string // [Payments].[dbo].[Debt_arh]
	DocsFQN       string // [Payments].[dbo].[Docs]
	WholesalesFQN string // [Payments].[dbo].[Wholesales_arh]
	// Orgs — УНП наших ЮЛ (UNPOrg): сужают выборку до индекса Debt_arh (UNPOrg, Date),
	// без них запрос читает все 85 млн строк. Обязателен.
	Orgs []string
}

var arhOrgRe = regexp.MustCompile(`^[0-9A-Za-z]{1,20}$`)

// arhOrgCond — условие `alias.UNPOrg IN (N'..')` по нашим ЮЛ. UNPOrg — nchar(20), хвостовые
// пробелы при сравнении не мешают, а предикат остаётся sargable (в отличие от LTRIM/RTRIM).
// Значения валидируются (идентификатор УНП), в SQL уходят литералами. Пусто → ошибка в
// runArh, поэтому здесь возвращаем заведомо ложное условие, а не «без фильтра».
func arhOrgCond(alias string, orgs []string) string {
	lits := make([]string, 0, len(orgs))
	for _, o := range orgs {
		if o = strings.TrimSpace(o); arhOrgRe.MatchString(o) {
			lits = append(lits, "N'"+o+"'")
		}
	}
	if len(lits) == 0 {
		return "1 = 0"
	}
	return alias + ".UNPOrg IN (" + strings.Join(lits, ", ") + ")"
}

// arhVGOCond — ВГО-контур остатков Debt_arh: Chnl или Folder = 'ГРУППА КОМПАНИЙ'. У РФ/РБ
// они совпадают, у ЮЛ УЗ/КЗ Chnl пуст, а Folder заполнен: без Folder эти ЮЛ выпали бы целиком.
// Сверка с FinDebt1 30.09.2026 (BYN): ЮЛ УЗ 305554644 сходится до единицы (после нетто по
// DocID в отчёте), ЮЛ КЗ 141240004842 — нет: FinDebt сворачивает документ и МЕЖДУ счетами
// (1210 ДЗ против 3310 КЗ), мы — внутри счёта (ДЗ 13.42 против 8.35 млн) — открытый вопрос.
func arhVGOCond(alias string) string {
	return "(" + alias + ".Chnl = @grp OR " + alias + ".Folder = @grp)"
}

// arhVGOSalesCond — ВГО-контур продаж Wholesales_arh: Chnl или Folder (Chnl в продажах пуст,
// контур задаёт Folder; сверено: ТД 99 779 348 RUB и 3 658 609 BYN за сентябрь 2026).
func arhVGOSalesCond(alias string) string {
	return "(" + alias + ".Chnl = @grp OR " + alias + ".Folder = @grp)"
}

// arhDayRange — выборка одного календарного дня по datetime-колонке без CONVERT по
// каждой строке (sargable). @dt — строка 'YYYYMMDD' (стиль 112: логин работает с
// DATEFORMAT dmy, ISO-литерал 'YYYY-MM-DD' читался бы неверно).
func arhDayRange(col string) string {
	return col + " >= CONVERT(date, @dt, 112) AND " + col + " < DATEADD(day, 1, CONVERT(date, @dt, 112))"
}

// arhSpanRange — короткий чанк дней [@from..@to] включительно, оба — 'YYYYMMDD'.
func arhSpanRange(col string) string {
	return col + " >= CONVERT(date, @from, 112) AND " + col + " < DATEADD(day, 1, CONVERT(date, @to, 112))"
}

// arhDate — дата строки (без времени) 'YYYY-MM-DD'.
func arhDate(col string) string { return "CONVERT(CHAR(10), CONVERT(date, " + col + "), 23)" }

// arhDocDate — валидная дата Docs: в источнике бывают 0001-01-01 и 2202-xx-xx, которые
// не влезают в CH Date → NULL (в MAX() игнорируется).
func arhDocDate(col string) string {
	return "CASE WHEN " + col + " >= '19700102' AND " + col + " < '21000101' THEN " + col + " END"
}

// factArhDebtRow — строка для insert'а в finance.fact_arh_debt (JSONEachRow).
type factArhDebtRow struct {
	SnapshotDate   string  `json:"snapshot_date"`
	CompanyID      string  `json:"company_id"`
	Company        string  `json:"company"`
	CounterpartyID string  `json:"counterparty_id"`
	Counterparty   string  `json:"counterparty"`
	Acc            string  `json:"acc"`
	AccRoot        string  `json:"acc_root"`
	DocID          string  `json:"doc_id"`
	DocNumber      string  `json:"doc_number"`
	DocDate        string  `json:"doc_date"`
	ContractID     string  `json:"contract_id"`
	ContractNumber string  `json:"contract_number"`
	ContractTitle  string  `json:"contract_title"`
	DocType        string  `json:"doc_type"`
	Delay          int32   `json:"delay"`
	PaymentDate    *string `json:"payment_date"`
	CurrencyKOD    int32   `json:"currency_kod"`
	SumD           string  `json:"sum_d"`
	SumK           string  `json:"sum_k"`
	Sum30D         string  `json:"sum30_d"`
	Sum60D         string  `json:"sum60_d"`
	Sum90D         string  `json:"sum90_d"`
	Sum999D        string  `json:"sum999_d"`
	Sum30K         string  `json:"sum30_k"`
	Sum60K         string  `json:"sum60_k"`
	Sum90K         string  `json:"sum90_k"`
	Sum999K        string  `json:"sum999_k"`
	SrcChangedAt   string  `json:"src_changed_at"`
}

// arhDebtNonZero — отсекаем полностью нулевые строки (сумма и все корзины).
const arhDebtNonZero = "(d.Sum <> 0 OR d.Sum30 <> 0 OR d.Sum60 <> 0 OR d.Sum90 <> 0 OR d.Sum999 <> 0)"

// arhChg — момент изменения строки в источнике (DateChange; если пуст — сама дата).
const arhChg = "ISNULL(d.DateChange, d.[Date])"

// extractArhDebtSQL — документная детализация остатков ВГО на ОДИН день (@dt, @grp).
//
// Docs читается дважды через CTE с дедупом по ID (ROW_NUMBER, rn = 1): сначала
// документы строк снэпшота (ID сравнивается с DocID напрямую, оба nvarchar(36) — без
// CAST по строке), затем их родители-договоры по ParentID. Выборка ограничена нужными
// ID, поэтому полный Docs (≈3.3 млн) не тянется на каждую дату. Пустой DocID не
// джойнится (Src его не содержит) — остаток без документа остаётся с пустым номером.
// Нулевые дубли Docs (ID = 0/пусто) не задваивают строки: дедуп по ID.
func extractArhDebtSQL(t ArhTables) string {
	org := "LTRIM(RTRIM(ISNULL(d.UNPOrg, '')))"
	unp := "LTRIM(RTRIM(ISNULL(d.UNP, '')))"
	acc := "LTRIM(RTRIM(ISNULL(d.Acc, '')))"
	root := accRootSQL(acc)
	docID := "ISNULL(d.DocID, '')"
	cur := "CONVERT(INT, ISNULL(d.Currency, 0))"
	pos := func(col string) string {
		return "CONVERT(VARCHAR(40), SUM(CASE WHEN d.Sum > 0 THEN ISNULL(d." + col + ", 0) ELSE 0 END))"
	}
	neg := func(col string) string {
		return "CONVERT(VARCHAR(40), SUM(CASE WHEN d.Sum < 0 THEN ISNULL(d." + col + ", 0) ELSE 0 END))"
	}
	return `
;WITH Src AS (
    SELECT DISTINCT d.DocID
    FROM ` + t.DebtFQN + ` d WITH (NOLOCK)
    WHERE ` + arhDayRange("d.[Date]") + `
      AND ` + arhOrgCond("d", t.Orgs) + `
      AND ` + arhVGOCond("d") + `
      AND ` + arhDebtNonZero + `
      AND d.DocID IS NOT NULL AND d.DocID <> ''
),
Doc AS (
    SELECT q.ID, q.ParentID, q.[Date], q.Number, q.DocType, q.Delay, q.PaymentDate
    FROM (
        SELECT x.ID, x.ParentID, x.[Date], x.Number, x.DocType, x.Delay, x.PaymentDate,
               ROW_NUMBER() OVER (PARTITION BY x.ID ORDER BY x.[Date] DESC, x.Number DESC) AS rn
        FROM ` + t.DocsFQN + ` x WITH (NOLOCK)
        WHERE x.ID IN (SELECT DocID FROM Src)
    ) q
    WHERE q.rn = 1
),
Par AS (
    SELECT q.ID, q.Number, q.Description
    FROM (
        SELECT p.ID, p.Number, p.Description,
               ROW_NUMBER() OVER (PARTITION BY p.ID ORDER BY p.[Date] DESC, p.Number DESC) AS rn
        FROM ` + t.DocsFQN + ` p WITH (NOLOCK)
        WHERE p.ID IN (SELECT ParentID FROM Doc WHERE ParentID IS NOT NULL AND ParentID <> '')
    ) q
    WHERE q.rn = 1
)
SELECT
    ` + arhDate("d.[Date]") + `                             AS snapshot_date,
    ` + org + `                                             AS company_id,
    ''                                                      AS company,
    ` + unp + `                                             AS counterparty_id,
    MAX(RTRIM(ISNULL(d.Name, '')))                          AS counterparty,
    ` + acc + `                                             AS acc,
    ` + root + `                                            AS acc_root,
    ` + docID + `                                           AS doc_id,
    MAX(LTRIM(RTRIM(ISNULL(dc.Number, ''))))                 AS doc_number,
    ISNULL(CONVERT(CHAR(10), MAX(` + arhDocDate("dc.[Date]") + `), 23), '1970-01-01') AS doc_date,
    MAX(ISNULL(dc.ParentID, ''))                            AS contract_id,
    MAX(LTRIM(RTRIM(ISNULL(pc.Number, ''))))                 AS contract_number,
    MAX(LTRIM(RTRIM(ISNULL(pc.Description, ''))))            AS contract_title,
    MAX(ISNULL(NULLIF(LTRIM(RTRIM(dc.DocType)), 'NULL'), ''))   AS doc_type,
    MAX(CONVERT(INT, ISNULL(dc.Delay, 0)))                  AS delay,
    CONVERT(CHAR(10), MAX(` + arhDocDate("dc.PaymentDate") + `), 23) AS payment_date,
    ` + cur + `                                             AS currency_kod,
    ` + pos("Sum") + `    AS sum_d,
    ` + neg("Sum") + `    AS sum_k,
    ` + pos("Sum30") + `  AS sum30_d,
    ` + pos("Sum60") + `  AS sum60_d,
    ` + pos("Sum90") + `  AS sum90_d,
    ` + pos("Sum999") + ` AS sum999_d,
    ` + neg("Sum30") + `  AS sum30_k,
    ` + neg("Sum60") + `  AS sum60_k,
    ` + neg("Sum90") + `  AS sum90_k,
    ` + neg("Sum999") + ` AS sum999_k,
    CONVERT(CHAR(19), MAX(` + arhChg + `), 120)             AS src_changed_at
FROM ` + t.DebtFQN + ` d WITH (NOLOCK)
LEFT JOIN Doc dc ON dc.ID = d.DocID
LEFT JOIN Par pc ON pc.ID = dc.ParentID
WHERE ` + arhDayRange("d.[Date]") + `
  AND ` + arhOrgCond("d", t.Orgs) + `
  AND ` + arhVGOCond("d") + `
  AND ` + arhDebtNonZero + `
GROUP BY ` + arhDate("d.[Date]") + `, ` + org + `, ` + unp + `, ` + acc + `, ` + root + `, ` + docID + `, ` + cur
}

// extractArhDebtDatesSQL — отпечатки дней чанка [@from..@to]: MAX(DateChange) по дню.
// По ним инкремент находит перезаписанные 1С даты (сравнение с src_changed_at в CH).
// Фильтр строк тот же, что в extractArhDebtSQL, иначе отпечатки разойдутся.
func extractArhDebtDatesSQL(t ArhTables) string {
	return `
SELECT ` + arhDate("d.[Date]") + ` AS d, CONVERT(CHAR(19), MAX(` + arhChg + `), 120) AS changed_at, COUNT(*) AS n
FROM ` + t.DebtFQN + ` d WITH (NOLOCK)
WHERE ` + arhSpanRange("d.[Date]") + `
  AND ` + arhOrgCond("d", t.Orgs) + `
  AND ` + arhVGOCond("d") + `
  AND ` + arhDebtNonZero + `
GROUP BY ` + arhDate("d.[Date]")
}

// scanArhDebtRow — Scan строки extractArhDebtSQL (28 колонок).
func scanArhDebtRow(rows *sql.Rows) (factArhDebtRow, error) {
	var r factArhDebtRow
	var pay sql.NullString
	if err := rows.Scan(
		&r.SnapshotDate, &r.CompanyID, &r.Company, &r.CounterpartyID, &r.Counterparty,
		&r.Acc, &r.AccRoot, &r.DocID, &r.DocNumber, &r.DocDate,
		&r.ContractID, &r.ContractNumber, &r.ContractTitle, &r.DocType, &r.Delay, &pay,
		&r.CurrencyKOD, &r.SumD, &r.SumK,
		&r.Sum30D, &r.Sum60D, &r.Sum90D, &r.Sum999D, &r.Sum30K, &r.Sum60K, &r.Sum90K, &r.Sum999K,
		&r.SrcChangedAt,
	); err != nil {
		return r, fmt.Errorf("scan arh debt: %w", err)
	}
	r.Company = strings.TrimSpace(r.Company)
	if pay.Valid && pay.String != "" {
		v := pay.String
		r.PaymentDate = &v
	}
	return r, nil
}
