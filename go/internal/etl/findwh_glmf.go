package etl

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"log"
	"math/big"
	"sort"
	"strconv"
	"strings"
	"time"
)

// GLMF → finance.findwh_glmf: сверка по отпечаткам дней и замена секций.
//
// Почему не инкремент: GLMF_exec каждую ночь пересобирает таблицу целиком
// (GLMF_load + SWITCH), GLMF.Num — identity и после пересборки другой, а Premaster
// перезаливает задним числом кусками (до 44 разных DateOfLoad в одной паре
// компания×месяц). Водяной знак DateOfLoad не видит удалений, сторно и
// переклассификации, ключа для upsert нет.
//
// Поэтому:
//  1. MSSQL сам считает отпечаток каждого дня (компания × дата): COUNT, SUM суммы
//     BYN и две контрольные суммы BINARY_CHECKSUM по всем колонкам, кроме Num.
//     По сети — ~8 тыс. строк; скан GLMF остаётся на сервере (~5 мин).
//  2. Сравниваем с finance.findwh_glmf_day_fp — отпечатками того, что лежит в реплике.
//     Разошедшийся, новый или пропавший день — «грязный».
//  3. Грязные дни компании берём пачками по ChunkMonths месяцев (seek в GLMF
//     возможен только по Company — кластерный индекс Company,DrAcc,CrAcc,Date).
//     Секция (company, месяц) собирается в finance.findwh_glmf_stage из чистых дней,
//     уже лежащих в CH, плюс свежих строк грязных дней, сверяется с отпечатками
//     источника и ставится REPLACE PARTITION — атомарно для читателя. Секция,
//     оставшаяся пустой, при REPLACE просто исчезает.
//  4. Отпечатки грязных дней пишутся после замены — прерванная загрузка
//     продолжится со следующей пачки, а не с начала.

// glmfCols — dbo.GLMF → finance.findwh_glmf (все колонки, кроме Num).
var glmfCols = []colSpec{
	{"BaseDocID", "base_doc_id", kStr},
	{"DocID", "doc_id", kStr},
	{"DocName1C", "doc_name_1c", kStr},
	{"CompanyID", "company_id", kStr},
	{"CodeCFO", "code_cfo", kStr},
	{"CodePL", "code_pl", kStr},
	{"Date", "date", kDateTime},
	{"TransDescription", "trans_description", kStr},
	{"OperationDescription", "operation_description", kStr},
	{"CounterpartyID", "counterparty_id", kStr},
	{"CounterpartyName", "counterparty_name", kStr},
	{"Performer1C", "performer_1c", kStr},
	{"AmountWOVATCurrency", "amount_wovat_currency", kDec},
	{"AmountWithVATCurrency", "amount_with_vat_currency", kDec},
	{"DrAcc", "dr_acc", kStr},
	{"CrAcc", "cr_acc", kStr},
	{"ResposiblePersonDept", "resposible_person_dept", kStr},
	{"AmountWOVATCurrencyFact", "amount_wovat_currency_fact", kDec},
	{"AmountWithVATCurrencyFact", "amount_with_vat_currency_fact", kDec},
	{"ExRate", "ex_rate", kFloat},
	{"AmountWOVATBelRubFact", "amount_wovat_byn_fact", kDec},
	{"AmountWithVATBelRubFact", "amount_with_vat_byn_fact", kDec},
	{"Country", "country", kStr},
	{"GroupCFO1", "group_cfo1", kStr},
	{"GroupCFO2", "group_cfo2", kStr},
	{"Department", "department", kStr},
	{"GroupPL", "group_pl", kStr},
	{"Expense", "expense", kStr},
	{"Month", "month", kDate},
	{"ResposiblePersonFinContr", "resposible_person_fin_contr", kStr},
	{"Company", "company", kStr},
	{"StratBudgetAmountWOVATCurrency", "strat_budget_amount_wovat_currency", kDec},
	{"StratBudgetAmountWOVATBelRub", "strat_budget_amount_wovat_byn", kDec},
	{"BudgetAmountWOVATCurrency", "budget_amount_wovat_currency", kDec},
	{"BudgetAmountWOVATBelRub", "budget_amount_wovat_byn", kDec},
	{"DbAnalytics1", "db_analytics1", kStr},
	{"PLDescrBK", "pl_descr_bk", kStr},
	{"ICO", "ico", kBit},
	{"DateOfLoad", "date_of_load", kDate},
	{"DrSubconto1Id", "dr_subconto1_id", kInt},
	{"DrSubconto2Id", "dr_subconto2_id", kInt},
	{"DrSubconto3Id", "dr_subconto3_id", kInt},
	{"DrSubconto4Id", "dr_subconto4_id", kInt},
	{"CrSubconto1Id", "cr_subconto1_id", kInt},
	{"CrSubconto2Id", "cr_subconto2_id", kInt},
	{"CrSubconto3Id", "cr_subconto3_id", kInt},
	{"CrSubconto4Id", "cr_subconto4_id", kInt},
}

const glmfStage = "finance.findwh_glmf_stage"

// dirtyWarnShare — доля грязных дней, выше которой пишем warn: так выглядит
// глобальная смена маппинга в DWH (по сути полная перезагрузка — делаем, но видно в логе).
const dirtyWarnShare = 0.3

// dayKey — день GLMF: код компании (” = NULL в источнике) и дата 'YYYY-MM-DD'.
type dayKey struct {
	Company string
	Date    string
}

// month — yyyymm дня (ключ секции вместе с компанией).
func (k dayKey) month() int {
	y, _ := strconv.Atoi(k.Date[0:4])
	m, _ := strconv.Atoi(k.Date[5:7])
	return y*100 + m
}

// dayFP — отпечаток дня.
type dayFP struct {
	N     uint64
	Sum   string // SUM(AmountWithVATBelRubFact), десятичная строка
	Xor   int32  // CHECKSUM_AGG(BINARY_CHECKSUM(...))
	FpSum int64  // SUM(CAST(BINARY_CHECKSUM(...) AS BIGINT))
}

// equal — суммы сравниваем как числа: CH печатает Decimal без хвостовых нулей.
func (a dayFP) equal(b dayFP) bool {
	return a.N == b.N && a.Xor == b.Xor && a.FpSum == b.FpSum && decEqual(a.Sum, b.Sum)
}

func decEqual(a, b string) bool {
	if a == b {
		return true
	}
	ra, ok1 := new(big.Rat).SetString(a)
	rb, ok2 := new(big.Rat).SetString(b)
	return ok1 && ok2 && ra.Cmp(rb) == 0
}

// glmfChecksumExpr — BINARY_CHECKSUM по всем колонкам, кроме Num. DateOfLoad
// входит: перезалитый день обновится в реплике вместе с датой загрузки.
func glmfChecksumExpr() string {
	parts := make([]string, len(glmfCols))
	for i, c := range glmfCols {
		parts[i] = "[" + c.Src + "]"
	}
	return "BINARY_CHECKSUM(" + strings.Join(parts, ", ") + ")"
}

// glmfFingerprintSQL — отпечатки всех дней с @from ('YYYYMMDD'). Один скан на
// стороне сервера; таймаут чтения в DSN задавать нельзя — запрос молчит, пока считает.
func glmfFingerprintSQL(opts FinDWHOpts) string {
	cs := glmfChecksumExpr()
	return `
SELECT ISNULL([Company], N'') AS company,
       CONVERT(char(10), CAST([Date] AS date), 23) AS d,
       COUNT_BIG(*) AS n,
       CONVERT(varchar(60), SUM(ISNULL([AmountWithVATBelRubFact], 0))) AS sum_byn,
       CHECKSUM_AGG(` + cs + `) AS fp_xor,
       SUM(CAST(` + cs + ` AS bigint)) AS fp_sum
FROM ` + opts.fqn("GLMF") + `
WHERE [Date] >= CONVERT(datetime, @from, 112)
GROUP BY ISNULL([Company], N''), CAST([Date] AS date)`
}

func srcFingerprints(ctx context.Context, db *sql.DB, opts FinDWHOpts) (map[dayKey]dayFP, error) {
	rows, err := db.QueryContext(ctx, glmfFingerprintSQL(opts),
		sql.Named("from", opts.GLMFFrom.Format("20060102")))
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := map[dayKey]dayFP{}
	for rows.Next() {
		var (
			k   dayKey
			fp  dayFP
			n   int64
			xor sql.NullInt32
		)
		if err := rows.Scan(&k.Company, &k.Date, &n, &fp.Sum, &xor, &fp.FpSum); err != nil {
			return nil, err
		}
		fp.N, fp.Xor = uint64(n), xor.Int32
		out[k] = fp
	}
	return out, rows.Err()
}

func chFingerprints(ctx context.Context, ch *chClient) (map[dayKey]dayFP, error) {
	body, err := ch.queryString(ctx, `SELECT company, toString(date), n, toString(sum_byn), fp_xor, fp_sum
FROM finance.findwh_glmf_day_fp FINAL WHERE is_deleted = 0 FORMAT TabSeparated`)
	if err != nil {
		return nil, err
	}
	return parseCHFingerprints(body)
}

// chDataDays — дни, за которые в finance.findwh_glmf реально есть строки.
func chDataDays(ctx context.Context, ch *chClient) (map[dayKey]struct{}, error) {
	body, err := ch.queryString(ctx,
		"SELECT DISTINCT company, toString(toDate(date)) FROM finance.findwh_glmf FORMAT TabSeparated")
	if err != nil {
		return nil, err
	}
	out := map[dayKey]struct{}{}
	if body == "" {
		return out, nil
	}
	for _, line := range strings.Split(body, "\n") {
		f := strings.Split(line, "\t")
		if len(f) != 2 {
			return nil, fmt.Errorf("finance.findwh_glmf: строка %q", line)
		}
		out[dayKey{Company: f[0], Date: f[1]}] = struct{}{}
	}
	return out, nil
}

func parseCHFingerprints(body string) (map[dayKey]dayFP, error) {
	out := map[dayKey]dayFP{}
	if body == "" {
		return out, nil
	}
	for _, line := range strings.Split(body, "\n") {
		f := strings.Split(line, "\t")
		if len(f) != 6 {
			return nil, fmt.Errorf("glmf_day_fp: неожиданная строка %q", line)
		}
		n, err1 := strconv.ParseUint(f[2], 10, 64)
		xor, err2 := strconv.ParseInt(f[4], 10, 32)
		sum, err3 := strconv.ParseInt(f[5], 10, 64)
		if err1 != nil || err2 != nil || err3 != nil {
			return nil, fmt.Errorf("glmf_day_fp: строка %q", line)
		}
		out[dayKey{Company: f[0], Date: f[1]}] = dayFP{N: n, Sum: f[3], Xor: int32(xor), FpSum: sum}
	}
	return out, nil
}

// diffFingerprints — грязные дни: новые, изменившиеся и пропавшие из источника.
func diffFingerprints(src, dst map[dayKey]dayFP) []dayKey {
	var dirty []dayKey
	for k, s := range src {
		if d, ok := dst[k]; !ok || !s.equal(d) {
			dirty = append(dirty, k)
		}
	}
	for k := range dst {
		if _, ok := src[k]; !ok {
			dirty = append(dirty, k)
		}
	}
	sortDays(dirty)
	return dirty
}

func sortDays(days []dayKey) {
	sort.Slice(days, func(i, j int) bool {
		if days[i].Company != days[j].Company {
			return days[i].Company < days[j].Company
		}
		return days[i].Date < days[j].Date
	})
}

// glmfChunk — пачка: одна компания, до ChunkMonths секций, их грязные дни.
type glmfChunk struct {
	Company string
	Months  []int    // yyyymm по возрастанию
	Days    []dayKey // грязные дни этих секций
}

// planChunks режет отсортированные грязные дни на пачки.
func planChunks(dirty []dayKey, chunkMonths int) []glmfChunk {
	var (
		out []glmfChunk
		cur *glmfChunk
	)
	for _, k := range dirty {
		m := k.month()
		if cur == nil || cur.Company != k.Company ||
			(cur.Months[len(cur.Months)-1] != m && len(cur.Months) >= chunkMonths) {
			out = append(out, glmfChunk{Company: k.Company})
			cur = &out[len(out)-1]
		}
		if len(cur.Months) == 0 || cur.Months[len(cur.Months)-1] != m {
			cur.Months = append(cur.Months, m)
		}
		cur.Days = append(cur.Days, k)
	}
	return out
}

// glmfCompanyPred — фильтр компании для MSSQL. Пустой код = NULL/” в источнике;
// для обычного кода — равенство, чтобы работал seek по кластерному индексу.
func glmfCompanyPred(company string) string {
	if company == "" {
		return "([Company] IS NULL OR [Company] = N'')"
	}
	return "[Company] = @c"
}

// glmfExtractSQL — строки грязных дней одной компании. Дни сравниваются строкой
// 'YYYYMMDD' (стиль 112) — без зависимости от DATEFORMAT сессии; диапазон
// @lo..@hi отсекает лишнее до сравнения.
func glmfExtractSQL(opts FinDWHOpts, company string, days []string) string {
	lits := make([]string, len(days))
	for i, d := range days {
		lits[i] = "'" + d + "'"
	}
	return "SELECT " + selectList(glmfCols) + "\nFROM " + opts.fqn("GLMF") +
		"\nWHERE " + glmfCompanyPred(company) +
		"\n  AND [Date] >= CONVERT(datetime, @lo, 112) AND [Date] < CONVERT(datetime, @hi, 112)" +
		"\n  AND CONVERT(char(8), [Date], 112) IN (" + strings.Join(lits, ", ") + ")"
}

// GLMFSyncStats — итог прогона GLMF (уходит в лог и finance.findwh_etl_runs.details).
type GLMFSyncStats struct {
	SourceDays  int   // дней в источнике с GLMFFrom
	DirtyDays   int   // перегружено дней (включая удалённые)
	DeletedDays int   // дней пропало из источника
	Partitions  int   // заменено секций
	Rows        int64 // строк прочитано из MSSQL
	FailedParts int   // секций, которые не удалось заменить (останутся грязными)
}

func (s GLMFSyncStats) String() string {
	return fmt.Sprintf("days=%d dirty=%d deleted=%d partitions=%d failed=%d",
		s.SourceDays, s.DirtyDays, s.DeletedDays, s.Partitions, s.FailedParts)
}

// RunFinDWHGLMF — сверка и докат GLMF. Пустая реплика = все дни грязные, так что
// начальная загрузка идёт тем же путём. GLMFFull → отпечатки реплики
// игнорируются и перегружается всё.
func RunFinDWHGLMF(ctx context.Context, deps Deps, opts FinDWHOpts) (GLMFSyncStats, error) {
	opts.normalize()
	var st GLMFSyncStats
	ch, err := newCHClient(deps.CHURL, deps.CHUser, deps.CHPass)
	if err != nil {
		return st, fmt.Errorf("ch client: %w", err)
	}

	t0 := time.Now()
	src, err := srcFingerprints(ctx, deps.MSSQL, opts)
	if err != nil {
		return st, fmt.Errorf("отпечатки MSSQL: %w", err)
	}
	st.SourceDays = len(src)
	dst, err := chFingerprints(ctx, ch)
	if err != nil {
		return st, fmt.Errorf("отпечатки CH: %w", err)
	}
	var dirty []dayKey
	if opts.GLMFFull {
		// Все дни источника и реплики. Дни реплики берём и из отпечатков, и из
		// самих данных: строки без отпечатка (правка руками, сбой) тоже уходят.
		present, err := chDataDays(ctx, ch)
		if err != nil {
			return st, fmt.Errorf("дни реплики: %w", err)
		}
		for k := range present {
			if _, ok := dst[k]; !ok {
				dst[k] = dayFP{}
			}
		}
		dirty = diffFingerprints(src, map[dayKey]dayFP{})
		for k := range dst {
			if _, ok := src[k]; !ok {
				dirty = append(dirty, k)
			}
		}
		sortDays(dirty)
	} else {
		dirty = diffFingerprints(src, dst)
	}
	st.DirtyDays = len(dirty)
	for _, k := range dirty {
		if _, ok := src[k]; !ok {
			st.DeletedDays++
		}
	}
	log.Printf("findwh: glmf отпечатки за %.0fs: дней в источнике %d, грязных %d (пропало %d)",
		time.Since(t0).Seconds(), st.SourceDays, st.DirtyDays, st.DeletedDays)
	if len(dirty) == 0 {
		return st, nil
	}
	if len(dst) > 0 && float64(len(dirty)) > dirtyWarnShare*float64(len(src)) {
		log.Printf("findwh: WARN glmf: грязных дней %d из %d — похоже на смену классификации в DWH, перегружаем почти всё",
			len(dirty), len(src))
	}

	if err := ch.exec(ctx, "DROP TABLE IF EXISTS "+glmfStage); err != nil {
		return st, fmt.Errorf("drop stage: %w", err)
	}
	if err := ch.exec(ctx, "CREATE TABLE "+glmfStage+" AS finance.findwh_glmf"); err != nil {
		return st, fmt.Errorf("create stage: %w", err)
	}
	defer func() { _ = ch.exec(context.WithoutCancel(ctx), "DROP TABLE IF EXISTS "+glmfStage) }()

	var firstErr error
	for _, c := range planChunks(dirty, opts.ChunkMonths) {
		if err := ctx.Err(); err != nil {
			return st, err
		}
		n, err := syncGLMFChunk(ctx, deps, ch, opts, c, src)
		st.Rows += n
		if err != nil {
			st.FailedParts += len(c.Months)
			log.Printf("findwh: glmf %s %v: %v", c.Company, c.Months, err)
			if firstErr == nil {
				firstErr = fmt.Errorf("glmf %s %v: %w", c.Company, c.Months, err)
			}
			continue
		}
		st.Partitions += len(c.Months)
		log.Printf("findwh: glmf %s %v: дней %d, строк из MSSQL %d", c.Company, c.Months, len(c.Days), n)
	}
	return st, firstErr
}

// syncGLMFChunk — сборка и замена секций одной пачки.
func syncGLMFChunk(ctx context.Context, deps Deps, ch *chClient, opts FinDWHOpts, c glmfChunk, src map[dayKey]dayFP) (int64, error) {
	if err := ch.exec(ctx, "TRUNCATE TABLE "+glmfStage); err != nil {
		return 0, fmt.Errorf("truncate stage: %w", err)
	}

	// 1. Чистые дни этих секций — копией из реплики (локально, по сети не едут).
	// В полном режиме реплике не доверяем: секции целиком из источника.
	months := make([]string, len(c.Months))
	for i, m := range c.Months {
		months[i] = strconv.Itoa(m)
	}
	dirtyDates := make([]string, len(c.Days))
	for i, k := range c.Days {
		dirtyDates[i] = chQuote(k.Date)
	}
	if !opts.GLMFFull {
		if _, err := ch.queryString(ctx, fmt.Sprintf(
			"INSERT INTO %s SELECT * FROM finance.findwh_glmf WHERE company = %s AND toYYYYMM(date) IN (%s) AND toDate(date) NOT IN (%s)",
			glmfStage, chQuote(c.Company), strings.Join(months, ", "), strings.Join(dirtyDates, ", "))); err != nil {
			return 0, fmt.Errorf("копия чистых дней: %w", err)
		}
	}

	// 2. Грязные дни, которые есть в источнике, — из MSSQL.
	var fetch []string
	for _, k := range c.Days {
		if _, ok := src[k]; ok {
			fetch = append(fetch, strings.ReplaceAll(k.Date, "-", ""))
		}
	}
	var n int64
	if len(fetch) > 0 {
		hi, _ := time.Parse("20060102", fetch[len(fetch)-1])
		rows, err := deps.MSSQL.QueryContext(ctx, glmfExtractSQL(opts, c.Company, fetch),
			sql.Named("c", c.Company),
			sql.Named("lo", fetch[0]),
			sql.Named("hi", hi.AddDate(0, 0, 1).Format("20060102")))
		if err != nil {
			return 0, fmt.Errorf("mssql query: %w", err)
		}
		n, err = streamToCH(ctx, ch, rows, glmfCols, glmfStage, opts.BatchSize)
		rows.Close()
		if err != nil {
			return n, err
		}
	}

	// 3. Сверка: секции в stage должны совпасть с источником день в день.
	if err := verifyGLMFStage(ctx, ch, c, src); err != nil {
		return n, err
	}

	// 4. Замена секций. Месяц, где не осталось строк, REPLACE из пустой stage удалит.
	for _, m := range c.Months {
		if err := ch.exec(ctx, fmt.Sprintf("ALTER TABLE finance.findwh_glmf REPLACE PARTITION tuple(%s, %d) FROM %s",
			chQuote(c.Company), m, glmfStage)); err != nil {
			return n, fmt.Errorf("replace partition %d: %w", m, err)
		}
	}

	// 5. Отпечатки грязных дней — теперь они описывают реплику.
	return n, writeGLMFFingerprints(ctx, ch, c.Days, src)
}

// verifyGLMFStage — каждый день источника из секций пачки есть в stage с тем же
// числом строк и суммой BYN, и лишних дней нет. Расхождение = источник поменялся
// посреди прогона или реплика разошлась со своими отпечатками; секции не трогаем.
func verifyGLMFStage(ctx context.Context, ch *chClient, c glmfChunk, src map[dayKey]dayFP) error {
	body, err := ch.queryString(ctx, "SELECT toString(toDate(date)), count(), toString(sum(ifNull(amount_with_vat_byn_fact, 0))) FROM "+
		glmfStage+" GROUP BY toDate(date) FORMAT TabSeparated")
	if err != nil {
		return fmt.Errorf("сверка stage: %w", err)
	}
	got := map[string]dayFP{}
	if body != "" {
		for _, line := range strings.Split(body, "\n") {
			f := strings.Split(line, "\t")
			if len(f) != 3 {
				return fmt.Errorf("сверка stage: строка %q", line)
			}
			n, _ := strconv.ParseUint(f[1], 10, 64)
			got[f[0]] = dayFP{N: n, Sum: f[2]}
		}
	}
	return compareStage(c, src, got)
}

// compareStage — чистая часть сверки (покрыта тестом).
func compareStage(c glmfChunk, src map[dayKey]dayFP, got map[string]dayFP) error {
	inChunk := map[int]bool{}
	for _, m := range c.Months {
		inChunk[m] = true
	}
	var problems []string
	for k, s := range src {
		if k.Company != c.Company || !inChunk[k.month()] {
			continue
		}
		g, ok := got[k.Date]
		switch {
		case !ok:
			problems = append(problems, k.Date+": нет в stage")
		case g.N != s.N || !decEqual(g.Sum, s.Sum):
			problems = append(problems, fmt.Sprintf("%s: строк %d/%d, сумма %s/%s", k.Date, g.N, s.N, g.Sum, s.Sum))
		}
		delete(got, k.Date)
	}
	for d := range got {
		problems = append(problems, d+": лишний день в stage")
	}
	if len(problems) == 0 {
		return nil
	}
	sort.Strings(problems)
	if len(problems) > 5 {
		problems = append(problems[:5], fmt.Sprintf("… ещё %d", len(problems)-5))
	}
	return fmt.Errorf("stage не сходится с источником (stage/источник): %s", strings.Join(problems, "; "))
}

// glmfFPRow — строка finance.findwh_glmf_day_fp.
type glmfFPRow struct {
	Company   string `json:"company"`
	Date      string `json:"date"`
	N         uint64 `json:"n"`
	SumByn    string `json:"sum_byn"`
	FpXor     int32  `json:"fp_xor"`
	FpSum     int64  `json:"fp_sum"`
	CheckedAt string `json:"checked_at"`
	IsDeleted uint8  `json:"is_deleted"`
}

func writeGLMFFingerprints(ctx context.Context, ch *chClient, days []dayKey, src map[dayKey]dayFP) error {
	now := time.Now().UTC().Format("2006-01-02 15:04:05.000")
	var buf bytes.Buffer
	enc := json.NewEncoder(&buf)
	for _, k := range days {
		r := glmfFPRow{Company: k.Company, Date: k.Date, SumByn: "0", CheckedAt: now, IsDeleted: 1}
		if fp, ok := src[k]; ok {
			r.N, r.SumByn, r.FpXor, r.FpSum, r.IsDeleted = fp.N, fp.Sum, fp.Xor, fp.FpSum, 0
		}
		if err := enc.Encode(&r); err != nil {
			return err
		}
	}
	if err := ch.insertJSON(ctx, "finance.findwh_glmf_day_fp", &buf); err != nil {
		return fmt.Errorf("glmf_day_fp: %w", err)
	}
	return nil
}
