package plans

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

type ownerStub struct {
	owners map[string]int64
	err    error
}

func (s ownerStub) StepOwners(context.Context, int64, string, string) (map[string]int64, error) {
	return s.owners, s.err
}

func actionOwnerHandler(owner int64, resolverErr error) *CardHandler {
	cards := &fakeCardStore{card: Card{ID: 42, PlID: 7, FormCode: TemplateMP,
		ScopeKey: "large", Country: "RU", Status: CardOnApproval, StepCode: "fin"},
		route: []RouteStep{{StepCode: "1.1", SortOrder: 10, Kind: StepFill, Enabled: true},
			{StepCode: "fin", SortOrder: 20, Kind: StepApprove, Enabled: true},
			{StepCode: "1.3", SortOrder: 30, Kind: StepApprove, Enabled: true}}}
	svc := NewCardService(cards).WithOwners(ownerStub{owners: map[string]int64{"fin": owner, "1.3": owner}, err: resolverErr})
	return NewCardHandler(svc, newMemScope(), func(*http.Request) (Principal, bool) {
		return Principal{UserID: 7}, true
	})
}

func cardActionRequest(h *CardHandler, payload string) *httptest.ResponseRecorder {
	req := httptest.NewRequest(http.MethodPost, "/api/plans/cards/42/action", strings.NewReader(payload))
	req.SetPathValue("cardId", "42")
	rec := httptest.NewRecorder()
	h.CardAction(rec, req)
	return rec
}

func TestCardActionRejectsWrongApprovalOwnerEvenWithBlankStageScope(t *testing.T) {
	h := actionOwnerHandler(8, nil)
	for _, body := range []string{`{"action":" approve "}`, `{"action":"return","target_step":"1.1","comment":"fix"}`} {
		got := cardActionRequest(h, body)
		if got.Code != http.StatusForbidden {
			t.Fatalf("%s: code %d, body %s", body, got.Code, got.Body.String())
		}
	}
}

func TestCardActionRejectsOwnerLookupFailure(t *testing.T) {
	h := actionOwnerHandler(0, errors.New("directory down"))
	got := cardActionRequest(h, `{"action":"approve"}`)
	if got.Code != http.StatusServiceUnavailable {
		t.Fatalf("code %d, body %s", got.Code, got.Body.String())
	}
}

func TestCardActionAllowsAssignedApprovalOwner(t *testing.T) {
	h := actionOwnerHandler(7, nil)
	got := cardActionRequest(h, `{"action":"approve"}`)
	if got.Code != http.StatusOK || !strings.Contains(got.Body.String(), `"step_code":"1.3"`) {
		t.Fatalf("code %d, body %s", got.Code, got.Body.String())
	}
}
