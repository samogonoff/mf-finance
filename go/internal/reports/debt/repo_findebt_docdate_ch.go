package debt

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"
)

// findebtDocDateCHRepo — ВТОРОЙ поток отчёта «Задолженность ВГО»
// (DEBT_BACKEND=findebt-docdate) по уточнённой методике аналитика
// (docs/reports/debt/vgo-doc-date-methodology.md §4-§7).
//
// В отличие от findebt (дефолт, суммы из FinDebt3) этот поток:
//   - суммы и документы берёт из СЫРЫХ таблиц Payments (CH debt_facts/turnover_facts),
//     где DocID уникален; FinDebt3 НЕ источник сумм (Doc_Number не уникален);
//   - BYN/USD пересчитывает САМ по курсу НА ДАТУ ДОКУМЕНТА (DocDate/EventDate) через
//     ASOF JOIN к finance.currency_daily (валюта в источнике — числовой KOD);
//   - сальдо на начало/конец — порогом по DocDate (< начала / <= конца), обороты —
//     из движений turnover_facts за период; агрегация по (UNPOrg, Acc[, контрагент]).
//
// Валюта: Amount_Target = Amount × курс(валюта_договора→BYN) / курс(target→BYN),
// для BYN знаменатель = 1. Курс через BYN как базовую (методика 1С/НБ РБ).
// Знак: сумма >= 0 → дебет (ДЗ), < 0 → кредит (КЗ) по модулю.
type findebtDocDateCHRepo struct {
	*findebtCHRepo
}

// NewFinDebtDocDateCHRepo — конструктор. (nil,nil) при ненастроенном CH.
func NewFinDebtDocDateCHRepo(baseURL, user, password string) (PremasterRepo, error) {
	base, err := NewFinDebtCHRepo(baseURL, user, password)
	if err != nil {
		return nil, err
	}
	if base == nil {
		return nil, nil
	}
	return &findebtDocDateCHRepo{findebtCHRepo: base.(*findebtCHRepo)}, nil
}

// entityNameForINN — имя нашего ЮЛ по УНП из seed (debt_facts не несёт имя ЮЛ,
// «Наименование базы» — статический справочник, ТЗ §8).
func entityNameForINN(inn string) string {
	for _, e := range Entities() {
		if e.INN == strings.TrimSpace(inn) {
			return e.Name
		}
	}
	return ""
}

// filterIn — инлайн IN-условие ` AND col IN (...)` или пусто (значения из seed, не ввод).
func filterIn(col string, vals []string) string {
	if len(vals) == 0 {
		return ""
	}
	return " AND " + col + " IN (" + quoteList(vals) + ")"
}

// docDateConvExpr — CH-выражение пересчёта суммы (col) в линзу lens по ASOF-курсам
// на дату документа: rSrc = валюта договора→BYN, rUsd = USD→BYN. BYN-документ: курс
// к BYN отсутствует → coalesce(...,1). USD: делим на курс USD→BYN (nullIf от /0).
func docDateConvExpr(lens, col string) string {
	switch lens {
	case LensBYN:
		return col + " * coalesce(rSrc.curr_rate, 1)"
	case LensUSD:
		return col + " * coalesce(rSrc.curr_rate, 1) / nullIf(rUsd.curr_rate, 0)"
	default: // LensContract — сумма как есть в валюте договора
		return col
	}
}

// asofRates — общий хвост запроса: два ASOF LEFT JOIN к курсам на дату документа.
// ASOF требует КОЛОНОЧНОГО equi-join, поэтому код USD подаётся колонкой
// usd_kod=toInt32(840), а не константой в ON. Проверено на CH 24.3.
const asofRates = `
    ASOF LEFT JOIN finance.currency_daily rSrc ON rSrc.kod = f.currency_kod AND f.rate_date >= rSrc.date
    ASOF LEFT JOIN finance.currency_daily rUsd ON rUsd.kod = f.usd_kod AND f.rate_date >= rUsd.date`

// debtOpenCloseQuery — сальдо на начало (DocDate < from) и конец (DocDate <= to)
// по (company_id, counterparty_id, acc). Пересчёт валют на DocDate.
func debtOpenCloseQuery(inns, accRoots []string, from, to, lens string) string {
	conv := docDateConvExpr(lens, "amount")
	return fmt.Sprintf(`
WITH toDate('%[1]s') AS pstart, toDate('%[2]s') AS rdate
SELECT company_id, counterparty_id, acc, acc_root,
       any(contragent) AS contragent,
       toString(ifNull(sumIf(amt_t, doc_date <  pstart AND amt_t >= 0), 0))  AS open_dz,
       toString(ifNull(-sumIf(amt_t, doc_date <  pstart AND amt_t <  0), 0)) AS open_kz,
       toString(ifNull(sumIf(amt_t, doc_date <= rdate AND amt_t >= 0), 0))   AS close_dz,
       toString(ifNull(-sumIf(amt_t, doc_date <= rdate AND amt_t <  0), 0))  AS close_kz
FROM (
    SELECT company_id, counterparty_id, acc, acc_root, contragent, doc_date, %[5]s AS amt_t
    FROM (
        SELECT df.company_id AS company_id, df.counterparty_id AS counterparty_id,
               df.acc AS acc, df.acc_root AS acc_root, df.contragent AS contragent,
               df.doc_date AS doc_date, df.rate_date AS rate_date, df.amount AS amount,
               df.currency_kod AS currency_kod, df.usd_kod AS usd_kod
        FROM (
            SELECT *, doc_date AS rate_date, toInt32(840) AS usd_kod
            FROM finance.debt_facts
            WHERE 1=1%[3]s%[4]s
        ) df
    ) f`+asofRates+`
)
GROUP BY company_id, counterparty_id, acc, acc_root
HAVING abs(ifNull(sumIf(amt_t, doc_date <= rdate AND amt_t >= 0), 0)) > 0.005
    OR abs(ifNull(sumIf(amt_t, doc_date <= rdate AND amt_t <  0), 0)) > 0.005
    OR abs(ifNull(sumIf(amt_t, doc_date <  pstart AND amt_t >= 0), 0)) > 0.005
    OR abs(ifNull(sumIf(amt_t, doc_date <  pstart AND amt_t <  0), 0)) > 0.005
ORDER BY company_id, counterparty_id, acc
FORMAT JSONEachRow`, from, to, filterIn("company_id", inns), filterIn("acc_root", accRoots), conv)
}

// turnoverQuery — обороты за период [from,to] и выручка (DocType LIKE 'Реализац%')
// из turnover_facts. Пересчёт валют на EventDate.
func turnoverQuery(inns, accRoots []string, from, to, lens string) string {
	conv := docDateConvExpr(lens, "amount")
	return fmt.Sprintf(`
SELECT company_id, counterparty_id, acc, acc_root,
       toString(ifNull(sumIf(amt_t, amt_t >= 0), 0))  AS turn_dz,
       toString(ifNull(-sumIf(amt_t, amt_t <  0), 0)) AS turn_kz,
       toString(ifNull(sumIf(amt_t, doc_type LIKE 'Реализац%%'), 0)) AS revenue
FROM (
    SELECT company_id, counterparty_id, acc, acc_root, doc_type, %[5]s AS amt_t
    FROM (
        SELECT tf.company_id AS company_id, tf.counterparty_id AS counterparty_id,
               tf.acc AS acc, tf.acc_root AS acc_root, tf.doc_type AS doc_type,
               tf.amount AS amount, tf.currency_kod AS currency_kod,
               tf.event_date AS rate_date, toInt32(840) AS usd_kod
        FROM finance.turnover_facts tf
        WHERE tf.event_date BETWEEN toDate('%[1]s') AND toDate('%[2]s')%[3]s%[4]s
    ) f`+asofRates+`
)
GROUP BY company_id, counterparty_id, acc, acc_root
ORDER BY company_id, counterparty_id, acc
FORMAT JSONEachRow`, from, to, filterIn("company_id", inns), filterIn("acc_root", accRoots), conv)
}

// Report — метод аналитика: сальдо (debt_facts) + обороты (turnover_facts),
// слитые по (company, counterparty, acc). Пересчёт на дату документа.
func (r *findebtDocDateCHRepo) Report(ctx context.Context, f Filters) ([]DebtRow, error) {
	if f.DateTo.IsZero() {
		return nil, errors.New("debt.findebt-docdate.Report: date_to required")
	}
	lens := normLens(f.Lens)
	from, to := asDate(f.DateFrom), asDate(f.DateTo)

	obody, err := r.post(ctx, debtOpenCloseQuery(f.EntityINNs, f.Accounts, from, to, lens))
	if err != nil {
		return nil, err
	}
	tbody, err := r.post(ctx, turnoverQuery(f.EntityINNs, f.Accounts, from, to, lens))
	if err != nil {
		return nil, err
	}

	type agg struct {
		row  DebtRow
		seen bool
	}
	order := []string{}
	byKey := map[string]*agg{}
	keyOf := func(company, cparty, acc string) string {
		return company + "\x1f" + cparty + "\x1f" + acc
	}
	get := func(company, cparty, acc string) *agg {
		k := keyOf(company, cparty, acc)
		a := byKey[k]
		if a == nil {
			country := countryOfINN(company)
			a = &agg{row: DebtRow{
				Country:     country,
				Company:     entityNameForINN(company),
				CompanyINN:  company,
				PartnerINN:  cparty,
				Account:     acc,
				AccountName: accountNameFor(country, AccountRoot(acc)),
				Subaccount:  acc,
				Currency:    lensCurrency(lens, ""),
			}}
			byKey[k] = a
			order = append(order, k)
		}
		return a
	}

	// Сальдо на начало/конец из debt_facts.
	dec := json.NewDecoder(bytes.NewReader(obody))
	for dec.More() {
		var jr struct {
			CompanyID      string `json:"company_id"`
			CounterpartyID string `json:"counterparty_id"`
			Acc            string `json:"acc"`
			Contragent     string `json:"contragent"`
			OpenDZ         string `json:"open_dz"`
			OpenKZ         string `json:"open_kz"`
			CloseDZ        string `json:"close_dz"`
			CloseKZ        string `json:"close_kz"`
		}
		if err := dec.Decode(&jr); err != nil {
			return nil, fmt.Errorf("debt.findebt-docdate.Report: decode saldo: %w", err)
		}
		a := get(jr.CompanyID, jr.CounterpartyID, jr.Acc)
		if a.row.Partner == "" {
			a.row.Partner = strings.TrimSpace(jr.Contragent)
		}
		a.row.OpeningDZ = parseF(jr.OpenDZ)
		a.row.OpeningKZ = parseF(jr.OpenKZ)
		a.row.ClosingDZ = parseF(jr.CloseDZ)
		a.row.ClosingKZ = parseF(jr.CloseKZ)
	}

	// Обороты и выручка из turnover_facts.
	dec = json.NewDecoder(bytes.NewReader(tbody))
	for dec.More() {
		var jr struct {
			CompanyID      string `json:"company_id"`
			CounterpartyID string `json:"counterparty_id"`
			Acc            string `json:"acc"`
			TurnDZ         string `json:"turn_dz"`
			TurnKZ         string `json:"turn_kz"`
			Revenue        string `json:"revenue"`
		}
		if err := dec.Decode(&jr); err != nil {
			return nil, fmt.Errorf("debt.findebt-docdate.Report: decode turnover: %w", err)
		}
		a := get(jr.CompanyID, jr.CounterpartyID, jr.Acc)
		a.row.TurnoverDZ = parseF(jr.TurnDZ)
		a.row.TurnoverKZ = parseF(jr.TurnKZ)
		a.row.RevenuePeriod = parseF(jr.Revenue)
	}

	out := make([]DebtRow, 0, len(order))
	for _, k := range order {
		out = append(out, byKey[k].row)
	}
	return out, nil
}

// docDrilldownQuery — документы (DocID) договора/счёта из debt_facts, пересчитанные
// на DocDate. Флаг is_opening: документ возник до начала периода.
func docDrilldownQuery(companyINN, partnerINN, acc, from, to, lens string) string {
	esc := func(s string) string { return strings.ReplaceAll(s, "'", "''") }
	cpClause := ""
	if strings.TrimSpace(partnerINN) != "" {
		cpClause = " AND counterparty_id = '" + esc(partnerINN) + "'"
	}
	conv := docDateConvExpr(lens, "amount")
	return fmt.Sprintf(`
SELECT doc_id,
       toString(doc_date) AS doc_date,
       doc_description,
       toString(%[6]s)    AS amt_t,
       is_opening
FROM (
    SELECT df.doc_id AS doc_id, df.doc_description AS doc_description, df.doc_date AS doc_date,
           df.rate_date AS rate_date, df.amount AS amount, df.currency_kod AS currency_kod, df.usd_kod AS usd_kod,
           (df.doc_date < toDate('%[4]s')) AS is_opening
    FROM (
        SELECT *, doc_date AS rate_date, toInt32(840) AS usd_kod
        FROM finance.debt_facts
        WHERE company_id = '%[1]s' AND acc = '%[3]s'%[7]s AND doc_date <= toDate('%[5]s')
    ) df
) f`+asofRates+`
ORDER BY doc_id
FORMAT JSONEachRow`, esc(companyINN), "", esc(acc), esc(from), esc(to), conv, cpClause)
}

// Drilldown — документная детализация (DocID) по (company, [counterparty], acc).
func (r *findebtDocDateCHRepo) Drilldown(ctx context.Context, q DrilldownQuery) ([]DocumentRow, error) {
	if q.CompanyINN == "" || q.Account == "" {
		return nil, errors.New("debt.findebt-docdate.Drilldown: company_inn/account required")
	}
	if q.DateTo.IsZero() {
		return nil, errors.New("debt.findebt-docdate.Drilldown: date_to required")
	}
	body, err := r.post(ctx, docDrilldownQuery(
		strings.TrimSpace(q.CompanyINN), strings.TrimSpace(q.PartnerINN),
		strings.TrimSpace(q.Account), asDate(q.DateFrom), asDate(q.DateTo), normLens(q.Lens)))
	if err != nil {
		return nil, err
	}
	out := []DocumentRow{}
	dec := json.NewDecoder(bytes.NewReader(body))
	for dec.More() {
		var jr struct {
			DocID          string `json:"doc_id"`
			DocDate        string `json:"doc_date"`
			DocDescription string `json:"doc_description"`
			AmtT           string `json:"amt_t"`
			IsOpening      int    `json:"is_opening"`
		}
		if err := dec.Decode(&jr); err != nil {
			return nil, fmt.Errorf("debt.findebt-docdate.Drilldown: decode: %w", err)
		}
		amt := parseF(jr.AmtT)
		kind := "Дебиторская"
		if amt < 0 {
			kind = "Кредиторская"
		}
		group := "Внутри периода"
		if jr.IsOpening == 1 {
			group = "На начало"
		}
		dr := DocumentRow{
			DocNumber:   strings.TrimSpace(jr.DocID),
			DocKind:     kind,
			TransGroup:  group,
			Amount:      absF64(amt),
			Description: strings.TrimSpace(jr.DocDescription),
			DZChange:    maxF(amt, 0),
			KZChange:    absF64(minF(amt, 0)),
		}
		if t, err := time.Parse("2006-01-02", jr.DocDate); err == nil {
			dr.DocDate = t
		}
		out = append(out, dr)
	}
	return out, nil
}

func maxF(a, b float64) float64 {
	if a > b {
		return a
	}
	return b
}

func minF(a, b float64) float64 {
	if a < b {
		return a
	}
	return b
}
