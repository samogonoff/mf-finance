package plans

import (
	"context"
	"database/sql"
	"log"
	"strings"
	"time"
)

// Источник строк и факта формы 1.5 (ТЗ §2).
//
// Основной источник — хранимая процедура затрат (ТЗ §2.1). Она без параметров,
// поэтому отбор и обработка выполняются здесь, в Go, ровно в том порядке, в
// каком их задаёт ТЗ: сначала выбрасываются служебные коды PL, затем история до
// 2024 года, затем переименовываются сценарии. Порядок важен — переименование
// после отбора дешевле, а отбор по CodePL уменьшает объём до всего остального.
//
// При PLANS_MOCK=1 или db == nil работают фикстуры exp_mock.go: без них форма не
// разрабатывается и не демонстрируется без VPN к OLAP.

// ExpCostCell — строка источника затрат (ТЗ §2.1), уже после обработки.
type ExpCostCell struct {
	Scenario       string
	Year           int
	Month          int
	CodePL         int
	GroupPL        string
	Expense        string
	CodeCFO        int
	CFO            string
	GroupCFO1      string
	GroupCFO2      string
	Country        string
	CountryCompany string
	Company        string
	Amount         float64
}

// ExpCfoAttrs — атрибуты ЦФО из справочника (ТЗ §2.4): то, чего нет в строке
// затрат — город, тип магазина, региональный менеджер, юридическое лицо.
type ExpCfoAttrs struct {
	CodeCFO     int
	CFO         string
	GroupCFO1   string
	GroupCFO2   string
	Country     string
	LegalEntity string
	StoreType   string
	RegManager  string
}

// ExpDataSource — данные ТЗ §2 для формы 1.5.
type ExpDataSource interface {
	// Costs — затраты по виду формы и стране за указанные годы.
	Costs(ctx context.Context, kind, country string, years []int) ([]ExpCostCell, error)
	// Cfo — справочник ЦФО страны (ТЗ §2.4).
	Cfo(ctx context.Context, kind, country string) (map[int]ExpCfoAttrs, error)
	// SalesPlan — утверждённый план продаж как ВХОД ТОЛЬКО ДЛЯ ЧТЕНИЯ (ТЗ §2.2):
	// ЦФО → месяц → сумма в национальной валюте. Форма 1.5 его не изменяет.
	SalesPlan(ctx context.Context, country string, year int) (map[int]map[int]float64, error)
}

// expSalesParam — значение колонки «Параметр» плана продаж (ТЗ §2.2).
const expSalesParam = "ПРОДАЖИ"

// ExpTables — имена источников. Как и у остальных форм модуля, задаются env'ом:
// на dev-стенде и в проде они отличаются, а зашивать их в код запрещено.
type ExpTables struct {
	CostsProc string // ZATRATY_DLYA_OKSANY_BYSTRYE, база BUDGETING
	CfoTable  string // FinDWH.dbo.[001 CodeCFO]
	// SalesTable — план продаж в НАЦИОНАЛЬНОЙ валюте: Budgeting.dbo.VFORMTOLOADPLAN
	// (у BYN-пары FormToLoadPlan числа другие — ТЗ §2.2).
	SalesTable string
}

// Отбор строк формы (ТЗ §2.1). Значения GroupCFO1 приходят из источника как есть.
const (
	expGroupCfoRetail = "3.Магазины"
	expGroupCfoMP     = "4.Маркетплейсы"
)

// expDropCodePL — служебные коды PL, которые ТЗ §2.1 велит отбросить.
var expDropCodePL = map[int]bool{50: true, 93: true, 1: true}

// Сценарии источника после переименования (ТЗ §2.1 + ответ Н1 от 10.09.2026).
const (
	ExpSrcFact     = "Факт"
	ExpSrcStrategy = "План"
	ExpSrcTactic   = "Тактика"
)

// expScenarioRename — переименование сценариев (ТЗ §2.1). Приёмник и вся форма
// оперируют короткими словами, а процедура отдаёт длинные названия.
//
// Третья строка добавлена ответом Н1: в эталоне сценария три, и этап 1.5
// заполняет именно «Тактика бюджет (таргеты)».
//
// ВНИМАНИЕ. ТЗ §2.1 называет фактическим сценарием «Факт по платежам», а Н1 —
// «Факт по начислениям». Это РАЗНЫЕ вещи, и сводить их в один «Факт» нельзя:
// если процедура отдаёт оба, суммы удвоятся. Пока переименовываем только тот,
// что назван в ТЗ; «Факт по начислениям» ждёт ответа (вопрос 44).
var expScenarioRename = map[string]string{
	"Стратегический бюджет":    ExpSrcStrategy,
	"Факт по платежам":         ExpSrcFact,
	"Тактика бюджет (таргеты)": ExpSrcTactic,
}

// expCutoffYear — строки не позже 31.12.2023 отбрасываются (ТЗ §2.1).
const expCutoffYear = 2024

// expRowCap — предохранитель на объём выборки. Процедура без параметров, и если
// она однажды вернёт не «быстрый» срез, а всю историю, сервис не должен унести
// память процесса. Превышение видно в логе — это сигнал вернуться к источнику.
const expRowCap = 400000

type expSource struct {
	mock   bool
	db     *sql.DB
	tables ExpTables
}

// NewExpSource — конструктор. mock=true или db=nil → фикстуры.
func NewExpSource(mock bool, db *sql.DB, t ExpTables) ExpDataSource {
	if mock || db == nil {
		return expMockSource{}
	}
	return &expSource{db: db, tables: t}
}

// expKindGroup — значение GroupCFO1 для вида формы.
func expKindGroup(kind string) string {
	if kind == ExpKindMP {
		return expGroupCfoMP
	}
	return expGroupCfoRetail
}

// Costs — вызов процедуры затрат с обработкой ТЗ §2.1.
//
// Отбор по стране и группе ЦФО делается здесь, а не в SQL: процедура
// параметров не принимает, оборачивать её во временную таблицу ради фильтра
// дороже, чем отбросить лишние строки на чтении.
func (s *expSource) Costs(ctx context.Context, kind, country string, years []int) ([]ExpCostCell, error) {
	proc := strings.TrimSpace(s.tables.CostsProc)
	if proc == "" {
		return nil, nil
	}
	wantYear := map[int]bool{}
	for _, y := range years {
		wantYear[y] = true
	}
	group := expKindGroup(kind)

	ctx, cancel := context.WithTimeout(ctx, 5*time.Minute)
	defer cancel()
	rows, err := s.db.QueryContext(ctx, "EXEC "+proc)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	out := make([]ExpCostCell, 0, 4096)
	capped := false
	for rows.Next() {
		var (
			c        ExpCostCell
			month    time.Time
			scenario sql.NullString
			expense  sql.NullString
			groupPL  sql.NullString
			cfo      sql.NullString
			gr1, gr2 sql.NullString
			cn, cnCo sql.NullString
			company  sql.NullString
			amount   sql.NullFloat64
			codePL   sql.NullInt64
			codeCFO  sql.NullInt64
		)
		if err := rows.Scan(&scenario, &month, &codePL, &groupPL, &expense, &codeCFO,
			&cfo, &gr1, &gr2, &cn, &cnCo, &company, &amount); err != nil {
			return nil, err
		}
		c.CodePL = int(codePL.Int64)
		// 1. служебные коды PL (ТЗ §2.1, п.1)
		if expDropCodePL[c.CodePL] {
			continue
		}
		// 2. история до 2024 года (ТЗ §2.1, п.2)
		if month.Year() < expCutoffYear {
			continue
		}
		c.Year, c.Month = month.Year(), int(month.Month())
		if len(wantYear) > 0 && !wantYear[c.Year] {
			continue
		}
		c.GroupCFO1 = gr1.String
		if c.GroupCFO1 != group {
			continue
		}
		c.Country = cn.String
		if country != "" && c.Country != country {
			continue
		}
		// 3. переименование сценариев (ТЗ §2.1, п.3)
		c.Scenario = scenario.String
		if v, ok := expScenarioRename[c.Scenario]; ok {
			c.Scenario = v
		}
		c.GroupPL, c.Expense = groupPL.String, expense.String
		c.CodeCFO, c.CFO = int(codeCFO.Int64), cfo.String
		c.GroupCFO2, c.CountryCompany, c.Company = gr2.String, cnCo.String, company.String
		c.Amount = amount.Float64
		out = append(out, c)
		if len(out) >= expRowCap {
			capped = true
			break
		}
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	if capped {
		log.Printf("plans/exp: выборка затрат обрезана на %d строк — источник отдал больше «быстрого» среза", expRowCap)
	}
	return out, nil
}

// Cfo — справочник ЦФО (ТЗ §2.4). Ошибка справочника не фатальна для формы:
// строки соберутся по атрибутам самого источника затрат, просто без города и РМ.
func (s *expSource) Cfo(ctx context.Context, kind, country string) (map[int]ExpCfoAttrs, error) {
	table := strings.TrimSpace(s.tables.CfoTable)
	if table == "" {
		return map[int]ExpCfoAttrs{}, nil
	}
	q := `SELECT [CodeCFO], [CFO], [GroupCFO1], [GroupCFO2], [Country],
	             [CompanyMF], [TypeOfStore], [RegManager]
	        FROM ` + table + `
	       WHERE [GroupCFO1] = @p1`
	args := []any{expKindGroup(kind)}
	if country != "" {
		q += ` AND [Country] = @p2`
		args = append(args, country)
	}
	ctx, cancel := context.WithTimeout(ctx, time.Minute)
	defer cancel()
	rows, err := s.db.QueryContext(ctx, q, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	out := map[int]ExpCfoAttrs{}
	for rows.Next() {
		var (
			a                     ExpCfoAttrs
			code                  sql.NullInt64
			cfo, g1, g2, cn       sql.NullString
			le, storeType, regMgr sql.NullString
		)
		if err := rows.Scan(&code, &cfo, &g1, &g2, &cn, &le, &storeType, &regMgr); err != nil {
			return nil, err
		}
		a.CodeCFO, a.CFO = int(code.Int64), cfo.String
		a.GroupCFO1, a.GroupCFO2, a.Country = g1.String, g2.String, cn.String
		a.LegalEntity, a.StoreType, a.RegManager = le.String, storeType.String, regMgr.String
		out[a.CodeCFO] = a
	}
	return out, rows.Err()
}

// SalesPlan — утверждённый план продаж (ТЗ §2.2). Вход только для чтения:
// форма 1.5 его не изменяет и в приёмник продаж не пишет.
func (s *expSource) SalesPlan(ctx context.Context, country string, year int) (map[int]map[int]float64, error) {
	table := strings.TrimSpace(s.tables.SalesTable)
	if table == "" {
		return map[int]map[int]float64{}, nil
	}
	ctx, cancel := context.WithTimeout(ctx, time.Minute)
	defer cancel()
	rows, err := s.db.QueryContext(ctx, `
		SELECT [КодЦФО], [Дата], SUM([Значение])
		  FROM `+table+`
		 WHERE [Параметр] = @p1 AND [Страна] = @p2
		   AND [Дата] >= @p3 AND [Дата] < @p4
		 GROUP BY [КодЦФО], [Дата]`,
		expSalesParam, country,
		time.Date(year, 1, 1, 0, 0, 0, 0, time.UTC),
		time.Date(year+1, 1, 1, 0, 0, 0, 0, time.UTC))
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	out := map[int]map[int]float64{}
	for rows.Next() {
		var (
			cfo  sql.NullInt64
			date time.Time
			v    sql.NullFloat64
		)
		if err := rows.Scan(&cfo, &date, &v); err != nil {
			return nil, err
		}
		c := int(cfo.Int64)
		if out[c] == nil {
			out[c] = map[int]float64{}
		}
		out[c][int(date.Month())] += v.Float64
	}
	return out, rows.Err()
}
