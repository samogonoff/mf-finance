package plans

import (
	"context"
	"database/sql"
	"fmt"
	"log"
	"regexp"
	"sort"
	"strings"
)

// Источники данных формы «Розница» (ТЗ §11). Онлайн — тот же *sql.DB MSSQL, что у
// МП (FinDWH/Budgeting/Checks на 10.10.6.15); при PLANS_MOCK=1 или db==nil —
// фикстуры retail_mock.go, чтобы форма и тесты работали без сети/VPN.
//
// Все запросы best-effort, как в olap_mp.go: сбой одного источника не роняет
// остальные — форма откроется без стратегии или без истории плана, и это видно
// в логе и в контрольных сверках §7 (недоступный источник помечается skipped,
// а не «расхождение 0»).

// RetailDataSource — данные ТЗ §11.
type RetailDataSource interface {
	// Stores — справочник магазинов страны (FinDWH.dbo.[001 CodeCFO]).
	Stores(ctx context.Context, country string) ([]RetailStore, error)
	// Fact — факт продаж (выручка с НДС) за указанные годы, все месяцы.
	Fact(ctx context.Context, country string, years []int) ([]RetailFactCell, error)
	// Strategy — стратегия года (Budgeting, нац. валюта).
	Strategy(ctx context.Context, country string, year int) ([]RetailFactCell, error)
	// PlanHistory — ранее утверждённая тактика (Checks.dbo.plan_saler_st*),
	// только чтение. Второй результат — соответствие CodeCFO → KLIENT_ID:
	// в [001 CodeCFO] колонки KLIENT_ID нет, а ТЗ §2 требует её как атрибут строки.
	PlanHistory(ctx context.Context, country string, year int) ([]RetailFactCell, map[int]string, error)
}

// RetailTables — имена таблиц-источников (ТЗ §11: «все имена таблиц — через конфиг»).
type RetailTables struct {
	StoreTable       string // FinDWH.dbo.[001 CodeCFO]
	StoreGroup       string // значение GroupCFO1 справочника: 'Магазины'
	FactTable        string // FinDWH.dbo.sales_and_COGG_from_FOX_offline_retail
	FactColumns      string // CSV: колонка ЦФО, колонка даты, колонка суммы
	StrategyTable    string // Budgeting.dbo.VFORMTOLOADPLAN (нац. валюта)
	StrategyGroup    string // ГруппыЦФО1 таблицы плана: '3.Магазины'
	PlanHistTable    string // Checks.dbo.plan_saler_st
	PlanHistNewTable string // Checks.dbo.plan_saler_st_new_stores
}

// mssqlRetailSource — online-источник.
type mssqlRetailSource struct {
	db  *sql.DB
	tbl RetailTables
}

// NewRetailSource — источник данных розницы. mock=true или db=nil → фикстуры.
func NewRetailSource(mock bool, db *sql.DB, tbl RetailTables) RetailDataSource {
	if mock || db == nil {
		return NewMockRetailSource()
	}
	return &mssqlRetailSource{db: db, tbl: tbl}
}

// sqlIdent — допустимый идентификатор колонки. Имена колонок приходят из env
// (FactColumns), поэтому подставлять их в SQL без проверки нельзя: env задаёт
// администратор, но одна опечатка с кавычкой ломает запрос неотличимо от сбоя БД.
var sqlIdent = regexp.MustCompile(`^[A-Za-zА-Яа-я_][A-Za-zА-Яа-я0-9_ ]*$`)

// factColumns — колонки таблицы факта. ТЗ §11 называет ТАБЛИЦУ
// (sales_and_COGG_from_FOX_offline_retail), но не её колонки, поэтому они
// настраиваются: PLANS_RETAIL_FACT_COLUMNS="ЦФО,Дата,Сумма". Значения по
// умолчанию — предположение, которое нужно подтвердить пробой
// (cmd/mssql-probe с PROBE_SQL). Пока не подтверждено — работаем на mock'е.
func (s *mssqlRetailSource) factColumns() (cfo, date, amount string, err error) {
	parts := strings.Split(s.tbl.FactColumns, ",")
	if len(parts) != 3 {
		return "", "", "", fmt.Errorf("PLANS_RETAIL_FACT_COLUMNS должен содержать 3 имени через запятую, получено %q", s.tbl.FactColumns)
	}
	for i := range parts {
		parts[i] = strings.TrimSpace(parts[i])
		if !sqlIdent.MatchString(parts[i]) {
			return "", "", "", fmt.Errorf("недопустимое имя колонки %q в PLANS_RETAIL_FACT_COLUMNS", parts[i])
		}
	}
	return parts[0], parts[1], parts[2], nil
}

// Stores — справочник магазинов (ТЗ §11). Фильтр GroupCFO1 берётся из конфига,
// а не из строкового литерала: ТЗ прямо предупреждает, что в справочнике
// 'Магазины', а в таблице плана '3.Магазины', и сопоставление идёт справочником.
func (s *mssqlRetailSource) Stores(ctx context.Context, country string) ([]RetailStore, error) {
	q := fmt.Sprintf(`
		SELECT [GroupCFO1], [GroupCFO2], [CodeCFO], [CFO], [Country], [CodeFOX],
		       [Ploschad], [TypeOfStore], [DateOpen], [DateClose], [StadiyaOfStore],
		       [CompanyMF], [Channel], [CFOold], [Category], [LfLStatus],
		       [RegManager], [Manager], [PLAnalyticCFO1]
		  FROM %s
		 WHERE [GroupCFO1] = @p1
		   AND (@p2 = '' OR [Country] = @p2)`, s.tbl.StoreTable)
	rows, err := s.db.QueryContext(ctx, q, s.tbl.StoreGroup, country)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := make([]RetailStore, 0, 400)
	for rows.Next() {
		var st RetailStore
		var g1, g2, cfo, ctry, fox, typ, open, close_, stage, comp, ch, old, cat, lfl, rm, mgr, pla sql.NullString
		var code sql.NullInt64
		var plo sql.NullFloat64
		if err := rows.Scan(&g1, &g2, &code, &cfo, &ctry, &fox, &plo, &typ, &open, &close_,
			&stage, &comp, &ch, &old, &cat, &lfl, &rm, &mgr, &pla); err != nil {
			return nil, err
		}
		if !code.Valid {
			continue // строка без CodeCFO бесполезна: это ключ строки формы (V-04)
		}
		st = RetailStore{
			CodeCFO: int(code.Int64), GroupCFO1: g1.String, City: g2.String, NameCFO: cfo.String,
			Country: ctry.String, CodeFOX: fox.String, Ploschad: plo.Float64, StoreType: typ.String,
			DateOpen: normalizeSourceDate(open.String), DateClose: normalizeSourceDate(close_.String),
			Stage: stage.String, CompanyMF: comp.String, Channel: ch.String, CFOold: old.String,
			Category: cat.String, LFLStatus: retailLFLStatus(lfl.String), RegManager: rm.String,
			Manager: mgr.String, PLAnalytic: pla.String,
		}
		out = append(out, st)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	sort.Slice(out, func(i, j int) bool { return out[i].CodeCFO < out[j].CodeCFO })
	return out, nil
}

// Fact — факт продаж (выручка с НДС) по магазинам за годы (ТЗ §11).
func (s *mssqlRetailSource) Fact(ctx context.Context, country string, years []int) ([]RetailFactCell, error) {
	if len(years) == 0 || s.tbl.FactTable == "" {
		return []RetailFactCell{}, nil
	}
	cfoCol, dateCol, amtCol, err := s.factColumns()
	if err != nil {
		return nil, err
	}
	q := fmt.Sprintf(`
		SELECT TRY_CONVERT(int, [%s]) AS cfo, YEAR([%s]) AS y, MONTH([%s]) AS m, SUM([%s]) AS amt
		  FROM %s
		 WHERE YEAR([%s]) IN (%s)
		 GROUP BY TRY_CONVERT(int, [%s]), YEAR([%s]), MONTH([%s])`,
		cfoCol, dateCol, dateCol, amtCol, s.tbl.FactTable,
		dateCol, intList(years), cfoCol, dateCol, dateCol)
	return s.scanCells(ctx, q)
}

// Strategy — стратегия года (ТЗ §11). Читаем нац. валюту (VFORMTOLOADPLAN) и
// ТОЛЬКО Параметр='ПРОДАЖИ'. Параметр 'ПРОДАЖИ с НДС с самовыв_' требует базы
// «Сумма+самовывоз» = Значение / курс П.М / (1 − инд. самовывоза), а ни курса П.М,
// ни индекса самовывоза ТЗ не определяет и в источниках их нет — эта база НЕ
// реализована (см. отчёт: открытые вопросы к BI).
func (s *mssqlRetailSource) Strategy(ctx context.Context, country string, year int) ([]RetailFactCell, error) {
	if s.tbl.StrategyTable == "" {
		return []RetailFactCell{}, nil
	}
	q := fmt.Sprintf(`
		SELECT TRY_CONVERT(int, [КодЦФО]) AS cfo, YEAR([Дата]) AS y, MONTH([Дата]) AS m, SUM([Значение]) AS amt
		  FROM %s
		 WHERE [Параметр] = N'ПРОДАЖИ'
		   AND [ГруппыЦФО1] = @p1
		   AND YEAR([Дата]) = @p2
		   AND (@p3 = '' OR [Страна] = @p3)
		 GROUP BY TRY_CONVERT(int, [КодЦФО]), YEAR([Дата]), MONTH([Дата])`, s.tbl.StrategyTable)
	return s.scanCells(ctx, q, s.tbl.StrategyGroup, year, country)
}

// PlanHistory — ранее утверждённая тактика (ТЗ §11, только чтение). Две таблицы:
// действующие магазины и новые (plan_saler_st_new_stores) — у новых своя, потому
// что KLIENT_ID у них ещё нет (те самые «ххх» из §2).
//
// Берётся МАКСИМАЛЬНАЯ версия (VERSION) по магазину и месяцу: история хранит все
// итерации согласования, а «ранее утверждённая тактика» из §4 — последняя из них.
func (s *mssqlRetailSource) PlanHistory(ctx context.Context, country string, year int) ([]RetailFactCell, map[int]string, error) {
	klient := map[int]string{}
	cells := make([]RetailFactCell, 0)
	for _, table := range []string{s.tbl.PlanHistTable, s.tbl.PlanHistNewTable} {
		if table == "" {
			continue
		}
		q := fmt.Sprintf(`
			SELECT TRY_CONVERT(int, h.[CFO]) AS cfo, h.[PYEAR] AS y, h.[PMONTH] AS m,
			       SUM(h.[SUMMA]) AS amt, MAX(h.[KLIENT_ID]) AS klient
			  FROM %s h
			  JOIN (SELECT [CFO], [PYEAR], [PMONTH], MAX([VERSION]) AS v
			          FROM %s WHERE [PYEAR] = @p1 GROUP BY [CFO], [PYEAR], [PMONTH]) last
			    ON last.[CFO] = h.[CFO] AND last.[PYEAR] = h.[PYEAR]
			   AND last.[PMONTH] = h.[PMONTH] AND last.v = h.[VERSION]
			 WHERE h.[PYEAR] = @p1
			 GROUP BY TRY_CONVERT(int, h.[CFO]), h.[PYEAR], h.[PMONTH]`, table, table)
		rows, err := s.db.QueryContext(ctx, q, year)
		if err != nil {
			log.Printf("plans retail: история плана (%s): %v", table, err)
			continue
		}
		for rows.Next() {
			var cfo, y, m sql.NullInt64
			var amt sql.NullFloat64
			var kl sql.NullString
			if err := rows.Scan(&cfo, &y, &m, &amt, &kl); err != nil {
				rows.Close()
				return cells, klient, err
			}
			if !cfo.Valid || !y.Valid || !m.Valid {
				continue
			}
			cells = append(cells, RetailFactCell{
				CodeCFO: int(cfo.Int64), Year: int(y.Int64), Month: int(m.Int64), Amount: amt.Float64,
			})
			if kl.Valid && strings.TrimSpace(kl.String) != "" {
				klient[int(cfo.Int64)] = strings.TrimSpace(kl.String)
			}
		}
		err = rows.Err()
		rows.Close()
		if err != nil {
			return cells, klient, err
		}
	}
	return cells, klient, nil
}

// scanCells — общий разбор запросов «(ЦФО, год, месяц) → сумма».
func (s *mssqlRetailSource) scanCells(ctx context.Context, q string, args ...any) ([]RetailFactCell, error) {
	rows, err := s.db.QueryContext(ctx, q, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := make([]RetailFactCell, 0, 4096)
	for rows.Next() {
		var cfo, y, m sql.NullInt64
		var amt sql.NullFloat64
		if err := rows.Scan(&cfo, &y, &m, &amt); err != nil {
			return nil, err
		}
		if !cfo.Valid || !y.Valid || !m.Valid {
			continue
		}
		out = append(out, RetailFactCell{
			CodeCFO: int(cfo.Int64), Year: int(y.Int64), Month: int(m.Int64), Amount: amt.Float64,
		})
	}
	return out, rows.Err()
}

// intList — список чисел через запятую (числа, безопасно для IN).
func intList(vals []int) string {
	parts := make([]string, 0, len(vals))
	for _, v := range vals {
		parts = append(parts, fmt.Sprintf("%d", v))
	}
	return strings.Join(parts, ",")
}

// normalizeSourceDate — дата из MSSQL к виду YYYY-MM-DD. Источник отдаёт datetime
// строкой, а снапшот атрибутов хранит date; сравнения V-03/V-09/W-05 работают по
// первым 10 символам, поэтому приводим тут, а не в каждой проверке.
func normalizeSourceDate(s string) string {
	s = strings.TrimSpace(s)
	if len(s) >= 10 {
		return s[:10]
	}
	return ""
}
