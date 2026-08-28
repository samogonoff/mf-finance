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

// Сервис формы «Розница»: собирает сетку (§4), сохраняет ввод (§3, §6),
// исполняет массовые операции (§5), считает экран согласования (§7, §8) и
// отчёт валидаций (§6).
//
// Разделение обязанностей: сервис ходит в БД и в источники, но НЕ считает —
// расчёт делают чистые функции retail_calc/bulk/validate/summary. Поэтому все
// формулы ТЗ проверяемы тестами без БД, а сервис отвечает только за права,
// транзакции и кэш срезов.

// RetailService — сервис формы.
type RetailService struct {
	repo     RetailRepo
	cards    *CardService
	source   RetailDataSource
	rates    *RateBook
	calendar CalendarStore
	scope    ScopeStore
	audit    AuditSink
	limits   map[string]float64 // V-02: верхняя граница значения по стране

	// Кэш срезов факта/стратегии/истории: сетка на 375 магазинов дёргает срезы
	// на каждый запрос, а источники суточные. Ключ — (страна, год периода).
	mu     sync.RWMutex
	cache  map[string]cachedSeries
	cached time.Duration
}

type cachedSeries struct {
	series   RetailSeries
	klient   map[int]string
	stores   []RetailStore
	loadedAt time.Time
}

// NewRetailService — конструктор.
func NewRetailService(repo RetailRepo, cards *CardService, source RetailDataSource) *RetailService {
	return &RetailService{
		repo: repo, cards: cards, source: source,
		cache: map[string]cachedSeries{}, cached: 10 * time.Minute,
		limits: map[string]float64{},
	}
}

// WithRates подключает курсы (слой отображения BYN/USD, ТЗ §4).
func (s *RetailService) WithRates(r *RateBook) *RetailService { s.rates = r; return s }

// WithCalendar подключает календарь. Без него «ожидание года» (§4.4) считает,
// что закрытых месяцев нет, — то есть год целиком по тактике.
func (s *RetailService) WithCalendar(c CalendarStore) *RetailService { s.calendar = c; return s }

// WithScope подключает ABAC-срезы (§9).
func (s *RetailService) WithScope(st ScopeStore) *RetailService { s.scope = st; return s }

// WithAudit подключает журнал модуля.
func (s *RetailService) WithAudit(a AuditSink) *RetailService { s.audit = a; return s }

// WithValueLimits задаёт верхние границы значения ячейки по стране (V-02).
func (s *RetailService) WithValueLimits(m map[string]float64) *RetailService {
	if m != nil {
		s.limits = m
	}
	return s
}

// retailAccess — что пользователю разрешено в этой карточке.
type retailAccess struct {
	card   Card
	inst   RetailInstance
	filter ScopeFilter
	// managers — значения RegManager пользователя. Пусто и не админ → магазины
	// не фильтруются по РМ (пользователь либо финансист по ABAC, либо не РМ).
	managers []string
	// finance — право финансиста: переопределение LFL (§3) и массовые операции.
	finance bool
}

// access — карточка + права. Здесь же реализован §9: РМ видит только свои
// магазины (соответствие «пользователь ↔ RegManager» из tp_reg_manager_map),
// финансист — все.
func (s *RetailService) access(ctx context.Context, cardID int64, p Principal) (retailAccess, error) {
	var a retailAccess
	c, err := s.cards.Card(ctx, cardID)
	if err != nil {
		return a, err
	}
	if c.FormCode != TemplateRetail {
		return a, fmt.Errorf("карточка %d — форма %s, а не розница", cardID, c.FormCode)
	}
	a.card = c
	a.finance = p.PlansAdmin

	if s.scope != nil {
		f, err := scopeFilterFor(ctx, s.scope, p)
		if err != nil {
			return a, err
		}
		a.filter = f
	} else {
		a.filter = NewScopeFilter(p.PlansAdmin, nil)
	}
	if !a.filter.AllowsCard(c) {
		return a, errors.New("карточка вне вашего среза доступа")
	}
	if mgrs, err := s.repo.RegManagers(ctx, p.UserID); err != nil {
		log.Printf("plans retail: соответствие РМ для пользователя %d не прочитано: %v", p.UserID, err)
	} else {
		a.managers = mgrs
	}

	inst, err := s.repo.EnsureInstance(ctx, c, p.UserID)
	if err != nil {
		return a, err
	}
	a.inst = inst
	return a, nil
}

// allowsRow — доступ к строке-магазину (§9 + ABAC по ЦФО).
func (a retailAccess) allowsRow(r RetailRow) bool {
	if a.filter.Admin {
		return true
	}
	if !a.filter.Allows(r.CodeCFO, a.card.Country, a.card.LegalEntity) {
		return false
	}
	// РМ видит только свои магазины (§9). Закрытые точки с RegManager
	// 'Closed'/'n/a' в соответствие не попадают и потому недоступны никому,
	// кроме админа — ровно как требует ТЗ.
	if len(a.managers) > 0 {
		for _, m := range a.managers {
			if strings.EqualFold(strings.TrimSpace(m), strings.TrimSpace(r.RegManager)) {
				return true
			}
		}
		return false
	}
	return true
}

// series — срезы факта/стратегии/истории для страны и года (с кэшем).
func (s *RetailService) series(ctx context.Context, country string, year, month int) (RetailSeries, map[int]string, []RetailStore) {
	key := fmt.Sprintf("%s:%d", country, year)
	s.mu.RLock()
	if c, ok := s.cache[key]; ok && time.Since(c.loadedAt) < s.cached {
		s.mu.RUnlock()
		out := c.series
		out.ClosedMonths = s.closedMonths(ctx, year)
		return out, c.klient, c.stores
	}
	s.mu.RUnlock()

	ser := NewRetailSeries()
	var klient map[int]string
	var stores []RetailStore

	if s.source != nil {
		if st, err := s.source.Stores(ctx, country); err != nil {
			log.Printf("plans retail: справочник магазинов %s недоступен: %v", country, err)
		} else {
			stores = st
		}
		// Факт нужен за текущий и предыдущий год: §4 требует Факт[M, Y−1] и
		// «откл. ожидания к факту прошлого года».
		if cells, err := s.source.Fact(ctx, country, []int{year - 1, year}); err != nil {
			log.Printf("plans retail: факт продаж %s недоступен: %v", country, err)
		} else {
			ser.AddFact(cells)
		}
		if cells, err := s.source.Strategy(ctx, country, year); err != nil {
			log.Printf("plans retail: стратегия %s недоступна: %v", country, err)
		} else {
			ser.AddStrategy(cells)
		}
		if cells, kl, err := s.source.PlanHistory(ctx, country, year); err != nil {
			log.Printf("plans retail: история плана %s недоступна: %v", country, err)
		} else {
			ser.AddApproved(cells)
			klient = kl
		}
	}
	s.mu.Lock()
	s.cache[key] = cachedSeries{series: ser, klient: klient, stores: stores, loadedAt: time.Now()}
	s.mu.Unlock()

	ser.ClosedMonths = s.closedMonths(ctx, year)
	return ser, klient, stores
}

// closedMonths — закрытые месяцы года ИЗ КАЛЕНДАРЯ (ТЗ §4.4). Недоступный
// календарь → пусто: лучше посчитать «ожидание года» целиком по тактике, чем
// подменить признак закрытия «наличием чисел», что ТЗ прямо запрещает.
func (s *RetailService) closedMonths(ctx context.Context, year int) map[int]bool {
	if s.calendar == nil {
		return map[int]bool{}
	}
	closed, err := s.calendar.ClosedMonths(ctx, year)
	if err != nil {
		log.Printf("plans retail: закрытые месяцы %d не прочитаны: %v", year, err)
		return map[int]bool{}
	}
	if closed == nil {
		return map[int]bool{}
	}
	return closed
}

// InvalidateCache — сброс кэша срезов (после синхронизации справочника).
func (s *RetailService) InvalidateCache() {
	s.mu.Lock()
	s.cache = map[string]cachedSeries{}
	s.mu.Unlock()
}

// Form — сетка формы (GET /api/plans/retail/{cardId}/form).
// currency — валюта ОТОБРАЖЕНИЯ: пусто/нац. → как введено; BYN/USD → слой
// отображения, индикаторы не пересчитываются (ТЗ §4).
func (s *RetailService) Form(ctx context.Context, p Principal, cardID int64, currency string) (RetailForm, error) {
	a, err := s.access(ctx, cardID, p)
	if err != nil {
		return RetailForm{}, err
	}
	ser, klient, stores := s.series(ctx, a.card.Country, a.card.Year, a.card.Month)

	// Строки приводим к справочнику при каждом открытии: ТЗ §2 требует, чтобы в
	// форме не было магазинов, закрытых до начала периода (V-09), а новые
	// магазины появлялись без ручного действия. На закрытой карточке не трогаем:
	// утверждённый период не должен менять состав строк.
	if len(stores) > 0 && cardEditable(a.card) {
		if added, updated, removed, err := s.repo.SyncRows(ctx, a.inst, stores, klient, p.UserID); err != nil {
			log.Printf("plans retail: синхронизация строк карточки %d: %v", cardID, err)
		} else if added+removed > 0 {
			log.Printf("plans retail: карточка %d — строк добавлено %d, обновлено %d, удалено %d",
				cardID, added, updated, removed)
		}
	}

	rows, err := s.repo.Rows(ctx, a.inst.ID)
	if err != nil {
		return RetailForm{}, err
	}
	params, err := s.repo.Params(ctx, a.inst.ID)
	if err != nil {
		return RetailForm{}, err
	}

	editable := cardEditable(a.card)
	planMonths := retailPlanMonths(a.card.Month)

	visible := make([]RetailRow, 0, len(rows))
	for _, r := range rows {
		if !a.allowsRow(r) {
			continue
		}
		// Флаг editable на ячейке: закрытая карточка (§2.4) закрывает ввод ДЛЯ ВСЕХ.
		for i := range r.Values {
			r.Values[i].Editable = editable
		}
		visible = append(visible, r)
	}
	sortRetailRows(visible)
	visible = ComputeRetailForm(visible, ser, a.card.Year, a.card.Month, planMonths)

	vin := s.validationInput(a, visible, ser, params, stores)
	blocking, warnings := ValidateRetailForm(vin)
	// Предупреждения раскладываем по строкам: сетка подсвечивает ячейку, где
	// сработал порог, а не показывает общий список (ТЗ §12).
	byRow := map[int][]ValidationIssue{}
	for _, w := range warnings {
		byRow[w.CodeCFO] = append(byRow[w.CodeCFO], w)
	}
	for i := range visible {
		visible[i].Warnings = byRow[visible[i].CodeCFO]
	}

	form := RetailForm{
		CardID: cardID, InstanceID: a.inst.ID, FormCode: a.card.FormCode,
		Country: a.card.Country, LegalEntity: a.card.LegalEntity,
		NatCurrency: a.inst.Currency, Currency: a.inst.Currency,
		Year: a.card.Year, Month: a.card.Month, Months: planMonths,
		Editable: editable, Status: CardStatusLabel(a.card.Status, a.card.StepCode),
		StepCode: a.card.StepCode, Params: params, Warnings: blocking, FxRate: 1,
	}
	// Слой отображения: пересчёт денежных величин, проценты те же (ТЗ §4).
	if cur := normalizeDisplayCurrency(currency); cur != "" && cur != a.inst.Currency && s.rates != nil {
		rate := s.rates.Convert(ctx, 1, a.inst.Currency, cur, a.card.Year, a.card.Month)
		form.Currency, form.FxRate = cur, rate
		visible = ConvertRetailRows(visible, rate)
	}
	form.Rows = visible
	return form, nil
}

// normalizeDisplayCurrency — допустимые валюты отображения (ТЗ §4, §8).
func normalizeDisplayCurrency(c string) string {
	switch strings.ToUpper(strings.TrimSpace(c)) {
	case "BYN":
		return "BYN"
	case "USD":
		return "USD"
	case "RUB":
		return "RUB"
	case "KZT":
		return "KZT"
	case "UZS":
		return "UZS"
	}
	return ""
}

// validationInput — вход валидаций (собирается один раз для формы, отчёта и
// сохранения: правила должны быть одни и те же).
func (s *RetailService) validationInput(a retailAccess, rows []RetailRow, ser RetailSeries,
	params []RetailParam, stores []RetailStore) RetailValidationInput {
	var dirCodes map[int]bool
	if len(stores) > 0 {
		dirCodes = make(map[int]bool, len(stores))
		for _, st := range stores {
			dirCodes[st.CodeCFO] = true
		}
	}
	return RetailValidationInput{
		Country: a.card.Country, LegalEntity: a.card.LegalEntity,
		NatCurrency: a.inst.Currency, Year: a.card.Year, Month: a.card.Month,
		PlanMonths: retailPlanMonths(a.card.Month), Rows: rows, Series: ser,
		Params: RetailParamSet{Params: params}, ValueLimit: s.limits[a.card.Country],
		DirectoryCodes: dirCodes,
	}
}

// RetailSaveRequest — сохранение ввода (PUT .../form).
type RetailSaveRequest struct {
	Cells    []RetailCellWrite `json:"cells"`
	Comments []struct {
		CodeCFO int    `json:"code_cfo"`
		Comment string `json:"comment"`
	} `json:"comments"`
}

// SaveForm — сохранение ячеек и комментариев (ТЗ §3).
//
// Порядок проверок важен: сначала «можно ли вообще писать» (карточка закрыта →
// отказ ДЛЯ ВСЕХ, §2.4), потом ABAC/РМ по каждому магазину (§9), потом
// значения (V-02/V-10/V-11). Блокирующие V-01/V-03/V-07 при сохранении НЕ
// применяются: незаполненная форма — нормальное промежуточное состояние, их
// проверяет отправка на согласование.
func (s *RetailService) SaveForm(ctx context.Context, p Principal, cardID int64, req RetailSaveRequest) (RetailForm, error) {
	a, err := s.access(ctx, cardID, p)
	if err != nil {
		return RetailForm{}, err
	}
	if !cardEditable(a.card) {
		return RetailForm{}, errors.New("период закрыт на запись: изменения только после возврата или reopen")
	}
	rows, err := s.repo.Rows(ctx, a.inst.ID)
	if err != nil {
		return RetailForm{}, err
	}
	byCode := map[int]RetailRow{}
	for _, r := range rows {
		byCode[r.CodeCFO] = r
	}
	planMonths := map[int]bool{}
	for _, m := range retailPlanMonths(a.card.Month) {
		planMonths[m] = true
	}
	limit := s.limits[a.card.Country]

	for i := range req.Cells {
		c := &req.Cells[i]
		row, ok := byCode[c.CodeCFO]
		if !ok {
			return RetailForm{}, fmt.Errorf("магазина %d нет в форме", c.CodeCFO)
		}
		if !a.allowsRow(row) {
			return RetailForm{}, fmt.Errorf("магазин %d вне вашего среза доступа", c.CodeCFO)
		}
		if c.Metric == "" {
			c.Metric = MetricSales
		}
		if c.Metric != MetricSales && !a.finance {
			return RetailForm{}, errors.New("статьи ФОТ и аренды правит финансист, а не заполняющий")
		}
		if c.Year == 0 {
			c.Year = a.card.Year
		}
		// V-10: значения вне планового периода не принимаем на входе, а не
		// «сохраним и покажем в отчёте» — иначе форма копит мусор.
		if c.Year != a.card.Year || !planMonths[c.Month] {
			return RetailForm{}, fmt.Errorf("магазин %d: %02d.%d вне планового периода", c.CodeCFO, c.Month, c.Year)
		}
		if c.Amount != nil {
			if *c.Amount < 0 {
				return RetailForm{}, fmt.Errorf("магазин %d, %02d: план не может быть отрицательным (V-02)", c.CodeCFO, c.Month)
			}
			if limit > 0 && *c.Amount > limit {
				return RetailForm{}, fmt.Errorf("магазин %d, %02d: план %.2f выше допустимой границы %.2f (V-02)",
					c.CodeCFO, c.Month, *c.Amount, limit)
			}
		}
		// Ручной ввод всегда manual: он и есть решение человека, которое массовые
		// операции обязаны уважать (ТЗ §5).
		if c.Source == "" {
			c.Source = ValueManual
		}
	}

	if _, err := s.repo.SaveValues(ctx, a.inst.ID, req.Cells, p.UserID, "manual_edit"); err != nil {
		return RetailForm{}, err
	}
	for _, cm := range req.Comments {
		row, ok := byCode[cm.CodeCFO]
		if !ok || !a.allowsRow(row) {
			return RetailForm{}, fmt.Errorf("магазин %d вне вашего среза доступа", cm.CodeCFO)
		}
		if err := s.repo.SaveComment(ctx, a.inst.ID, cm.CodeCFO, cm.Comment, p.UserID); err != nil {
			return RetailForm{}, err
		}
	}
	s.rec(ctx, p.UserID, "retail_form_save", cardID)
	return s.Form(ctx, p, cardID, "")
}

// Bulk — массовая операция (ТЗ §5). При Preview=true НИЧЕГО не сохраняется.
func (s *RetailService) Bulk(ctx context.Context, p Principal, cardID int64, req RetailBulkRequest) (RetailBulkResult, error) {
	a, err := s.access(ctx, cardID, p)
	if err != nil {
		return RetailBulkResult{}, err
	}
	if err := ValidateBulkRequest(req); err != nil {
		return RetailBulkResult{}, err
	}
	if !req.Preview && !cardEditable(a.card) {
		return RetailBulkResult{}, errors.New("период закрыт на запись: массовая операция недоступна")
	}
	if !a.finance && (req.Op == BulkPayroll || req.Op == BulkRent) {
		return RetailBulkResult{}, errors.New("операции «ФОТ от продаж» и «Аренда» доступны финансисту")
	}

	ser, _, _ := s.series(ctx, a.card.Country, a.card.Year, a.card.Month)
	rows, err := s.repo.Rows(ctx, a.inst.ID)
	if err != nil {
		return RetailBulkResult{}, err
	}
	visible := make([]RetailRow, 0, len(rows))
	for _, r := range rows {
		if a.allowsRow(r) {
			visible = append(visible, r)
		}
	}
	params, err := s.repo.Params(ctx, a.inst.ID)
	if err != nil {
		return RetailBulkResult{}, err
	}

	bctx := RetailBulkContext{
		Country: a.card.Country, Year: a.card.Year, Month: a.card.Month,
		PlanMonths: retailPlanMonths(a.card.Month), Rows: visible, Series: ser,
		Params: RetailParamSet{Params: params},
	}
	if req.Op == BulkPayroll && s.rates != nil {
		// Удельный вес ФОТ применяется к продажам БЕЗ НДС (ответ финблока
		// §12 п.12), поэтому операции нужна ставка страны из справочника.
		bctx.VatRate = s.rates.Vat(ctx, a.card.Country, 0)
	}
	if req.Op == BulkRent {
		py, pm := retailPrevMonth(a.card.Year, a.card.Month)
		if prev, err := s.repo.PrevMetric(ctx, a.card.Country, MetricRent, py, pm); err != nil {
			log.Printf("plans retail: аренда предыдущего периода недоступна: %v", err)
			bctx.RentPrevFact = map[int]float64{}
		} else {
			bctx.RentPrevFact = prev
		}
	}

	res, err := ApplyRetailBulk(req, bctx)
	if err != nil {
		return res, err
	}
	if req.Preview {
		return res, nil
	}

	// Применение — теми же значениями, что показал предпросмотр.
	cells := make([]RetailCellWrite, 0, len(res.Diff))
	for _, d := range res.Diff {
		after := d.After
		cells = append(cells, RetailCellWrite{
			CodeCFO: d.CodeCFO, Metric: d.Metric, Year: d.Year, Month: d.Month,
			Amount: &after, Source: d.AfterSource, Note: d.Note,
		})
	}
	if _, err := s.repo.SaveValues(ctx, a.inst.ID, cells, p.UserID, req.Op); err != nil {
		return res, err
	}
	s.rec(ctx, p.UserID, "retail_bulk_"+req.Op, cardID)
	return res, nil
}

// Params — параметры периода.
func (s *RetailService) Params(ctx context.Context, p Principal, cardID int64) ([]RetailParam, error) {
	a, err := s.access(ctx, cardID, p)
	if err != nil {
		return nil, err
	}
	return s.repo.Params(ctx, a.inst.ID)
}

// SaveParams — правка параметров периода (индекс, ФОТ, аренда, пороги).
// Только финансист/админ процессов: индекс роста — решение на всю страну (§5).
func (s *RetailService) SaveParams(ctx context.Context, p Principal, cardID int64, params []RetailParam) ([]RetailParam, error) {
	a, err := s.access(ctx, cardID, p)
	if err != nil {
		return nil, err
	}
	if !a.finance {
		return nil, errors.New("параметры периода задаёт финансист или администратор процессов")
	}
	if !cardEditable(a.card) {
		return nil, errors.New("период закрыт: параметры не меняются")
	}
	for i := range params {
		if params[i].ScopeKind == "" {
			params[i].ScopeKind = ParamScopeCountry
			params[i].ScopeValue = a.card.Country
		}
		if err := validateRetailParam(params[i]); err != nil {
			return nil, err
		}
	}
	if err := s.repo.SaveParams(ctx, a.inst.ID, params, p.UserID); err != nil {
		return nil, err
	}
	s.rec(ctx, p.UserID, "retail_params_save", cardID)
	return s.repo.Params(ctx, a.inst.ID)
}

// validateRetailParam — проверка параметра периода.
func validateRetailParam(p RetailParam) error {
	switch p.ParamCode {
	case ParamSalesIndex:
		// Индекс МОЖЕТ быть отрицательным (ТЗ §5) — границ не ставим.
	case ParamPayrollShare, ParamRentShare, ParamRentRate:
		if p.Value < 0 {
			return fmt.Errorf("параметр %s не может быть отрицательным", p.ParamCode)
		}
	case ParamPayrollCap:
		if p.Value <= 0 {
			return errors.New("порог ФОТ должен быть больше нуля (по умолчанию 1.06 = 106 %)")
		}
	case ParamRentThresh:
		if p.Value < 0 {
			return errors.New("порог оборота аренды не может быть отрицательным")
		}
	case ParamLFLWarn, ParamLFMWarn, ParamStrategyWarn:
		if p.Value <= 0 {
			return fmt.Errorf("порог предупреждения %s должен быть больше нуля", p.ParamCode)
		}
	default:
		return fmt.Errorf("неизвестный параметр периода %q", p.ParamCode)
	}
	switch p.ScopeKind {
	case ParamScopeCountry, ParamScopeCity, ParamScopeLFL, ParamScopeStoreType, ParamScopeStore:
	default:
		return fmt.Errorf("неизвестная область параметра %q", p.ScopeKind)
	}
	return nil
}

// SetLFLOverride — переопределение LFL-статуса (ТЗ §3): только финансист,
// причина обязательна, справочник не перезатирается.
func (s *RetailService) SetLFLOverride(ctx context.Context, p Principal, cardID int64,
	codeCFO int, status, reason string) error {
	a, err := s.access(ctx, cardID, p)
	if err != nil {
		return err
	}
	if !a.finance {
		return errors.New("переопределение LFL-статуса доступно только финансисту")
	}
	if !cardEditable(a.card) {
		return errors.New("период закрыт: LFL-статус не меняется")
	}
	if strings.TrimSpace(status) != "" && strings.TrimSpace(reason) == "" {
		return errors.New("переопределение LFL-статуса требует причины")
	}
	if st := strings.TrimSpace(status); st != "" {
		switch st {
		case LFLYes, LFLUnder1, LFLNew, LFLNoID, LFLClosed:
		default:
			return fmt.Errorf("неизвестный LFL-статус %q", st)
		}
	}
	if err := s.repo.SetLFLOverride(ctx, a.inst.ID, codeCFO, strings.TrimSpace(status), reason, p.UserID); err != nil {
		return err
	}
	s.rec(ctx, p.UserID, "retail_lfl_override", cardID)
	return nil
}

// Summary — экран согласования (ТЗ §7, §8).
func (s *RetailService) Summary(ctx context.Context, p Principal, cardID int64) (RetailSummary, error) {
	a, err := s.access(ctx, cardID, p)
	if err != nil {
		return RetailSummary{}, err
	}
	ser, _, stores := s.series(ctx, a.card.Country, a.card.Year, a.card.Month)
	rows, err := s.repo.Rows(ctx, a.inst.ID)
	if err != nil {
		return RetailSummary{}, err
	}
	visible := make([]RetailRow, 0, len(rows))
	for _, r := range rows {
		if a.allowsRow(r) {
			visible = append(visible, r)
		}
	}
	in := RetailSummaryInput{
		CardID: cardID, Country: a.card.Country, NatCurrency: a.inst.Currency,
		Year: a.card.Year, Month: a.card.Month, PlanMonths: retailPlanMonths(a.card.Month),
		Rows: visible, Series: ser, FxRates: map[string]float64{},
		DirectoryActiveCount: -1,
	}
	if s.rates != nil {
		for _, cur := range []string{"BYN", "USD"} {
			in.FxRates[cur] = s.rates.Convert(ctx, 1, a.inst.Currency, cur, a.card.Year, a.card.Month)
		}
	}
	// Сверка §7 №2: итог факта из источника за тот же период. Считаем по срезу
	// факта — это ровно «данные источника», а не пересчёт формы.
	if len(ser.Fact) > 0 {
		total := 0.0
		for k, v := range ser.Fact {
			if k.Year == a.card.Year && k.Month == a.card.Month {
				total += v
			}
		}
		in.SourceFactTotal = &total
	}
	// Сверка §7 №4: полнота справочника — активные магазины страны на период.
	if len(stores) > 0 {
		active := 0
		for _, st := range stores {
			if !retailStoreClosedBefore(st.DateClose, a.card.Year, a.card.Month) {
				active++
			}
		}
		in.DirectoryActiveCount = active
	}
	return ComputeRetailSummary(in), nil
}

// Validate — полный отчёт валидаций V-*/W-* + контрольные сверки (ТЗ §6, §7).
func (s *RetailService) Validate(ctx context.Context, p Principal, cardID int64) (RetailValidationReport, error) {
	a, err := s.access(ctx, cardID, p)
	if err != nil {
		return RetailValidationReport{}, err
	}
	ser, _, stores := s.series(ctx, a.card.Country, a.card.Year, a.card.Month)
	rows, err := s.repo.Rows(ctx, a.inst.ID)
	if err != nil {
		return RetailValidationReport{}, err
	}
	params, err := s.repo.Params(ctx, a.inst.ID)
	if err != nil {
		return RetailValidationReport{}, err
	}
	// Отчёт валидаций считается по ВСЕМ строкам экземпляра, а не по видимым:
	// согласующий обязан видеть форму целиком, иначе «можно отправлять» у РМ и у
	// финансиста разошлись бы.
	rows = ComputeRetailForm(rows, ser, a.card.Year, a.card.Month, retailPlanMonths(a.card.Month))
	blocking, warnings := ValidateRetailForm(s.validationInput(a, rows, ser, params, stores))

	sum, err := s.Summary(ctx, p, cardID)
	if err != nil {
		return RetailValidationReport{}, err
	}
	// V-05: несошедшаяся контрольная сверка — блокирующее замечание, а не просто
	// красный блок в своде. Иначе форму можно было бы отправить с расхождением.
	blocking = append(blocking, ReconciliationIssues(sum.Reconciliations)...)
	return RetailValidationReport{
		CardID: cardID, Blocking: blocking, Warnings: warnings,
		CanSubmit: len(blocking) == 0, Reconciliations: sum.Reconciliations,
	}, nil
}

// rec — событие в журнал модуля (аудит выключен → no-op).
func (s *RetailService) rec(ctx context.Context, userID int64, action string, cardID int64) {
	if s.audit == nil || !s.audit.Enabled() {
		return
	}
	_ = s.audit.Record(ctx, AuditEvent{
		UserID: userID, Action: action, EntityType: "form_card", EntityID: cardID,
	})
}
