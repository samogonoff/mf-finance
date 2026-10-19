package etl

import (
	"fmt"
	"strings"

	"database/sql"
)

// Поток оборотов FinDebt: оборотные строки [Payments].[report].[FinDebt1]
// (DEBT_FILTER = 'Отображать платежи и отгр.') → finance.fact_findebt_turnover.
//
// В этих строках SUM = 0, а D/K_Wholesales (отгрузки/поступления товара) и
// D/K_Payment (оплаты/поступления д/с) накопительные С НАЧАЛА МЕСЯЦА
// (DATE_F = 1-е число, DATE_T = дата снэпшота). Объём продаж — K_Wholesales (+D) по
// счетам выручки 90.x; отчёт собирает период из месячных накоплений (см.
// findebtTurnoverQuery). Каналы и сотрудников сворачиваем на extract: ВГО-контур
// отбирается Channel = 'ГРУППА КОМПАНИЙ', зерно таблицы — снэпшот × ЮЛ × контрагент ×
// счёт × линза × валюта (= ключ дедупа ReplacingMergeTree).

const finDebtTurnoverFilter = "Отображать платежи и отгр."

type factFinDebtTurnoverRow struct {
	SnapshotDate   string `json:"snapshot_date"`
	DateFrom       string `json:"date_from"`
	CompanyID      string `json:"company_id"`
	Company        string `json:"company"`
	CounterpartyID string `json:"counterparty_id"`
	Counterparty   string `json:"counterparty"`
	Acc            string `json:"acc"`
	AccRoot        string `json:"acc_root"`
	CurFilter      string `json:"cur_filter"`
	Currency       string `json:"currency"`
	DWholesales    string `json:"d_wholesales"`
	KWholesales    string `json:"k_wholesales"`
	DPayment       string `json:"d_payment"`
	KPayment       string `json:"k_payment"`
}

// extractFinDebtTurnoverSQL — оборотные строки FinDebt1 по снэпшотам >= @min.
// Имена берём MAX() (не в ключ), суммы схлопываем сложением по каналам/сотрудникам.
// DATE_F тоже MAX: внутри снэпшота он одинаков для всех строк (начало месяца).
func extractFinDebtTurnoverSQL(t FinDebtTables) string {
	root := accRootSQL("LTRIM(RTRIM(f.Acc))")
	return `
SELECT
    CONVERT(CHAR(10), f.[Date], 23)                       AS snapshot_date,
    CONVERT(CHAR(10), MAX(f.DATE_F), 23)                  AS date_from,
    LTRIM(RTRIM(f.UNPOrg))                                AS company_id,
    MAX(f.Organisation)                                   AS company,
    LTRIM(RTRIM(f.UNP))                                   AS counterparty_id,
    MAX(f.Contragent)                                     AS counterparty,
    LTRIM(RTRIM(f.Acc))                                   AS acc,
    ` + root + `                                          AS acc_root,
    LTRIM(RTRIM(f.CUR_FILTER))                            AS cur_filter,
    LTRIM(RTRIM(ISNULL(f.Currency, '')))                  AS currency,
    CONVERT(VARCHAR(40), SUM(ISNULL(f.D_Wholesales, 0)))  AS d_wholesales,
    CONVERT(VARCHAR(40), SUM(ISNULL(f.K_Wholesales, 0)))  AS k_wholesales,
    CONVERT(VARCHAR(40), SUM(ISNULL(f.D_Payment, 0)))     AS d_payment,
    CONVERT(VARCHAR(40), SUM(ISNULL(f.K_Payment, 0)))     AS k_payment
FROM ` + t.Fin1FQN + ` f WITH (NOLOCK)
WHERE f.Channel = @grp
  AND f.DEBT_FILTER = N'` + strings.ReplaceAll(finDebtTurnoverFilter, "'", "''") + `'
  AND (f.D_Wholesales <> 0 OR f.K_Wholesales <> 0 OR f.D_Payment <> 0 OR f.K_Payment <> 0)
  AND f.[Date] >= @min
GROUP BY CONVERT(CHAR(10), f.[Date], 23), LTRIM(RTRIM(f.UNPOrg)),
         LTRIM(RTRIM(f.UNP)), LTRIM(RTRIM(f.Acc)), ` + root + `,
         LTRIM(RTRIM(f.CUR_FILTER)), LTRIM(RTRIM(ISNULL(f.Currency, '')))`
}

// scanFinDebtTurnoverRow — Scan строки extractFinDebtTurnoverSQL (14 колонок).
func scanFinDebtTurnoverRow(rows *sql.Rows) (factFinDebtTurnoverRow, error) {
	var r factFinDebtTurnoverRow
	if err := rows.Scan(
		&r.SnapshotDate, &r.DateFrom, &r.CompanyID, &r.Company, &r.CounterpartyID, &r.Counterparty,
		&r.Acc, &r.AccRoot, &r.CurFilter, &r.Currency,
		&r.DWholesales, &r.KWholesales, &r.DPayment, &r.KPayment,
	); err != nil {
		return r, fmt.Errorf("scan findebt turnover: %w", err)
	}
	return r, nil
}
