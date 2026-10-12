package main

import (
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"testing"
)

// Keep the embedded route contract and the actual ServeMux registrations in sync.
// A manifest entry without this wrapper would otherwise look reviewed while remaining
// unreachable (or accidentally binding a different handler).
func TestTechnicalReadonlyManifestMatchesMainRegistrations(t *testing.T) {
	source, err := os.ReadFile("main.go")
	if err != nil {
		t.Fatal(err)
	}
	raw, err := os.ReadFile("../../internal/auth/technical_readonly_routes.json")
	if err != nil {
		t.Fatal(err)
	}
	var routes []struct {
		Pattern string `json:"pattern"`
		Handler string `json:"handler"`
	}
	if err := json.Unmarshal(raw, &routes); err != nil {
		t.Fatal(err)
	}
	text := string(source)
	for _, route := range routes {
		needle := fmt.Sprintf("technicalRead(%q, %q,", route.Pattern, route.Handler)
		if !strings.Contains(text, needle) {
			t.Errorf("manifest route %q/%q is not registered through technicalRead", route.Pattern, route.Handler)
		}
	}
}
