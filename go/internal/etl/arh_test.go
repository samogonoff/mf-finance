package etl

import (
	"context"
	"reflect"
	"strings"
	"testing"
)

var testArhTables = ArhTables{
	DebtFQN: "[Payments].[dbo].[Debt_arh]", DocsFQN: "[Payments].[dbo].[Docs]",
	WholesalesFQN: "[Payments].[dbo].[Wholesales_arh]", Orgs: []string{"6950135110", "690591512"},
}

// Остатки: ВГО-фильтр, узкий диапазон одного дня по 'YYYYMMDD', знак строки, корзины по
// знаку Sum, дедуп Docs по ID (ROW_NUMBER) и родитель-договор вторым присоединением.
func TestExtractArhDebtSQL(t *testing.T) {
	q := extractArhDebtSQL(testArhTables)
	for _, want := range []string{
		"[Payments].[dbo].[Debt_arh]", "[Payments].[dbo].[Docs]",
		"d.UNPOrg IN (N'6950135110', N'690591512')", // sargable по индексу (UNPOrg, Date)
		"(d.Chnl = @grp OR d.Folder = @grp)",
		"d.[Date] >= CONVERT(date, @dt, 112)", "DATEADD(day, 1, CONVERT(date, @dt, 112))",
		"SUM(CASE WHEN d.Sum > 0 THEN ISNULL(d.Sum, 0) ELSE 0 END)",
		"SUM(CASE WHEN d.Sum < 0 THEN ISNULL(d.Sum, 0) ELSE 0 END)",
		"SUM(CASE WHEN d.Sum > 0 THEN ISNULL(d.Sum999, 0) ELSE 0 END)",
		"SUM(CASE WHEN d.Sum < 0 THEN ISNULL(d.Sum30, 0) ELSE 0 END)",
		"ROW_NUMBER() OVER (PARTITION BY x.ID", "WHERE q.rn = 1",
		"LEFT JOIN Doc dc ON dc.ID = d.DocID", "LEFT JOIN Par pc ON pc.ID = dc.ParentID",
		"x.ID IN (SELECT DocID FROM Src)", "p.ID IN (SELECT ParentID FROM Doc",
		"d.DocID IS NOT NULL AND d.DocID <> ''",
		"MAX(CONVERT(INT, ISNULL(dc.Delay, 0)))", "AS payment_date", "AS src_changed_at",
		"ISNULL(d.DateChange, d.[Date])", "GROUP BY", "UNION ALL", "'__snapshot__'",
	} {
		if !strings.Contains(q, want) {
			t.Errorf("extractArhDebtSQL не содержит %q", want)
		}
	}
	// DATE_DOC в Debt_arh пуст — не использовать; ISO-литералов дат нет.
	if strings.Contains(q, "DATE_DOC") {
		t.Errorf("DATE_DOC не должен использоваться")
	}
	if strings.Contains(q, "CAST(") {
		t.Errorf("CAST по строке не нужен: ID и DocID — nvarchar")
	}
	if strings.Contains(q, "'2026-") {
		t.Errorf("даты — только YYYYMMDD через параметры")
	}
	// Невалидные даты Docs (0001-01-01, 2202-xx) не должны улетать в CH Date.
	if !strings.Contains(q, ">= '19700102'") || !strings.Contains(q, "< '21000101'") {
		t.Errorf("нет защиты диапазона дат Docs:\n%s", q)
	}
}

// Без списка наших ЮЛ запрос не должен читать всю Debt_arh: условие заведомо ложное,
// а невалидный УНП в литерал не попадает.
func TestArhOrgCond(t *testing.T) {
	if got := arhOrgCond("d", nil); got != "1 = 0" {
		t.Errorf("пустой список → %q", got)
	}
	got := arhOrgCond("d", []string{"690591512", "x'; DROP TABLE y;--", " 6950135110 "})
	if got != "d.UNPOrg IN (N'690591512', N'6950135110')" {
		t.Errorf("фильтр = %q", got)
	}
}

// Отпечатки дней: узкий чанк [@from..@to] и весь контур наших ЮЛ, чтобы заметить
// появление/исчезновение служебной строки присутствия при нулевом ВГО-балансе.
func TestExtractArhDebtDatesSQL(t *testing.T) {
	q := extractArhDebtDatesSQL(testArhTables)
	for _, want := range []string{
		"MAX(ISNULL(d.DateChange, d.[Date]))", "CONVERT(date, @from, 112)", "DATEADD(day, 1, CONVERT(date, @to, 112))",
		"d.UNPOrg IN (", "GROUP BY",
	} {
		if !strings.Contains(q, want) {
			t.Errorf("extractArhDebtDatesSQL не содержит %q", want)
		}
	}
	if strings.Contains(q, "d.Chnl = @grp") || strings.Contains(q, arhDebtNonZero) {
		t.Errorf("отпечаток должен учитывать присутствие ЮЛ вне ненулевого ВГО-контура:\n%s", q)
	}
}

// Продажи: корни CrAcc 90/6010/9010, знак в ключе (продажа и возврат не схлопываются),
// ВГО по Chnl или Folder, один день.
func TestExtractArhSalesSQL(t *testing.T) {
	q := extractArhSalesSQL(testArhTables)
	for _, want := range []string{
		"[Payments].[dbo].[Wholesales_arh]", "w.UNPOrg IN (N'6950135110', N'690591512')",
		"(w.Chnl = @grp OR w.Folder = @grp)",
		"IN ('90', '6010', '9010')", "LTRIM(RTRIM(ISNULL(w.CrAcc, '')))",
		"w.[Date] >= CONVERT(date, @dt, 112)",
		"SIGN(ISNULL(w.Sum, 0))", "AS amount_sign", "SUM(ISNULL(w.Sum, 0))",
		"AS event_date", "AS src_changed_at", "GROUP BY",
	} {
		if !strings.Contains(q, want) {
			t.Errorf("extractArhSalesSQL не содержит %q", want)
		}
	}
	grp := q[strings.Index(q, "GROUP BY"):]
	if !strings.Contains(grp, "SIGN(ISNULL(w.Sum, 0))") {
		t.Errorf("знак суммы должен быть в GROUP BY:\n%s", grp)
	}
	if !strings.Contains(extractArhSalesDatesSQL(testArhTables), "MAX(ISNULL(w.DateChange, w.[Date]))") {
		t.Errorf("отпечаток продаж по DateChange отсутствует")
	}
}

// Окно: концы месяцев до окна + каждый день окна; не раньше MinDate; продажи — каждый день.
func TestArhCandidateDates(t *testing.T) {
	a := ArhOpts{MinDate: "2026-06-01", WindowDays: 3}
	got, err := arhCandidateDates(FinDWHArhDebt, "2026-10-19", a)
	if err != nil {
		t.Fatal(err)
	}
	want := []string{"2026-06-30", "2026-07-31", "2026-08-31", "2026-09-30", "2026-10-16", "2026-10-17", "2026-10-18", "2026-10-19"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("debt: %v\nожидалось %v", got, want)
	}
	// Окно длиннее истории — клампится к MinDate, концы месяцев не дублируются.
	got, _ = arhCandidateDates(FinDWHArhDebt, "2026-06-03", ArhOpts{MinDate: "2026-06-01", WindowDays: 45})
	if len(got) != 3 || got[0] != "2026-06-01" {
		t.Errorf("кламп к MinDate: %v", got)
	}
	got, _ = arhCandidateDates(FinDWHArhSales, "2026-06-03", ArhOpts{MinDate: "2026-06-01", WindowDays: 3})
	if !reflect.DeepEqual(got, []string{"2026-06-01", "2026-06-02", "2026-06-03"}) {
		t.Errorf("sales: %v", got)
	}
	if _, err := arhCandidateDates(FinDWHArhDebt, "2026-10-19", ArhOpts{MinDate: "01.06.2026"}); err == nil {
		t.Errorf("ожидалась ошибка разбора MinDate")
	}
}

func TestArhChunks(t *testing.T) {
	dates := []string{"2026-06-30", "2026-07-31", "2026-10-10", "2026-10-11", "2026-10-12", "2026-10-13"}
	got := arhChunks(dates, 3)
	want := [][2]string{{"2026-06-30", "2026-06-30"}, {"2026-07-31", "2026-07-31"}, {"2026-10-10", "2026-10-12"}, {"2026-10-13", "2026-10-13"}}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("чанки: %v, ожидалось %v", got, want)
	}
}

// Инкремент по отпечатку: перезагружаются только расходящиеся/новые даты, исчезнувшие
// в источнике — сносятся; full — все даты источника.
func TestPlanArhDates(t *testing.T) {
	cand := []string{"2026-10-17", "2026-10-18", "2026-10-19", "2026-10-20"}
	src := map[string]arhFP{
		"2026-10-17": {"2026-10-19 10:00:00", 3}, "2026-10-18": {"2026-10-19 12:00:00", 3}, "2026-10-19": {"2026-10-19 09:00:00", 3},
	}
	have := map[string]string{
		"2026-10-17": arhFP{"2026-10-19 10:00:00", 3}.key(), "2026-10-18": arhFP{"2026-10-18 08:00:00", 3}.key(),
		"2026-10-20": arhFP{"2026-10-20 01:00:00", 1}.key(),
	}
	p := planArhDates(cand, src, have, nil, false)
	if !reflect.DeepEqual(p.reload, []string{"2026-10-18", "2026-10-19"}) {
		t.Errorf("reload = %v", p.reload)
	}
	if !reflect.DeepEqual(p.drop, []string{"2026-10-20"}) {
		t.Errorf("drop = %v", p.drop)
	}
	if p := planArhDates(cand, src, have, nil, true); len(p.reload) != 3 {
		t.Errorf("full: reload = %v", p.reload)
	}
	// число строк источника изменилось при том же MAX(DateChange) — тоже перезагрузка
	src["2026-10-17"] = arhFP{"2026-10-19 10:00:00", 4}
	if p := planArhDates(cand, src, have, nil, false); len(p.reload) != 3 {
		t.Errorf("изменилось число строк: reload = %v", p.reload)
	}
	// данные есть, отпечатка нет (оборванная замена), в источнике даты нет → drop
	if p := planArhDates([]string{"2026-09-30"}, map[string]arhFP{}, map[string]string{}, map[string]bool{"2026-09-30": true}, false); len(p.drop) != 1 {
		t.Errorf("drop по данным без отпечатка: %v", p.drop)
	}
}

// ARH встроен в реплику: таблицы появляются в списке (и на странице /admin/etl) только
// когда поток настроен; готовность — по часу ARH_READY_AFTER_HOUR, а не по msdb.
func TestArhInFinDWHReplica(t *testing.T) {
	o := FinDWHOpts{}
	o.normalize()
	if len(o.Tables) != 3 {
		t.Errorf("без ARH — только три таблицы FinDWH: %v", o.Tables)
	}
	o = FinDWHOpts{Arh: ArhOpts{Tables: testArhTables}}
	o.normalize()
	if len(o.Tables) != 5 || o.Tables[3] != FinDWHArhDebt || o.Tables[4] != FinDWHArhSales {
		t.Errorf("с ARH ожидались 5 таблиц: %v", o.Tables)
	}
	if o.Arh.ReadyAfterHour != arhDefaultReady || o.Arh.WindowDays != arhDefaultWindow || o.Arh.MinDate != arhDefaultMinDate {
		t.Errorf("дефолты ARH: %+v", o.Arh)
	}
	o.ReadyCheck = true // msdb-проверка для ARH не применяется
	for hour, want := range map[int]bool{12: false, 13: true, 20: true} {
		got, err := sourceReady(context.Background(), nil, o, FinDWHArhDebt, hour)
		if err != nil || got != want {
			t.Errorf("hour=%d: ready=%v err=%v, ожидалось %v", hour, got, err, want)
		}
	}
	for _, tb := range FinDWHArhTables {
		if finDWHMeta[tb].Target == "" || finDWHMeta[tb].Title == "" {
			t.Errorf("нет подписей страницы статуса для %s", tb)
		}
	}
	if (ArhOpts{Tables: ArhTables{DebtFQN: "a", DocsFQN: "b", WholesalesFQN: "c"}}).Enabled() {
		t.Errorf("без списка ЮЛ поток считается ненастроенным")
	}
}
