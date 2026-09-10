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
}

// NewExpService — конструктор.
func NewExpService(repo ExpRepo, cards *CardService, source ExpDataSource) *ExpService {
	return &ExpService{repo: repo, cards: cards, source: source,
		factTTL: 6 * time.Hour, factSeen: map[int64]time.Time{}}
}

// expKindOf — вид формы по области карточки. Область задаётся реестром форм
// (form_registry.go) в виде "R-BY" / "MP-RU": префикс — вид, суффикс — страна.
func expKindOf(scopeKey string) (kind, country string) {
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
		if err := s.syncFromSource(ctx, c, in); err != nil {
			// Недоступный источник не должен закрывать форму: введённое уже
			// лежит в БД и должно открываться без OLAP.
			log.Printf("plans/exp: источник карточки %d не прочитан: %v", cardID, err)
		}
	}
	rows, total, err := s.repo.Rows(ctx, in.ID, f)
	if err != nil {
		return ExpForm{}, err
	}
	SortExpRows(in.FormKind, rows)
	return ExpForm{
		Instance: in, Card: c, Rows: rows, Total: total, Offset: f.Offset,
		Groups: ExpGroupOrder(in.FormKind), ReadOnly: !cardEditable(c),
	}, nil
}

// syncFromSource — состав строк и снимок факта из источника (ТЗ §2).
//
// Строки собираются по ФАКТУ ПРОШЛОГО ГОДА: план на год заводится по тем парам
// «ЦФО × статья», по которым расход был. Так же устроен эталон — в нём строки
// приходят из выгрузки, а не заводятся руками.
func (s *ExpService) syncFromSource(ctx context.Context, c Card, in ExpInstance) error {
	if s.source == nil {
		return nil
	}
	if s.factFresh(in.ID) {
		return nil
	}
	prev := in.PlanYear - 1
	cells, err := s.source.Costs(ctx, in.FormKind, in.Country, []int{prev})
	if err != nil {
		return err
	}
	if len(cells) == 0 {
		return nil
	}
	attrs, err := s.source.Cfo(ctx, in.FormKind, in.Country)
	if err != nil {
		log.Printf("plans/exp: справочник ЦФО не прочитан (%s/%s): %v", in.FormKind, in.Country, err)
		attrs = map[int]ExpCfoAttrs{}
	}

	rowByKey := map[[2]int]ExpRow{}
	fact := make([]ExpCell, 0, len(cells))
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
		fact = append(fact, ExpCell{CodeCFO: cell.CodeCFO, CodePL: cell.CodePL,
			Month: cell.Month, Amount: &amt})
	}
	rows := make([]ExpRow, 0, len(rowByKey))
	for _, r := range rowByKey {
		rows = append(rows, r)
	}
	if _, err := s.repo.SyncRows(ctx, in.ID, rows); err != nil {
		return err
	}
	if err := s.repo.SaveFact(ctx, in.ID, fact); err != nil {
		return err
	}
	s.factSynced(in.ID)
	return nil
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
