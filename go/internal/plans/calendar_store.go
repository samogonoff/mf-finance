package plans

import (
	"context"
	"encoding/json"
	"log"
	"sync"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

// Календари из БД (plans_country_calendar), а не из кода.
//
// Таблица существует с 0016, но движок сроков читал CalendarSeed() — правки
// календаря в интерфейсе ни на что не влияли. Обе новые формы считают дедлайны
// по производственному календарю страны ответственного (ТЗ МП §2.3 «дедлайн —
// 2-й р.д.», Розница §2.3 «дедлайны 2–6-й р.д. считаются по настраиваемому
// производственному календарю»), а «ожидание года» розницы (§4.4) требует ещё и
// признака закрытого месяца — тоже из календаря, а не «по наличию чисел».

// CalendarStore — календари стран и закрытые периоды.
type CalendarStore interface {
	Calendars(ctx context.Context, year int) ([]CountryCalendar, error)
	// ClosedMonths — закрытые (отфакченные) месяцы года: месяц → true.
	// Признак закрытия — утверждённая карточка формы факта или явная отметка;
	// пока источник один: pl_instance со статусом archived/approved.
	ClosedMonths(ctx context.Context, year int) (map[int]bool, error)
}

type pgCalendarStore struct {
	pool *pgxpool.Pool

	mu     sync.RWMutex
	cache  map[int][]CountryCalendar
	loaded map[int]time.Time
}

// NewCalendarStore — конструктор.
func NewCalendarStore(pool *pgxpool.Pool) CalendarStore {
	return &pgCalendarStore{
		pool:   pool,
		cache:  map[int][]CountryCalendar{},
		loaded: map[int]time.Time{},
	}
}

func (s *pgCalendarStore) Calendars(ctx context.Context, year int) ([]CountryCalendar, error) {
	s.mu.RLock()
	if cached, ok := s.cache[year]; ok && time.Since(s.loaded[year]) < 5*time.Minute {
		s.mu.RUnlock()
		return cached, nil
	}
	s.mu.RUnlock()

	rows, err := s.pool.Query(ctx, `
		SELECT country, year, holidays, work_weekends
		  FROM plans_country_calendar WHERE year = $1`, year)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := make([]CountryCalendar, 0, 4)
	for rows.Next() {
		var c CountryCalendar
		var hol, work []byte
		if err := rows.Scan(&c.Country, &c.Year, &hol, &work); err != nil {
			return nil, err
		}
		c.Holidays = dateSet(hol)
		c.WorkWeekends = dateSet(work)
		out = append(out, c)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	s.mu.Lock()
	s.cache[year], s.loaded[year] = out, time.Now()
	s.mu.Unlock()
	return out, nil
}

func (s *pgCalendarStore) ClosedMonths(ctx context.Context, year int) (map[int]bool, error) {
	rows, err := s.pool.Query(ctx, `
		SELECT period_month FROM pl_instance
		 WHERE period_year = $1 AND status IN ('approved','archived')`, year)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := map[int]bool{}
	for rows.Next() {
		var m int
		if err := rows.Scan(&m); err != nil {
			return nil, err
		}
		out[m] = true
	}
	return out, rows.Err()
}

// dateSet — JSONB-массив дат ['2026-01-01',…] → множество.
func dateSet(raw []byte) map[string]bool {
	out := map[string]bool{}
	if len(raw) == 0 {
		return out
	}
	var list []string
	if err := json.Unmarshal(raw, &list); err != nil {
		return out
	}
	for _, d := range list {
		out[d] = true
	}
	return out
}

// calendarsOrSeed — календари года из БД с фолбэком на seed. Ошибка чтения не
// должна ломать процесс: сроки посчитаются по базовому календарю, а расхождение
// видно в логе.
func calendarsOrSeed(ctx context.Context, store CalendarStore, year int) []CountryCalendar {
	if store == nil {
		return CalendarSeed()
	}
	cals, err := store.Calendars(ctx, year)
	if err != nil {
		log.Printf("plans: календари %d не прочитаны, работаем на seed: %v", year, err)
		return CalendarSeed()
	}
	if len(cals) == 0 {
		return CalendarSeed()
	}
	return cals
}
