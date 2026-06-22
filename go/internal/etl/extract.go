package etl

import (
	"database/sql"
	"fmt"
)

// Общий extract Premaster1C → fact_premaster для bootstrap и incremental.
// Денормализуем сюда же ДОГОВОР (дебиторка 62): сырую 1С-ссылку субконто, имя
// из Objects и срок/отсрочку из Payments.Docs. ClickHouse не умеет джойнить
// MSSQL, поэтому имя/срок материализуются на этапе заливки — repo_clickhouse
// потом просто отдаёт их в rawRow, а просрочку считает общий BuildReport.
//
// Договор берётся ТОЛЬКО для счёта 62 (субконто #2 той стороны, где стоит 62) —
// та же логика и ограничение, что в MSSQL-пути (repo_premaster.go). Кредиторку
// (60/76) не резолвим (позиция субконто не универсальна) → contract_* пустые.
//
// Пакет etl сознательно не импортирует reports/debt (иначе цикл импорта), поэтому
// SQL-хелперы продублированы здесь (как CountryByINN).

// accRootSQL — корень счёта (LEFT до первой точки).
func accRootSQL(col string) string {
	return "LEFT(" + col + ", CHARINDEX('.', " + col + " + '.') - 1)"
}

// idrrefSQLToGUID — 1С-ссылка `{"#",тип,N:hex}` → чистый GUID (как Docs.ID).
// Реплика штатной UDF Convert_IDRRefToGUID инлайном (без зависимости от функции в БД).
func idrrefSQLToGUID(col string) string {
	h := "LOWER(SUBSTRING(" + col + ",LEN(" + col + ")-32,32))"
	return "(CASE WHEN " + col + " LIKE '{%:%}' AND LEN(" + col + ")>=33 THEN " +
		"SUBSTRING(" + h + ",25,8)+'-'+SUBSTRING(" + h + ",21,4)+'-'+SUBSTRING(" + h + ",17,4)+'-'+SUBSTRING(" + h + ",1,4)+'-'+SUBSTRING(" + h + ",5,12) END)"
}

// extractSelectFrom — список колонок + FROM/JOIN'ы. Каллер дописывает WHERE/ORDER BY.
// cc.ref (CROSS APPLY) — сырая 1С-ссылка договора 62-стороны (NULL, если не 62).
func extractSelectFrom() string {
	cref := "CASE WHEN " + accRootSQL("p.DrAcc") + " = '62' THEN NULLIF(p.DrSubconto2,'')" +
		" WHEN " + accRootSQL("p.CrAcc") + " = '62' THEN NULLIF(p.CrSubconto2,'') END"
	return `
SELECT
    p.CompanyID,
    ISNULL(p.CounterpartyID, ''),
    CONVERT(NVARCHAR(MAX), p.DocID, 1),
    p.RwNm,
    CONVERT(CHAR(10), p.[Date], 23),
    p.DrAcc, p.CrAcc,
    ` + accRootSQL("p.DrAcc") + `,
    ` + accRootSQL("p.CrAcc") + `,
    CONVERT(VARCHAR(40), p.AmountWithVATCurrency),
    ISNULL(p.ICO, 0),
    ISNULL(o.[Name], ''),
    ISNULL(p.Mapping, ''),
    ISNULL(p.TransDescription, ''),
    ISNULL(p.OperationDescription, ''),
    CONVERT(VARCHAR(19), ISNULL(p.DateOfChange, p.[Date]), 120),
    ISNULL(cc.ref, ''),
    ISNULL(oc.[Name], ''),
    ISNULL(CONVERT(VARCHAR(10), dc.Delay), ''),
    ISNULL(CONVERT(CHAR(10), dc.[Date], 23), ''),
    ISNULL(CONVERT(CHAR(10), dc.PaymentDate, 23), '')
FROM [FinDWH].[dbo].[Premaster1C] AS p WITH (NOLOCK)
LEFT JOIN [FinDWH].[dbo].[Objects] AS o WITH (NOLOCK) ON o.ID = p.DocID
CROSS APPLY (SELECT ` + cref + ` AS ref) cc
LEFT JOIN [FinDWH].[dbo].[Objects] AS oc WITH (NOLOCK) ON oc.ID = cc.ref
LEFT JOIN [Payments].[dbo].[Docs] AS dc WITH (NOLOCK) ON dc.ID = ` + idrrefSQLToGUID("cc.ref") + ` COLLATE DATABASE_DEFAULT`
}

// scanExtractRow — единый Scan строки extract'а в row (21 колонка).
func scanExtractRow(rows *sql.Rows, country string) (row, error) {
	var r row
	if err := rows.Scan(
		&r.CompanyID, &r.CounterpartyID, &r.DocID, &r.RwNm, &r.Date,
		&r.DrAcc, &r.CrAcc, &r.DrAccRoot, &r.CrAccRoot,
		&r.Amount, &r.ICO,
		&r.DocName1C, &r.Mapping, &r.TransDescription, &r.OperationDescription,
		&r.DateOfChange,
		&r.ContractRef, &r.ContractName, &r.ContractDelay, &r.ContractDocDate, &r.ContractPayDate,
	); err != nil {
		return r, fmt.Errorf("scan: %w", err)
	}
	r.Country = country
	return r, nil
}
