package plans

import (
	"context"
	"os"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

// Бюджет источника подставляется в пустые ячейки плана и обновляется, пока человек их не тронул; введённое
// руками (и из файла) не затирается ни при каком обновлении источника.
func TestExpRepo_FillPlanFromSource_PG(t *testing.T) {
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
	pl, err := NewPgStore(pool).EnsureInstance(ctx, 2094, 10)
	if err != nil {
		t.Fatal(err)
	}
	defer pool.Exec(ctx, `DELETE FROM pl_instance WHERE id=$1`, pl)
	card, err := NewCardStore(pool).EnsureCard(ctx, Card{PlID: pl, FormCode: TemplateExp, ScopeKey: "R-BY", Country: "BY", Currency: "BYN"})
	if err != nil {
		t.Fatal(err)
	}
	defer pool.Exec(ctx, `DELETE FROM form_card WHERE id=$1`, card.ID)
	repo := NewExpRepo(pool)
	inst, err := repo.EnsureInstance(ctx, ExpInstance{CardID: card.ID, FormKind: ExpKindRetail, Country: "BY", PlanYear: 2094, Currency: "BYN"}, 0)
	if err != nil {
		t.Fatal(err)
	}
	defer pool.Exec(ctx, `DELETE FROM exp_instance WHERE id=$1`, inst.ID)
	if _, err := repo.SyncRows(ctx, inst.ID, []ExpRow{{CodeCFO: 101, CodePL: 24, Expense: "ЗП"}, {CodeCFO: 101, CodePL: 54, Expense: "Аренда"}}); err != nil {
		t.Fatal(err)
	}
	amt := func(v float64) *float64 { return &v }
	bud := func(code, month int, v float64) ExpCell {
		return ExpCell{CodeCFO: 101, CodePL: code, Month: month, Amount: amt(v), Source: ExpSourceBudget}
	}
	plan := func(code, month int) (float64, string, bool) {
		var v float64
		var src string
		err := pool.QueryRow(ctx, `
			SELECT v.amount, v.source FROM exp_value v JOIN exp_row r ON r.id = v.row_id
			 WHERE r.instance_id = $1 AND r.code_cfo = 101 AND r.code_pl = $2 AND v.scenario = 'plan' AND v.month = $3`,
			inst.ID, code, month).Scan(&v, &src)
		return v, src, err == nil
	}

	// 1. Пустые ячейки заполняются.
	if err := repo.FillPlanFromSource(ctx, inst.ID, 10, []ExpCell{bud(24, 10, 1000), bud(24, 11, 1100), bud(54, 10, 500)}); err != nil {
		t.Fatal(err)
	}
	if v, src, ok := plan(24, 10); !ok || v != 1000 || src != ExpSourceBudget {
		t.Fatalf("октябрь не подставлен: %v %q %v", v, src, ok)
	}
	// 2. Человек правит ячейку — она становится ручной.
	if _, err := repo.SaveValues(ctx, inst.ID, []ExpCell{{CodeCFO: 101, CodePL: 24, Month: 10, Amount: amt(1234)}}, 0, 1); err != nil {
		t.Fatal(err)
	}
	// 3. Источник обновился: «бюджетные» ячейки следуют за ним, ручная — нет.
	if err := repo.FillPlanFromSource(ctx, inst.ID, 10, []ExpCell{bud(24, 10, 2000), bud(24, 11, 2100), bud(54, 10, 600)}); err != nil {
		t.Fatal(err)
	}
	if v, src, _ := plan(24, 10); v != 1234 || src != ExpSourceManual {
		t.Errorf("введённое руками затёрто источником: %v %q", v, src)
	}
	if v, src, _ := plan(24, 11); v != 2100 || src != ExpSourceBudget {
		t.Errorf("нетронутая «бюджетная» ячейка должна обновиться по источнику: %v %q", v, src)
	}
	if v, _, _ := plan(54, 10); v != 600 {
		t.Errorf("аренда за октябрь: %v", v)
	}
	// 4. Ячейка без суммы не создаётся.
	if err := repo.FillPlanFromSource(ctx, inst.ID, 10, []ExpCell{bud(24, 11, 2100), bud(54, 10, 600), {CodeCFO: 101, CodePL: 24, Month: 12}}); err != nil {
		t.Fatal(err)
	}
	if _, _, ok := plan(24, 12); ok {
		t.Error("ячейка без суммы не должна создаваться")
	}
	// 5. Бюджет по ключу исчез из источника (24/ноябрь не пришёл): нетронутая «бюджетная» ячейка удаляется,
	// введённое руками (24/октябрь) и пришедшие снова ячейки остаются.
	if err := repo.FillPlanFromSource(ctx, inst.ID, 10, []ExpCell{bud(54, 10, 600)}); err != nil {
		t.Fatal(err)
	}
	if _, _, ok := plan(24, 11); ok {
		t.Error("исчезнувший из источника бюджет должен быть удалён из плана")
	}
	if v, src, ok := plan(24, 10); !ok || v != 1234 || src != ExpSourceManual {
		t.Errorf("введённое руками удалено вместе с исчезнувшим бюджетом: %v %q %v", v, src, ok)
	}
	if v, _, ok := plan(54, 10); !ok || v != 600 {
		t.Errorf("пришедший бюджет должен остаться: %v %v", v, ok)
	}
	// Месяцы раньше fromMonth (закрыты) удаление не затрагивает: карточка ноябрьская, октябрь не трогаем.
	if err := repo.FillPlanFromSource(ctx, inst.ID, 11, nil); err != nil {
		t.Fatal(err)
	}
	if _, _, ok := plan(54, 10); !ok {
		t.Error("месяц раньше fromMonth не должен удаляться")
	}
	// 5б. Дубли ключа в одной выдаче (одно и то же ЦФО/статья/месяц дважды) не роняют запись: побеждает последнее.
	if err := repo.FillPlanFromSource(ctx, inst.ID, 10, []ExpCell{bud(54, 10, 600), bud(24, 12, 1), bud(24, 12, 2)}); err != nil {
		t.Fatalf("дубли ключа в выдаче: %v", err)
	}
	if v, _, ok := plan(24, 12); !ok || v != 2 {
		t.Errorf("при дубле ключа должно победить последнее значение: %v %v", v, ok)
	}
	// 5в. Много ячеек одним запросом (как у розницы: тысячи пар × месяцы).
	rows := make([]ExpRow, 0, 300)
	for i := 0; i < 300; i++ {
		rows = append(rows, ExpRow{CodeCFO: 1000 + i, CodePL: 24, Expense: "ЗП"})
	}
	if _, err := repo.SyncRows(ctx, inst.ID, rows); err != nil {
		t.Fatal(err)
	}
	var many []ExpCell
	for _, r := range rows {
		for m := 10; m <= 12; m++ {
			many = append(many, ExpCell{CodeCFO: r.CodeCFO, CodePL: 24, Month: m, Amount: amt(float64(m)), Source: ExpSourceBudget})
		}
	}
	many = append(many, bud(54, 10, 600), bud(24, 12, 2))
	if err := repo.FillPlanFromSource(ctx, inst.ID, 10, many); err != nil {
		t.Fatal(err)
	}
	var n int
	if err := pool.QueryRow(ctx, `SELECT count(*) FROM exp_value v JOIN exp_row r ON r.id = v.row_id
		 WHERE r.instance_id = $1 AND v.scenario = 'plan' AND v.source = $2 AND r.code_cfo >= 1000`, inst.ID, ExpSourceBudget).Scan(&n); err != nil || n != 900 {
		t.Fatalf("подставлено ячеек %d (ожидали 900): %v", n, err)
	}
	// 6. Карточку отправили на согласование, пока шла загрузка: план не меняется — ни подстановки, ни удаления.
	if _, err := pool.Exec(ctx, `UPDATE form_card SET status = $2 WHERE id = $1`, card.ID, CardOnApproval); err != nil {
		t.Fatal(err)
	}
	if err := repo.FillPlanFromSource(ctx, inst.ID, 10, []ExpCell{bud(54, 10, 7777), bud(54, 11, 5)}); err != nil {
		t.Fatal(err)
	}
	if v, _, _ := plan(54, 10); v != 600 {
		t.Errorf("в карточке на согласовании бюджет перезаписан: %v", v)
	}
	if _, _, ok := plan(54, 11); ok {
		t.Error("в карточку на согласовании подставлена новая ячейка")
	}
	if err := repo.FillPlanFromSource(ctx, inst.ID, 10, nil); err != nil {
		t.Fatal(err)
	}
	if _, _, ok := plan(54, 10); !ok {
		t.Error("из карточки на согласовании удалён бюджет")
	}
}