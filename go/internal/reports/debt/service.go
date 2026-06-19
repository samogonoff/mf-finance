package debt

import (
	"context"
	"errors"
	"strings"
	"time"
)

// Service — фасад над источниками данных отчёта.
//
// В mock-режиме все запросы отдают фикстуру из mocks.go (без выхода в MSSQL).
// В live-режиме сюда подставляется реализация PremasterRepo (см. repo_premaster.go).
type Service struct {
	mock    bool
	repo    PremasterRepo // может быть nil, если mock=true
}

// PremasterRepo описывает интерфейс live-источника данных (Premaster1C).
// Реализация — repo_premaster.go.
type PremasterRepo interface {
	Report(ctx context.Context, f Filters) ([]DebtRow, error)
	Drilldown(ctx context.Context, q DrilldownQuery) ([]DocumentRow, error)
}

// DrilldownQuery — параметры запроса деталей по договору.
type DrilldownQuery struct {
	CompanyINN string
	PartnerINN string
	Account    string
	Contract   string
	Currency   string
	DateFrom   time.Time
	DateTo     time.Time
}

// NewService — конструктор. Если mock=true, repo может быть nil.
func NewService(mock bool, repo PremasterRepo) *Service {
	return &Service{mock: mock, repo: repo}
}

// FilterOptions — справочные значения для UI.
// Не зависит от MSSQL — отдаёт seed-данные из приложения к ТЗ.
//
// Level 1 MVP: возвращаем только юрлица и счета стран из MVP_LEVEL1_COUNTRIES
// (РФ+РБ). По остальным странам план счетов не подтверждён автором ТЗ —
// см. open-questions.md §A1/§A2. Расширение — после ответов от финансиста.
func (s *Service) FilterOptions() FilterOptions {
	return FilterOptions{
		Entities:   EntitiesLevel1(),
		Accounts:   AccountsLevel1(),
		Currencies: append([]string{}, Currencies...),
	}
}

// Report — основной отчёт. Применяет фильтры и возвращает плоский список строк.
// UI группирует строки на клиенте (Компания → Партнёр → Счёт → Договор → Валюта).
func (s *Service) Report(ctx context.Context, f Filters) (ReportResponse, error) {
	if f.DateTo.Before(f.DateFrom) {
		return ReportResponse{}, errors.New("date_to must be >= date_from")
	}

	now := time.Now().UTC()
	resp := ReportResponse{
		GeneratedAt: now,
		ReportDate:  f.DateTo,
	}

	var rows []DebtRow
	if s.mock {
		rows = applyFilters(mockRows(), f)
	} else {
		if s.repo == nil {
			return ReportResponse{}, errors.New("debt: premaster repo not configured (set DEBT_MOCK=1 or MSSQL_PREMASTER_* env)")
		}
		r, err := s.repo.Report(ctx, f)
		if err != nil {
			return ReportResponse{}, err
		}
		rows = r
	}

	resp.Rows = rows
	return resp, nil
}

// Drilldown — документы внутри (company, partner, account, contract, currency).
// Заполняет PaymentDueDate и OverdueDays (по ТЗ — только на этом уровне).
func (s *Service) Drilldown(ctx context.Context, q DrilldownQuery) ([]DocumentRow, error) {
	reportDate := q.DateTo
	if s.mock {
		return mockDrilldown(q.Contract, q.Currency, reportDate), nil
	}
	if s.repo == nil {
		return nil, errors.New("debt: premaster repo not configured")
	}
	return s.repo.Drilldown(ctx, q)
}

// applyFilters — серверная пре-фильтрация для mock-режима. В live-режиме фильтры
// уходят в SQL (см. repo_premaster.go).
func applyFilters(in []DebtRow, f Filters) []DebtRow {
	innSet := strSet(f.EntityINNs)
	accSet := strSet(f.Accounts)
	curSet := strSet(f.Currencies)

	out := make([]DebtRow, 0, len(in))
	for _, r := range in {
		if len(innSet) > 0 && !innSet[r.CompanyINN] {
			continue
		}
		if len(accSet) > 0 && !accSet[r.Account] {
			continue
		}
		if len(curSet) > 0 && !curSet[r.Currency] {
			continue
		}
		out = append(out, r)
	}
	return out
}

func strSet(ss []string) map[string]bool {
	if len(ss) == 0 {
		return nil
	}
	m := make(map[string]bool, len(ss))
	for _, s := range ss {
		s = strings.TrimSpace(s)
		if s != "" {
			m[s] = true
		}
	}
	return m
}

// docDueDate — плановая дата оплаты документа из Payments.Docs.
// Приоритет: явная PaymentDate; иначе Date + Delay дней; иначе ноль (срок неизвестен).
func docDueDate(payDate, baseDate time.Time, delayDays int, hasDelay bool) time.Time {
	if !payDate.IsZero() {
		return payDate
	}
	if !baseDate.IsZero() && hasDelay {
		return baseDate.AddDate(0, 0, delayDays)
	}
	return time.Time{}
}

// daysOverdue считает дни просрочки от due до now, обрезая на 0.
func daysOverdue(due, now time.Time) int {
	if due.IsZero() || now.Before(due) {
		return 0
	}
	d := now.Sub(due) / (24 * time.Hour)
	if d < 0 {
		return 0
	}
	return int(d)
}

// BuildReport раскручивает сырые signed-сальдо из Premaster в готовые DebtRow:
//   - находит страну юрлица через Entities() (seed.go)
//   - классифицирует корень счёта (chart_of_accounts.go) в KindDZ/KindKZ/KindRevenue
//   - signed-сальдо разворачивает в OpeningDZ/KZ (для KZ-счетов знак сальдо меняем,
//     чтобы наш долг показывался как положительная сумма)
//   - строки без CounterpartyID отбрасываются (внутренние операции — курсы, переоценки)
//   - KindOther (счета не из chart) отбрасываются
//   - Currency в v1 не заполняется (см. M4)
//
// Экспортирована, чтобы repo_premaster.Report мог вызвать после Scan.
func BuildReport(raw []rawRow) []DebtRow {
	companyByINN := indexCompaniesByINN()
	partnerByINN := indexPartnersByINN()

	out := make([]DebtRow, 0, len(raw))
	for _, rr := range raw {
		if !rr.CounterpartyID.Valid || strings.TrimSpace(rr.CounterpartyID.String) == "" {
			continue
		}
		ent, ok := companyByINN[rr.CompanyID]
		if !ok {
			continue
		}
		kind := ClassifyAccount(ent.Country, rr.AccountRoot)
		if kind == KindOther {
			continue
		}

		row := DebtRow{
			Country:    ent.Country,
			Company:    ent.Name,
			CompanyINN: rr.CompanyID,
			PartnerINN: rr.CounterpartyID.String,
			Account:    rr.AccountRoot,
			Currency:   CurrencyForCountry(ent.Country), // функциональная валюта юрлица
		}
		row.AccountName = accountNameFor(ent.Country, rr.AccountRoot)
		// Имя партнёра: приоритет — справочник Counterparty1C (покрывает контрагентов,
		// попавших по ICO=1 вне наших 15 ЮЛ), затем seed-имя нашего ЮЛ, затем голый ИНН.
		switch {
		case rr.PartnerName.Valid && strings.TrimSpace(rr.PartnerName.String) != "":
			row.Partner = strings.TrimSpace(rr.PartnerName.String)
		case partnerByINN[rr.CounterpartyID.String] != "":
			row.Partner = partnerByINN[rr.CounterpartyID.String]
		default:
			row.Partner = rr.CounterpartyID.String // fallback: показываем ИНН
		}
		if rr.Channel.Valid {
			row.Channel = strings.TrimSpace(rr.Channel.String)
		}
		if rr.Manager.Valid {
			row.Manager = strings.TrimSpace(rr.Manager.String)
		}

		switch kind {
		case KindDZ:
			row.OpeningDZ = rr.OpeningSigned
			row.TurnoverDZ = rr.TurnoverSigned
			row.ClosingDZ = rr.ClosingSigned
		case KindKZ:
			// Для пассивных счетов «наш долг» — это кредитовое сальдо. В нашем signed-учёте
			// (Σ Dr − Σ Cr) у пассивного остатка знак отрицательный → переворачиваем.
			row.OpeningKZ = -rr.OpeningSigned
			row.TurnoverKZ = -rr.TurnoverSigned
			row.ClosingKZ = -rr.ClosingSigned
		case KindRevenue:
			// Выручка по 90.x: проводка Cr 90.x → signed = −amt; делаем положительной.
			row.RevenuePeriod = -rr.TurnoverSigned
			row.RevenueLastMonth = -rr.LastMonthSigned
		}

		out = append(out, row)
	}
	return out
}

// indexCompaniesByINN строит индекс наших юрлиц для быстрого lookup внутри BuildReport.
func indexCompaniesByINN() map[string]Entity {
	ent := Entities()
	out := make(map[string]Entity, len(ent))
	for _, e := range ent {
		out[e.INN] = e
	}
	return out
}

// indexPartnersByINN — для ВГО-операций партнёр всегда другое наше ЮЛ ГК МФ,
// имя берём из Entities() (15 ЮЛ с именами и странами). Для внешних партнёров
// справочника пока нет — отображается ИНН (M5).
func indexPartnersByINN() map[string]string {
	out := map[string]string{}
	for _, e := range Entities() {
		out[e.INN] = e.Name
	}
	return out
}

// accountNameFor — имя счёта по коду и стране из seed.go.
// Если не нашли в seed (помеченные Country="" — общие для РФ/РБ) — возвращаем «Счёт <код>».
func accountNameFor(country Country, accountRoot string) string {
	for _, a := range Accounts() {
		if (a.Country == country || a.Country == "") && AccountRoot(a.Code) == accountRoot {
			return a.Name
		}
	}
	return "Счёт " + accountRoot
}

// BuildDrilldown группирует сырые проводки документа по DocID и считает
// дельты DZ/KZ для выбранного account root в выбранной стране.
//
// Правила:
//   - В пределах одного DocID все строки идут в один DocumentRow.
//   - DocDate берётся как min(Date) внутри документа.
//   - DocKind/DocNumber через ResolveDoc (Objects → Mapping → TransDesc → fallback).
//   - DZChange: сумма по строкам, где DrAcc.root = accountRoot И счёт в категории KindDZ для страны.
//     минус сумма где CrAcc.root = accountRoot И счёт в KindDZ.
//   - KZChange: зеркально для KindKZ (с положительным знаком долга — Cr увеличивает, Dr уменьшает).
//   - Если страна неизвестна (CompanyINN не наш) — отдаём все строки с DZChange/KZChange = 0,
//     но имена документов всё равно показываем.
//   - PaymentDueDate/OverdueDays заполняются из Payments.Docs (PaymentDate/Delay),
//     если кросс-БД джойн включён; иначе остаются нулевыми. reportDate (= конец
//     периода отчёта) — точка отсчёта для просрочки.
//
// Экспортирована, чтобы repo_premaster.Drilldown мог её вызвать после Scan.
func BuildDrilldown(raw []drillRow, country Country, accountRoot string, reportDate time.Time) []DocumentRow {
	if len(raw) == 0 {
		return []DocumentRow{}
	}
	kind := ClassifyAccount(country, accountRoot)

	type bucket struct {
		date          time.Time
		dzDelta       float64
		kzDelta       float64
		amount        float64 // суммарный модуль проводок документа
		objects       string
		mapping       string
		trans         string
		descOperation string // первая непустая operation_description

		// Срок оплаты из Payments.Docs (одна строка Docs на DocID → берём первую непустую).
		docBaseDate time.Time // Docs.Date
		docPayDate  time.Time // Docs.PaymentDate (= плановая дата оплаты)
		docDelay    int       // Docs.Delay (дни)
		hasDelay    bool
	}
	byDoc := map[string]*bucket{}
	order := []string{}

	for _, r := range raw {
		b, ok := byDoc[r.DocID]
		if !ok {
			b = &bucket{date: r.Date}
			byDoc[r.DocID] = b
			order = append(order, r.DocID)
		}
		if r.Date.Before(b.date) {
			b.date = r.Date
		}
		if b.objects == "" {
			b.objects = r.ObjectsName
		}
		if b.mapping == "" && r.Mapping.Valid {
			b.mapping = r.Mapping.String
		}
		if b.trans == "" && r.TransDescription.Valid {
			b.trans = r.TransDescription.String
		}
		if b.descOperation == "" && r.OperationDescription.Valid {
			b.descOperation = r.OperationDescription.String
		}
		if b.docPayDate.IsZero() && r.DocPaymentDate.Valid {
			b.docPayDate = r.DocPaymentDate.Time
		}
		if b.docBaseDate.IsZero() && r.DocBaseDate.Valid {
			b.docBaseDate = r.DocBaseDate.Time
		}
		if !b.hasDelay && r.DocDelay.Valid {
			b.docDelay = int(r.DocDelay.Int64)
			b.hasDelay = true
		}

		// Суммарный «вес» документа — модуль каждой проводки (по сути это abs(Amount),
		// поскольку Amount в Premaster всегда положительный; знак уходит в Dr/Cr).
		b.amount += r.Amount

		drRoot := AccountRoot(r.DrAcc)
		crRoot := AccountRoot(r.CrAcc)
		switch kind {
		case KindDZ:
			if drRoot == accountRoot {
				b.dzDelta += r.Amount
			}
			if crRoot == accountRoot {
				b.dzDelta -= r.Amount
			}
		case KindKZ:
			if crRoot == accountRoot {
				b.kzDelta += r.Amount
			}
			if drRoot == accountRoot {
				b.kzDelta -= r.Amount
			}
		}
	}

	out := make([]DocumentRow, 0, len(order))
	for _, id := range order {
		b := byDoc[id]
		p := ResolveDoc(b.objects, b.mapping, b.trans)
		number := p.Number
		if number == "" {
			number = id
		}
		dKind := p.Kind
		if dKind == "" {
			dKind = "Документ"
		}
		docDate := b.date
		if !p.Date.IsZero() {
			docDate = p.Date
		}
		desc := b.descOperation
		if desc == "" {
			desc = b.trans
		}
		due := docDueDate(b.docPayDate, b.docBaseDate, b.docDelay, b.hasDelay)
		out = append(out, DocumentRow{
			DocDate:        docDate,
			DocNumber:      number,
			DocKind:        dKind,
			TransGroup:     b.trans, // тип операции для UI-группировки (M5)
			Amount:         b.amount,
			Description:    desc,
			DZChange:       b.dzDelta,
			KZChange:       b.kzDelta,
			PaymentDueDate: due,
			OverdueDays:    daysOverdue(due, reportDate),
		})
	}
	return out
}
