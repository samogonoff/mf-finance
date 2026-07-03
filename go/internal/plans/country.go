package plans

import (
	"context"
	"database/sql"
)

// Единый справочник стран (ТЗ: «чтобы все коды/сокращения были в одном месте,
// и сведение строилось через него, а не хардкоды в коде»). Канонический маппинг
// живёт ЗДЕСЬ (одна точка), наполняет справочник dir_country (данные из Лисы
// s_country) и используется при разборе страны магазина. Доменные коды:
// RU/BY/KZ/UZ — рабочие страны планирования; сокращения РФ/РБ/КЗ/УЗ — для UI.

// CountryRef — каноничная запись страны.
type CountryRef struct {
	Domain string // RU|BY|KZ|UZ — внутренний код планирования
	AbbrRU string // РФ|РБ|КЗ|УЗ — сокращение
	NameRU string // имя в Лисе (s_country.NAIM)
	ISO    string // ISO-3166 numeric (s_country.CODE)
}

// canonicalCountries — рабочие страны планирования (источник истины кода→страна).
// Сведение и маппинг должны идти через этот список / dir_country, а не через
// разрозненные switch'и по строкам.
var canonicalCountries = []CountryRef{
	{Domain: "BY", AbbrRU: "РБ", NameRU: "Беларусь", ISO: "112"},
	{Domain: "RU", AbbrRU: "РФ", NameRU: "Россия", ISO: "643"},
	{Domain: "KZ", AbbrRU: "КЗ", NameRU: "Казахстан", ISO: "398"},
	{Domain: "UZ", AbbrRU: "УЗ", NameRU: "Узбекистан", ISO: "860"},
}

var countryByNameMap = func() map[string]CountryRef {
	m := make(map[string]CountryRef, len(canonicalCountries))
	for _, c := range canonicalCountries {
		m[c.NameRU] = c
	}
	return m
}()

// domainByCountryName — доменный код по русскому имени страны (s_country.NAIM).
// Единственная точка маппинга имя→код (заменяет хардкод-switch'и).
func domainByCountryName(naim string) string {
	if c, ok := countryByNameMap[trimSpace(naim)]; ok {
		return c.Domain
	}
	return ""
}

func trimSpace(s string) string {
	// локальный trim без импорта strings в этом файле (strings уже есть в пакете)
	start, end := 0, len(s)
	for start < end && (s[start] == ' ' || s[start] == '\t') {
		start++
	}
	for end > start && (s[end-1] == ' ' || s[end-1] == '\t') {
		end--
	}
	return s[start:end]
}

// --- Провайдер справочника стран (Лиса s_country → dir_country) ---

type countryProvider struct{ l *LisaDB }

func (countryProvider) Code() string { return "dir_country" }

func (p countryProvider) Fetch(ctx context.Context) ([]SyncRow, error) {
	if p.l == nil || p.l.db == nil {
		// Без Лисы наполняем хотя бы рабочими странами (канон) — справочник не пуст.
		return canonicalCountryRows(), nil
	}
	const q = `SELECT ITEM_ID, CODE, NAIM, FULL_NAIM FROM s_country WHERE NAIM IS NOT NULL`
	rows, err := p.l.db.QueryContext(ctx, q)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := make([]SyncRow, 0, 64)
	for rows.Next() {
		var item, code, naim, full sql.NullString
		if err := rows.Scan(&item, &code, &naim, &full); err != nil {
			return nil, err
		}
		name := ns(naim)
		if name == "" {
			continue
		}
		out = append(out, SyncRow{
			ExternalID: ns(item),
			Payload: map[string]any{
				"lisa_id":   ns(item),
				"iso_code":  ns(code),
				"name":      name,
				"full_name": ns(full),
				"domain":    domainByCountryName(name),
				"abbr_ru":   abbrByName(name),
			},
		})
	}
	return out, rows.Err()
}

var domainByAbbrMap = func() map[string]string {
	m := make(map[string]string, len(canonicalCountries))
	for _, c := range canonicalCountries {
		m[c.AbbrRU] = c.Domain
	}
	return m
}()

// domainByAbbr — доменный код по сокращению (РБ→BY, РФ→RU…). Через единый
// справочник стран, не хардкод по месту использования.
func domainByAbbr(abbr string) string {
	if d, ok := domainByAbbrMap[trimSpace(abbr)]; ok {
		return d
	}
	return ""
}

func abbrByName(naim string) string {
	if c, ok := countryByNameMap[trimSpace(naim)]; ok {
		return c.AbbrRU
	}
	return ""
}

// canonicalCountryRows — фолбэк-набор (рабочие страны) без Лисы.
func canonicalCountryRows() []SyncRow {
	out := make([]SyncRow, 0, len(canonicalCountries))
	for _, c := range canonicalCountries {
		out = append(out, SyncRow{
			ExternalID: c.ISO,
			Payload: map[string]any{
				"lisa_id": "", "iso_code": c.ISO, "name": c.NameRU,
				"full_name": c.NameRU, "domain": c.Domain, "abbr_ru": c.AbbrRU,
			},
		})
	}
	return out
}
