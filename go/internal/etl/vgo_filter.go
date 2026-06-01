package etl

import (
	"database/sql"
	"fmt"
	"strings"
)

// ourINNs — ИНН/УНП всех 15 ЮЛ ГК «Марк Формэль». Дубликат debt.Entities()
// (как CountryByINN/companyNames выше), чтобы пакет etl не зависел от reports/debt
// (иначе цикл импорта). Согласованность с debt.OurINNs() проверяется тестом
// TestVGOFilter_listMatchesDebtSeed — при правке seed обнови и этот список.
var ourINNs = []string{
	"690591512", "690719790", "6950135110", "5031159833", "9909349268",
	"9731039708", "695018688905", "141240004842", "305554644", "310170662",
	"6201029158", "CZ08373159", "40003074497", "91310115MAK0BGF88G", "315362-3301-000",
}

// vgoFilter — SQL-фрагмент и именованные параметры для ВГО-фильтра импорта.
// Проводка считается внутригрупповой (ВГО), если upstream проставил ICO=1 ИЛИ
// контрагент — одно из наших ЮЛ (CounterpartyID ∈ ourINNs, с TRIM ведущих/хвостовых
// пробелов — УНП РБ-контрагентов приходят с ведущим пробелом, см. schema-draft.md §6).
//
// Возвращает clause вида
//
//	" AND (p.ICO = 1 OR LTRIM(RTRIM(p.CounterpartyID)) IN (@vgo0,@vgo1,...))"
//
// и срез аргументов (sql.NamedArg) для подстановки. Значения ИНН передаются
// ТОЛЬКО параметрами — без конкатенации в текст запроса.
func vgoFilter() (string, []interface{}) {
	placeholders := make([]string, len(ourINNs))
	args := make([]interface{}, len(ourINNs))
	for i, inn := range ourINNs {
		name := fmt.Sprintf("vgo%d", i)
		placeholders[i] = "@" + name
		args[i] = sql.Named(name, inn)
	}
	clause := " AND (p.ICO = 1 OR LTRIM(RTRIM(p.CounterpartyID)) IN (" +
		strings.Join(placeholders, ",") + "))"
	return clause, args
}
