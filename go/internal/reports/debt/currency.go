package debt

// CurrencyForCountry возвращает функциональную валюту юрлица по стране.
// Это упрощённое представление: каждое юрлицо ведёт учёт в нацвалюте своей страны.
//
// Соответствует тому, что лежит в `[FinDWH].[dbo].[CompaniesMF].CurrID` — но
// без необходимости хранить числовой ID валюты. Если кейс «у юрлица функциональная
// валюта отличается от страны» появится (например, кипрские/мальтийские дочки в USD) —
// добавим override-карту по INN.
//
// Подтверждённое из ETL-процедур MSSQL (см. docs/reports/debt/schema-draft.md §8.3):
//   CurrID=1 = BYN (из IIF(B.CurrID=1, 1, ...) в GLMF_exec)
//   CurrID=3 = USD (из комментария в spExRates)
// Остальные коды — гипотезы по странам (требуют сверки с [srv-sql].Gpartner.dbo.valuta1).
func CurrencyForCountry(c Country) string {
	switch c {
	case CountryRB:
		return "BYN"
	case CountryRF:
		return "RUB"
	case CountryKZ:
		return "KZT"
	case CountryUZ:
		return "UZS"
	case CountryTR:
		return "TRY"
	case CountryCZ:
		return "CZK"
	case CountryGB:
		return "GBP"
	case CountryCN:
		return "CNY"
	case CountryKG:
		return "KGS"
	default:
		return ""
	}
}

// CurrencyForINN — функциональная валюта по ИНН/УНП нашего юрлица.
// Возвращает "" если ИНН не наш.
func CurrencyForINN(inn string) string {
	for _, e := range Entities() {
		if e.INN == inn {
			return CurrencyForCountry(e.Country)
		}
	}
	return ""
}
