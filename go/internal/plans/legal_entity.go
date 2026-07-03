package plans

import "strings"

// Единый резолв ЮЛ: сырые алиасы из xlsx → каноничное имя (Приложение B ТЗ).
// Одна точка истины (как canonicalCountries для стран); справочник
// dir_legal_entity отражает это для правки заказчиком. Гипотеза маппинга —
// уточняется в UI; спорные (МФЦ/ПТИР/ДД/...) ведут на собственное «(?)»-ЮЛ.

var legalEntityAliases = map[string]string{
	"мф":          "Mark Formelle",
	"фмф":         "Mark Formelle",
	"мф, ф":       "Mark Formelle",
	"мф, гп":      "Mark Formelle",
	"мф,ф, гп":    "Mark Formelle",
	"ф":           "Formelle",
	"мфц":         "Formelle",
	"мф it":       "MF IT",
	"мф кз":       "MF Kazakhstan",
	"мф текс":     "MF Tex",
	"тд":          "TD Mark Formelle",
	"mark formelle trade": "TD Mark Formelle",
	"птир":        "ПТИР (?)",
	"дд":          "ДД / Дримдом (?)",
	"barreiros soft ooo": "BARREIROS SOFT OOO (?)",
	"mf fashion":  "MF FASHION (?)",
}

// canonicalLegalEntity приводит сырое значение ЮЛ к каноничному имени.
// Неизвестное — возвращается как есть (видно в справочнике, правится вручную).
func canonicalLegalEntity(raw string) string {
	key := strings.ToLower(strings.TrimSpace(raw))
	if key == "" {
		return ""
	}
	if name, ok := legalEntityAliases[key]; ok {
		return name
	}
	return raw
}
