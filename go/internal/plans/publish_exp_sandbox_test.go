package plans

import (
	"context"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

const expTargetBYN = "Budgeting.dbo.FormToLoadPlan"

func expSandboxFixture(target *fakeTarget, rows []PublishRow) (*CardService, *fakeCardStore, *fakePubStore) {
	cards := &fakeCardStore{card: Card{
		ID: 7, FormCode: TemplateExp, ScopeKey: "R-BY", Country: "BY", CurrentVersion: 2,
		Status: CardApproved, StepCode: "1.4",
	}}
	pub := &fakePubStore{
		// Как в БД после миграции 0075: включено правило BYN-пары, национальное выключено — оба с одним ключом.
		mappings: []PublishMapping{
			{FormCode: TemplateExp, BlockType: "exp", ParamName: "ЗАТРАТЫ", TargetTable: expTargetBYN, TargetCurrency: "BYN", Enabled: true},
			{FormCode: TemplateExp, BlockType: "exp", ParamName: "ЗАТРАТЫ", TargetTable: "Budgeting.dbo.VFORMTOLOADPLAN", Enabled: false},
		},
		published: map[string]bool{},
	}
	svc := NewCardService(cards).WithPublisher(target, pub).WithLegalEntities(NewLegalEntityResolver(nil))
	svc.RegisterForm(TemplateExp, nil, fakeSource{rows: rows})
	return svc, cards, pub
}

func expPubRow(cfo, pl, month int, v float64) PublishRow {
	return PublishRow{BlockType: "exp", Param: expPublishParam, Country: "BY", CodeCFO: cfo, CodePL: pl, Date: pubDate(2026, month), Value: v}
}

// Расходы публикуются в песочницу: включённое правило, запись, контроль «введено = записано», пометка песочницы.
func TestPublishExp_SandboxWrites(t *testing.T) {
	target := &fakeTarget{sandbox: true}
	svc, cards, _ := expSandboxFixture(target, []PublishRow{expPubRow(101, 24, 10, 1000), expPubRow(101, 54, 10, 500)})
	res, err := svc.Publish(context.Background(), 7, PublishModeWrite, 1)
	if err != nil {
		t.Fatal(err)
	}
	if res.Status != "ok" || res.RowsWritten != 2 || !res.Sandbox || res.Target != expTargetBYN {
		t.Fatalf("публикация расходов в песочницу: %+v", res)
	}
	if res.Diff != 0 {
		t.Errorf("контроль записи не сошёлся: %v", res.Diff)
	}
	if !cards.marked || !cards.pubOK || !strings.Contains(cards.pubNote, "ПЕСОЧНИЦА") {
		t.Errorf("карточка должна быть помечена опубликованной в песочнице: %+v", cards)
	}
}

// Боевой публикатор расходы не принимает ни при каких настройках: blocked (а не failed), записи нет, карточка
// не помечается — иначе утверждение на проде закончилось бы «публикацией с ошибкой».
func TestPublishExp_RealPublisherIsBlocked(t *testing.T) {
	target := &fakeTarget{sandbox: false}
	svc, cards, pub := expSandboxFixture(target, []PublishRow{expPubRow(101, 24, 10, 1000)})
	for _, mode := range []string{PublishModeWrite, PublishModeDryRun} {
		res, err := svc.Publish(context.Background(), 7, mode, 1)
		if err != nil {
			t.Fatal(err)
		}
		if res.Status != "blocked" || !strings.Contains(res.Error, "только в песочницу") {
			t.Fatalf("режим %s: расходы в боевой приёмник должны блокироваться: %+v", mode, res)
		}
	}
	if target.written != 0 || len(target.rows) != 0 {
		t.Errorf("в боевой приёмник что-то записано: %+v", target.rows)
	}
	if cards.marked {
		t.Error("карточка не должна помечаться опубликованной/неудачной")
	}
	if len(pub.keys) != 0 {
		t.Errorf("ключ идемпотентности не должен записываться: %v", pub.keys)
	}
}

// Включённое правило выбирается независимо от порядка строк: раньше побеждало «последнее по порядку из БД», и при
// выключенном втором правиле публикация блокировалась как «не подтверждено».
func TestPickMapping_PrefersEnabled(t *testing.T) {
	on := PublishMapping{BlockType: "exp", ParamName: "A", TargetTable: "T1", Enabled: true}
	off := PublishMapping{BlockType: "exp", ParamName: "B", TargetTable: "T2", Enabled: false}
	for name, list := range map[string][]PublishMapping{"включённое первым": {on, off}, "включённое последним": {off, on}} {
		m, ok := pickMapping(list, "exp", "BY")
		if !ok || !m.Enabled || m.TargetTable != "T1" {
			t.Errorf("%s: выбрано %+v", name, m)
		}
	}
	// Общее правило (без блока) — так же.
	gOn := PublishMapping{ParamName: "A", TargetTable: "G1", Enabled: true}
	gOff := PublishMapping{ParamName: "B", TargetTable: "G2"}
	if m, _ := pickMapping([]PublishMapping{gOn, gOff}, "x", "BY"); m.TargetTable != "G1" {
		t.Errorf("общее правило: %+v", m)
	}
	// Правило страны по-прежнему важнее правила блока.
	byCountry := PublishMapping{BlockType: "exp", Country: "BY", TargetTable: "TC"}
	if m, _ := pickMapping([]PublishMapping{on, byCountry}, "exp", "BY"); m.TargetTable != "TC" {
		t.Errorf("правило страны должно быть важнее: %+v", m)
	}
}

// Песочница принимает приёмники расходов всегда (без правки конфига), боевой публикатор — нет.
func TestPublishSandboxOnlyTargets(t *testing.T) {
	for _, tg := range publishSandboxOnlyTargets {
		if !isSandboxOnlyTarget(tg) || !isSandboxOnlyTarget(strings.ToUpper(" "+tg+" ")) {
			t.Errorf("%s должен считаться приёмником только для песочницы", tg)
		}
		sb := NewSandboxPublisher(nil, []string{"Budgeting.dbo.VFORMTOLOADTAKTTARGET"}).(*sandboxPublisher)
		if err := sb.checkTarget(tg); err != nil {
			t.Errorf("песочница должна принимать %s: %v", tg, err)
		}
	}
	if isSandboxOnlyTarget("Budgeting.dbo.VFORMTOLOADTAKTTARGET") {
		t.Error("приёмник продаж не должен быть «только песочница»")
	}
	real := NewMssqlPublisher(nil, []string{"Budgeting.dbo.VFORMTOLOADTAKTTARGET"}).(*mssqlPublisher)
	if err := real.checkTarget(expTargetBYN); err == nil {
		t.Error("боевой публикатор не должен принимать приёмник расходов")
	}
}

// Реальные правила из БД после миграции 0075: ровно одно включённое правило расходов — BYN-пара, и именно его выбирает
// публикация. Сквозной прогон в песочницу PG (publish_sandbox).
func TestPublishExp_MigrationRuleAndPGSandbox_PG(t *testing.T) {
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
	maps, err := NewPublishStore(pool).Mappings(ctx, TemplateExp)
	if err != nil {
		t.Fatal(err)
	}
	var enabled []string
	for _, m := range maps {
		if m.BlockType == "exp" && m.Enabled {
			enabled = append(enabled, m.TargetTable)
		}
	}
	if len(enabled) != 1 || enabled[0] != expTargetBYN {
		t.Fatalf("включено правил расходов: %v (ожидали только %s)", enabled, expTargetBYN)
	}
	m, ok := pickMapping(maps, "exp", "BY")
	if !ok || !m.Enabled || m.TargetTable != expTargetBYN {
		t.Fatalf("публикация выбрала правило %+v", m)
	}

	rows := []PublishRow{expPubRow(101, 24, 10, 1000), expPubRow(101, 54, 10, 500)}
	rows[0].Company, rows[1].Company = "100000000", "100000000"
	pub := NewSandboxPublisher(pool, nil) // белый список конфига пуст: расходы разрешены песочнице сами
	mapped, rep := applyPublishMapping(Card{Country: "BY"}, rows, maps)
	if len(mapped) != 2 || len(rep.pending) != 0 {
		t.Fatalf("маппинг расходов: %d строк, ожидают подтверждения: %v", len(mapped), rep.pending)
	}
	written, sum, err := pub.Write(ctx, mapped[0].TargetTable, mapped)
	if err != nil {
		t.Fatalf("запись в песочницу: %v", err)
	}
	defer pool.Exec(ctx, `DELETE FROM publish_sandbox WHERE target_table = $1 AND param = $2 AND country = 'BY' AND code_cfo = '101' AND plan_date = $3`,
		expTargetBYN, expPublishParam, pubDate(2026, 10))
	if written != 2 || sum != 1500 {
		t.Errorf("записано %d строк на сумму %v, ожидали 2 / 1500", written, sum)
	}
}
