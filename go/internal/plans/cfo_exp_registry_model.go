package plans

import (
	"fmt"
	"strings"
)

// CfoExpenseRegistrySheet describes a manually maintained supplemental sheet.
type CfoExpenseRegistrySheet struct {
	Code                 CfoExpFormCode            `json:"code"`
	Name                 string                    `json:"name"`
	Label                string                    `json:"label"`
	Fields               []CfoExpenseRegistryField `json:"fields"`
	ConfigurationPending bool                      `json:"configuration_pending,omitempty"`
	SharedBook           bool                      `json:"shared_book,omitempty"`
}
type CfoExpenseRegistryField struct {
	Key   string `json:"key"`
	Label string `json:"label"`
	Type  string `json:"type"`
}

var cfoRegistryFields = []CfoExpenseRegistryField{
	{"date", "Дата", "date"}, {"country", "Страна", "text"}, {"city", "Город", "text"}, {"purpose", "Цель", "text"}, {"people", "Люди", "number"}, {"days", "Дни", "number"}, {"nights", "Ночёвки", "number"}, {"tickets", "Билеты", "number"}, {"lodging", "Проживание", "number"}, {"per_diem", "Суточные", "number"}, {"currency", "Валюта", "text"}, {"price", "Цена", "number"}, {"trips_week", "Рейсы в неделю", "number"}, {"rate", "Курс", "number"}, {"company", "Юрлицо", "text"}, {"employee", "ФИО", "text"},
}

func fields(keys ...string) []CfoExpenseRegistryField {
	out := make([]CfoExpenseRegistryField, 0, len(keys))
	for _, k := range keys {
		for _, f := range cfoRegistryFields {
			if f.Key == k {
				out = append(out, f)
				break
			}
		}
	}
	return out
}
func sheet(code CfoExpFormCode, name string, keys ...string) CfoExpenseRegistrySheet {
	return CfoExpenseRegistrySheet{Code: code, Name: name, Label: name, Fields: fields(keys...)}
}

// CfoExpenseRegistryCatalog is the explicitly supported book/sheet catalogue.
// Rows are always edited and transferred manually; sheets never write the grid.
func CfoExpenseRegistryCatalog(code CfoExpFormCode) []CfoExpenseRegistrySheet {
	switch code {
	case CfoExp31:
		return []CfoExpenseRegistrySheet{
			sheet(code, "construction", "company", "employee", "price"), sheet(code, "training", "employee", "purpose", "price"), sheet(code, "budget_h1", "company", "price"), sheet(code, "budget_h2", "company", "price"), sheet(code, "laboratory_h1", "company", "price", "currency"), sheet(code, "laboratory_h2", "company", "price", "currency"), sheet(code, "safety", "company", "price"),
		}
	case CfoExp32:
		return []CfoExpenseRegistrySheet{
			sheet(code, "delivery52", "company", "price", "trips_week", "rate"), sheet(code, "couriers24", "company", "price"), sheet(code, "route93", "country", "price", "trips_week", "rate"), sheet(code, "route53", "country", "price", "trips_week", "rate"), sheet(code, "route50", "country", "price", "trips_week", "rate"), sheet(code, "insurance", "company", "price"), sheet(code, "insurance2026", "company", "price"), sheet(code, "fund", "employee", "company", "price", "rate"), sheet(code, "travel", "date", "country", "city", "purpose", "days", "nights", "people", "tickets", "lodging", "per_diem", "currency"), sheet(code, "travel2024", "date", "country", "city", "purpose", "days", "nights", "people", "tickets", "lodging", "per_diem", "currency"),
			{Code: code, Name: "country_RU", Label: "РФ (коды 50,51,52,53,93)", Fields: fields("country", "company", "price"), ConfigurationPending: true}, {Code: code, Name: "country_BY", Label: "РБ (коды 50,51,52,93)", Fields: fields("country", "company", "price"), ConfigurationPending: true}, {Code: code, Name: "country_KZ", Label: "КЗ (коды 50,51,52,53,93)", Fields: fields("country", "company", "price"), ConfigurationPending: true}, {Code: code, Name: "country_UZ", Label: "УЗ (коды 50,51,52,53,93)", Fields: fields("country", "company", "price"), ConfigurationPending: true},
		}
	case CfoExp33:
		return []CfoExpenseRegistrySheet{sheet(code, "payroll", "employee", "company", "price"), sheet(code, "travelBY", "date", "country", "purpose", "days", "nights", "people", "tickets", "lodging", "per_diem", "currency"), sheet(code, "travelRU", "date", "country", "purpose", "days", "nights", "people", "tickets", "lodging", "per_diem", "currency")}
	case CfoExp34:
		return []CfoExpenseRegistrySheet{{Code: code, Name: "warehouse", Label: "Склад (схема уточняется)", Fields: fields("company", "employee", "price"), ConfigurationPending: true}}
	case CfoExp37:
		return []CfoExpenseRegistrySheet{sheet(code, "travelBY25", "date", "country", "purpose", "days", "nights", "people", "tickets", "lodging", "per_diem", "currency"), sheet(code, "travelRU25", "date", "country", "purpose", "days", "nights", "people", "tickets", "lodging", "per_diem", "currency"), sheet(code, "salary", "employee", "company", "price")}
	case CfoExp962: // second block uses the 37 shared book, with country isolation.
		out := CfoExpenseRegistryCatalog(CfoExp37)
		for i := range out {
			out[i].Code = code
			out[i].SharedBook = true
		}
		return out
	}
	return nil
}
func CfoExpenseRegistrySheetFor(code CfoExpFormCode, name string) (CfoExpenseRegistrySheet, bool) {
	for _, s := range CfoExpenseRegistryCatalog(code) {
		if s.Name == name {
			return s, true
		}
	}
	return CfoExpenseRegistrySheet{}, false
}

// CfoExpenseRegistryRow keeps nullable months and arbitrary sheet attributes.
type CfoExpenseRegistryRow struct {
	ID        int64                     `json:"id"`
	CardID    int64                     `json:"card_id,omitempty"`
	BookScope string                    `json:"book_scope,omitempty"`
	Sheet     string                    `json:"sheet"`
	Attrs     map[string]any            `json:"attrs"`
	Months    map[int]*float64          `json:"months"`
	Total     float64                   `json:"total,omitempty"`
	Issues    []CfoExpenseRegistryIssue `json:"issues,omitempty"`
}
type CfoExpenseRegistryIssue struct {
	Code     string `json:"code"`
	Message  string `json:"message"`
	Month    int    `json:"month,omitempty"`
	Blocking bool   `json:"blocking"`
}
type CfoExpenseRegistry struct {
	CardID               int64                     `json:"card_id"`
	Code                 CfoExpFormCode            `json:"code"`
	Country              string                    `json:"country"`
	BookScope            string                    `json:"book_scope"`
	Sheets               []CfoExpenseRegistrySheet `json:"sheets"`
	Sheet                string                    `json:"sheet"`
	Rows                 []CfoExpenseRegistryRow   `json:"rows"`
	ReadOnly             bool                      `json:"read_only"`
	Total                float64                   `json:"total"`
	ConfigurationPending bool                      `json:"configuration_pending,omitempty"`
}

func (r CfoExpenseRegistryRow) SumMonths() float64 {
	var n float64
	for _, v := range r.Months {
		if v != nil {
			n += *v
		}
	}
	return n
}
func cfoRegistryScope(code CfoExpFormCode, country string) string {
	return fmt.Sprintf("CFO-%d-%s", code, strings.ToUpper(country))
}
