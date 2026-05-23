package debt

// Список 16 ЮЛ ГК «Марк Формэль» (sheet «список ГК МФ» из
// var/fin/Приложение_к_ТЗ_задолженность_ВГО.xlsx).
// Используется для /filter-options и в качестве справочника при свёртке
// строк из Premaster1C — фильтр на стороне SQL идёт по ИНН/УНП.
var entities = []Entity{
	{INN: "690591512", Name: "ООО «Марк Формэль»", Country: CountryRB},
	{INN: "690719790", Name: "ООО «Формэль»", Country: CountryRB},
	{INN: "6950135110", Name: "ООО ТД «Марк Формэль»", Country: CountryRF},
	{INN: "5031159833", Name: "ООО «МАРК ФОРМЭЛЬ ТЕКС»", Country: CountryRF},
	{INN: "9909349268", Name: "Филиал ООО «Марк Формэль»", Country: CountryRF},
	{INN: "9731039708", Name: "ООО «ПТИР»", Country: CountryRF},
	{INN: "695018688905", Name: "ИП Сипарова Светлана Геннадьевна", Country: CountryRF},
	{INN: "141240004842", Name: "ТОО «Mark Formelle Kazakhstan» (Марк Формэль Казахстан)", Country: CountryKZ},
	{INN: "305554644", Name: "ООО «MARK FORMELLE IT» МЧЖ", Country: CountryUZ},
	{INN: "310170662", Name: "«BARREIROS SOFT» MCHJ", Country: CountryUZ},
	{INN: "6201029158", Name: "MF FASHION TEKSTIL SANAYI VE TICARET ANONIM SIRKETI", Country: CountryTR},
	{INN: "CZ08373159", Name: "Mark Formelle EU s.r.o", Country: CountryCZ},
	{INN: "40003074497", Name: "Hutchison Trading LP", Country: CountryGB},
	{INN: "91310115MAK0BGF88G", Name: "MARK FORMELLE TRADE (SHANGHAI) LIMITED", Country: CountryCN},
	{INN: "315362-3301-000", Name: "ООО «МАРК ФОРМЭЛЬ КЕЙДЖИ»", Country: CountryKG},
}

// Счета БУ из ТЗ-приложения (sheet «счета БУ»). Каждый счёт привязан к стране,
// чтобы UI мог фильтровать «только КЗ-счета», и Go-слой мог применить правильные
// правила свёртки выручки (Дт/Кт корреспонденция отличается по странам).
var accounts = []Account{
	// РБ / РФ
	{Code: "60", Name: "Расчёты с поставщиками и подрядчиками", Country: ""},
	{Code: "62", Name: "Расчёты с покупателями и заказчиками", Country: ""},
	{Code: "76", Name: "Расчёты с разными дебиторами и кредиторами", Country: ""},
	// КЗ
	{Code: "1210", Name: "Краткосрочная дебиторская задолженность покупателей и заказчиков", Country: CountryKZ},
	{Code: "3310", Name: "Краткосрочная задолженность поставщикам и подрядчикам", Country: CountryKZ},
	{Code: "3510", Name: "Краткосрочные авансы полученные", Country: CountryKZ},
	// УЗ
	{Code: "4000", Name: "Расчёты с покупателями и заказчиками", Country: CountryUZ},
	{Code: "4010", Name: "Расчёты с покупателями и заказчиками", Country: CountryUZ},
	{Code: "4090", Name: "Расчёты с розничными покупателями", Country: CountryUZ},
	{Code: "4167", Name: "Прочая долгосрочная кредиторская задолженность", Country: CountryUZ},
	{Code: "4300", Name: "Авансы выданные поставщикам и подрядчикам", Country: CountryUZ},
	{Code: "4800", Name: "Расчёты с разными дебиторами", Country: CountryUZ},
	{Code: "6000", Name: "Расчёты с поставщиками и подрядчиками", Country: CountryUZ},
	{Code: "6300", Name: "Авансы, полученные от покупателей и заказчиков", Country: CountryUZ},
	{Code: "6910", Name: "Начисления за оперативную аренду", Country: CountryUZ},
}

// Entities возвращает копию seed-списка ЮЛ (immutable наружу).
func Entities() []Entity {
	out := make([]Entity, len(entities))
	copy(out, entities)
	return out
}

// Accounts возвращает копию seed-списка счетов.
func Accounts() []Account {
	out := make([]Account, len(accounts))
	copy(out, accounts)
	return out
}
