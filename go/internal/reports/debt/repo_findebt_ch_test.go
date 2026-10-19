package debt

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func date(y int, m time.Month, d int) time.Time {
	return time.Date(y, m, d, 0, 0, 0, 0, time.UTC)
}

func approx(a, b float64) bool {
	d := a - b
	if d < 0 {
		d = -d
	}
	return d < 0.005
}

// Регрессия бага 500 (ILLEGAL_AGGREGATION): acc_root должен быть обычной колонкой
// в GROUP BY, а не any(acc_root) — иначе WHERE acc_root IN (...) падает.
func TestFinDebtReportQuery_AccRootInGroupBy(t *testing.T) {
	q := findebtReportQuery([]string{"690591512"}, []string{"60"}, "2026-05-01", "2026-05-31", LensContract)
	if !strings.Contains(q, "GROUP BY company_id, counterparty_id, acc, acc_root, doc_number") {
		t.Errorf("acc_root должен быть в GROUP BY:\n%s", q)
	}
	if strings.Contains(q, "any(acc_root)") {
		t.Errorf("acc_root не должен агрегироваться (регрессия ILLEGAL_AGGREGATION):\n%s", q)
	}
	if !strings.Contains(q, "company_id IN ('690591512')") {
		t.Errorf("фильтр по ЮЛ отсутствует:\n%s", q)
	}
	if !strings.Contains(q, "acc_root IN ('60')") {
		t.Errorf("фильтр по счёту отсутствует:\n%s", q)
	}
	// Линза обязана быть в WHERE (иначе задвоение ×3), суммы — из sum_d/sum_k.
	if !strings.Contains(q, "cur_filter = 'В валюте договора'") {
		t.Errorf("фильтр линзы отсутствует:\n%s", q)
	}
	if !strings.Contains(q, "finance.fact_findebt_ccy") || strings.Contains(q, "sum_d_byn") {
		t.Errorf("должна читаться fact_findebt_ccy по sum_d/sum_k:\n%s", q)
	}
}

func TestFinDebtReportQuery_NoFilters(t *testing.T) {
	q := findebtReportQuery(nil, nil, "2026-05-01", "2026-05-31", LensBYN)
	// snapshot_date IN (...) остаётся всегда; пользовательских фильтров быть не должно.
	if strings.Contains(q, "company_id IN (") {
		t.Errorf("без фильтров не должно быть company_id IN:\n%s", q)
	}
	if strings.Contains(q, "acc_root IN (") {
		t.Errorf("без фильтров не должно быть acc_root IN:\n%s", q)
	}
	if !strings.Contains(q, "cur_filter = 'В бел. рублях'") {
		t.Errorf("линза BYN должна быть в WHERE:\n%s", q)
	}
}

// Маппинг свода: КЗ во вьюхе отрицательна → репо переворачивает в положительный
// долг; договор/отсрочка/оборот считаются корректно.
func TestFinDebtReport_Mapping(t *testing.T) {
	body := `{"company_id":"690591512","company":"ООО Марк Формэль","counterparty_id":"690719790","counterparty":"Формэль","acc":"60.01","acc_root":"60","doc_number":"1.18.","description":"1.18. от 20.01.2025","currency":"BYN","delay":30,"close_dz":"0","close_kz":"-155491.53","open_dz":"0","open_kz":"-150000"}` + "\n"
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.Contains(readBody(r), "fact_findebt_turnover") {
			return // пустая выручка
		}
		_, _ = w.Write([]byte(body))
	}))
	defer srv.Close()

	repo, err := NewFinDebtCHRepo(srv.URL, "u", "p")
	if err != nil || repo == nil {
		t.Fatalf("NewFinDebtCHRepo: %v", err)
	}
	rows, err := repo.Report(context.Background(), Filters{DateFrom: date(2026, 5, 1), DateTo: date(2026, 5, 31)})
	if err != nil {
		t.Fatalf("Report: %v", err)
	}
	if len(rows) != 1 {
		t.Fatalf("ожидалась 1 строка, получено %d", len(rows))
	}
	r := rows[0]
	if !approx(r.ClosingKZ, 155491.53) {
		t.Errorf("ClosingKZ = %v, ожид. 155491.53 (перевёрнутая в положительную)", r.ClosingKZ)
	}
	if !approx(r.OpeningKZ, 150000) {
		t.Errorf("OpeningKZ = %v, ожид. 150000", r.OpeningKZ)
	}
	if !approx(r.TurnoverKZ, 5491.53) {
		t.Errorf("TurnoverKZ = %v, ожид. 5491.53 (закрытие−открытие)", r.TurnoverKZ)
	}
	if r.Contract != "1.18. от 20.01.2025" {
		t.Errorf("Contract = %q", r.Contract)
	}
	if r.ContractRef != "1.18." {
		t.Errorf("ContractRef = %q", r.ContractRef)
	}
	if r.PaymentTermDays != 30 {
		t.Errorf("PaymentTermDays (отсрочка) = %d, ожид. 30", r.PaymentTermDays)
	}
	// Дефолтная линза «В валюте договора» → подпись = native-валюта строки (BYN).
	if r.Currency != "BYN" {
		t.Errorf("Currency = %q, ожид. BYN", r.Currency)
	}
	// Срок оплаты/просрочка на уровне договора не заполняются (ТЗ — только документ).
	if !r.PaymentDueDate.IsZero() || r.OverdueDays != 0 {
		t.Errorf("на уровне договора срок/просрочка должны быть пустыми")
	}
}

func TestFinDebtDrilldownQuery_DocFilter(t *testing.T) {
	with := findebtDrilldownQuery("a", "b", "60.01", "1.18.", "2026-05-31", LensContract)
	if !strings.Contains(with, "doc_number = '1.18.'") {
		t.Errorf("фильтр по № договора отсутствует:\n%s", with)
	}
	if !strings.Contains(with, "cur_filter = 'В валюте договора'") {
		t.Errorf("фильтр линзы отсутствует в drilldown:\n%s", with)
	}
	without := findebtDrilldownQuery("a", "b", "60.01", "", "2026-05-31", LensUSD)
	if strings.Contains(without, "doc_number =") {
		t.Errorf("без договора не должно быть фильтра по номеру:\n%s", without)
	}
	if !strings.Contains(without, "cur_filter = 'В долларах США'") {
		t.Errorf("линза USD должна быть в drilldown:\n%s", without)
	}
}

// Маппинг документа: срок оплаты (payment_date) и просрочка (day_delay) — по ТЗ
// здесь; КЗ переворачивается в положительную.
func TestFinDebtDrilldown_Mapping(t *testing.T) {
	body := `{"doc_number":"1.18.","doc_date":"2025-01-20","description":"1.18. от 20.01.2025","payment_date":"2025-02-19","day_delay":45,"delay":30,"sum_d":"0","sum_k":"-155491.53"}` + "\n"
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(body))
	}))
	defer srv.Close()

	repo, _ := NewFinDebtCHRepo(srv.URL, "u", "p")
	docs, err := repo.Drilldown(context.Background(), DrilldownQuery{
		CompanyINN: "690591512", PartnerINN: "690719790", Account: "60.01",
		Contract: "1.18.", DateTo: date(2026, 5, 31),
	})
	if err != nil {
		t.Fatalf("Drilldown: %v", err)
	}
	if len(docs) != 1 {
		t.Fatalf("ожидался 1 документ, получено %d", len(docs))
	}
	d := docs[0]
	if !approx(d.KZChange, 155491.53) {
		t.Errorf("KZChange = %v, ожид. 155491.53", d.KZChange)
	}
	if d.OverdueDays != 45 {
		t.Errorf("OverdueDays (просрочка) = %d, ожид. 45", d.OverdueDays)
	}
	if d.PaymentDueDate != date(2025, 2, 19) {
		t.Errorf("PaymentDueDate (дата оплаты) = %v, ожид. 2025-02-19", d.PaymentDueDate)
	}
	if d.DocKind != "Кредиторская" || d.DocNumber != "1.18." {
		t.Errorf("DocKind=%q DocNumber=%q", d.DocKind, d.DocNumber)
	}
}

// Корзины просрочки: запрос тянет sum*_d/k на снэпшоте закрытия, маппинг
// переворачивает КЗ в положительную и считает «без просрочки» = остаток − Σ корзин.
func TestFinDebtReportQuery_AgingColumns(t *testing.T) {
	q := findebtReportQuery(nil, nil, "2026-05-01", "2026-05-31", LensBYN)
	for _, want := range []string{
		"sumIf(sum30_d,  snapshot_date = close_dt)", "sumIf(sum999_d, snapshot_date = close_dt)",
		"sumIf(sum30_k,  snapshot_date = close_dt)", "sumIf(sum999_k, snapshot_date = close_dt)",
	} {
		if !strings.Contains(q, want) {
			t.Errorf("в запросе нет %q:\n%s", want, q)
		}
	}
}

func TestFinDebtReport_AgingMapping(t *testing.T) {
	body := `{"company_id":"690591512","company":"МФ","counterparty_id":"690719790","counterparty":"Формэль","acc":"62.1.1","acc_root":"62","doc_number":"1","description":"1","currency":"BYN","delay":0,` +
		`"close_dz":"1000","close_kz":"-500","open_dz":"0","open_kz":"0",` +
		`"dz30":"100","dz60":"200","dz90":"50","dz999":"150","kz30":"-50","kz60":"-25","kz90":"0","kz999":"-125"}` + "\n"
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.Contains(r.URL.Path+readBody(r), "fact_findebt_turnover") {
			return // пустой ответ по выручке
		}
		_, _ = w.Write([]byte(body))
	}))
	defer srv.Close()
	repo, _ := NewFinDebtCHRepo(srv.URL, "u", "p")
	rows, err := repo.Report(context.Background(), Filters{DateFrom: date(2026, 5, 1), DateTo: date(2026, 5, 31), Lens: LensBYN})
	if err != nil || len(rows) != 1 {
		t.Fatalf("Report: rows=%d err=%v", len(rows), err)
	}
	dz, kz := rows[0].AgingDZ, rows[0].AgingKZ
	if !approx(dz.Lt30, 100) || !approx(dz.D30to60, 200) || !approx(dz.D60to90, 50) || !approx(dz.Gt90, 150) {
		t.Errorf("AgingDZ корзины = %+v", dz)
	}
	if !approx(dz.Current, 500) { // 1000 − (100+200+50+150)
		t.Errorf("AgingDZ.Current = %v, ожид. 500", dz.Current)
	}
	// КЗ: корзины и остаток перевёрнуты в положительные; current = 500 − (50+25+0+125) = 300
	if !approx(kz.Lt30, 50) || !approx(kz.Gt90, 125) || !approx(kz.Current, 300) {
		t.Errorf("AgingKZ = %+v", kz)
	}
}

func readBody(r *http.Request) string {
	b := make([]byte, 4096)
	n, _ := r.Body.Read(b)
	return string(b[:n])
}

// Выручка: запрос — только счета выручки по странам, накопления по месяцам;
// фильтр счетов отчёта на неё не действует; строки приходят без договора.
func TestFinDebtRevenueQuery(t *testing.T) {
	q := findebtRevenueQuery([]string{"690591512"}, "2026-10-01", "2026-10-17", LensBYN)
	for _, want := range []string{
		"finance.fact_findebt_turnover FINAL",
		"acc_root = '90'",
		"acc_root = '6010'", // КЗ — привязка к ЮЛ КЗ
		"acc_root = '9010'", // УЗ
		"cur_filter = 'В бел. рублях'",
		"company_id IN ('690591512')",
		"argMax(k_wholesales + d_wholesales, snapshot_date)",
		"snapshot_date >= addMonths(toStartOfMonth(toDate('2026-10-17')), -1)",
	} {
		if !strings.Contains(q, want) {
			t.Errorf("в запросе выручки нет %q:\n%s", want, q)
		}
	}
	if strings.Contains(q, "acc_root IN") {
		t.Errorf("фильтр счетов отчёта не должен применяться к выручке:\n%s", q)
	}
}

func TestFinDebtReport_RevenueRowsMerged(t *testing.T) {
	debtBody := `{"company_id":"6950135110","company":"ТД","counterparty_id":"690591512","counterparty":"МФ","acc":"62.01","acc_root":"62","doc_number":"1","description":"1","currency":"RUB","delay":0,"close_dz":"10","close_kz":"0","open_dz":"0","open_kz":"0"}` + "\n"
	revBody := `{"company_id":"6950135110","company":"ТД","counterparty_id":"690591512","counterparty":"МФ","acc":"90.01.1","currency":"RUB","revenue_period":"14000","revenue_last_month":"9000"}` + "\n"
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.Contains(readBody(r), "fact_findebt_turnover") {
			_, _ = w.Write([]byte(revBody))
			return
		}
		_, _ = w.Write([]byte(debtBody))
	}))
	defer srv.Close()
	repo, _ := NewFinDebtCHRepo(srv.URL, "u", "p")
	rows, err := repo.Report(context.Background(), Filters{DateFrom: date(2026, 10, 1), DateTo: date(2026, 10, 17)})
	if err != nil || len(rows) != 2 {
		t.Fatalf("ожидались долг + выручка (2 строки): rows=%d err=%v", len(rows), err)
	}
	rev := rows[1]
	if rev.Account != "90.01.1" || rev.Contract != "" || rev.Country != CountryRF {
		t.Errorf("строка выручки: %+v", rev)
	}
	if !approx(rev.RevenuePeriod, 14000) || !approx(rev.RevenueLastMonth, 9000) {
		t.Errorf("выручка = %v / %v", rev.RevenuePeriod, rev.RevenueLastMonth)
	}
	if rev.ClosingDZ != 0 || rev.ClosingKZ != 0 {
		t.Errorf("строка выручки не должна нести сальдо")
	}
}

// Нет таблицы оборотов (миграция 013 не накатана) → отчёт по долгу жив, без выручки.
func TestFinDebtReport_RevenueUnavailableDegrades(t *testing.T) {
	debtBody := `{"company_id":"690591512","company":"МФ","counterparty_id":"690719790","counterparty":"Ф","acc":"62.1","acc_root":"62","doc_number":"1","description":"1","currency":"BYN","delay":0,"close_dz":"10","close_kz":"0","open_dz":"0","open_kz":"0"}` + "\n"
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.Contains(readBody(r), "fact_findebt_turnover") {
			http.Error(w, "Table finance.fact_findebt_turnover doesn't exist", http.StatusNotFound)
			return
		}
		_, _ = w.Write([]byte(debtBody))
	}))
	defer srv.Close()
	repo, _ := NewFinDebtCHRepo(srv.URL, "u", "p")
	rows, err := repo.Report(context.Background(), Filters{DateFrom: date(2026, 10, 1), DateTo: date(2026, 10, 17)})
	if err != nil || len(rows) != 1 {
		t.Fatalf("долг должен вернуться без выручки: rows=%d err=%v", len(rows), err)
	}
}
