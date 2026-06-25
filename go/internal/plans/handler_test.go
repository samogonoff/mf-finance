package plans

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func newTestHandler() *Handler {
	fact := NewMockFactSource()
	svc := NewService(newMemStore(), fact, newMemScope())
	adminPrincipal := func(*http.Request) (Principal, bool) { return Principal{PlansAdmin: true}, true }
	return NewHandler(NewSeedSource(), fact, svc, adminPrincipal)
}

func TestHealth_ReturnsOK(t *testing.T) {
	req := httptest.NewRequest(http.MethodGet, "/api/plans/health", nil)
	rec := httptest.NewRecorder()

	newTestHandler().Health(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("Health status = %d, want 200", rec.Code)
	}
	var body struct {
		OK bool `json:"ok"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("Health body not JSON: %v (%q)", err, rec.Body.String())
	}
	if !body.OK {
		t.Errorf("Health body.ok = false, want true (%q)", rec.Body.String())
	}
	if ct := rec.Header().Get("Content-Type"); ct != "application/json" {
		t.Errorf("Health Content-Type = %q, want application/json", ct)
	}
}

func TestDirectoryRows_MarketplaceLarge(t *testing.T) {
	req := httptest.NewRequest(http.MethodGet, "/api/plans/directories/dir_marketplace/rows", nil)
	req.SetPathValue("code", "dir_marketplace")
	rec := httptest.NewRecorder()

	newTestHandler().DirectoryRows(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("DirectoryRows status = %d, want 200 (%q)", rec.Code, rec.Body.String())
	}
	var rows []MarketplaceRow
	if err := json.Unmarshal(rec.Body.Bytes(), &rows); err != nil {
		t.Fatalf("rows not JSON: %v (%q)", err, rec.Body.String())
	}
	var largeCount int
	for _, r := range rows {
		if r.Segment == "large" {
			largeCount++
			if r.CodeCFO == 0 || r.NameCFO == "" || r.Country == "" {
				t.Errorf("неполная large-строка: %+v", r)
			}
		}
	}
	if largeCount < 4 {
		t.Errorf("ожидалось >=4 large площадок в ответе, got %d", largeCount)
	}
}

func TestDirectoryRows_UnknownReturns404(t *testing.T) {
	req := httptest.NewRequest(http.MethodGet, "/api/plans/directories/dir_bogus/rows", nil)
	req.SetPathValue("code", "dir_bogus")
	rec := httptest.NewRecorder()

	newTestHandler().DirectoryRows(rec, req)

	if rec.Code != http.StatusNotFound {
		t.Errorf("неизвестный справочник должен дать 404, got %d", rec.Code)
	}
}

func TestMpFact_Endpoint_LargeMay2026(t *testing.T) {
	req := httptest.NewRequest(http.MethodGet, "/api/plans/mp/fact?year=2026&month=5&segment=large", nil)
	rec := httptest.NewRecorder()

	newTestHandler().MpFact(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("MpFact status = %d, want 200 (%q)", rec.Code, rec.Body.String())
	}
	var rows []FactRow
	if err := json.Unmarshal(rec.Body.Bytes(), &rows); err != nil {
		t.Fatalf("rows not JSON: %v", err)
	}
	var found bool
	for _, r := range rows {
		if r.CodeCFO == 335 && r.CodePL == 1046 {
			found = true
			if r.Amount < 357034569 || r.Amount > 357034571 {
				t.Errorf("WB 1046 = %.2f, want ≈357034569.85", r.Amount)
			}
		}
	}
	if !found {
		t.Errorf("в ответе нет WB(335)/1046")
	}
}

func TestMpFact_Endpoint_BadMonth(t *testing.T) {
	req := httptest.NewRequest(http.MethodGet, "/api/plans/mp/fact?year=2026&month=0&segment=large", nil)
	rec := httptest.NewRecorder()
	newTestHandler().MpFact(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Errorf("month=0 должен дать 400, got %d", rec.Code)
	}
}

func TestMpForm_PutThenGet_RoundTrip(t *testing.T) {
	h := newTestHandler()

	body := `{"segment":"large","period":{"year":2026,"month":5},
		"header":{"currency":"RUB","scenario":"Тактика бюджет (таргеты)"},
		"rows":[{"code_cfo":335,"code_pl":1046,"block_type":"sales_manager_price","amount":777000,"is_manual":true,"comment":"round-trip"}]}`
	put := httptest.NewRequest(http.MethodPut, "/api/plans/mp/form", strings.NewReader(body))
	putRec := httptest.NewRecorder()
	h.MpFormSave(putRec, put)
	if putRec.Code != http.StatusOK {
		t.Fatalf("PUT status = %d (%q)", putRec.Code, putRec.Body.String())
	}

	get := httptest.NewRequest(http.MethodGet, "/api/plans/mp/form?year=2026&month=5&segment=large", nil)
	getRec := httptest.NewRecorder()
	h.MpFormGet(getRec, get)
	if getRec.Code != http.StatusOK {
		t.Fatalf("GET status = %d (%q)", getRec.Code, getRec.Body.String())
	}
	var form MpForm
	if err := json.Unmarshal(getRec.Body.Bytes(), &form); err != nil {
		t.Fatalf("form not JSON: %v", err)
	}
	var ok bool
	for _, b := range form.Blocks {
		if b.CodePL != 1046 {
			continue
		}
		for _, r := range b.Rows {
			if r.CodeCFO == 335 {
				ok = true
				if r.Tactic == nil || *r.Tactic != 777000 {
					t.Errorf("тактика не сохранилась round-trip через HTTP: %v", r.Tactic)
				}
			}
		}
	}
	if !ok {
		t.Error("WB(335)/1046 не найден в форме")
	}
}

func TestMpFormSave_RejectsForeignCFO(t *testing.T) {
	h := newTestHandler()
	body := `{"segment":"large","period":{"year":2026,"month":5},
		"rows":[{"code_cfo":338,"code_pl":1046,"block_type":"sales_manager_price","amount":1}]}`
	req := httptest.NewRequest(http.MethodPut, "/api/plans/mp/form", strings.NewReader(body))
	rec := httptest.NewRecorder()
	h.MpFormSave(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Errorf("чужой code_cfo должен дать 400, got %d", rec.Code)
	}
}

func TestDirectories_IncludesExpected(t *testing.T) {
	req := httptest.NewRequest(http.MethodGet, "/api/plans/directories", nil)
	rec := httptest.NewRecorder()

	newTestHandler().Directories(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("Directories status = %d, want 200", rec.Code)
	}
	var dirs []Directory
	if err := json.Unmarshal(rec.Body.Bytes(), &dirs); err != nil {
		t.Fatalf("dirs not JSON: %v", err)
	}
	want := map[string]bool{"dir_marketplace": false, "dir_pl_line": false, "dir_cfo": false}
	for _, d := range dirs {
		if _, ok := want[d.Code]; ok {
			want[d.Code] = true
			if d.RowCount == 0 {
				t.Errorf("справочник %s должен иметь row_count>0", d.Code)
			}
		}
	}
	for code, seen := range want {
		if !seen {
			t.Errorf("реестр должен содержать %s", code)
		}
	}
}
