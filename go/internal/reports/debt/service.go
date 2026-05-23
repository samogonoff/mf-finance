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
func (s *Service) FilterOptions() FilterOptions {
	return FilterOptions{
		Entities:   Entities(),
		Accounts:   Accounts(),
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
		}
		row.AccountName = accountNameFor(ent.Country, rr.AccountRoot)
		if p, ok := partnerByINN[rr.CounterpartyID.String]; ok {
			row.Partner = p
		} else {
			row.Partner = rr.CounterpartyID.String // fallback: показываем ИНН
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
			// RevenueLastMonth заполнится в M4 отдельным запросом по дате последнего месяца.
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

// indexPartnersByINN — в M1 заглушка, partner-имена идут как ИНН.
// В M2/M3 заполним через cache из [FinDWH].[dbo].[Counterparty].
func indexPartnersByINN() map[string]string { return map[string]string{} }

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
