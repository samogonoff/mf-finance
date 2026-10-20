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
