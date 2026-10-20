package etl

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"log"
	"strings"
	"time"
)

// Реплика FinDWH → ClickHouse (таблицы finance.findwh_*) для аналитиков: GLMF, Table_Fin_PL,
// ExRates. Дизайн и замеры — docs/bi/findwh-etl.md, схема —
// clickhouse/migrations/012_findwh.up.sql.
//
// Чтение из MSSQL — обычным read committed, БЕЗ NOLOCK: в FinDWH включён
// READ_COMMITTED_SNAPSHOT, так что каждый SELECT видит согласованный снимок на
// момент старта и не блокирует процедуры DWH. NOLOCK же мог бы вернуть строки
// посреди их DELETE+INSERT.
//
// Даты в SQL — только 'YYYYMMDD' (CONVERT(..., 112)): логин работает с
// DATEFORMAT dmy, и ISO-литерал '2026-09-01' для datetime читается как 9 января.

// Таблицы реплики (значения FINDWH_TABLES и finance.findwh_etl_runs.table_name).
const (
	FinDWHGLMF       = "glmf"
	FinDWHTableFinPL = "table_fin_pl"
	FinDWHExRates    = "ex_rates"
)

// FinDWHAllTables — порядок ежедневного прогона: от мелкого к крупному, чтобы
// сбой долгого GLMF не задерживал справочники.
var FinDWHAllTables = []string{FinDWHExRates, FinDWHTableFinPL, FinDWHGLMF}

// FinDWHOpts — параметры реплики.
type FinDWHOpts struct {
	DB             string    // БД источника, дефолт FinDWH
	GLMFFrom       time.Time // нижняя граница GLMF.Date (дефолт 2025-01-01)
	ChunkMonths    int       // сколько месяцев одной компании перегружать за один запрос к MSSQL (дефолт 3)
	BatchSize      int       // строк в одном INSERT в CH (дефолт 20000)
	ReadyCheck     bool      // проверять готовность источника по истории SQL Agent (msdb)
	ReadyAfterHour int       // без ReadyCheck: не раньше этого часа по часам MSSQL (дефолт 9)
	Tables         []string  // какие таблицы гонять; пусто → FinDWHAllTables
	Force          bool      // игнорировать «уже загружено сегодня» и готовность источника
	GLMFFull       bool      // GLMF: игнорировать отпечатки реплики и перегрузить всё
	Arh            ArhOpts   // потоки arh_debt/arh_sales (сырые Debt_arh/Docs/Wholesales_arh); пусто → выключены
	TriggeredBy    string    // cron | cli

	// progress — куда сообщать, что сейчас грузится (воркер показывает это на
	// странице статуса). note == "" — таблица только начата, table == "" —
	// таблица закончена. nil → никуда.
	progress func(table, note string)
}

// report — сообщить о ходе загрузки, если кто-то слушает.
func (o FinDWHOpts) report(table, note string) {
	if o.progress != nil {
		o.progress(table, note)
	}
}

func (o *FinDWHOpts) normalize() {
	if o.DB == "" {
		o.DB = "FinDWH"
	}
	if o.GLMFFrom.IsZero() {
		o.GLMFFrom = time.Date(2025, 1, 1, 0, 0, 0, 0, time.UTC)
	}
	if o.ChunkMonths <= 0 {
		o.ChunkMonths = 3
	}
	if o.BatchSize <= 0 {
		o.BatchSize = 20000
	}
	if o.ReadyAfterHour <= 0 {
		o.ReadyAfterHour = 9
	}
	o.Arh.normalize()
	if len(o.Tables) == 0 {
		o.Tables = append([]string{}, FinDWHAllTables...)
		if o.Arh.Enabled() {
			o.Tables = append(o.Tables, FinDWHArhTables...)
		}
	}
	if o.TriggeredBy == "" {
		o.TriggeredBy = "cli"
	}
}

// fqn — трёхчастное имя таблицы источника.
func (o FinDWHOpts) fqn(table string) string {
	return "[" + o.DB + "].[dbo].[" + table + "]"
}

// --- описание колонок -------------------------------------------------------

// colKind — как колонка источника превращается в текст для JSONEachRow.
// Всё читается строкой (sql.NullString): CH сам разбирает числа и даты из строк,
// а NULL уходит JSON-null'ом (Nullable хранит NULL, остальные берут дефолт типа).
type colKind int

const (
	kStr      colKind = iota // nvarchar → как есть
	kDec                     // numeric/decimal → '123.45'
	kFloat                   // float → decimal(38,12), без экспоненты
	kInt                     // int/bigint → '42'
	kDate                    // date/datetime → 'YYYY-MM-DD'
	kDateTime                // datetime → 'YYYY-MM-DD hh:mm:ss'
	kBit                     // bit → '0'/'1'
)

// colSpec — колонка реплики: исходная колонка FinDWH → колонка CH.
type colSpec struct {
	Src  string // имя в источнике (без скобок)
	Name string // имя в CH
	Kind colKind
}

// selectExpr — SQL-выражение колонки для SELECT из MSSQL.
func (c colSpec) selectExpr() string {
	col := "[" + c.Src + "]"
	switch c.Kind {
	case kDec, kInt:
		return "CONVERT(varchar(60), " + col + ")"
	case kFloat:
		return "CONVERT(varchar(60), CAST(" + col + " AS decimal(38, 12)))"
	case kDate:
		return "CONVERT(char(10), " + col + ", 23)"
	case kDateTime:
		return "CONVERT(char(19), " + col + ", 120)"
	case kBit:
		return "CONVERT(varchar(1), CONVERT(tinyint, " + col + "))"
	default:
		return col
	}
}

// selectList — список выражений через запятую.
func selectList(cols []colSpec) string {
	parts := make([]string, len(cols))
	for i, c := range cols {
		parts[i] = c.selectExpr()
	}
	return strings.Join(parts, ", ")
}

// --- запись в CH ------------------------------------------------------------

// jsonRowEncoder пишет строки источника в JSONEachRow. Префиксы `"name":`
// считаются один раз — на 100+ млн строк GLMF это заметно.
type jsonRowEncoder struct {
	prefixes [][]byte
}

func newJSONRowEncoder(cols []colSpec) *jsonRowEncoder {
	e := &jsonRowEncoder{prefixes: make([][]byte, len(cols))}
	for i, c := range cols {
		name, _ := json.Marshal(c.Name)
		e.prefixes[i] = append(name, ':')
	}
	return e
}

func (e *jsonRowEncoder) encode(buf *bytes.Buffer, vals []sql.NullString) error {
	buf.WriteByte('{')
	for i, v := range vals {
		if i > 0 {
			buf.WriteByte(',')
		}
		buf.Write(e.prefixes[i])
		if !v.Valid {
			buf.WriteString("null")
			continue
		}
		s, err := json.Marshal(v.String)
		if err != nil {
			return err
		}
		buf.Write(s)
	}
	buf.WriteString("}\n")
	return nil
}

// streamToCH — курсор MSSQL → батчи JSONEachRow → CH-таблица. Возвращает число
// залитых строк.
func streamToCH(ctx context.Context, ch *chClient, rows *sql.Rows, cols []colSpec, table string, batch int) (int64, error) {
	enc := newJSONRowEncoder(cols)
	vals := make([]sql.NullString, len(cols))
	ptrs := make([]any, len(cols))
	for i := range vals {
		ptrs[i] = &vals[i]
	}
	var (
		buf   bytes.Buffer
		inBuf int
		total int64
	)
	flush := func() error {
		if inBuf == 0 {
			return nil
		}
		if err := ch.insertJSON(ctx, table, &buf); err != nil {
			return err
		}
		total += int64(inBuf)
		inBuf = 0
		buf.Reset()
		return nil
	}
	for rows.Next() {
		if err := rows.Scan(ptrs...); err != nil {
			return total, fmt.Errorf("scan: %w", err)
		}
		if err := enc.encode(&buf, vals); err != nil {
			return total, fmt.Errorf("encode: %w", err)
		}
		inBuf++
		if inBuf >= batch {
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

// chQuote — строковый литерал CH: экранирует обратный слэш и апостроф
// (sqlEscape удваивает только апостроф, а CH трактует '\' как escape).
func chQuote(s string) string {
	s = strings.ReplaceAll(s, `\`, `\\`)
	return "'" + strings.ReplaceAll(s, "'", `\'`) + "'"
}

// --- журнал и готовность ------------------------------------------------------

// finDWHRun — строка finance.findwh_etl_runs.
type finDWHRun struct {
	TableName   string `json:"table_name"`
	SourceDate  string `json:"source_date"`
	StartedAt   int64  `json:"started_at"` // unix-секунды: не зависят от часового пояса сервера CH
	FinishedAt  int64  `json:"finished_at"`
	Status      string `json:"status"`
	Rows        int64  `json:"rows"`
	Details     string `json:"details"`
	TriggeredBy string `json:"triggered_by"`
}

func writeFinDWHRun(ctx context.Context, ch *chClient, r finDWHRun) {
	var buf bytes.Buffer
	if err := json.NewEncoder(&buf).Encode(r); err != nil {
		log.Printf("findwh: etl_runs encode: %v", err)
		return
	}
	if err := ch.insertJSON(ctx, "finance.findwh_etl_runs", &buf); err != nil {
		log.Printf("findwh: etl_runs insert: %v", err)
	}
}

// sourceClock — дата и час на сервере MSSQL. Расписание DWH живёт по его часам,
// поэтому «сегодня» берём оттуда, а не из часового пояса контейнера.
func sourceClock(ctx context.Context, db *sql.DB) (date string, hour int, err error) {
	err = db.QueryRowContext(ctx,
		"SELECT CONVERT(char(10), GETDATE(), 23), DATEPART(hour, GETDATE())").Scan(&date, &hour)
	return date, hour, err
}

// finDWHReadyJob — шаг SQL Agent, после успешного окончания которого таблица
// источника на сегодня собрана (расписание — docs/bi/findwh-etl.md §2).
type finDWHReadyJob struct {
	Job  string
	Step int
}

var finDWHReadyJobs = map[string]finDWHReadyJob{
	FinDWHExRates:    {Job: "FinDWH fullbackup", Step: 1},          // spExRates*
	FinDWHGLMF:       {Job: "FinDWH fullbackup", Step: 2},          // GLMF_exec
	FinDWHTableFinPL: {Job: "FinDWH Update_Table_Fin_PL", Step: 1}, // Update_Table_Fin_PL
}

const finDWHReadySQL = `
SELECT COUNT(*)
FROM msdb.dbo.sysjobhistory h
JOIN msdb.dbo.sysjobs j ON j.job_id = h.job_id
WHERE j.name = @job AND h.step_id = @step AND h.run_status = 1
  AND h.run_date = CONVERT(int, CONVERT(char(8), GETDATE(), 112))`

// sourceReady — собрана ли таблица источника сегодня. С ReadyCheck смотрим
// историю SQL Agent (нужен SELECT на msdb.dbo.sysjobs/sysjobhistory), без него —
// только час на сервере.
func sourceReady(ctx context.Context, db *sql.DB, opts FinDWHOpts, table string, hour int) (bool, error) {
	// Debt_arh/Wholesales_arh заполняет 1С, а не джоб DWH: смотреть в SQL Agent нечего,
	// ориентируемся на час по часам MSSQL (ARH_READY_AFTER_HOUR).
	if isArhTable(table) {
		return hour >= opts.Arh.ReadyAfterHour, nil
	}
	if !opts.ReadyCheck {
		return hour >= opts.ReadyAfterHour, nil
	}
	j, ok := finDWHReadyJobs[table]
	if !ok {
		return false, fmt.Errorf("неизвестная таблица %q", table)
	}
	var n int
	if err := db.QueryRowContext(ctx, finDWHReadySQL,
		sql.Named("job", j.Job), sql.Named("step", j.Step)).Scan(&n); err != nil {
		return false, fmt.Errorf("msdb: %w", err)
	}
	return n > 0, nil
}

// loadedToday — есть ли успешный прогон таблицы за дату источника.
func loadedToday(ctx context.Context, ch *chClient, table, sourceDate string) (bool, error) {
	s, err := ch.queryString(ctx, fmt.Sprintf(
		"SELECT count() FROM finance.findwh_etl_runs WHERE table_name = %s AND status = 'ok' AND source_date = toDate(%s)",
		chQuote(table), chQuote(sourceDate)))
	if err != nil {
		return false, err
	}
	return s != "" && s != "0", nil
}

// RunFinDWHDaily — ежедневная загрузка реплики: для каждой таблицы проверяет,
// не загружена ли она уже сегодня и готов ли источник, и загружает. Ошибка одной
// таблицы не останавливает остальные; возвращается первая. Безопасно звать часто
// (воркер раз в FINDWH_SYNC_INTERVAL): лишние вызовы — два лёгких запроса.
func RunFinDWHDaily(ctx context.Context, deps Deps, opts FinDWHOpts) (int64, error) {
	opts.normalize()
	ch, err := newCHClient(deps.CHURL, deps.CHUser, deps.CHPass)
	if err != nil {
		return 0, fmt.Errorf("findwh: ch client: %w", err)
	}
	sourceDate, hour, err := sourceClock(ctx, deps.MSSQL)
	if err != nil {
		return 0, fmt.Errorf("findwh: часы MSSQL: %w", err)
	}

	var (
		total    int64
		firstErr error
	)
	for _, table := range opts.Tables {
		if !opts.Force {
			done, err := loadedToday(ctx, ch, table, sourceDate)
			if err != nil {
				return total, fmt.Errorf("findwh: etl_runs: %w", err)
			}
			if done {
				continue
			}
			ready, err := sourceReady(ctx, deps.MSSQL, opts, table, hour)
			if err != nil {
				log.Printf("findwh: %s: проверка готовности: %v", table, err)
				continue
			}
			if !ready {
				continue
			}
		}

		startedAt := time.Now()
		opts.report(table, "")
		var (
			n       int64
			details string
		)
		switch table {
		case FinDWHGLMF:
			var st GLMFSyncStats
			st, err = RunFinDWHGLMF(ctx, deps, opts)
			n, details = st.Rows, st.String()
		case FinDWHTableFinPL:
			n, err = runFinDWHSnapshot(ctx, deps, ch, tableFinPLSnapshot(opts), opts.BatchSize)
		case FinDWHExRates:
			n, err = runFinDWHSnapshot(ctx, deps, ch, exRatesSnapshot(opts), opts.BatchSize)
		case FinDWHArhDebt, FinDWHArhSales:
			var st ArhStats
			st, err = RunFinDWHArh(ctx, deps, ch, opts, table, sourceDate)
			n, details = st.Rows, st.String()
		default:
			err = fmt.Errorf("неизвестная таблица %q (допустимо: %s)", table, strings.Join(append(append([]string{}, FinDWHAllTables...), FinDWHArhTables...), ", "))
		}

		run := finDWHRun{
			TableName:   table,
			SourceDate:  sourceDate,
			StartedAt:   startedAt.Unix(),
			FinishedAt:  time.Now().Unix(),
			Status:      "ok",
			Rows:        n,
			Details:     details,
			TriggeredBy: opts.TriggeredBy,
		}
		if err != nil {
			run.Status, run.Details = "error", cleanErr(err)
			log.Printf("findwh: %s: %v", table, err)
			if firstErr == nil {
				firstErr = fmt.Errorf("findwh %s: %w", table, err)
			}
		} else {
			log.Printf("findwh: %s загружена: rows=%d %s за %.0fs", table, n, details, time.Since(startedAt).Seconds())
		}
		writeFinDWHRun(ctx, ch, run)
		opts.report("", "")
		total += n
	}
	return total, firstErr
}

// ParseFinDWHDate — 'YYYY-MM-DD' из env (FINDWH_GLMF_FROM).
func ParseFinDWHDate(s string) (time.Time, error) {
	t, err := time.Parse("2006-01-02", strings.TrimSpace(s))
	if err != nil {
		return time.Time{}, fmt.Errorf("ожидается YYYY-MM-DD, получено %q", s)
	}
	return t, nil
}
