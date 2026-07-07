package debt

import "strings"

// Линзы CUR_FILTER (FinDebt3) — это НЕ валюта, а представление ОДНОЙ суммы в
// разных пересчётах. Отчёт выбирает ровно одну линзу (иначе задвоение ×3, см.
// docs/reports/debt/findebt-verification.md §«CUR_FILTER»). Строки в CH-таблице
// fact_findebt_ccy разложены по этим значениям в колонке cur_filter.
const (
	LensContract = "В валюте договора" // native-валюта строки (sum в валюте договора)
	LensBYN      = "В бел. рублях"     // всё пересчитано в BYN
	LensUSD      = "В долларах США"    // всё пересчитано в USD
)

// LensDefault — дефолтная линза отчёта: native-валюта договора (рекомендация
// заказчика, см. переписку по ВГО). Пустой Lens в фильтрах трактуется как эта.
const LensDefault = LensContract

// Lenses — закрытый перечень линз для UI и валидации.
var Lenses = []string{LensContract, LensBYN, LensUSD}

// normLens приводит вход к валидной линзе; неизвестное/пустое → LensDefault.
// Защищает SQL от произвольного значения (линза инлайнится в CH-запрос).
func normLens(s string) string {
	switch strings.TrimSpace(s) {
	case LensBYN:
		return LensBYN
	case LensUSD:
		return LensUSD
	case LensContract:
		return LensContract
	default:
		return LensDefault
	}
}

// lensCurrency — валюта-подпись строки отчёта для выбранной линзы. В линзах BYN/USD
// сумма уже пересчитана движком → подпись фиксирована. В линзе «В валюте договора»
// подпись = native-валюта строки (из FinDebt3.Currency).
func lensCurrency(lens, native string) string {
	switch lens {
	case LensBYN:
		return "BYN"
	case LensUSD:
		return "USD"
	default:
		return normCurrency(native)
	}
}

// normCurrency чистит native-код валюты FinDebt3 (в источнике встречается
// «BYN», «RUR.», «USD») до канона UI. Пустой остаётся пустым (незнакомый УНП/строка).
func normCurrency(s string) string {
	s = strings.TrimRight(strings.TrimSpace(s), ".")
	switch strings.ToUpper(s) {
	case "":
		return ""
	case "RUR", "RUB":
		return "RUB"
	default:
		return strings.ToUpper(s)
	}
}
