package etl

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
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
// даты, поэтому по дешёвому отпечатку дня (MAX(DateChange) и число строк источника против
// finance.fact_arh_day_fp) определяются только изменившиеся даты, и каждая такая дата
// перезагружается ЦЕЛИКОМ: сначала в stage-таблицу, и только после полной успешной загрузки
// заменяется в основной (короткие ALTER DELETE по дате + INSERT SELECT). Отпечаток пишется
// после успеха, поэтому оборванная загрузка основную таблицу не портит, а дата
// перепланируется на следующем прогоне.
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
	stage     string // finance.fact_arh_*_stage — загрузка одной даты до замены
	scan      func(rows *sql.Rows) (any, error)
}

func arhStreamFor(table string, t ArhTables) (arhStream, error) {
	switch table {
	case FinDWHArhDebt:
		return arhStream{
			name: "debt", table: "finance.fact_arh_debt", stage: "finance.fact_arh_debt_stage", dateCol: "snapshot_date",
			datesSQL: extractArhDebtDatesSQL(t), daySQL: extractArhDebtSQL(t), chunkDays: arhChunkDays,
			scan: func(rows *sql.Rows) (any, error) { r, err := scanArhDebtRow(rows); return r, err },
		}, nil
	case FinDWHArhSales:
		return arhStream{
			name: "sales", table: "finance.fact_arh_sales", stage: "finance.fact_arh_sales_stage", dateCol: "event_date",
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

// arhFP — отпечаток дня источника: MAX(DateChange) и число строк за ВЕСЬ день.
type arhFP struct {
	Changed string // 'YYYY-MM-DD hh:mm:ss'
	Rows    int64
}

func (f arhFP) key() string { return fmt.Sprintf("%s|%d", f.Changed, f.Rows) }

// arhPlan — какие даты перезагрузить и какие снести.
type arhPlan struct {
	reload []string
	drop   []string
}

// planArhDates — сверка отпечатков источника (src) и CH (have: ключ отпечатка уже
// успешно замещённых дат) по ведомым датам cand. Перезагружаем дату, которой нет в have
// или у которой отпечаток отличается (full — все даты источника). Дату, которой нет в
// источнике, но есть в CH (валидный отпечаток или строки данных — inData), сносим.
func planArhDates(cand []string, src map[string]arhFP, have map[string]string, inData map[string]bool, full bool) arhPlan {
	var p arhPlan
	for _, d := range cand {
		fp, inSrc := src[d]
		if inSrc && (full || have[d] != fp.key()) {
			p.reload = append(p.reload, d)
		}
		if !inSrc {
			if _, ok := have[d]; ok || inData[d] {
				p.drop = append(p.drop, d)
			}
		}
	}
	sort.Strings(p.reload)
	sort.Strings(p.drop)
	return p
}

// execLong — DDL/мутация с таймаутом клиента 10 минут (обычный клиент ETL — 60 с, а
// синхронная мутация на крупной таблице может идти дольше).
func (c *chClient) execLong(ctx context.Context, query string) error {
	long := &chClient{url: c.url, client: &http.Client{Timeout: 10 * time.Minute}}
	return long.exec(ctx, query)
}

// arhDeleteDate — снести дату из таблицы и ДОЖДАТЬСЯ мутации (mutations_sync=1): новые
// строки вставляются только после неё, так что в отчёте нет окна с дублями старой и новой
// версии. Мутация переписывает только куски, существовавшие на момент запуска, поэтому
// вставленное ПОСЛЕ неё она не затрагивает.
func arhDeleteDate(ctx context.Context, ch *chClient, table, dateCol, day string) error {
	return ch.execLong(ctx, fmt.Sprintf("ALTER TABLE %s DELETE WHERE %s = toDate('%s') SETTINGS mutations_sync = 1",
		table, dateCol, sqlEscape(day)))
}

var arhVerLast uint64

// arhVer — монотонная версия записи отпечатка (unix-наносекунды).
func arhVer() uint64 {
	v := uint64(time.Now().UnixNano())
	if v <= arhVerLast {
		v = arhVerLast + 1
	}
	arhVerLast = v
	return v
}

// arhFPRow — строка finance.fact_arh_day_fp.
type arhFPRow struct {
	Stream       string `json:"stream"`
	Day          string `json:"day"`
	SrcChangedAt string `json:"src_changed_at"`
	SrcRows      int64  `json:"src_rows"`
	LoadedRows   int64  `json:"loaded_rows"`
	Ver          uint64 `json:"ver"`
}

// writeArhFP — записать отпечаток дня. srcRows = 0 — «дата не загружена целиком» (маркер
// перед заменой и после удаления): такая запись никогда не совпадёт с источником.
func writeArhFP(ctx context.Context, ch *chClient, s arhStream, day, changed string, srcRows, loaded int64) error {
	if changed == "" {
		changed = "2000-01-01 00:00:00"
	}
	return ch.insertRows(ctx, "finance.fact_arh_day_fp", []any{arhFPRow{
		Stream: s.name, Day: day, SrcChangedAt: changed, SrcRows: srcRows, LoadedRows: loaded, Ver: arhVer(),
	}})
}

func chDateList(dates []string) string {
	parts := make([]string, len(dates))
	for i, d := range dates {
		parts[i] = "toDate('" + sqlEscape(d) + "')"
	}
	return strings.Join(parts, ", ")
}

// chArhFingerprints — успешно замещённые даты в CH: 'YYYY-MM-DD' → ключ отпечатка.
// Берётся последняя версия записи; src_rows = 0 (маркер) в результат не попадает.
func chArhFingerprints(ctx context.Context, ch *chClient, s arhStream, cand []string) (map[string]string, error) {
	have := map[string]string{}
	if len(cand) == 0 {
		return have, nil
	}
	q := fmt.Sprintf("SELECT toString(day), toString(argMax(src_changed_at, ver)), argMax(src_rows, ver) FROM finance.fact_arh_day_fp "+
		"WHERE stream = '%s' AND day IN (%s) GROUP BY day FORMAT TSV", s.name, chDateList(cand))
	out, err := ch.queryString(ctx, q)
	if err != nil {
		return nil, err
	}
	for _, line := range strings.Split(out, "\n") {
		parts := strings.Split(strings.TrimSpace(line), "\t")
		if len(parts) != 3 || parts[2] == "0" {
			continue
		}
		var n int64
		_, _ = fmt.Sscan(parts[2], &n)
		have[parts[0]] = arhFP{Changed: parts[1], Rows: n}.key()
	}
	return have, nil
}

// chArhDataDates — даты ведомого набора, по которым в основной таблице есть строки.
func chArhDataDates(ctx context.Context, ch *chClient, s arhStream, cand []string) (map[string]bool, error) {
	out := map[string]bool{}
	if len(cand) == 0 {
		return out, nil
	}
	body, err := ch.queryString(ctx, fmt.Sprintf("SELECT toString(%[2]s) FROM %[1]s WHERE %[2]s IN (%[3]s) GROUP BY %[2]s FORMAT TSV",
		s.table, s.dateCol, chDateList(cand)))
	if err != nil {
		return nil, err
	}
	for _, line := range strings.Split(body, "\n") {
		if d := strings.TrimSpace(line); d != "" {
			out[d] = true
		}
	}
	return out, nil
}

// srcArhFingerprints — отпечатки дней источника, чанками по узкому диапазону Date.
func srcArhFingerprints(ctx context.Context, deps Deps, s arhStream, cand []string) (map[string]arhFP, error) {
	src := map[string]arhFP{}
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
			var n int64
			if err := rows.Scan(&d, &chg, &n); err != nil {
				rows.Close()
				return nil, fmt.Errorf("scan отпечатков: %w", err)
			}
			src[d] = arhFP{Changed: chg, Rows: n}
		}
		err = rows.Err()
		rows.Close()
		if err != nil {
			return nil, err
		}
	}
	return src, nil
}

// arhSource — чтение источника (MSSQL) для RunFinDWHArh; в тестах подменяется.
type arhSource interface {
	Fingerprints(ctx context.Context, s arhStream, cand []string) (map[string]arhFP, error)
	Day(ctx context.Context, s arhStream, day string, emit func(row any) error) error
}

type mssqlArhSource struct{ deps Deps }

func (m mssqlArhSource) Fingerprints(ctx context.Context, s arhStream, cand []string) (map[string]arhFP, error) {
	return srcArhFingerprints(ctx, m.deps, s, cand)
}

func (m mssqlArhSource) Day(ctx context.Context, s arhStream, day string, emit func(row any) error) error {
	d, err := parseISODate(day)
	if err != nil {
		return err
	}
	rows, err := m.deps.MSSQL.QueryContext(ctx, s.daySQL, sql.Named("grp", finDebtVGOFolder), sql.Named("dt", ymd(d)))
	if err != nil {
		return fmt.Errorf("mssql query: %w", err)
	}
	defer rows.Close()
	for rows.Next() {
		r, err := s.scan(rows)
		if err != nil {
			return err
		}
		if err := emit(r); err != nil {
			return err
		}
	}
	if err := rows.Err(); err != nil {
		return fmt.Errorf("rows: %w", err)
	}
	return nil
}

// RunFinDWHArh — загрузка одной ARH-таблицы (arh_debt | arh_sales): окно дат, отпечатки,
// перезагрузка только изменившихся дат. today — дата источника (часы MSSQL). Ошибка на
// одной дате прерывает прогон, но уже замещённые даты остаются (каждая замена полная);
// неудавшаяся дата сохраняет прежние данные и прежний отпечаток и перепланируется.
func RunFinDWHArh(ctx context.Context, deps Deps, ch *chClient, opts FinDWHOpts, table, today string) (ArhStats, error) {
	return runArh(ctx, mssqlArhSource{deps}, ch, opts, table, today)
}

func runArh(ctx context.Context, src arhSource, ch *chClient, opts FinDWHOpts, table, today string) (ArhStats, error) {
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
	srcFP, err := src.Fingerprints(ctx, s, cand)
	if err != nil {
		return st, err
	}
	have, err := chArhFingerprints(ctx, ch, s, cand)
	if err != nil {
		return st, fmt.Errorf("ch отпечатки: %w", err)
	}
	inData, err := chArhDataDates(ctx, ch, s, cand)
	if err != nil {
		return st, fmt.Errorf("ch даты данных: %w", err)
	}
	plan := planArhDates(cand, srcFP, have, inData, a.Full)
	for _, d := range plan.drop {
		if err := dropArhDate(ctx, ch, s, d); err != nil {
			return st, err
		}
		st.Dropped++
		log.Printf("etl: arh %s: дата %s исчезла в источнике — удалена из CH", s.name, d)
	}
	for i, d := range plan.reload {
		opts.report(table, fmt.Sprintf("дата %s (%d из %d)", d, i+1, len(plan.reload)))
		n, err := reloadArhDate(ctx, src, ch, opts, s, d, srcFP[d])
		st.Rows += n
		if err != nil {
			return st, fmt.Errorf("дата %s: %w", d, err)
		}
		st.Reloaded++
		log.Printf("etl: arh %s %s rows=%d", s.name, d, n)
	}
	return st, nil
}

// dropArhDate — дата исчезла из источника: маркер «не загружена», затем удаление.
func dropArhDate(ctx context.Context, ch *chClient, s arhStream, day string) error {
	if err := writeArhFP(ctx, ch, s, day, "", 0, 0); err != nil {
		return fmt.Errorf("отпечаток %s %s: %w", s.table, day, err)
	}
	if err := arhDeleteDate(ctx, ch, s.table, s.dateCol, day); err != nil {
		return fmt.Errorf("drop %s %s: %w", s.table, day, err)
	}
	return nil
}

// reloadArhDate — полная замена одной даты:
//  1. дата грузится в stage (основная таблица не тронута); ошибка → stage чистится, выход;
//  2. число строк в stage сверяется с прочитанным из MSSQL;
//  3. маркер «не загружена» в fact_arh_day_fp (оборванная дальше замена перепланируется);
//  4. ALTER DELETE по дате в основной (синхронно) + INSERT SELECT из stage — секунды;
//  5. отпечаток дня (MAX(DateChange), число строк источника) пишется ПОСЛЕ успеха.
func reloadArhDate(ctx context.Context, src arhSource, ch *chClient, opts FinDWHOpts, s arhStream, day string, fp arhFP) (int64, error) {
	if err := ch.exec(ctx, "TRUNCATE TABLE "+s.stage); err != nil {
		return 0, fmt.Errorf("очистка %s: %w", s.stage, err)
	}
	defer func() { _ = ch.exec(context.WithoutCancel(ctx), "TRUNCATE TABLE "+s.stage) }()

	n, err := streamArhDate(ctx, src, ch, opts, s, day)
	if err != nil {
		return n, err
	}
	got, err := ch.queryString(ctx, "SELECT count() FROM "+s.stage)
	if err != nil {
		return n, fmt.Errorf("count %s: %w", s.stage, err)
	}
	if got != fmt.Sprint(n) {
		return n, fmt.Errorf("в %s %s строк, прочитано из MSSQL %d — замену не делаем", s.stage, got, n)
	}
	if err := writeArhFP(ctx, ch, s, day, "", 0, 0); err != nil {
		return n, fmt.Errorf("маркер отпечатка: %w", err)
	}
	if err := arhDeleteDate(ctx, ch, s.table, s.dateCol, day); err != nil {
		return n, fmt.Errorf("cleanup %s: %w", s.table, err)
	}
	if n > 0 {
		if err := ch.execLong(ctx, fmt.Sprintf("INSERT INTO %s SELECT * FROM %s", s.table, s.stage)); err != nil {
			return n, fmt.Errorf("замена даты (insert select): %w", err)
		}
	}
	if err := writeArhFP(ctx, ch, s, day, fp.Changed, fp.Rows, n); err != nil {
		return n, fmt.Errorf("отпечаток дня: %w", err)
	}
	return n, nil
}

// streamArhDate — курсор одного дня → батчи → stage-таблица (без накопления всего в памяти).
func streamArhDate(ctx context.Context, src arhSource, ch *chClient, opts FinDWHOpts, s arhStream, day string) (int64, error) {
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
		if err := ch.insertRows(ctx, s.stage, buf); err != nil {
			return err
		}
		total += int64(len(buf))
		buf = buf[:0]
		return nil
	}
	err := src.Day(ctx, s, day, func(r any) error {
		buf = append(buf, r)
		if len(buf) >= batch {
			return flush()
		}
		return nil
	})
	if err != nil {
		return total, err
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
