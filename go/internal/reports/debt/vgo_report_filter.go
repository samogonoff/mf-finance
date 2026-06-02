package debt

import (
	"database/sql"
	"fmt"
	"strings"
)

// Отчёт «Задолженность ВГО» по построению показывает только внутригрупповые
// проводки, поэтому ВГО-фильтр БЕЗУСЛОВНЫЙ (галки «только ВГО» больше нет).
// Определение ВГО совпадает с импортом (etl.vgoFilter): проводка внутригрупповая,
// если upstream проставил ICO=1 ИЛИ контрагент — одно из наших 15 ЮЛ.
// Список ИНН берём из OurINNs() (источник истины — seed.go).

// vgoCHClause — ВГО-условие для ClickHouse-отчёта. Значения инлайнятся в SQL
// (CH-HTTP не параметризует IN в простом виде — так же инлайнятся EntityINNs
// в repo_clickhouse.go); ИНН захардкожены, не пользовательский ввод.
func vgoCHClause() string {
	inns := OurINNs()
	quoted := make([]string, len(inns))
	for i, inn := range inns {
		quoted[i] = "'" + strings.ReplaceAll(inn, "'", "''") + "'"
	}
	return " AND (ico = 1 OR counterparty_id IN (" + strings.Join(quoted, ",") + "))"
}

// vgoMSSQLClause — то же условие для прямого premaster-отчёта, но через именованные
// параметры (значения не конкатенируются в текст SQL). TRIM — УНП РБ-контрагентов
// приходят с ведущим пробелом (schema-draft.md §6). Параметр @vgoN можно
// переиспользовать в нескольких WHERE одного запроса.
func vgoMSSQLClause() (string, []any) {
	inns := OurINNs()
	ph := make([]string, len(inns))
	args := make([]any, len(inns))
	for i, inn := range inns {
		name := fmt.Sprintf("vgo%d", i)
		ph[i] = "@" + name
		args[i] = sql.Named(name, inn)
	}
	clause := " AND (ICO = 1 OR LTRIM(RTRIM(CounterpartyID)) IN (" +
		strings.Join(ph, ",") + "))"
	return clause, args
}
