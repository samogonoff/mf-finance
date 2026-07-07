package etl

import (
	"database/sql"
	"fmt"
	"strings"
)

// sqlEscape — экранирование одинарной кавычки для инлайна в CH-SQL (даты/ключи
// из контролируемых источников, не пользовательский ввод).
func sqlEscape(s string) string { return strings.ReplaceAll(s, "'", "''") }

// Поток FinDebt: [Payments].[report].[FinDebt3] (документная детализация ДЗ/КЗ)
// → finance.fact_findebt. Готовый расчётный слой, сверен аналитиком с 1С
// копейка-в-копейку (docs/reports/debt/findebt-verification.md). Не сырые
// проводки, а уже свёрнутый суточный снэпшот на уровне договора/документа.
//
// Extract пре-агрегирует на стороне MSSQL до бизнес-ключа (снэпшот × ЮЛ ×
// контрагент × счёт × № документа × дата документа × линза CUR_FILTER × валюта),
// пиннит ВГО-контур (Folder='ГРУППА КОМПАНИЙ'). Линзу CUR_FILTER БОЛЬШЕ НЕ
// пиннит: тащим все три представления суммы ({В валюте договора, В бел. рублях,
// В долларах США}) как отдельное измерение, отчёт выбирает нужную линзу. Каждая
// задолженность приходит в 3 экземплярах — cur_filter в ключе дедупа не даёт им
// схлопнуться, а любой SUM в отчёте обязан фильтровать одну линзу (иначе ×3).

// FinDebtTables — трёхчастные имена вьюх FinDebt на OLAP (БД Payments, схема report).
// Fin1FQN оставлен для совместимости конфигурации, extract использует только Fin3FQN.
type FinDebtTables struct {
	Fin1FQN string // [Payments].[report].[FinDebt1] (не используется extract'ом)
	Fin3FQN string // [Payments].[report].[FinDebt3] — источник
}

const finDebtVGOFolder = "ГРУППА КОМПАНИЙ"

// factFinDebtRow — строка для CH insert'а в finance.fact_findebt (JSONEachRow).
// PaymentDate — указатель: NULL (срок не заведён) шлём как json null в Nullable(Date).
type factFinDebtRow struct {
	SnapshotDate   string  `json:"snapshot_date"`
	CompanyID      string  `json:"company_id"`
	Company        string  `json:"company"`
	CounterpartyID string  `json:"counterparty_id"`
	Counterparty   string  `json:"counterparty"`
	Acc            string  `json:"acc"`
	AccRoot        string  `json:"acc_root"`
	DocNumber      string  `json:"doc_number"`
	DocDate        string  `json:"doc_date"`
	Description    string  `json:"description"`
	Delay          int32   `json:"delay"`
	PaymentDate    *string `json:"payment_date"`
	DayDelay       int32   `json:"day_delay"`
	CurFilter      string  `json:"cur_filter"`
	Currency       string  `json:"currency"`
	SumD           string  `json:"sum_d"`
	SumK           string  `json:"sum_k"`
}

// extractFinDebtSQL — документная детализация по снэпшотам >= @min.
//
// GROUP BY совпадает С ТОЧНОСТЬЮ до колонок с ключом дедупликации
// ReplacingMergeTree fact_findebt_ccy — (snapshot, company, counterparty, acc,
// doc_number, doc_date, cur_filter, currency). Линза cur_filter и валюта currency
// — измерения (в ключе), а не MAX: три линзы одной задолженности должны остаться
// тремя строками. Имена/срок/просрочку/описание берём MAX() (а не в ключ): иначе
// два документа с одинаковым номером+датой, но разным сроком дали бы две строки
// на один ключ, и ReplacingMergeTree выкинул бы одну — потеря суммы (баг: −33.6М
// вместо −53.2М). Суммы SUM_D/SUM_K схлопываются сложением → итог сохраняется
// даже при коллизии номеров.
func extractFinDebtSQL(t FinDebtTables) string {
	root := accRootSQL("LTRIM(RTRIM(d.Acc))")
	return `
SELECT
    CONVERT(CHAR(10), d.[Date], 23)                       AS snapshot_date,
    LTRIM(RTRIM(d.UNPOrg))                                AS company_id,
    MAX(d.Organisation)                                   AS company,
    LTRIM(RTRIM(d.UNP))                                   AS counterparty_id,
    MAX(d.Contragent)                                     AS counterparty,
    LTRIM(RTRIM(d.Acc))                                   AS acc,
    ` + root + `                                          AS acc_root,
    ISNULL(d.Doc_Number, '')                              AS doc_number,
    ISNULL(CONVERT(CHAR(10), d.Doc_Date, 23), '1970-01-01') AS doc_date,
    MAX(ISNULL(d.Doc_Description, ''))                   AS description,
    MAX(CONVERT(INT, ISNULL(d.Delay, 0)))               AS delay,
    MAX(CONVERT(CHAR(10), d.Payment_Date, 23))           AS payment_date,
    MAX(CONVERT(INT, ISNULL(d.DAY_DELAY, 0)))            AS day_delay,
    LTRIM(RTRIM(d.CUR_FILTER))                            AS cur_filter,
    LTRIM(RTRIM(ISNULL(d.Currency, '')))                 AS currency,
    CONVERT(VARCHAR(40), SUM(ISNULL(d.SUM_D, 0)))        AS sum_d,
    CONVERT(VARCHAR(40), SUM(ISNULL(d.SUM_K, 0)))        AS sum_k
FROM ` + t.Fin3FQN + ` d WITH (NOLOCK)
WHERE d.Folder = @grp
  AND (d.SUM_D <> 0 OR d.SUM_K <> 0)
  AND d.[Date] >= @min
GROUP BY CONVERT(CHAR(10), d.[Date], 23), LTRIM(RTRIM(d.UNPOrg)),
         LTRIM(RTRIM(d.UNP)), LTRIM(RTRIM(d.Acc)), ` + root + `,
         ISNULL(d.Doc_Number, ''),
         ISNULL(CONVERT(CHAR(10), d.Doc_Date, 23), '1970-01-01'),
         LTRIM(RTRIM(d.CUR_FILTER)), LTRIM(RTRIM(ISNULL(d.Currency, '')))`
}

// scanFinDebtRow — Scan строки extractFinDebtSQL (17 колонок).
func scanFinDebtRow(rows *sql.Rows) (factFinDebtRow, error) {
	var r factFinDebtRow
	var payDate sql.NullString
	if err := rows.Scan(
		&r.SnapshotDate, &r.CompanyID, &r.Company, &r.CounterpartyID, &r.Counterparty,
		&r.Acc, &r.AccRoot, &r.DocNumber, &r.DocDate, &r.Description,
		&r.Delay, &payDate, &r.DayDelay, &r.CurFilter, &r.Currency, &r.SumD, &r.SumK,
	); err != nil {
		return r, fmt.Errorf("scan findebt: %w", err)
	}
	if payDate.Valid && payDate.String != "" {
		v := payDate.String
		r.PaymentDate = &v
	}
	return r, nil
}
