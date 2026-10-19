package etl

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"log"
	"strconv"
	"strings"
	"sync"
	"time"
)

// Ночной снимок затрат для формы «Расходы ЦП» (этап 1.5): результат процедуры
// Budgeting.dbo.ZATRATY_DLYA_OKSANY_BYSTRYE → finance.exp_costs. Схема —
// clickhouse/migrations/013_exp_costs.up.sql, читатель — plans/exp_ch_source.go.
//
// Процедура без параметров отдаёт весь бюджет (~1 млн строк) и работает 4–5 минут, поэтому
// форма не может звать её при открытии. Раз в сутки в окно [AfterHour, BeforeHour) по часам
// MSSQL (дефолт 5:00–9:00: ночью, пока нет рабочей нагрузки; повторы после ошибки — в том же
// окне) снимок перезаливается целиком: сначала во временную таблицу, затем EXCHANGE.

// ExpCostsOpts — параметры заливки затрат.
type ExpCostsOpts struct {
	Proc        string // полное имя процедуры, дефолт [Budgeting].[dbo].[ZATRATY_DLYA_OKSANY_BYSTRYE]
	AfterHour   int    // с какого часа (по часам MSSQL) можно грузить автоматически, дефолт 5
	BeforeHour  int    // до какого часа; <= AfterHour — без верхней границы, дефолт 9
	MinYear     int    // строки раньше этого года отбрасываются, дефолт 2024
	BatchSize   int    // строк в одном INSERT в CH, дефолт 20000
	RetryAfter  time.Duration
	Force       bool   // игнорировать «уже загружено сегодня» и окно времени
	TriggeredBy string // cron | cli | api
}

const expCostsTarget = "finance.exp_costs"

func (o *ExpCostsOpts) normalize() {
	if strings.TrimSpace(o.Proc) == "" {
		o.Proc = "[Budgeting].[dbo].[ZATRATY_DLYA_OKSANY_BYSTRYE]"
	}
	if o.AfterHour <= 0 {
		o.AfterHour = 5
	}
	if o.BeforeHour == 0 {
		o.BeforeHour = 9
	}
	if o.MinYear <= 0 {
		o.MinYear = 2024
	}
	if o.BatchSize <= 0 {
		o.BatchSize = 20000
	}
	if o.RetryAfter <= 0 {
		o.RetryAfter = time.Hour
	}
	if o.TriggeredBy == "" {
		o.TriggeredBy = "cli"
	}
}

// expCostsCols — колонка выдачи процедуры → колонка CH. Имена источника ищутся без
// учёта регистра: процедура без параметров, порядок и регистр колонок нам не подчиняются.
var expCostsCols = []struct {
	Src      string
	Alt      string // запасное имя колонки в выдаче процедуры (в ТЗ — «Projects», в действующей выдаче — «ProjectName»)
	Name     string
	Kind     colKind
	Required bool
}{
	{"Scenario", "", "scenario", kStr, true},
	{"Country", "", "country", kStr, true},
	{"Month", "", "month", kDate, true},
	{"CodePL", "", "code_pl", kInt, true},
	{"GroupPL", "", "group_pl", kStr, false},
	{"GroupPL_new", "", "group_pl_new", kStr, false},
	{"Expense", "", "expense", kStr, false},
	{"CF_item", "", "cf_item", kStr, false},
	{"SpecialAccess", "", "special_access", kStr, false},
	{"CodeCFO", "", "code_cfo", kInt, true},
	{"CFO", "", "cfo", kStr, false},
	{"GroupCFO1", "", "group_cfo1", kStr, true},
	{"GroupCFO2", "", "group_cfo2", kStr, false},
	{"GroupCFO3", "", "group_cfo3", kStr, false},
	{"ProjectName", "Projects", "project_name", kStr, false},
	{"Amount", "", "amount", kDec, true},
	{"AmountBYN", "", "amount_byn", kDec, false},
	{"AmountUSD", "", "amount_usd", kDec, false},
}

// expCostsValue — значение ячейки выдачи процедуры как текст для JSONEachRow.
// CH сам разбирает числа и даты из строк; NULL уходит JSON-null'ом. Числовые колонки
// процедуры местами содержат нечисловой мусор (в CodeCFO встречается текст, как «n/a» в
// справочнике ЦФО): CH на таком значении отвергает весь батч, а прежнее чтение процедуры
// (plans.expSource) давало для него 0 — так же поступаем здесь.
func expCostsValue(v any, kind colKind) sql.NullString {
	out := expCostsRaw(v, kind)
	if !out.Valid || (kind != kInt && kind != kDec && kind != kFloat) {
		return out
	}
	s := strings.ReplaceAll(strings.TrimSpace(out.String), ",", ".")
	f, err := strconv.ParseFloat(s, 64)
	switch {
	case err != nil:
		return sql.NullString{String: "0", Valid: true}
	case kind == kInt:
		return sql.NullString{String: strconv.FormatInt(int64(f), 10), Valid: true}
	default:
		return sql.NullString{String: s, Valid: true}
	}
}

func expCostsRaw(v any, kind colKind) sql.NullString {
	switch x := v.(type) {
	case nil:
		return sql.NullString{}
	case time.Time:
		if kind == kDateTime {
			return sql.NullString{String: x.Format("2006-01-02 15:04:05"), Valid: true}
		}
		return sql.NullString{String: x.Format("2006-01-02"), Valid: true}
	case []byte:
		return sql.NullString{String: strings.TrimSpace(string(x)), Valid: true}
	case string:
		return sql.NullString{String: strings.TrimSpace(x), Valid: true}
	case float64:
		return sql.NullString{String: strconv.FormatFloat(x, 'f', -1, 64), Valid: true}
	case float32:
		return sql.NullString{String: strconv.FormatFloat(float64(x), 'f', -1, 32), Valid: true}
	case int64:
		return sql.NullString{String: strconv.FormatInt(x, 10), Valid: true}
	case int32:
		return sql.NullString{String: strconv.FormatInt(int64(x), 10), Valid: true}
	case int:
		return sql.NullString{String: strconv.Itoa(x), Valid: true}
	case bool:
		if x {
			return sql.NullString{String: "1", Valid: true}
		}
		return sql.NullString{String: "0", Valid: true}
	default:
		return sql.NullString{String: strings.TrimSpace(fmt.Sprint(x)), Valid: true}
	}
}

// expCostsLayout — какие колонки выдачи в какие позиции CH-строки; ошибка, если нет
// обязательной колонки.
func expCostsLayout(cols []string) (idx []int, err error) {
	byName := map[string]int{}
	for i, c := range cols {
		byName[strings.ToLower(strings.TrimSpace(c))] = i
	}
	idx = make([]int, len(expCostsCols))
	var missing []string
	for i, c := range expCostsCols {
		j, ok := byName[strings.ToLower(c.Src)]
		if !ok && c.Alt != "" {
			j, ok = byName[strings.ToLower(c.Alt)]
		}
		if !ok {
			idx[i] = -1
			if c.Required {
				missing = append(missing, c.Src)
			}
			continue
		}
		idx[i] = j
	}
	if len(missing) > 0 {
		return nil, fmt.Errorf("в выдаче процедуры нет обязательных колонок: %s", strings.Join(missing, ", "))
	}
	return idx, nil
}

// Точки подмены для тестов: часы источника, решение «нужна ли заливка» и сама заливка.
var (
	expCostsSourceClock = sourceClock
	expCostsNeededFn    = expCostsNeeded
	expCostsLoad        = runExpCosts
)

// expCostsLast — итог последней законченной заливки в этом процессе: кнопка, дождавшаяся
// чужой заливки, должна узнать, чем та закончилась.
var expCostsLast struct {
	sync.Mutex
	seq int
	err error
}

func expCostsNoteResult(err error) {
	expCostsLast.Lock()
	expCostsLast.seq++
	expCostsLast.err = err
	expCostsLast.Unlock()
}

func expCostsResult() (int, error) {
	expCostsLast.Lock()
	defer expCostsLast.Unlock()
	return expCostsLast.seq, expCostsLast.err
}

// expCostsRunSem — одна заливка за раз на процесс (канал: ожидание чужой заливки
// прерывается по контексту).
var expCostsRunSem = make(chan struct{}, 1)

// RunExpCostsDaily — ежедневная заливка. Безопасно звать часто (воркер раз в
// интервал): без Force заливка идёт только если за сегодня её ещё не было и сейчас
// окно времени (или таблица пуста), а после ошибки — не чаще раза в RetryAfter.
// ran == false — заливка не понадобилась.
func RunExpCostsDaily(ctx context.Context, deps Deps, opts ExpCostsOpts) (rows int64, ran bool, err error) {
	opts.normalize()
	ch, err := newCHClient(deps.CHURL, deps.CHUser, deps.CHPass)
	if err != nil {
		return 0, false, fmt.Errorf("expcosts: ch client: %w", err)
	}
	sourceDate, hour, err := expCostsSourceClock(ctx, deps.MSSQL)
	if err != nil {
		return 0, false, fmt.Errorf("expcosts: часы MSSQL: %w", err)
	}
	if !opts.Force {
		need, err := expCostsNeededFn(ctx, ch, sourceDate, hour, opts)
		if err != nil {
			return 0, false, fmt.Errorf("expcosts: журнал: %w", err)
		}
		if !need {
			return 0, false, nil
		}
	}

	// Очередь занимаем только на саму заливку, а не на проверку «нужна ли она»: короткая
	// проверка воркера не должна принимать кнопку за «чужую заливку».
	select {
	case expCostsRunSem <- struct{}{}:
		defer func() { <-expCostsRunSem }()
	default:
		return 0, false, nil // уже идёт другая заливка
	}

	started := time.Now()
	n, skipped, err := expCostsLoad(ctx, deps, ch, opts)
	run := expCostsRun{
		SourceDate: sourceDate, StartedAt: started.Unix(), FinishedAt: time.Now().Unix(),
		Status: "ok", Rows: n, Skipped: skipped, TriggeredBy: opts.TriggeredBy,
	}
	if err != nil {
		run.Status, run.Details = "error", cleanErr(err)
		log.Printf("expcosts: %v", err)
	} else {
		log.Printf("expcosts: загружено %d строк (отброшено старше %d года: %d) за %.0fs", n, opts.MinYear, skipped, time.Since(started).Seconds())
	}
	writeExpCostsRun(ctx, ch, run)
	expCostsNoteResult(err)
	return n, true, err
}

// expCostsNeeded — нужна ли заливка сейчас (без Force).
func expCostsNeeded(ctx context.Context, ch *chClient, sourceDate string, hour int, opts ExpCostsOpts) (bool, error) {
	done, err := chCount(ctx, ch, fmt.Sprintf(
		"SELECT count() FROM finance.exp_costs_etl_runs WHERE status = 'ok' AND source_date = toDate(%s)", chQuote(sourceDate)))
	if err != nil {
		return false, err
	}
	if done > 0 {
		return false, nil
	}
	// После ошибки не долбим источник: процедура тяжёлая.
	recentFail, err := chCount(ctx, ch, fmt.Sprintf(
		"SELECT count() FROM finance.exp_costs_etl_runs WHERE status = 'error' AND started_at > now() - INTERVAL %d SECOND",
		int(opts.RetryAfter/time.Second)))
	if err != nil {
		return false, err
	}
	if recentFail > 0 {
		return false, nil
	}
	inWindow := hour >= opts.AfterHour && (opts.BeforeHour <= opts.AfterHour || hour < opts.BeforeHour)
	if inWindow {
		return true, nil
	}
	// Первая заливка после выката: таблица пуста — не ждём следующей ночи.
	have, err := chCount(ctx, ch, "SELECT count() FROM "+expCostsTarget)
	if err != nil {
		return false, err
	}
	return have == 0, nil
}

func chCount(ctx context.Context, ch *chClient, q string) (int64, error) {
	s, err := ch.queryString(ctx, q)
	if err != nil {
		return 0, err
	}
	if s == "" {
		return 0, nil
	}
	return strconv.ParseInt(s, 10, 64)
}

// runExpCosts — процедура → <target>__new → сверка → EXCHANGE.
func runExpCosts(ctx context.Context, deps Deps, ch *chClient, opts ExpCostsOpts) (rows, skipped int64, err error) {
	stage := expCostsTarget + "__new"
	if err := ch.exec(ctx, "DROP TABLE IF EXISTS "+stage); err != nil {
		return 0, 0, fmt.Errorf("drop stage: %w", err)
	}
	if err := ch.exec(ctx, "CREATE TABLE "+stage+" AS "+expCostsTarget); err != nil {
		return 0, 0, fmt.Errorf("create stage: %w", err)
	}
	defer func() { _ = ch.exec(context.WithoutCancel(ctx), "DROP TABLE IF EXISTS "+stage) }()

	ctx, cancel := context.WithTimeout(ctx, 45*time.Minute)
	defer cancel()
	// NOCOUNT: иначе процедура сначала отдаёт «N rows affected» вместо набора строк.
	res, err := deps.MSSQL.QueryContext(ctx, "SET NOCOUNT ON; EXEC "+opts.Proc)
	if err != nil {
		return 0, 0, fmt.Errorf("mssql proc: %w", err)
	}
	defer res.Close()
	cols, err := res.Columns()
	if err != nil {
		return 0, 0, err
	}
	idx, err := expCostsLayout(cols)
	if err != nil {
		return 0, 0, err
	}

	var (
		buf   bytes.Buffer
		inBuf int
		raw   = make([]any, len(cols))
		ptrs  = make([]any, len(cols))
		names = make([][]byte, len(expCostsCols))
		minM  = fmt.Sprintf("%04d-01-01", opts.MinYear)
		mCol  = idx[2] // Month
	)
	for i := range raw {
		ptrs[i] = &raw[i]
	}
	for i, c := range expCostsCols {
		n, _ := json.Marshal(c.Name)
		names[i] = append(n, ':')
	}
	flush := func() error {
		if inBuf == 0 {
			return nil
		}
		if err := ch.insertJSON(ctx, stage, &buf); err != nil {
			return err
		}
		rows += int64(inBuf)
		inBuf = 0
		buf.Reset()
		return nil
	}
	for res.Next() {
		if err := res.Scan(ptrs...); err != nil {
			return rows, skipped, fmt.Errorf("scan: %w", err)
		}
		// Историю старше MinYear не храним (ТЗ §2.1: строки до 2024 года отбрасываются).
		if mv := expCostsValue(raw[mCol], kDate); !mv.Valid || !expCostsIsDate(mv.String) || mv.String < minM {
			skipped++
			continue
		}
		buf.WriteByte('{')
		for i, c := range expCostsCols {
			if i > 0 {
				buf.WriteByte(',')
			}
			buf.Write(names[i])
			if idx[i] < 0 {
				buf.WriteString("null")
				continue
			}
			v := expCostsValue(raw[idx[i]], c.Kind)
			if !v.Valid {
				buf.WriteString("null")
				continue
			}
			s, err := json.Marshal(v.String)
			if err != nil {
				return rows, skipped, err
			}
			buf.Write(s)
		}
		buf.WriteString("}\n")
		inBuf++
		if inBuf >= opts.BatchSize {
			if err := flush(); err != nil {
				return rows, skipped, err
			}
		}
	}
	if err := res.Err(); err != nil {
		return rows, skipped, fmt.Errorf("rows: %w", err)
	}
	if err := flush(); err != nil {
		return rows, skipped, err
	}

	got, err := chCount(ctx, ch, "SELECT count() FROM "+stage)
	if err != nil {
		return rows, skipped, fmt.Errorf("count stage: %w", err)
	}
	if got != rows {
		return rows, skipped, fmt.Errorf("в %s %d строк, прочитано из MSSQL %d — подмену не делаем", stage, got, rows)
	}
	// Пустой или резко урезанный ответ процедуры (например, оборвалась на полпути без
	// ошибки) не должен заменить вчерашний снимок.
	have, err := chCount(ctx, ch, "SELECT count() FROM "+expCostsTarget)
	if err != nil {
		return rows, skipped, fmt.Errorf("count target: %w", err)
	}
	if err := expCostsSwapAllowed(rows, have); err != nil {
		return rows, skipped, err
	}
	if err := ch.exec(ctx, "EXCHANGE TABLES "+stage+" AND "+expCostsTarget); err != nil {
		return rows, skipped, fmt.Errorf("exchange: %w", err)
	}
	return rows, skipped, nil
}

// expCostsRun — строка finance.exp_costs_etl_runs.
type expCostsRun struct {
	SourceDate  string `json:"source_date"`
	StartedAt   int64  `json:"started_at"`
	FinishedAt  int64  `json:"finished_at"`
	Status      string `json:"status"`
	Rows        int64  `json:"rows"`
	Skipped     int64  `json:"skipped"`
	Details     string `json:"details"`
	TriggeredBy string `json:"triggered_by"`
}

func writeExpCostsRun(ctx context.Context, ch *chClient, r expCostsRun) {
	var buf bytes.Buffer
	if err := json.NewEncoder(&buf).Encode(r); err != nil {
		log.Printf("expcosts: журнал encode: %v", err)
		return
	}
	if err := ch.insertJSON(context.WithoutCancel(ctx), "finance.exp_costs_etl_runs", &buf); err != nil {
		log.Printf("expcosts: журнал insert: %v", err)
	}
}

// ExpCostsWorker — фоновая заливка затрат. Тикает раз в interval, сама заливка —
// раз в сутки (RunExpCostsDaily пропускает лишнее).
type ExpCostsWorker struct {
	deps Deps
	opts ExpCostsOpts
}

// NewExpCostsWorker — конструктор.
func NewExpCostsWorker(deps Deps, opts ExpCostsOpts) *ExpCostsWorker {
	opts.TriggeredBy, opts.Force = "cron", false
	return &ExpCostsWorker{deps: deps, opts: opts}
}

// Start запускает цикл. interval <= 0 — воркер выключен.
func (w *ExpCostsWorker) Start(ctx context.Context, interval time.Duration) {
	if interval <= 0 {
		log.Printf("expcosts-worker: disabled (EXP_COSTS_SYNC_INTERVAL=0)")
		return
	}
	o := w.opts
	o.normalize()
	log.Printf("expcosts-worker: enabled, interval=%s, окно %02d:00–%02d:00 по часам MSSQL", interval, o.AfterHour, o.BeforeHour)
	go func() {
		t := time.NewTimer(time.Minute)
		defer t.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-t.C:
				if _, _, err := RunExpCostsDaily(ctx, w.deps, w.opts); err != nil {
					log.Printf("expcosts-worker: %v", err)
				}
				t.Reset(interval)
			}
		}
	}()
}

// RefreshNow — внеплановая заливка по кнопке «Обновить из источника» (минуты).
func (w *ExpCostsWorker) RefreshNow(ctx context.Context) error {
	o := w.opts
	o.Force, o.TriggeredBy = true, "api"
	seqBefore, _ := expCostsResult()
	_, ran, err := RunExpCostsDaily(ctx, w.deps, o)
	if err == nil && !ran {
		// Заливка уже идёт (ночной воркер или кнопка на другой форме): дожидаемся её
		// окончания, а не отвечаем ошибкой — свежий снимок нужен и этому пользователю.
		select {
		case expCostsRunSem <- struct{}{}:
			<-expCostsRunSem
		case <-ctx.Done():
			return ctx.Err()
		}
		// Чужая заливка могла упасть: пользователь должен об этом узнать, а не увидеть
		// вчерашний снимок без объяснения.
		if seq, lastErr := expCostsResult(); seq != seqBefore && lastErr != nil {
			return fmt.Errorf("заливка, которую дождались, закончилась ошибкой: %w", lastErr)
		}
	}
	return err
}

// expCostsIsDate — «YYYY-MM-DD»: нечитаемая дата отбросила бы в CH весь батч.
func expCostsIsDate(s string) bool {
	if len(s) != 10 || s[4] != '-' || s[7] != '-' {
		return false
	}
	_, err := time.Parse("2006-01-02", s)
	return err == nil
}

// expCostsSwapAllowed — можно ли заменить действующий снимок новым: пустой или резко
// урезанный ответ процедуры (например, оборвалась на полпути без ошибки) вчерашний
// снимок не заменяет.
func expCostsSwapAllowed(rows, have int64) error {
	if rows == 0 || (have > 0 && rows < have/2) {
		return fmt.Errorf("процедура вернула %d строк при %d в снимке — подмену не делаем", rows, have)
	}
	return nil
}