package plans

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"net/url"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"
)

// Чтение затрат формы 1.5 из ClickHouse (finance.exp_costs).
//
// Процедура затрат на OLAP работает 4–5 минут и отдаёт ~1 млн строк, поэтому раз в
// сутки (5:00 по часам MSSQL) её результат заливается в CH (go/internal/etl/exp_costs.go,
// схема — clickhouse/migrations/013_exp_costs.up.sql), а форма читает снимок оттуда за
// секунды. Если таблица пуста (до первой заливки) или CH недоступен — читаем процедуру
// напрямую, как раньше (медленно, но форма не остаётся без строк).

// ExpSourceInfo — источник умеет сказать, на какой момент залит ночной снимок.
type ExpSourceInfo interface {
	SnapshotAsOf(ctx context.Context) (asOf time.Time, ok bool)
}

// ExpSourceTrace — «след» одного чтения: источник пишет сюда, откуда реально прочитал
// данные. Передаётся в контексте вызова Costs, поэтому параллельные загрузки разных форм
// не путают друг друга (общего состояния на процесс нет).
type ExpSourceTrace struct {
	Kind string
	// FactBasis — на чём построен «факт»: пусто — по начислениям (как в ТЗ), ExpFactBasisPayments —
	// запасной вариант по платежам (начислений в выдаче нет). Нужен экрану для честной подписи.
	FactBasis string
}

// ExpFactBasisPayments — факт взят из сценария «Факт по платежам».
const ExpFactBasisPayments = "payments"

func noteExpFactBasis(ctx context.Context, basis string) {
	if tr, ok := ctx.Value(expTraceKey{}).(*ExpSourceTrace); ok {
		tr.FactBasis = basis
	}
}

// expFactBasisFrom — на чём построен факт в чтении, след которого лежит в контексте.
func expFactBasisFrom(ctx context.Context) string {
	if tr, ok := ctx.Value(expTraceKey{}).(*ExpSourceTrace); ok {
		return tr.FactBasis
	}
	return ""
}

type expTraceKey struct{}

// WithExpSourceTrace кладёт след в контекст.
func WithExpSourceTrace(ctx context.Context) (context.Context, *ExpSourceTrace) {
	tr := &ExpSourceTrace{}
	return context.WithValue(ctx, expTraceKey{}, tr), tr
}

func noteExpSource(ctx context.Context, kind string) {
	if tr, ok := ctx.Value(expTraceKey{}).(*ExpSourceTrace); ok {
		tr.Kind = kind
	}
}

// ExpSourceRefresher — источник умеет перечитать данные у первоисточника по кнопке
// «Обновить из источника» (внеплановая ночная заливка, минуты).
type ExpSourceRefresher interface {
	RefreshSource(ctx context.Context) error
}

// Виды источника для экрана.
const (
	ExpSourceKindCH   = "clickhouse"
	ExpSourceKindOLAP = "olap"
)

type chExpSource struct {
	base     ExpDataSource // процедура на OLAP: запасной путь; справочник ЦФО и план продаж
	url     string
	client  *http.Client
	refresh func(ctx context.Context) error
	// accrualFromFinPL — «Факт по начислениям» берётся из реплики P&L finance.findwh_table_fin_pl (см. accrualFact).
	accrualFromFinPL bool
	// planFromFinPL — стратегия и тактика (бюджет) розницы, ИМ и опта берутся из реплики P&L, а не из процедуры затрат;
	// finPLStrategy / finPLTactic — названия сценариев в Table_Fin_PL (без учёта регистра).
	planFromFinPL                bool
	finPLStrategy, finPLTactic string
}

// expKindFromFinPL — виды форм, расходы которых аналитики относят к Table_Fin_PL (FinDWH): розница, интернет-магазин, опт.
// Маркетплейсы читаются из процедуры затрат (ответ аналитика от 22.10.2026): «затраты по сценарию факт идут из
// ZATRATY_DLYA_OKSANY_BYSTRYE».
func expKindFromFinPL(kind string) bool {
	return kind == ExpKindRetail || kind == ExpKindIM || kind == ExpKindOpt
}

// ExpCHSourcePlanFromFinPL включает чтение стратегии и тактики розницы, ИМ и опта из реплики P&L (PLANS_EXP_PLAN_FROM_FINPL).
// strategy и tactic — названия сценариев в Table_Fin_PL («бюджет» и «тактика» по умолчанию). Для других источников — как есть.
func ExpCHSourcePlanFromFinPL(src ExpDataSource, on bool, strategy, tactic string) ExpDataSource {
	if ch, ok := src.(*chExpSource); ok {
		ch.planFromFinPL = on
		ch.finPLStrategy, ch.finPLTactic = strings.TrimSpace(strategy), strings.TrimSpace(tactic)
		if ch.finPLStrategy == "" {
			ch.finPLStrategy = "бюджет"
		}
		if ch.finPLTactic == "" {
			ch.finPLTactic = "тактика"
		}
	}
	return src
}

// ExpCHSourceAccrualFact включает у чтения из ClickHouse факт по начислениям из реплики P&L (PLANS_EXP_FACT_FROM_FINPL).
// Для других источников возвращает его как есть.
func ExpCHSourceAccrualFact(src ExpDataSource, on bool) ExpDataSource {
	if ch, ok := src.(*chExpSource); ok {
		ch.accrualFromFinPL = on
	}
	return src
}

// NewExpCHSource — читатель затрат из ClickHouse с откатом на base. refresh — запуск
// внеплановой заливки (nil — кнопка просто перечитывает снимок).
func NewExpCHSource(base ExpDataSource, chURL, user, password string, refresh func(ctx context.Context) error) ExpDataSource {
	u, err := url.Parse(chURL)
	if err != nil || chURL == "" {
		log.Printf("plans/exp: ClickHouse URL не разобран (%q) — затраты только из процедуры", chURL)
		return base
	}
	u.User = url.UserPassword(user, password)
	return &chExpSource{base: base, url: u.String(), client: &http.Client{Timeout: 90 * time.Second}, refresh: refresh}
}

func (s *chExpSource) Cfo(ctx context.Context, kind, country string) (map[int]ExpCfoAttrs, error) {
	return s.base.Cfo(ctx, kind, country)
}

func (s *chExpSource) SalesPlan(ctx context.Context, country string, year int) (map[int]map[int]float64, error) {
	return s.base.SalesPlan(ctx, country, year)
}

// RefreshSource — внеплановая заливка снимка (минуты); без refresh — ничего не делает.
func (s *chExpSource) RefreshSource(ctx context.Context) error {
	if s.refresh == nil {
		return nil
	}
	return s.refresh(ctx)
}

// SnapshotAsOf — время последней успешной заливки снимка.
func (s *chExpSource) SnapshotAsOf(ctx context.Context) (time.Time, bool) {
	out, err := s.query(ctx, "SELECT toUnixTimestamp(max(finished_at)) FROM finance.exp_costs_etl_runs WHERE status = 'ok'", "TSV")
	if err != nil {
		return time.Time{}, false
	}
	sec, _ := strconv.ParseInt(strings.TrimSpace(string(out)), 10, 64)
	if sec <= 0 {
		return time.Time{}, false
	}
	return time.Unix(sec, 0), true
}

func (s *chExpSource) query(ctx context.Context, sql, format string) ([]byte, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, s.url+"/?default_format="+format, strings.NewReader(sql))
	if err != nil {
		return nil, err
	}
	resp, err := s.client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	if resp.StatusCode >= 300 {
		return nil, fmt.Errorf("clickhouse %s: %s", resp.Status, strings.TrimSpace(string(body)))
	}
	return body, nil
}

// chLit — строковый литерал CH.
func chLit(s string) string {
	s = strings.ReplaceAll(s, `\`, `\\`)
	return "'" + strings.ReplaceAll(s, "'", `\'`) + "'"
}

// expCostsSQL — отбор ТЗ §2.1 тем же правилом, что у чтения процедуры (expSource.Costs):
// группа ЦФО вида формы, страна, годы, без служебных кодов PL и проектов, сценарии вида.
func expCostsSQL(kind, country string, years []int) string {
	return expCostsSQLScenario(kind, country, years, "")
}

// expCostsSQLScenario — то же, но с явным сценарием вместо отбора по виду формы (запасной
// запрос платёжного факта).
func expCostsSQLScenario(kind, country string, years []int, scenario string) string {
	var w []string
	// Сумма в национальной валюте страны; у форм с несколькими странами (опт) — в BYN.
	amountCol := "amount"
	if expKindInBYN(kind) {
		amountCol = "amount_byn"
	}
	group1, group3 := expKindFilter(kind)
	if group1 != "" {
		w = append(w, "group_cfo1 = "+chLit(group1))
	}
	if len(group3) > 0 {
		lits := make([]string, len(group3))
		for i, g := range group3 {
			lits[i] = chLit(g)
		}
		w = append(w, "group_cfo3 IN ("+strings.Join(lits, ", ")+")")
	}
	if country != "" {
		w = append(w, "country = "+chLit(country))
	}
	if len(years) > 0 {
		ys := make([]string, len(years))
		for i, y := range years {
			ys[i] = strconv.Itoa(y)
		}
		w = append(w, "toYear(month) IN ("+strings.Join(ys, ",")+")")
	}
	w = append(w, fmt.Sprintf("month >= '%d-01-01'", expCutoffYear))
	var drop []string
	for c := range expDropCodePL {
		drop = append(drop, strconv.Itoa(c))
	}
	w = append(w, "code_pl NOT IN ("+strings.Join(drop, ",")+")")
	if _, cfo := CfoExpCodeFromKind(kind); !cfo {
		// Продажи, себестоимость, маржа, отгрузки — не расходы (см. expSalesGroupWords).
		for _, word := range expSalesGroupWords {
			w = append(w, "positionCaseInsensitiveUTF8(group_pl, "+chLit(word)+") = 0")
		}
	}
	// Проект: пусто или «Нет проектов!» (ответ 5.4), иначе строки задвоятся по проектам.
	w = append(w, "(project_name = '' OR project_name = "+chLit(expProjectsNone)+")")
	if scenario != "" {
		w = append(w, "scenario = "+chLit(scenario))
	} else if code, ok := CfoExpCodeFromKind(kind); ok {
		w = append(w, "code_cfo = "+strconv.Itoa(int(code)))
		if spec, exists := LookupCfoExpSpec(code); exists {
			w = append(w, "scenario IN ("+chLit(spec.FactScenario)+", "+chLit(spec.PlanScenario)+")")
		} else {
			w = append(w, "0")
		}
	} else {
		w = append(w, "scenario != '' AND scenario != "+chLit(ExpSrcFactPayments))
	}
	return `SELECT scenario, country, toYear(month) AS y, toMonth(month) AS m, code_pl, group_pl, expense,
       code_cfo, cfo, group_cfo1, group_cfo2, project_name, special_access, toFloat64(` + amountCol + `) AS amount
  FROM finance.exp_costs
 WHERE ` + strings.Join(w, "\n   AND ") + `
 LIMIT ` + strconv.Itoa(expRowCap+1)
}

type chCostRow struct {
	Scenario      string  `json:"scenario"`
	Country       string  `json:"country"`
	Year          int     `json:"y"`
	Month         int     `json:"m"`
	CodePL        int     `json:"code_pl"`
	GroupPL       string  `json:"group_pl"`
	Expense       string  `json:"expense"`
	CodeCFO       int     `json:"code_cfo"`
	CFO           string  `json:"cfo"`
	GroupCFO1     string  `json:"group_cfo1"`
	GroupCFO2     string  `json:"group_cfo2"`
	ProjectName   string  `json:"project_name"`
	SpecialAccess string  `json:"special_access"`
	Amount        float64 `json:"amount"`
}

// Costs читает снимок из ClickHouse; при пустой таблице или ошибке — процедура.
func (s *chExpSource) Costs(ctx context.Context, kind, country string, years []int) ([]ExpCostCell, error) {
	have, err := s.query(ctx, "SELECT count() FROM finance.exp_costs", "TSV")
	if err != nil {
		log.Printf("plans/exp: ClickHouse недоступен, читаем процедуру напрямую: %v", err)
		noteExpSource(ctx, ExpSourceKindOLAP)
		return s.base.Costs(ctx, kind, country, years)
	}
	if n, _ := strconv.ParseInt(strings.TrimSpace(string(have)), 10, 64); n == 0 {
		log.Printf("plans/exp: finance.exp_costs пуста (заливка ещё не выполнялась), читаем процедуру напрямую")
		noteExpSource(ctx, ExpSourceKindOLAP)
		return s.base.Costs(ctx, kind, country, years)
	}
	body, err := s.query(ctx, expCostsSQL(kind, country, years), "JSONEachRow")
	if err != nil {
		log.Printf("plans/exp: чтение finance.exp_costs: %v — читаем процедуру напрямую", err)
		noteExpSource(ctx, ExpSourceKindOLAP)
		return s.base.Costs(ctx, kind, country, years)
	}
	noteExpSource(ctx, ExpSourceKindCH)
	out, err := parseChCostRows(body)
	if err != nil {
		return nil, err
	}
	// Стратегия и тактика розницы, ИМ и опта — из реплики P&L, если включено (маркетплейсы остаются на процедуре).
	if s.planFromFinPL && expKindFromFinPL(kind) {
		out = s.planFromReplica(ctx, kind, country, years, out)
	}
	// Факт: «по начислениям» (ТЗ, ответ 44); пока его в выдаче нет — платёжный (как и при чтении
	// процедуры напрямую): без него колонки факта рядом с вводом пустые. Появятся начисления —
	// платёжный снова не используется.
	if _, cfo := CfoExpCodeFromKind(kind); !cfo && !expHasScenario(out, ExpSrcFact) {
		// Факт по начислениям — из реплики P&L (Table_Fin_PL), если включено: в выдаче процедуры его нет.
		pay := s.paymentsFact(ctx, kind, country, years)
		if acc := s.accrualFact(ctx, kind, country, years); len(acc) > 0 {
			// Начисления в реплике появляются после закрытия месяца: по месяцам, где их за срез нет вовсе, остаётся
			// платёжный факт (колонки «прошлый / текущий месяц» не должны пустеть). Такие ячейки помечены Basis.
			have := map[[2]int]bool{}
			for _, c := range acc {
				have[[2]int{c.Year, c.Month}] = true
			}
			out = append(out, acc...)
			for _, c := range pay {
				if !have[[2]int{c.Year, c.Month}] {
					out = append(out, c)
				}
			}
			return out, nil
		}
		if len(pay) > 0 {
			noteExpFactBasis(ctx, ExpFactBasisPayments)
		}
		out = append(out, pay...)
	}
	return out, nil
}

// expFinPLReports — значения «Для отчета» в Table_Fin_PL, из которых строится факт P&L. Как в копии факта контура
// «Бюджетирование» (budget.usp_RefreshFact, сверена с FactAndBudgetPlusCompany на 144 672 ячейках).
var expFinPLReports = []string{"PL", "BS_20", "BS_25"}

// expAccrualFactSQL — факт по начислениям из реплики P&L (finance.findwh_table_fin_pl, сценарий «факт»), приведённый к
// виду строк затрат. Каталог статей (группа, название, спецдоступ) берётся из снимка затрат: Table_Fin_PL не знает
// «спецдоступа», а статьи вне каталога формы не показываются. Проекта в P&L нет — суммы по всем проектам; ЦФО
// без числового кода (35BK, 250BK, XXX) отбрасываются, как в копии факта контура.
func expAccrualFactSQL(kind, country string, years []int) string {
	return expFinPLSQL(kind, country, years, "факт", ExpSrcFact)
}

// expFinPLSQL — сценарий реплики P&L (finScenario, без учёта регистра) как строки затрат со сценарием outScenario.
func expFinPLSQL(kind, country string, years []int, finScenario, outScenario string) string {
	amountCol := "amount"
	if expKindInBYN(kind) {
		amountCol = "amount_byn"
	}
	reports := make([]string, len(expFinPLReports))
	for i, r := range expFinPLReports {
		reports[i] = chLit(r)
	}
	w := []string{
		"lowerUTF8(scenario) = lowerUTF8(" + chLit(finScenario) + ")",
		"for_report IN (" + strings.Join(reports, ", ") + ")",
		fmt.Sprintf("month >= '%d-01-01'", expCutoffYear),
		"toInt32OrZero(code_cfo) > 0",
	}
	group1, group3 := expKindFilter(kind)
	if group1 != "" {
		// Группа ЦФО в справочнике бывает с номером («3.Магазины») и без («Магазины») — сравниваем без номера.
		w = append(w, "replaceRegexpOne(group_cfo1, "+chLit(`^[0-9]+\.\s*`)+", '') = "+chLit(expStripGroupNumber(group1)))
	}
	if len(group3) > 0 {
		lits := make([]string, len(group3))
		for i, g := range group3 {
			lits[i] = chLit(g)
		}
		w = append(w, "group_cfo3 IN ("+strings.Join(lits, ", ")+")")
	}
	if country != "" {
		w = append(w, "country = "+chLit(country))
	}
	if len(years) > 0 {
		ys := make([]string, len(years))
		for i, y := range years {
			ys[i] = strconv.Itoa(y)
		}
		w = append(w, "toYear(month) IN ("+strings.Join(ys, ",")+")")
	}
	// Группа ЦФО в строках формы — в написании источника затрат («3.Магазины»), чтобы факт не завёл вторую группу.
	g1Expr := "any(f.g1)"
	if group1 != "" {
		g1Expr = chLit(group1)
	}
	var drop []string
	for c := range expDropCodePL {
		drop = append(drop, strconv.Itoa(c))
	}
	sort.Strings(drop)
	var notSales []string
	for _, word := range expSalesGroupWords {
		notSales = append(notSales, "positionCaseInsensitiveUTF8(d.gp, "+chLit(word)+") = 0")
	}
	return `SELECT ` + chLit(outScenario) + ` AS scenario, f.country AS country, toYear(f.month) AS y, toMonth(f.month) AS m,
       f.pl AS code_pl, any(d.gp) AS group_pl, any(d.ex) AS expense,
       f.cfo_n AS code_cfo, any(f.cfn) AS cfo, ` + g1Expr + ` AS group_cfo1, any(f.g2) AS group_cfo2,
       '' AS project_name, any(d.sa) AS special_access, toFloat64(sum(f.amt)) AS amount
  FROM (SELECT country, month, toInt32OrZero(code_pl) AS pl, toInt32OrZero(code_cfo) AS cfo_n, cfo AS cfn,
               group_cfo1 AS g1, group_cfo2 AS g2, toFloat64(` + amountCol + `) AS amt
          FROM finance.findwh_table_fin_pl
         WHERE ` + strings.Join(w, "\n           AND ") + `) AS f
 INNER JOIN (SELECT code_pl AS dpl, any(group_pl) AS gp, any(expense) AS ex, max(special_access) AS sa
               FROM finance.exp_costs GROUP BY code_pl) AS d ON d.dpl = f.pl
 WHERE f.pl NOT IN (` + strings.Join(drop, ",") + `)
   AND ` + strings.Join(notSales, "\n   AND ") + `
 GROUP BY f.country, y, m, f.pl, f.cfo_n
 LIMIT ` + strconv.Itoa(expRowCap+1)
}

// expStripGroupNumber — «3.Магазины» → «Магазины».
func expStripGroupNumber(g string) string {
	return strings.TrimSpace(expGroupNumberRe.ReplaceAllString(g, ""))
}

var expGroupNumberRe = regexp.MustCompile(`^[0-9]+\.\s*`)

// accrualFact — «Факт по начислениям» из реплики P&L; пусто, если выключено, таблицы нет или в ней нет факта за срез
// (тогда работает запасной платёжный факт). Ошибка чтения не ломает форму: пишется в лог.
func (s *chExpSource) accrualFact(ctx context.Context, kind, country string, years []int) []ExpCostCell {
	if !s.accrualFromFinPL || !expKindFromFinPL(kind) {
		return nil
	}
	body, err := s.query(ctx, expAccrualFactSQL(kind, country, years), "JSONEachRow")
	if err != nil {
		log.Printf("plans/exp: факт по начислениям из finance.findwh_table_fin_pl не прочитан, берём платёжный: %v", err)
		return nil
	}
	cells, err := parseChCostRows(body)
	if err != nil {
		log.Printf("plans/exp: факт по начислениям из finance.findwh_table_fin_pl: %v", err)
		return nil
	}
	return cells
}

// planFromReplica заменяет стратегию и тактику среза данными реплики P&L. Сценарий заменяется, только если в реплике
// за срез что-то нашлось: пустая реплика или ошибка чтения оставляют данные процедуры (форма не остаётся без плана).
func (s *chExpSource) planFromReplica(ctx context.Context, kind, country string, years []int, out []ExpCostCell) []ExpCostCell {
	for _, m := range []struct{ fin, out string }{{s.finPLStrategy, ExpSrcStrategy}, {s.finPLTactic, ExpSrcTactic}} {
		body, err := s.query(ctx, expFinPLSQL(kind, country, years, m.fin, m.out), "JSONEachRow")
		if err != nil {
			log.Printf("plans/exp: сценарий %q из finance.findwh_table_fin_pl не прочитан, остаётся процедура: %v", m.fin, err)
			continue
		}
		cells, err := parseChCostRows(body)
		if err != nil || len(cells) == 0 {
			if err != nil {
				log.Printf("plans/exp: сценарий %q из finance.findwh_table_fin_pl: %v", m.fin, err)
			}
			continue
		}
		kept := out[:0:0]
		for _, c := range out {
			if c.Scenario != m.out {
				kept = append(kept, c)
			}
		}
		out = append(kept, cells...)
	}
	return out
}

// paymentsFact — платёжный факт среза как «Факт по начислениям» (запасной путь); ячейки помечены Basis=payments.
func (s *chExpSource) paymentsFact(ctx context.Context, kind, country string, years []int) []ExpCostCell {
	pb, err := s.query(ctx, expCostsSQLScenario(kind, country, years, ExpSrcFactPayments), "JSONEachRow")
	if err != nil {
		return nil
	}
	pc, err := parseChCostRows(pb)
	if err != nil {
		return nil
	}
	for i := range pc {
		pc[i].Scenario = ExpSrcFact
		pc[i].Basis = ExpFactBasisPayments
	}
	return pc
}

func expHasScenario(cells []ExpCostCell, scenario string) bool {
	for _, c := range cells {
		if c.Scenario == scenario {
			return true
		}
	}
	return false
}

// parseChCostRows разбирает JSONEachRow выборки затрат.
func parseChCostRows(body []byte) ([]ExpCostCell, error) {
	out := make([]ExpCostCell, 0, 4096)
	sc := bufio.NewScanner(strings.NewReader(string(body)))
	sc.Buffer(make([]byte, 0, 1<<20), 1<<20)
	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		if line == "" {
			continue
		}
		var r chCostRow
		if err := json.Unmarshal([]byte(line), &r); err != nil {
			return nil, fmt.Errorf("finance.exp_costs: разбор строки: %w", err)
		}
		if len(out) >= expRowCap {
			log.Printf("plans/exp: выборка затрат обрезана на %d строк", expRowCap)
			break
		}
		out = append(out, ExpCostCell{
			Scenario: r.Scenario, Year: r.Year, Month: r.Month, CodePL: r.CodePL,
			GroupPL: r.GroupPL, Expense: r.Expense, CodeCFO: r.CodeCFO, CFO: r.CFO,
			GroupCFO1: r.GroupCFO1, GroupCFO2: r.GroupCFO2, Country: r.Country,
			Amount: r.Amount, Projects: r.ProjectName, SpecialAccess: r.SpecialAccess,
		})
	}
	return out, sc.Err()
}