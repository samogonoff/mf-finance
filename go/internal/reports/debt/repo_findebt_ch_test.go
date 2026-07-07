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
