package debt

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestParseBoolDefault(t *testing.T) {
	tests := []struct {
		in   string
		def  bool
		want bool
	}{
		{"", true, true},
		{"", false, false},
		{"1", false, true},
		{"true", false, true},
		{"yes", false, true},
		{"on", false, true},
		{"0", true, false},
		{"false", true, false},
		{"no", true, false},
		{"off", true, false},
		// Регистр и пробелы — допустимы.
		{" TRUE ", false, true},
		{"FALSE", true, false},
		// Невалидное значение — дефолт.
		{"maybe", true, true},
		{"maybe", false, false},
	}
	for _, tc := range tests {
		if got := parseBoolDefault(tc.in, tc.def); got != tc.want {
			t.Errorf("parseBoolDefault(%q, %v) = %v, want %v", tc.in, tc.def, got, tc.want)
		}
	}
}

// FilterPayload должен корректно мигрировать старые JSON-пресеты, у которых
// ещё нет поля only_ico (поле — указатель *bool с omitempty).
func TestFilterPayload_BackCompatNoOnlyICO(t *testing.T) {
	// Старый формат пресета — без only_ico.
	legacy := `{"date_from":"2026-04-01","date_to":"2026-04-30","entity_inns":["6950135110"],"accounts":[],"currencies":[]}`
	var p FilterPayload
	if err := json.Unmarshal([]byte(legacy), &p); err != nil {
		t.Fatalf("Unmarshal legacy preset: %v", err)
	}
	if p.OnlyICO != nil {
		t.Errorf("OnlyICO должно быть nil для старого пресета, got %v", *p.OnlyICO)
	}
}

func TestFilterPayload_OnlyICOExplicitTrue(t *testing.T) {
	src := `{"date_from":"2026-04-01","date_to":"2026-04-30","entity_inns":[],"accounts":[],"currencies":[],"only_ico":true}`
	var p FilterPayload
	if err := json.Unmarshal([]byte(src), &p); err != nil {
		t.Fatalf("Unmarshal: %v", err)
	}
	if p.OnlyICO == nil || !*p.OnlyICO {
		t.Errorf("OnlyICO должно быть *true, got %v", p.OnlyICO)
	}
}

func TestFilterPayload_OnlyICOExplicitFalse(t *testing.T) {
	src := `{"date_from":"2026-04-01","date_to":"2026-04-30","entity_inns":[],"accounts":[],"currencies":[],"only_ico":false}`
	var p FilterPayload
	if err := json.Unmarshal([]byte(src), &p); err != nil {
		t.Fatalf("Unmarshal: %v", err)
	}
	if p.OnlyICO == nil || *p.OnlyICO {
		t.Errorf("OnlyICO должно быть *false, got %v", p.OnlyICO)
	}
}

// При Marshal nil OnlyICO не должно появляться поле only_ico (omitempty).
func TestFilterPayload_MarshalOmitsNilOnlyICO(t *testing.T) {
	p := FilterPayload{DateFrom: "2026-04-01", DateTo: "2026-04-30"}
	b, err := json.Marshal(p)
	if err != nil {
		t.Fatalf("Marshal: %v", err)
	}
	if strings.Contains(string(b), "only_ico") {
		t.Errorf("nil OnlyICO не должен попасть в JSON, got %s", string(b))
	}
}
