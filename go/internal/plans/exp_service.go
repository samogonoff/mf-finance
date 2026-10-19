package plans

import (
	"context"
	"errors"
	"fmt"
	"log"
	"strings"
	"sync"
	"time"
)

// Сценарии формы 1.5 «Расходы центров прибыли».
//
// Сервис знает три вещи, которых не знают ни хранилище, ни источник:
// какой вид формы у карточки, когда пересобирать состав строк из источника и
// можно ли вообще писать в период. Права по карточке — общие для комплекта
// (оболочка процесса), поэтому проверяются здесь же, а не в ручке.

// ExpService — сервис формы 1.5.
type ExpService struct {
	repo   ExpRepo
	cards  *CardService
	source ExpDataSource
	// factTTL — как часто перечитывать факт из источника. Факт меняется раз в
	// сутки, а процедура затрат тяжёлая: дёргать её на каждое открытие формы
	// нельзя, иначе форма будет открываться минутами.
	//
	// Сервис живёт один на процесс и вызывается из обработчиков параллельно,
	// поэтому отметки под мьютексом: одновременная запись в map — это паника
	// рантайма, а не «редкая гонка».
	factTTL  time.Duration
	factMu   sync.Mutex
	factSeen map[int64]time.Time
	// syncState — состояние фоновой загрузки по экземпляру; syncWait — сколько
	// Form готов ждать её окончания, прежде чем отдать форму как есть (быстрые
	// источники и фикстуры успевают, процедура на OLAP — нет).
	syncMu    sync.Mutex
	syncState map[int64]*expSyncRun
	syncWait  time.Duration
	// reg — реестры формы (ТЗ §5). nil — форма работает без них, как до Ф4.
	reg ExpRegRepo
	// norms — справочники командировок: суточные, нормативы перелёта, курс.
	norms ExpTravelNorms
	// sales — утверждённый план продаж: вход расчёта §6.1, только чтение.
	sales ExpDataSource
}

// NewExpService — конструктор.
func NewExpService(repo ExpRepo, cards *CardService, source ExpDataSource) *ExpService {
	return &ExpService{repo: repo, cards: cards, source: source,
		factTTL: 6 * time.Hour, factSeen: map[int64]time.Time{},
		syncState: map[int64]*expSyncRun{}, syncWait: 3 * time.Second}
}

// expKindOf — вид формы по области карточки. Область задаётся реестром форм
// (form_registry.go) в виде "R-BY" / "MP-RU": префикс — вид, суффикс — страна.
func expKindOf(scopeKey string) (kind, country string) {
	if code, parsedCountry, ok := ParseCfoExpScope(scopeKey); ok {
		return CfoExpKind(code), parsedCountry
	}
	parts := strings.SplitN(scopeKey, "-", 2)
	if len(parts) != 2 {
		return ExpKindRetail, ""
	}
	if strings.EqualFold(parts[0], "MP") {
		return ExpKindMP, strings.ToUpper(parts[1])
	}
	return ExpKindRetail, strings.ToUpper(parts[1])
}

// ExpPlanYear — плановый год формы. Форма 1.5 заполняется на год целиком
// (ТЗ §1.3), а карточка живёт в периоде — год берём из неё.
func ExpPlanYear(c Card) int { return c.Year }

// access — карточка формы плюс проверка, что она вообще формы 1.5.
func (s *ExpService) access(ctx context.Context, cardID int64) (Card, ExpInstance, error) {
	c, err := s.cards.Card(ctx, cardID)
	if err != nil {
		return Card{}, ExpInstance{}, err
	}
	if c.FormCode != TemplateExp {
		return Card{}, ExpInstance{}, fmt.Errorf("карточка %d не относится к форме расходов ЦП", cardID)
	}
	kind, country := expKindOf(c.ScopeKey)
	in, err := s.repo.EnsureInstance(ctx, ExpInstance{
		CardID: c.ID, FormKind: kind, Country: country, PlanYear: ExpPlanYear(c),
		Currency: c.Currency, LegalEntity: c.LegalEntity,
	}, c.CreatedBy)
	if err != nil {
		return Card{}, ExpInstance{}, err
	}
	return c, in, nil
}

// Form — форма порцией: состав строк, значения плана и факта, итоги групп.
func (s *ExpService) Form(ctx context.Context, p Principal, cardID int64, f ExpFilter) (ExpForm, error) {
	c, in, err := s.access(ctx, cardID)
	if err != nil {
		return ExpForm{}, err
	}
	// Состав строк подтягиваем из источника, пока карточка редактируема. На
	// закрытой карточке состав не трогаем: утверждённая форма обязана остаться
	// той, какой её согласовали (ТЗ §1.1, снимок версии).
	if cardEditable(c) {
		// Загрузка идёт в фоне: процедура затрат работает минуты и дольше таймаута
		// веб-запроса. Недоступный источник не закрывает форму: введённое уже лежит в
		// БД, а причина видна в Sync.
		s.ensureSync(c, in, false)
	}
	rows, total, err := s.repo.Rows(ctx, in.ID, f)
	if err != nil {
		return ExpForm{}, err
	}
	groups := s.groupOrder(ctx, in.FormKind)
	SortExpRowsBy(groups, rows)
	return ExpForm{
		Instance: in, Card: c, Rows: rows, Total: total, Offset: f.Offset,
		Groups: groups, ReadOnly: !cardEditable(c), Sync: s.syncStatus(in.ID),
	}, nil
}

// expSyncRun — одна фоновая загрузка; done закрывается по её окончании.
type expSyncRun struct {
	status ExpSyncStatus
	done   chan struct{}
}

// expSyncRetryAfter — после ошибки автоматически не повторяем: открытие формы и
// опрос состояния не должны запускать четырёхминутную выгрузку снова и снова.
// Повторить сразу можно кнопкой «Обновить из источника».
const expSyncRetryAfter = 10 * time.Minute

// ensureSync запускает загрузку из источника в фоне (не больше одной на экземпляр)
// и ждёт её не дольше syncWait. force — запуск по кнопке: игнорирует TTL и паузу
// после ошибки.
func (s *ExpService) ensureSync(c Card, in ExpInstance, force bool) {
	if s.source == nil {
		return
	}
	s.syncMu.Lock()
	run := s.syncState[in.ID]
	if run != nil && run.status.State == ExpSyncRunning {
		s.syncMu.Unlock()
		s.waitSync(run)
		return
	}
	if !force {
		if s.factFresh(in.ID) {
			s.syncMu.Unlock()
			return
		}
		if run != nil && run.status.State == ExpSyncError && run.status.FinishedAt != nil &&
			time.Since(*run.status.FinishedAt) < expSyncRetryAfter {
			s.syncMu.Unlock()
			return
		}
	}
	now := time.Now()
	run = &expSyncRun{status: ExpSyncStatus{State: ExpSyncRunning, StartedAt: &now}, done: make(chan struct{})}
	s.syncState[in.ID] = run
	s.syncMu.Unlock()

	go func() {
		// Контекст запроса здесь не годится: он умирает вместе с ответом, а загрузка
		// должна дойти до конца и лечь в БД.
		// Таймаут самой выгрузки отсчитывается внутри источника после получения очереди
		// (expProcSem); здесь — только предохранитель от вечного зависания.
		ctx, cancel := context.WithTimeout(context.Background(), 3*time.Hour)
		defer cancel()
		// Кнопка «Обновить из источника»: сначала перезалить ночной снимок (минуты),
		// затем прочитать его. Без снимка (источник напрямую) шаг пропускается.
		// Не удалась перезаливка (миграция ClickHouse не применена, нет EXEC, сбой MSSQL) —
		// не повод оставить форму без данных: пишем в журнал и читаем текущий снимок или,
		// если его нет, процедуру напрямую.
		if r, ok := s.source.(ExpSourceRefresher); ok && force {
			if rerr := r.RefreshSource(ctx); rerr != nil {
				log.Printf("plans/exp: обновление снимка затрат не удалось, читаем то, что есть: %v", rerr)
			}
		}
		// След чтения: откуда реально взяты данные именно в этой загрузке.
		ctx, trace := WithExpSourceTrace(ctx)
		n, err := s.syncFromSource(ctx, c, in, force)
		fin := time.Now()
		// Откуда прочитано — до блокировки: это сетевой вызов, а под syncMu читают
		// состояние при каждом открытии формы.
		srcKind := trace.Kind
		var srcAsOf *time.Time
		if info, ok := s.source.(ExpSourceInfo); ok && srcKind == ExpSourceKindCH {
			if asOf, has := info.SnapshotAsOf(ctx); has {
				srcAsOf = &asOf
			}
		}
		s.syncMu.Lock()
		run.status.FinishedAt = &fin
		run.status.SourceCells = n
		if err != nil {
			run.status.State, run.status.Error = ExpSyncError, expSyncErrText(err)
			log.Printf("plans/exp: источник карточки %d не прочитан: %v", c.ID, err)
		} else {
			run.status.State = ExpSyncOK
		}
		run.status.SourceKind, run.status.SourceAsOf = srcKind, srcAsOf
		s.syncMu.Unlock()
		close(run.done)
	}()
	s.waitSync(run)
}

func (s *ExpService) waitSync(run *expSyncRun) {
	if s.syncWait <= 0 {
		return
	}
	select {
	case <-run.done:
	case <-time.After(s.syncWait):
	}
}

// syncStatus — состояние загрузки по экземпляру (idle, если её не было).
func (s *ExpService) syncStatus(instanceID int64) ExpSyncStatus {
	s.syncMu.Lock()
	defer s.syncMu.Unlock()
	if run := s.syncState[instanceID]; run != nil {
		return run.status
	}
	return ExpSyncStatus{State: ExpSyncIdle}
}

// expSyncErrText — причина для экрана. Текст драйвера (адрес сервера, порт, имя логина)
// пользователю не отдаём: обобщённая причина, подробности — в журнале сервера. Свои
// ошибки загрузки (нет колонки в выдаче процедуры) безопасны и показываются как есть,
// обрезка — по символам, а не по байтам.
func expSyncErrText(err error) string {
	msg := err.Error()
	low := strings.ToLower(msg)
	switch {
	case strings.Contains(low, "deadline exceeded") || strings.Contains(low, "timeout") || strings.Contains(low, "timed out"):
		return "источник затрат не ответил вовремя (подробности в журнале сервера)"
	case strings.Contains(low, "нет колонки") || strings.Contains(low, "нет обязательных колонок"):
		r := []rune(msg)
		if len(r) > 300 {
			return string(r[:300]) + "…"
		}
		return msg
	case strings.Contains(low, "login") || strings.Contains(low, "tcp") || strings.Contains(low, "dial") ||
		strings.Contains(low, "connection") || strings.Contains(low, "refused") || strings.Contains(low, "no such host"):
		return "нет соединения с источником затрат (проверьте доступ к OLAP; подробности в журнале сервера)"
	default:
		return "источник затрат вернул ошибку (подробности в журнале сервера)"
	}
}

// SyncNow — POST /api/plans/exp/{cardId}/sync: перечитать состав строк из источника
// сейчас (игнорируя TTL). Возвращает состояние; загрузка идёт в фоне.
func (s *ExpService) SyncNow(ctx context.Context, p Principal, cardID int64) (ExpSyncStatus, error) {
	c, in, err := s.access(ctx, cardID)
	if err != nil {
		return ExpSyncStatus{}, err
	}
	if !cardEditable(c) {
		return ExpSyncStatus{}, errors.New("период закрыт: состав строк не меняется")
	}
	s.ensureSync(c, in, true)
	return s.syncStatus(in.ID), nil
}

// syncFromSource — состав строк и снимок факта из источника (ТЗ §2).
//
// Строки собираются по ФАКТУ ПРОШЛОГО ГОДА: план на год заводится по тем парам
// «ЦФО × статья», по которым расход был. Так же устроен эталон — в нём строки
// приходят из выгрузки, а не заводятся руками.
func (s *ExpService) syncFromSource(ctx context.Context, c Card, in ExpInstance, force bool) (int, error) {
	if s.source == nil {
		return 0, nil
	}
	if !force && s.factFresh(in.ID) {
		return 0, nil
	}
	// Прошлый год даёт состав строк и факт, плановый — стратегию как ориентир
	// (ответ Н1: сценариев три, форма заполняет тактику и обязана показывать
	// стратегию рядом, иначе заполнять не от чего).
	prev := in.PlanYear - 1
	cells, err := s.source.Costs(ctx, in.FormKind, in.Country, []int{prev, in.PlanYear})
	if err != nil {
		return 0, err
	}
	if len(cells) == 0 {
		// Источник ответил, но по этой форме строк нет. Это тоже «прочитано»: без отметки
		// каждое открытие формы запускало бы выгрузку заново (минуты), а фронт —
		// перезагружал форму по кругу.
		s.factSynced(in.ID)
		return 0, nil
	}
	attrs, err := s.source.Cfo(ctx, in.FormKind, in.Country)
	if err != nil {
		log.Printf("plans/exp: справочник ЦФО не прочитан (%s/%s): %v", in.FormKind, in.Country, err)
		attrs = map[int]ExpCfoAttrs{}
	}

	rowByKey := map[[2]int]ExpRow{}
	fact := make([]ExpCell, 0, len(cells))
	strategy := make([]ExpCell, 0, len(cells))
	for _, cell := range cells {
		key := [2]int{cell.CodeCFO, cell.CodePL}
		if _, ok := rowByKey[key]; !ok {
			r := ExpRow{
				CodeCFO: cell.CodeCFO, CodePL: cell.CodePL, CFO: cell.CFO,
				GroupCFO1: cell.GroupCFO1, GroupCFO2: cell.GroupCFO2,
				GroupPL: cell.GroupPL, Expense: cell.Expense,
				LegalEntity: cell.Company, CountryCompany: cell.CountryCompany,
			}
			// Атрибуты, которых нет в строке затрат (ТЗ §2.4).
			if a, ok := attrs[cell.CodeCFO]; ok {
				r.StoreType, r.RegManager = a.StoreType, a.RegManager
				if r.GroupCFO2 == "" {
					r.GroupCFO2 = a.GroupCFO2
				}
				if r.LegalEntity == "" {
					r.LegalEntity = a.LegalEntity
				}
				if r.CFO == "" {
					r.CFO = a.CFO
				}
			}
			rowByKey[key] = r
		}
		amt := cell.Amount
		c := ExpCell{CodeCFO: cell.CodeCFO, CodePL: cell.CodePL, Month: cell.Month, Amount: &amt}
		switch {
		case cell.Scenario == ExpSrcFact && cell.Year == prev:
			fact = append(fact, c)
		case cell.Scenario == ExpSrcStrategy && cell.Year == in.PlanYear:
			strategy = append(strategy, c)
		}
		// Тактику из источника не читаем: её и заполняет форма. Прочитать её
		// значило бы затереть ввод пользователя снимком приёмника.
	}
	rows := make([]ExpRow, 0, len(rowByKey))
	for _, r := range rowByKey {
		rows = append(rows, r)
	}
	if _, err := s.repo.SyncRows(ctx, in.ID, rows); err != nil {
		return len(cells), err
	}
	// Справочник статей пополняется из того же среза (ответ И-12): перечень ТЗ
	// неполон, а новый код PL не должен требовать релиза. Второй уровень
	// (укрупнённая группа P&L) при этом не трогается — это решение НСИ.
	lines := make([]ExpPLLine, 0, len(rowByKey))
	seenPL := map[int]bool{}
	for _, r := range rowByKey {
		if seenPL[r.CodePL] {
			continue
		}
		seenPL[r.CodePL] = true
		lines = append(lines, ExpPLLine{CodePL: r.CodePL, Expense: r.Expense, GroupPL: r.GroupPL})
	}
	if n, err := s.repo.SyncPLLines(ctx, lines); err != nil {
		log.Printf("plans/exp: справочник статей не пополнен: %v", err)
	} else if n > 0 {
		log.Printf("plans/exp: в справочник статей добавлено кодов PL: %d", n)
	}
	if err := s.repo.SaveReadOnly(ctx, in.ID, ExpScenarioFact, fact); err != nil {
		return len(cells), err
	}
	if err := s.repo.SaveReadOnly(ctx, in.ID, ExpScenarioStrategy, strategy); err != nil {
		return len(cells), err
	}
	s.factSynced(in.ID)
	return len(cells), nil
}

// factFresh — факт по экземпляру перечитан недавно.
func (s *ExpService) factFresh(instanceID int64) bool {
	s.factMu.Lock()
	defer s.factMu.Unlock()
	seen, ok := s.factSeen[instanceID]
	return ok && time.Since(seen) < s.factTTL
}

// factSynced — отметить, что факт перечитан.
func (s *ExpService) factSynced(instanceID int64) {
	s.factMu.Lock()
	defer s.factMu.Unlock()
	s.factSeen[instanceID] = time.Now()
}

// SaveCells — сохранение введённых значений.
func (s *ExpService) SaveCells(ctx context.Context, p Principal, cardID int64, cells []ExpCell) (int, error) {
	c, in, err := s.access(ctx, cardID)
	if err != nil {
		return 0, err
	}
	if !cardEditable(c) {
		return 0, errors.New("период закрыт на запись: изменения только после возврата или reopen")
	}
	return s.repo.SaveValues(ctx, in.ID, cells, p.UserID, c.CurrentVersion)
}

// SaveComment — комментарий к строке.
func (s *ExpService) SaveComment(ctx context.Context, p Principal, cardID int64, codeCFO, codePL int, comment string) error {
	c, in, err := s.access(ctx, cardID)
	if err != nil {
		return err
	}
	if !cardEditable(c) {
		return errors.New("период закрыт на запись: комментарий не меняется")
	}
	return s.repo.SaveComment(ctx, in.ID, codeCFO, codePL, comment)
}

// Виды массового действия по строке (ТЗ §7.8).
const (
	ExpBulkSpreadYear = "spread_year"   // размножить на все двенадцать месяцев
	ExpBulkSpreadTail = "spread_to_end" // размножить с текущего месяца по декабрь
	ExpBulkClearRow   = "clear_row"     // очистить строку; пусто не равно нулю
)

// ExpBulkRequest — массовое действие по одной строке формы.
type ExpBulkRequest struct {
	Action  string   `json:"action"`
	CodeCFO int      `json:"code_cfo"`
	CodePL  int      `json:"code_pl"`
	Month   int      `json:"month"`
	Amount  *float64 `json:"amount"`
}

// Bulk — размножение и очистка по строке (ТЗ §7.8).
func (s *ExpService) Bulk(ctx context.Context, p Principal, cardID int64, req ExpBulkRequest) (int, error) {
	c, in, err := s.access(ctx, cardID)
	if err != nil {
		return 0, err
	}
	if !cardEditable(c) {
		return 0, errors.New("период закрыт на запись: массовая операция недоступна")
	}
	cells, err := expBulkCells(req)
	if err != nil {
		return 0, err
	}
	return s.repo.SaveValues(ctx, in.ID, cells, p.UserID, c.CurrentVersion)
}

// expBulkCells — раскладка массового действия в набор ячеек. Вынесено отдельно
// от БД, чтобы правила «с какого месяца и чем заполняем» проверялись тестом.
func expBulkCells(req ExpBulkRequest) ([]ExpCell, error) {
	from, to := 1, 12
	switch req.Action {
	case ExpBulkSpreadYear:
		if req.Amount == nil {
			return nil, errors.New("размножение требует значение")
		}
	case ExpBulkSpreadTail:
		if req.Amount == nil {
			return nil, errors.New("размножение требует значение")
		}
		if req.Month < 1 || req.Month > 12 {
			return nil, fmt.Errorf("месяц вне диапазона: %d", req.Month)
		}
		from = req.Month
	case ExpBulkClearRow:
		req.Amount = nil
	default:
		return nil, fmt.Errorf("неизвестное действие %q", req.Action)
	}
	out := make([]ExpCell, 0, to-from+1)
	for m := from; m <= to; m++ {
		out = append(out, ExpCell{
			CodeCFO: req.CodeCFO, CodePL: req.CodePL, Month: m,
			Amount: req.Amount, Source: ExpSourceSpread,
		})
	}
	if req.Action == ExpBulkClearRow {
		for i := range out {
			out[i].Source = ExpSourceManual
		}
	}
	return out, nil
}

// Totals — итоги по группам PL (ТЗ §7.6).
func (s *ExpService) Totals(ctx context.Context, p Principal, cardID int64) (map[string]float64, error) {
	_, in, err := s.access(ctx, cardID)
	if err != nil {
		return nil, err
	}
	return s.repo.GroupTotals(ctx, in.ID)
}

// groupOrder — порядок групп PL: справочник, а при его отсутствии — перечень
// ТЗ §7.3 из кода. Ошибку справочника не поднимаем: порядок групп не повод не
// открыть форму, а фолбэк даёт ровно тот же порядок, каким его задало ТЗ.
func (s *ExpService) groupOrder(ctx context.Context, kind string) []string {
	got, err := s.repo.GroupOrder(ctx, kind)
	if err != nil {
		log.Printf("plans/exp: порядок групп PL (%s) не прочитан из справочника: %v", kind, err)
	}
	if len(got) > 0 {
		return got
	}
	return ExpGroupOrder(kind)
}
