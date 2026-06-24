package etl

import (
	"encoding/json"
	"strings"
	"testing"
)

// T4: эвристика «это название договора?» — отделяет договоры от документов/
// контрагентов в субконто (probe-contracts.md). Сырой резолв даёт мусор.
func TestIsContractName(t *testing.T) {
	keep := []string{
		"Договор поставки № 17/12 от 17.12.2025",
		"20/09 от 20.09.2021",
		"Дополнительное соглашение к Договору 20/09",
		"Счёт-оферта (возмещение ДМС)",
		"Контракт №К-10-1873/А от 20.06.2022",
	}
	for _, s := range keep {
		if !isContractName(s) {
			t.Errorf("isContractName(%q) = false, ожидался договор", s)
		}
	}
	drop := []string{
		"Поступление (акт, накладная) 00БС-000058",
		"Реализация (акт) ТДБП-000001",
		"Оказание производственных услуг 00БС-123",
		"ООО Торговый Дом «Марк Формэль»",
		"Без Договора", // КЗ-литерал «нет договора» — не показывать как договор
		"Без договора",
		"",
	}
	// КЗ-форматы договоров (3310 CrSubconto1) — должны оставаться
	for _, s := range []string{"Договор б/н о 01.11.2025", "14ВМ-02/2023", "39-360496/24 ОЗОН RUB"} {
		if !isContractName(s) {
			t.Errorf("isContractName(%q) = false, ожидался КЗ-договор", s)
		}
	}
	for _, s := range drop {
		if isContractName(s) {
			t.Errorf("isContractName(%q) = true, НЕ договор", s)
		}
	}
}

// extract субконто→договор: позиция зависит от счёта (62→DrSubconto2, 60/76→CrSubconto1).
func TestExtractContractSelectFrom(t *testing.T) {
	q := extractContractSelectFrom("[FinDWH].[dbo].[Premaster1C]")
	for _, frag := range []string{
		"[FinDWH].[dbo].[Premaster1C]", "DocID", "DrSubconto2", "CrSubconto1",
		"DrSubconto1", "1210", "3310", // КЗ/УЗ: договор в Subconto1 (T8)
		"[Objects]", "account_kind", "DISTINCT",
	} {
		if !strings.Contains(q, frag) {
			t.Errorf("extractContractSelectFrom не содержит %q", frag)
		}
	}
}

// contractRow → JSON-ключи = колонки dim_contract.
func TestContractRowJSONKeys(t *testing.T) {
	want := []string{"doc_id", "contract_ref", "contract_name", "account_kind"}
	raw, _ := json.Marshal(contractRow{})
	b := string(raw)
	for _, k := range want {
		if !strings.Contains(b, `"`+k+`"`) {
			t.Errorf("contractRow JSON без ключа %q (%s)", k, b)
		}
	}
}
