package plans

// Реестр форм комплекта: какие карточки существуют в периоде и с какими
// атрибутами. Данные из ТЗ:
//   - МП (ТЗ §2.2): крупные и мелкие — независимые подзадачи одного периода;
//     заполняются разными людьми, возврат по одной не блокирует другую. Внутри
//     формы работают все площадки сегмента (объединение large+small в одну сетку
//     сделано в VS15 — это про UI, а не про маршрут).
//   - Розница (ТЗ §2.0): «форм розницы четыре — по одной на страну… у каждой
//     страны свой перечень магазинов, свой набор статей, своя национальная валюта
//     и своё единственное юридическое лицо». Соответствие страна→ЮЛ подтверждено
//     запросом B1: BY→F, RU→TDMF, KZ→MFKaz, UZ→MFUz.

// TemplateRetail — код формы «Тактический план продаж, Розница».
const TemplateRetail = "TPL-TO-RETAIL"

// FormScope — область одной карточки формы (сегмент МП или страна розницы).
type FormScope struct {
	Key         string `json:"key"`
	Title       string `json:"title"`
	Country     string `json:"country"`
	LegalEntity string `json:"legal_entity"`
	Currency    string `json:"currency"` // валюта ВВОДА (национальная)
}

// FormDef — описание формы: код, название и набор карточек на период.
type FormDef struct {
	Code   string      `json:"code"`
	Name   string      `json:"name"`
	Scopes []FormScope `json:"scopes"`
}

// FormRegistry — формы, у которых есть карточка процесса.
func FormRegistry() []FormDef {
	return []FormDef{
		{
			Code: TemplateMP, Name: "Маркетплейсы",
			Scopes: []FormScope{
				// Валюта ввода площадок large — RUB (все четыре RU). Для small
				// валюта определяется площадкой (RU/KZ/UZ), поэтому на карточке
				// пусто: ввод идёт в валюте площадки (ТЗ МП §3.3, МП-10).
				{Key: "large", Title: "Крупные МП", Country: "RU", LegalEntity: "TD Mark Formelle", Currency: "RUB"},
				{Key: "small", Title: "Мелкие МП", Country: "", LegalEntity: "", Currency: ""},
			},
		},
		{
			Code: TemplateRetail, Name: "Розница",
			Scopes: []FormScope{
				{Key: "BY", Title: "Розница РБ", Country: "BY", LegalEntity: "F", Currency: "BYN"},
				{Key: "RU", Title: "Розница РФ", Country: "RU", LegalEntity: "TDMF", Currency: "RUB"},
				{Key: "KZ", Title: "Розница KZ", Country: "KZ", LegalEntity: "MFKaz", Currency: "KZT"},
				{Key: "UZ", Title: "Розница UZ", Country: "UZ", LegalEntity: "MFUz", Currency: "UZS"},
			},
		},
	}
}

// formDef — описание формы по коду.
func formDef(code string) (FormDef, bool) {
	for _, f := range FormRegistry() {
		if f.Code == code {
			return f, true
		}
	}
	return FormDef{}, false
}

// formScope — область формы по ключу.
func formScope(code, key string) (FormScope, bool) {
	f, ok := formDef(code)
	if !ok {
		return FormScope{}, false
	}
	for _, s := range f.Scopes {
		if s.Key == key {
			return s, true
		}
	}
	return FormScope{}, false
}

// RetailCountries — страны формы розницы в порядке вывода.
func RetailCountries() []FormScope {
	f, _ := formDef(TemplateRetail)
	return f.Scopes
}

// retailLegalEntity — единственное ЮЛ страны (валидация V-08: план не должен
// «разлетаться» по нескольким ЮЛ).
func retailLegalEntity(country string) (string, bool) {
	for _, s := range RetailCountries() {
		if s.Country == country {
			return s.LegalEntity, true
		}
	}
	return "", false
}

// retailCurrency — национальная валюта страны розницы (валюта ввода, V-11).
func retailCurrency(country string) string {
	for _, s := range RetailCountries() {
		if s.Country == country {
			return s.Currency
		}
	}
	return ""
}
