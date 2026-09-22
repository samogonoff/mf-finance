package plans

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"testing"
)

// Тесты песочницы приёмника (PLANS_PUBLISH_SANDBOX=1). Живая БД не нужна: путь
// записи проверяется на фейке, который ведёт себя как приёмник Budgeting —
// DELETE по логическому ключу + INSERT, чтение суммы из «таблицы», без PK.

// fakeTarget — приёмник без первичного ключа: карта логический ключ → значение.
type fakeTarget struct {
	rows    map[string]float64
	written int
	sandbox bool
	lossy   bool // приёмник «съел» последнюю строку пачки (триггер/усечение на его стороне)
	fail    error
}

func (f *fakeTarget) key(r PublishRow) string {
	return fmt.Sprintf("%s|%s|%d|%d|%s", r.Param, r.Country, r.CodeCFO, r.CodePL, r.Date.Format("2006-01"))
}

func (f *fakeTarget) Write(_ context.Context, _ string, rows []PublishRow) (int, float64, error) {
	if f.fail != nil {
		return 0, 0, f.fail
	}
	if f.rows == nil {
		f.rows = map[string]float64{}
	}
	for i, r := range rows {
		delete(f.rows, f.key(r)) // DELETE по логическому ключу
		if f.lossy && i == len(rows)-1 {
			continue // строка до приёмника не дошла — контроль сумм обязан это увидеть
		}
		f.rows[f.key(r)] = r.Value
		f.written++
	}
	sum := 0.0
	for _, r := range rows {
		sum += f.rows[f.key(r)]
	}
	return f.written, round4(sum), nil
}

func (f *fakeTarget) Probe(_ context.Context, _ string, rows []PublishRow) (float64, error) {
	sum := 0.0
	for _, r := range rows {
		sum += f.rows[f.key(r)]
	}
	return round4(sum), nil
}

func (f *fakeTarget) Available() bool { return true }
func (f *fakeTarget) Sandbox() bool   { return f.sandbox }

// fakeCardStore — карточка в памяти. Из всего интерфейса тестам нужны Card и
// MarkPublished; остальное — заглушки (публикация их не трогает).
type fakeCardStore struct {
	card    Card
	route   []RouteStep
	pubOK   bool
	pubNote string
	marked  bool
}

func (s *fakeCardStore) Card(context.Context, int64) (Card, error) { return s.card, nil }
func (s *fakeCardStore) MarkPublished(_ context.Context, _ int64, ok bool, note string) error {
	s.marked, s.pubOK, s.pubNote = true, ok, note
	return nil
}
func (s *fakeCardStore) EnsureCard(_ context.Context, c Card) (Card, error) { return c, nil }
func (s *fakeCardStore) CardByScope(context.Context, int64, string, string) (Card, error) {
	return s.card, nil
}
func (s *fakeCardStore) Cards(context.Context, int64) ([]Card, error) { return nil, nil }
func (s *fakeCardStore) SaveTransition(_ context.Context, _ int64, tr CardTransition, _ []byte, _ int64) (Card, error) {
	// Возвращаем карточку в состоянии ПОСЛЕ перехода: тесты процесса проверяют,
	// куда именно уехала карточка, а не только что вызов не упал.
	next := s.card
	if tr.Status != "" {
		next.Status = tr.Status
	}
	if tr.StepCode != "" {
		next.StepCode = tr.StepCode
	}
	next.Locked = tr.Locked
	s.card = next
	return next, nil
}
func (s *fakeCardStore) SetCalcMode(context.Context, int64, string) error           { return nil }
func (s *fakeCardStore) SetFxSnapshot(context.Context, int64, map[string]any) error { return nil }
func (s *fakeCardStore) CardApprovals(context.Context, int64) ([]CardApproval, error) {
	return nil, nil
}
func (s *fakeCardStore) CardVersions(context.Context, int64) ([]CardVersion, error) { return nil, nil }
func (s *fakeCardStore) Route(context.Context, string) ([]RouteStep, error)         { return s.route, nil }
func (s *fakeCardStore) UpsertRouteStep(context.Context, RouteStep, int64) error    { return nil }
func (s *fakeCardStore) CardVersionPayload(context.Context, int64, int) (json.RawMessage, error) {
	return json.RawMessage(`{}`), nil
}

// fakePubStore — маппинг и журнал в памяти.
type fakePubStore struct {
	mappings  []PublishMapping
	logged    []PublishResult
	keys      []string
	published map[string]bool
}

func (s *fakePubStore) Mappings(context.Context, string) ([]PublishMapping, error) {
	return s.mappings, nil
}
func (s *fakePubStore) UpsertMapping(context.Context, PublishMapping, int64) error { return nil }
func (s *fakePubStore) LogPublish(_ context.Context, r PublishResult, _ int64, _ []byte) error {
	s.logged = append(s.logged, r)
	if r.Mode == PublishModeWrite && r.Status == "ok" {
		s.keys = append(s.keys, publishKey(r))
		if s.published == nil {
			s.published = map[string]bool{}
		}
		s.published[publishKey(r)] = true
	}
	return nil
}
func (s *fakePubStore) PublishLog(context.Context, int64) ([]PublishLogItem, error) {
	return nil, nil
}
func (s *fakePubStore) AlreadyPublished(_ context.Context, key string) (bool, error) {
	return s.published[key], nil
}

// fakeSource — строки формы для публикации.
type fakeSource struct{ rows []PublishRow }

func (f fakeSource) PublishRows(context.Context, Card) ([]PublishRow, error) { return f.rows, nil }

// sandboxFixture — карточка розницы + включённое правило маппинга + приёмник.
func sandboxFixture(target *fakeTarget, rows []PublishRow) (*CardService, *fakeCardStore, *fakePubStore) {
	cards := &fakeCardStore{card: Card{
		ID: 42, FormCode: TemplateRetail, Country: "BY", CurrentVersion: 3,
		Status: CardApproved, StepCode: "1.4",
	}}
	pub := &fakePubStore{
		mappings: []PublishMapping{{
			FormCode: TemplateRetail, BlockType: "sales_plan", ParamName: "ПРОДАЖИ", CodePL: 1001,
			TargetTable: "Budgeting.dbo.VFORMTOLOADTAKTTARGET", Enabled: true,
		}},
		published: map[string]bool{},
	}
	// Резолвер ЮЛ на сиде: план пишется на реальный УНП (правки §15/§16), без
	// него режим записи блокируется — это проверяется отдельным тестом.
	svc := NewCardService(cards).WithPublisher(target, pub).
		WithLegalEntities(NewLegalEntityResolver(nil))
	svc.RegisterForm(TemplateRetail, nil, fakeSource{rows: rows})
	return svc, cards, pub
}

func sandboxRow(cfo int, month int, v float64) PublishRow {
	return PublishRow{BlockType: "sales_plan", CodeCFO: cfo, Date: pubDate(2026, month), Value: v}
}

// Прогон в песочнице обязан пройти весь путь записи и быть помечен песочницей:
// иначе «published» в карточке невозможно отличить от реальной выгрузки в Budgeting.
func TestPublish_SandboxRunIsMarkedEverywhere(t *testing.T) {
	target := &fakeTarget{sandbox: true}
	svc, cards, pub := sandboxFixture(target, []PublishRow{
		sandboxRow(100, 7, 1000), sandboxRow(101, 7, 500),
	})

	res, err := svc.Publish(context.Background(), 42, PublishModeWrite, 7)
	if err != nil {
		t.Fatalf("публикация в песочницу: %v", err)
	}
	if res.Status != "ok" || res.RowsWritten != 2 {
		t.Fatalf("ожидалась успешная запись 2 строк, получено %s/%d (%s)", res.Status, res.RowsWritten, res.Error)
	}
	if !res.Sandbox {
		t.Fatal("результат прогона в песочнице должен быть помечен sandbox=true")
	}
	if res.Diff != 0 {
		t.Fatalf("контроль записи должен сойтись в ноль, получено %.4f", res.Diff)
	}
	if !cards.marked || !cards.pubOK {
		t.Fatalf("карточка должна быть помечена опубликованной: marked=%v ok=%v", cards.marked, cards.pubOK)
	}
	if !strings.Contains(cards.pubNote, "ПЕСОЧНИЦА") {
		t.Fatalf("в листе согласования должно быть видно, что записи в Budgeting не было: %q", cards.pubNote)
	}
	if len(pub.keys) != 1 || !strings.Contains(pub.keys[0], "sandbox:") {
		t.Fatalf("ключ идемпотентности песочницы должен отличаться от прод-ключа: %+v", pub.keys)
	}
}

// Ключ прогона в песочнице НЕ должен блокировать реальную публикацию той же
// версии: иначе после проверки функционала в Budgeting не уйдёт ничего.
func TestPublishKey_SandboxDoesNotShadowRealPublish(t *testing.T) {
	real := PublishResult{CardID: 42, VersionNo: 3, Target: "Budgeting.dbo.VFORMTOLOADTAKTTARGET"}
	sandbox := real
	sandbox.Sandbox = true
	if publishKey(real) == publishKey(sandbox) {
		t.Fatalf("ключи прода и песочницы совпали: %s", publishKey(real))
	}
	if !strings.HasSuffix(publishKey(real), "Budgeting.dbo.VFORMTOLOADTAKTTARGET") {
		t.Fatalf("прод-ключ не должен меняться: %s", publishKey(real))
	}
}

// Смысл песочницы — гонять контроль записи «Σ введено − Σ записано = 0» по-настоящему.
// Если приёмник принял не все строки, прогон обязан дать failed, а не тихое «ок»
// (дубли одного логического ключа до приёмника не доезжают — их сворачивает
// foldPublishRows, см. TestApplyPublishMapping_AggregateFoldsRows).
func TestPublish_SandboxCatchesSumMismatch(t *testing.T) {
	target := &fakeTarget{sandbox: true, lossy: true}
	svc, cards, _ := sandboxFixture(target, []PublishRow{
		sandboxRow(100, 7, 1000), sandboxRow(101, 7, 500),
	})

	res, _ := svc.Publish(context.Background(), 42, PublishModeWrite, 7)
	if res.Status != "failed" {
		t.Fatalf("недописанная строка должна давать failed, получено %s (%s)", res.Status, res.Error)
	}
	if !strings.Contains(res.Error, "контроль записи не сошёлся") {
		t.Fatalf("ожидалось сообщение контроля записи, получено %q", res.Error)
	}
	if res.Diff == 0 {
		t.Fatal("расхождение должно быть отражено в отчёте")
	}
	if cards.pubOK {
		t.Fatal("карточка не должна помечаться опубликованной при несошедшемся контроле")
	}
}

// Повтор прогона той же версии не пишет второй раз: у приёмников нет PK, и
// идемпотентность — единственная защита от удвоения. Проверяем её в песочнице,
// потому что в проде проверять это нечем.
func TestPublish_SandboxIsIdempotentPerVersion(t *testing.T) {
	target := &fakeTarget{sandbox: true}
	svc, _, _ := sandboxFixture(target, []PublishRow{sandboxRow(100, 7, 1000)})

	if res, _ := svc.Publish(context.Background(), 42, PublishModeWrite, 7); res.Status != "ok" {
		t.Fatalf("первый прогон должен пройти: %s (%s)", res.Status, res.Error)
	}
	res, err := svc.Publish(context.Background(), 42, PublishModeWrite, 7)
	if err != nil {
		t.Fatalf("повтор: %v", err)
	}
	if res.RowsWritten != 0 || !strings.Contains(res.Error, "уже опубликована") {
		t.Fatalf("повтор не должен писать: rows=%d err=%q", res.RowsWritten, res.Error)
	}
	if target.written != 1 {
		t.Fatalf("в приёмник должна уйти одна строка, ушло %d", target.written)
	}
}

// Без песочницы и без Budgeting публикация остаётся предпросмотром, а в сообщении
// обязана быть подсказка, чем это включается: иначе диагноз занимает полдня.
func TestPublish_BlockedMessagePointsToSandbox(t *testing.T) {
	svc, _, _ := sandboxFixture(nil, []PublishRow{sandboxRow(100, 7, 1000)})
	svc.publisher = NewNopPublisher()

	res, err := svc.Publish(context.Background(), 42, PublishModeWrite, 7)
	if err != nil {
		t.Fatalf("недоступный приёмник не должен быть ошибкой запроса: %v", err)
	}
	if res.Status != "blocked" || res.Mode != PublishModeDryRun {
		t.Fatalf("ожидался blocked/dry_run, получено %s/%s", res.Status, res.Mode)
	}
	if !strings.Contains(res.Error, "PLANS_PUBLISH_SANDBOX") {
		t.Fatalf("сообщение должно подсказывать про песочницу: %q", res.Error)
	}
	if res.Sandbox {
		t.Fatal("заблокированный прогон не является записью в песочницу")
	}
}

// Белый список таблиц действует и в песочнице: имя приёмника приходит из данных
// маппинга, и «в тесте можно всё» — тот путь, которым дырка уезжает в прод.
func TestSandboxPublisher_TargetWhitelist(t *testing.T) {
	p := &sandboxPublisher{allowed: map[string]bool{"budgeting.dbo.vformtoloadtakttarget": true}}
	if err := p.checkTarget("Budgeting.dbo.VFORMTOLOADTAKTTARGET"); err != nil {
		t.Fatalf("разрешённая таблица отклонена: %v", err)
	}
	if err := p.checkTarget("Budgeting.dbo.Users"); err == nil {
		t.Fatal("таблица вне белого списка должна быть отклонена")
	}
	if !p.Sandbox() {
		t.Fatal("sandboxPublisher.Sandbox() должен быть true")
	}
}

// Песочница без пула недоступна — сервис обязан остаться в dry-run, а не паниковать.
func TestSandboxPublisher_UnavailableWithoutPool(t *testing.T) {
	if NewSandboxPublisher(nil, []string{"Budgeting.dbo.VFORMTOLOADTAKTTARGET"}).Available() {
		t.Fatal("без пула PG песочница не должна считаться доступной")
	}
}

// ЮЛ в приёмнике тактики (правки ТЗ 02.09.2026 §15/§16): колонка «Компания» есть
// только у TaktTarget, и заполняться она обязана РЕАЛЬНЫМ УНП.
func TestPublish_CompanyFilledWithRealUNP(t *testing.T) {
	target := &fakeTarget{sandbox: true}
	svc, _, _ := sandboxFixture(target, []PublishRow{sandboxRow(100, 7, 1000)})

	res, err := svc.Publish(context.Background(), 42, PublishModeWrite, 7)
	if err != nil || res.Status != "ok" {
		t.Fatalf("публикация: %s (%s) %v", res.Status, res.Error, err)
	}
	if len(res.Preview) == 0 || res.Preview[0].Company != "690719790" {
		t.Fatalf("в строку приёмника тактики обязан попасть УНП ЮЛ страны, got %+v", res.Preview)
	}
}

// Нет реального ЮЛ страны — запись блокируется: строка без ЮЛ в приёмнике
// неотличима от исторических строк с фиктивными кодами.
func TestPublish_BlockedWithoutLegalEntity(t *testing.T) {
	target := &fakeTarget{sandbox: true}
	cards := &fakeCardStore{card: Card{
		ID: 42, FormCode: TemplateRetail, Country: "CN", CurrentVersion: 1,
		Status: CardApproved, StepCode: "1.4",
	}}
	pub := &fakePubStore{
		mappings: []PublishMapping{{
			FormCode: TemplateRetail, BlockType: "sales_plan", ParamName: "ПРОДАЖИ", CodePL: 1001,
			TargetTable: "Budgeting.dbo.VFORMTOLOADTAKTTARGET", Enabled: true,
		}},
		published: map[string]bool{},
	}
	svc := NewCardService(cards).WithPublisher(target, pub).
		WithLegalEntities(NewLegalEntityResolver(nil))
	svc.RegisterForm(TemplateRetail, nil, fakeSource{rows: []PublishRow{sandboxRow(100, 7, 1000)}})

	res, _ := svc.Publish(context.Background(), 42, PublishModeWrite, 7)
	if res.Status != "blocked" {
		t.Fatalf("без реального ЮЛ запись должна блокироваться, got %s", res.Status)
	}
	if !strings.Contains(res.Error, "УНП") {
		t.Fatalf("в причине должно быть сказано про УНП: %q", res.Error)
	}
	if target.written != 0 {
		t.Fatal("в приёмник не должно уйти ни строки")
	}
}

// Предпросмотр отсутствие УНП НЕ блокирует: dry-run и нужен, чтобы увидеть это
// заранее — проблема уходит в отчёт, а не отменяет отчёт.
func TestPublish_DryRunReportsMissingLegalEntity(t *testing.T) {
	target := &fakeTarget{sandbox: true}
	cards := &fakeCardStore{card: Card{
		ID: 42, FormCode: TemplateRetail, Country: "CN", CurrentVersion: 1,
		Status: CardApproved, StepCode: "1.4",
	}}
	pub := &fakePubStore{
		mappings: []PublishMapping{{
			FormCode: TemplateRetail, BlockType: "sales_plan", ParamName: "ПРОДАЖИ", CodePL: 1001,
			TargetTable: "Budgeting.dbo.VFORMTOLOADTAKTTARGET", Enabled: true,
		}},
		published: map[string]bool{},
	}
	svc := NewCardService(cards).WithPublisher(target, pub).
		WithLegalEntities(NewLegalEntityResolver(nil))
	svc.RegisterForm(TemplateRetail, nil, fakeSource{rows: []PublishRow{sandboxRow(100, 7, 1000)}})

	res, err := svc.Publish(context.Background(), 42, PublishModeDryRun, 7)
	if err != nil {
		t.Fatalf("dry-run: %v", err)
	}
	if res.Status == "blocked" {
		t.Fatalf("предпросмотр не должен блокироваться из-за ЮЛ: %s", res.Error)
	}
	found := false
	for _, p := range res.Pending {
		if strings.Contains(p, "УНП") {
			found = true
		}
	}
	if !found {
		t.Fatalf("проблема с ЮЛ обязана попасть в отчёт: %+v", res.Pending)
	}
}

// Колонка «Компания» пишется ТОЛЬКО в приёмники тактики (правка §16): в плане и
// факте её нет, и попытка записать её туда уронила бы всю пачку.
func TestTargetHasCompany(t *testing.T) {
	cases := map[string]bool{
		"Budgeting.dbo.VFORMTOLOADTAKTTARGET": true,
		"Budgeting.dbo.FormToLoaTaktTarget":   true,
		"Budgeting.dbo.FormToLoadPlan":        false,
		"Budgeting.dbo.FormToLoadFact":        false,
	}
	for target, want := range cases {
		if got := targetHasCompany(target); got != want {
			t.Errorf("%s: got %v, want %v", target, got, want)
		}
	}
}

// УНП берётся из справочника, а фиктивные коды (historical_only) на запись не
// годятся — иначе план уедет на заглушку для чтения истории.
func TestLegalEntityResolver_SkipsHistoricalOnly(t *testing.T) {
	rows := []DirRow{
		{Payload: map[string]any{"country": "BY", "unp": "1", "historical_only": true}},
		{Payload: map[string]any{"country": "BY", "unp": "690719790", "historical_only": false}},
	}
	r := NewLegalEntityResolver(fakeDirRows{rows: rows})
	unp, ok := r.UNP(context.Background(), "BY")
	if !ok || unp != "690719790" {
		t.Fatalf("ожидался реальный УНП, got %q ok=%v", unp, ok)
	}
	if _, ok := r.UNP(context.Background(), "CN"); ok {
		t.Fatal("для страны без реального ЮЛ резолвер должен отвечать «не найдено»")
	}
}

// fakeDirRows — строки справочника в памяти.
type fakeDirRows struct{ rows []DirRow }

func (f fakeDirRows) Rows(context.Context, string) ([]DirRow, error) { return f.rows, nil }
