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
	// rates — курсы для пересчёта стран в BYN (свод расходов розницы); nil — без пересчёта.
	rates     *RateBook
	syncMu    sync.Mutex
	syncState map[int64]*expSyncRun
	syncWait  time.Duration
	// reg — реестры формы (ТЗ §5). nil — форма работает без них, как до Ф4.
	reg ExpRegRepo
	// norms — справочники командировок: суточные, нормативы перелёта, курс.
	norms ExpTravelNorms
	// sales — утверждённый план продаж: вход расчёта §6.1, только чтение.
	sales ExpDataSource
	// scope — срез доступа пользователей (свод опта проверяет доступ к карточке); nil — без проверки.
	scope ScopeStore
	// svodWait — сколько свод расходов ждёт загрузки карточек из источника (0 — svodSyncWait).
	svodWait time.Duration
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
	// Опт — одна форма на три страны листа «ОПТ» (ключ без страны).
	if strings.EqualFold(scopeKey, "OPT") {
		return ExpKindOpt, ""
	}
	parts := strings.SplitN(scopeKey, "-", 2)
	if len(parts) != 2 {
		return ExpKindRetail, ""
	}
	switch strings.ToUpper(parts[0]) {
	case "MP":
		return ExpKindMP, strings.ToUpper(parts[1])
	case "IM":
		return ExpKindIM, strings.ToUpper(parts[1])
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
	order := s.groupOrder(ctx, in.FormKind)
	SortExpRowsBy(order, rows)
	// Список групп для фильтра - фактические группы формы (а не справочник): после смены названий групп справочник мог
	// остаться на старых названиях, и выбор такой группы давал бы пустую форму.
	groups := order
	if present, perr := s.repo.Groups(ctx, in.ID); perr != nil {
		log.Printf("plans/exp: группы формы не прочитаны: %v", perr)
	} else if len(present) > 0 {
		groups = orderGroups(present, order)
	}
	hasPlan := false
	if c.Status == CardDraft {
		if gt, gerr := s.repo.GroupTotals(ctx, in.ID); gerr == nil {
			for _, v := range gt {
				if v != 0 {
					hasPlan = true
					break
				}
			}
		}
	}
	return ExpForm{
		Instance: in, Card: c, Rows: rows, Total: total, Offset: f.Offset,
		Groups: groups, ReadOnly: !cardEditable(c), HasPlan: hasPlan, Sync: s.syncStatusWithBasis(ctx, in.ID),
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
	if run := s.startSync(c, in, force); run != nil {
		s.waitSync(run)
	}
}

// startSync запускает загрузку из источника в фоне (не больше одной на экземпляр) и сразу возвращается. Возвращает
// идущую загрузку (новую или уже запущенную) или nil, если запускать нечего: источника нет, данные свежие, после
// ошибки пауза. Ожидание — на вызывающем (форма ждёт syncWait, своды — один общий дедлайн).
func (s *ExpService) startSync(c Card, in ExpInstance, force bool) *expSyncRun {
	if s.source == nil {
		return nil
	}
	s.syncMu.Lock()
	run := s.syncState[in.ID]
	if run != nil && run.status.State == ExpSyncRunning {
		s.syncMu.Unlock()
		return run
	}
	if !force {
		if s.factFresh(in.ID) {
			s.syncMu.Unlock()
			return nil
		}
		if run != nil && run.status.State == ExpSyncError && run.status.FinishedAt != nil &&
			time.Since(*run.status.FinishedAt) < expSyncRetryAfter {
			s.syncMu.Unlock()
			return nil
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
		var refreshWarn string
		if r, ok := s.source.(ExpSourceRefresher); ok && force {
			if rerr := r.RefreshSource(ctx); rerr != nil {
				log.Printf("plans/exp: обновление снимка затрат не удалось, читаем то, что есть: %v", rerr)
				refreshWarn = "Обновить снимок затрат не удалось: " + expSyncErrText(rerr) + "."
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
		run.status.FactBasis = trace.FactBasis
		// Хвост предупреждения — по тому, откуда реально прочитано (после чтения): снимок
		// прошлой заливки или процедура напрямую; иначе плашка противоречила бы шапке.
		if refreshWarn != "" {
			if srcKind == ExpSourceKindOLAP {
				refreshWarn += " Данные прочитаны напрямую из источника."
			} else {
				refreshWarn += " Показаны данные прошлой заливки."
			}
		}
		run.status.Warning = refreshWarn
		s.syncMu.Unlock()
		close(run.done)
	}()
	return run
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

// syncStatusWithBasis — состояние загрузки с основой факта: после рестарта процесса память пуста,
// и признак «по платежам» берётся из сохранённых данных.
func (s *ExpService) syncStatusWithBasis(ctx context.Context, instanceID int64) ExpSyncStatus {
	st := s.syncStatus(instanceID)
	if st.FactBasis == "" {
		st.FactBasis = s.factBasisStored(ctx, instanceID)
	}
	return st
}

// factBasisStored — основа факта по сохранённым данным экземпляра.
func (s *ExpService) factBasisStored(ctx context.Context, instanceID int64) string {
	b, err := s.repo.FactBasis(ctx, instanceID)
	if err != nil {
		log.Printf("plans/exp: основа факта экземпляра %d не прочитана: %v", instanceID, err)
		return ""
	}
	return b
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
	case strings.Contains(low, "could not find stored procedure") || strings.Contains(low, "не удалось найти хранимую процедуру"):
		return "процедура затрат не найдена в базе источника (проверьте PLANS_EXP_COSTS_PROC и доступ)"
	case strings.Contains(low, "execute permission") ||
		(strings.Contains(low, "отказано в разрешении") && strings.Contains(low, "execute")):
		// Английский сервер: «The EXECUTE permission was denied…»; русский: «Отказано в разрешении "EXECUTE" на объект…».
		return "нет права выполнить процедуру затрат (нужен EXECUTE у логина кабинета)"
	case strings.Contains(low, "permission was denied") || strings.Contains(low, "отказано в разрешении") || strings.Contains(low, "отказано в доступе"):
		// Права MSSQL бывают на процедуру и на её таблицы в Budgeting (цепочка владения между
		// FinDWH и Budgeting не действует): текст не должен отправлять админа выдавать EXECUTE.
		return "нет прав на процедуру затрат или её таблицы в Budgeting (подробности в журнале сервера)"
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
	// Платёжный факт помечается в данных (см. SaveReadOnly), а не только в памяти процесса.
	factBasis := expFactBasisFrom(ctx)
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
	factCur := make([]ExpCell, 0, len(cells))
	strategy := make([]ExpCell, 0, len(cells))
	budget := make([]ExpCell, 0, len(cells))
	card := c // в цикле c затеняется ячейкой
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
			c.Source = expCellBasis(cell, factBasis)
			fact = append(fact, c)
		case cell.Scenario == ExpSrcFact && cell.Year == in.PlanYear:
			c.Source = expCellBasis(cell, factBasis)
			// Факт планового года: нужен рядом с вводом («прошлый месяц», «текущий месяц»).
			factCur = append(factCur, c)
		case cell.Scenario == ExpSrcStrategy && cell.Year == in.PlanYear:
			strategy = append(strategy, c)
		case cell.Scenario == ExpSrcTactic && cell.Year == in.PlanYear && !expPastMonth(card, cell.Month):
			// Бюджет источника — как колонка «бюджет» листов Excel: подставляется в план автоматически.
			// Вводимое человеком не затирается (см. FillPlanFromSource), прошедшие месяцы закрыты.
			c.Source = ExpSourceBudget
			budget = append(budget, c)
		}
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
	// Выдача по начислениям: платёжные ячейки прежней загрузки не должны остаться «фактом» (состав строк
	// и месяцев у платежей и начислений разный, перезапись затрагивает только пришедшие ключи).
	if factBasis == "" {
		if err := s.repo.ClearPaymentsFact(ctx, in.ID); err != nil {
			return len(cells), err
		}
	}
	if err := s.repo.SaveReadOnly(ctx, in.ID, ExpScenarioFact, fact); err != nil {
		return len(cells), err
	}
	if err := s.repo.SaveReadOnly(ctx, in.ID, ExpScenarioFactCur, factCur); err != nil {
		return len(cells), err
	}
	if err := s.repo.SaveReadOnly(ctx, in.ID, ExpScenarioStrategy, strategy); err != nil {
		return len(cells), err
	}
	// Бюджет источника — в план, как колонка «бюджет» листов Excel. Только в открытой карточке: утверждённая
	// форма остаётся такой, какой её согласовали.
	if cardEditable(card) {
		if err := s.repo.FillPlanFromSource(ctx, in.ID, card.Month, budget); err != nil {
			return len(cells), err
		}
	}
	s.factSynced(in.ID)
	return len(cells), nil
}

// expCellBasis — пометка ячейки факта: собственная (платёжная ячейка, добавленная к начислениям) или общая для выборки.
func expCellBasis(cell ExpCostCell, readBasis string) string {
	if cell.Basis != "" {
		return cell.Basis
	}
	return readBasis
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
	return s.saveValues(ctx, c, in, p, cells)
}

// saveValues — единственный путь записи плановых ячеек формы: прошедшие месяцы отбрасываются
// здесь, чтобы ни импорт, ни расчёт, ни массовое действие не обошли закрытие.
func (s *ExpService) saveValues(ctx context.Context, c Card, in ExpInstance, p Principal, cells []ExpCell) (int, error) {
	cells = expDropPastMonths(c, cells)
	if len(cells) == 0 {
		return 0, nil
	}
	return s.repo.SaveValues(ctx, in.ID, cells, p.UserID, c.CurrentVersion)
}

// expPastMonth — месяц плана уже прошёл (по нему подтягивается факт).
func expPastMonth(c Card, month int) bool { return month >= 1 && month < c.Month }

// expOpenMonths — месяцы, доступные для планирования (без прошедших).
func expOpenMonths(c Card, months []int) []int {
	out := make([]int, 0, len(months))
	for _, m := range months {
		if !expPastMonth(c, m) {
			out = append(out, m)
		}
	}
	return out
}

// expDropPastMonths — месяцы плана до месяца карточки уже прошли: по ним подтягивается
// факт, и планировать их нельзя. Скрытое от правки на форме закрывается и на сервере,
// чтобы импорт Excel и массовые действия не перезаписали прошедшие месяцы.
func expDropPastMonths(c Card, cells []ExpCell) []ExpCell {
	if c.Month <= 1 {
		return cells
	}
	out := cells[:0:0]
	for _, x := range cells {
		if expPastMonth(c, x.Month) {
			continue
		}
		out = append(out, x)
	}
	return out
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
	Action  string `json:"action"`
	CodeCFO int    `json:"code_cfo"`
	CodePL  int    `json:"code_pl"`
	Month   int    `json:"month"`
	// MonthTo — последний месяц окна (включительно); 0 — до декабря. Окно нужно, когда форма
	// готовится на один месяц или «до конца года», а ранние месяцы трогать нельзя.
	MonthTo int      `json:"month_to"`
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
	return s.saveValues(ctx, c, in, p, cells)
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
		// Очистка с указанным месяцем — только это окно, а не вся строка.
		if req.Month >= 1 && req.Month <= 12 {
			from = req.Month
		}
	default:
		return nil, fmt.Errorf("неизвестное действие %q", req.Action)
	}
	if req.MonthTo >= from && req.MonthTo <= 12 {
		to = req.MonthTo
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
		// Новые названия групп (GroupPL_new) идут первыми, затем порядок справочника с прежними названиями.
		return expDedupGroups(append(append([]string{}, expGroupOrderNew...), got...))
	}
	return ExpGroupOrder(kind)
}
