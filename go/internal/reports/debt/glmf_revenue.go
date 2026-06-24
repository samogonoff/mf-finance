package debt

import "strings"

// Выручка ВГО по ТОЧНЫМ Дт/Кт-корреспонденциям из ТЗ (per country, см.
// docs/reports/debt/tz-requirements.md). Это эталон постановщика; group_pl='ПРОДАЖИ'
// в fact_glmf — удобный прокси, сверяемый на CHECKPOINT B.

// revCorr — корреспонденция выручки: Дт-префикс → Кт-префикс.
type revCorr struct{ Dr, Cr string }

// revenueCorrespondences — Дт/Кт выручки по странам (ТЗ).
var revenueCorrespondences = map[Country][]revCorr{
	CountryRB: {{"62.1", "90.1.1"}},
	CountryRF: {{"62", "90.01"}, {"76.09", "90.01"}},
	CountryKZ: {{"1210", "6010"}},
	CountryUZ: {{"4015", "9010"}},
}

// accMatch — субсчёт acc совпадает с префиксом ТЗ: равен ему ИЛИ начинается с
// "prefix." (точечная граница, чтобы 62.1 не цеплял 62.10).
func accMatch(acc, prefix string) bool {
	return acc == prefix || strings.HasPrefix(acc, prefix+".")
}

// IsRevenue — пара (dr, cr) для страны = выручка по ТЗ-корреспонденции.
func IsRevenue(country Country, dr, cr string) bool {
	for _, c := range revenueCorrespondences[country] {
		if accMatch(dr, c.Dr) && accMatch(cr, c.Cr) {
			return true
		}
	}
	return false
}

// revenueCHClause — SQL-условие выручки для fact_glmf: OR по странам, в каждой —
// OR по корреспонденциям, префиксное совпадение (равенство ИЛИ LIKE 'p.%').
// Страны идут в детерминированном порядке (РБ, РФ, КЗ, УЗ) для стабильности SQL/тестов.
func revenueCHClause() string {
	order := []Country{CountryRB, CountryRF, CountryKZ, CountryUZ}
	var countryOr []string
	for _, country := range order {
		corrs := revenueCorrespondences[country]
		if len(corrs) == 0 {
			continue
		}
		var corrOr []string
		for _, c := range corrs {
			corrOr = append(corrOr,
				"("+accMatchSQL("dr_acc", c.Dr)+" AND "+accMatchSQL("cr_acc", c.Cr)+")")
		}
		countryOr = append(countryOr,
			"(country = '"+string(country)+"' AND ("+strings.Join(corrOr, " OR ")+"))")
	}
	return "(" + strings.Join(countryOr, "\n     OR ") + ")"
}

// accMatchSQL — SQL-фрагмент префиксного совпадения субсчёта (col = 'p' OR col LIKE 'p.%').
func accMatchSQL(col, prefix string) string {
	return "(" + col + " = '" + prefix + "' OR " + col + " LIKE '" + prefix + ".%')"
}
