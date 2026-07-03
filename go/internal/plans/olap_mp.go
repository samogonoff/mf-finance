package plans

import (
	"context"
	"database/sql"
	"fmt"
	"sort"
)

// OlapFactSource — online-факт МП из FinDWH (тот же сервер, что у ВГО-отчёта).
// Использует уже открытый *sql.DB (microsoft/go-mssqldb), без своего пула.
//
// ВНИМАНИЕ (Q1-pending, SPEC §10): точное имя вьюхи (PLANS_MP_FACT_VIEW) и набор
// колонок факта (сценарий «Факт» vs «Тактика») уточняются через cmd/mssql-probe.
// Запрос воспроизводит SUMIFS прототипа: ключи month+code_cfo+scenario+CodePL,
// сумма по Amount_RUB, фильтр по Group_МП_new (сегмент). В dev по умолчанию
// PLANS_MOCK=1, поэтому этот путь до сверки чисел не активен.
type OlapFactSource struct {
	db   *sql.DB
	view string
}

// NewOlapFactSource — конструктор.
func NewOlapFactSource(db *sql.DB, view string) *OlapFactSource {
	return &OlapFactSource{db: db, view: view}
}

// MpFact — агрегированный факт сегмента за период из FinDWH.
func (s *OlapFactSource) MpFact(ctx context.Context, year, month int, segment string) ([]FactRow, error) {
	group, ok := segmentGroup(segment)
	if !ok {
		return []FactRow{}, nil
	}
	// Имя вьюхи подставляется из конфига (идентификатор, не пользовательский ввод).
	q := fmt.Sprintf(`
		SELECT [CodeCFO], [CodePL], SUM([Amount_RUB]) AS amt
		FROM [%s]
		WHERE [Год] = @p1 AND [Номер месяца] = @p2 AND [Group_МП_new] = @p3
		GROUP BY [CodeCFO], [CodePL]`, s.view)
	rows, err := s.db.QueryContext(ctx, q, year, month, group)
	if err != nil {
		return nil, fmt.Errorf("plans olap mp fact: %w", err)
	}
	defer rows.Close()

	platforms := segmentPlatforms(segment)
	out := make([]FactRow, 0)
	for rows.Next() {
		var codeCFO, codePL int
		var amt sql.NullFloat64
		if err := rows.Scan(&codeCFO, &codePL, &amt); err != nil {
			return nil, fmt.Errorf("plans olap mp fact scan: %w", err)
		}
		out = append(out, FactRow{
			CodeCFO:  codeCFO,
			NameCFO:  platforms[codeCFO],
			CodePL:   codePL,
			Year:     year,
			Month:    month,
			Scenario: "Факт",
			Currency: "RUB",
			Amount:   amt.Float64,
		})
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("plans olap mp fact rows: %w", err)
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].CodeCFO != out[j].CodeCFO {
			return out[i].CodeCFO < out[j].CodeCFO
		}
		return out[i].CodePL < out[j].CodePL
	})
	return out, nil
}
