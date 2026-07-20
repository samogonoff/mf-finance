package debt

import (
	"context"
	"errors"
	"fmt"
	"strings"
)

// findebtDocDateCHRepo — ВТОРОЙ поток отчёта «Задолженность ВГО»
// (DEBT_BACKEND=findebt-docdate, docs/reports/debt/vgo-doc-date-methodology.md).
//
// Читает ТУ ЖЕ витрину finance.fact_findebt_ccy, что и findebt-ch, но берёт только
// линзу «В валюте договора» и пересчитывает BYN/USD САМ по курсу НА ДАТУ ДОКУМЕНТА
// (Doc_Date), а не на дату снэпшота. Курс подбирается через ASOF LEFT JOIN к
// finance.currency_daily (последний известный курс на Doc_Date или раньше), код
// валюты — из finance.dim_valuta по наименованию (NAIM).
//
// Свёртка начало/оборот/конец — та же (разница снэпшотов close_dt/open_dt), поэтому
// в линзе «В валюте договора» числа СОВПАДАЮТ с findebt-ch (sanity-check), а BYN/USD
// отличаются: findebt-ch даёт «плавающую» сумму (курс на дату снэпшота), а этот поток
// — стабильную (курс на дату документа). Оба бэкенда переключаются через DEBT_BACKEND
// и сравниваются на одних фильтрах.
//
// USD всегда через BYN: Saldo_USD = Saldo_BYN / курс(USD[kod=840]→BYN на Doc_Date) —
// прямых кросс-курсов в источниках нет (методика 1С/НБ РБ).
type findebtDocDateCHRepo struct {
	*findebtCHRepo
}

// NewFinDebtDocDateCHRepo — конструктор. (nil,nil) при ненастроенном CH (как база).
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

// docDateConvExpr — CH-выражение пересчёта суммы (col = sum_d|sum_k) в линзу lens
// по ASOF-джойненным курсам: rSrc = валюта_договора→BYN, rUsd = USD→BYN, оба на
// Doc_Date. Для BYN-документов курс к BYN отсутствует → coalesce(...,1) (сумма как
// есть). Для USD делим на курс USD→BYN, nullIf защищает от деления на 0.
func docDateConvExpr(lens, col string) string {
	switch lens {
	case LensBYN:
		return col + " * coalesce(rSrc.curr_rate, 1)"
	case LensUSD:
		return col + " * coalesce(rSrc.curr_rate, 1) / nullIf(rUsd.curr_rate, 0)"
	default: // LensContract — сумма как есть в валюте договора, без пересчёта
		return col
	}
}

// findebtDocDateReportQuery — свод ДЗ/КЗ по договорам с пересчётом на Doc_Date.
// Всегда читает линзу «В валюте договора» (cur_filter), а BYN/USD получает
// умножением на курс. Структура вывода идентична findebtReportQuery — разбирается
// общим decodeFinDebtReport.
//
// ⚠ Два ASOF LEFT JOIN в одном запросе (курс валюты договора + курс USD) — как в
// методике аналитика (§7). ASOF требует КОЛОНОЧНОГО equi-join, поэтому код валюты
// USD подаётся не константой в ON (rUsd.kod = 840 → ошибка «needs at least one
// equi-join column»), а колонкой usd_kod = toInt32(840) в левой части. Проверено
// на CH 24.3.
func findebtDocDateReportQuery(inns, accRoots []string, from, to, lens string) string {
	orgClause := ""
	if len(inns) > 0 {
		orgClause = " AND ff.company_id IN (" + quoteList(inns) + ")"
	}
	accClause := ""
	if len(accRoots) > 0 {
		accClause = " AND ff.acc_root IN (" + quoteList(accRoots) + ")"
	}
	contractLens := strings.ReplaceAll(LensContract, "'", "''")
	dConv := docDateConvExpr(lens, "sum_d")
	kConv := docDateConvExpr(lens, "sum_k")
	return fmt.Sprintf(`
WITH
  (SELECT max(snapshot_date) FROM finance.fact_findebt_ccy WHERE snapshot_date <= toDate('%[2]s') AND cur_filter = '%[5]s') AS close_dt,
  (SELECT max(snapshot_date) FROM finance.fact_findebt_ccy WHERE snapshot_date <  toDate('%[1]s') AND cur_filter = '%[5]s') AS open_dt
SELECT company_id,
       any(company)      AS company,
       counterparty_id,
       any(counterparty) AS counterparty,
       acc,
       acc_root,
       doc_number,
       any(description)  AS description,
       any(currency)     AS currency,
       max(delay)        AS delay,
       toString(sumIf(d_conv, snapshot_date = close_dt)) AS close_dz,
       toString(sumIf(k_conv, snapshot_date = close_dt)) AS close_kz,
       toString(sumIf(d_conv, snapshot_date = open_dt))  AS open_dz,
       toString(sumIf(k_conv, snapshot_date = open_dt))  AS open_kz
FROM (
    SELECT snapshot_date, company_id, company, counterparty_id, counterparty,
           acc, acc_root, doc_number, description, currency, delay,
           %[6]s AS d_conv,
           %[7]s AS k_conv
    FROM (
        SELECT ff.snapshot_date AS snapshot_date, ff.company_id AS company_id, ff.company AS company,
               ff.counterparty_id AS counterparty_id, ff.counterparty AS counterparty,
               ff.acc AS acc, ff.acc_root AS acc_root, ff.doc_number AS doc_number,
               ff.description AS description, ff.currency AS currency, ff.delay AS delay,
               ff.doc_date AS doc_date, ff.sum_d AS sum_d, ff.sum_k AS sum_k, v.kod AS src_kod,
               toInt32(840) AS usd_kod
        FROM finance.fact_findebt_ccy AS ff FINAL
        LEFT JOIN finance.dim_valuta v ON v.naim = ff.currency
        WHERE ff.cur_filter = '%[5]s'
          AND ff.snapshot_date IN (close_dt, open_dt)%[3]s%[4]s
    ) f
    ASOF LEFT JOIN finance.currency_daily rSrc ON rSrc.kod = f.src_kod AND f.doc_date >= rSrc.date
    ASOF LEFT JOIN finance.currency_daily rUsd ON rUsd.kod = f.usd_kod AND f.doc_date >= rUsd.date
)
GROUP BY company_id, counterparty_id, acc, acc_root, doc_number
HAVING abs(sumIf(d_conv, snapshot_date = close_dt)) > 0.005
    OR abs(sumIf(k_conv, snapshot_date = close_dt)) > 0.005
    OR abs(sumIf(d_conv, snapshot_date = open_dt)) > 0.005
    OR abs(sumIf(k_conv, snapshot_date = open_dt)) > 0.005
ORDER BY company, counterparty, acc, doc_number
FORMAT JSONEachRow`, from, to, orgClause, accClause, contractLens, dConv, kConv)
}

// Report — свод с пересчётом валют на Doc_Date. Разбор — общий decodeFinDebtReport.
func (r *findebtDocDateCHRepo) Report(ctx context.Context, f Filters) ([]DebtRow, error) {
	if f.DateTo.IsZero() {
		return nil, errors.New("debt.findebt-docdate.Report: date_to required")
	}
	lens := normLens(f.Lens)
	body, err := r.post(ctx, findebtDocDateReportQuery(f.EntityINNs, f.Accounts, asDate(f.DateFrom), asDate(f.DateTo), lens))
	if err != nil {
		return nil, err
	}
	return decodeFinDebtReport(body, lens)
}

// findebtDocDateDrilldownQuery — документы договора на снэпшот закрытия, суммы
// пересчитаны на Doc_Date каждого документа. Структура — как findebtDrilldownQuery.
func findebtDocDateDrilldownQuery(companyINN, partnerINN, acc, docNumber, to, lens string) string {
	esc := func(s string) string { return strings.ReplaceAll(s, "'", "''") }
	docClause := ""
	if strings.TrimSpace(docNumber) != "" {
		docClause = " AND ff.doc_number = '" + esc(docNumber) + "'"
	}
	contractLens := esc(LensContract)
	dConv := docDateConvExpr(lens, "sum_d")
	kConv := docDateConvExpr(lens, "sum_k")
	return fmt.Sprintf(`
SELECT doc_number,
       toString(doc_date)                 AS doc_date,
       description,
       ifNull(toString(payment_date), '') AS payment_date,
       day_delay,
       delay,
       toString(%[7]s)                    AS sum_d,
       toString(%[8]s)                    AS sum_k
FROM (
    SELECT ff.doc_number AS doc_number, ff.doc_date AS doc_date, ff.description AS description,
           ff.payment_date AS payment_date, ff.day_delay AS day_delay, ff.delay AS delay,
           ff.sum_d AS sum_d, ff.sum_k AS sum_k, ff.currency AS currency, v.kod AS src_kod,
           toInt32(840) AS usd_kod
    FROM finance.fact_findebt_ccy AS ff FINAL
    LEFT JOIN finance.dim_valuta v ON v.naim = ff.currency
    WHERE ff.snapshot_date = (SELECT max(snapshot_date) FROM finance.fact_findebt_ccy WHERE snapshot_date <= toDate('%[4]s') AND cur_filter = '%[6]s')
      AND ff.cur_filter = '%[6]s'
      AND ff.company_id = '%[1]s' AND ff.counterparty_id = '%[2]s' AND ff.acc = '%[3]s'%[5]s
) f
ASOF LEFT JOIN finance.currency_daily rSrc ON rSrc.kod = f.src_kod AND f.doc_date >= rSrc.date
ASOF LEFT JOIN finance.currency_daily rUsd ON rUsd.kod = f.usd_kod AND f.doc_date >= rUsd.date
ORDER BY doc_date, doc_number
FORMAT JSONEachRow`, esc(companyINN), esc(partnerINN), esc(acc), esc(to), docClause, contractLens, dConv, kConv)
}

// Drilldown — документная детализация договора с пересчётом на Doc_Date.
func (r *findebtDocDateCHRepo) Drilldown(ctx context.Context, q DrilldownQuery) ([]DocumentRow, error) {
	if q.CompanyINN == "" || q.PartnerINN == "" || q.Account == "" {
		return nil, errors.New("debt.findebt-docdate.Drilldown: company_inn/partner_inn/account required")
	}
	if q.DateTo.IsZero() {
		return nil, errors.New("debt.findebt-docdate.Drilldown: date_to required")
	}
	body, err := r.post(ctx, findebtDocDateDrilldownQuery(
		strings.TrimSpace(q.CompanyINN), strings.TrimSpace(q.PartnerINN),
		strings.TrimSpace(q.Account), strings.TrimSpace(q.Contract), asDate(q.DateTo), normLens(q.Lens)))
	if err != nil {
		return nil, err
	}
	return decodeFinDebtDrilldown(body)
}
