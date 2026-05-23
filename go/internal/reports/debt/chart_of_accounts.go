package debt

import (
	"strings"
)

// AccountKind — категория счёта для свёртки отчёта.
type AccountKind int

const (
	KindOther   AccountKind = iota // не используется в отчёте
	KindDZ                         // дебиторская задолженность (нам должны)
	KindKZ                         // кредиторская задолженность (мы должны)
	KindRevenue                    // выручка (для RevenuePeriod/RevenueLastMonth)
)

// chartByCountry — белый список счетов по странам с их категорией.
//
// **Источник для РФ** — общая практика 1С + комментарии в `[Payments].[dbo].[fin_debt_report_exec]`
// (где использовалось разделение `DrAcc LIKE '5%' OR '9%'` vs `'6%' OR '7%'`).
//
// **Источник для КЗ/УЗ** — частичные данные из `seed.go::accounts` (приложение к ТЗ)
// плюс типовые планы счетов этих юрисдикций. Для боевого запуска по этим странам
// нужно сверить с ТЗ-приложением «счета БУ» (см. §10.3 п.1 в schema-draft).
//
// **Источник для РБ** — типовая ПБУ РБ (60/62/76, выручка 90.x).
//
// **TR, CZ, GB, CN, KG** — не заполнено. До получения плана счетов от автора ТЗ
// отчёт по этим странам вернёт пустой результат (rawRow → DebtRow никого не пропустит).
//
// **Important:** ключ карты — `account_root` (до первой точки). `62.01` и `62.02` оба попадут
// под root=`62`. Если для отдельных субсчётов нужна другая категория — делаем отдельную
// таблицу `chartBySubaccount`, пока не требуется.
var chartByCountry = map[Country]map[string]AccountKind{
	CountryRF: {
		"60": KindKZ,      // Расчёты с поставщиками и подрядчиками
		"62": KindDZ,      // Расчёты с покупателями и заказчиками
		"76": KindDZ,      // По умолчанию ДЗ; в drill-down видно Dr/Cr знак. Уточнить с ТЗ — иногда 76.05/06 разделяют на ДЗ/КЗ.
		"90": KindRevenue, // Продажи (`90.01.x` — выручка)
	},
	CountryRB: {
		"60": KindKZ,
		"62": KindDZ,
		"76": KindDZ,
		"90": KindRevenue,
	},
	CountryKZ: {
		// 4-значный план счетов
		"1210": KindDZ,      // Краткосрочная дебиторская задолженность покупателей
		"3310": KindKZ,      // Краткосрочная задолженность поставщикам
		"3510": KindKZ,      // Краткосрочные авансы полученные
		"6010": KindRevenue, // Доход от реализации (типовой код)
	},
	CountryUZ: {
		"4000": KindDZ,
		"4010": KindDZ,
		"4090": KindDZ,
		"4300": KindDZ, // Авансы выданные поставщикам (по сути ДЗ к поставщику)
		"4800": KindDZ,
		"6000": KindKZ,
		"6300": KindKZ, // Авансы полученные от покупателей
		"6910": KindKZ,
		"9010": KindRevenue,
		"9020": KindRevenue,
	},
	// TR, CZ, GB, CN, KG: TODO — нужны коды счетов из ТЗ-приложения.
}

// AccountRoot возвращает «корень» счёта — всё до первой точки.
// "62.01" → "62", "9010" → "9010".
func AccountRoot(acc string) string {
	if i := strings.IndexByte(acc, '.'); i > 0 {
		return acc[:i]
	}
	return acc
}

// ClassifyAccount — категория счёта для конкретной страны.
// Возвращает KindOther, если счёт неизвестен — такой счёт в отчёт debt не попадает.
func ClassifyAccount(country Country, acc string) AccountKind {
	chart, ok := chartByCountry[country]
	if !ok {
		return KindOther
	}
	if kind, ok := chart[AccountRoot(acc)]; ok {
		return kind
	}
	return KindOther
}

// AccountsForCountry возвращает плоский список всех счетов страны
// (DZ + KZ + Revenue). Используется для построения WHERE-фильтра SQL
// и точной выборки строк из Premaster1C по интересующим счетам.
func AccountsForCountry(country Country) []string {
	chart, ok := chartByCountry[country]
	if !ok {
		return nil
	}
	out := make([]string, 0, len(chart))
	for acc := range chart {
		out = append(out, acc)
	}
	return out
}

// AccountsForCountries — объединение списков по нескольким странам, дедуп.
func AccountsForCountries(countries []Country) []string {
	seen := map[string]struct{}{}
	for _, c := range countries {
		for _, acc := range AccountsForCountry(c) {
			seen[acc] = struct{}{}
		}
	}
	out := make([]string, 0, len(seen))
	for acc := range seen {
		out = append(out, acc)
	}
	return out
}
