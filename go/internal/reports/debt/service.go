package debt

import (
	"context"
	"errors"
	"strings"
	"time"
)

// Service — фасад над источником данных отчёта «Задолженность ВГО».
//
// В mock-режиме (DEBT_MOCK=1) запросы отдают фикстуру из mocks.go (без выхода
// в источник). В live-режиме сюда подставляется реализация PremasterRepo —
// findebt-CH (finance.fact_findebt) или findebt-live (MSSQL FinDebt-вьюхи).
type Service struct {
	mock bool
	repo PremasterRepo // может быть nil, если mock=true
}

// PremasterRepo описывает интерфейс live-источника отчёта.
// Имя историческое (отчёт раньше считался по Premaster1C); ныне реализации —
// repo_findebt_ch.go (CH) и repo_findebt.go (MSSQL FinDebt-вьюхи).
type PremasterRepo interface {
	Report(ctx context.Context, f Filters) ([]DebtRow, error)
	Drilldown(ctx context.Context, q DrilldownQuery) ([]DocumentRow, error)
}

// DrilldownQuery — параметры запроса деталей по (компания, партнёр, счёт).
type DrilldownQuery struct {
	CompanyINN string
	PartnerINN string
	Account    string
	Contract   string
	Currency   string
	// Lens — линза представления суммы (см. Filters.Lens); документы drill-down
	// должны прийти в той же линзе, что и свод. Пусто → LensDefault.
	Lens     string
	DateFrom time.Time
	DateTo   time.Time
}

// NewService — конструктор. Если mock=true, repo может быть nil.
func NewService(mock bool, repo PremasterRepo) *Service {
	return &Service{mock: mock, repo: repo}
}

// FilterOptions — справочные значения для UI (из seed-данных приложения к ТЗ).
//
// Level 1 MVP: только юрлица/счета стран из MVP_LEVEL1_COUNTRIES (РФ+РБ).
func (s *Service) FilterOptions() FilterOptions {
	return FilterOptions{
		Entities: EntitiesLevel1(),
		Accounts: AccountsLevel1(),
		Lenses:   append([]string{}, Lenses...),
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
		rows = applyLens(applyFilters(mockRows(), f), normLens(f.Lens))
	} else {
		if s.repo == nil {
			return ReportResponse{}, errors.New("debt: repo not configured (set DEBT_MOCK=1 or configure DEBT_BACKEND source)")
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
func (s *Service) Drilldown(ctx context.Context, q DrilldownQuery) ([]DocumentRow, error) {
	reportDate := q.DateTo
	if s.mock {
		return mockDrilldown(q.Contract, q.Currency, reportDate), nil
	}
	if s.repo == nil {
		return nil, errors.New("debt: repo not configured")
	}
	return s.repo.Drilldown(ctx, q)
}

// applyFilters — серверная пре-фильтрация для mock-режима. В live-режиме фильтры
// уходят в источник (см. repo_findebt*).
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
// Используется mock-drilldown'ом (live-источник отдаёт DAY_DELAY готовым).
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

// accountNameFor — имя счёта по коду и стране из seed.go.
// Если не нашли в seed (Country="" — общие для РФ/РБ) — возвращаем «Счёт <код>».
func accountNameFor(country Country, accountRoot string) string {
	for _, a := range Accounts() {
		if (a.Country == country || a.Country == "") && AccountRoot(a.Code) == accountRoot {
			return a.Name
		}
	}
	return "Счёт " + accountRoot
}
