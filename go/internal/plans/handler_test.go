package plans

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

func newTestHandler() *Handler { return NewHandler(NewSeedSource(), NewMockFactSource()) }

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
