package plans

import (
	"context"
	"database/sql"
	"fmt"
	"log"
	"slices"
	"strconv"
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
	// Basis — на чём построена ячейка факта, если иначе, чем вся выборка: ExpFactBasisPayments у платёжных ячеек, которые
	// добавлены к начислениям по месяцам, где начислений нет. Пусто — как задаёт след чтения (expFactBasisFrom).
	Basis string
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
	expGroupCfoIM     = "5.Интернет-магазин"
	// expGroupCfoOpt — группа, в которой лежат ЦФО опта по факту (отделы). Для отбора не используется:
	// опт выбирается по GroupCFO3 (expOptGroup3), а группа ЦФО у него бывает любой; значение нужно
	// подписи строк публикации и фикстурам.
	expGroupCfoOpt = "2.Отделы"
)

// expOptGroups3 — GroupCFO3 опта: лист «ОПТ» файла PL 2025-2026 отбирает расходы по этому полю
// (колонка K листа DATA) для трёх стран — ОПТ РБ / ОПТ РФ / ОПТ УЗ. Форма опта одна на все три страны
// (как лист Excel), поэтому берутся все три значения.
var expOptGroups3 = []string{"ОПТ РБ", "ОПТ РФ", "ОПТ УЗ"}

// expKindFilter — отбор строк источника для вида формы: группа ЦФО 1 и список значений группы ЦФО 3.
// Пустое значение — без отбора по этому полю. У опта группы ЦФО 1 нет: ЦФО опта лежат в «2.Отделы»,
// а признак опта — GroupCFO3. Страна у опта не задаётся: форма сводит три страны сразу.
func expKindFilter(kind string) (group1 string, group3 []string) {
	if kind == ExpKindOpt {
		return "", expOptGroups3
	}
	return expKindGroup(kind), nil
}

// ЦФО 40 относится к интернет-магазину по бизнес-смыслу, хотя в источнике
// помечен группой «Магазины». Он должен входить в ИМ и не дублироваться в рознице.
func expGroupCfoAllowed(kind, group string, codeCFO int) bool {
	if codeCFO == 40 {
		return kind == ExpKindIM
	}
	want, _ := expKindFilter(kind)
	return want == "" || group == want
}

// expKindInBYN — форма вводится в BYN по суммам источника в BYN (колонка AmountBYN), а не в
// национальной валюте страны: у формы несколько стран с разными валютами (опт).
func expKindInBYN(kind string) bool { return kind == ExpKindOpt }

// expSalesGroupWords — части названия группы статьи (GroupPL), по которым строка источника — не расход: продажи,
// себестоимость, маржа, отгрузки. Процедура затрат отдаёт их вместе с расходами по тем же ЦФО (проверка 21.10.2026:
// у розницы РБ продажи, себестоимость и маржа — на сотни миллионов BYN против десятков миллионов расходов), а листы
// Excel «Розница», «ОПТ», «ИМ» берут в блок расходов только расходные коды. В форму расходов такие строки не идут.
var expSalesGroupWords = []string{"ПРОДАЖИ", "СЕБЕСТОИМОСТЬ", "МАРЖА", "ОТГРУЗК"}

// expIsSalesGroupPL — группа статьи относится к продажам, себестоимости, марже или отгрузкам (не к расходам).
func expIsSalesGroupPL(groupPL string) bool {
	g := strings.ToUpper(strings.TrimSpace(groupPL))
	for _, w := range expSalesGroupWords {
		if strings.Contains(g, w) {
			return true
		}
	}
	return false
}

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

// expProcSem — одна выгрузка процедуры затрат за раз (канал, а не мьютекс: ожидание
// очереди прерывается по контексту).
var expProcSem = make(chan struct{}, 1)

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
	if _, ok := CfoExpCodeFromKind(kind); ok {
		return "Отдел"
	}
	switch kind {
	case ExpKindMP:
		return expGroupCfoMP
	case ExpKindIM:
		return expGroupCfoIM
	case ExpKindOpt:
		return expGroupCfoOpt
	}
	return expGroupCfoRetail
}

// Costs — вызов процедуры затрат с обработкой ТЗ §2.1.
//
// Отбор по стране и группе ЦФО делается здесь, а не в SQL: процедура
// параметров не принимает, оборачивать её во временную таблицу ради фильтра
// дороже, чем отбросить лишние строки на чтении.
func (s *expSource) Costs(ctx context.Context, kind, country string, years []int) ([]ExpCostCell, error) {
	proc := expQualifiedProc(s.tables.CostsProc)
	if proc == "" {
		return nil, nil
	}
	wantYear := map[int]bool{}
	for _, y := range years {
		wantYear[y] = true
	}
	_, group3 := expKindFilter(kind)
	inBYN := expKindInBYN(kind)

	// Процедура без параметров отдаёт весь бюджет (~1 млн строк, около 4 минут на
	// OLAP). Параллельные выгрузки по разным странам и видам форм не нужны: идут по
	// одной, чтобы не класть источник.
	// Очередь ждём с учётом контекста, а таймаут самой выгрузки отсчитываем после
	// получения очереди: иначе формы в хвосте очереди «просыпались» бы с уже
	// истёкшим временем и падали, хотя до них ещё не дошли.
	select {
	case expProcSem <- struct{}{}:
		defer func() { <-expProcSem }()
	case <-ctx.Done():
		return nil, ctx.Err()
	}
	ctx, cancel := context.WithTimeout(ctx, 15*time.Minute)
	defer cancel()
	rows, err := s.db.QueryContext(ctx, "SET NOCOUNT ON; EXEC "+proc)
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
	// Обязательны только колонки, без которых строку не собрать. «country_company» и
	// «company» процедура сейчас не отдаёт вовсе (проверено 19.10.2026): раньше из-за
	// них источник считался сломанным, ошибка уходила в лог, и форма открывалась
	// пустой. Юрлицо и страна компании берутся из справочника ЦФО (Cfo), если их нет.
	need := []string{"scenario", "month", "codepl", "grouppl", "expense", "codecfo",
		"cfo", "groupcfo1", "groupcfo2", "country"}
	for _, n := range need {
		if _, ok := idx[n]; !ok {
			return nil, fmt.Errorf("в выдаче процедуры %s нет колонки %q", proc, n)
		}
	}
	if _, ok := idx["amount"]; !ok {
		if _, ok := idx["amountbyn"]; !ok {
			return nil, fmt.Errorf("в выдаче процедуры %s нет колонки %q или %q", proc, "amount", "amountbyn")
		}
	}
	if len(group3) > 0 {
		if _, ok := idx["groupcfo3"]; !ok {
			return nil, fmt.Errorf("в выдаче процедуры %s нет колонки %q — опт отобрать нельзя", proc, "groupcfo3")
		}
	}
	if inBYN {
		if _, ok := idx["amountbyn"]; !ok {
			return nil, fmt.Errorf("в выдаче процедуры %s нет колонки %q — форма вводится в BYN", proc, "amountbyn")
		}
	}

	out := make([]ExpCostCell, 0, 4096)
	var payments []ExpCostCell // платёжный факт — запасной вариант (см. ниже)
	hasAccrual := false
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
		case int:
			return float64(v)
		case int32:
			return float64(v)
		case float32:
			return float64(v)
		case []byte:
			parsed, _ := strconv.ParseFloat(strings.ReplaceAll(strings.TrimSpace(string(v)), ",", "."), 64)
			return parsed
		case string:
			parsed, _ := strconv.ParseFloat(strings.ReplaceAll(strings.TrimSpace(v), ",", "."), 64)
			return parsed
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
		var month time.Time
		switch v := raw[idx["month"]].(type) {
		case time.Time:
			month = v
		case string:
			month, _ = time.Parse("2006-01-02", strings.TrimSpace(v))
		case []byte:
			month, _ = time.Parse("2006-01-02", strings.TrimSpace(string(v)))
		}
		// 2. история до 2024 года (ТЗ §2.1, п.2)
		if month.Year() < expCutoffYear {
			continue
		}
		c.Year, c.Month = month.Year(), int(month.Month())
		if len(wantYear) > 0 && !wantYear[c.Year] {
			continue
		}
		c.GroupCFO1 = str("groupcfo1")
		if !expGroupCfoAllowed(kind, c.GroupCFO1, int(num("codecfo"))) {
			continue
		}
		if len(group3) > 0 && !slices.Contains(group3, str("groupcfo3")) {
			continue
		}
		c.CodeCFO = int(num("codecfo"))
		if code, ok := CfoExpCodeFromKind(kind); ok && c.CodeCFO != int(code) {
			continue
		}
		c.Country = str("country")
		if country != "" && c.Country != country {
			continue
		}
		c.Projects, c.SpecialAccess = str(expProjectsCol(idx)), str("specialaccess")
		// Фильтр страницы эталона (ответ 5.4): Projects обязателен, иначе
		// источник задвоит строки по проектам.
		if c.Projects != "" && c.Projects != expProjectsNone {
			continue
		}
		// 3. Сценарий — как пришёл из источника. Legacy forms omit payment
		// facts; code-based CFO forms retain the exact scenarios from ТЗ v2.
		c.Scenario = str("scenario")
		paymentsFact := false
		if !expScenarioForKind(kind, c.Scenario) {
			// «Факт по платежам» в форму не идёт, пока в источнике есть «Факт по
			// начислениям» (ответ 44). Сейчас начислений в выдаче нет вовсе, и без
			// запасного варианта колонка факта и база расчётов остались бы пустыми:
			// запоминаем платёжный факт отдельно и подставляем только в таком случае.
			if _, legacy := CfoExpCodeFromKind(kind); !legacy && c.Scenario == ExpSrcFactPayments {
				paymentsFact = true
			} else {
				continue
			}
		}
		c.GroupPL, c.Expense = str("grouppl"), str("expense")
		// Группа статьи — GroupPL_new (актуальная группировка P&L); старая GroupPL остаётся запасной, если новой нет в выдаче.
		if g := str("grouppl_new"); g != "" {
			c.GroupPL = g
		}
		if _, legacy := CfoExpCodeFromKind(kind); !legacy && expIsSalesGroupPL(c.GroupPL) {
			continue // продажи, себестоимость, маржа — не расходы формы
		}
		c.CFO = str("cfo")
		c.GroupCFO2, c.CountryCompany, c.Company = str("groupcfo2"), str("country_company"), str("company")
		if _, ok := idx["amount"]; ok && !inBYN {
			c.Amount = num("amount")
		} else {
			c.Amount = num("amountbyn")
		}
		if c.CountryCompany == "" && kind == ExpKindOpt {
			// У формы несколько стран: по строке должно быть видно, чья она.
			c.CountryCompany = c.Country
		}
		if paymentsFact {
			payments = append(payments, c)
			continue
		}
		if c.Scenario == ExpSrcFact {
			hasAccrual = true
		}
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
	if !hasAccrual && len(payments) > 0 {
		log.Printf("plans/exp: в источнике нет «%s», факт взят из «%s» (%d строк)", ExpSrcFact, ExpSrcFactPayments, len(payments))
		for i := range payments {
			payments[i].Scenario = ExpSrcFact
		}
		noteExpFactBasis(ctx, ExpFactBasisPayments)
		out = append(out, payments...)
	}
	return out, nil
}

// expProjectsCol — колонка проекта в выдаче процедуры: в ТЗ она «Projects», в
// действующей выдаче — «ProjectName». Значение «Нет проектов!» и пусто проходят отбор.
func expProjectsCol(idx map[string]int) string {
	if _, ok := idx["projects"]; ok {
		return "projects"
	}
	return "projectname"
}

// expScenarioForKind keeps legacy source filtering for retail/marketplace forms
// while accepting the exact fact and plan scenario names of CFO forms.
func expScenarioForKind(kind, scenario string) bool {
	if code, ok := CfoExpCodeFromKind(kind); ok {
		spec, exists := LookupCfoExpSpec(code)
		return exists && (scenario == spec.FactScenario || scenario == spec.PlanScenario)
	}
	return expScenarioForForm(scenario)
}

// Cfo — справочник ЦФО (ТЗ §2.4). Ошибка справочника не фатальна для формы:
// строки соберутся по атрибутам самого источника затрат, просто без города и РМ.
func (s *expSource) Cfo(ctx context.Context, kind, country string) (map[int]ExpCfoAttrs, error) {
	table := strings.TrimSpace(s.tables.CfoTable)
	if table == "" || kind == ExpKindOpt {
		// У опта город, тип магазина и РМ не нужны (это атрибуты магазина), а группы ЦФО 1 у него нет.
		return map[int]ExpCfoAttrs{}, nil
	}
	// В процедуре затрат группа называется «3.Магазины», в справочнике ЦФО — «Магазины»
	// (без номера, проверено 19.10.2026): по одному написанию справочник не находился,
	// и у строк не было города, типа магазина и регионального менеджера.
	group := expKindGroup(kind)
	q := `SELECT [CodeCFO], [CFO], [GroupCFO1], [GroupCFO2], [Country],
	             [CompanyMF], [TypeOfStore], [RegManager]
	        FROM ` + table + `
	       WHERE ([GroupCFO1] IN (@p1, @p2)`
	if kind == ExpKindIM {
		q += ` OR TRY_CONVERT(int, [CodeCFO]) = 40`
	}
	q += `)`
	args := []any{group, expGroupWithoutNumber(group)}
	if country != "" {
		q += ` AND [Country] = @p3`
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

// expGroupWithoutNumber — «3.Магазины» → «Магазины»: название группы ЦФО без
// порядкового номера. Строка без номера возвращается как есть.
func expGroupWithoutNumber(group string) string {
	if i := strings.Index(group, "."); i > 0 && i <= 2 {
		if _, err := strconv.Atoi(group[:i]); err == nil {
			return group[i+1:]
		}
	}
	return group
}

// expQualifiedProc — полное имя процедуры затрат. Общий OLAP-пул кабинета подключён к базе
// FinDWH (MSSQL_PREMASTER_DB), а процедура лежит в Budgeting: имя без базы
// («ZATRATY_DLYA_OKSANY_BYSTRYE», так оно и в .env.example) из FinDWH не находится
// (проверено 19.10.2026), форма затрат открывалась пустой, а причина уходила в журнал.
// Дополняется недостающее: «имя» → [Budgeting].[dbo].[имя], «схема.имя» → [Budgeting].[схема].[имя];
// полное «база.схема.имя» используется как есть. Та же логика — в etl.ExpCostsOpts.normalize.
func expQualifiedProc(name string) string {
	name = strings.TrimSpace(name)
	if name == "" {
		return name
	}
	parts := strings.Split(name, ".")
	switch len(parts) {
	case 1:
		return "[Budgeting].[dbo].[" + strings.Trim(parts[0], "[] ") + "]"
	case 2:
		return "[Budgeting].[" + strings.Trim(parts[0], "[] ") + "].[" + strings.Trim(parts[1], "[] ") + "]"
	default:
		return name
	}
}