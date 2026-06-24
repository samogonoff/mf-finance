package etl

import (
	"database/sql"
	"strings"
	"testing"
)

// Кластерный индекс GLMF — по Company (код), не CompanyID (ИНН). Фильтр должен
// идти по коду, иначе full scan 209M строк (bootstrap «висит без батчей»).
func TestGLMFCompanyWhere_usesCode(t *testing.T) {
	clause, args := glmfCompanyWhere("6950135110")
	if !strings.Contains(clause, "p.Company = @co") {
		t.Errorf("clause не фильтрует по Company (коду): %q", clause)
	}
	// среди аргументов должен быть код TDMF
	found := false
	for _, a := range args {
		if na, ok := a.(sql.NamedArg); ok {
			if s, ok := na.Value.(string); ok && s == "TDMF" {
				found = true
			}
		}
	}
	if !found {
		t.Errorf("args не содержат код TDMF для ИНН 6950135110: %v", args)
	}
}

// Неизвестный ИНН (без кода) — фолбэк на CompanyID, без падения.
func TestGLMFCompanyWhere_fallback(t *testing.T) {
	clause, _ := glmfCompanyWhere("000000")
	if !strings.Contains(clause, "p.CompanyID = @inn") {
		t.Errorf("фолбэк должен фильтровать по CompanyID: %q", clause)
	}
}

func TestCodeByINN_TDMF(t *testing.T) {
	if CodeByINN["6950135110"] != "TDMF" || CodeByINN["690591512"] != "MF" {
		t.Errorf("CodeByINN некорректен: TDMF=%q MF=%q", CodeByINN["6950135110"], CodeByINN["690591512"])
	}
}
