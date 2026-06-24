package debt

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"sort"
	"strconv"
	"strings"
	"time"
)

// glmfCHRepo — источник отчёта из ClickHouse finance.fact_glmf (поток GLMF).
// Считает выручку по точным Дт/Кт-корреспонденциям ТЗ (glmf_revenue.go) и
// ДЗ/КЗ-сальдо до субсчёта; договор подмешивает dim_contract (T5). Данные в
// fact_glmf уже ВГО-отфильтрованы на этапе ETL → повторный ВГО-фильтр не нужен.
type glmfCHRepo struct {
	httpURL string
	client  *http.Client
}

// NewGLMFCHRepo — конструктор; (nil,nil) при пустом URL/user (CH не настроен).
func NewGLMFCHRepo(baseURL, user, password string) (PremasterRepo, error) {
	if baseURL == "" || user == "" {
		return nil, nil
	}
	u, err := url.Parse(baseURL)
	if err != nil {
		return nil, fmt.Errorf("glmf-ch: parse URL: %w", err)
	}
	u.User = url.UserPassword(user, password)
	return &glmfCHRepo{httpURL: u.String(), client: &http.Client{Timeout: 60 * time.Second}}, nil
}

// debtAccountRoots — корни ДЗ/КЗ-счетов из chart (все страны), для фильтра сальдо.
// Выручочные счета (90/6010/9010) сюда НЕ попадают.
func debtAccountRoots() []string {
	seen := map[string]bool{}
	for _, chart := range activeChart {
		for root, kind := range chart {
			if kind == KindDZ || kind == KindKZ {
				seen[root] = true
			}
		}
	}
	out := make([]string, 0, len(seen))
	for r := range seen {
		out = append(out, r)
	}
	sort.Strings(out)
	return out
}

func quoteList(vals []string) string {
	q := make([]string, len(vals))
	for i, v := range vals {
		q[i] = "'" + strings.ReplaceAll(v, "'", "''") + "'"
	}
	return strings.Join(q, ",")
}

// glmfSaldoQuery — signed-сальдо ДЗ/КЗ по (company, counterparty, субсчёт).
// amt_withvat_byn (долг с НДС). opening (date<from), turnover (from..to), closing(<=to).
func glmfSaldoQuery(inns []string, from, to string) string {
	in := quoteList(inns)
	roots := quoteList(debtAccountRoots())
	return fmt.Sprintf(`
WITH src AS (
    SELECT company_id, counterparty_id, dr_acc AS acc, dr_acc_root AS root,
           amt_withvat_byn AS amt, CAST(1 AS Int8) AS sgn, date
    FROM finance.fact_glmf FINAL
    WHERE company_id IN (%[1]s) AND date <= toDate('%[3]s') AND dr_acc_root IN (%[4]s)
    UNION ALL
    SELECT company_id, counterparty_id, cr_acc AS acc, cr_acc_root AS root,
           amt_withvat_byn, CAST(-1 AS Int8) AS sgn, date
    FROM finance.fact_glmf FINAL
    WHERE company_id IN (%[1]s) AND date <= toDate('%[3]s') AND cr_acc_root IN (%[4]s)
)
SELECT company_id, counterparty_id, acc, any(root) AS root,
    toString(sumIf(amt * sgn, date <  toDate('%[2]s')))                              AS opening,
    toString(sumIf(amt * sgn, date >= toDate('%[2]s') AND date <= toDate('%[3]s')))  AS turnover,
    toString(sum(amt * sgn))                                                         AS closing
FROM src
GROUP BY company_id, counterparty_id, acc
HAVING abs(sum(amt * sgn)) > 0.005
    OR abs(sumIf(amt * sgn, date <  toDate('%[2]s'))) > 0.005
    OR abs(sumIf(amt * sgn, date >= toDate('%[2]s') AND date <= toDate('%[3]s'))) > 0.005
FORMAT JSONEachRow`, in, from, to, roots)
}

// glmfRevenueQuery — выручка по корреспонденциям ТЗ (revenueCHClause), per пара.
// rev — за период, rev_last — за последний месяц (lastFrom..to). amt_wovat_byn (без НДС).
func glmfRevenueQuery(inns []string, from, to, lastFrom string) string {
	in := quoteList(inns)
	return fmt.Sprintf(`
SELECT company_id, counterparty_id,
    toString(sumIf(amt_wovat_byn, date >= toDate('%[2]s') AND date <= toDate('%[3]s'))) AS rev,
    toString(sumIf(amt_wovat_byn, date >= toDate('%[4]s') AND date <= toDate('%[3]s'))) AS rev_last
FROM finance.fact_glmf FINAL
WHERE company_id IN (%[1]s)
  AND date >= toDate('%[2]s') AND date <= toDate('%[3]s')
  AND %[5]s
GROUP BY company_id, counterparty_id
HAVING abs(sumIf(amt_wovat_byn, date >= toDate('%[2]s') AND date <= toDate('%[3]s'))) > 0.005
FORMAT JSONEachRow`, in, from, to, lastFrom, revenueCHClause())
}

func (r *glmfCHRepo) post(ctx context.Context, q string) ([]byte, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, r.httpURL+"/", strings.NewReader(q))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "text/plain")
	resp, err := r.client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	if resp.StatusCode >= 300 {
		return nil, fmt.Errorf("glmf-ch: status %s: %s", resp.Status, string(body))
	}
	return body, nil
}

// Report — выручка (точные Дт/Кт) + ДЗ/КЗ-сальдо (субсчёт) из fact_glmf.
func (r *glmfCHRepo) Report(ctx context.Context, f Filters) ([]DebtRow, error) {
	if len(f.EntityINNs) == 0 {
		return nil, errors.New("debt.glmf-ch.Report: entity_inns required")
	}
	if f.DateTo.IsZero() {
		return nil, errors.New("debt.glmf-ch.Report: date_to required")
	}
	from := asDate(f.DateFrom)
	to := asDate(f.DateTo)
	lastFrom := asDate(startOfLastMonth(f.DateTo))

	companyByINN := indexCompaniesByINN()
	partnerByINN := indexPartnersByINN()

	out := []DebtRow{}

	// 1. ДЗ/КЗ-сальдо по субсчетам.
	saldoBody, err := r.post(ctx, glmfSaldoQuery(f.EntityINNs, from, to))
	if err != nil {
		return nil, err
	}
	dec := json.NewDecoder(bytes.NewReader(saldoBody))
	for dec.More() {
		var jr struct {
			CompanyID      string `json:"company_id"`
			CounterpartyID string `json:"counterparty_id"`
			Acc            string `json:"acc"`
			Root           string `json:"root"`
			Opening        string `json:"opening"`
			Turnover       string `json:"turnover"`
			Closing        string `json:"closing"`
		}
		if err := dec.Decode(&jr); err != nil {
			return nil, fmt.Errorf("debt.glmf-ch.Report: decode saldo: %w", err)
		}
		ent, ok := companyByINN[jr.CompanyID]
		if !ok {
			continue
		}
		kind := ClassifyAccount(ent.Country, jr.Root)
		if kind != KindDZ && kind != KindKZ {
			continue
		}
		op, _ := strconv.ParseFloat(jr.Opening, 64)
		tu, _ := strconv.ParseFloat(jr.Turnover, 64)
		cl, _ := strconv.ParseFloat(jr.Closing, 64)
		row := DebtRow{
			Country: ent.Country, Company: ent.Name, CompanyINN: jr.CompanyID,
			PartnerINN: jr.CounterpartyID, Currency: "BYN",
			Account: jr.Root, AccountName: accountNameFor(ent.Country, jr.Root),
			Subaccount: jr.Acc, SubaccountName: accountNameFor(ent.Country, jr.Root),
		}
		if p := partnerByINN[jr.CounterpartyID]; p != "" {
			row.Partner = p
		} else {
			row.Partner = jr.CounterpartyID
		}
		switch kind {
		case KindDZ:
			row.OpeningDZ, row.TurnoverDZ, row.ClosingDZ = op, tu, cl
		case KindKZ:
			row.OpeningKZ, row.TurnoverKZ, row.ClosingKZ = -op, -tu, -cl
		}
		out = append(out, row)
	}

	// 2. Выручка по парам (отдельные строки; UI группирует по Компания→Партнёр).
	revBody, err := r.post(ctx, glmfRevenueQuery(f.EntityINNs, from, to, lastFrom))
	if err != nil {
		return nil, err
	}
	dec = json.NewDecoder(bytes.NewReader(revBody))
	for dec.More() {
		var jr struct {
			CompanyID      string `json:"company_id"`
			CounterpartyID string `json:"counterparty_id"`
			Rev            string `json:"rev"`
			RevLast        string `json:"rev_last"`
		}
		if err := dec.Decode(&jr); err != nil {
			return nil, fmt.Errorf("debt.glmf-ch.Report: decode revenue: %w", err)
		}
		ent, ok := companyByINN[jr.CompanyID]
		if !ok {
			continue
		}
		rev, _ := strconv.ParseFloat(jr.Rev, 64)
		revLast, _ := strconv.ParseFloat(jr.RevLast, 64)
		row := DebtRow{
			Country: ent.Country, Company: ent.Name, CompanyINN: jr.CompanyID,
			PartnerINN: jr.CounterpartyID, Currency: "BYN",
			RevenuePeriod: rev, RevenueLastMonth: revLast,
		}
		if p := partnerByINN[jr.CounterpartyID]; p != "" {
			row.Partner = p
		} else {
			row.Partner = jr.CounterpartyID
		}
		out = append(out, row)
	}

	return out, nil
}

// Drilldown — месячная PL-детализация из fact_glmf (T5). Пока заглушка.
func (r *glmfCHRepo) Drilldown(ctx context.Context, q DrilldownQuery) ([]DocumentRow, error) {
	return nil, errors.New("debt.glmf-ch.Drilldown: not implemented (T5)")
}
