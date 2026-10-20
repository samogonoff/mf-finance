package plans

import (
	"strings"
	"sync/atomic"
)

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

// Формы отделов (CFO-31-BY … CFO-962-RU) ещё не доведены до выкладки. Код и данные
// остаются, но по умолчанию такие карточки не создаются в новых периодах и не
// показываются в списках: включает PLANS_CFO_DEPT_FORMS=1. Скрытие обратимо —
// уже созданные карточки не удаляются.
var cfoDeptFormsOn atomic.Bool

// SetCfoDeptFormsEnabled включает (true) или скрывает (false) формы отделов.
func SetCfoDeptFormsEnabled(on bool) { cfoDeptFormsOn.Store(on) }

// isCfoDeptScope — ключ области формы отдела («CFO-31-BY»).
func isCfoDeptScope(formCode, scopeKey string) bool {
	return formCode == TemplateExp && strings.HasPrefix(strings.ToUpper(scopeKey), "CFO-")
}

// cfoDeptHidden — карточку не показываем и не создаём.
func cfoDeptHidden(formCode, scopeKey string) bool {
	return !cfoDeptFormsOn.Load() && isCfoDeptScope(formCode, scopeKey)
}

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

// FormRegistry — формы, у которых есть карточка процесса (без скрытых флагом).
func FormRegistry() []FormDef {
	all := formRegistryAll()
	for i := range all {
		kept := all[i].Scopes[:0:0]
		for _, sc := range all[i].Scopes {
			if !cfoDeptHidden(all[i].Code, sc.Key) {
				kept = append(kept, sc)
			}
		}
		all[i].Scopes = kept
	}
	return all
}

// formRegistryAll — реестр целиком, включая скрытые области.
func formRegistryAll() []FormDef {
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
			// Этап 1.5 (ТЗ §3, §4): форм две — розница и маркетплейсы, — и у
			// каждой свой отбор строк из источника затрат.
			// Ключ области несёт вид формы и страну: "R-BY", "MP-RU" — по нему
			// exp_service выбирает отбор источника, а не по отдельной колонке.
			Code: TemplateExp, Name: "Расходы ЦП",
			Scopes: []FormScope{
				// Ответ 42 (10.09.2026): источник у всех стран один, страна —
				// параметр карточки (Country = <страна> AND GroupCFO1 =
				// '3.Магазины'). Поэтому у розницы карточек ЧЕТЫРЕ, а не одна:
				// ТЗ §2.1 показывал только отбор РБ, но расходы планируются по
				// всем четырём странам, как и продажи на этапе 1.1.
				// ТЗ v2: отдельные карточки расходов отделов. Legacy R-/MP-
				// scopes remain for the existing 1.5 contour.
				{Key: "CFO-31-BY", Title: "Форма 31 · Строительный отдел", Country: "BY", Currency: "BYN"},
				{Key: "CFO-32-BY", Title: "Форма 32 · Отдел логистики", Country: "BY", Currency: "BYN"},
				{Key: "CFO-33-BY", Title: "Форма 33 · Отдел снабжения", Country: "BY", Currency: "BYN"},
				{Key: "CFO-34-BY", Title: "Форма 34 · Центральный склад", Country: "BY", Currency: "BYN"},
				{Key: "CFO-37-BY", Title: "Форма 37 · Конструкторско-технологический отдел", Country: "BY", Currency: "BYN"},
				{Key: "CFO-962-RU", Title: "Форма 962 · блок книги 37", Country: "RU", Currency: "RUB"},
				{Key: "R-BY", Title: "Розница РБ", Country: "BY", LegalEntity: "F", Currency: "BYN"},
				{Key: "R-RU", Title: "Розница РФ", Country: "RU", LegalEntity: "TDMF", Currency: "RUB"},
				{Key: "R-KZ", Title: "Розница KZ", Country: "KZ", LegalEntity: "MFKaz", Currency: "KZT"},
				{Key: "R-UZ", Title: "Розница UZ", Country: "UZ", LegalEntity: "MFUz", Currency: "UZS"},
				{Key: "MP-RU", Title: "Маркетплейсы РФ", Country: "RU", LegalEntity: "TDMF", Currency: "RUB"},
				{Key: "MP-KZ", Title: "Маркетплейсы KZ", Country: "KZ", LegalEntity: "MFKaz", Currency: "KZT"},
				{Key: "MP-UZ", Title: "Маркетплейсы UZ", Country: "UZ", LegalEntity: "MFUz", Currency: "UZS"},
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
	for _, f := range formRegistryAll() {
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
