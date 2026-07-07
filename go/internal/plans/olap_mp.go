package plans

import (
	"context"
	"database/sql"
	"fmt"
	"log"
	"sort"
	"strings"
)

// OlapFactSource — online-факт/план МП из FinDWH-сервера (10.10.6.15). Использует
// уже открытый *sql.DB (microsoft/go-mssqldb) ВГО-отчёта.
//
// Источники (подтверждены probe'ом 2026-07-05, 2-й аналитик по базе):
//   - factTable  = Budgeting.dbo.FormToLoadFact  — ФАКТ (продажи/себестоимость/маржа)
//   - planTable  = Budgeting.dbo.FormToLoadPlan  — ПЛАН/стратегия
//     (тактика-таргеты — FormToLoaTaktTarget; форма ведёт свою тактику в pl_metric)
//     Колонки: Параметр|Страна|КодЦФО|ГруппыЦФО1|ГруппыЦФО2|ЦФО|КодPL|Дата|Значение.
//     КодPL = code_pl (1046 продажи с НДС, 1045 без НДС, 1022/1006 цена площадки,
//     8006 себест.отпускная, 2006+6006 COGS осн+пошив, 9006 розн.маржа). Значение — в
//     **BYN** (WB/1046/2026-05 = 13.63M BYN ≈ 357M RUB прототипа). Статей прямых
//     затрат (64/51/13…) здесь НЕТ — они ручной ввод + штрафы из FINDWHACCESSGROUP.
//   - penaltyView = FinDWH.dbo.FINDWHACCESSGROUP — штрафы (Наименование LIKE '%Штраф%').
//
// Все запросы best-effort: сбой одного источника не роняет остальные (логируется).
type OlapFactSource struct {
	db          *sql.DB
	factTable   string
	planTable   string
	penaltyView string
}

// NewOlapFactSource — конструктор.
func NewOlapFactSource(db *sql.DB, factTable, planTable, penaltyView string) *OlapFactSource {
	return &OlapFactSource{db: db, factTable: factTable, planTable: planTable, penaltyView: penaltyView}
}

// MpFact — факт сегмента за период: продажи/себестоимость (FormToLoadFact, BYN) +
// штрафы (FINDWHACCESSGROUP, RUB, CodePL=mpPenaltyPL). Валюта — в поле FactRow.Currency,
// пересчёт в валюту формы делает MpFormData через convertAmount.
func (s *OlapFactSource) MpFact(ctx context.Context, year, month int, segment string) ([]FactRow, error) {
	platforms := segmentPlatforms(segment)
	if len(platforms) == 0 {
		return []FactRow{}, nil
	}
	out := make([]FactRow, 0)
	if s.factTable != "" {
		if rows, err := s.formRows(ctx, s.factTable, year, month, platforms, "BYN"); err != nil {
			log.Printf("plans mp fact (%s): %v", s.factTable, err)
		} else {
			out = append(out, rows...)
		}
	}
	if s.penaltyView != "" {
		if rows, err := s.penaltyRows(ctx, year, month, platforms); err != nil {
			log.Printf("plans mp penalties (%s): %v", s.penaltyView, err)
		} else {
			out = append(out, rows...)
		}
	}
	sortFacts(out)
	return out, nil
}

// MpStrategy — план/стратегия сегмента (FormToLoadPlan, BYN). Пусто при отсутствии
// таблицы. Используется для колонки «Стратегия» формы (read-only, TPL-09).
func (s *OlapFactSource) MpStrategy(ctx context.Context, year, month int, segment string) ([]FactRow, error) {
	platforms := segmentPlatforms(segment)
	if len(platforms) == 0 || s.planTable == "" {
		return []FactRow{}, nil
	}
	rows, err := s.formRows(ctx, s.planTable, year, month, platforms, "BYN")
	if err != nil {
		return nil, err
	}
	sortFacts(rows)
	return rows, nil
}

// formRows — общий запрос к плоским таблицам Budgeting FormToLoad* (факт/план):
// сумма Значение по (КодЦФО, КодPL) за месяц, фильтр по площадкам сегмента.
func (s *OlapFactSource) formRows(ctx context.Context, table string, year, month int, platforms map[int]string, currency string) ([]FactRow, error) {
	q := fmt.Sprintf(`
		SELECT TRY_CONVERT(int,[КодЦФО]) AS cfo, TRY_CONVERT(int,[КодPL]) AS pl, SUM([Значение]) AS amt
		FROM %s
		WHERE YEAR([Дата]) = @p1 AND MONTH([Дата]) = @p2
		  AND TRY_CONVERT(int,[КодЦФО]) IN (%s)
		GROUP BY TRY_CONVERT(int,[КодЦФО]), TRY_CONVERT(int,[КодPL])`, table, platformIDs(platforms))
	rows, err := s.db.QueryContext(ctx, q, year, month)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := make([]FactRow, 0)
	for rows.Next() {
		var cfo, pl sql.NullInt64
		var amt sql.NullFloat64
		if err := rows.Scan(&cfo, &pl, &amt); err != nil {
			return nil, err
		}
		if !cfo.Valid || !pl.Valid {
			continue
		}
		out = append(out, FactRow{
			CodeCFO: int(cfo.Int64), NameCFO: platforms[int(cfo.Int64)], CodePL: int(pl.Int64),
			Year: year, Month: month, Scenario: "Факт", Currency: currency, Amount: amt.Float64,
		})
	}
	return out, rows.Err()
}

// penaltyRows — штрафы МП из FINDWHACCESSGROUP (Наименование LIKE '%Штраф%'), RUB.
// Фильтр по CodeCFO площадок отсекает не-МП штрафы (УФК, аренда и т.п.).
func (s *OlapFactSource) penaltyRows(ctx context.Context, year, month int, platforms map[int]string) ([]FactRow, error) {
	q := fmt.Sprintf(`
		SELECT TRY_CONVERT(int, [CodeCFO]) AS cfo, SUM([Сумма без НДС_RUR]) AS amt
		FROM [%s]
		WHERE CAST([Наименование] AS nvarchar(max)) LIKE N'%%Штраф%%'
		  AND YEAR([Месяц]) = @p1 AND MONTH([Месяц]) = @p2
		  AND TRY_CONVERT(int, [CodeCFO]) IN (%s)
		GROUP BY TRY_CONVERT(int, [CodeCFO])`, s.penaltyView, platformIDs(platforms))
	rows, err := s.db.QueryContext(ctx, q, year, month)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := make([]FactRow, 0)
	for rows.Next() {
		var cfo sql.NullInt64
		var amt sql.NullFloat64
		if err := rows.Scan(&cfo, &amt); err != nil {
			return nil, err
		}
		if !cfo.Valid {
			continue
		}
		out = append(out, FactRow{
			CodeCFO: int(cfo.Int64), NameCFO: platforms[int(cfo.Int64)], CodePL: mpPenaltyPL,
			Year: year, Month: month, Scenario: "Факт", Currency: "RUB", Amount: amt.Float64,
		})
	}
	return out, rows.Err()
}

// platformIDs — список code_cfo площадок через запятую (числа из seed, безопасно для IN).
func platformIDs(platforms map[int]string) string {
	ids := make([]string, 0, len(platforms))
	for c := range platforms {
		ids = append(ids, fmt.Sprintf("%d", c))
	}
	return strings.Join(ids, ",")
}

func sortFacts(rows []FactRow) {
	sort.Slice(rows, func(i, j int) bool {
		if rows[i].CodeCFO != rows[j].CodeCFO {
			return rows[i].CodeCFO < rows[j].CodeCFO
		}
		return rows[i].CodePL < rows[j].CodePL
	})
}
