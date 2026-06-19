package debt

import (
	"database/sql"
	"testing"
	"time"
)

func nstrL(s string) sql.NullString {
	if s == "" {
		return sql.NullString{}
	}
	return sql.NullString{String: s, Valid: true}
}

// Сценарий: «Реализация» — Dr 62.01 / Cr 90.01.1, две строки.
// account=62, kind=DZ → каждая строка увеличивает DZ.
func TestBuildDrilldown_SalesIncreasesDZ(t *testing.T) {
	d := time.Date(2026, 4, 15, 10, 0, 0, 0, time.UTC)
	raw := []drillRow{
		{
			Date: d, DocID: "DOC1", RwNm: 1, DrAcc: "62.01", CrAcc: "90.01.1", Amount: 1000,
			ObjectsName: "Реализация ТДБП-003950 от 15.04.2026 10:00:00",
		},
		{
			Date: d, DocID: "DOC1", RwNm: 2, DrAcc: "62.01", CrAcc: "90.01.1", Amount: 500,
			ObjectsName: "Реализация ТДБП-003950 от 15.04.2026 10:00:00",
		},
	}
	got := BuildDrilldown(raw, CountryRF, "62", time.Time{})
	if len(got) != 1 {
		t.Fatalf("один документ → одна строка, got %d", len(got))
	}
	r := got[0]
	if r.DZChange != 1500 {
		t.Errorf("DZChange = %v, want 1500", r.DZChange)
	}
	if r.KZChange != 0 {
		t.Errorf("KZChange = %v, want 0", r.KZChange)
	}
	if r.DocNumber != "ТДБП-003950" {
		t.Errorf("DocNumber = %q", r.DocNumber)
	}
	if r.DocKind != "Реализация" {
		t.Errorf("DocKind = %q, want 'Реализация'", r.DocKind)
	}
	want := time.Date(2026, 4, 15, 10, 0, 0, 0, time.UTC)
	if !r.DocDate.Equal(want) {
		t.Errorf("DocDate = %v, want %v", r.DocDate, want)
	}
}

// Сценарий: «Оплата покупателя» — Dr 51 / Cr 62.01. Уменьшение ДЗ.
func TestBuildDrilldown_PaymentDecreasesDZ(t *testing.T) {
	d := time.Date(2026, 4, 20, 0, 0, 0, 0, time.UTC)
	raw := []drillRow{
		{
			Date: d, DocID: "PAY1", RwNm: 1, DrAcc: "51", CrAcc: "62.01", Amount: 1500,
			Mapping: nstrL("Платёжное поручение ПП-0001 от 20.04.2026 (документ), Поступление на расчётный счёт (субконто)"),
		},
	}
	got := BuildDrilldown(raw, CountryRF, "62", time.Time{})
	if got[0].DZChange != -1500 {
		t.Errorf("Оплата должна уменьшить ДЗ: DZChange = %v, want -1500", got[0].DZChange)
	}
	if got[0].DocNumber != "ПП-0001" {
		t.Errorf("DocNumber из Mapping = %q", got[0].DocNumber)
	}
	if got[0].DocKind != "Платёжное поручение" {
		t.Errorf("DocKind = %q", got[0].DocKind)
	}
}

// Сценарий: «Поступление от поставщика» — Dr 44.01 / Cr 60.01.
// account=60, kind=KZ → Cr 60 увеличивает наш долг (KZChange > 0).
func TestBuildDrilldown_VendorBillIncreasesKZ(t *testing.T) {
	d := time.Date(2026, 4, 10, 8, 35, 32, 0, time.UTC)
	raw := []drillRow{
		{
			Date: d, DocID: "VB1", RwNm: 1, DrAcc: "44.01", CrAcc: "60.01", Amount: 8000,
			TransDescription: nstrL("Услуга по хранению товара по вх.д. 3692 от 10.04.2026"),
		},
	}
	got := BuildDrilldown(raw, CountryRF, "60", time.Time{})
	if got[0].KZChange != 8000 {
		t.Errorf("KZChange = %v, want 8000", got[0].KZChange)
	}
	if got[0].DZChange != 0 {
		t.Errorf("DZChange должен быть 0 для счёта КЗ")
	}
	if got[0].DocNumber != "3692" {
		t.Errorf("DocNumber из TransDesc = %q", got[0].DocNumber)
	}
	if got[0].DocKind != "Входящий документ" {
		t.Errorf("DocKind = %q", got[0].DocKind)
	}
}

// Сценарий: оплата поставщику — Dr 60.01 / Cr 51. Уменьшает наш долг.
func TestBuildDrilldown_PaymentToVendorDecreasesKZ(t *testing.T) {
	raw := []drillRow{
		{
			Date: time.Date(2026, 4, 22, 0, 0, 0, 0, time.UTC),
			DocID: "PV1", RwNm: 1, DrAcc: "60.01", CrAcc: "51", Amount: 5000,
		},
	}
	got := BuildDrilldown(raw, CountryRF, "60", time.Time{})
	if got[0].KZChange != -5000 {
		t.Errorf("Оплата поставщику должна уменьшить КЗ: %v, want -5000", got[0].KZChange)
	}
}

// Сценарий: несколько документов в одной выборке — каждый отдельным DocumentRow.
func TestBuildDrilldown_MultipleDocsKeepOrder(t *testing.T) {
	d1 := time.Date(2026, 4, 1, 0, 0, 0, 0, time.UTC)
	d2 := time.Date(2026, 4, 5, 0, 0, 0, 0, time.UTC)
	d3 := time.Date(2026, 4, 10, 0, 0, 0, 0, time.UTC)
	raw := []drillRow{
		{Date: d1, DocID: "A", DrAcc: "62.01", CrAcc: "90.01.1", Amount: 100},
		{Date: d2, DocID: "B", DrAcc: "62.01", CrAcc: "90.01.1", Amount: 200},
		{Date: d3, DocID: "C", DrAcc: "62.01", CrAcc: "90.01.1", Amount: 300},
	}
	got := BuildDrilldown(raw, CountryRF, "62", time.Time{})
	if len(got) != 3 {
		t.Fatalf("3 документа → 3 строки, got %d", len(got))
	}
	if got[0].DZChange != 100 || got[1].DZChange != 200 || got[2].DZChange != 300 {
		t.Errorf("DZ-суммы в неправильном порядке: %+v", got)
	}
}

// Fallback: ни Objects, ни Mapping, ни TransDescription не дали парсинг —
// DocNumber становится DocID, DocKind = "Документ".
func TestBuildDrilldown_FallbackToDocID(t *testing.T) {
	raw := []drillRow{
		{
			Date: time.Date(2026, 4, 15, 0, 0, 0, 0, time.UTC),
			DocID: "{guid-strange}", DrAcc: "62.01", CrAcc: "90.01.1", Amount: 1,
			// нет ObjectsName, Mapping и TransDescription
		},
	}
	got := BuildDrilldown(raw, CountryRF, "62", time.Time{})
	if got[0].DocNumber != "{guid-strange}" {
		t.Errorf("fallback DocNumber должен быть DocID, got %q", got[0].DocNumber)
	}
	if got[0].DocKind != "Документ" {
		t.Errorf("fallback DocKind должен быть 'Документ', got %q", got[0].DocKind)
	}
}

// Страна неизвестна → DZ/KZ нулевые (классификация не сработала), но строки возвращаются.
func TestBuildDrilldown_UnknownCountryReturnsRowsWithZeroDelta(t *testing.T) {
	raw := []drillRow{
		{
			Date: time.Date(2026, 4, 1, 0, 0, 0, 0, time.UTC),
			DocID: "X", DrAcc: "62.01", CrAcc: "90.01.1", Amount: 999,
			ObjectsName: "Реализация ТДБП-001 от 01.04.2026",
		},
	}
	got := BuildDrilldown(raw, "", "62", time.Time{})
	if len(got) != 1 {
		t.Fatalf("строка должна вернуться даже без классификации, got %d", len(got))
	}
	if got[0].DZChange != 0 || got[0].KZChange != 0 {
		t.Errorf("без страны дельты должны быть 0: %+v", got[0])
	}
	if got[0].DocNumber != "ТДБП-001" {
		t.Errorf("DocNumber всё равно парсится: %q", got[0].DocNumber)
	}
}

func ntime(t time.Time) sql.NullTime { return sql.NullTime{Time: t, Valid: true} }
func nint(i int64) sql.NullInt64     { return sql.NullInt64{Int64: i, Valid: true} }

// Просрочка из Payments.Docs: PaymentDate задаёт срок напрямую, reportDate — точку отсчёта.
func TestBuildDrilldown_OverdueFromPaymentDate(t *testing.T) {
	d := time.Date(2026, 1, 10, 0, 0, 0, 0, time.UTC)
	due := time.Date(2026, 2, 9, 0, 0, 0, 0, time.UTC) // PaymentDate
	report := time.Date(2026, 3, 1, 0, 0, 0, 0, time.UTC)
	raw := []drillRow{
		{Date: d, DocID: "D1", DrAcc: "62.01", CrAcc: "90.01.1", Amount: 100,
			DocPaymentDate: ntime(due)},
	}
	got := BuildDrilldown(raw, CountryRF, "62", report)
	if !got[0].PaymentDueDate.Equal(due) {
		t.Errorf("PaymentDueDate = %v, want %v", got[0].PaymentDueDate, due)
	}
	if got[0].OverdueDays != 20 { // 9 фев → 1 мар = 20 дней
		t.Errorf("OverdueDays = %d, want 20", got[0].OverdueDays)
	}
}

// Если PaymentDate нет — срок = Date + Delay; до срока просрочка 0.
func TestBuildDrilldown_DueFromDatePlusDelayNotYetOverdue(t *testing.T) {
	d := time.Date(2026, 2, 1, 0, 0, 0, 0, time.UTC)
	report := time.Date(2026, 2, 20, 0, 0, 0, 0, time.UTC)
	raw := []drillRow{
		{Date: d, DocID: "D1", DrAcc: "62.01", CrAcc: "90.01.1", Amount: 100,
			DocBaseDate: ntime(d), DocDelay: nint(30)}, // срок = 3 мар
	}
	got := BuildDrilldown(raw, CountryRF, "62", report)
	wantDue := time.Date(2026, 3, 3, 0, 0, 0, 0, time.UTC)
	if !got[0].PaymentDueDate.Equal(wantDue) {
		t.Errorf("PaymentDueDate = %v, want %v", got[0].PaymentDueDate, wantDue)
	}
	if got[0].OverdueDays != 0 {
		t.Errorf("OverdueDays = %d, want 0 (срок ещё не наступил)", got[0].OverdueDays)
	}
}

// Без данных Docs (джойн отключён) — срок пустой, просрочка 0.
func TestBuildDrilldown_NoDocsMetaLeavesDueEmpty(t *testing.T) {
	raw := []drillRow{
		{Date: time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC),
			DocID: "D1", DrAcc: "62.01", CrAcc: "90.01.1", Amount: 100},
	}
	got := BuildDrilldown(raw, CountryRF, "62", time.Date(2026, 6, 1, 0, 0, 0, 0, time.UTC))
	if !got[0].PaymentDueDate.IsZero() {
		t.Errorf("PaymentDueDate должен быть пустым без Docs, got %v", got[0].PaymentDueDate)
	}
	if got[0].OverdueDays != 0 {
		t.Errorf("OverdueDays = %d, want 0", got[0].OverdueDays)
	}
}

func TestBuildDrilldown_Empty(t *testing.T) {
	got := BuildDrilldown(nil, CountryRF, "62", time.Time{})
	if got == nil {
		t.Error("должен вернуть пустой slice, не nil")
	}
	if len(got) != 0 {
		t.Errorf("пустой вход → пустой выход, got %d", len(got))
	}
}
