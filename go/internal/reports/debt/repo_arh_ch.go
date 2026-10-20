package debt

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"strings"
	"time"
)

// arhCHRepo — источник отчёта DEBT_BACKEND=arh: остатки и продажи собраны из СЫРЫХ
// таблиц OLAP (Debt_arh + Docs, Wholesales_arh), залитых ETL в ClickHouse
// finance.fact_arh_debt / finance.fact_arh_sales (миграция 015, правила — в
// docs/reports/debt/arh-runbook.md). Готовые вьюхи FinDebt1/3 не используются.
//
// Отличия от findebt:
//   - линзы BYN/USD считает САМ отчёт: сумма в валюте учёта строки × курс из
//     finance.currency_daily (ASOF) НА ДАТУ СНЭПШОТА (остатки) или ДАТУ СОБЫТИЯ
//     (продажи); BYN (933) — курс 1, USD = BYN / курс USD (840). Линза «В валюте
//     договора» (дефолт) — родная сумма строки с подписью валюты по ISO-коду;
//   - зерно свода — договор (Docs.ParentID; нет родителя — сам документ);
//   - продажи отдают и возвраты (отрицательные строки Wholesales_arh) отдельными полями.
type arhCHRepo struct {
	*findebtCHRepo
}

// NewArhCHRepo — конструктор; (nil,nil) при ненастроенном CH.
func NewArhCHRepo(baseURL, user, password string) (PremasterRepo, error) {
	base, err := NewFinDebtCHRepo(baseURL, user, password)
	if err != nil || base == nil {
		return nil, err
	}
	return &arhCHRepo{findebtCHRepo: base.(*findebtCHRepo)}, nil
}

const arhNoDate = "1970-01-01" // Date по умолчанию в CH: «снэпшота нет»

// arhAmtCols — суммы fact_arh_debt: алиас внутри запроса → колонка таблицы.
var arhAmtCols = [][2]string{
	{"v_d", "sum_d"}, {"v_k", "sum_k"},
	{"v30_d", "sum30_d"}, {"v60_d", "sum60_d"}, {"v90_d", "sum90_d"}, {"v999_d", "sum999_d"},
	{"v30_k", "sum30_k"}, {"v60_k", "sum60_k"}, {"v90_k", "sum90_k"}, {"v999_k", "sum999_k"},
}

// arhRateK — множитель пересчёта суммы строки в линзу по ASOF-курсам (алиасы f/rSrc/rUsd,
// см. arhRates). BYN-строка не ищет курс (курс 1). Нет курса → 0 (сумма обнулится, а не
// пересчитается «как есть»). USD: деление на курс USD с защитой от нуля (NULL → не в сумме).
func arhRateK(lens string) string {
	toBYN := "multiIf(f.currency_kod = 933, toFloat64(1), toFloat64(rSrc.curr_rate))"
	if lens == LensUSD {
		return toBYN + " / nullIf(toFloat64(rUsd.curr_rate), 0)"
	}
	return toBYN
}

// arhRates — ASOF-подключение курсов к зерну f (колонки currency_kod, rate_date, usd_kod).
// Переиспользует asofRates второго потока; ASOF требует колоночного equi-join, поэтому
// код USD передаётся колонкой usd_kod.
const arhRates = asofRates

// arhLabelExpr — подпись договора построчно: название/номер договора-родителя, иначе
// номер документа, иначе тип документа; пусто — нет документа (UI покажет «(без договора)»).
const arhLabelExpr = `multiIf(contract_id != '' AND contract_title != '', contract_title, contract_id != '' AND contract_number != '', contract_number, doc_number != '', doc_number, doc_type)`

// arhSnapshotDates — close_dt (последний снэпшот ≤ to) и open_dt (последний < from).
// Пустая строка — такого снэпшота нет.
func arhSnapshotDatesQuery(from, to string) string {
	return fmt.Sprintf(`SELECT toString(maxIf(snapshot_date, snapshot_date <= toDate('%[2]s'))),
       toString(maxIf(snapshot_date, snapshot_date < toDate('%[1]s')))
FROM finance.fact_arh_debt WHERE snapshot_date <= toDate('%[2]s') FORMAT TSV`, from, to)
}

func (r *arhCHRepo) snapshotDates(ctx context.Context, from, to string) (closeDt, openDt string, err error) {
	body, err := r.post(ctx, arhSnapshotDatesQuery(from, to))
	if err != nil {
		return "", "", err
	}
	parts := strings.Split(strings.TrimSpace(string(body)), "\t")
	if len(parts) != 2 {
		return "", "", fmt.Errorf("arh: даты снэпшотов: неожиданный ответ %q", string(body))
	}
	norm := func(s string) string {
		if s = strings.TrimSpace(s); s == arhNoDate {
			return ""
		}
		return s
	}
	return norm(parts[0]), norm(parts[1]), nil
}

// arhReportQuery — свод ДЗ/КЗ на уровне договора: остатки на close_dt/open_dt, корзины
// просрочки на close_dt. Для линз BYN/USD суммы пересчитываются на дату снэпшота и
// схлопываются по всем валютам (cur_kod = 0); для «В валюте договора» валюта — измерение.
// Все значения в SQL — даты из CH, УНП/счета из seed (не пользовательский ввод).
func arhReportQuery(inns, accRoots []string, closeDt, openDt, lens string) string {
	if openDt == "" {
		openDt = arhNoDate
	}
	orgClause, accClause := "", ""
	if len(inns) > 0 {
		orgClause = " AND company_id IN (" + quoteList(inns) + ")"
	}
	if len(accRoots) > 0 {
		accClause = " AND acc_root IN (" + quoteList(accRoots) + ")"
	}
	// grain — свёртка до (снэпшот, ЮЛ, контрагент, счёт, договор, валюта).
	var gb strings.Builder
	for _, c := range arhAmtCols {
		gb.WriteString(", sum(" + c[1] + ") AS " + c[0])
	}
	grain := `SELECT snapshot_date, company_id, any(company) AS company, counterparty_id, any(counterparty) AS counterparty,
           acc, acc_root, if(contract_id != '', contract_id, doc_id) AS contract_key,
           anyIf(` + arhLabelExpr + `, ` + arhLabelExpr + ` != '') AS contract_label,
           max(delay) AS delay, currency_kod` + gb.String() + `,
           snapshot_date AS rate_date, toInt32(840) AS usd_kod
    FROM finance.fact_arh_debt FINAL
    WHERE snapshot_date IN (toDate('` + closeDt + `'), toDate('` + openDt + `'))` + orgClause + accClause + `
    GROUP BY snapshot_date, company_id, counterparty_id, acc, acc_root, contract_key, currency_kod`

	var inner string
	if lens == LensContract {
		var sb strings.Builder
		for _, c := range arhAmtCols {
			sb.WriteString(", " + c[0] + " AS s" + c[0][1:])
		}
		inner = `SELECT snapshot_date, company_id, company, counterparty_id, counterparty, acc, acc_root,
           contract_key, contract_label, delay, currency_kod AS cur_kod` + sb.String() + `
    FROM (` + grain + `)`
	} else {
		k := arhRateK(lens)
		var sb strings.Builder
		for _, c := range arhAmtCols {
			sb.WriteString(", toFloat64(f." + c[0] + ") * (" + k + ") AS s" + c[0][1:])
		}
		inner = `SELECT f.snapshot_date AS snapshot_date, f.company_id AS company_id, f.company AS company,
           f.counterparty_id AS counterparty_id, f.counterparty AS counterparty, f.acc AS acc, f.acc_root AS acc_root,
           f.contract_key AS contract_key, f.contract_label AS contract_label, f.delay AS delay, toUInt32(0) AS cur_kod` + sb.String() + `
    FROM (` + grain + `) f` + arhRates
	}
	at := func(col, dt string) string {
		return fmt.Sprintf("toString(round(sumIf(%s, snapshot_date = toDate('%s')), 2))", col, dt)
	}
	return `
SELECT company_id,
       any(company)      AS company,
       counterparty_id,
       any(counterparty) AS counterparty,
       acc,
       acc_root,
       contract_key,
       anyIf(contract_label, contract_label != '') AS contract_label,
       max(delay)        AS delay,
       cur_kod,
       ` + at("s_d", closeDt) + ` AS close_dz,
       ` + at("s_k", closeDt) + ` AS close_kz,
       ` + at("s_d", openDt) + ` AS open_dz,
       ` + at("s_k", openDt) + ` AS open_kz,
       ` + at("s30_d", closeDt) + ` AS dz30,
       ` + at("s60_d", closeDt) + ` AS dz60,
       ` + at("s90_d", closeDt) + ` AS dz90,
       ` + at("s999_d", closeDt) + ` AS dz999,
       ` + at("s30_k", closeDt) + ` AS kz30,
       ` + at("s60_k", closeDt) + ` AS kz60,
       ` + at("s90_k", closeDt) + ` AS kz90,
       ` + at("s999_k", closeDt) + ` AS kz999
FROM (
    ` + inner + `
)
GROUP BY company_id, counterparty_id, acc, acc_root, contract_key, cur_kod
HAVING abs(sumIf(s_d, snapshot_date = toDate('` + closeDt + `'))) > 0.005
    OR abs(sumIf(s_k, snapshot_date = toDate('` + closeDt + `'))) > 0.005
    OR abs(sumIf(s_d, snapshot_date = toDate('` + openDt + `'))) > 0.005
    OR abs(sumIf(s_k, snapshot_date = toDate('` + openDt + `'))) > 0.005
ORDER BY company, counterparty, acc, contract_label
FORMAT JSONEachRow`
}

// arhSalesQuery — продажи и возвраты ВГО из fact_arh_sales по дню события.
//
//	period     = Σ по дням [from..to]
//	last_month = календарный месяц, предшествующий месяцу `to`
//	продажи    = Σ положительных строк, возвраты = Σ отрицательных по модулю
//
// Счета выручки — revenueAccCondition() (90.x / КЗ-6010 / УЗ-9010; УЗ-6010 — расчёты с
// поставщиками). Фильтр счетов отчёта на продажи НЕ действует; фильтр по ЮЛ и линза — да.
// BYN/USD — по курсу на дату события.
func arhSalesQuery(inns []string, from, to, lens string) string {
	orgClause := ""
	if len(inns) > 0 {
		orgClause = " AND company_id IN (" + quoteList(inns) + ")"
	}
	period := fmt.Sprintf("event_date >= toDate('%[1]s') AND event_date <= toDate('%[2]s')", from, to)
	last := fmt.Sprintf("event_date >= addMonths(toStartOfMonth(toDate('%[1]s')), -1) AND event_date < toStartOfMonth(toDate('%[1]s'))", to)
	grain := `SELECT event_date, company_id, any(company) AS company, counterparty_id, any(counterparty) AS counterparty,
           acc, currency_kod, amount_sign, sum(amount) AS v,
           event_date AS rate_date, toInt32(840) AS usd_kod
    FROM finance.fact_arh_sales FINAL
    WHERE (` + revenueAccCondition() + `)
      AND ((` + period + `) OR (` + last + `))` + orgClause + `
    GROUP BY event_date, company_id, counterparty_id, acc, currency_kod, amount_sign`
	var inner string
	if lens == LensContract {
		inner = `SELECT event_date, company_id, company, counterparty_id, counterparty, acc, currency_kod AS cur_kod, amount_sign, v AS s
    FROM (` + grain + `)`
	} else {
		inner = `SELECT f.event_date AS event_date, f.company_id AS company_id, f.company AS company,
           f.counterparty_id AS counterparty_id, f.counterparty AS counterparty, f.acc AS acc,
           toUInt32(0) AS cur_kod, f.amount_sign AS amount_sign, toFloat64(f.v) * (` + arhRateK(lens) + `) AS s
    FROM (` + grain + `) f` + arhRates
	}
	sum := func(cond, sign string) string { return sign + "sumIf(s, " + cond + ")" }
	sp := sum(period+" AND amount_sign > 0", "")
	sl := sum(last+" AND amount_sign > 0", "")
	rp := sum(period+" AND amount_sign < 0", "-")
	rl := sum(last+" AND amount_sign < 0", "-")
	return `
SELECT company_id,
       any(company)      AS company,
       counterparty_id,
       any(counterparty) AS counterparty,
       acc,
       cur_kod,
       toString(round(` + sp + `, 2)) AS revenue_period,
       toString(round(` + sl + `, 2)) AS revenue_last_month,
       toString(round(` + rp + `, 2)) AS returns_period,
       toString(round(` + rl + `, 2)) AS returns_last_month
FROM (
    ` + inner + `
)
GROUP BY company_id, counterparty_id, acc, cur_kod
HAVING abs(` + sp + `) > 0.005 OR abs(` + sl + `) > 0.005 OR abs(` + rp + `) > 0.005 OR abs(` + rl + `) > 0.005
ORDER BY company, counterparty, acc
FORMAT JSONEachRow`
}

// Report — остатки ДЗ/КЗ по договорам + строки продаж/возвратов (без договора).
func (r *arhCHRepo) Report(ctx context.Context, f Filters) ([]DebtRow, error) {
	if f.DateTo.IsZero() {
		return nil, errors.New("debt.arh.Report: date_to required")
	}
	lens := normLens(f.Lens)
	from, to := asDate(f.DateFrom), asDate(f.DateTo)
	if f.DateFrom.IsZero() {
		from = to
	}
	closeDt, openDt, err := r.snapshotDates(ctx, from, to)
	if err != nil {
		return nil, err
	}
	rows := []DebtRow{}
	if closeDt != "" {
		body, err := r.post(ctx, arhReportQuery(f.EntityINNs, f.Accounts, closeDt, openDt, lens))
		if err != nil {
			return nil, err
		}
		if rows, err = decodeArhReport(body, lens); err != nil {
			return nil, err
		}
	}
	// Продажи — отдельная таблица: нет миграции 015 / поток не залит → отчёт по долгу
	// остаётся рабочим, только без продаж и возвратов (причина — в логе).
	sales, err := r.salesRows(ctx, f, from, to, lens)
	if err != nil {
		log.Printf("debt.arh: продажи недоступны (fact_arh_sales): %v", err)
		return rows, nil
	}
	return append(rows, sales...), nil
}

func (r *arhCHRepo) salesRows(ctx context.Context, f Filters, from, to, lens string) ([]DebtRow, error) {
	body, err := r.post(ctx, arhSalesQuery(f.EntityINNs, from, to, lens))
	if err != nil {
		return nil, err
	}
	return decodeArhSales(body, lens)
}

// arhCompanyName — имя ЮЛ: в сырых таблицах его нет, берём из seed.
func arhCompanyName(inn, fromSource string) string {
	if s := strings.TrimSpace(fromSource); s != "" {
		return s
	}
	return entityNameForINN(inn)
}

// arhCurrency — подпись валюты строки: для линз BYN/USD фиксированная, для «В валюте
// договора» — по числовому ISO-коду.
func arhCurrency(lens string, kod int) string {
	if lens == LensContract {
		return isoCurrencyName(kod)
	}
	return lensCurrency(lens, "")
}

func decodeArhReport(body []byte, lens string) ([]DebtRow, error) {
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
			ContractKey    string `json:"contract_key"`
			ContractLabel  string `json:"contract_label"`
			Delay          int    `json:"delay"`
			CurKod         int    `json:"cur_kod"`
			CloseDZ        string `json:"close_dz"`
			CloseKZ        string `json:"close_kz"`
			OpenDZ         string `json:"open_dz"`
			OpenKZ         string `json:"open_kz"`
			DZ30           string `json:"dz30"`
			DZ60           string `json:"dz60"`
			DZ90           string `json:"dz90"`
			DZ999          string `json:"dz999"`
			KZ30           string `json:"kz30"`
			KZ60           string `json:"kz60"`
			KZ90           string `json:"kz90"`
			KZ999          string `json:"kz999"`
		}
		if err := dec.Decode(&jr); err != nil {
			return nil, fmt.Errorf("debt.arh.Report: decode: %w", err)
		}
		closeDZ, closeKZ := parseF(jr.CloseDZ), parseF(jr.CloseKZ)
		openDZ, openKZ := parseF(jr.OpenDZ), parseF(jr.OpenKZ)
		country := countryOfINN(jr.CompanyID)
		out = append(out, DebtRow{
			Country:         country,
			Company:         arhCompanyName(jr.CompanyID, jr.Company),
			CompanyINN:      jr.CompanyID,
			Partner:         strings.TrimSpace(jr.Counterparty),
			PartnerINN:      jr.CounterpartyID,
			Account:         jr.Acc,
			AccountName:     accountNameFor(country, jr.AccRoot),
			Subaccount:      jr.Acc,
			Contract:        strings.TrimSpace(jr.ContractLabel),
			ContractRef:     strings.TrimSpace(jr.ContractKey),
			PaymentTermDays: jr.Delay,
			Currency:        arhCurrency(lens, jr.CurKod),
			// КЗ в источнике отрицательна → в положительный долг.
			OpeningDZ:  openDZ,
			OpeningKZ:  -openKZ,
			ClosingDZ:  closeDZ,
			ClosingKZ:  -closeKZ,
			TurnoverDZ: closeDZ - openDZ,
			TurnoverKZ: (-closeKZ) - (-openKZ),
			AgingDZ:    Aging{Lt30: parseF(jr.DZ30), D30to60: parseF(jr.DZ60), D60to90: parseF(jr.DZ90), Gt90: parseF(jr.DZ999)}.withCurrent(closeDZ),
			AgingKZ:    Aging{Lt30: -parseF(jr.KZ30), D30to60: -parseF(jr.KZ60), D60to90: -parseF(jr.KZ90), Gt90: -parseF(jr.KZ999)}.withCurrent(-closeKZ),
		})
	}
	return out, nil
}

func decodeArhSales(body []byte, lens string) ([]DebtRow, error) {
	out := []DebtRow{}
	dec := json.NewDecoder(bytes.NewReader(body))
	for dec.More() {
		var jr struct {
			CompanyID        string `json:"company_id"`
			Company          string `json:"company"`
			CounterpartyID   string `json:"counterparty_id"`
			Counterparty     string `json:"counterparty"`
			Acc              string `json:"acc"`
			CurKod           int    `json:"cur_kod"`
			RevenuePeriod    string `json:"revenue_period"`
			RevenueLastMonth string `json:"revenue_last_month"`
			ReturnsPeriod    string `json:"returns_period"`
			ReturnsLastMonth string `json:"returns_last_month"`
		}
		if err := dec.Decode(&jr); err != nil {
			return nil, fmt.Errorf("debt.arh.Sales: decode: %w", err)
		}
		country := countryOfINN(jr.CompanyID)
		out = append(out, DebtRow{
			Country:          country,
			Company:          arhCompanyName(jr.CompanyID, jr.Company),
			CompanyINN:       jr.CompanyID,
			Partner:          strings.TrimSpace(jr.Counterparty),
			PartnerINN:       jr.CounterpartyID,
			Account:          jr.Acc,
			AccountName:      accountNameFor(country, AccountRoot(jr.Acc)),
			Subaccount:       jr.Acc,
			Currency:         arhCurrency(lens, jr.CurKod),
			RevenuePeriod:    parseF(jr.RevenuePeriod),
			RevenueLastMonth: parseF(jr.RevenueLastMonth),
			ReturnsPeriod:    parseF(jr.ReturnsPeriod),
			ReturnsLastMonth: parseF(jr.ReturnsLastMonth),
		})
	}
	return out, nil
}

// arhDrilldownQuery — документы договора на снэпшот закрытия (closeDt) для (ЮЛ,
// контрагент, счёт, ключ договора). contractRef — Docs.ParentID договора или DocID
// документа без договора (ContractRef строки свода); пусто → все документы счёта.
// curKod != 0 → только строки этой валюты учёта (линза «В валюте договора»).
func arhDrilldownQuery(companyINN, partnerINN, acc, contractRef, closeDt, lens string, curKod int) string {
	esc := func(s string) string { return strings.ReplaceAll(s, "'", "''") }
	cond := " AND company_id = '" + esc(companyINN) + "' AND counterparty_id = '" + esc(partnerINN) + "' AND acc = '" + esc(acc) + "'"
	if strings.TrimSpace(contractRef) != "" {
		cond += " AND if(contract_id != '', contract_id, doc_id) = '" + esc(contractRef) + "'"
	}
	if curKod != 0 && lens == LensContract {
		cond += fmt.Sprintf(" AND currency_kod = %d", curKod)
	}
	grain := `SELECT doc_id, any(doc_number) AS doc_number, max(doc_date) AS doc_date, any(doc_type) AS doc_type,
           any(contract_title) AS contract_title, max(payment_date) AS payment_date, max(delay) AS delay, currency_kod,
           sum(sum_d) AS v_d, sum(sum_k) AS v_k, toDate('` + esc(closeDt) + `') AS rate_date, toInt32(840) AS usd_kod
    FROM finance.fact_arh_debt FINAL
    WHERE snapshot_date = toDate('` + esc(closeDt) + `')` + cond + `
    GROUP BY doc_id, currency_kod`
	var inner string
	if lens == LensContract {
		inner = `SELECT doc_id, doc_number, doc_date, doc_type, contract_title, payment_date, delay, v_d AS s_d, v_k AS s_k FROM (` + grain + `)`
	} else {
		k := arhRateK(lens)
		inner = `SELECT f.doc_id AS doc_id, f.doc_number AS doc_number, f.doc_date AS doc_date, f.doc_type AS doc_type,
           f.contract_title AS contract_title, f.payment_date AS payment_date, f.delay AS delay,
           toFloat64(f.v_d) * (` + k + `) AS s_d, toFloat64(f.v_k) * (` + k + `) AS s_k
    FROM (` + grain + `) f` + arhRates
	}
	return `
SELECT doc_id,
       any(doc_number)                        AS doc_number,
       toString(max(doc_date))                AS doc_date,
       any(doc_type)                          AS doc_type,
       any(contract_title)                    AS contract_title,
       ifNull(toString(max(payment_date)), '') AS payment_date,
       max(delay)                             AS delay,
       toString(round(sum(s_d), 2))           AS sum_d,
       toString(round(sum(s_k), 2))           AS sum_k
FROM (
    ` + inner + `
)
GROUP BY doc_id
ORDER BY doc_date, doc_number
FORMAT JSONEachRow`
}

// Drilldown — документы договора на снэпшот закрытия со сроком оплаты из Docs.PaymentDate
// и просрочкой в днях = max(0, дата отчёта − срок оплаты).
func (r *arhCHRepo) Drilldown(ctx context.Context, q DrilldownQuery) ([]DocumentRow, error) {
	if q.CompanyINN == "" || q.PartnerINN == "" || q.Account == "" {
		return nil, errors.New("debt.arh.Drilldown: company_inn/partner_inn/account required")
	}
	if q.DateTo.IsZero() {
		return nil, errors.New("debt.arh.Drilldown: date_to required")
	}
	to := asDate(q.DateTo)
	closeDt, _, err := r.snapshotDates(ctx, to, to)
	if err != nil {
		return nil, err
	}
	if closeDt == "" {
		return []DocumentRow{}, nil
	}
	body, err := r.post(ctx, arhDrilldownQuery(
		strings.TrimSpace(q.CompanyINN), strings.TrimSpace(q.PartnerINN), strings.TrimSpace(q.Account),
		strings.TrimSpace(q.Contract), closeDt, normLens(q.Lens), isoCurrencyKod(q.Currency)))
	if err != nil {
		return nil, err
	}
	return decodeArhDrilldown(body, q.DateTo)
}

func decodeArhDrilldown(body []byte, reportDate time.Time) ([]DocumentRow, error) {
	out := []DocumentRow{}
	dec := json.NewDecoder(bytes.NewReader(body))
	for dec.More() {
		var jr struct {
			DocID         string `json:"doc_id"`
			DocNumber     string `json:"doc_number"`
			DocDate       string `json:"doc_date"`
			DocType       string `json:"doc_type"`
			ContractTitle string `json:"contract_title"`
			PaymentDate   string `json:"payment_date"`
			SumD          string `json:"sum_d"`
			SumK          string `json:"sum_k"`
		}
		if err := dec.Decode(&jr); err != nil {
			return nil, fmt.Errorf("debt.arh.Drilldown: decode: %w", err)
		}
		sumD, sumK := parseF(jr.SumD), parseF(jr.SumK)
		kind := "Документ"
		if sumD != 0 {
			kind = "Дебиторская"
		} else if sumK != 0 {
			kind = "Кредиторская"
		}
		number := strings.TrimSpace(jr.DocNumber)
		if number == "" {
			number = strings.TrimSpace(jr.DocID)
		}
		group := strings.TrimSpace(jr.DocType)
		if group == "" {
			group = kind
		}
		dr := DocumentRow{
			DocNumber:   number,
			DocKind:     kind,
			TransGroup:  group,
			Amount:      absF64(sumD) + absF64(sumK),
			Description: strings.TrimSpace(jr.ContractTitle),
			DZChange:    sumD,
			KZChange:    -sumK,
		}
		if t, err := time.Parse("2006-01-02", jr.DocDate); err == nil && t.Year() > 1970 {
			dr.DocDate = t
		}
		if t, err := time.Parse("2006-01-02", jr.PaymentDate); err == nil {
			dr.PaymentDueDate = t
			if !reportDate.IsZero() && reportDate.After(t) {
				dr.OverdueDays = int(asMSSQLDate(reportDate).Sub(t) / (24 * time.Hour))
			}
		}
		out = append(out, dr)
	}
	return out, nil
}
