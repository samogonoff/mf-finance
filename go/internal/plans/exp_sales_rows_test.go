package plans

import (
	"context"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

// Продажи, себестоимость, маржа и отгрузки — не расходы: по названию группы статьи.
func TestExpIsSalesGroupPL(t *testing.T) {
	for _, g := range []string{
		"ПРОДАЖИ", "ПРОДАЖИ с НДС с самовыв_", "СЕБЕСТОИМОСТЬ", "СЕБЕСТОИМОСТЬ ПО ОТПУСКНЫМ ЦЕНАМ",
		"СЕБЕСТОИМОСТЬ ПОШИВА, РАСКРОЯ, ПЕЧАТИ", "СЫРЬЕВАЯ МАРЖА", "ОПТОВО-РОЗНИЧНАЯ МАРЖА", "РОЗНИЧНАЯ МАРЖА",
		"ОТГРУЗКИ", "ОТГРУЗКИ С НДС_Orodoro", "ОПТОВАЯ МАРЖА ОТГРУЗКИ", "  продажи  ",
	} {
		if !expIsSalesGroupPL(g) {
			t.Errorf("%q должна считаться продажами/себестоимостью/маржой", g)
		}
	}
	for _, g := range []string{
		"ВЫПЛАТЫ РАБОТНИКАМ", "АРЕНДА", "ОСНОВНЫЕ РАСХОДЫ", "ПРОЧИЕ РАСХОДЫ", "ВЛОЖЕНИЯ", "АМОРТИЗАЦИЯ",
		"РЕКЛАМА И МАРКЕТИНГ", "ВСПОМОГАТЕЛЬНЫЕ РАСХОДЫ", "НАЛОГИ", "Связь и телекоммуникации",
		"Расходы на IT техника, обсл.техники", "",
	} {
		if expIsSalesGroupPL(g) {
			t.Errorf("%q — расходная группа, отсекать нельзя", g)
		}
	}
}

// Запрос к ClickHouse отсекает те же группы; у форм отделов (свои сценарии) условие не добавляется.
func TestExpCostsSQL_ExcludesSalesGroups(t *testing.T) {
	for _, kind := range []string{ExpKindRetail, ExpKindMP, ExpKindIM, ExpKindOpt} {
		q := expCostsSQL(kind, "BY", []int{2026})
		for _, w := range expSalesGroupWords {
			if !strings.Contains(q, "positionCaseInsensitiveUTF8(group_pl, '"+w+"') = 0") {
				t.Errorf("%s: в запросе нет отсечения группы %q:\n%s", kind, w, q)
			}
		}
	}
	if q := expCostsSQL(CfoExpKind(CfoExp31), "BY", []int{2026}); strings.Contains(q, "positionCaseInsensitiveUTF8(group_pl") {
		t.Errorf("у формы отдела условие по группе статьи не нужно:\n%s", q)
	}
}

// Миграция 0076: из открытых карточек уходят пустые строки продаж/маржи; расходы, введённый руками план и
// утверждённые карточки остаются.
func TestMigration0076_DropsOnlyUntouchedSalesRows_PG(t *testing.T) {
	dsn := os.Getenv("PLANS_TEST_PG_DSN")
	if dsn == "" {
		t.Skip("PLANS_TEST_PG_DSN not set")
	}
	sqlBytes, err := os.ReadFile("../../../migrations/0076_plans_exp_drop_sales_rows.up.sql")
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	pool, err := pgxpool.New(ctx, dsn)
	if err != nil {
		t.Fatal(err)
	}
	defer pool.Close()
	pl, err := NewPgStore(pool).EnsureInstance(ctx, 2093, 10)
	if err != nil {
		t.Fatal(err)
	}
	defer pool.Exec(ctx, `DELETE FROM pl_instance WHERE id=$1`, pl)
	repo := NewExpRepo(pool)
	mk := func(scope string) (Card, ExpInstance) {
		c, err := NewCardStore(pool).EnsureCard(ctx, Card{PlID: pl, FormCode: TemplateExp, ScopeKey: scope, Country: "BY", Currency: "BYN"})
		if err != nil {
			t.Fatal(err)
		}
		kind, country := expKindOf(scope)
		inst, err := repo.EnsureInstance(ctx, ExpInstance{CardID: c.ID, FormKind: kind, Country: country, PlanYear: 2093, Currency: "BYN"}, 0)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := repo.SyncRows(ctx, inst.ID, []ExpRow{
			{CodeCFO: 1, CodePL: 24, GroupPL: "ВЫПЛАТЫ РАБОТНИКАМ", Expense: "ЗП"},
			{CodeCFO: 1, CodePL: 1001, GroupPL: "ПРОДАЖИ", Expense: "Розница"},
			{CodeCFO: 1, CodePL: 5001, GroupPL: "СЫРЬЕВАЯ МАРЖА", Expense: "Маржа"},
			{CodeCFO: 1, CodePL: 2001, GroupPL: "СЕБЕСТОИМОСТЬ", Expense: "Себестоимость"},
		}); err != nil {
			t.Fatal(err)
		}
		return c, inst
	}
	defer pool.Exec(ctx, `DELETE FROM form_card WHERE pl_id=$1 AND form_code=$2`, pl, TemplateExp)
	_, openInst := mk("R-BY")
	closedCard, closedInst := mk("R-RU")
	_, touchedInst := mk("OPT")
	if _, err := pool.Exec(ctx, `UPDATE form_card SET status=$2 WHERE id=$1`, closedCard.ID, CardApproved); err != nil {
		t.Fatal(err)
	}
	// В карточке опта человек ввёл план в строке продаж — её не трогаем.
	amt := 100.0
	if _, err := repo.SaveValues(ctx, touchedInst.ID, []ExpCell{{CodeCFO: 1, CodePL: 1001, Month: 10, Amount: &amt}}, 0, 1); err != nil {
		t.Fatal(err)
	}
	// В открытой карточке РБ бюджет источника уже подставлен автоматически (source = budget) — и в строки продаж, и в
	// расходную строку. Подставленный бюджет введённым планом не считается: строки продаж с ним тоже удаляются,
	// расходная остаётся (она не из групп продаж).
	budget := func(code int) ExpCell {
		a := 357.0
		return ExpCell{CodeCFO: 1, CodePL: code, Month: 10, Amount: &a, Source: ExpSourceBudget}
	}
	if err := repo.FillPlanFromSource(ctx, openInst.ID, 10, []ExpCell{budget(24), budget(1001), budget(5001), budget(2001)}); err != nil {
		t.Fatal(err)
	}
	var autoFilled int
	if err := pool.QueryRow(ctx, `SELECT count(*) FROM exp_value v JOIN exp_row r ON r.id = v.row_id
		 WHERE r.instance_id = $1 AND v.scenario = 'plan' AND v.source = $2`, openInst.ID, ExpSourceBudget).Scan(&autoFilled); err != nil || autoFilled != 4 {
		t.Fatalf("бюджет не подставился в открытую карточку: %d ячеек, %v", autoFilled, err)
	}
	if _, err := pool.Exec(ctx, string(sqlBytes)); err != nil {
		t.Fatalf("миграция 0076: %v", err)
	}
	count := func(instID int64) (n int, codes map[int]bool) {
		rows, err := pool.Query(ctx, `SELECT code_pl FROM exp_row WHERE instance_id = $1`, instID)
		if err != nil {
			t.Fatal(err)
		}
		defer rows.Close()
		codes = map[int]bool{}
		for rows.Next() {
			var c int
			if err := rows.Scan(&c); err != nil {
				t.Fatal(err)
			}
			codes[c] = true
			n++
		}
		return
	}
	if n, codes := count(openInst.ID); n != 1 || !codes[24] {
		t.Errorf("в открытой карточке должна остаться только строка расходов (24): %v", codes)
	}
	if n, _ := count(closedInst.ID); n != 4 {
		t.Errorf("утверждённая карточка не должна меняться: строк %d", n)
	}
	if n, codes := count(touchedInst.ID); n != 2 || !codes[24] || !codes[1001] {
		t.Errorf("строка продаж с введённым планом должна остаться: %v", codes)
	}
}

// Группа статьи в форме — GroupPL_new (актуальная группировка P&L), старая GroupPL остаётся запасной.
func TestExpCostsSQL_UsesGroupPLNew(t *testing.T) {
	for _, kind := range []string{ExpKindRetail, ExpKindMP, ExpKindIM, ExpKindOpt} {
		q := expCostsSQL(kind, "BY", []int{2026})
		if !strings.Contains(q, "if(group_pl_new != '', group_pl_new, group_pl) AS group_pl") {
			t.Errorf("%s: группа статьи должна браться из group_pl_new:\n%s", kind, q)
		}
	}
	// Каталог статей запроса факта из P&L — тоже по новой группировке.
	if q := expAccrualFactSQL(ExpKindRetail, "BY", []int{2026}); !strings.Contains(q, "any(if(group_pl_new != '', group_pl_new, group_pl)) AS gp") {
		t.Errorf("каталог групп факта из P&L:\n%s", q)
	}
}

// Строки продаж, себестоимости и маржи, уже попавшие в карточку, форма расходов не показывает: ни в строках, ни в итогах групп.
func TestExpRepo_HidesSalesRowsOnRead_PG(t *testing.T) {
	dsn := os.Getenv("PLANS_TEST_PG_DSN")
	if dsn == "" {
		t.Skip("PLANS_TEST_PG_DSN not set")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	pool, err := pgxpool.New(ctx, dsn)
	if err != nil {
		t.Fatal(err)
	}
	defer pool.Close()
	pl, err := NewPgStore(pool).EnsureInstance(ctx, 2093, 9)
	if err != nil {
		t.Fatal(err)
	}
	defer pool.Exec(ctx, `DELETE FROM pl_instance WHERE id=$1`, pl)
	defer pool.Exec(ctx, `DELETE FROM form_card WHERE pl_id=$1 AND form_code=$2`, pl, TemplateExp)
	c, err := NewCardStore(pool).EnsureCard(ctx, Card{PlID: pl, FormCode: TemplateExp, ScopeKey: "IM-BY", Country: "BY", Currency: "BYN"})
	if err != nil {
		t.Fatal(err)
	}
	repo := NewExpRepo(pool)
	inst, err := repo.EnsureInstance(ctx, ExpInstance{CardID: c.ID, FormKind: ExpKindIM, Country: "BY", PlanYear: 2093, Currency: "BYN"}, 0)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := repo.SyncRows(ctx, inst.ID, []ExpRow{
		{CodeCFO: 1, CodePL: 24, GroupPL: "Заработная плата", Expense: "ЗП"},
		{CodeCFO: 1, CodePL: 1002, GroupPL: "ПРОДАЖИ", Expense: "Продажи"},
		{CodeCFO: 1, CodePL: 9002, GroupPL: "РОЗНИЧНАЯ МАРЖА", Expense: "Маржа"},
		{CodeCFO: 1, CodePL: 8002, GroupPL: "СЕБЕСТОИМОСТЬ ПО ОТПУСКНЫМ ЦЕНАМ", Expense: "Себестоимость"},
	}); err != nil {
		t.Fatal(err)
	}
	amt := 50.0
	if _, err := repo.SaveValues(ctx, inst.ID, []ExpCell{{CodeCFO: 1, CodePL: 1002, Month: 9, Amount: &amt}}, 0, 1); err != nil {
		t.Fatal(err)
	}
	// Признак «Выполняется»: сумма только в строке продаж данных расходов не даёт; сумма расхода — даёт.
	cs := NewCardStore(pool)
	if cc, err := cs.Card(ctx, c.ID); err != nil || cc.HasData {
		t.Fatalf("только продажи: has_data должен быть false: %+v %v", cc.HasData, err)
	}
	exp := 70.0
	if _, err := repo.SaveValues(ctx, inst.ID, []ExpCell{{CodeCFO: 1, CodePL: 24, Month: 9, Amount: &exp}}, 0, 1); err != nil {
		t.Fatal(err)
	}
	if cc, err := cs.Card(ctx, c.ID); err != nil || !cc.HasData {
		t.Fatalf("сумма расхода: has_data должен быть true: %v", err)
	}
	rows, total, err := repo.Rows(ctx, inst.ID, ExpFilter{})
	if err != nil {
		t.Fatal(err)
	}
	if total != 1 || len(rows) != 1 || rows[0].CodePL != 24 {
		t.Fatalf("форма расходов должна показывать только расходную строку: total=%d %+v", total, rows)
	}
	totals, err := repo.GroupTotals(ctx, inst.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(totals) != 1 {
		t.Errorf("в итогах групп только расходные группы: %v", totals)
	}
	// Список групп для фильтра - фактические группы формы без продаж и маржи.
	groups, err := repo.Groups(ctx, inst.ID)
	if err != nil || len(groups) != 1 || groups[0] != "Заработная плата" {
		t.Errorf("группы формы: %v, %v", groups, err)
	}
	// Фильтр по новой группе находит строку.
	if rows, total, err := repo.Rows(ctx, inst.ID, ExpFilter{GroupPL: "Заработная плата"}); err != nil || total != 1 || len(rows) != 1 {
		t.Errorf("фильтр по группе GroupPL_new: %d %v", total, err)
	}

	// Формы отделов (cfo:N) строки продаж и маржи загружают сознательно: у них фильтр чтения не действует.
	cfoCard, err := NewCardStore(pool).EnsureCard(ctx, Card{PlID: pl, FormCode: TemplateExp, ScopeKey: "CFO-31-BY", Country: "BY", Currency: "BYN"})
	if err != nil {
		t.Fatal(err)
	}
	cfoInst, err := repo.EnsureInstance(ctx, ExpInstance{CardID: cfoCard.ID, FormKind: CfoExpKind(CfoExp31), Country: "BY", PlanYear: 2093, Currency: "BYN"}, 0)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := repo.SyncRows(ctx, cfoInst.ID, []ExpRow{
		{CodeCFO: 31, CodePL: 24, GroupPL: "Заработная плата", Expense: "ЗП"},
		{CodeCFO: 31, CodePL: 1002, GroupPL: "ПРОДАЖИ", Expense: "Продажи"},
	}); err != nil {
		t.Fatal(err)
	}
	if _, cfoTotal, err := repo.Rows(ctx, cfoInst.ID, ExpFilter{}); err != nil || cfoTotal != 2 {
		t.Errorf("форма отдела должна показывать обе строки: %d %v", cfoTotal, err)
	}
}

// Группы формы: новые названия (GroupPL_new) идут в порядке листов Excel, остальные — по алфавиту после них.
func TestOrderGroups_NewNamesFollowExcelOrder(t *testing.T) {
	got := orderGroups([]string{"Банковские расходы", "Яблоки", "Аренда", "Заработная плата", "ВЫПЛАТЫ РАБОТНИКАМ", "Амортизация"}, ExpGroupOrder(ExpKindRetail))
	want := []string{"Заработная плата", "Аренда", "Банковские расходы", "Амортизация", "ВЫПЛАТЫ РАБОТНИКАМ", "Яблоки"}
	if strings.Join(got, "|") != strings.Join(want, "|") {
		t.Fatalf("порядок групп:\n got  %v\n want %v", got, want)
	}
}
