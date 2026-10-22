package debt

import (
	"bytes"
	"context"
	"errors"
	"log"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
)

// arhServer — httptest-«ClickHouse»: отвечает по виду запроса.
func arhServer(t *testing.T, snap, debt, sales, drill string, salesStatus int) (*httptest.Server, *[]string) {
	t.Helper()
	var seen []string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		b := readBodyFull(r)
		seen = append(seen, b)
		switch {
		case strings.Contains(b, "FROM finance.currency_daily"):
			_, _ = w.Write([]byte("42\n"))
		case strings.Contains(b, "HAVING dateDiff('day'"):
			// По умолчанию все выбранные ЮЛ имеют свежий снэпшот.
			_, _ = w.Write(nil)
		case strings.Contains(b, "SELECT toString(maxIf(snapshot_date"):
			_, _ = w.Write([]byte(snap))
		case strings.Contains(b, "FROM finance.fact_arh_sales"):
			if salesStatus != 0 {
				http.Error(w, "Table finance.fact_arh_sales doesn't exist", salesStatus)
				return
			}
			_, _ = w.Write([]byte(sales))
		case strings.Contains(b, "GROUP BY doc_id, currency_kod"):
			_, _ = w.Write([]byte(drill))
		default:
			_, _ = w.Write([]byte(debt))
		}
	}))
	t.Cleanup(srv.Close)
	return srv, &seen
}

func readBodyFull(r *http.Request) string {
	var sb strings.Builder
	b := make([]byte, 8192)
	for {
		n, err := r.Body.Read(b)
		sb.Write(b[:n])
		if err != nil {
			break
		}
	}
	return sb.String()
}

// Запрос свода: группировка до договора, остатки close/open, корзины на close_dt,
// фильтры ЮЛ/счетов; для линзы «В валюте договора» курсы не подключаются.
func TestArhReportQuery_Contract(t *testing.T) {
	q := arhReportQuery([]string{"690591512"}, []string{"62"}, "2026-09-30", "2026-08-31", LensContract)
	for _, want := range []string{
		"FROM finance.fact_arh_debt FINAL",
		"maxIf(snapshot_date, snapshot_date <= toDate('2026-09-30')) AS close_dt",
		"maxIf(snapshot_date, snapshot_date <= toDate('2026-08-31')) AS open_dt",
		"snapshot_date = close_dt OR snapshot_date = open_dt",
		"acc != '__snapshot__'",
		"company_id IN ('690591512')", "acc_root IN ('62')",
		"if(contract_id != '', contract_id, doc_id) AS contract_key",
		"GROUP BY company_id, counterparty_id, acc, acc_root, contract_key, cur_kod",
		"sumIf(s30_d, is_close)", "sumIf(s999_k, is_close)",
		"currency_kod AS cur_kod",
	} {
		if !strings.Contains(q, want) {
			t.Errorf("в запросе нет %q:\n%s", want, q)
		}
	}
	if strings.Contains(q, "currency_daily") {
		t.Errorf("в линзе «В валюте договора» курсы не нужны:\n%s", q)
	}
}

// BYN/USD: курс на ДАТУ СНЭПШОТА (ASOF по snapshot_date), BYN-строка без курса (933 → 1),
// USD = BYN / курс USD; валюта схлопывается (cur_kod = 0).
func TestArhReportQuery_Lenses(t *testing.T) {
	byn := arhReportQuery(nil, nil, "2026-09-30", "", LensBYN)
	for _, want := range []string{
		"snapshot_date AS rate_date", "ASOF LEFT JOIN finance.currency_daily rSrc ON rSrc.kod = f.currency_kod AND f.rate_date >= rSrc.date",
		"multiIf(f.currency_kod = 933, toFloat64(1), toFloat64(rSrc.curr_rate))", "toUInt32(0) AS cur_kod",
		"toDate('1970-01-01')", // нет открытого снэпшота — заглушка, суммы нулевые
	} {
		if !strings.Contains(byn, want) {
			t.Errorf("BYN: нет %q:\n%s", want, byn)
		}
	}
	if strings.Contains(byn, "nullIf(toFloat64(rUsd") {
		t.Errorf("BYN не делит на курс USD")
	}
	usd := arhReportQuery(nil, nil, "2026-09-30", "2026-08-31", LensUSD)
	if !strings.Contains(usd, "/ nullIf(toFloat64(rUsd.curr_rate), 0)") {
		t.Errorf("USD должен делить на курс USD:\n%s", usd)
	}
	if strings.Contains(byn, " AND company_id IN (") || strings.Contains(byn, " AND acc_root IN (") {
		t.Errorf("без фильтров не должно быть фильтрующих IN-условий")
	}
}

// Маппинг свода: КЗ в источнике отрицательна → положительный долг; договор = название
// договора-родителя; корзины ДЗ/КЗ, «без просрочки» = остаток − Σ корзин; подпись валюты по коду.
func TestArhReport_Mapping(t *testing.T) {
	debt := `{"company_id":"6950135110","company":"","counterparty_id":"690591512","counterparty":"МФ","acc":"62.01","acc_root":"62","contract_key":"f931-ctr","contract_label":"Договор поставки 1.15. от 01.02.2023","delay":90,"cur_kod":643,` +
		`"close_dz":"1000","close_kz":"-500","open_dz":"400","open_kz":"-100","dz30":"100","dz60":"200","dz90":"50","dz999":"150","kz30":"-50","kz60":"-25","kz90":"0","kz999":"-125"}` + "\n"
	srv, seen := arhServer(t, "2026-09-30\t2026-08-31\n", debt, "", "", 0)
	repo, err := NewArhCHRepo(srv.URL, "u", "p")
	if err != nil || repo == nil {
		t.Fatalf("NewArhCHRepo: %v", err)
	}
	rows, err := repo.Report(context.Background(), Filters{DateFrom: date(2026, 9, 1), DateTo: date(2026, 9, 30)})
	if err != nil {
		t.Fatalf("Report: %v", err)
	}
	if len(rows) != 1 {
		t.Fatalf("ожидалась 1 строка (продаж нет), получено %d", len(rows))
	}
	r := rows[0]
	if r.Contract != "Договор поставки 1.15. от 01.02.2023" || r.ContractRef != "f931-ctr" || r.PaymentTermDays != 90 {
		t.Errorf("договор: %q ref=%q term=%d", r.Contract, r.ContractRef, r.PaymentTermDays)
	}
	if r.Currency != "RUB" {
		t.Errorf("Currency = %q, ожид. RUB (по коду 643)", r.Currency)
	}
	if r.Company == "" || r.Country != CountryRF {
		t.Errorf("имя ЮЛ берётся из seed: %q / %q", r.Company, r.Country)
	}
	if !approx(r.ClosingDZ, 1000) || !approx(r.ClosingKZ, 500) || !approx(r.OpeningDZ, 400) || !approx(r.OpeningKZ, 100) {
		t.Errorf("остатки: %+v", r)
	}
	if !approx(r.TurnoverDZ, 600) || !approx(r.TurnoverKZ, 400) {
		t.Errorf("обороты ДЗ/КЗ = %v / %v", r.TurnoverDZ, r.TurnoverKZ)
	}
	if !approx(r.AgingDZ.Gt90, 150) || !approx(r.AgingDZ.Current, 500) {
		t.Errorf("AgingDZ = %+v", r.AgingDZ)
	}
	if !approx(r.AgingKZ.Lt30, 50) || !approx(r.AgingKZ.Gt90, 125) || !approx(r.AgingKZ.Current, 300) {
		t.Errorf("AgingKZ = %+v", r.AgingKZ)
	}
	// Границы снэпшотов подставлены в запрос свода после проверки свежести по ЮЛ.
	var reportQuerySeen bool
	for _, q := range *seen {
		if strings.Contains(q, "toDate('2026-09-30')) AS close_dt") && strings.Contains(q, "toDate('2026-08-31')) AS open_dt") {
			reportQuerySeen = true
			break
		}
	}
	if !reportQuerySeen {
		t.Errorf("запрос свода не получил даты снэпшотов: %v", *seen)
	}
}

// Линза BYN: подпись валюты фиксирована независимо от кода.
func TestArhReport_LensCurrencyLabel(t *testing.T) {
	debt := `{"company_id":"690591512","company":"МФ","counterparty_id":"1","counterparty":"К","acc":"62.1","acc_root":"62","contract_key":"","contract_label":"","delay":0,"cur_kod":0,"close_dz":"10","close_kz":"0","open_dz":"0","open_kz":"0"}` + "\n"
	srv, _ := arhServer(t, "2026-09-30\t1970-01-01\n", debt, "", "", 0)
	repo, _ := NewArhCHRepo(srv.URL, "u", "p")
	rows, err := repo.Report(context.Background(), Filters{DateFrom: date(2026, 9, 1), DateTo: date(2026, 9, 30), Lens: LensUSD})
	if err != nil || len(rows) != 1 {
		t.Fatalf("rows=%d err=%v", len(rows), err)
	}
	if rows[0].Currency != "USD" || rows[0].Contract != "" {
		t.Errorf("Currency=%q Contract=%q", rows[0].Currency, rows[0].Contract)
	}
}

// Продажи/возвраты: счета выручки по странам, период и прошлый месяц, возвраты отдельно,
// курс на дату события; фильтр счетов отчёта на них не действует.
func TestArhSalesQuery(t *testing.T) {
	q := arhSalesQuery([]string{"6950135110"}, "2026-09-01", "2026-09-30", LensBYN)
	for _, want := range []string{
		"FROM finance.fact_arh_sales FINAL", "acc_root = '90'", "acc_root = '6010'", "acc_root = '9010'",
		"company_id IN ('6950135110')",
		"event_date >= toDate('2026-09-01') AND event_date <= toDate('2026-09-30')",
		"event_date >= addMonths(toStartOfMonth(toDate('2026-09-30')), -1) AND event_date < toStartOfMonth(toDate('2026-09-30'))",
		"eff_sign > 0", "-sumIf(s,", "eff_sign < 0",
		"event_date AS rate_date", "ASOF LEFT JOIN finance.currency_daily rSrc",
	} {
		if !strings.Contains(q, want) {
			t.Errorf("в запросе продаж нет %q:\n%s", want, q)
		}
	}
	if strings.Contains(q, "acc_root IN") {
		t.Errorf("фильтр счетов отчёта не должен применяться к продажам")
	}
	c := arhSalesQuery(nil, "2026-09-01", "2026-09-30", LensContract)
	if strings.Contains(c, "currency_daily") || !strings.Contains(c, "currency_kod AS cur_kod") {
		t.Errorf("линза «В валюте договора»: курсы не нужны, валюта — измерение:\n%s", c)
	}
}

func TestArhReport_SalesAndReturnsMerged(t *testing.T) {
	debt := `{"company_id":"6950135110","company":"","counterparty_id":"690591512","counterparty":"МФ","acc":"62.01","acc_root":"62","contract_key":"c","contract_label":"Д","delay":0,"cur_kod":643,"close_dz":"10","close_kz":"0","open_dz":"0","open_kz":"0"}` + "\n"
	sales := `{"company_id":"6950135110","company":"","counterparty_id":"690591512","counterparty":"МФ","acc":"90.01.1","cur_kod":643,"revenue_period":"14000","revenue_last_month":"9000","returns_period":"1500","returns_last_month":"0"}` + "\n"
	srv, _ := arhServer(t, "2026-09-30\t2026-08-31\n", debt, sales, "", 0)
	repo, _ := NewArhCHRepo(srv.URL, "u", "p")
	rows, err := repo.Report(context.Background(), Filters{DateFrom: date(2026, 9, 1), DateTo: date(2026, 9, 30)})
	if err != nil || len(rows) != 2 {
		t.Fatalf("ожидались долг + продажи: rows=%d err=%v", len(rows), err)
	}
	s := rows[1]
	if s.Account != "90.01.1" || s.Contract != "" || s.Currency != "RUB" || s.Country != CountryRF {
		t.Errorf("строка продаж: %+v", s)
	}
	if !approx(s.RevenuePeriod, 14000) || !approx(s.RevenueLastMonth, 9000) {
		t.Errorf("продажи = %v / %v", s.RevenuePeriod, s.RevenueLastMonth)
	}
	if !approx(s.ReturnsPeriod, 1500) || s.ReturnsLastMonth != 0 {
		t.Errorf("возвраты = %v / %v", s.ReturnsPeriod, s.ReturnsLastMonth)
	}
	if s.ClosingDZ != 0 || s.ClosingKZ != 0 {
		t.Errorf("строка продаж не несёт сальдо")
	}
}

// fact_arh_sales ещё нет (миграция/поток) → отчёт по долгу жив; пустая fact_arh_debt → пусто.
func TestArhReport_SalesUnavailableDegrades(t *testing.T) {
	debt := `{"company_id":"690591512","company":"МФ","counterparty_id":"1","counterparty":"К","acc":"62.1","acc_root":"62","contract_key":"","contract_label":"","delay":0,"cur_kod":933,"close_dz":"10","close_kz":"0","open_dz":"0","open_kz":"0"}` + "\n"
	srv, _ := arhServer(t, "2026-09-30\t2026-08-31\n", debt, "", "", http.StatusNotFound)
	repo, _ := NewArhCHRepo(srv.URL, "u", "p")
	rows, err := repo.Report(context.Background(), Filters{DateFrom: date(2026, 9, 1), DateTo: date(2026, 9, 30)})
	if err != nil || len(rows) != 1 {
		t.Fatalf("долг должен вернуться без продаж: rows=%d err=%v", len(rows), err)
	}

	empty, _ := arhServer(t, "1970-01-01\t1970-01-01\n", "", "", "", 0)
	repo2, _ := NewArhCHRepo(empty.URL, "u", "p")
	_, err = repo2.Report(context.Background(), Filters{DateFrom: date(2026, 9, 1), DateTo: date(2026, 9, 30)})
	var ns *NoSnapshotError
	if !errors.As(err, &ns) {
		t.Errorf("без снэпшотов — понятная ошибка, а не пустой отчёт: %v", err)
	}
}

func TestArhDrilldownQuery(t *testing.T) {
	q := arhDrilldownQuery("6950135110", "690591512", "62.01", "ctr-1", "2026-09-30", LensContract, 643)
	for _, want := range []string{
		"snapshot_date = toDate('2026-09-30')", "company_id = '6950135110'", "counterparty_id = '690591512'", "acc = '62.01'",
		"if(contract_id != '', contract_id, doc_id) = 'ctr-1'", "currency_kod = 643", "max(payment_date) AS payment_date",
	} {
		if !strings.Contains(q, want) {
			t.Errorf("drilldown: нет %q:\n%s", want, q)
		}
	}
	noRef := arhDrilldownQuery("a", "b", "62.01", "", "2026-09-30", LensBYN, 643)
	if strings.Contains(noRef, "contract_id != ''") && strings.Contains(noRef, "= '") && strings.Contains(noRef, "doc_id) =") {
		t.Errorf("без договора не должно быть фильтра по ключу:\n%s", noRef)
	}
	if strings.Contains(noRef, "currency_kod = 643") {
		t.Errorf("в линзе BYN фильтра по валюте нет")
	}
	if !strings.Contains(noRef, "ASOF LEFT JOIN finance.currency_daily rSrc") {
		t.Errorf("в линзе BYN документы пересчитываются по курсу:\n%s", noRef)
	}
	if !strings.Contains(arhDrilldownQuery("a'b", "b", "x", "", "2026-09-30", LensContract, 0), "company_id = 'a''b'") {
		t.Errorf("значения должны экранироваться")
	}
}

// Документы договора: срок оплаты из Docs.PaymentDate, просрочка = дата отчёта − срок,
// не отрицательная; КЗ переворачивается; без срока просрочки нет.
func TestArhDrilldown_Mapping(t *testing.T) {
	drill := `{"doc_id":"d1","doc_number":"ТДБП-008592","doc_date":"2026-09-11","doc_type":"Реализация (акт, накладная, УПД)","contract_title":"Договор поставки 1.15.","payment_date":"2026-09-10","sum_d":"1500.5","sum_k":"0"}` + "\n" +
		`{"doc_id":"d2","doc_number":"","doc_date":"1970-01-01","doc_type":"","contract_title":"","payment_date":"2026-12-10","sum_d":"0","sum_k":"-77"}` + "\n" +
		`{"doc_id":"d3","doc_number":"Н3","doc_date":"2026-09-01","doc_type":"Оказание услуг","contract_title":"","payment_date":"","sum_d":"5","sum_k":"0"}` + "\n"
	srv, _ := arhServer(t, "2026-09-30\t1970-01-01\n", "", "", drill, 0)
	repo, _ := NewArhCHRepo(srv.URL, "u", "p")
	docs, err := repo.Drilldown(context.Background(), DrilldownQuery{
		CompanyINN: "6950135110", PartnerINN: "690591512", Account: "62.01", Contract: "ctr-1", Currency: "RUB", DateTo: date(2026, 9, 30),
	})
	if err != nil || len(docs) != 3 {
		t.Fatalf("docs=%d err=%v", len(docs), err)
	}
	d := docs[0]
	if d.DocNumber != "ТДБП-008592" || d.DocKind != "Дебиторская" || d.TransGroup != "Реализация (акт, накладная, УПД)" {
		t.Errorf("док 1: %+v", d)
	}
	if d.PaymentDueDate != date(2026, 9, 10) || d.OverdueDays != 20 {
		t.Errorf("срок %v просрочка %d, ожид. 2026-09-10 / 20", d.PaymentDueDate, d.OverdueDays)
	}
	if d.DocDate != date(2026, 9, 11) || !approx(d.DZChange, 1500.5) {
		t.Errorf("док 1 дата/сумма: %v %v", d.DocDate, d.DZChange)
	}
	k := docs[1]
	if k.DocNumber != "d2" || k.DocKind != "Кредиторская" || !approx(k.KZChange, 77) || k.OverdueDays != 0 || !k.DocDate.IsZero() {
		t.Errorf("док 2 (КЗ, срок в будущем): %+v", k)
	}
	if docs[2].OverdueDays != 0 || !docs[2].PaymentDueDate.IsZero() || docs[2].TransGroup != "Оказание услуг" {
		t.Errorf("док 3 (без срока): %+v", docs[2])
	}
}

func TestIsoCurrency(t *testing.T) {
	for kod, want := range map[int]string{643: "RUB", 933: "BYN", 840: "USD", 978: "EUR", 156: "CNY", 398: "KZT", 860: "UZS", 999: "999"} {
		if got := isoCurrencyName(kod); got != want {
			t.Errorf("isoCurrencyName(%d) = %q, ожид. %q", kod, got, want)
		}
	}
	if isoCurrencyKod("rub") != 643 || isoCurrencyKod("XXX") != 0 {
		t.Errorf("isoCurrencyKod")
	}
}

// Нетто по DocID (как у FinDebt): РБ/РФ/УЗ сворачиваются внутри счёта, КЗ — между
// счетами с назначением итога счёту стороны своего знака. Корзины неттируются в том же
// зерне; строки без doc_id сохраняют исходный счёт и не взаимозачитываются.
func TestArhReportQuery_NetByDocID(t *testing.T) {
	q := arhReportQuery(nil, nil, "2026-09-30", "2026-08-31", LensContract)
	for _, want := range []string{
		"GROUP BY r.snapshot_date, r.company_id, r.counterparty_id, r.doc_id, r.net_acc, r.currency_kod",
		"if(r.doc_id = '' OR NOT (r.company_id IN ('141240004842')), r.net_acc",
		"argMaxIf(r.acc, tuple(r.sum_d, r.acc), r.sum_d > 0)",
		"argMaxIf(r.acc, tuple(-r.sum_k, r.acc), r.sum_k < 0)",
		"if(r.doc_id = '', sum(r.sum_d), if((sum(r.sum_d) + sum(r.sum_k)) > 0, (sum(r.sum_d) + sum(r.sum_k)), toDecimal64(0, 2))) AS sum_d",
		"if(r.doc_id = '', sum(r.sum999_d), if((sum(r.sum_d) + sum(r.sum_k)) > 0 AND",
		"(sum(r.sum30_d) + sum(r.sum30_k)) >= toDecimal64(0, 2) AND (sum(r.sum60_d) + sum(r.sum60_k)) >= toDecimal64(0, 2)",
		"if(r.doc_id = '', sum(r.sum30_k), if((sum(r.sum_d) + sum(r.sum_k)) < 0 AND",
		"(sum(r.sum90_d) + sum(r.sum90_k)) <= toDecimal64(0, 2) AND (sum(r.sum999_d) + sum(r.sum999_k)) <= toDecimal64(0, 2)",
		"if(doc_id = '' OR NOT (company_id IN ('141240004842')), acc, '') AS net_acc",
	} {
		if !strings.Contains(q, want) {
			t.Errorf("нет выражения нетто %q:\n%s", want, q)
		}
	}
	// Фильтр счёта применяется после межсчетного нетто, иначе КЗ снова раздуется.
	filtered := arhReportQuery(nil, []string{"3310"}, "2026-09-30", "2026-08-31", LensContract)
	if netAt, filterAt := strings.Index(filtered, "argMaxIf(r.acc"), strings.Index(filtered, "acc_root IN ('3310')"); netAt < 0 || filterAt < 0 || filterAt < netAt {
		t.Errorf("фильтр счёта применён до нетто:\n%s", filtered)
	}
	// drill-down считает документ тем же подзапросом, затем выбирает назначенный счёт.
	dq := arhDrilldownQuery("a", "b", "6315", "", "2026-09-30", LensContract, 0)
	if !strings.Contains(dq, "argMaxIf(r.acc, tuple(r.sum_d, r.acc), r.sum_d > 0)") ||
		!strings.Contains(dq, "WHERE acc = '6315'") ||
		strings.Index(dq, "WHERE acc = '6315'") < strings.Index(dq, "argMaxIf(r.acc") {
		t.Errorf("drill-down обязан использовать то же нетто:\n%s", dq)
	}
}

// Курс не найден: строка не теряется (HAVING по родным суммам), в лог — одно сообщение
// со списком валют/дат; пустая currency_daily — отдельное явное сообщение.
func TestArhReport_MissingRateKeepsRowAndLogs(t *testing.T) {
	q := arhReportQuery(nil, nil, "2026-09-30", "2026-08-31", LensBYN)
	if !strings.Contains(q, "HAVING sumIf(abs(n_d) + abs(n_k), is_close OR is_open)") {
		t.Errorf("HAVING должен смотреть на родные суммы:\n%s", q)
	}
	if !strings.Contains(arhSalesQuery(nil, "2026-09-01", "2026-09-30", LensBYN), "HAVING sumIf(abs(n),") {
		t.Errorf("продажи: HAVING по родным суммам")
	}
	var logBuf bytes.Buffer
	log.SetOutput(&logBuf)
	defer log.SetOutput(os.Stderr)

	debt := `{"company_id":"6950135110","company":"","counterparty_id":"1","counterparty":"К","acc":"62.1","acc_root":"62","contract_key":"c","contract_label":"Д","delay":0,"cur_kod":0,"close_dz":"0","close_kz":"0","open_dz":"0","open_kz":"0"}` + "\n"
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		b := readBody(r)
		switch {
		case strings.Contains(b, "FROM finance.currency_daily"):
			_, _ = w.Write([]byte("42\n"))
		case strings.Contains(b, "HAVING dateDiff('day'"):
			_, _ = w.Write(nil)
		case strings.Contains(b, "SELECT toString(maxIf(snapshot_date"):
			_, _ = w.Write([]byte("2026-09-30\t2026-08-31\n"))
		case strings.Contains(b, "SELECT DISTINCT concat") && strings.Contains(b, "fact_arh_debt"):
			_, _ = w.Write([]byte("643@2026-09-30\n"))
		case strings.Contains(b, "FROM finance.fact_arh_sales"):
			http.Error(w, "no table", http.StatusNotFound)
		default:
			_, _ = w.Write([]byte(debt))
		}
	}))
	defer srv.Close()
	repo, _ := NewArhCHRepo(srv.URL, "u", "p")
	rows, err := repo.Report(context.Background(), Filters{DateFrom: date(2026, 9, 1), DateTo: date(2026, 9, 30), Lens: LensBYN})
	if err != nil || len(rows) != 1 {
		t.Fatalf("строка без курса не должна теряться: rows=%d err=%v", len(rows), err)
	}
	if !strings.Contains(logBuf.String(), "нет курса") || !strings.Contains(logBuf.String(), "RUB@2026-09-30") {
		t.Errorf("в логе нет списка валют без курса: %q", logBuf.String())
	}

	// пустая currency_daily — явное сообщение
	logBuf.Reset()
	empty := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		b := readBody(r)
		switch {
		case strings.Contains(b, "FROM finance.currency_daily"):
			_, _ = w.Write([]byte("0\n"))
		case strings.Contains(b, "HAVING dateDiff('day'"):
			_, _ = w.Write(nil)
		case strings.Contains(b, "SELECT toString(maxIf(snapshot_date"):
			_, _ = w.Write([]byte("2026-09-30\t2026-08-31\n"))
		case strings.Contains(b, "FROM finance.fact_arh_sales"):
			http.Error(w, "no table", http.StatusNotFound)
		default:
			_, _ = w.Write([]byte(debt))
		}
	}))
	defer empty.Close()
	repo2, _ := NewArhCHRepo(empty.URL, "u", "p")
	if _, err := repo2.Report(context.Background(), Filters{DateFrom: date(2026, 9, 1), DateTo: date(2026, 9, 30), Lens: LensUSD}); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(logBuf.String(), "currency_daily ПУСТА") {
		t.Errorf("нет сообщения про пустую currency_daily: %q", logBuf.String())
	}
}

// Дата вне окна ежедневных снэпшотов: ближайший снэпшот дальше 3 дней — ошибка 422 с
// понятным текстом, а не молчаливая подмена концом месяца.
func TestArhReport_SnapshotTooFarIsError(t *testing.T) {
	srv, _ := arhServer(t, "2026-08-31\t2026-07-31\n", "", "", "", 0)
	repo, _ := NewArhCHRepo(srv.URL, "u", "p")
	_, err := repo.Report(context.Background(), Filters{DateFrom: date(2026, 9, 1), DateTo: date(2026, 9, 15)})
	var ns *NoSnapshotError
	if !errors.As(err, &ns) || !strings.Contains(err.Error(), "нет снэпшота на дату 15.09.2026; ближайший: 31.08.2026") {
		t.Fatalf("ожидалась NoSnapshotError, получено: %v", err)
	}
	if errStatus(err) != http.StatusUnprocessableEntity || errStatus(errors.New("x")) != http.StatusBadRequest {
		t.Errorf("errStatus")
	}
	// открытый снэпшот слишком далеко от date_from
	srv2, _ := arhServer(t, "2026-09-30\t2026-07-31\n", "", "", "", 0)
	repo2, _ := NewArhCHRepo(srv2.URL, "u", "p")
	_, err = repo2.Report(context.Background(), Filters{DateFrom: date(2026, 9, 1), DateTo: date(2026, 9, 30)})
	if !errors.As(err, &ns) || !strings.Contains(err.Error(), "на начало периода") {
		t.Errorf("open_dt далеко от date_from: %v", err)
	}
	// 1–3 дня — нормально (выходные)
	one := `{"company_id":"690591512","company":"МФ","counterparty_id":"1","counterparty":"К","acc":"62.1","acc_root":"62","contract_key":"","contract_label":"","delay":0,"cur_kod":933,"close_dz":"5","close_kz":"0","open_dz":"0","open_kz":"0"}` + "\n"
	srv3, _ := arhServer(t, "2026-09-27\t2026-08-31\n", one, "", "", http.StatusNotFound)
	repo3, _ := NewArhCHRepo(srv3.URL, "u", "p")
	if rows, err := repo3.Report(context.Background(), Filters{DateFrom: date(2026, 9, 1), DateTo: date(2026, 9, 30)}); err != nil || len(rows) != 1 {
		t.Errorf("снэпшот в пределах 3 дней: rows=%d err=%v", len(rows), err)
	}
}

// Снэпшоты ищутся с учётом выбранных ЮЛ.
func TestArhSnapshotDatesQuery_CompanyFilter(t *testing.T) {
	q := arhSnapshotDatesQuery([]string{"305554644"}, "2026-09-01", "2026-09-30")
	if !strings.Contains(q, "AND company_id IN ('305554644')") {
		t.Errorf("нет фильтра по ЮЛ:\n%s", q)
	}
	if strings.Contains(arhSnapshotDatesQuery(nil, "2026-09-01", "2026-09-30"), "company_id") {
		t.Errorf("без ЮЛ фильтра быть не должно")
	}
}

// Частичный провал источника нельзя скрывать свежим снэпшотом другого ЮЛ: при отчёте
// сразу по нескольким компаниям каждая ранее встречавшаяся компания проверяется отдельно.
func TestArhReport_StaleCompanySnapshotIsError(t *testing.T) {
	var queries []string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		b := readBody(r)
		queries = append(queries, b)
		switch {
		case strings.Contains(b, "HAVING dateDiff('day'"):
			_, _ = w.Write([]byte("6950135110\t2026-10-14\t2026-10-13\n9909349268\t2026-10-14\t2026-10-13\n"))
		case strings.Contains(b, "SELECT toString(maxIf(snapshot_date"):
			_, _ = w.Write([]byte("2026-10-21\t2026-10-13\n"))
		default:
			http.Error(w, "unexpected query", http.StatusBadRequest)
		}
	}))
	t.Cleanup(srv.Close)
	repo, _ := NewArhCHRepo(srv.URL, "u", "p")
	_, err := repo.Report(context.Background(), Filters{
		DateFrom: date(2026, 10, 14), DateTo: date(2026, 10, 21),
		EntityINNs: []string{"690591512", "6950135110", "9909349268"},
	})
	var ns *NoSnapshotError
	if !errors.As(err, &ns) {
		t.Fatalf("ожидалась NoSnapshotError, получено: %v", err)
	}
	for _, want := range []string{"6950135110", "9909349268", "14.10.2026", "21.10.2026"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("в ошибке нет %q: %v", want, err)
		}
	}
	if len(queries) != 2 || !strings.Contains(queries[1], "company_id IN ('690591512','6950135110','9909349268')") {
		t.Errorf("проверка свежести должна учитывать все выбранные ЮЛ: %v", queries)
	}
}

// «(без договора)»: пустой ключ получает явный маркер, drill-down по маркеру фильтрует
// пустой contract_key, пустой contract — без фильтра (старые ссылки), пустой partner разрешён.
func TestArhNoContractMarker(t *testing.T) {
	debt := `{"company_id":"6950135110","company":"","counterparty_id":"","counterparty":"Без УНП","acc":"62.1","acc_root":"62","contract_key":"","contract_label":"","delay":0,"cur_kod":643,"close_dz":"7","close_kz":"0","open_dz":"0","open_kz":"0"}` + "\n"
	srv, _ := arhServer(t, "2026-09-30\t2026-08-31\n", debt, "", "", http.StatusNotFound)
	repo, _ := NewArhCHRepo(srv.URL, "u", "p")
	rows, err := repo.Report(context.Background(), Filters{DateFrom: date(2026, 9, 1), DateTo: date(2026, 9, 30)})
	if err != nil || len(rows) != 1 || rows[0].ContractRef != arhNoContractRef {
		t.Fatalf("маркер пустого ключа: rows=%+v err=%v", rows, err)
	}
	q := arhDrilldownQuery("6950135110", "", "62.1", arhNoContractRef, "2026-09-30", LensContract, 0)
	if !strings.Contains(q, "AND if(contract_id != '', contract_id, doc_id) = ''") || !strings.Contains(q, "counterparty_id = ''") {
		t.Errorf("drill-down «без договора» / пустой партнёр:\n%s", q)
	}
	if strings.Contains(arhDrilldownQuery("a", "b", "62.1", "", "2026-09-30", LensContract, 0), "contract_id, doc_id) =") {
		t.Errorf("пустой contract — без фильтра по договору")
	}
	drill := `{"doc_id":"d1","doc_number":"Н1","doc_date":"2026-09-01","doc_type":"","contract_title":"","payment_date":"","sum_d":"7","sum_k":"0"}` + "\n"
	srv2, _ := arhServer(t, "2026-09-30\t1970-01-01\n", "", "", drill, 0)
	repo2, _ := NewArhCHRepo(srv2.URL, "u", "p")
	docs, err := repo2.Drilldown(context.Background(), DrilldownQuery{CompanyINN: "6950135110", PartnerINN: "", Account: "62.1", Contract: arhNoContractRef, DateTo: date(2026, 9, 30)})
	if err != nil || len(docs) != 1 {
		t.Errorf("пустой partner_inn не должен давать ошибку: docs=%d err=%v", len(docs), err)
	}
}

// Знак продаж по стране: для КЗ продажа в источнике — минус, знак инвертируется.
func TestArhSalesQuery_KZSignInverted(t *testing.T) {
	q := arhSalesQuery(nil, "2026-09-01", "2026-09-30", LensContract)
	kz := "company_id IN ('141240004842')"
	for _, want := range []string{
		"if(" + kz + ", -amount_sign, amount_sign) AS eff_sign",
		"sum(if(" + kz + ", -amount, amount)) AS v",
		"eff_sign > 0", "eff_sign < 0",
	} {
		if !strings.Contains(q, want) {
			t.Errorf("нет %q:\n%s", want, q)
		}
	}
}

// Корзины нулевые, а остаток нет (снэпшоты findebt до повторного bootstrap / нет просрочки):
// сервер данные не скрывает — «Без просрочки» = весь остаток, решение о показе — на фронте.
func TestArhReport_ZeroBucketsKeepCurrent(t *testing.T) {
	debt := `{"company_id":"690591512","company":"МФ","counterparty_id":"1","counterparty":"К","acc":"62.1","acc_root":"62","contract_key":"c","contract_label":"Д","delay":0,"cur_kod":933,"close_dz":"100","close_kz":"-40","open_dz":"0","open_kz":"0","dz30":"0","dz60":"0","dz90":"0","dz999":"0","kz30":"0","kz60":"0","kz90":"0","kz999":"0"}` + "\n"
	srv, _ := arhServer(t, "2026-09-30\t2026-08-31\n", debt, "", "", http.StatusNotFound)
	repo, _ := NewArhCHRepo(srv.URL, "u", "p")
	rows, _ := repo.Report(context.Background(), Filters{DateFrom: date(2026, 9, 1), DateTo: date(2026, 9, 30)})
	if len(rows) != 1 || !approx(rows[0].AgingDZ.Current, 100) || !approx(rows[0].AgingKZ.Current, 40) || rows[0].AgingDZ.Gt90 != 0 {
		t.Errorf("корзины: %+v", rows)
	}
}
