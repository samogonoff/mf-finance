package etl

import (
	"database/sql"
	"fmt"
	"strings"
)

// Продажи/возвраты ARH-потока: [Payments].[dbo].[Wholesales_arh] → finance.fact_arh_sales.
//
// Правила (сверены на живом OLAP: ТД, сентябрь 2026, ВГО — 99 779 348 RUB / 292 док. и
// 3 658 609 BYN / 24 док.):
//   - выручка = корень CrAcc 90 (РБ/РФ — 90.x), 6010 (КЗ), 9010 (УЗ). ЭТЛ льёт все три
//     корня, страновую привязку 6010/9010 делает отчёт (revenueAccCondition): УЗ-6010 —
//     расчёты с поставщиками, не выручка;
//   - знак Sum: плюс = продажа, минус = возврат; знак входит в ключ (amount_sign),
//     чтобы продажа и возврат одного документа за день не схлопнулись в нетто;
//   - дата события = Wholesales_arh.Date по календарному дню;
//   - ВГО-контур: Chnl или Folder = 'ГРУППА КОМПАНИЙ' (Chnl в продажах обычно пуст).

// arhSalesRoots — корни CrAcc, которые льёт ЭТЛ (значения константные, не ввод).
const arhSalesRoots = "'90', '6010', '9010'"

// factArhSalesRow — строка для insert'а в finance.fact_arh_sales (JSONEachRow).
type factArhSalesRow struct {
	EventDate      string `json:"event_date"`
	CompanyID      string `json:"company_id"`
	Company        string `json:"company"`
	CounterpartyID string `json:"counterparty_id"`
	Counterparty   string `json:"counterparty"`
	Acc            string `json:"acc"`
	AccRoot        string `json:"acc_root"`
	DocID          string `json:"doc_id"`
	DocType        string `json:"doc_type"`
	CurrencyKOD    int32  `json:"currency_kod"`
	AmountSign     int8   `json:"amount_sign"`
	Amount         string `json:"amount"`
	SrcChangedAt   string `json:"src_changed_at"`
}

const arhSalesChg = "ISNULL(w.DateChange, w.[Date])"

// arhSalesCond — общий фильтр строк продаж (ВГО + выручечные корни CrAcc).
func arhSalesCond(orgs []string) string {
	acc := "LTRIM(RTRIM(ISNULL(w.CrAcc, '')))"
	return arhOrgCond("w", orgs) + " AND " + arhVGOSalesCond("w") + " AND " + accRootSQL(acc) + " IN (" + arhSalesRoots + ") AND ISNULL(w.Sum, 0) <> 0"
}

// extractArhSalesSQL — строки продаж/возвратов ВГО за ОДИН день (@dt, @grp). Дубли
// ключа (несколько позиций одного документа) суммируются на extract.
func extractArhSalesSQL(t ArhTables) string {
	org := "LTRIM(RTRIM(ISNULL(w.UNPOrg, '')))"
	unp := "LTRIM(RTRIM(ISNULL(w.UNP, '')))"
	acc := "LTRIM(RTRIM(ISNULL(w.CrAcc, '')))"
	root := accRootSQL(acc)
	docID := "ISNULL(w.DocID, '')"
	cur := "CONVERT(INT, ISNULL(w.Currency, 0))"
	sign := "CONVERT(INT, SIGN(ISNULL(w.Sum, 0)))"
	return `
SELECT
    ` + arhDate("w.[Date]") + `                              AS event_date,
    ` + org + `                                              AS company_id,
    ''                                                       AS company,
    ` + unp + `                                              AS counterparty_id,
    MAX(RTRIM(ISNULL(w.Name, '')))                           AS counterparty,
    ` + acc + `                                              AS acc,
    ` + root + `                                             AS acc_root,
    ` + docID + `                                            AS doc_id,
    MAX(ISNULL(w.DocumentType, ''))                          AS doc_type,
    ` + cur + `                                              AS currency_kod,
    ` + sign + `                                             AS amount_sign,
    CONVERT(VARCHAR(40), SUM(ISNULL(w.Sum, 0)))              AS amount,
    CONVERT(CHAR(19), MAX(` + arhSalesChg + `), 120)         AS src_changed_at
FROM ` + t.WholesalesFQN + ` w WITH (NOLOCK)
WHERE ` + arhDayRange("w.[Date]") + `
  AND ` + arhSalesCond(t.Orgs) + `
GROUP BY ` + arhDate("w.[Date]") + `, ` + org + `, ` + unp + `, ` + acc + `, ` + root + `, ` + docID + `, ` + cur + `, ` + sign
}

// extractArhSalesDatesSQL — отпечатки дней чанка [@from..@to] (MAX(DateChange) по дню);
// фильтр как в основном extract'е, иначе отпечатки разойдутся.
func extractArhSalesDatesSQL(t ArhTables) string {
	return `
SELECT ` + arhDate("w.[Date]") + ` AS d, CONVERT(CHAR(19), MAX(` + arhSalesChg + `), 120) AS changed_at
FROM ` + t.WholesalesFQN + ` w WITH (NOLOCK)
WHERE ` + arhSpanRange("w.[Date]") + `
  AND ` + arhSalesCond(t.Orgs) + `
GROUP BY ` + arhDate("w.[Date]")
}

// scanArhSalesRow — Scan строки extractArhSalesSQL (13 колонок).
func scanArhSalesRow(rows *sql.Rows) (factArhSalesRow, error) {
	var r factArhSalesRow
	var sign int32
	if err := rows.Scan(
		&r.EventDate, &r.CompanyID, &r.Company, &r.CounterpartyID, &r.Counterparty,
		&r.Acc, &r.AccRoot, &r.DocID, &r.DocType, &r.CurrencyKOD, &sign, &r.Amount, &r.SrcChangedAt,
	); err != nil {
		return r, fmt.Errorf("scan arh sales: %w", err)
	}
	r.AmountSign = int8(sign)
	r.Company = strings.TrimSpace(r.Company)
	return r, nil
}
