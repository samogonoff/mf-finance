package debt

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"strconv"
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
//   - ДЗ/КЗ документа считаются как у FinDebt: сначала нетто (sum_d + sum_k) по непустому
//     doc_id внутри (дата, ЮЛ, контрагент, счёт, валюта); для КЗ — включая разные счета,
//     с назначением результата счёту стороны итогового знака; строки без doc_id не
//     сворачиваются; корзины идут на сторону нетто;
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

// arhSnapshotMarker — нулевая строка присутствия ЮЛ, которую ETL добавляет на каждый
// сформированный день. Она участвует только в выборе/проверке даты снэпшота.
const arhSnapshotMarker = "__snapshot__"

// arhNoContractRef — маркер группы «без договора» (пустой ключ договора) в DebtRow.ContractRef:
// фронт передаёт contract_ref в drill-down как есть, а пустая строка там значит «без фильтра».
const arhNoContractRef = "__none__"

// arhMaxSnapshotGapDays — на сколько дней ближайший снэпшот может отстоять от даты отчёта.
// Ежедневные снэпшоты есть только в окне ARH_WINDOW_DAYS, дальше — концы месяцев; без этого
// порога дата молча подменялась бы концом месяца.
const arhMaxSnapshotGapDays = 3

// NoSnapshotError — на запрошенную дату нет снэпшота остатков (handler отдаёт 422).
type NoSnapshotError struct{ Msg string }

func (e *NoSnapshotError) Error() string { return e.Msg }

// arhAmtCols — суммы уже нормализованных строк arhNetDebtRows в зерне свода.
var arhAmtCols = func() [][2]string {
	cols := [][2]string{{"v_d", "sum(sum_d)"}, {"v_k", "sum(sum_k)"}}
	for _, b := range []string{"30", "60", "90", "999"} {
		cols = append(cols,
			[2]string{"v" + b + "_d", "sum(sum" + b + "_d)"},
			[2]string{"v" + b + "_k", "sum(sum" + b + "_k)"})
	}
	return cols
}()

// arhNetDebtRows сворачивает непустой документ до нетто. РБ/РФ/УЗ сворачиваются внутри
// счёта. Для КЗ FinDebt сворачивает один DocID между счетами: положительное нетто остаётся
// на дебетовом счёте документа, отрицательное — на кредитовом. Если счетов одной стороны
// несколько, выбирается счёт с наибольшей суммой; номер счёта разрешает равенство
// детерминированно. Корзины суммируются в том же зерне; если хотя бы одна корзина имеет
// знак, противоположный итогу документа, FinDebt обнуляет все его корзины. Пустой doc_id
// сохраняет обе стороны без взаимозачёта. source — FROM/JOIN/WHERE по fact_arh_debt.
func arhNetDebtRows(source string, keepSnapshotBounds bool) string {
	const zero = "toDecimal64(0, 2)"
	const net = "(sum(r.sum_d) + sum(r.sum_k))"
	crossAccount := "r.company_id IN (" + quoteList(entityINNsByCountry(CountryKZ)) + ")"
	rawCrossAccount := "company_id IN (" + quoteList(entityINNsByCountry(CountryKZ)) + ")"
	assignedAcc := `if(r.doc_id = '' OR NOT (` + crossAccount + `), r.net_acc,
           if(` + net + ` >= 0,
              argMaxIf(r.acc, tuple(r.sum_d, r.acc), r.sum_d > 0),
              argMaxIf(r.acc, tuple(-r.sum_k, r.acc), r.sum_k < 0)))`

	var amounts strings.Builder
	amounts.WriteString(", if(r.doc_id = '', sum(r.sum_d), if(" + net + " > 0, " + net + ", " + zero + ")) AS sum_d")
	amounts.WriteString(", if(r.doc_id = '', sum(r.sum_k), if(" + net + " < 0, " + net + ", " + zero + ")) AS sum_k")
	bucketNets := make(map[string]string, 4)
	for _, b := range []string{"30", "60", "90", "999"} {
		bucketNets[b] = "(sum(r.sum" + b + "_d) + sum(r.sum" + b + "_k))"
	}
	var allNonNegative, allNonPositive []string
	for _, b := range []string{"30", "60", "90", "999"} {
		allNonNegative = append(allNonNegative, bucketNets[b]+" >= "+zero)
		allNonPositive = append(allNonPositive, bucketNets[b]+" <= "+zero)
	}
	validD := net + " > 0 AND " + strings.Join(allNonNegative, " AND ")
	validK := net + " < 0 AND " + strings.Join(allNonPositive, " AND ")
	for _, b := range []string{"30", "60", "90", "999"} {
		amounts.WriteString(", if(r.doc_id = '', sum(r.sum" + b + "_d), if(" + validD + ", " + bucketNets[b] + ", " + zero + ")) AS sum" + b + "_d")
		amounts.WriteString(", if(r.doc_id = '', sum(r.sum" + b + "_k), if(" + validK + ", " + bucketNets[b] + ", " + zero + ")) AS sum" + b + "_k")
	}
	bounds := ""
	if keepSnapshotBounds {
		bounds = ", any(r.close_dt) AS close_dt, any(r.open_dt) AS open_dt"
	}

	return `SELECT r.snapshot_date AS snapshot_date, r.company_id AS company_id, any(r.company) AS company,
       r.counterparty_id AS counterparty_id, any(r.counterparty) AS counterparty,
       ` + assignedAcc + ` AS acc,
       arrayElement(splitByChar('.', ` + assignedAcc + `), 1) AS acc_root,
       r.doc_id AS doc_id, anyIf(r.doc_number, r.doc_number != '') AS doc_number, max(r.doc_date) AS doc_date,
       anyIf(r.contract_id, r.contract_id != '') AS contract_id,
       anyIf(r.contract_number, r.contract_number != '') AS contract_number,
       anyIf(r.contract_title, r.contract_title != '') AS contract_title,
       anyIf(r.doc_type, r.doc_type != '') AS doc_type,
       max(r.delay) AS delay, max(r.payment_date) AS payment_date, r.currency_kod AS currency_kod` + bounds + amounts.String() + `
FROM (
    SELECT *, if(doc_id = '' OR NOT (` + rawCrossAccount + `), acc, '') AS net_acc
    ` + source + `
) AS r
GROUP BY r.snapshot_date, r.company_id, r.counterparty_id, r.doc_id, r.net_acc, r.currency_kod`
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

// arhSnapshotDates — общие верхние границы close_dt (последний снэпшот ≤ to) и open_dt
// (последний < from). Пустая строка — такого снэпшота нет. Сам свод выбирает ближайшие
// снэпшоты отдельно по каждому ЮЛ в пределах этих границ.
func arhSnapshotDatesQuery(inns []string, from, to string) string {
	orgClause := ""
	if len(inns) > 0 {
		orgClause = " AND company_id IN (" + quoteList(inns) + ")"
	}
	return fmt.Sprintf(`SELECT toString(maxIf(snapshot_date, snapshot_date <= toDate('%[2]s'))),
       toString(maxIf(snapshot_date, snapshot_date < toDate('%[1]s')))
FROM finance.fact_arh_debt WHERE snapshot_date <= toDate('%[2]s')%[3]s FORMAT TSV`, from, to, orgClause)
}

func (r *arhCHRepo) snapshotDates(ctx context.Context, inns []string, from, to string, checkOpen bool) (closeDt, openDt string, err error) {
	body, err := r.post(ctx, arhSnapshotDatesQuery(inns, from, to))
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
	closeDt, openDt = norm(parts[0]), norm(parts[1])
	if err := r.checkCompanySnapshotFreshness(ctx, inns, from, to, checkOpen); err != nil {
		return "", "", err
	}
	return closeDt, openDt, nil
}

// checkCompanySnapshotFreshness catches a partial upstream outage: some companies may
// disappear from Debt_arh while other companies keep producing fresh snapshots. A single
// global MAX(snapshot_date) would hide that loss and the report would show zero debt for
// the missing companies. Companies that have never had rows are ignored; for every company
// present on or before the report date, both available period boundaries must be no more
// than three days away from the requested dates.
func (r *arhCHRepo) checkCompanySnapshotFreshness(ctx context.Context, inns []string, from, to string, checkOpen bool) error {
	if len(inns) == 0 {
		for _, e := range Entities() {
			inns = append(inns, e.INN)
		}
	}
	if len(inns) == 0 {
		return nil
	}
	having := `dateDiff('day', maxIf(snapshot_date, snapshot_date <= toDate('` + to + `')), toDate('` + to + `')) > ` + strconv.Itoa(arhMaxSnapshotGapDays)
	if checkOpen {
		having += `
    OR (maxIf(snapshot_date, snapshot_date < toDate('` + from + `')) != toDate('` + arhNoDate + `')
        AND dateDiff('day', maxIf(snapshot_date, snapshot_date < toDate('` + from + `')), toDate('` + from + `')) > ` + strconv.Itoa(arhMaxSnapshotGapDays) + `)`
	}
	q := `SELECT company_id,
       toString(maxIf(snapshot_date, snapshot_date <= toDate('` + to + `'))),
       toString(maxIf(snapshot_date, snapshot_date < toDate('` + from + `')))
FROM finance.fact_arh_debt
WHERE snapshot_date <= toDate('` + to + `')
  AND company_id IN (` + quoteList(inns) + `)
GROUP BY company_id
HAVING ` + having + `
ORDER BY company_id
FORMAT TSV`
	body, err := r.post(ctx, q)
	if err != nil {
		return fmt.Errorf("arh: свежесть снэпшотов по ЮЛ: %w", err)
	}
	var stale []string
	for _, line := range strings.Split(strings.TrimSpace(string(body)), "\n") {
		parts := strings.Split(strings.TrimSpace(line), "\t")
		if len(parts) != 3 {
			continue
		}
		inn := strings.TrimSpace(parts[0])
		closeDay, openDay := strings.TrimSpace(parts[1]), strings.TrimSpace(parts[2])
		name := entityNameForINN(inn)
		if name == "" {
			name = inn
		} else {
			name += " (" + inn + ")"
		}
		dates := "закрытие " + ruDay(closeDay)
		if checkOpen && openDay != arhNoDate && daysBetweenISO(openDay, from) > arhMaxSnapshotGapDays {
			dates += ", открытие " + ruDay(openDay)
		}
		stale = append(stale, name+": "+dates)
	}
	if len(stale) > 0 {
		return &NoSnapshotError{Msg: fmt.Sprintf("нет актуального снэпшота по ЮЛ на дату %s: %s", ruDay(to), strings.Join(stale, "; "))}
	}
	return nil
}

func ruDay(iso string) string {
	if t, err := time.Parse("2006-01-02", iso); err == nil {
		return t.Format("02.01.2006")
	}
	return iso
}

func daysBetweenISO(from, to string) int {
	a, err1 := time.Parse("2006-01-02", from)
	b, err2 := time.Parse("2006-01-02", to)
	if err1 != nil || err2 != nil {
		return 0
	}
	return int(b.Sub(a).Hours() / 24)
}

// checkArhSnapshots — снэпшот на дату отчёта должен быть «свежим»: не дальше
// arhMaxSnapshotGapDays от date_to (и open_dt — от date_from), иначе понятная ошибка вместо
// молчаливой подмены даты концом месяца. Нет open_dt вовсе — период начинается с первого
// загруженного снэпшота, остаток на начало нулевой (это не ошибка).
func checkArhSnapshots(closeDt, openDt, from, to string) error {
	if closeDt == "" {
		return &NoSnapshotError{Msg: fmt.Sprintf("нет снэпшота на дату %s: в данных нет снэпшотов остатков ранее этой даты (загрузка arh не выполнялась?)", ruDay(to))}
	}
	if daysBetweenISO(closeDt, to) > arhMaxSnapshotGapDays {
		return &NoSnapshotError{Msg: fmt.Sprintf("нет снэпшота на дату %s; ближайший: %s (ежедневные снэпшоты есть только за последние ARH_WINDOW_DAYS дней, раньше — концы месяцев)", ruDay(to), ruDay(closeDt))}
	}
	if openDt != "" && daysBetweenISO(openDt, from) > arhMaxSnapshotGapDays {
		return &NoSnapshotError{Msg: fmt.Sprintf("нет снэпшота на начало периода (перед %s); ближайший: %s (ежедневные снэпшоты есть только за последние ARH_WINDOW_DAYS дней, раньше — концы месяцев)", ruDay(from), ruDay(openDt))}
	}
	return nil
}

// entityINNsByCountry — УНП наших ЮЛ страны c из seed.
func entityINNsByCountry(c Country) []string {
	out := []string{}
	for _, e := range Entities() {
		if e.Country == c {
			out = append(out, e.INN)
		}
	}
	return out
}

// arhCompanyDatesSubquery выбирает границы периода отдельно по каждому ЮЛ. Это важно,
// когда страны публикуют снэпшоты в разные дни: строка компании с допустимым лагом не должна
// исчезнуть только потому, что у другой компании уже есть более свежая дата.
func arhCompanyDatesSubquery(inns []string, closeDt, openDt string) string {
	if openDt == "" {
		openDt = arhNoDate
	}
	orgClause := ""
	if len(inns) > 0 {
		orgClause = " AND company_id IN (" + quoteList(inns) + ")"
	}
	return `SELECT company_id,
       maxIf(snapshot_date, snapshot_date <= toDate('` + closeDt + `')) AS close_dt,
       maxIf(snapshot_date, snapshot_date <= toDate('` + openDt + `')) AS open_dt
FROM finance.fact_arh_debt
WHERE snapshot_date <= toDate('` + closeDt + `')` + orgClause + `
GROUP BY company_id`
}

// arhReportQuery — свод ДЗ/КЗ на уровне договора: остатки на close_dt/open_dt, корзины
// просрочки на close_dt. Для линз BYN/USD суммы пересчитываются на дату снэпшота и
// схлопываются по всем валютам (cur_kod = 0); для «В валюте договора» валюта — измерение.
// Строка не теряется при отсутствии курса: HAVING смотрит на РОДНЫЕ суммы (n_d/n_k), а
// пересчитанные при этом остаются нулевыми (см. arhRateK).
// Все значения в SQL — даты из CH, УНП/счета из seed (не пользовательский ввод).
func arhReportQuery(inns, accRoots []string, closeDt, openDt, lens string) string {
	if openDt == "" {
		openDt = arhNoDate
	}
	companyDates := arhCompanyDatesSubquery(inns, closeDt, openDt)
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
		gb.WriteString(", " + c[1] + " AS " + c[0])
	}
	netRows := arhNetDebtRows(`FROM finance.fact_arh_debt FINAL
    INNER JOIN (`+companyDates+`) d USING (company_id)
    WHERE (snapshot_date = close_dt OR snapshot_date = open_dt)
      AND acc != '`+arhSnapshotMarker+`'`+orgClause, true)
	grain := `SELECT snapshot_date, company_id, any(company) AS company, counterparty_id, any(counterparty) AS counterparty,
           acc, acc_root, if(contract_id != '', contract_id, doc_id) AS contract_key,
           anyIf(` + arhLabelExpr + `, ` + arhLabelExpr + ` != '') AS contract_label,
           max(delay) AS delay, currency_kod` + gb.String() + `,
           snapshot_date = close_dt AS is_close, snapshot_date = open_dt AS is_open,
           snapshot_date AS rate_date, toInt32(840) AS usd_kod
    FROM (` + netRows + `)
    WHERE 1 = 1` + accClause + `
    GROUP BY snapshot_date, company_id, counterparty_id, acc, acc_root, contract_key, currency_kod, is_close, is_open`

	var inner string
	if lens == LensContract {
		var sb strings.Builder
		for _, c := range arhAmtCols {
			sb.WriteString(", " + c[0] + " AS s" + c[0][1:])
		}
		inner = `SELECT snapshot_date, is_close, is_open, company_id, company, counterparty_id, counterparty, acc, acc_root,
           contract_key, contract_label, delay, currency_kod AS cur_kod, v_d AS n_d, v_k AS n_k` + sb.String() + `
    FROM (` + grain + `)`
	} else {
		k := arhRateK(lens)
		var sb strings.Builder
		for _, c := range arhAmtCols {
			sb.WriteString(", toFloat64(f." + c[0] + ") * (" + k + ") AS s" + c[0][1:])
		}
		inner = `SELECT f.snapshot_date AS snapshot_date, f.is_close AS is_close, f.is_open AS is_open,
           f.company_id AS company_id, f.company AS company,
           f.counterparty_id AS counterparty_id, f.counterparty AS counterparty, f.acc AS acc, f.acc_root AS acc_root,
           f.contract_key AS contract_key, f.contract_label AS contract_label, f.delay AS delay, toUInt32(0) AS cur_kod,
           f.v_d AS n_d, f.v_k AS n_k` + sb.String() + `
    FROM (` + grain + `) f` + arhRates
	}
	at := func(col, boundary string) string {
		return fmt.Sprintf("toString(round(sumIf(%s, %s), 2))", col, boundary)
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
       ` + at("s_d", "is_close") + ` AS close_dz,
       ` + at("s_k", "is_close") + ` AS close_kz,
       ` + at("s_d", "is_open") + ` AS open_dz,
       ` + at("s_k", "is_open") + ` AS open_kz,
       ` + at("s30_d", "is_close") + ` AS dz30,
       ` + at("s60_d", "is_close") + ` AS dz60,
       ` + at("s90_d", "is_close") + ` AS dz90,
       ` + at("s999_d", "is_close") + ` AS dz999,
       ` + at("s30_k", "is_close") + ` AS kz30,
       ` + at("s60_k", "is_close") + ` AS kz60,
       ` + at("s90_k", "is_close") + ` AS kz90,
       ` + at("s999_k", "is_close") + ` AS kz999
FROM (
    ` + inner + `
)
GROUP BY company_id, counterparty_id, acc, acc_root, contract_key, cur_kod
HAVING sumIf(abs(n_d) + abs(n_k), is_close OR is_open) > 0.005
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
//
// Знак по стране: у КЗ (141240004842) продажи Дт 1210/Кт 6010 в источнике идут со знаком МИНУС,
// поэтому для ЮЛ КЗ знак инвертируется (продажа = минус в источнике). Это эмпирика из данных
// сентября 2026 (2070 строк, −389.6 млн по всем каналам; по ВГО лишь 4 строки −266 тыс.),
// ТРЕБУЕТ ПОДТВЕРЖДЕНИЯ аналитиком. Отгрузки КЗ по ВГО идут по Кт 3310 / Дт 1310 и 1330
// (+206 млн) и в правило 6010 НЕ попадают — выручка КЗ по ВГО здесь фактически нулевая.
func arhSalesQuery(inns []string, from, to, lens string) string {
	orgClause := ""
	if len(inns) > 0 {
		orgClause = " AND company_id IN (" + quoteList(inns) + ")"
	}
	period := fmt.Sprintf("event_date >= toDate('%[1]s') AND event_date <= toDate('%[2]s')", from, to)
	last := fmt.Sprintf("event_date >= addMonths(toStartOfMonth(toDate('%[1]s')), -1) AND event_date < toStartOfMonth(toDate('%[1]s'))", to)
	kz := "company_id IN (" + quoteList(entityINNsByCountry(CountryKZ)) + ")"
	grain := `SELECT event_date, company_id, any(company) AS company, counterparty_id, any(counterparty) AS counterparty,
           acc, currency_kod, if(` + kz + `, -amount_sign, amount_sign) AS eff_sign,
           sum(if(` + kz + `, -amount, amount)) AS v,
           event_date AS rate_date, toInt32(840) AS usd_kod
    FROM finance.fact_arh_sales FINAL
    WHERE (` + revenueAccCondition() + `)
      AND ((` + period + `) OR (` + last + `))` + orgClause + `
    GROUP BY event_date, company_id, counterparty_id, acc, currency_kod, amount_sign, eff_sign`
	var inner string
	if lens == LensContract {
		inner = `SELECT event_date, company_id, company, counterparty_id, counterparty, acc, currency_kod AS cur_kod, eff_sign, v AS s, v AS n
    FROM (` + grain + `)`
	} else {
		inner = `SELECT f.event_date AS event_date, f.company_id AS company_id, f.company AS company,
           f.counterparty_id AS counterparty_id, f.counterparty AS counterparty, f.acc AS acc,
           toUInt32(0) AS cur_kod, f.eff_sign AS eff_sign, f.v AS n, toFloat64(f.v) * (` + arhRateK(lens) + `) AS s
    FROM (` + grain + `) f` + arhRates
	}
	sum := func(cond, sign string) string { return sign + "sumIf(s, " + cond + ")" }
	sp := sum(period+" AND eff_sign > 0", "")
	sl := sum(last+" AND eff_sign > 0", "")
	rp := sum(period+" AND eff_sign < 0", "-")
	rl := sum(last+" AND eff_sign < 0", "-")
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
HAVING sumIf(abs(n), ((` + period + `) OR (` + last + `)) AND eff_sign != 0) > 0.005
ORDER BY company, counterparty, acc
FORMAT JSONEachRow`
}

// Report — остатки ДЗ/КЗ по договорам + строки продаж/возвратов (без договора).
func (r *arhCHRepo) Report(ctx context.Context, f Filters) ([]DebtRow, error) {
	if f.DateTo.IsZero() {
		return nil, errors.New("debt.arh.Report: date_to required")
	}
	lens := normLens(f.Lens)
	hasFrom := !f.DateFrom.IsZero()
	from, to := asDate(f.DateFrom), asDate(f.DateTo)
	if !hasFrom {
		from = to
	}
	closeDt, openDt, err := r.snapshotDates(ctx, f.EntityINNs, from, to, hasFrom)
	if err != nil {
		return nil, err
	}
	if !hasFrom {
		openDt = ""
	}
	if err := checkArhSnapshots(closeDt, openDt, from, to); err != nil {
		return nil, err
	}
	body, err := r.post(ctx, arhReportQuery(f.EntityINNs, f.Accounts, closeDt, openDt, lens))
	if err != nil {
		return nil, err
	}
	rows, err := decodeArhReport(body, lens)
	if err != nil {
		return nil, err
	}
	defer r.warnMissingRates(ctx, lens, closeDt, openDt, from, to)
	// Продажи — отдельная таблица: нет миграции 015 / поток не залит → отчёт по долгу
	// остаётся рабочим, только без продаж и возвратов (причина — в логе).
	sales, err := r.salesRows(ctx, f, from, to, lens)
	if err != nil {
		log.Printf("debt.arh: продажи недоступны (fact_arh_sales): %v", err)
		return rows, nil
	}
	return append(rows, sales...), nil
}

// arhMissingRatesQuery — пары «валюта@дата» без курса (и курса USD для линзы USD) по таблице.
func arhMissingRatesQuery(table, dateCol, dateCond, lens string) string {
	miss := "(f.currency_kod != 933 AND rSrc.curr_rate = 0)"
	if lens == LensUSD {
		miss += " OR rUsd.curr_rate = 0"
	}
	realRows := ""
	if table == "finance.fact_arh_debt" {
		realRows = " AND acc != '" + arhSnapshotMarker + "'"
	}
	return `SELECT DISTINCT concat(toString(f.currency_kod), '@', toString(f.rate_date)) FROM (
    SELECT currency_kod, ` + dateCol + ` AS rate_date, toInt32(840) AS usd_kod FROM ` + table + `
    WHERE ` + dateCond + realRows + ` GROUP BY currency_kod, ` + dateCol + `
) f` + arhRates + `
WHERE ` + miss + ` ORDER BY 1 FORMAT TSV`
}

// warnMissingRates — одно сообщение в лог на запрос: для каких валют/дат нет курса (строки
// при этом остаются, но пересчитанные суммы нулевые) и что currency_daily пуста. Только для
// линз BYN/USD; ошибки самой проверки не мешают отчёту.
func (r *arhCHRepo) warnMissingRates(ctx context.Context, lens, closeDt, openDt, from, to string) {
	if lens == LensContract {
		return
	}
	cnt, err := r.post(ctx, "SELECT count() FROM finance.currency_daily FORMAT TSV")
	if err != nil {
		return
	}
	if n, _ := strconv.Atoi(strings.TrimSpace(string(cnt))); n == 0 {
		log.Printf("debt.arh: finance.currency_daily ПУСТА — суммы в линзе %q будут нулевыми; залейте курсы (MODE=currency /findebt-etl или CURRENCY_SYNC_INTERVAL)", lens)
		return
	}
	if openDt == "" {
		openDt = arhNoDate
	}
	missing := []string{}
	queries := []string{
		arhMissingRatesQuery("finance.fact_arh_debt", "snapshot_date", fmt.Sprintf("snapshot_date IN (toDate('%s'), toDate('%s'))", closeDt, openDt), lens),
		arhMissingRatesQuery("finance.fact_arh_sales", "event_date", fmt.Sprintf("event_date >= toDate('%s') AND event_date <= toDate('%s')", from, to), lens),
	}
	for _, q := range queries {
		body, err := r.post(ctx, q)
		if err != nil {
			continue
		}
		for _, line := range strings.Split(strings.TrimSpace(string(body)), "\n") {
			if parts := strings.SplitN(strings.TrimSpace(line), "@", 2); len(parts) == 2 {
				kod, _ := strconv.Atoi(parts[0])
				missing = append(missing, isoCurrencyName(kod)+"@"+parts[1])
			}
		}
	}
	if len(missing) > 0 {
		log.Printf("debt.arh: нет курса для линзы %q (пересчитанная сумма этих строк = 0): %s", lens, strings.Join(missing, ", "))
	}
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

// arhContractRef — ключ договора для DebtRow.ContractRef; пустой ключ → явный маркер.
func arhContractRef(key string) string {
	if key = strings.TrimSpace(key); key != "" {
		return key
	}
	return arhNoContractRef
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
			ContractRef:     arhContractRef(jr.ContractKey),
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
	cond := " AND company_id = " + chQuote(companyINN) + " AND counterparty_id = " + chQuote(partnerINN)
	switch ref := strings.TrimSpace(contractRef); ref {
	case "": // без фильтра по договору
	case arhNoContractRef: // группа «без договора»: пустой ключ
		cond += " AND if(contract_id != '', contract_id, doc_id) = ''"
	default:
		cond += " AND if(contract_id != '', contract_id, doc_id) = " + chQuote(ref)
	}
	if curKod != 0 && lens == LensContract {
		cond += fmt.Sprintf(" AND currency_kod = %d", curKod)
	}
	netRows := arhNetDebtRows(`FROM finance.fact_arh_debt FINAL
    WHERE snapshot_date = toDate(`+chQuote(closeDt)+`)`+cond, false)
	grain := `SELECT doc_id, any(doc_number) AS doc_number, max(doc_date) AS doc_date, any(doc_type) AS doc_type,
           any(contract_title) AS contract_title, max(payment_date) AS payment_date, max(delay) AS delay, currency_kod,
           ` + arhAmtCols[0][1] + ` AS v_d, ` + arhAmtCols[1][1] + ` AS v_k, toDate(` + chQuote(closeDt) + `) AS rate_date, toInt32(840) AS usd_kod
    FROM (` + netRows + `)
    WHERE acc = ` + chQuote(acc) + `
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
	// partner_inn может быть пустым (контрагент без УНП) — тогда фильтр counterparty_id = ''.
	if q.CompanyINN == "" || q.Account == "" {
		return nil, errors.New("debt.arh.Drilldown: company_inn/account required")
	}
	if q.DateTo.IsZero() {
		return nil, errors.New("debt.arh.Drilldown: date_to required")
	}
	companyINN := strings.TrimSpace(q.CompanyINN)
	if entityNameForINN(companyINN) == "" {
		return nil, errors.New("debt.arh.Drilldown: unknown company_inn")
	}
	to := asDate(q.DateTo)
	closeDt, _, err := r.snapshotDates(ctx, []string{companyINN}, to, to, false)
	if err != nil {
		return nil, err
	}
	if err := checkArhSnapshots(closeDt, "", to, to); err != nil {
		return nil, err
	}
	body, err := r.post(ctx, arhDrilldownQuery(
		companyINN, strings.TrimSpace(q.PartnerINN), strings.TrimSpace(q.Account),
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
