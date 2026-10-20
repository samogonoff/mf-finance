package etl

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"log"
	"sort"
	"strings"
	"time"
)

// Поток ARH (сырые Debt_arh/Docs/Wholesales_arh → finance.fact_arh_debt / fact_arh_sales)
// встроен в реплику FinDWH: те же воркер, журнал finance.findwh_etl_runs, «уже загружено
// сегодня», прогресс и страница /admin/etl (таблицы arh_debt и arh_sales).
//
// Лёгкость: никаких полных перезагрузок. Окно — последние ARH_WINDOW_DAYS дней (дневные
// снэпшоты) плюс концы месяцев от DEBTARH_RAW_MIN_DATE; продажи — каждый день от
// DEBTARH_RAW_MIN_DATE (события нужны сплошным периодом, а Wholesales_arh — 4 млн строк).
// Каждый запрос к MSSQL — одна дата или короткий чанк по Date, с фильтром по нашим
// UNPOrg (индекс Debt_arh (UNPOrg, Date)) и по ВГО-каналу. 1С перезаписывает прошлые
// даты, поэтому по дешёвому отпечатку (MAX(DateChange) дня против src_changed_at в CH)
// определяются только изменившиеся даты, и перезагружается ЦЕЛИКОМ каждая такая дата.
//
// Чтение — с NOLOCK: в Payments READ_COMMITTED_SNAPSHOT выключен (is_read_committed_
// snapshot_on = 0, проверено на OLAP), так что обычный read committed блокировался бы
// об процедуры перезаливки; осознанный риск «грязного» чтения тот же, что в остальных
// extract'ах по Payments. Даты в параметрах — только 'YYYYMMDD'.

// Таблицы реплики ARH (значения FINDWH_TABLES и finance.findwh_etl_runs.table_name).
const (
	FinDWHArhDebt  = "arh_debt"
	FinDWHArhSales = "arh_sales"
)

// FinDWHArhTables — порядок прогона ARH-таблиц.
var FinDWHArhTables = []string{FinDWHArhDebt, FinDWHArhSales}

func isArhTable(table string) bool { return table == FinDWHArhDebt || table == FinDWHArhSales }

// ArhOpts — параметры потока ARH внутри FinDWHOpts.
type ArhOpts struct {
	Tables         ArhTables
	MinDate        string // 'YYYY-MM-DD': нижняя граница (концы месяцев и продажи), дефолт 2026-06-01
	WindowDays     int    // глубина окна дневных снэпшотов, дней (дефолт 45)
	ReadyAfterHour int    // не грузить раньше этого часа по часам MSSQL (дефолт 13)
	Full           bool   // перезагрузить все даты окна, игнорируя отпечатки (CLI ARH_FULL=1)
}

const (
	arhDefaultMinDate = "2026-06-01"
	arhDefaultWindow  = 45
	arhDefaultReady   = 13
	arhChunkDays      = 7  // длина чанка отпечатков дебиторки
	arhSalesChunkDays = 31 // продажи: таблица небольшая, чанк — месяц
)

// Enabled — поток настроен: заданы FQN и список наших ЮЛ.
func (a ArhOpts) Enabled() bool {
	t := a.Tables
	return t.DebtFQN != "" && t.DocsFQN != "" && t.WholesalesFQN != "" && len(t.Orgs) > 0
}

func (a *ArhOpts) normalize() {
	if a.MinDate == "" {
		a.MinDate = arhDefaultMinDate
	}
	if a.WindowDays <= 0 {
		a.WindowDays = arhDefaultWindow
	}
	if a.ReadyAfterHour <= 0 {
		a.ReadyAfterHour = arhDefaultReady
	}
}

// ArhStats — итог прогона одной ARH-таблицы.
type ArhStats struct {
	Rows     int64
	Dates    int // дат в окне
	Reloaded int // перезагружено
	Dropped  int // исчезло в источнике
}

func (s ArhStats) String() string {
	return fmt.Sprintf("дат в окне %d, перезагружено %d, удалено %d", s.Dates, s.Reloaded, s.Dropped)
}

// arhStream — описание потока: таблица CH, колонка даты, SQL и построчный разбор.
type arhStream struct {
	name      string
	table     string // finance.fact_arh_*
	dateCol   string // snapshot_date | event_date
	datesSQL  string // отпечатки дней чанка (@from, @to, @grp)
	daySQL    string // выгрузка одного дня (@dt, @grp)
	chunkDays int
	scan      func(rows *sql.Rows) (any, error)
}

func arhStreamFor(table string, t ArhTables) (arhStream, error) {
	switch table {
	case FinDWHArhDebt:
		return arhStream{
			name: "debt", table: "finance.fact_arh_debt", dateCol: "snapshot_date",
			datesSQL: extractArhDebtDatesSQL(t), daySQL: extractArhDebtSQL(t), chunkDays: arhChunkDays,
			scan: func(rows *sql.Rows) (any, error) { r, err := scanArhDebtRow(rows); return r, err },
		}, nil
	case FinDWHArhSales:
		return arhStream{
			name: "sales", table: "finance.fact_arh_sales", dateCol: "event_date",
			datesSQL: extractArhSalesDatesSQL(t), daySQL: extractArhSalesSQL(t), chunkDays: arhSalesChunkDays,
			scan: func(rows *sql.Rows) (any, error) { r, err := scanArhSalesRow(rows); return r, err },
		}, nil
	}
	return arhStream{}, fmt.Errorf("неизвестная ARH-таблица %q", table)
}

func parseISODate(s string) (time.Time, error) { return time.Parse("2006-01-02", s) }

func ymd(t time.Time) string { return t.Format("20060102") }

// arhCandidateDates — какие даты вообще ведём. today — дата по часам MSSQL.
//   - sales: каждый день [MinDate..today];
//   - debt: каждый день окна [today-WindowDays..today] (но не раньше MinDate) плюс последний
//     день каждого месяца от MinDate до начала окна (исторические снэпшоты — помесячные).
//
// Результат — 'YYYY-MM-DD' по возрастанию.
func arhCandidateDates(table, today string, a ArhOpts) ([]string, error) {
	end, err := parseISODate(today)
	if err != nil {
		return nil, fmt.Errorf("today %q: %w", today, err)
	}
	min, err := parseISODate(a.MinDate)
	if err != nil {
		return nil, fmt.Errorf("DEBTARH_RAW_MIN_DATE %q: %w", a.MinDate, err)
	}
	var out []string
	if table == FinDWHArhSales {
		for d := min; !d.After(end); d = d.AddDate(0, 0, 1) {
			out = append(out, d.Format("2006-01-02"))
		}
		return out, nil
	}
	winStart := end.AddDate(0, 0, -a.WindowDays)
	if winStart.Before(min) {
		winStart = min
	}
	// концы месяцев строго до окна
	for m := time.Date(min.Year(), min.Month(), 1, 0, 0, 0, 0, time.UTC); ; m = m.AddDate(0, 1, 0) {
		last := m.AddDate(0, 1, -1)
		if !last.Before(winStart) {
			break
		}
		if !last.Before(min) {
			out = append(out, last.Format("2006-01-02"))
		}
	}
	for d := winStart; !d.After(end); d = d.AddDate(0, 0, 1) {
		out = append(out, d.Format("2006-01-02"))
	}
	return out, nil
}

// arhChunks — разбивка отсортированных дат на чанки подряд идущих дней длиной ≤ maxDays:
// узкий диапазон Date на каждый запрос. Возвращает пары [from, to] в 'YYYY-MM-DD'.
func arhChunks(dates []string, maxDays int) [][2]string {
	var out [][2]string
	var start, prev time.Time
	var have bool
	flush := func() {
		if have {
			out = append(out, [2]string{start.Format("2006-01-02"), prev.Format("2006-01-02")})
		}
	}
	for _, s := range dates {
		d, err := parseISODate(s)
		if err != nil {
			continue
		}
		if have && d.Sub(prev) == 24*time.Hour && int(d.Sub(start).Hours()/24) < maxDays {
			prev = d
			continue
		}
		flush()
		start, prev, have = d, d, true
	}
	flush()
	return out
}

// arhPlan — какие даты перезагрузить и какие снести.
type arhPlan struct {
	reload []string
	drop   []string
}

// planArhDates — сверка отпечатков источника (src) и CH (have) по ведомым датам cand:
// перезагружаем дату, которой нет в CH или у которой MAX(DateChange) отличается (full —
// все даты, что есть в источнике); дату, оставшуюся в CH, но исчезнувшую в источнике, сносим.
func planArhDates(cand []string, src, have map[string]string, full bool) arhPlan {
	var p arhPlan
	for _, d := range cand {
		chg, inSrc := src[d]
		if inSrc && (full || have[d] != chg) {
			p.reload = append(p.reload, d)
		}
		if !inSrc {
			if _, inCH := have[d]; inCH {
				p.drop = append(p.drop, d)
			}
		}
	}
	sort.Strings(p.reload)
	sort.Strings(p.drop)
	return p
}

// arhDelete — снести даты по условию и ДОЖДАТЬСЯ мутации (mutations_sync=1): иначе
// отложенный ALTER DELETE мог бы стереть только что вставленные строки.
func arhDelete(ctx context.Context, ch *chClient, s arhStream, cond string) error {
	return ch.exec(ctx, fmt.Sprintf("ALTER TABLE %s DELETE WHERE %s SETTINGS mutations_sync = 1", s.table, cond))
}

func chDateList(dates []string) string {
	parts := make([]string, len(dates))
	for i, d := range dates {
		parts[i] = "toDate('" + sqlEscape(d) + "')"
	}
	return strings.Join(parts, ", ")
}

// chArhFingerprints — отпечатки ведомых дат в CH: 'YYYY-MM-DD' → max(src_changed_at).
func chArhFingerprints(ctx context.Context, ch *chClient, s arhStream, cand []string) (map[string]string, error) {
	have := map[string]string{}
	if len(cand) == 0 {
		return have, nil
	}
	q := fmt.Sprintf("SELECT toString(%[2]s), toString(max(src_changed_at)) FROM %[1]s WHERE %[2]s IN (%[3]s) GROUP BY %[2]s FORMAT TSV",
		s.table, s.dateCol, chDateList(cand))
	out, err := ch.queryString(ctx, q)
	if err != nil {
		return nil, err
	}
	for _, line := range strings.Split(out, "\n") {
		if parts := strings.Split(strings.TrimSpace(line), "\t"); len(parts) == 2 {
			have[parts[0]] = parts[1]
		}
	}
	return have, nil
}

// srcArhFingerprints — отпечатки дней источника, чанками по узкому диапазону Date.
func srcArhFingerprints(ctx context.Context, deps Deps, s arhStream, cand []string) (map[string]string, error) {
	src := map[string]string{}
	for _, c := range arhChunks(cand, s.chunkDays) {
		from, _ := parseISODate(c[0])
		to, _ := parseISODate(c[1])
		rows, err := deps.MSSQL.QueryContext(ctx, s.datesSQL,
			sql.Named("grp", finDebtVGOFolder), sql.Named("from", ymd(from)), sql.Named("to", ymd(to)))
		if err != nil {
			return nil, fmt.Errorf("mssql отпечатки %s..%s: %w", c[0], c[1], err)
		}
		for rows.Next() {
			var d, chg string
			if err := rows.Scan(&d, &chg); err != nil {
				rows.Close()
				return nil, fmt.Errorf("scan отпечатков: %w", err)
			}
			src[d] = chg
		}
		err = rows.Err()
		rows.Close()
		if err != nil {
			return nil, err
		}
	}
	return src, nil
}

// RunFinDWHArh — загрузка одной ARH-таблицы (arh_debt | arh_sales): окно дат, отпечатки,
// перезагрузка только изменившихся дат. today — дата источника (часы MSSQL).
func RunFinDWHArh(ctx context.Context, deps Deps, ch *chClient, opts FinDWHOpts, table, today string) (ArhStats, error) {
	a := opts.Arh
	a.normalize()
	if !a.Enabled() {
		return ArhStats{}, fmt.Errorf("ARH не настроен: нужны MSSQL_DEBT_ARH_FQN, MSSQL_DOCS_FQN, MSSQL_WHOLESALES_ARH_FQN и список ЮЛ")
	}
	s, err := arhStreamFor(table, a.Tables)
	if err != nil {
		return ArhStats{}, err
	}
	cand, err := arhCandidateDates(table, today, a)
	if err != nil {
		return ArhStats{}, err
	}
	st := ArhStats{Dates: len(cand)}

	opts.report(table, fmt.Sprintf("сверка отпечатков: %d дат", len(cand)))
	src, err := srcArhFingerprints(ctx, deps, s, cand)
	if err != nil {
		return st, err
	}
	have, err := chArhFingerprints(ctx, ch, s, cand)
	if err != nil {
		return st, fmt.Errorf("ch отпечатки: %w", err)
	}
	plan := planArhDates(cand, src, have, a.Full)
	st.Dropped = len(plan.drop)
	for _, d := range plan.drop {
		if err := arhDelete(ctx, ch, s, fmt.Sprintf("%s = toDate('%s')", s.dateCol, sqlEscape(d))); err != nil {
			return st, fmt.Errorf("drop %s %s: %w", s.table, d, err)
		}
		log.Printf("etl: arh %s: дата %s исчезла в источнике — удалена из CH", s.name, d)
	}
	for i, d := range plan.reload {
		opts.report(table, fmt.Sprintf("дата %s (%d из %d)", d, i+1, len(plan.reload)))
		if err := arhDelete(ctx, ch, s, fmt.Sprintf("%s = toDate('%s')", s.dateCol, sqlEscape(d))); err != nil {
			return st, fmt.Errorf("cleanup %s %s: %w", s.table, d, err)
		}
		n, err := streamArhDate(ctx, deps, ch, opts, s, d)
		st.Rows += n
		st.Reloaded++
		if err != nil {
			return st, fmt.Errorf("дата %s: %w", d, err)
		}
		log.Printf("etl: arh %s %s rows=%d", s.name, d, n)
	}
	return st, nil
}

// streamArhDate — курсор одного дня → батчи → CH (без накопления всего в памяти).
func streamArhDate(ctx context.Context, deps Deps, ch *chClient, opts FinDWHOpts, s arhStream, date string) (int64, error) {
	d, err := parseISODate(date)
	if err != nil {
		return 0, err
	}
	rows, err := deps.MSSQL.QueryContext(ctx, s.daySQL, sql.Named("grp", finDebtVGOFolder), sql.Named("dt", ymd(d)))
	if err != nil {
		return 0, fmt.Errorf("mssql query: %w", err)
	}
	defer rows.Close()

	batch := opts.BatchSize
	if batch <= 0 || batch > 5000 {
		batch = 5000
	}
	buf := make([]any, 0, batch)
	var total int64
	flush := func() error {
		if len(buf) == 0 {
			return nil
		}
		if err := ch.insertRows(ctx, s.table, buf); err != nil {
			return err
		}
		total += int64(len(buf))
		buf = buf[:0]
		return nil
	}
	for rows.Next() {
		r, err := s.scan(rows)
		if err != nil {
			return total, err
		}
		buf = append(buf, r)
		if len(buf) >= batch {
			if err := flush(); err != nil {
				return total, err
			}
		}
	}
	if err := rows.Err(); err != nil {
		return total, fmt.Errorf("rows: %w", err)
	}
	return total, flush()
}

// insertRows — батч структур → JSONEachRow → таблица CH.
func (c *chClient) insertRows(ctx context.Context, table string, rows []any) error {
	var buf bytes.Buffer
	enc := json.NewEncoder(&buf)
	for i := range rows {
		if err := enc.Encode(rows[i]); err != nil {
			return fmt.Errorf("encode: %w", err)
		}
	}
	return c.insertJSON(ctx, table, &buf)
}
