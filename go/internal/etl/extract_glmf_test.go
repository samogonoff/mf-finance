package etl

import (
	"encoding/json"
	"strings"
	"testing"
)

// T1: glmfRow → JSONEachRow для CH. Ключи JSON ДОЛЖНЫ совпадать с колонками
// finance.fact_glmf (миграция 004), иначе insert молча потеряет поля.
func TestGLMFRowJSONKeys(t *testing.T) {
	b, err := json.Marshal(&glmfRow{})
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	var m map[string]any
	if err := json.Unmarshal(b, &m); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	want := []string{
		"company_id", "counterparty_id", "doc_id", "num", "date", "month",
		"dr_acc", "cr_acc", "dr_acc_root", "cr_acc_root", "code_pl", "group_pl",
		"ico", "country", "amt_wovat_byn", "amt_withvat_byn", "amt_wovat_usd",
		"amt_withvat_usd", "doc_name_1c", "operation_description", "date_of_load",
	}
	for _, k := range want {
		if _, ok := m[k]; !ok {
			t.Errorf("glmfRow JSON missing key %q", k)
		}
	}
	if len(m) != len(want) {
		t.Errorf("glmfRow has %d JSON keys, want %d (%v)", len(m), len(want), keysOf(m))
	}
}

// extract из базовой GLMF (есть DateOfLoad-watermark; vGLMFAddUSD его не имеет).
func TestExtractGLMFSelectFrom(t *testing.T) {
	q := extractGLMFSelectFrom()
	for _, frag := range []string{
		"[FinDWH].[dbo].[GLMF]", "GroupPL", "CodePL",
		"AmountWOVATBelRubFact", "AmountWithVATBelRubFact", "DateOfLoad",
		"p.DocID", "p.Num",
		"CONVERT(TINYINT", // GLMF.ICO — bit; кастим в tinyint, иначе bool→uint8 scan error
		"РБ", "РФ", "КЗ", "УЗ", // маппинг страны витрины → доменной
	} {
		if !strings.Contains(q, frag) {
			t.Errorf("extractGLMFSelectFrom не содержит %q", frag)
		}
	}
}

func keysOf(m map[string]any) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	return out
}
