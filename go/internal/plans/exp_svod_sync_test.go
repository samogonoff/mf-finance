package plans

import (
	"context"
	"errors"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

// Период августа 2090 года: карточки созданы, но ни одну форму не открывали (строк и факта в БД нет).
func svodSyncFixture(t *testing.T, src ExpDataSource) (*ExpService, *CardService, *pgxpool.Pool, int64, context.Context) {
	t.Helper()
	dsn := os.Getenv("PLANS_TEST_PG_DSN")
	if dsn == "" {
		t.Skip("PLANS_TEST_PG_DSN not set")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	t.Cleanup(cancel)
	pool, err := pgxpool.New(ctx, dsn)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(pool.Close)
	pl, err := NewPgStore(pool).EnsureInstance(ctx, 2090, 8)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		bg := context.Background()
		pool.Exec(bg, `DELETE FROM form_card WHERE pl_id=$1 AND form_code=$2`, pl, TemplateExp)
		pool.Exec(bg, `DELETE FROM pl_instance WHERE id=$1`, pl)
	})
	cardsSvc := NewCardService(NewCardStore(pool))
	if _, err := cardsSvc.EnsureCards(ctx, pl, 0); err != nil {
		t.Fatal(err)
	}
	svc := NewExpService(NewExpRepo(pool), cardsSvc, src)
	svc.syncWait = 100 * time.Millisecond
	svc.svodWait = 3 * time.Second
	return svc, cardsSvc, pool, pl, ctx
}

func svodCells() []ExpCostCell {
	c := func(scenario string, year, month int, amt float64) ExpCostCell {
		return ExpCostCell{Scenario: scenario, Year: year, Month: month, CodePL: 24, CodeCFO: 101, Amount: amt,
			Country: "BY", GroupPL: "ВЫПЛАТЫ РАБОТНИКАМ", Expense: "ЗП", CFO: "Магазин"}
	}
	return []ExpCostCell{
		c(ExpSrcFact, 2089, 8, 100), // август года назад
		c(ExpSrcFact, 2090, 7, 200), // прошлый месяц
		c(ExpSrcFact, 2090, 8, 300), // текущий месяц периода
		c(ExpSrcTactic, 2090, 8, 400),
		c(ExpSrcTactic, 2090, 9, 500),
	}
}

// Свод за период, форму которого никто не открывал, сам запускает загрузку из источника и показывает данные:
// месяц периода — август: факт авг прошлого года, июль и август текущего, бюджет августа в плане.
func TestSvodSync_PeriodWithoutOpenedForms_PG(t *testing.T) {
	src := &stubExpSource{cells: svodCells()}
	svc, _, _, pl, ctx := svodSyncFixture(t, src)

	out, err := svc.RetailConsolidated(ctx, Principal{UserID: 1, PlansAdmin: true}, pl, ExpWindowMonth)
	if err != nil {
		t.Fatal(err)
	}
	if out.Syncing {
		t.Error("загрузка из ClickHouse/стаба должна успеть за время ожидания")
	}
	if src.callCount() == 0 {
		t.Fatal("свод не запустил загрузку из источника")
	}
	if !near(out.Total.FactLY, 100) || !near(out.Total.FactPrev, 200) || !near(out.Total.FactCur, 300) {
		t.Errorf("колонки факта за август: авг прошлого года %v, июль %v, август %v (ожидали 100 / 200 / 300)",
			out.Total.FactLY, out.Total.FactPrev, out.Total.FactCur)
	}
	if !near(out.Total.Plan["BY"], 400) {
		t.Errorf("бюджет августа в плане: %v (ожидали 400)", out.Total.Plan["BY"])
	}
	if len(out.Groups) == 0 {
		t.Error("свод пуст, хотя источник отдал строки")
	}
	// Окно «до конца года»: бюджет августа и сентября.
	tail, err := svc.RetailConsolidated(ctx, Principal{UserID: 1, PlansAdmin: true}, pl, ExpWindowTail)
	if err != nil {
		t.Fatal(err)
	}
	if !near(tail.Total.Plan["BY"], 900) {
		t.Errorf("окно август–декабрь: план %v (ожидали 400 + 500)", tail.Total.Plan["BY"])
	}
}

// Загрузка идёт дольше ожидания свода: свод отвечает сразу с признаком syncing, а повторный запрос после окончания
// загрузки возвращает данные.
func TestSvodSync_StillRunningThenDone_PG(t *testing.T) {
	release := make(chan struct{})
	src := &stubExpSource{cells: svodCells(), release: release}
	svc, _, _, pl, ctx := svodSyncFixture(t, src)
	svc.svodWait = 400 * time.Millisecond

	started := time.Now()
	out, err := svc.RetailConsolidated(ctx, Principal{UserID: 1, PlansAdmin: true}, pl, ExpWindowMonth)
	if err != nil {
		t.Fatal(err)
	}
	if time.Since(started) > 5*time.Second {
		t.Errorf("свод не должен ждать загрузку дольше ограничения: %v", time.Since(started))
	}
	if !out.Syncing {
		t.Fatalf("загрузка ещё идёт — должен быть признак syncing: %+v", out.Warnings)
	}
	close(release)
	deadline := time.Now().Add(5 * time.Second)
	for anySyncRunning(svc, svodItemsFor(t, svc, ctx, pl)) && time.Now().Before(deadline) {
		time.Sleep(50 * time.Millisecond)
	}
	out, err = svc.RetailConsolidated(ctx, Principal{UserID: 1, PlansAdmin: true}, pl, ExpWindowMonth)
	if err != nil {
		t.Fatal(err)
	}
	if out.Syncing || !near(out.Total.FactCur, 300) {
		t.Errorf("после окончания загрузки: syncing=%v, факт месяца %v", out.Syncing, out.Total.FactCur)
	}
}

// Сбой загрузки не прячется: свод показывает то, что есть, и предупреждение с причиной.
func TestSvodSync_SourceError_PG(t *testing.T) {
	src := &stubExpSource{err: errors.New("dial tcp: connection refused")}
	svc, _, _, pl, ctx := svodSyncFixture(t, src)
	out, err := svc.RetailConsolidated(ctx, Principal{UserID: 1, PlansAdmin: true}, pl, ExpWindowMonth)
	if err != nil {
		t.Fatal(err)
	}
	joined := strings.Join(out.Warnings, " | ")
	if !strings.Contains(joined, "загрузка из источника не удалась") || !strings.Contains(joined, "нет соединения с источником") {
		t.Errorf("предупреждение о сбое загрузки: %v", out.Warnings)
	}
	if strings.Contains(joined, "dial tcp") {
		t.Errorf("текст драйвера не должен попадать на экран: %v", out.Warnings)
	}
}

// Закрытая карточка не загружается (утверждённая остаётся как согласована), а пустой свод по ней объяснён.
func TestSvodSync_ClosedCardNotLoaded_PG(t *testing.T) {
	src := &stubExpSource{cells: svodCells()}
	svc, _, pool, pl, ctx := svodSyncFixture(t, src)
	if _, err := pool.Exec(ctx, `UPDATE form_card SET status=$3 WHERE pl_id=$1 AND form_code=$2 AND scope_key='IM-BY'`, pl, TemplateExp, CardApproved); err != nil {
		t.Fatal(err)
	}
	out, err := svc.ChannelConsolidated(ctx, Principal{UserID: 1, PlansAdmin: true}, pl, ExpChannelIM, ExpWindowMonth)
	if err != nil {
		t.Fatal(err)
	}
	if src.callCount() != 0 {
		t.Errorf("в утверждённую карточку загрузка не запускается: вызовов источника %d", src.callCount())
	}
	if len(out.Warnings) != 1 || !strings.Contains(out.Warnings[0], "карточка закрыта") {
		t.Errorf("пустой свод закрытой карточки должен быть объяснён: %v", out.Warnings)
	}
}

// Общий свод переносит признак загрузки и пояснения каналов.
func TestSvodSync_AllChannelsCarriesSyncAndWarnings_PG(t *testing.T) {
	release := make(chan struct{})
	src := &stubExpSource{cells: svodCells(), release: release}
	svc, _, _, pl, ctx := svodSyncFixture(t, src)
	svc.svodWait = 300 * time.Millisecond
	defer close(release)

	out, err := svc.ChannelConsolidated(ctx, Principal{UserID: 1, PlansAdmin: true}, pl, ExpChannelAll, ExpWindowMonth)
	if err != nil {
		t.Fatal(err)
	}
	if !out.Syncing {
		t.Errorf("общий свод должен показать, что загрузка ещё идёт")
	}
}

// svodItemsFor — экземпляры карточек периода, чтобы дождаться окончания фоновой загрузки.
func svodItemsFor(t *testing.T, svc *ExpService, ctx context.Context, pl int64) []svodLoad {
	t.Helper()
	cards, err := svc.cards.Cards(ctx, pl)
	if err != nil {
		t.Fatal(err)
	}
	var items []svodLoad
	for _, c := range cards {
		if c.FormCode != TemplateExp {
			continue
		}
		in, err := svc.repo.Instance(ctx, c.ID)
		if err != nil {
			continue // экземпляра ещё нет — загрузка по этой карточке не запускалась
		}
		items = append(items, svodLoad{Card: c, Inst: in})
	}
	return items
}

// anySyncRunning — по какой-то из карточек загрузка ещё идёт.
func anySyncRunning(svc *ExpService, items []svodLoad) bool {
	for _, it := range items {
		if svc.syncStatus(it.Inst.ID).State == ExpSyncRunning {
			return true
		}
	}
	return false
}

// Общий свод на медленном источнике: каналы читаются параллельно и делят один дедлайн, поэтому ответ приходит за
// одно ожидание (а не три подряд) и с признаком syncing — раньше три ожидания складывались и не укладывались в
// WriteTimeout сервера.
func TestSvodSync_AllChannelsShareOneDeadline_PG(t *testing.T) {
	release := make(chan struct{})
	src := &stubExpSource{cells: svodCells(), release: release}
	svc, _, _, pl, ctx := svodSyncFixture(t, src)
	svc.svodWait = 600 * time.Millisecond
	defer close(release)

	started := time.Now()
	out, err := svc.ChannelConsolidated(ctx, Principal{UserID: 1, PlansAdmin: true}, pl, ExpChannelAll, ExpWindowMonth)
	took := time.Since(started)
	if err != nil {
		t.Fatal(err)
	}
	if !out.Syncing {
		t.Error("загрузка ещё идёт — общий свод должен отдать syncing")
	}
	// Один дедлайн 0,6 с на все три канала; три последовательных ожидания дали бы ≥ 1,8 с.
	if took > 1500*time.Millisecond {
		t.Errorf("ожидания каналов сложились: общий свод ответил за %v (дедлайн %v)", took, svc.svodWait)
	}
}

// Клиент ушёл (контекст отменён): свод не висит до дедлайна, а возвращается сразу.
func TestSvodSync_CancelledRequestDoesNotWait_PG(t *testing.T) {
	release := make(chan struct{})
	src := &stubExpSource{cells: svodCells(), release: release}
	svc, _, _, pl, ctx := svodSyncFixture(t, src)
	svc.svodWait = 10 * time.Second
	defer close(release)

	cctx, cancel := context.WithCancel(ctx)
	go func() {
		time.Sleep(200 * time.Millisecond)
		cancel()
	}()
	started := time.Now()
	if _, err := svc.RetailConsolidated(cctx, Principal{UserID: 1, PlansAdmin: true}, pl, ExpWindowMonth); err != nil {
		// Чтение из БД с отменённым контекстом может вернуть ошибку — важно, что вернулось быстро.
		t.Logf("ошибка при отменённом контексте: %v", err)
	}
	if took := time.Since(started); took > 3*time.Second {
		t.Errorf("отменённый запрос ждал загрузку %v", took)
	}
}

// Статус прошлой загрузки (ошибка) не показывается по карточке, которую уже отправили на согласование: свод её не
// загружает, а повторить загрузку нельзя.
func TestSvodSync_ClosedCardStaleErrorNotShown_PG(t *testing.T) {
	src := &stubExpSource{err: errors.New("dial tcp: connection refused")}
	svc, _, pool, pl, ctx := svodSyncFixture(t, src)
	admin := Principal{UserID: 1, PlansAdmin: true}
	first, err := svc.ChannelConsolidated(ctx, admin, pl, ExpChannelIM, ExpWindowMonth)
	if err != nil || len(first.Warnings) == 0 {
		t.Fatalf("черновик: ждали предупреждение о сбое загрузки: %v / %v", first.Warnings, err)
	}
	if _, err := pool.Exec(ctx, `UPDATE form_card SET status=$3 WHERE pl_id=$1 AND form_code=$2 AND scope_key='IM-BY'`, pl, TemplateExp, CardOnApproval); err != nil {
		t.Fatal(err)
	}
	second, err := svc.ChannelConsolidated(ctx, admin, pl, ExpChannelIM, ExpWindowMonth)
	if err != nil {
		t.Fatal(err)
	}
	for _, w := range second.Warnings {
		if strings.Contains(w, "загрузка из источника не удалась") {
			t.Errorf("по закрытой карточке не должно быть предупреждения о сбое прошлой загрузки: %v", second.Warnings)
		}
	}
}