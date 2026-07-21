package etl

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"log"
	"regexp"
	"time"
)

// Сырые факты метода аналитика (docs/reports/debt/vgo-doc-date-methodology.md §5):
//   debt_facts     ← Payments.dbo.Debt_arh, дедуп по DocID, DocDate=MIN(Date).
//   turnover_facts ← Payments.dbo.Wholesales_arh, движения по EventDate.
// FinDebt1 — только whitelist пар (UNPOrg, Acc) на ПОСЛЕДНЮЮ дату снэпшота по каналу
// ВГО. Полный reload обеих таблиц (ВГО-контур мал); дедуп — на стороне SQL Server.

// DebtArhTables — FQN сырых таблиц и whitelist-вьюхи. CpartyCol — имя колонки УНП
// контрагента в Debt_arh/Wholesales_arh (параметризуемо: пусто → без УНП в ключе).
type DebtArhTables struct {
	DebtArhFQN    string // [Payments].[dbo].[Debt_arh]
	WholesalesFQN string // [Payments].[dbo].[Wholesales_arh]
	Fin1FQN       string // [Payments].[report].[FinDebt1] — whitelist
	CpartyCol     string // напр. "UNP" (если есть); "" → контрагент только по Name
}

var cpartyColRe = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_]*$`)

// cpartyExpr — SQL-выражение УНП контрагента: MAX(RTRIM(alias.col)) при валидной
// колонке, иначе пустая строка. Валидируем идентификатор (анти-инъекция).
func cpartyExpr(alias, col string) string {
	if col != "" && cpartyColRe.MatchString(col) {
		return "MAX(RTRIM(" + alias + "." + col + "))"
	}
	return "''"
}

type debtFactRow struct {
	DocID          string `json:"doc_id"`
	CompanyID      string `json:"company_id"`
	CounterpartyID string `json:"counterparty_id"`
	Contragent     string `json:"contragent"`
	Acc            string `json:"acc"`
	AccRoot        string `json:"acc_root"`
	DocDescription string `json:"doc_description"`
	CurrencyKOD    int32  `json:"currency_kod"`
	Amount         string `json:"amount"`
	DocDate        string `json:"doc_date"`
}

type turnoverFactRow struct {
	DocID          string `json:"doc_id"`
	CompanyID      string `json:"company_id"`
	CounterpartyID string `json:"counterparty_id"`
	Contragent     string `json:"contragent"`
	Acc            string `json:"acc"`
	AccRoot        string `json:"acc_root"`
	DocType        string `json:"doc_type"`
	CurrencyKOD    int32  `json:"currency_kod"`
	Amount         string `json:"amount"`
	EventDate      string `json:"event_date"`
}

// debtClassCTE — whitelist пар (UNPOrg, Acc) на последнюю дату FinDebt1 по каналу ВГО.
func debtClassCTE(fin1FQN string) string {
	return `;WITH DebtClass AS (
    SELECT DISTINCT LTRIM(RTRIM(UNPOrg)) AS UNPOrg, LTRIM(RTRIM(Acc)) AS Acc
    FROM ` + fin1FQN + `
    WHERE [Date] = (SELECT MAX([Date]) FROM ` + fin1FQN + ` WHERE Channel = @grp)
      AND Channel = @grp
      AND DEBT_FILTER NOT LIKE N'Отображать платежи%'
      AND SUM <> 0
)`
}

func extractDebtArhSQL(t DebtArhTables) string {
	root := accRootSQL("LTRIM(RTRIM(d.Acc))")
	cp := cpartyExpr("d", t.CpartyCol)
	return debtClassCTE(t.Fin1FQN) + `
SELECT
    d.DocID                               AS doc_id,
    LTRIM(RTRIM(d.UNPOrg))                AS company_id,
    ` + cp + `                            AS counterparty_id,
    MAX(RTRIM(d.Name))                    AS contragent,
    LTRIM(RTRIM(d.Acc))                   AS acc,
    ` + root + `                          AS acc_root,
    MAX(ISNULL(d.Description, ''))        AS doc_description,
    CONVERT(INT, d.Currency)              AS currency_kod,
    CONVERT(VARCHAR(40), d.Sum)           AS amount,
    CONVERT(CHAR(10), MIN(d.[Date]), 23)  AS doc_date
FROM ` + t.DebtArhFQN + ` d WITH (NOLOCK)
INNER JOIN DebtClass dc ON LTRIM(RTRIM(d.UNPOrg)) = dc.UNPOrg AND LTRIM(RTRIM(d.Acc)) = dc.Acc
GROUP BY d.DocID, LTRIM(RTRIM(d.UNPOrg)), LTRIM(RTRIM(d.Acc)), ` + root + `,
         CONVERT(INT, d.Currency), d.Sum`
}

func extractWholesalesSQL(t DebtArhTables) string {
	root := accRootSQL("LTRIM(RTRIM(w.DrAcc))")
	cp := cpartyExpr("w", t.CpartyCol)
	return debtClassCTE(t.Fin1FQN) + `
SELECT
    w.DocID                               AS doc_id,
    LTRIM(RTRIM(w.UNPOrg))                AS company_id,
    ` + cp + `                            AS counterparty_id,
    MAX(RTRIM(w.Name))                    AS contragent,
    LTRIM(RTRIM(w.DrAcc))                 AS acc,
    ` + root + `                          AS acc_root,
    MAX(w.DocumentType)                   AS doc_type,
    CONVERT(INT, w.Currency)              AS currency_kod,
    CONVERT(VARCHAR(40), w.Sum)           AS amount,
    CONVERT(CHAR(10), w.[Date], 23)       AS event_date
FROM ` + t.WholesalesFQN + ` w WITH (NOLOCK)
INNER JOIN DebtClass dc ON LTRIM(RTRIM(w.UNPOrg)) = dc.UNPOrg AND LTRIM(RTRIM(w.DrAcc)) = dc.Acc
WHERE w.[Date] >= @min
GROUP BY w.DocID, LTRIM(RTRIM(w.UNPOrg)), LTRIM(RTRIM(w.DrAcc)), ` + root + `,
         CONVERT(INT, w.Currency), w.Sum, CONVERT(CHAR(10), w.[Date], 23)`
}

// RunDebtArhSync — полный reload debt_facts (все открытые документы whitelisted пар)
// и turnover_facts (движения с minDate). Возвращает суммарное число строк.
func RunDebtArhSync(ctx context.Context, deps Deps, t DebtArhTables, minDate string) (int64, error) {
	if t.DebtArhFQN == "" || t.WholesalesFQN == "" || t.Fin1FQN == "" {
		return 0, fmt.Errorf("debtarh-sync: DebtArhFQN/WholesalesFQN/Fin1FQN required")
	}
	if minDate == "" {
		minDate = "2021-01-01"
	}
	ch, err := newCHClient(deps.CHURL, deps.CHUser, deps.CHPass)
	if err != nil {
		return 0, fmt.Errorf("debtarh-sync: ch client: %w", err)
	}
	startedAt := time.Now()

	nd, err := reloadDebtFacts(ctx, deps, ch, t)
	if err != nil {
		return 0, fmt.Errorf("debtarh-sync: debt_facts: %w", err)
	}
	nt, err := reloadTurnoverFacts(ctx, deps, ch, t, minDate)
	if err != nil {
		return nd, fmt.Errorf("debtarh-sync: turnover_facts: %w", err)
	}
	log.Printf("etl: debtarh-sync debt_facts=%d turnover_facts=%d in %.1fs",
		nd, nt, time.Since(startedAt).Seconds())
	return nd + nt, nil
}

func reloadDebtFacts(ctx context.Context, deps Deps, ch *chClient, t DebtArhTables) (int64, error) {
	if err := ch.exec(ctx, "TRUNCATE TABLE IF EXISTS finance.debt_facts"); err != nil {
		return 0, fmt.Errorf("truncate debt_facts: %w", err)
	}
	rows, err := deps.MSSQL.QueryContext(ctx, extractDebtArhSQL(t), sql.Named("grp", finDebtVGOFolder))
	if err != nil {
		return 0, fmt.Errorf("mssql query: %w", err)
	}
	defer rows.Close()

	batch := make([]debtFactRow, 0, 5000)
	var total int64
	flush := func() error {
		if len(batch) == 0 {
			return nil
		}
		var buf bytes.Buffer
		enc := json.NewEncoder(&buf)
		for i := range batch {
			if err := enc.Encode(&batch[i]); err != nil {
				return fmt.Errorf("encode: %w", err)
			}
		}
		if err := ch.insertJSON(ctx, "finance.debt_facts", &buf); err != nil {
			return err
		}
		total += int64(len(batch))
		batch = batch[:0]
		return nil
	}
	for rows.Next() {
		var r debtFactRow
		if err := rows.Scan(&r.DocID, &r.CompanyID, &r.CounterpartyID, &r.Contragent,
			&r.Acc, &r.AccRoot, &r.DocDescription, &r.CurrencyKOD, &r.Amount, &r.DocDate); err != nil {
			return total, fmt.Errorf("scan debt_facts: %w", err)
		}
		batch = append(batch, r)
		if len(batch) >= 5000 {
			if err := flush(); err != nil {
				return total, err
			}
		}
	}
	if err := rows.Err(); err != nil {
		return total, fmt.Errorf("rows: %w", err)
	}
	if err := flush(); err != nil {
		return total, err
	}
	return total, nil
}

func reloadTurnoverFacts(ctx context.Context, deps Deps, ch *chClient, t DebtArhTables, minDate string) (int64, error) {
	if err := ch.exec(ctx, "TRUNCATE TABLE IF EXISTS finance.turnover_facts"); err != nil {
		return 0, fmt.Errorf("truncate turnover_facts: %w", err)
	}
	rows, err := deps.MSSQL.QueryContext(ctx, extractWholesalesSQL(t),
		sql.Named("grp", finDebtVGOFolder), sql.Named("min", minDate))
	if err != nil {
		return 0, fmt.Errorf("mssql query: %w", err)
	}
	defer rows.Close()

	batch := make([]turnoverFactRow, 0, 5000)
	var total int64
	flush := func() error {
		if len(batch) == 0 {
			return nil
		}
		var buf bytes.Buffer
		enc := json.NewEncoder(&buf)
		for i := range batch {
			if err := enc.Encode(&batch[i]); err != nil {
				return fmt.Errorf("encode: %w", err)
			}
		}
		if err := ch.insertJSON(ctx, "finance.turnover_facts", &buf); err != nil {
			return err
		}
		total += int64(len(batch))
		batch = batch[:0]
		return nil
	}
	for rows.Next() {
		var r turnoverFactRow
		if err := rows.Scan(&r.DocID, &r.CompanyID, &r.CounterpartyID, &r.Contragent,
			&r.Acc, &r.AccRoot, &r.DocType, &r.CurrencyKOD, &r.Amount, &r.EventDate); err != nil {
			return total, fmt.Errorf("scan turnover_facts: %w", err)
		}
		batch = append(batch, r)
		if len(batch) >= 5000 {
			if err := flush(); err != nil {
				return total, err
			}
		}
	}
	if err := rows.Err(); err != nil {
		return total, fmt.Errorf("rows: %w", err)
	}
	if err := flush(); err != nil {
		return total, err
	}
	return total, nil
}
