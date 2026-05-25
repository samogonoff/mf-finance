package debt

import "testing"

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
