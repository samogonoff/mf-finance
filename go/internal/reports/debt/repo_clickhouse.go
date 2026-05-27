package debt

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"
)

// clickhouseRepo — реализация PremasterRepo через локальный CH-снэпшот
// finance.fact_premaster (см. clickhouse-design.md §6, миграция
// clickhouse/migrations/001_init_finance.up.sql).
//
// Использует HTTP-протокол CH через FORMAT JSONEachRow — без внешних
// драйверов в go.mod. Для свёртки достаточно: один POST с SQL, parse JSON.
//
// Drilldown в CH-варианте не реализован (см. clickhouse-design.md §4.2 —
// drill-down оставляем в MSSQL, потому что seek по (DocID, RwNm) уже
// мгновенный в clustered индексе). В compositeRepo Drilldown идёт в MSSQL.
type clickhouseRepo struct {
	httpURL string // http://user:pass@host:port
	client  *http.Client
}

// NewClickHouseRepo строит репо, возвращает (nil, nil) если URL/user пустые
// (сигнал main'у, что CH не настроен — оставляйся на MSSQL).
func NewClickHouseRepo(baseURL, user, password string) (PremasterRepo, error) {
	if baseURL == "" || user == "" {
		return nil, nil
	}
	u, err := url.Parse(baseURL)
	if err != nil {
		return nil, fmt.Errorf("clickhouse: parse URL: %w", err)
	}
	u.User = url.UserPassword(user, password)
	return &clickhouseRepo{
		httpURL: u.String(),
		client:  &http.Client{Timeout: 60 * time.Second},
	}, nil
}

// Report — CH-вариант свёртки. SQL построен по образцу design-doc §6:
// UNION ALL Dr/Cr c sign'ом, потом sumIf по диапазонам дат.
// FINAL гарантирует, что ReplacingMergeTree схлопнул дубли по (doc_id, rw_nm).
func (r *clickhouseRepo) Report(ctx context.Context, f Filters) ([]DebtRow, error) {
	if len(f.EntityINNs) == 0 {
		return nil, errors.New("debt.clickhouse.Report: entity_inns is required")
	}
	if f.DateTo.IsZero() {
		return nil, errors.New("debt.clickhouse.Report: date_to required")
	}

	innsList := make([]string, len(f.EntityINNs))
	for i, v := range f.EntityINNs {
		// CH не имеет параметризованного IN через HTTP API в простом виде —
		// инлайним значения с экранированием '.
		innsList[i] = "'" + strings.ReplaceAll(v, "'", "''") + "'"
	}

	dfrom := asDate(f.DateFrom)
	dto := asDate(f.DateTo)
	dlast := asDate(startOfLastMonth(f.DateTo))

	icoClause := ""
	if f.OnlyICO {
		icoClause = " AND ico = 1"
	}

	q := fmt.Sprintf(`
WITH src AS (
    SELECT company_id, counterparty_id, dr_acc_root AS acc_root,
           amount AS amt, CAST(1 AS Int8) AS sgn, date
    FROM finance.fact_premaster FINAL
    WHERE company_id IN (%[1]s) AND date <= toDate('%[2]s')%[5]s
    UNION ALL
    SELECT company_id, counterparty_id, cr_acc_root AS acc_root,
           amount, CAST(-1 AS Int8) AS sgn, date
    FROM finance.fact_premaster FINAL
    WHERE company_id IN (%[1]s) AND date <= toDate('%[2]s')%[5]s
)
SELECT
    company_id,
    counterparty_id,
    acc_root,
    sumIf(amt * sgn, date <  toDate('%[3]s'))                                AS opening_signed,
    sumIf(amt * sgn, date >= toDate('%[3]s') AND date <= toDate('%[2]s'))    AS turnover_signed,
    sumIf(amt * sgn, date >= toDate('%[4]s') AND date <= toDate('%[2]s'))    AS last_month_signed,
    sum(amt * sgn)                                                            AS closing_signed
FROM src
GROUP BY company_id, counterparty_id, acc_root
HAVING abs(closing_signed) > 0.005
    OR abs(opening_signed)  > 0.005
    OR abs(turnover_signed) > 0.005
FORMAT JSONEachRow`,
		strings.Join(innsList, ","), dto, dfrom, dlast, icoClause)

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, r.httpURL+"/", strings.NewReader(q))
	if err != nil {
		return nil, fmt.Errorf("debt.clickhouse.Report: build req: %w", err)
	}
	req.Header.Set("Content-Type", "text/plain")
	resp, err := r.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("debt.clickhouse.Report: http: %w", err)
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	if resp.StatusCode >= 300 {
		return nil, fmt.Errorf("debt.clickhouse.Report: status %s: %s", resp.Status, string(body))
	}

	raw := make([]rawRow, 0, 256)
	dec := json.NewDecoder(bytes.NewReader(body))
	for dec.More() {
		var jr struct {
			CompanyID       string `json:"company_id"`
			CounterpartyID  string `json:"counterparty_id"`
			AccRoot         string `json:"acc_root"`
			OpeningSigned   string `json:"opening_signed"`
			TurnoverSigned  string `json:"turnover_signed"`
			LastMonthSigned string `json:"last_month_signed"`
			ClosingSigned   string `json:"closing_signed"`
		}
		if err := dec.Decode(&jr); err != nil {
			return nil, fmt.Errorf("debt.clickhouse.Report: decode: %w", err)
		}
		op, _ := strconv.ParseFloat(jr.OpeningSigned, 64)
		tu, _ := strconv.ParseFloat(jr.TurnoverSigned, 64)
		lm, _ := strconv.ParseFloat(jr.LastMonthSigned, 64)
		cl, _ := strconv.ParseFloat(jr.ClosingSigned, 64)

		cp := sql.NullString{}
		if jr.CounterpartyID != "" {
			cp = sql.NullString{String: jr.CounterpartyID, Valid: true}
		}
		raw = append(raw, rawRow{
			CompanyID:       jr.CompanyID,
			CounterpartyID:  cp,
			AccountRoot:     jr.AccRoot,
			OpeningSigned:   op,
			TurnoverSigned:  tu,
			LastMonthSigned: lm,
			ClosingSigned:   cl,
		})
	}

	return BuildReport(raw), nil
}

// Drilldown — пока fallback на MSSQL через compositeRepo (см. ниже).
// Если ходим напрямую clickhouseRepo без обёртки — отдаём 501.
func (r *clickhouseRepo) Drilldown(ctx context.Context, q DrilldownQuery) ([]DocumentRow, error) {
	return nil, errors.New("debt.clickhouse.Drilldown: not implemented (use composite or mssql backend)")
}

// compositeRepo — Report через CH, Drilldown через MSSQL.
// Используется при DEBT_BACKEND=ch, чтобы свёртка шла быстро (CH-снэпшот),
// а детализация документа — точно как сейчас в проде (MSSQL clustered seek).
type compositeRepo struct {
	report    PremasterRepo
	drilldown PremasterRepo
}

func NewCompositeRepo(report, drilldown PremasterRepo) PremasterRepo {
	return &compositeRepo{report: report, drilldown: drilldown}
}

func (c *compositeRepo) Report(ctx context.Context, f Filters) ([]DebtRow, error) {
	return c.report.Report(ctx, f)
}

func (c *compositeRepo) Drilldown(ctx context.Context, q DrilldownQuery) ([]DocumentRow, error) {
	if c.drilldown == nil {
		return nil, errors.New("debt.composite.Drilldown: no drilldown backend configured (set MSSQL_PREMASTER_* for ch-backend drill-down)")
	}
	return c.drilldown.Drilldown(ctx, q)
}

func asDate(t time.Time) string { return t.Format("2006-01-02") }
