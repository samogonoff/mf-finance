package plans

import (
	"context"
	"database/sql"
	"fmt"
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
	// Projects и SpecialAccess — измерения фильтра страницы эталона (ответ 5.4).
	Projects      string
	SpecialAccess string
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

// expProjectsNone — значение измерения Projects, на котором построен эталон
// (ответ 5.4 от 11.09.2026). Фиксировать его ОБЯЗАТЕЛЬНО: без этого условия
// источник вернёт задвоение по проектам, и суммы формы разойдутся с эталоном на
// величину, которую потом не объяснить.
const expProjectsNone = "Нет проектов!"

// expSpecialAccessAll — второе условие фильтра страницы эталона. Значение «All»
// означает «без отбора», поэтому строка проверяется только на непустоту.
const expSpecialAccessAll = "All"

// expScenarioForForm — сценарий источника идёт в форму.
//
// «Факт по платежам» отбрасывается: листы ввода эталона на нём не построены, а
// сложение двух фактических сценариев дало бы двойные суммы (ответ 44).
func expScenarioForForm(scenario string) bool {
	return scenario != ExpSrcFactPayments && scenario != ""
}

// Сценарии источника. Имена ХРАНЯТСЯ КАК ПРИХОДЯТ (ответ 44 от 11.09.2026:
// «сценарии не схлопывать и не переименовывать на входе; если нужно короткое имя
// — делать отображением»).
//
// Их три — и это важно: ТЗ §2.1 велело переименовывать «Факт по платежам» → «Факт»,
// а листы ввода эталона построены на «Факте ПО НАЧИСЛЕНИЯМ». Источник отдаёт оба,
// они сосуществуют как разные значения одного измерения; схлопни их — и суммы
// удвоятся, а колонка факта покажет не тот сценарий. От неё же зависят база
// сравнения E-18 и база ограничения расчёта ФОТ §6.1.
const (
	// ExpSrcFact — факт, который читают листы ввода эталона.
	ExpSrcFact = "Факт по начислениям"
	// ExpSrcFactPayments — второй фактический сценарий. В форму НЕ идёт: он
	// встречается только в сводных листах источника.
	ExpSrcFactPayments = "Факт по платежам"
	// ExpSrcStrategy — стратегический бюджет планового года.
	ExpSrcStrategy = "Стратегический бюджет"
	// ExpSrcCurrent — «Текущий бюджет». Есть только у МП_RU (ответ 44), у
	// остальных карточек блока нет.
	ExpSrcCurrent = "Текущий бюджет"
	// ExpSrcTactic — то, что заполняет форма и что уходит в приёмник.
	ExpSrcTactic = "Тактика бюджет (таргеты)"
)

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

	// Колонки берём ПО ИМЕНИ, а не по позиции: процедура без параметров, её
	// состав колонок нам не подчиняется, и перестановка столбцов при очередной
	// правке молча сдвинула бы все значения на одну колонку.
	cols, err := rows.Columns()
	if err != nil {
		return nil, err
	}
	idx := map[string]int{}
	for i, c := range cols {
		idx[strings.ToLower(strings.TrimSpace(c))] = i
	}
	need := []string{"scenario", "month", "codepl", "grouppl", "expense", "codecfo",
		"cfo", "groupcfo1", "groupcfo2", "country", "country_company", "company", "amount"}
	for _, n := range need {
		if _, ok := idx[n]; !ok {
			return nil, fmt.Errorf("в выдаче процедуры %s нет колонки %q", proc, n)
		}
	}

	out := make([]ExpCostCell, 0, 4096)
	capped := false
	raw := make([]any, len(cols))
	ptrs := make([]any, len(cols))
	for i := range raw {
		ptrs[i] = &raw[i]
	}
	str := func(name string) string {
		i, ok := idx[name]
		if !ok || raw[i] == nil {
			return ""
		}
		switch v := raw[i].(type) {
		case string:
			return strings.TrimSpace(v)
		case []byte:
			return strings.TrimSpace(string(v))
		default:
			return strings.TrimSpace(fmt.Sprint(v))
		}
	}
	num := func(name string) float64 {
		i, ok := idx[name]
		if !ok || raw[i] == nil {
			return 0
		}
		switch v := raw[i].(type) {
		case float64:
			return v
		case int64:
			return float64(v)
		default:
			return 0
		}
	}
	for rows.Next() {
		if err := rows.Scan(ptrs...); err != nil {
			return nil, err
		}
		var c ExpCostCell
		c.CodePL = int(num("codepl"))
		// 1. служебные коды PL (ТЗ §2.1, п.1)
		if expDropCodePL[c.CodePL] {
			continue
		}
		month, _ := raw[idx["month"]].(time.Time)
		// 2. история до 2024 года (ТЗ §2.1, п.2)
		if month.Year() < expCutoffYear {
			continue
		}
		c.Year, c.Month = month.Year(), int(month.Month())
		if len(wantYear) > 0 && !wantYear[c.Year] {
			continue
		}
		c.GroupCFO1 = str("groupcfo1")
		if c.GroupCFO1 != group {
			continue
		}
		c.Country = str("country")
		if country != "" && c.Country != country {
			continue
		}
		c.Projects, c.SpecialAccess = str("projects"), str("specialaccess")
		// Фильтр страницы эталона (ответ 5.4): Projects обязателен, иначе
		// источник задвоит строки по проектам.
		if c.Projects != "" && c.Projects != expProjectsNone {
			continue
		}
		// 3. сценарий — КАК ПРИШЁЛ (ответ 44). Отбрасываем только «Факт по
		// платежам»: на нём листы ввода эталона не построены.
		c.Scenario = str("scenario")
		if !expScenarioForForm(c.Scenario) {
			continue
		}
		c.GroupPL, c.Expense = str("grouppl"), str("expense")
		c.CodeCFO, c.CFO = int(num("codecfo")), str("cfo")
		c.GroupCFO2, c.CountryCompany, c.Company = str("groupcfo2"), str("country_company"), str("company")
		c.Amount = num("amount")
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
