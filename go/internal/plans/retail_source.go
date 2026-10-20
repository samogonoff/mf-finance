package plans

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"log"
	"regexp"
	"sort"
	"strings"
	"sync/atomic"
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
	ComparisonFact(ctx context.Context, country string, years []int) ([]RetailFactCell, error)
	// Strategy — стратегия года (Budgeting, нац. валюта).
	Strategy(ctx context.Context, country string, year int) ([]RetailFactCell, error)
	// ApprovedTactic and NewStrategy are Budgeting scenarios for bulk operations.
	ApprovedTactic(ctx context.Context, country string, year int) ([]RetailFactCell, error)
	NewStrategy(ctx context.Context, country string, year int) ([]RetailFactCell, error)
	// Cogs — себестоимость магазинов, ТОЛЬКО в BYN (правка ТЗ §6: ExRate в её
	// строках жёстко равен 1, валютной аналитики по себестоимости нет).
	Cogs(ctx context.Context, country string, years []int) ([]RetailFactCell, error)
	// FactFxRate — курс нац. валюты к BYN из факта (правка ТЗ §7):
	// Σ выручки в BYN ÷ Σ выручки в валюте.
	FactFxRate(ctx context.Context, country string, year, month int) (float64, bool, error)
	// TacticFxRate — курс сценария «Тактика» из Budgeting.dbo.ExRateFactTarget
	// (правка ТЗ §8). Нет месяца — ошибка заполнения, а не подстановка дефолта.
	TacticFxRate(ctx context.Context, country string, year, month int, toUSD bool) (float64, error)
	// PlanHistory — ранее утверждённая тактика (Checks.dbo.plan_saler_st*),
	// только чтение. Второй результат — соответствие CodeCFO → KLIENT_ID:
	// в [001 CodeCFO] колонки KLIENT_ID нет, а ТЗ §2 требует её как атрибут строки.
	PlanHistory(ctx context.Context, country string, year int) ([]RetailFactCell, map[int]string, error)
}

// RetailTables — имена таблиц-источников (ТЗ §11: «все имена таблиц — через конфиг»).
//
// Колонок факта здесь больше нет: правки ТЗ 02.09.2026 §4 назвали их точно
// (Month, Country, CodeCFO, CodePL, GroupPL, Company/CompanyID, DbAnalytics1,
// PLDescrBK + четыре колонки сумм), и настраивать их env'ом стало вредно —
// запрос зависит от смысла колонок, а не только от их имён.
type RetailTables struct {
	StoreTable          string // FinDWH.dbo.[001 CodeCFO]
	StoreGroup          string // значение GroupCFO1 справочника: 'Магазины'
	LFLTable            string // DWH.dim.s_klient: актуальный LFL-статус из карточки клиента Лисы (пусто — берётся LfLStatus справочника ЦФО)
	FactTable           string // FinDWH.dbo.sales_and_COGG_from_FOX_offline_retail
	ComparisonFactTable string // Budgeting.dbo.VFORMTOLOADFACT, начисленный факт в нац. валюте
	StrategyTable       string // Budgeting.dbo.VFORMTOLOADPLAN (нац. валюта)
	ApprovedTable       string // Budgeting.dbo.VFORMTOLOADTAKTTARGET (нац. валюта)
	NewStrategyTable    string // optional flat table with Scenario and localized columns
	StrategyGroup       string // ГруппыЦФО1 таблицы плана: '3.Магазины'
	PlanHistTable       string // Checks.dbo.plan_saler_st
	PlanHistNewTable    string // Checks.dbo.plan_saler_st_new_stores
}

// Значения PLDescrBK — разделение выручки и себестоимости (правка ТЗ §1).
// Фильтровать надо по НИМ, а не по КодPL: в представлении КодPL захардкожен
// значениями 1001 и 2001, а кодов 2106 и 6106 в нём нет вообще.
const (
	retailPLRevenue = "RevenueDirect" // выручка
	retailPLCogs    = "COGS"          // себестоимость
)

// Колонки сумм источника (правка ТЗ §4). Себестоимость лежит ТОЛЬКО в
// белорусских рублях (ExRate в её строках жёстко равен 1, правка §6), поэтому
// валютной аналитики по ней не запрашиваем.
const (
	retailAmtWithVATCur = "AmountWithVATCurrencyFact"
	retailAmtWithVATBYN = "AmountWithVATBelRubFact"
	retailAmtWOVATCur   = "AmountWOVATCurrencyFact"
	retailAmtWOVATBYN   = "AmountWOVATBelRubFact"
)

// allViewGuards — три условия ALL_view (правка ТЗ §14). Если форма читает [ALL]
// напрямую, их надо повторить, иначе цифры не сойдутся с отчётами. Для канальных
// объектов (sales_and_COGG_from_FOX_*) они уже применены внутри представления,
// поэтому добавляем условия только при чтении [ALL].
func allViewGuards(table string) string {
	if !strings.Contains(strings.ToUpper(table), "[ALL]") {
		return ""
	}
	return ` AND [Month] >= '2020-01-01' AND [CodeCFO] <> '722' AND [CodePL] <> '1'`
}

// mssqlRetailSource — online-источник.
type mssqlRetailSource struct {
	db  *sql.DB
	tbl RetailTables
	// lisaFailed — последний запрос магазинов с карточкой Лисы не удался (нет прав, таблицы нет) и был
	// повторён без неё: статусы взяты из справочника ЦФО. Нужен подписи источника на экране.
	lisaFailed atomic.Bool
}

// NewRetailSource — источник данных розницы. mock=true или db=nil → фикстуры.
func NewRetailSource(mock bool, db *sql.DB, tbl RetailTables) RetailDataSource {
	if mock || db == nil {
		return NewMockRetailSource()
	}
	return &mssqlRetailSource{db: db, tbl: tbl}
}

// storeJoinCFO — выражение соединения факта со справочником ЦФО (правка ТЗ §13).
// Код 'XXX' в справочнике реально используется — по одной записи на BY, KZ, RU,
// UZ, а в контуре затрат встречаются значения вида 'UZ_XXX'. Поэтому простое
// A.CodeCFO = D.CodeCFO даёт потери строк.
const storeJoinCFO = `IIF(D.[CodeCFO] = 'XXX', D.[Country] + '_' + D.[CodeCFO], D.[CodeCFO])`

// retailStoresQuery — выборка справочника магазинов. С таблицей Лисы (lflTable) к строке
// присоединяется карточка клиента по KLIENT_ID (= CodeFOX): оттуда берётся актуальный LFL-статус.
// Без неё колонки Лисы пустые, и действует LfLStatus справочника ЦФО.
func retailStoresQuery(storeTable, lflTable string) string {
	lisaCols, lisaJoin := "CAST(NULL AS int), CAST(NULL AS nvarchar(40))", ""
	if strings.TrimSpace(lflTable) != "" {
		lisaCols = "K.[ITEM_ID], K.[LFL_STATUS]"
		lisaJoin = fmt.Sprintf(" LEFT JOIN %s K ON K.[ITEM_ID] = TRY_CAST(S.[CodeFOX] AS int)", lflTable)
	}
	return fmt.Sprintf(`
		SELECT S.[GroupCFO1], S.[GroupCFO2], S.[CodeCFO], S.[CFO], S.[Country], S.[CodeFOX],
		       S.[Ploschad], S.[TypeOfStore], S.[DateOpen], S.[DateClose], S.[StadiyaOfStore],
		       S.[CompanyMF], S.[Channel], S.[CFOold], S.[Category], S.[LfLStatus],
		       S.[RegManager], S.[Manager], S.[PLAnalyticCFO1], %s
		  FROM %s S%s
		 WHERE S.[GroupCFO1] = @p1
		   AND (@p2 = '' OR S.[Country] = @p2)`, lisaCols, storeTable, lisaJoin)
}

// RetailStatusSourceLabel — подпись источника LFL-статусов для экрана.
type RetailStatusSourceLabel interface {
	LFLSourceLabel() string
}

// LFLSourceLabel — откуда сейчас берутся LFL-статусы: Лиса (с запасом справочник ЦФО), только справочник
// ЦФО (Лиса не настроена) или справочник ЦФО из-за недоступности Лисы.
func (s *mssqlRetailSource) LFLSourceLabel() string {
	return retailLFLSourceLabel(s.tbl.StoreTable, s.tbl.LFLTable, s.lisaFailed.Load())
}

func retailLFLSourceLabel(storeTable, lflTable string, lisaFailed bool) string {
	cfo := storeTable + ".LfLStatus"
	switch {
	case strings.TrimSpace(lflTable) == "":
		return cfo
	case lisaFailed:
		return cfo + " (карточки Лисы " + lflTable + " недоступны)"
	default:
		return lflTable + ".LFL_STATUS (нет карточки в Лисе — " + cfo + ")"
	}
}

// retailLFLSource — LFL-статус магазина: если карточка клиента найдена в Лисе, берётся её статус
// (пустой — значит, статус не присвоен), иначе — LfLStatus справочника ЦФО.
func retailLFLSource(cfoStatus, lisaStatus string, lisaFound bool) string {
	if lisaFound {
		return strings.TrimSpace(lisaStatus)
	}
	return cfoStatus
}

// Stores — справочник магазинов (ТЗ §11). Фильтр GroupCFO1 берётся из конфига,
// а не из строкового литерала: ТЗ прямо предупреждает, что в справочнике
// 'Магазины', а в таблице плана '3.Магазины', и сопоставление идёт справочником.
func (s *mssqlRetailSource) Stores(ctx context.Context, country string) ([]RetailStore, error) {
	rows, err := s.db.QueryContext(ctx, retailStoresQuery(s.tbl.StoreTable, s.tbl.LFLTable), s.tbl.StoreGroup, country)
	if err != nil && strings.TrimSpace(s.tbl.LFLTable) != "" {
		// Лиса нужна только для уточнения статуса: нет прав на её таблицу — не должно ронять форму,
		// синхронизацию и свод. Повторяем без присоединения, статусы — из справочника ЦФО.
		log.Printf("plans retail: карточки Лисы (%s) недоступны, LFL-статус взят из справочника ЦФО: %v", s.tbl.LFLTable, err)
		s.lisaFailed.Store(true)
		rows, err = s.db.QueryContext(ctx, retailStoresQuery(s.tbl.StoreTable, ""), s.tbl.StoreGroup, country)
	} else if err == nil {
		s.lisaFailed.Store(false)
	}
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := make([]RetailStore, 0, 400)
	skipped := 0
	for rows.Next() {
		var st RetailStore
		var g1, g2, cfo, ctry, fox, typ, open, close_, stage, comp, ch, old, cat, lfl, rm, mgr, pla, lisa sql.NullString
		var lisaID sql.NullInt64
		// CodeCFO читаем СТРОКОЙ, а не int64: в справочнике встречаются
		// нечисловые коды («40RUBK»), и на первом же таком синк падал целиком —
		// вместе с ним пропадали все магазины, а не одна битая строка.
		var code sql.NullString
		var plo sql.NullFloat64
		if err := rows.Scan(&g1, &g2, &code, &cfo, &ctry, &fox, &plo, &typ, &open, &close_,
			&stage, &comp, &ch, &old, &cat, &lfl, &rm, &mgr, &pla, &lisaID, &lisa); err != nil {
			return nil, err
		}
		codeNum, ok := parseNumericCFO(code.String)
		if !code.Valid || !ok {
			// Строка формы ключуется числовым CodeCFO (V-04). Нечисловой код —
			// это не магазин розницы (валютные/технические ЦФО), пропускаем.
			skipped++
			continue
		}
		// Актуальный статус — из карточки клиента Лисы: справочник ЦФО хранит устаревший (у большинства
		// магазинов «новый»), из-за чего у них не считались LFL и % выполнения.
		lflSrc := retailLFLSource(lfl.String, lisa.String, lisaID.Valid)
		st = RetailStore{
			CodeCFO: codeNum, GroupCFO1: g1.String, City: g2.String, NameCFO: cfo.String,
			Country: ctry.String, CodeFOX: fox.String, Ploschad: plo.Float64, StoreType: typ.String,
			DateOpen: retailDateOrEmpty(open.String), DateClose: retailDateOrEmpty(close_.String),
			Stage: stage.String, CompanyMF: comp.String, Channel: ch.String, CFOold: old.String,
			Category: cat.String, LFLStatus: retailLFLStatus(lflSrc), LFLRaw: strings.TrimSpace(lflSrc), RegManager: rm.String,
			Manager: mgr.String, PLAnalytic: pla.String,
		}
		out = append(out, st)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	if skipped > 0 {
		// Молча терять строки нельзя: расхождение «в справочнике N, в форме N−k»
		// иначе всплывёт только в контрольной сверке полноты (ТЗ §7).
		log.Printf("plans retail: пропущено строк справочника с нечисловым CodeCFO: %d", skipped)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].CodeCFO < out[j].CodeCFO })
	return out, nil
}

// Fact — факт продаж (выручка с НДС) по магазинам за годы (ТЗ §11).
//
// Правки ТЗ 02.09.2026, §1–§3: фильтр по PLDescrBK (а не по КодPL — в
// представлении он захардкожен 1001/2001), выручка хранится С МИНУСОМ и берётся
// по модулю, а знаки офлайна и онлайна противоположны: в офлайне выручка минус и
// себестоимость плюс, в Click & Collect наоборот. Поэтому сумма без фильтра по
// PLDescrBK бессмысленна — слагаемые частично гасят друг друга, и фильтр
// обязателен ВСЕГДА, а не «по возможности».
func (s *mssqlRetailSource) Fact(ctx context.Context, country string, years []int) ([]RetailFactCell, error) {
	if len(years) == 0 || s.tbl.FactTable == "" {
		return []RetailFactCell{}, nil
	}
	// ABS вокруг суммы, а не вокруг слагаемых: знак задан направлением строки
	// (выручка/себестоимость), и внутри одного PLDescrBK он однороден.
	q := fmt.Sprintf(`
		SELECT TRY_CONVERT(int, [CodeCFO]) AS cfo, YEAR([Month]) AS y, MONTH([Month]) AS m,
		       ABS(SUM([%s])) AS amt
		  FROM %s
		 WHERE [PLDescrBK] = @p1
		   AND YEAR([Month]) IN (%s)
		   AND (@p2 = '' OR [Country] = @p2)
		   -- Страна '- ' (с пробелом) — 21 088 строк факта (правка ТЗ §17).
		   -- Исключаем ЯВНО: это не страна контура, но и потеряться она не должна
		   -- незаметно, поэтому условие видно в запросе, а не спрятано в фильтре.
		   AND LTRIM(RTRIM([Country])) <> '-'%s
		 GROUP BY TRY_CONVERT(int, [CodeCFO]), YEAR([Month]), MONTH([Month])`,
		retailAmtWithVATCur, s.tbl.FactTable, intList(years), allViewGuards(s.tbl.FactTable))
	return s.scanCells(ctx, q, retailPLRevenue, country)
}

func (s *mssqlRetailSource) ComparisonFact(ctx context.Context, country string, years []int) ([]RetailFactCell, error) {
	if len(years) == 0 {
		return []RetailFactCell{}, nil
	}
	table := s.tbl.ComparisonFactTable
	if table == "" {
		table = "Budgeting.dbo.VFORMTOLOADFACT"
	}
	q := fmt.Sprintf(`
		SELECT TRY_CONVERT(int, [КодЦФО]) AS cfo, YEAR([Дата]) AS y, MONTH([Дата]) AS m, SUM([Значение]) AS amt
		  FROM %s
		 WHERE [Параметр] = N'ПРОДАЖИ'
		   AND [ГруппыЦФО1] = @p1
		   AND YEAR([Дата]) IN (%s)
		   AND (@p2 = '' OR [Страна] = @p2)
		 GROUP BY TRY_CONVERT(int, [КодЦФО]), YEAR([Дата]), MONTH([Дата])`, table, intList(years))
	return s.scanCells(ctx, q, s.tbl.StrategyGroup, country)
}

// Cogs — себестоимость магазинов (правка ТЗ §6): лежит ТОЛЬКО в белорусских
// рублях, ExRate в её строках жёстко равен 1. Валютной аналитики по
// себестоимости в источнике нет, поэтому и метод отдаёт BYN.
func (s *mssqlRetailSource) Cogs(ctx context.Context, country string, years []int) ([]RetailFactCell, error) {
	if len(years) == 0 || s.tbl.FactTable == "" {
		return []RetailFactCell{}, nil
	}
	q := fmt.Sprintf(`
		SELECT TRY_CONVERT(int, [CodeCFO]) AS cfo, YEAR([Month]) AS y, MONTH([Month]) AS m,
		       ABS(SUM([%s])) AS amt
		  FROM %s
		 WHERE [PLDescrBK] = @p1
		   AND YEAR([Month]) IN (%s)
		   AND (@p2 = '' OR [Country] = @p2)%s
		 GROUP BY TRY_CONVERT(int, [CodeCFO]), YEAR([Month]), MONTH([Month])`,
		retailAmtWithVATBYN, s.tbl.FactTable, intList(years), allViewGuards(s.tbl.FactTable))
	return s.scanCells(ctx, q, retailPLCogs, country)
}

// FactFxRate — курс нац. валюты к BYN, посчитанный ИЗ ФАКТА (правка ТЗ §7):
// Σ выручки в BYN ÷ Σ выручки в валюте по строкам выручки. Из справочника его
// брать нельзя: в факте лежит тот курс, по которому суммы уже пересчитаны, и
// любой другой даст расхождение с отчётами.
//
// Второй результат — найден ли курс: ноль в знаменателе (нет выручки за период)
// это «нет данных», а не «курс 0».
func (s *mssqlRetailSource) FactFxRate(ctx context.Context, country string, year, month int) (float64, bool, error) {
	if s.tbl.FactTable == "" {
		return 0, false, nil
	}
	q := retailFactFxRateQuery(s.tbl.FactTable)
	var byn, cur sql.NullFloat64
	if err := s.db.QueryRowContext(ctx, q, retailPLRevenue, year, month, country).Scan(&byn, &cur); err != nil {
		return 0, false, err
	}
	if !byn.Valid || !cur.Valid || cur.Float64 == 0 {
		return 0, false, nil
	}
	return byn.Float64 / cur.Float64, true, nil
}

// TacticFxRate — курс сценария «Тактика» (правка ТЗ §8).
//
// Источник — Budgeting.dbo.ExRateFactTarget, для долларового блока
// ExRateToUSDFactTarget. Курсы заданы к белорусскому рублю, по BY значение 1.
// Своего справочника курсов у сценария нет.
//
// Нет строки на месяц — это ОШИБКА ЗАПОЛНЕНИЯ источника, а не повод подставить
// курс по умолчанию: подстановка тихо изменила бы утверждённые суммы.
func (s *mssqlRetailSource) TacticFxRate(ctx context.Context, country string, year, month int, toUSD bool) (float64, error) {
	if eqCountry(country, "BY") && !toUSD {
		return 1, nil // по BY курс к белорусскому рублю равен 1 по определению
	}
	table, q := retailTacticFxRateQuery(toUSD)
	var rate sql.NullFloat64
	if err := s.db.QueryRowContext(ctx, q, year, month, country).Scan(&rate); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return 0, fmt.Errorf("курс тактики за %04d-%02d (%s) не заполнен в %s", year, month, country, table)
		}
		return 0, err
	}
	if !rate.Valid || rate.Float64 == 0 {
		return 0, fmt.Errorf("курс тактики за %04d-%02d (%s) в %s пустой или нулевой", year, month, country, table)
	}
	return rate.Float64, nil
}

// retailFactFxRateQuery — запрос курса из факта; параметры @p1 PLDescrBK,
// @p2 год, @p3 месяц, @p4 страна. Колонок NetsalesCurrency* в представлении
// факта нет (проверено 16.10.2026): курс считается по суммам выручки с НДС в
// BYN и в валюте — за 09.2026 он совпадает с Budgeting.dbo.ExRateFact до
// четвёртого знака (KZ 0,006764 / 0,006763, RU 0,03584 / 0,03585).
func retailFactFxRateQuery(table string) string {
	return fmt.Sprintf(`
		SELECT SUM([%s]) AS byn, SUM([%s]) AS cur
		  FROM %s
		 WHERE [PLDescrBK] = @p1
		   AND YEAR([Month]) = @p2 AND MONTH([Month]) = @p3
		   AND (@p4 = '' OR [Country] = @p4)%s`,
		retailAmtWithVATBYN, retailAmtWithVATCur, table, allViewGuards(table))
}

// retailTacticFxRateQuery — таблица и запрос курса тактики; параметры @p1 год,
// @p2 месяц, @p3 страна. В Budgeting.dbo.ExRateFactTarget и
// ExRateToUSDFactTarget колонки Country, Month, KURS_BANK (колонок ExRate и
// Date нет). Направление курсов разное: в ExRateFactTarget — BYN за единицу
// нац. валюты (BY 1, RU 0,0375 на 11.2026), в ExRateToUSDFactTarget — единиц
// нац. валюты за 1 USD (RU 80). Вызывающий код должен это учитывать.
func retailTacticFxRateQuery(toUSD bool) (table, query string) {
	table = "Budgeting.dbo.ExRateFactTarget"
	if toUSD {
		table = "Budgeting.dbo.ExRateToUSDFactTarget"
	}
	return table, fmt.Sprintf(`
		SELECT TOP 1 [KURS_BANK] FROM %s
		 WHERE YEAR([Month]) = @p1 AND MONTH([Month]) = @p2
		   AND (@p3 = '' OR [Country] = @p3)`, table)
}

// Strategy — стратегия года (ТЗ §11). Читаем нац. валюту (VFORMTOLOADPLAN) и
// ТОЛЬКО Параметр='ПРОДАЖИ'. Параметр 'ПРОДАЖИ с НДС с самовыв_' требует базы
// «Сумма+самовывоз» = Значение / курс П.М / (1 − инд. самовывоза), а ни курса П.М,
// ни индекса самовывоза ТЗ не определяет и в источниках их нет — эта база НЕ
// реализована (см. отчёт: открытые вопросы к BI).
func (s *mssqlRetailSource) Strategy(ctx context.Context, country string, year int) ([]RetailFactCell, error) {
	return s.planScenario(ctx, s.tbl.StrategyTable, country, year)
}

func (s *mssqlRetailSource) ApprovedTactic(ctx context.Context, country string, year int) ([]RetailFactCell, error) {
	table := s.tbl.ApprovedTable
	if table == "" {
		table = "Budgeting.dbo.VFORMTOLOADTAKTTARGET"
	}
	return s.planScenario(ctx, table, country, year)
}

// retailNewStrategyFinPL — витрина, где «НовСтрат» лежит по ЦФО магазинов
// (Scenario = 'НовСтрат', GroupPL = 'ПРОДАЖИ', Amount — национальная валюта).
// Сверка 16.10.2026: за 11.2026 сумма Amount совпадает со значениями
// VFORMTOLOADTAKTTARGET по тем же странам, то есть читается тот же разрез.
const retailNewStrategyFinPL = "FinDWH.dbo.Table_Fin_PL"

// retailNewStrategyFinPLQuery — запрос НовСтрат из Table_Fin_PL; параметры
// @p1 группа ЦФО, @p2 год, @p3 страна (пусто — все).
func retailNewStrategyFinPLQuery(table string) string {
	return fmt.Sprintf(`
		SELECT TRY_CONVERT(int, [CodeCFO]) AS cfo, YEAR([Month]) AS y, MONTH([Month]) AS m, SUM([Amount]) AS amt
		  FROM %s
		 WHERE [Scenario] = N'НовСтрат'
		   AND [GroupPL] = N'ПРОДАЖИ'
		   AND [GroupCFO1] = @p1
		   AND YEAR([Month]) = @p2
		   AND (@p3 = '' OR [Country] = @p3)
		 GROUP BY TRY_CONVERT(int, [CodeCFO]), YEAR([Month]), MONTH([Month])`, table)
}

func (s *mssqlRetailSource) NewStrategy(ctx context.Context, country string, year int) ([]RetailFactCell, error) {
	if s.tbl.NewStrategyTable == "" {
		// Плоские таблицы Budgeting (VFORMTOLOADPLAN и др.) колонки Scenario не
		// имеют и здесь не используются — сценарий берётся из Table_Fin_PL.
		return s.scanCells(ctx, retailNewStrategyFinPLQuery(retailNewStrategyFinPL), s.tbl.StrategyGroup, year, country)
	}
	// Отдельно настроенная плоская таблица с локализованными колонками плана и
	// Scenario (PLANS_RETAIL_NEW_STRATEGY_TABLE) — по-прежнему поддерживается.
	q := fmt.Sprintf(`
		SELECT TRY_CONVERT(int, [КодЦФО]) AS cfo, YEAR([Дата]) AS y, MONTH([Дата]) AS m, SUM([Значение]) AS amt
		  FROM %s
		 WHERE [Scenario] = @p1
		   AND [Параметр] = N'ПРОДАЖИ'
		   AND [ГруппыЦФО1] = @p2
		   AND YEAR([Дата]) = @p3
		   AND (@p4 = '' OR [Страна] = @p4)
		 GROUP BY TRY_CONVERT(int, [КодЦФО]), YEAR([Дата]), MONTH([Дата])`, s.tbl.NewStrategyTable)
	return s.scanCells(ctx, q, "НовСтрат", s.tbl.StrategyGroup, year, country)
}

// The flat plan tables identify scenarios by table and have no Scenario column.
func (s *mssqlRetailSource) planScenario(ctx context.Context, table, country string, year int) ([]RetailFactCell, error) {
	if table == "" {
		return []RetailFactCell{}, nil
	}
	q := fmt.Sprintf(`
		SELECT TRY_CONVERT(int, [КодЦФО]) AS cfo, YEAR([Дата]) AS y, MONTH([Дата]) AS m, SUM([Значение]) AS amt
		  FROM %s
		 WHERE [Параметр] = N'ПРОДАЖИ'
		   AND [ГруппыЦФО1] = @p1
		   AND YEAR([Дата]) = @p2
		   AND (@p3 = '' OR [Страна] = @p3)
		 GROUP BY TRY_CONVERT(int, [КодЦФО]), YEAR([Дата]), MONTH([Дата])`, table)
	return s.scanCells(ctx, q, s.tbl.StrategyGroup, year, country)
}

// RetailPlanVersionLabel — метка версии тактики при переносе истории плана.
// Открытый вопрос §12 п.8 закрыт нашим решением (27.08.2026: «это что мы можем
// сразу закрыть»), поэтому формат задаёт приложение:
//
//	TAKT-<год>-<месяц>-v<номер версии карточки>   →   TAKT-2026-07-v003
//
// Три свойства, ради которых формат именно такой:
//   - лексикографический порядок совпадает с хронологическим, поэтому
//     MAX([VERSION]) в PlanHistory возвращает последнюю итерацию согласования
//     (номер версии дополнен нулями — иначе «v10» стало бы меньше «v9»);
//   - метка несёт период и номер версии, то есть однозначно ведёт к строке
//     card_version, из которой перенесена история;
//   - фиксированная длина 18 символов — влезает в nvarchar-колонку источника.
func RetailPlanVersionLabel(year, month, versionNo int) string {
	return fmt.Sprintf("TAKT-%04d-%02d-v%03d", year, month, versionNo)
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

// numericCFO — код ЦФО целиком из цифр. В справочнике [001 CodeCFO] попадаются
// технические коды вида «40RUBK» (валютные разрезы, а не магазины): строка формы
// ключуется числовым кодом (V-04), поэтому такие записи в розницу не берём.
var numericCFO = regexp.MustCompile(`^[0-9]+$`)

// parseNumericCFO — число из кода ЦФО; ok=false, если код нечисловой.
func parseNumericCFO(raw string) (int, bool) {
	raw = strings.TrimSpace(raw)
	if raw == "" || !numericCFO.MatchString(raw) {
		return 0, false
	}
	n := 0
	for _, ch := range raw {
		n = n*10 + int(ch-'0')
		if n > 1_000_000_000 { // защита от абсурдно длинных строк
			return 0, false
		}
	}
	return n, true
}
