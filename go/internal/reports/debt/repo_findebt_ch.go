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
	"strconv"
	"strings"
	"time"
)

// findebtCHRepo — источник отчёта DEBT_BACKEND=findebt из ClickHouse
// finance.fact_findebt (поток FinDebt, залитый ETL из [Payments].[report].
// [FinDebt3]). Числа сверены с 1С копейка-в-копейку (docs/reports/debt/
// findebt-verification.md); свою свёртку не считаем.
//
// Зерно fact_findebt — договор/приложение (doc_number) на дату снэпшота. Отчёт
// группирует до уровня договора (Компания→Контрагент→Счёт→Договор), UI сворачивает
// выше; drill-down раскрывает документы договора со сроком/просрочкой. Данные уже
// ВГО-отфильтрованы и в BYN на этапе ETL.
type findebtCHRepo struct {
	httpURL string
	client  *http.Client
}

// NewFinDebtCHRepo — конструктор; (nil,nil) при пустом URL/user (CH не настроен).
func NewFinDebtCHRepo(baseURL, user, password string) (PremasterRepo, error) {
	if baseURL == "" || user == "" {
		return nil, nil
	}
	u, err := url.Parse(baseURL)
	if err != nil {
		return nil, fmt.Errorf("findebt-ch: parse URL: %w", err)
	}
	u.User = url.UserPassword(user, password)
	return &findebtCHRepo{httpURL: u.String(), client: &http.Client{Timeout: 60 * time.Second}}, nil
}

func (r *findebtCHRepo) post(ctx context.Context, q string) ([]byte, error) {
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
		return nil, fmt.Errorf("findebt-ch: status %s: %s", resp.Status, string(body))
	}
	return body, nil
}

// findebtReportQuery — свод ДЗ/КЗ на уровне ДОГОВОРА = разница снэпшотов
// закрытия/открытия.
//
//	close_dt = max(snapshot_date <= to)   — остаток на конец периода
//	open_dt  = max(snapshot_date < from)  — остаток до начала периода
//
// Опциональные фильтры (инлайн): по ЮЛ (company_id) и корню счёта (acc_root).
// Значения из seed/справочника, не пользовательский ввод. lens (нормализованная,
// закрытый набор) фильтрует линзу CUR_FILTER — ОБЯЗАТЕЛЬНА, иначе три линзы одной
// суммы сложатся (×3). currency — native-валюта строки; в зерне договора она
// единственна, берём any().
func findebtReportQuery(inns, accRoots []string, from, to, lens string) string {
	orgClause := ""
	if len(inns) > 0 {
		orgClause = " AND company_id IN (" + quoteList(inns) + ")"
	}
	accClause := ""
	if len(accRoots) > 0 {
		accClause = " AND acc_root IN (" + quoteList(accRoots) + ")"
	}
	lensClause := " AND cur_filter = '" + strings.ReplaceAll(lens, "'", "''") + "'"
	return fmt.Sprintf(`
WITH
  (SELECT max(snapshot_date) FROM finance.fact_findebt_ccy WHERE snapshot_date <= toDate('%[2]s')) AS close_dt,
  (SELECT max(snapshot_date) FROM finance.fact_findebt_ccy WHERE snapshot_date <  toDate('%[1]s')) AS open_dt
SELECT company_id,
       any(company)         AS company,
       counterparty_id,
       any(counterparty)    AS counterparty,
       acc,
       acc_root,
       doc_number,
       any(description)     AS description,
       any(currency)        AS currency,
       max(delay)           AS delay,
       toString(sumIf(sum_d, snapshot_date = close_dt)) AS close_dz,
       toString(sumIf(sum_k, snapshot_date = close_dt)) AS close_kz,
       toString(sumIf(sum_d, snapshot_date = open_dt))  AS open_dz,
       toString(sumIf(sum_k, snapshot_date = open_dt))  AS open_kz
FROM finance.fact_findebt_ccy FINAL
WHERE snapshot_date IN (close_dt, open_dt)%[5]s%[3]s%[4]s
GROUP BY company_id, counterparty_id, acc, acc_root, doc_number
HAVING abs(sumIf(sum_d, snapshot_date = close_dt)) > 0.005
    OR abs(sumIf(sum_k, snapshot_date = close_dt)) > 0.005
    OR abs(sumIf(sum_d, snapshot_date = open_dt)) > 0.005
    OR abs(sumIf(sum_k, snapshot_date = open_dt)) > 0.005
ORDER BY company, counterparty, acc, doc_number
FORMAT JSONEachRow`, from, to, orgClause, accClause, lensClause)
}

// Report — остатки ДЗ/КЗ по договорам. КЗ во вьюхе отрицательна → в
// «положительный долг» переворачиваем здесь. Договор = doc_number/description,
// отсрочка (PaymentTermDays) = delay. Срок оплаты/просрочка по ТЗ живут на уровне
// документа (drill-down), на уровне договора не заполняются.
func (r *findebtCHRepo) Report(ctx context.Context, f Filters) ([]DebtRow, error) {
	if f.DateTo.IsZero() {
		return nil, errors.New("debt.findebt-ch.Report: date_to required")
	}
	lens := normLens(f.Lens)
	body, err := r.post(ctx, findebtReportQuery(f.EntityINNs, f.Accounts, asDate(f.DateFrom), asDate(f.DateTo), lens))
	if err != nil {
		return nil, err
	}
	out := []DebtRow{}
	dec := json.NewDecoder(bytes.NewReader(body))
	for dec.More() {
		var jr struct {
			CompanyID      string `json:"company_id"`
			Company        string `json:"company"`
			CounterpartyID string `json:"counterparty_id"`
			Counterparty   string `json:"counterparty"`
			Acc            string `json:"acc"`
			AccRoot        string `json:"acc_root"`
			DocNumber      string `json:"doc_number"`
			Description    string `json:"description"`
			Currency       string `json:"currency"`
			Delay          int    `json:"delay"`
			CloseDZ        string `json:"close_dz"`
			CloseKZ        string `json:"close_kz"`
			OpenDZ         string `json:"open_dz"`
			OpenKZ         string `json:"open_kz"`
		}
		if err := dec.Decode(&jr); err != nil {
			return nil, fmt.Errorf("debt.findebt-ch.Report: decode: %w", err)
		}
		closeDZ := parseF(jr.CloseDZ)
		closeKZ := parseF(jr.CloseKZ)
		openDZ := parseF(jr.OpenDZ)
		openKZ := parseF(jr.OpenKZ)
		country := countryOfINN(jr.CompanyID)
		contract := strings.TrimSpace(jr.Description)
		if contract == "" {
			contract = strings.TrimSpace(jr.DocNumber)
		}
		row := DebtRow{
			Country:         country,
			Company:         strings.TrimSpace(jr.Company),
			CompanyINN:      jr.CompanyID,
			Partner:         strings.TrimSpace(jr.Counterparty),
			PartnerINN:      jr.CounterpartyID,
			Account:         jr.Acc,
			AccountName:     accountNameFor(country, jr.AccRoot),
			Subaccount:      jr.Acc,
			Contract:        contract,
			ContractRef:     strings.TrimSpace(jr.DocNumber),
			PaymentTermDays: jr.Delay,
			Currency:        lensCurrency(lens, jr.Currency),
			OpeningDZ:       openDZ,
			OpeningKZ:       -openKZ,
			ClosingDZ:       closeDZ,
			ClosingKZ:       -closeKZ,
			TurnoverDZ:      closeDZ - openDZ,
			TurnoverKZ:      (-closeKZ) - (-openKZ),
		}
		out = append(out, row)
	}
	return out, nil
}

// findebtDrilldownQuery — документы договора (fact_findebt) на снэпшот закрытия
// для (ЮЛ, контрагент, счёт, № договора). Срок оплаты (payment_date) и просрочка
// (day_delay) — по ТЗ показываются именно здесь, на уровне документа.
func findebtDrilldownQuery(companyINN, partnerINN, acc, docNumber, to, lens string) string {
	esc := func(s string) string { return strings.ReplaceAll(s, "'", "''") }
	docClause := ""
	if strings.TrimSpace(docNumber) != "" {
		docClause = " AND doc_number = '" + esc(docNumber) + "'"
	}
	return fmt.Sprintf(`
SELECT doc_number,
       toString(doc_date)                 AS doc_date,
       description,
       ifNull(toString(payment_date), '') AS payment_date,
       day_delay,
       delay,
       toString(sum_d)                    AS sum_d,
       toString(sum_k)                    AS sum_k
FROM finance.fact_findebt_ccy FINAL
WHERE snapshot_date = (SELECT max(snapshot_date) FROM finance.fact_findebt_ccy WHERE snapshot_date <= toDate('%[4]s'))
  AND cur_filter = '%[6]s'
  AND company_id = '%[1]s' AND counterparty_id = '%[2]s' AND acc = '%[3]s'%[5]s
ORDER BY doc_date, doc_number
FORMAT JSONEachRow`, esc(companyINN), esc(partnerINN), esc(acc), esc(to), docClause, esc(lens))
}

// Drilldown — документная детализация договора. Дата оплаты и просрочка приходят
// готовыми из FinDebt (ТЗ: «Дата оплаты по договору», «Задолженность в днях»).
func (r *findebtCHRepo) Drilldown(ctx context.Context, q DrilldownQuery) ([]DocumentRow, error) {
	if q.CompanyINN == "" || q.PartnerINN == "" || q.Account == "" {
		return nil, errors.New("debt.findebt-ch.Drilldown: company_inn/partner_inn/account required")
	}
	if q.DateTo.IsZero() {
		return nil, errors.New("debt.findebt-ch.Drilldown: date_to required")
	}
	body, err := r.post(ctx, findebtDrilldownQuery(
		strings.TrimSpace(q.CompanyINN), strings.TrimSpace(q.PartnerINN),
		strings.TrimSpace(q.Account), strings.TrimSpace(q.Contract), asDate(q.DateTo), normLens(q.Lens)))
	if err != nil {
		return nil, err
	}
	out := []DocumentRow{}
	dec := json.NewDecoder(bytes.NewReader(body))
	for dec.More() {
		var jr struct {
			DocNumber   string `json:"doc_number"`
			DocDate     string `json:"doc_date"`
			Description string `json:"description"`
			PaymentDate string `json:"payment_date"`
			DayDelay    int    `json:"day_delay"`
			Delay       int    `json:"delay"`
			SumD        string `json:"sum_d"`
			SumK        string `json:"sum_k"`
		}
		if err := dec.Decode(&jr); err != nil {
			return nil, fmt.Errorf("debt.findebt-ch.Drilldown: decode: %w", err)
		}
		sumD := parseF(jr.SumD)
		sumK := parseF(jr.SumK)
		kind := "Документ"
		if sumD != 0 {
			kind = "Дебиторская"
		} else if sumK != 0 {
			kind = "Кредиторская"
		}
		desc := strings.TrimSpace(jr.Description)
		dr := DocumentRow{
			DocNumber:   strings.TrimSpace(jr.DocNumber),
			DocKind:     kind,
			TransGroup:  kind,
			Amount:      absF64(sumD) + absF64(sumK),
			Description: desc,
			DZChange:    sumD,
			KZChange:    -sumK,
			OverdueDays: jr.DayDelay,
		}
		if t, err := time.Parse("2006-01-02", jr.DocDate); err == nil {
			dr.DocDate = t
		}
		if jr.PaymentDate != "" {
			if t, err := time.Parse("2006-01-02", jr.PaymentDate); err == nil {
				dr.PaymentDueDate = t
			}
		}
		out = append(out, dr)
	}
	return out, nil
}

// parseF — float из строки CH (пусто/ошибка → 0).
func parseF(s string) float64 {
	v, _ := strconv.ParseFloat(strings.TrimSpace(s), 64)
	return v
}

// absF64 — модуль (локально, чтобы не тащить math).
func absF64(v float64) float64 {
	if v < 0 {
		return -v
	}
	return v
}
