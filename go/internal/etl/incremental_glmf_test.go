package etl

import (
	"strings"
	"testing"
)

// T6: CountryByINN должен покрывать КЗ/УЗ (chart_of_accounts для них работает
// только при известной стране ЮЛ). До T6 в карте были только РФ/РБ.
func TestCountryByINN_coversKZUZ(t *testing.T) {
	cases := map[string]string{
		"141240004842": "КЗ", // MFKaz
		"305554644":    "УЗ", // MFUz
		"310170662":    "УЗ", // BARREIROS SOFT
		"690591512":    "РБ", // MF (регресс)
		"6950135110":   "РФ", // TDMF (регресс)
	}
	for inn, want := range cases {
		if got := CountryByINN[inn]; got != want {
			t.Errorf("CountryByINN[%s] = %q, want %q", inn, got, want)
		}
	}
}

// Инкремент fact_glmf — по watermark DateOfLoad (в GLMF нет DateOfChange).
func TestIncrementalGLMFWhere(t *testing.T) {
	w := incrementalGLMFWhere()
	if !strings.Contains(w, "DateOfLoad >") {
		t.Errorf("incrementalGLMFWhere = %q, ожидался фильтр по DateOfLoad", w)
	}
}
