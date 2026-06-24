package debt

import "strings"

// Справочник ВГО-контрагентов: имя (CounterpartyName из Table_Fin_PL/vGLMFAddUSD,
// ICO=1) → ИНН/УНП. Table_Fin_PL хранит контрагента только именем (без ИНН),
// поэтому для группировки revenue-строк с ДЗ/КЗ (по ИНН) и для адресации в
// drilldown резолвим имя в ИНН по наблюдаемым значениям витрины (сняты с OLAP).
//
// ⚠ Имена не нормализованы апстримом — это репрезентативные значения; при
// расхождении со значениями в наполненной Table_Fin_PL матчинг расширить
// (см. docs/reports/debt/finpl-merge.md, CHECKPOINT B/C). Робастное решение —
// добавить CounterpartyID в витрину апстримом.
var vgoCounterpartyNames = map[string]string{
	"Марк Формэль":            "690591512",
	"Формэль":                 "690719790",
	"Марк Формэль ТД ООО, РФ": "6950135110",
	"Марк Формэль Текс ООО":   "5031159833",
	`Филиал "Марк Формэль"`:   "9909349268",
	"ПТИР ООО":                "9731039708",
	`"Mark Formelle Kazakhstan"(Марк Формэль Казахстан)`: "141240004842",
	"MARK FORMELLE IT, ООО, Узбекистан":                  "305554644",
	"BARREIROS SOFT OOO":                                 "310170662",
	"Дримдом, ООО":                                       "692221084",
	"Дримдом 2 ООО":                                      "693335015",
	"Сипарова Светлана Геннадьевна ИП":                   "695018688905",
}

// vgoCounterpartyINNByName резолвит имя контрагента в ИНН. ok=false для пустого/
// неизвестного имени (внешние контрагенты, контрагенты без ИНН — напр. Летникова).
func vgoCounterpartyINNByName(name string) (string, bool) {
	name = strings.TrimSpace(name)
	if name == "" {
		return "", false
	}
	inn, ok := vgoCounterpartyNames[name]
	return inn, ok
}

// vgoNameByINN — обратный резолв (ИНН → репрезентативное имя). Нужен drilldown'у,
// чтобы по partner_inn отфильтровать строки Table_Fin_PL (где контрагент — имя).
func vgoNameByINN(inn string) (string, bool) {
	inn = strings.TrimSpace(inn)
	for name, v := range vgoCounterpartyNames {
		if v == inn {
			return name, true
		}
	}
	return "", false
}

// codeByINN — ИНН юрлица → код Table_Fin_PL.Компания (обратный к справочнику
// codeIndex). Нужен drilldown'у/фильтру (company_inn → код витрины).
func codeByINN(inn string) (string, bool) {
	inn = strings.TrimSpace(inn)
	for code, e := range codeIndex {
		if e.INN == inn {
			return code, true
		}
	}
	return "", false
}
