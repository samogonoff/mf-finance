package etl

import (
	"bytes"
	"database/sql"
	"os"
	"regexp"
	"sort"
	"strings"
	"testing"
	"time"
)

var testFinDWHOpts = FinDWHOpts{DB: "FinDWH", GLMFFrom: time.Date(2025, 1, 1, 0, 0, 0, 0, time.UTC)}

func TestColSpecSelectExpr(t *testing.T) {
	cases := map[colKind]string{
		kStr:      "[X]",
		kDec:      "CONVERT(varchar(60), [X])",
		kFloat:    "CONVERT(varchar(60), CAST([X] AS decimal(38, 12)))",
		kInt:      "CONVERT(varchar(60), [X])",
		kDate:     "CONVERT(char(10), [X], 23)",
		kDateTime: "CONVERT(char(19), [X], 120)",
		kBit:      "CONVERT(varchar(1), CONVERT(tinyint, [X]))",
	}
	for k, want := range cases {
		if got := (colSpec{Src: "X", Kind: k}).selectExpr(); got != want {
			t.Errorf("kind %d: %q, want %q", k, got, want)
		}
	}
}

func TestJSONRowEncoder(t *testing.T) {
	enc := newJSONRowEncoder([]colSpec{{Name: "a"}, {Name: "b"}, {Name: "c"}})
	var buf bytes.Buffer
	err := enc.encode(&buf, []sql.NullString{
		{String: `кавычка " и \ слэш`, Valid: true},
		{},
		{String: "12.50", Valid: true},
	})
	if err != nil {
		t.Fatal(err)
	}
	want := `{"a":"кавычка \" и \\ слэш","b":null,"c":"12.50"}` + "\n"
	if buf.String() != want {
		t.Errorf("got %s want %s", buf.String(), want)
	}
}

func TestChQuote(t *testing.T) {
	if got := chQuote(`O'K\`); got != `'O\'K\\'` {
		t.Errorf("chQuote = %s", got)
	}
}

// Источник читаем без NOLOCK (RCSI даёт согласованный снимок) и без ISO-литералов дат.
func assertSafeSourceSQL(t *testing.T, name, q string) {
	t.Helper()
	if strings.Contains(strings.ToUpper(q), "NOLOCK") {
		t.Errorf("%s: NOLOCK не нужен — в FinDWH включён READ_COMMITTED_SNAPSHOT:\n%s", name, q)
	}
	if strings.Contains(strings.ToUpper(q), "ORDER BY") {
		t.Errorf("%s: ORDER BY гонит сортировку в tempdb:\n%s", name, q)
	}
	if regexp.MustCompile(`'\d{4}-\d{2}-\d{2}'`).MatchString(q) {
		t.Errorf("%s: ISO-литерал даты сломается при DATEFORMAT dmy:\n%s", name, q)
	}
}

func TestGLMFFingerprintSQL(t *testing.T) {
	q := glmfFingerprintSQL(testFinDWHOpts)
	assertSafeSourceSQL(t, "fingerprint", q)
	for _, want := range []string{
		"FROM [FinDWH].[dbo].[GLMF]",
		"[Date] >= CONVERT(datetime, @from, 112)",
		"COUNT_BIG(*)",
		"CHECKSUM_AGG(BINARY_CHECKSUM(",
		"SUM(CAST(BINARY_CHECKSUM(",
		"GROUP BY ISNULL([Company], N''), CAST([Date] AS date)",
		"[DateOfLoad]",
		"[TransDescription]",
	} {
		if !strings.Contains(q, want) {
			t.Errorf("fingerprint SQL не содержит %q", want)
		}
	}
	// Num — identity, новый после каждой пересборки: в отпечатке он сделал бы грязным всё.
	if strings.Contains(q, "[Num]") {
		t.Errorf("Num не должен входить в отпечаток")
	}
}

func TestGLMFExtractSQL(t *testing.T) {
	q := glmfExtractSQL(testFinDWHOpts, "MF", []string{"20260105", "20260107"})
	assertSafeSourceSQL(t, "extract", q)
	for _, want := range []string{
		"[Company] = @c",
		"[Date] >= CONVERT(datetime, @lo, 112) AND [Date] < CONVERT(datetime, @hi, 112)",
		"CONVERT(char(8), [Date], 112) IN ('20260105', '20260107')",
		"CONVERT(char(19), [Date], 120)",
		"CONVERT(varchar(1), CONVERT(tinyint, [ICO]))",
	} {
		if !strings.Contains(q, want) {
			t.Errorf("extract SQL не содержит %q:\n%s", want, q)
		}
	}
	if strings.Contains(q, "[Num]") {
		t.Errorf("Num не переносится в реплику")
	}
	empty := glmfExtractSQL(testFinDWHOpts, "", []string{"20260105"})
	if !strings.Contains(empty, "([Company] IS NULL OR [Company] = N'')") || strings.Contains(empty, "@c") {
		t.Errorf("пустой код компании должен искать NULL/'':\n%s", empty)
	}
}

func TestSnapshotSQL(t *testing.T) {
	ex := exRatesSnapshot(testFinDWHOpts)
	assertSafeSourceSQL(t, "ex_rates", ex.Query)
	for _, sfx := range exRatesTables {
		if !strings.Contains(ex.Query, "SELECT N'"+sfx+"' AS [country], ") ||
			!strings.Contains(ex.Query, "FROM [FinDWH].[dbo].[ExRates"+sfx+"]") {
			t.Errorf("ex_rates: нет ветки для ExRates%s:\n%s", sfx, ex.Query)
		}
	}
	if got := strings.Count(ex.Query, "UNION ALL"); got != len(exRatesTables)-1 {
		t.Errorf("ex_rates: UNION ALL = %d", got)
	}

	pl := tableFinPLSnapshot(testFinDWHOpts)
	assertSafeSourceSQL(t, "table_fin_pl", pl.Query)
	for _, want := range []string{"[Компания]", "[Корр-ка]", "[Метод.нюансы]", "[Для отчета_скрипт]", "FROM [FinDWH].[dbo].[Table_Fin_PL]"} {
		if !strings.Contains(pl.Query, want) {
			t.Errorf("table_fin_pl SQL не содержит %q", want)
		}
	}
}

// Колонки в Go обязаны совпадать с миграцией: иначе INSERT упадёт уже на проде.
func TestFinDWHColumnsMatchMigration(t *testing.T) {
	raw, err := os.ReadFile("../../../clickhouse/migrations/012_findwh.up.sql")
	if err != nil {
		t.Fatal(err)
	}
	tables := migrationColumns(string(raw))
	check := func(table string, cols []colSpec) {
		t.Helper()
		var got []string
		for _, c := range cols {
			got = append(got, c.Name)
		}
		want := tables[table]
		sort.Strings(got)
		sort.Strings(want)
		if strings.Join(got, ",") != strings.Join(want, ",") {
			t.Errorf("%s:\n go:  %v\n sql: %v", table, got, want)
		}
	}
	check("finance.findwh_glmf", glmfCols)
	check("finance.findwh_table_fin_pl", tableFinPLCols)
	check("finance.findwh_ex_rates", exRatesCols)
}

// migrationColumns — колонки CREATE TABLE (кроме служебной loaded_at).
func migrationColumns(sql string) map[string][]string {
	out := map[string][]string{}
	var cur string
	head := regexp.MustCompile(`CREATE TABLE IF NOT EXISTS (\S+)`)
	col := regexp.MustCompile(`^\s+([a-z_0-9]+)\s+\S`)
	for _, line := range strings.Split(sql, "\n") {
		if m := head.FindStringSubmatch(line); m != nil {
			cur = m[1]
			continue
		}
		if strings.HasPrefix(line, ")") {
			cur = ""
			continue
		}
		if m := col.FindStringSubmatch(line); cur != "" && m != nil && m[1] != "loaded_at" {
			out[cur] = append(out[cur], m[1])
		}
	}
	return out
}

func TestDiffFingerprints(t *testing.T) {
	fp := func(n uint64, sum string, x int32) dayFP { return dayFP{N: n, Sum: sum, Xor: x, FpSum: int64(x)} }
	src := map[dayKey]dayFP{
		{"MF", "2026-01-05"}: fp(10, "10.50", 1), // совпал (CH печатает 10.5)
		{"MF", "2026-01-06"}: fp(10, "1.00", 2),  // изменилась контрольная сумма
		{"MF", "2026-01-07"}: fp(3, "1.00", 3),   // новый день
	}
	dst := map[dayKey]dayFP{
		{"MF", "2026-01-05"}: fp(10, "10.5", 1),
		{"MF", "2026-01-06"}: fp(10, "1", 9),
		{"MF", "2026-01-04"}: fp(1, "0", 4), // пропал из источника
	}
	got := diffFingerprints(src, dst)
	want := []dayKey{{"MF", "2026-01-04"}, {"MF", "2026-01-06"}, {"MF", "2026-01-07"}}
	if len(got) != len(want) {
		t.Fatalf("dirty = %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("dirty = %v, want %v", got, want)
		}
	}
}

func TestPlanChunks(t *testing.T) {
	dirty := []dayKey{
		{"MF", "2025-01-03"}, {"MF", "2025-01-09"}, {"MF", "2025-02-01"},
		{"MF", "2025-03-15"}, {"MF", "2025-07-01"},
		{"TDMF", "2025-01-02"},
	}
	got := planChunks(dirty, 3)
	if len(got) != 3 {
		t.Fatalf("chunks = %+v", got)
	}
	if got[0].Company != "MF" || len(got[0].Months) != 3 || got[0].Months[0] != 202501 || got[0].Months[2] != 202503 || len(got[0].Days) != 4 {
		t.Errorf("chunk0 = %+v", got[0])
	}
	if got[1].Company != "MF" || len(got[1].Months) != 1 || got[1].Months[0] != 202507 {
		t.Errorf("chunk1 = %+v", got[1])
	}
	if got[2].Company != "TDMF" || got[2].Months[0] != 202501 {
		t.Errorf("chunk2 = %+v", got[2])
	}
}

func TestCompareStage(t *testing.T) {
	c := glmfChunk{Company: "MF", Months: []int{202601}}
	src := map[dayKey]dayFP{
		{"MF", "2026-01-05"}:   {N: 2, Sum: "10.00"},
		{"MF", "2026-01-06"}:   {N: 1, Sum: "-3.10"},
		{"MF", "2026-02-01"}:   {N: 7, Sum: "1"}, // чужая секция — не сверяем
		{"TDMF", "2026-01-05"}: {N: 9, Sum: "1"}, // чужая компания
	}
	ok := map[string]dayFP{"2026-01-05": {N: 2, Sum: "10"}, "2026-01-06": {N: 1, Sum: "-3.1"}}
	if err := compareStage(c, src, ok); err != nil {
		t.Errorf("ожидали совпадение: %v", err)
	}
	bad := map[string]dayFP{"2026-01-05": {N: 1, Sum: "10"}, "2026-01-09": {N: 1, Sum: "0"}}
	err := compareStage(c, src, bad)
	if err == nil {
		t.Fatal("ожидали расхождение")
	}
	for _, want := range []string{"2026-01-05: строк 1/2", "2026-01-06: нет в stage", "2026-01-09: лишний день"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("в ошибке нет %q: %v", want, err)
		}
	}
}

func TestParseCHFingerprints(t *testing.T) {
	got, err := parseCHFingerprints("MF\t2026-01-05\t10\t10.5\t-12\t345\n\t2026-01-06\t1\t0\t0\t0")
	if err != nil {
		t.Fatal(err)
	}
	if fp := got[dayKey{"MF", "2026-01-05"}]; fp.N != 10 || fp.Sum != "10.5" || fp.Xor != -12 || fp.FpSum != 345 {
		t.Errorf("MF = %+v", fp)
	}
	if _, ok := got[dayKey{"", "2026-01-06"}]; !ok {
		t.Errorf("пустой код компании потерян: %v", got)
	}
	if _, err := parseCHFingerprints("битая строка"); err == nil {
		t.Error("ожидали ошибку разбора")
	}
}

func TestParseFinDWHDate(t *testing.T) {
	if d, err := ParseFinDWHDate("2025-01-01"); err != nil || d.Format("20060102") != "20250101" {
		t.Errorf("got %v %v", d, err)
	}
	if _, err := ParseFinDWHDate("01.01.2025"); err == nil {
		t.Error("ожидали ошибку")
	}
}
