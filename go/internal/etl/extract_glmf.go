package etl

import (
	"database/sql"
	"fmt"
)

// Extract GLMF → finance.fact_glmf (поток 1). GLMF = Premaster + каноничная
// классификация (CodePL/GroupPL/ICO/Country/имена), наполняется ежедневно
// процедурой GLMF_exec. См. SPEC §2, docs/reports/debt/prod-verification.md.
//
// Источник — БАЗОВАЯ таблица [FinDWH].[dbo].[GLMF], а НЕ view vGLMFAddUSD:
// view даёт USD, но НЕ несёт DateOfLoad (watermark инкремента, T6). Поэтому
// берём GLMF (есть DateOfLoad + всё нужное), а USD откладываем — в отчёте
// валюта = BYN-консолидация (SPEC §10.7, валюта вне скоупа). amt_*_usd пока ''.
//
// Договор в GLMF НЕТ (субконто не несётся) — он во втором потоке dim_contract
// (extract_contract.go), join по doc_id. doc_id = 1С-ссылка `{"#",...}`,
// идентична Premaster.DocID (проверено) → ключ связи потоков.

// glmfRow — строка для CH insert'а (JSONEachRow). Ключи json = колонки fact_glmf.
type glmfRow struct {
	CompanyID            string `json:"company_id"`
	CounterpartyID       string `json:"counterparty_id"`
	DocID                string `json:"doc_id"`
	Num                  int64  `json:"num"`
	Date                 string `json:"date"`
	Month                string `json:"month"`
	DrAcc                string `json:"dr_acc"`
	CrAcc                string `json:"cr_acc"`
	DrAccRoot            string `json:"dr_acc_root"`
	CrAccRoot            string `json:"cr_acc_root"`
	CodePL               string `json:"code_pl"`
	GroupPL              string `json:"group_pl"`
	ICO                  uint8  `json:"ico"`
	Country              string `json:"country"`
	AmtWOVATByn          string `json:"amt_wovat_byn"`
	AmtWithVATByn        string `json:"amt_withvat_byn"`
	AmtWOVATUsd          string `json:"amt_wovat_usd"`
	AmtWithVATUsd        string `json:"amt_withvat_usd"`
	DocName1C            string `json:"doc_name_1c"`
	OperationDescription string `json:"operation_description"`
	DateOfLoad           string `json:"date_of_load"`
}

// extractGLMFSelectFrom — список колонок + FROM. Каллер дописывает WHERE/ORDER BY.
// Страна витрины ('BY'/'RU'/'KZ'/'UZ') маппится в доменную (РБ/РФ/КЗ/УЗ), чтобы
// ClassifyAccount/chart_of_accounts работали как на mssql-пути.
// USD пока не несём (нет в базовой GLMF) → '' (две колонки-плейсхолдера).
func extractGLMFSelectFrom() string {
	country := `CASE LTRIM(RTRIM(p.Country))
	    WHEN 'BY' THEN N'РБ' WHEN 'RU' THEN N'РФ'
	    WHEN 'KZ' THEN N'КЗ' WHEN 'UZ' THEN N'УЗ'
	    ELSE ISNULL(p.Country,'') END`
	// ВСЕ колонки обёрнуты в ISNULL: scan идёт в не-nullable Go-типы (string/uint8),
	// а у GLMF nullable почти всё (Month/DateOfLoad/DrAcc/CrAcc/суммы при NULL-курсе)
	// → любой NULL роняет scan. ICO (bit) кастим в TINYINT (иначе bool→uint8 ошибка).
	return `
SELECT
    ISNULL(p.CompanyID, ''),
    ISNULL(p.CounterpartyID, ''),
    ISNULL(CONVERT(NVARCHAR(MAX), p.DocID), ''),
    ISNULL(p.Num, 0),
    ISNULL(CONVERT(CHAR(10), p.[Date], 23), '1970-01-01'),
    ISNULL(CONVERT(CHAR(10), p.[Month], 23), ISNULL(CONVERT(CHAR(10), p.[Date], 23), '1970-01-01')),
    ISNULL(p.DrAcc, ''), ISNULL(p.CrAcc, ''),
    ISNULL(` + accRootSQL("ISNULL(p.DrAcc,'')") + `, ''),
    ISNULL(` + accRootSQL("ISNULL(p.CrAcc,'')") + `, ''),
    ISNULL(p.CodePL, ''),
    ISNULL(p.GroupPL, ''),
    CONVERT(TINYINT, ISNULL(p.ICO, 0)),
    ` + country + `,
    CONVERT(VARCHAR(40), ISNULL(p.AmountWOVATBelRubFact, 0)),
    CONVERT(VARCHAR(40), ISNULL(p.AmountWithVATBelRubFact, 0)),
    '' , '' ,
    ISNULL(p.DocName1C, ''),
    ISNULL(p.OperationDescription, ''),
    -- DateOfLoad бывает NULL → фолбэк на дату проводки (валидная Date для CH + watermark)
    ISNULL(CONVERT(CHAR(10), p.DateOfLoad, 23), ISNULL(CONVERT(CHAR(10), p.[Date], 23), '1970-01-01'))
FROM [FinDWH].[dbo].[GLMF] AS p WITH (NOLOCK)`
}

// scanGLMFRow — Scan строки extract'а в glmfRow (21 колонка).
func scanGLMFRow(rows *sql.Rows) (glmfRow, error) {
	var r glmfRow
	if err := rows.Scan(
		&r.CompanyID, &r.CounterpartyID, &r.DocID, &r.Num, &r.Date, &r.Month,
		&r.DrAcc, &r.CrAcc, &r.DrAccRoot, &r.CrAccRoot,
		&r.CodePL, &r.GroupPL, &r.ICO, &r.Country,
		&r.AmtWOVATByn, &r.AmtWithVATByn, &r.AmtWOVATUsd, &r.AmtWithVATUsd,
		&r.DocName1C, &r.OperationDescription, &r.DateOfLoad,
	); err != nil {
		return r, fmt.Errorf("scan glmf: %w", err)
	}
	return r, nil
}
